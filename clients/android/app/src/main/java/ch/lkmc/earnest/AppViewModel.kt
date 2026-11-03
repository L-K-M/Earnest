package ch.lkmc.earnest

import android.app.Application
import androidx.core.content.edit
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import ch.lkmc.earnest.core.Activity
import ch.lkmc.earnest.core.Annotation
import ch.lkmc.earnest.core.ApiException
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.RejectedOperation
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.StateWrite
import ch.lkmc.earnest.core.SubscriptionBody
import ch.lkmc.earnest.core.SubscriptionPatch
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncRepository
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.db.BriefEntity
import ch.lkmc.earnest.db.ContentEntity
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

data class UiState(
    val snapshot: SyncSnapshot = SyncSnapshot(Revision("", 0)),
    val principalId: Long = 0,
    val states: Map<Long, State> = emptyMap(),
    val cadences: Map<Long, String> = emptyMap(),
    val todayEntryIds: Set<Long> = initialTodayIds(snapshot, states, cadences),
    val accountKey: String? = null,
    val cacheNotice: String? = null,
    val agentChanges: List<Activity> = emptyList(),
    val sessionAgentChanges: List<Activity> = emptyList(),
    val agentChangesError: String? = null,
    val briefs: List<BriefEntity> = emptyList(),
    val principals: Map<Long, String> = emptyMap(),
    val loading: Boolean = false,
    val error: String? = null,
    val rejected: List<RejectedOperation> = emptyList(),
    val expired: List<String> = emptyList(),
)

/**
 * Publishes account-scoped reader state. Foreground and background writers
 * share the reload-through-flush critical section in [SessionCache].
 */
class AppViewModel internal constructor(
    application: Application,
    private val sessions: SessionStore,
    private val apiFactory: (AuthenticatedSession) -> EarnestApi,
) : AndroidViewModel(application) {
    constructor(application: Application) : this(application, Secrets(application), {
        EarnestApi(it.identity.server, it.token)
    })

    @Volatile private var account: SessionCache? = null
    @Volatile private var activeSession: AuthenticatedSession? = null
    private var sessionStartedAt = Instant.now()
    private var changesSince = Instant.EPOCH.toString()
    private val json = Json { ignoreUnknownKeys = true }

    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state.asStateFlow()

    private val _loggedIn = MutableStateFlow(sessions.current() != null)
    val loggedIn: StateFlow<Boolean> = _loggedIn.asStateFlow()

    init {
        if (_loggedIn.value) onLoggedIn()
    }

    /** Called only after /v1/me established the authenticated account. */
    fun onLoggedIn() {
        account = null
        activeSession = null
        _state.value = UiState()
        viewModelScope.launch(Dispatchers.IO) {
            val session = sessions.current() ?: return@launch
            val cache = SessionCache(getApplication(), session.identity)
            try {
                cache.withEngine { syncEngine ->
                    if (sessions.current() != session) return@withEngine
                    account = cache
                    activeSession = session
                    sessionStartedAt = Instant.now()
                    changesSince = cache.preferences.getString(SessionCache.KEY_CHANGES_SINCE, null)
                        ?.takeIf { runCatching { Instant.parse(it) }.isSuccess } ?: Instant.EPOCH.toString()
                    val changes = activitySince(cache.agentChanges(), Instant.parse(changesSince))
                    _state.value = restoredState(syncEngine, session.identity.principalId).copy(
                        accountKey = session.identity.cacheKey,
                        cacheNotice = if (cache.legacyCacheRetained) "An older cache is retained on this device. Its unsent actions need manual recovery because their account is unknown." else null,
                        briefs = cache.db.briefs().get(),
                        principals = cachedPrincipalNames(cache),
                        agentChanges = changes,
                        agentChangesError = if (cache.agentChangesStale) "Agent changes could not be refreshed. Reconnect to check automated actions." else null,
                    )
                    _loggedIn.value = true
                }
            } catch (error: Exception) {
                if (sessions.current() == session) {
                    _state.value = UiState(error = "Could not restore the account cache: ${error.message}")
                    _loggedIn.value = true
                }
                return@launch
            }
            refresh()
        }
    }

    fun logout() {
        sessions.clearToken()
        activeSession = null
        account = null
        _state.value = UiState()
        _loggedIn.value = false
    }

    /** Pulls change pages, flushes the outbox and refreshes the cached briefs. */
    fun refresh() {
        viewModelScope.launch(Dispatchers.IO) {
            val cache = account ?: return@launch
            val session = activeSession ?: return@launch
            if (!isCurrent(session, cache)) return@launch
            _state.value = _state.value.copy(loading = true, error = null)
            try {
                val api = apiFactory(session)
                cache.withEngine { syncEngine ->
                    if (!isCurrent(session, cache)) return@withEngine
                    val repository = SyncRepository(api, syncEngine)
                    cache.markAgentChangesStale()
                    val rejected = repository.pull() + repository.flush()
                    val expired = syncEngine.expire(Instant.now()).map { it.operation.opId }
                    syncEngine.flush()
                    val briefs = runCatching { api.todaysBriefs() }.getOrNull()
                    if (briefs != null) cacheBriefs(cache, briefs)
                    val cached = briefs?.map { BriefEntity(it.principalId, it.value, it.rev) } ?: cache.db.briefs().get()
                    val names = principalNames(api)
                    savePrincipalNames(cache, names)
                    val changesResult = runCatching { cache.refreshAgentChanges(api, changesSince) }
                    changesResult.getOrNull()?.let {
                        cache.preferences.edit {
                            putString(SessionCache.KEY_CHANGES_SINCE, sessionStartedAt.toString())
                        }
                    }
                    if (!isCurrent(session, cache)) return@withEngine
                    val restored = restoredState(syncEngine, cache.identity.principalId)
                    val changes = changesResult.getOrNull() ?: _state.value.agentChanges
                    _state.value = _state.value.copy(
                        snapshot = syncEngine.snapshot(),
                        principalId = cache.identity.principalId,
                        states = effectiveStates(syncEngine, cache.identity.principalId),
                        cadences = restored.cadences,
                        todayEntryIds = restored.todayEntryIds,
                        agentChanges = changes,
                        sessionAgentChanges = activitySince(changes, sessionStartedAt),
                        agentChangesError = changesResult.exceptionOrNull()?.let { "Could not refresh agent changes: ${it.message}" },
                        briefs = cached,
                        principals = names.ifEmpty { cachedPrincipalNames(cache) },
                        rejected = rejected,
                        error = if (rejected.isEmpty()) null else "Some reading actions could not be synced because they expired or their articles were removed.",
                        expired = expired,
                    )
                }
            } catch (error: ApiException) {
                if (!isCurrent(session, cache)) return@launch
                if (error.isUnauthorized) {
                    logout()
                }
                _state.value = _state.value.copy(error = error.message)
            } catch (error: Exception) {
                if (isCurrent(session, cache)) _state.value = _state.value.copy(error = error.message)
            } finally {
                if (isCurrent(session, cache)) _state.value = _state.value.copy(loading = false)
            }
        }
    }

    fun markSeen(entryId: Long) {
        queue(entryId, SyncEngine.REGISTER_SEEN, JsonPrimitive(SyncEngine.VALUE_SEEN), SyncEngine.KIND_PASSIVE, "render")
    }

    fun later(entryId: Long, onSaved: () -> Unit = {}) {
        queue(entryId, SyncEngine.REGISTER_SHELF, JsonPrimitive(SyncEngine.VALUE_LATER), SyncEngine.KIND_EXPLICIT, "later", onSaved)
    }

    fun done(entryId: Long, onSaved: () -> Unit = {}) {
        queue(entryId, SyncEngine.REGISTER_SHELF, JsonPrimitive(SyncEngine.VALUE_DONE), SyncEngine.KIND_EXPLICIT, "done", onSaved)
    }

    fun keep(entryId: Long) {
        queue(entryId, SyncEngine.REGISTER_KEPT, JsonPrimitive(true), SyncEngine.KIND_EXPLICIT, "keep")
    }

    fun unkeep(entryId: Long) {
        queue(entryId, SyncEngine.REGISTER_KEPT, JsonPrimitive(false), SyncEngine.KIND_EXPLICIT, "unkeep")
    }

    fun undoShelf(entryId: Long) {
        queue(entryId, SyncEngine.REGISTER_SHELF, JsonPrimitive(SyncEngine.VALUE_NONE), SyncEngine.KIND_EXPLICIT, "undo")
    }

    fun undoDone(entryId: Long) {
        queue(entryId, SyncEngine.REGISTER_SHELF, JsonPrimitive(SyncEngine.VALUE_NONE), SyncEngine.KIND_EXPLICIT, "undo")
    }

    private fun queue(entryId: Long, register: String, value: JsonElement, kind: String, action: String, onSaved: () -> Unit = {}) {
        viewModelScope.launch(Dispatchers.IO) {
            val cache = account ?: return@launch
            val session = activeSession ?: return@launch
            try {
                cache.withEngine { syncEngine ->
                    if (!isCurrent(session, cache)) return@withEngine
                    val principal = cache.identity.principalId
                    if (principal == 0L || syncEngine.snapshot().cursor.generation.isEmpty()) {
                        _state.value = _state.value.copy(error = "Still signing in; the action was not saved.")
                        return@withEngine
                    }
                    val current = syncEngine.state(principal, entryId)
                    val base = when (register) {
                        SyncEngine.REGISTER_SHELF -> current.shelf.rev
                        SyncEngine.REGISTER_KEPT -> current.kept.rev
                        else -> current.seen.rev
                    }
                    val write = StateWrite(
                        opId = "$action-$entryId-${java.util.UUID.randomUUID()}",
                        principal = principal,
                        entryId = entryId,
                        register = register,
                        value = value,
                        base = Revision(syncEngine.snapshot().cursor.generation, base),
                        occurredAt = Instant.now().toString(),
                        kind = kind,
                    )
                    syncEngine.queue(write)
                    syncEngine.flush()
                    _state.value = _state.value.copy(
                        snapshot = syncEngine.snapshot(),
                        states = effectiveStates(syncEngine, principal),
                        cadences = restoredState(syncEngine, principal).cadences,
                        agentChanges = cache.agentChanges(),
                        sessionAgentChanges = activitySince(cache.agentChanges(), sessionStartedAt),
                    )
                    onSaved()
                }
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                if (isCurrent(session, cache)) {
                    _state.value = _state.value.copy(error = "Could not save the action: ${error.message}")
                }
            }
        }
    }

    fun subscribe(url: String, cadence: String) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.subscribe(SubscriptionBody(url = url, opId = opId("subscribe"), cadence = cadence)) }
                .onSuccess { refresh() }
                .onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    fun unsubscribe(subscriptionId: Long) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.unsubscribe(subscriptionId, opId("unsubscribe")) }
                .onSuccess { refresh() }
                .onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    fun setCadence(subscriptionId: Long, cadence: String) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching {
                api.updateSubscription(subscriptionId, SubscriptionPatch(opId = opId("cadence"), cadence = cadence))
            }.onSuccess { refresh() }
            .onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    fun revert(activityId: Long) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.revertActivity(activityId, opId("revert")) }
                .onSuccess { refresh() }
                .onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    /**
     * Fetches the signed content URL and caches the readable content. Returns
     * null when the server cannot be reached and nothing is cached.
     */
    suspend fun openEntry(entryId: Long): ContentUrlResult? = withContext(Dispatchers.IO) {
        val cache = account ?: return@withContext null
        val session = activeSession ?: return@withContext null
        if (!isCurrent(session, cache)) return@withContext null
        val api = apiFactory(session)
        val cached = cache.db.content().get(entryId)
        val content = runCatching { api.content(entryId) }.getOrNull()
        content?.let {
            cache.db.content().save(
                ContentEntity(
                    entryId = entryId,
                    html = it.html,
                    text = it.text,
                    sanitizerVersion = it.sanitizerVersion,
                    source = it.source,
                    updatedAt = System.currentTimeMillis(),
                ),
            )
        }
        val url = runCatching { api.contentUrl(entryId) }.getOrNull()
        val fallback = content?.html ?: cached?.html
        if (!isCurrent(session, cache)) return@withContext null
        when {
            url != null -> ContentUrlResult(url.url, url.expiresAt, fallback)
            fallback != null -> ContentUrlResult(null, null, fallback)
            else -> null
        }
    }

    fun activity(since: String) = kotlinx.coroutines.flow.flow {
        val api = api() ?: return@flow
        emit(runCatching { api.activity(since = since, limit = 100, grouped = true) }.getOrNull())
    }

    /** Resolves a stored notification to the entry it should open. */
    suspend fun notificationEntry(notificationId: Long): Long? = withContext(Dispatchers.IO) {
        val cache = account ?: return@withContext null
        val session = activeSession ?: return@withContext null
        val api = api() ?: return@withContext null
        val entry = runCatching { api.notification(notificationId).entryIds?.firstOrNull() }.getOrNull()
        entry.takeIf { isCurrent(session, cache) }
    }

    internal suspend fun loadEntry(entryId: Long): ch.lkmc.earnest.core.CachedEntry? = withContext(Dispatchers.IO) {
        val cache = account ?: return@withContext null
        val session = activeSession ?: return@withContext null
        cache.withEngine { engine ->
            if (!isCurrent(session, cache)) return@withEngine null
            val entry = SyncRepository(apiFactory(session), engine).loadEntry(entryId)
            if (!isCurrent(session, cache)) return@withEngine null
            _state.value = _state.value.copy(
                snapshot = engine.snapshot(),
                states = effectiveStates(engine, cache.identity.principalId),
            )
            entry
        }
    }

    private fun isCurrent(session: AuthenticatedSession, cache: SessionCache): Boolean =
        account === cache && activeSession == session && sessions.current() == session

    private fun api(): EarnestApi? {
        val session = sessions.current() ?: return null
        if (session.identity != account?.identity) return null
        return apiFactory(session)
    }

    /** Maps principal IDs to the names agents chose for themselves. */
    private suspend fun principalNames(api: EarnestApi): Map<Long, String> =
        runCatching { api.tokens() }.getOrDefault(emptyList())
            .filter { it.revokedAt == null && it.name.isNotBlank() }
            .associate { it.principalId to it.name }

    /** Keeps the last known agent names so briefs stay labelled offline. */
    private fun savePrincipalNames(cache: SessionCache, names: Map<Long, String>) {
        if (names.isEmpty()) return
        val encoded = runCatching {
            json.encodeToString(MapSerializer(Long.serializer(), String.serializer()), names)
        }.getOrNull() ?: return
        cache.preferences
            .edit { putString(KEY_PRINCIPALS, encoded) }
    }

    private fun cachedPrincipalNames(cache: SessionCache): Map<Long, String> {
        val encoded = cache.preferences
            .getString(KEY_PRINCIPALS, null) ?: return emptyMap()
        return runCatching {
            json.decodeFromString(MapSerializer(Long.serializer(), String.serializer()), encoded)
        }.getOrDefault(emptyMap())
    }

    private suspend fun cacheBriefs(cache: SessionCache, briefs: List<Annotation>) {
        cache.db.briefs().clear()
        cache.db.briefs().saveAll(briefs.map { BriefEntity(it.principalId, it.value, it.rev) })
    }

    private fun effectiveStates(syncEngine: SyncEngine, principal: Long): Map<Long, State> {
        if (principal == 0L) return emptyMap()
        return syncEngine.snapshot().entries
            .filterNot { it.deleted }
            .associate { it.id to syncEngine.state(principal, it.id) }
    }

    private fun opId(action: String) = "$action-${java.util.UUID.randomUUID()}"

    companion object {
        private const val KEY_PRINCIPALS = "principal_names"
    }
}

/** The signed content URL plus the cached HTML fallback for offline reading. */
data class ContentUrlResult(
    val url: String?,
    val expiresAt: String?,
    val cachedHtml: String?,
)

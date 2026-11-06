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
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.db.BriefEntity
import ch.lkmc.earnest.db.ContentEntity
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
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
    val riverEntryIds: Set<Long> = initialRiverIds(snapshot, states),
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
    private val syncSignals: Flow<String> = SyncAccess.committed,
    private val apiFactory: (AuthenticatedSession) -> EarnestApi,
) : AndroidViewModel(application) {
    constructor(application: Application) : this(application, Secrets(application), apiFactory = {
        EarnestApi(it.identity.server, it.token)
    })

    @Volatile private var account: SessionCache? = null
    @Volatile private var activeSession: AuthenticatedSession? = null
    private var sessionStartedAt = Instant.now()
    private var changesSince = Instant.EPOCH.toString()
    private val json = Json { ignoreUnknownKeys = true }
    private val flushScheduler by lazy {
        DebouncedFlush(viewModelScope, FLUSH_DEBOUNCE_MILLIS) { flushOutbox() }
    }

    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state.asStateFlow()

    private val _loggedIn = MutableStateFlow(sessions.current() != null)
    val loggedIn: StateFlow<Boolean> = _loggedIn.asStateFlow()

    init {
        viewModelScope.launch {
            syncSignals.collect { key ->
                if (key == activeSession?.identity?.cacheKey) reloadFromCache()
            }
        }
        if (_loggedIn.value) onLoggedIn()
    }

    override fun onCleared() {
        flushScheduler.cancel()
        super.onCleared()
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
                        agentChangesError = if (cache.agentChangesStale) AGENT_CHANGES_STALE_MESSAGE else null,
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

    /**
     * Rebuilds the UI from the account cache after a background writer (the
     * periodic WorkManager sync) committed. Makes no network requests, so
     * signalling an unchanged cache only recomputes the same state.
     */
    private fun reloadFromCache() {
        viewModelScope.launch(Dispatchers.IO) {
            val cache = account ?: return@launch
            val session = activeSession ?: return@launch
            if (!isCurrent(session, cache)) return@launch
            cache.withEngine { engine ->
                if (!isCurrent(session, cache)) return@withEngine
                val restored = restoredState(engine, cache.identity.principalId)
                val changes = cache.agentChanges()
                // update{} keeps the reload from clobbering a concurrent
                // foreground write to the state flow.
                _state.update { previous -> previous.copy(
                    snapshot = restored.snapshot,
                    principalId = restored.principalId,
                    states = restored.states,
                    cadences = restored.cadences,
                    riverEntryIds = restored.riverEntryIds,
                    agentChanges = changes,
                    sessionAgentChanges = activitySince(changes, sessionStartedAt),
                    agentChangesError = if (cache.agentChangesStale) AGENT_CHANGES_STALE_MESSAGE else null,
                    briefs = cache.db.briefs().get(),
                ) }
            }
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
            _state.update { current -> current.copy(loading = true, error = null) }
            try {
                val api = apiFactory(session)
                val synced = cache.withSync(api) { repository ->
                    if (!isCurrent(session, cache)) return@withSync null
                    cache.markAgentChangesStale()
                    val rejected = repository.pull() + repository.flush()
                    val expired = repository.expire(Instant.now()).map { it.operation.opId }
                    // The agent-changes cursor must advance under the sync lock:
                    // a concurrent worker cycle outside it could write an older one.
                    val changes = runCatching { cache.refreshAgentChanges(api, changesSince) }
                    changes.getOrNull()?.let {
                        cache.preferences.edit {
                            putString(SessionCache.KEY_CHANGES_SINCE, sessionStartedAt.toString())
                        }
                    }
                    Triple(rejected, expired, changes)
                } ?: return@launch
                val (rejected, expired, changesResult) = synced
                val briefs = runCatching { api.todaysBriefs() }.getOrNull()
                if (briefs != null) cacheBriefs(cache, briefs)
                val cached = briefs?.map { BriefEntity(it.principalId, it.value, it.rev) } ?: cache.db.briefs().get()
                val names = principalNames(api)
                savePrincipalNames(cache, names)
                cache.withEngine { syncEngine ->
                    if (!isCurrent(session, cache)) return@withEngine
                    val restored = restoredState(syncEngine, cache.identity.principalId)
                    val changes = changesResult.getOrNull() ?: _state.value.agentChanges
                    _state.update { current -> current.copy(
                        snapshot = restored.snapshot,
                        principalId = restored.principalId,
                        states = restored.states,
                        cadences = restored.cadences,
                        riverEntryIds = restored.riverEntryIds,
                        agentChanges = changes,
                        sessionAgentChanges = activitySince(changes, sessionStartedAt),
                        agentChangesError = changesResult.exceptionOrNull()?.let { "Could not refresh agent changes: ${it.message}" },
                        briefs = cached,
                        principals = names.ifEmpty { cachedPrincipalNames(cache) },
                        rejected = rejected,
                        error = droppedOperationMessage(rejected, expired),
                        expired = expired,
                    ) }
                }
                refreshWidgets(getApplication())
            } catch (error: ApiException) {
                if (!isCurrent(session, cache)) return@launch
                if (error.isUnauthorized) {
                    logout()
                }
                _state.update { current -> current.copy(error = error.message) }
            } catch (error: Exception) {
                if (isCurrent(session, cache)) _state.update { current -> current.copy(error = error.message) }
            } finally {
                if (isCurrent(session, cache)) _state.update { current -> current.copy(loading = false) }
            }
        }
    }

    fun markSeen(entryId: Long) {
        queue(entryId, SyncEngine.REGISTER_SEEN, JsonPrimitive(SyncEngine.VALUE_SEEN), SyncEngine.KIND_PASSIVE, "seen")
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
                        _state.update { current -> current.copy(error = "Still signing in; the action was not saved.") }
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
                    val restored = restoredState(syncEngine, principal)
                    val agentChanges = cache.agentChanges()
                    _state.update { current -> current.copy(
                        snapshot = restored.snapshot,
                        states = restored.states,
                        cadences = restored.cadences,
                        agentChanges = agentChanges,
                        sessionAgentChanges = activitySince(agentChanges, sessionStartedAt),
                    ) }
                    onSaved()
                    flushScheduler.request()
                }
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                if (isCurrent(session, cache)) {
                    _state.update { current -> current.copy(error = "Could not save the action: ${error.message}") }
                }
            }
        }
    }

    /**
     * Sends the durable outbox shortly after the last queued action. A network
     * failure leaves the queue intact for the periodic sync; only a rejected
     * operation is reported, because local state still shows the intent.
     */
    private suspend fun flushOutbox() {
        // Room refuses main-thread access, so the flush leaves Main after the debounce.
        withContext(Dispatchers.IO) {
            val cache = account ?: return@withContext
            val session = activeSession ?: return@withContext
            if (!isCurrent(session, cache)) return@withContext
            try {
                // withSync serialises the flush against pull cycles; its engine
                // sections take SyncAccess.mutex only around load-mutate-save,
                // so the batch request never blocks a queued action.
                val synced = cache.withSync(apiFactory(session)) { repository ->
                    if (!isCurrent(session, cache)) return@withSync null
                    val rejected = repository.flush()
                    val expired = repository.expire(Instant.now()).map { it.operation.opId }
                    rejected to expired
                } ?: return@withContext
                val (rejected, expired) = synced
                cache.withEngine { syncEngine ->
                    if (!isCurrent(session, cache)) return@withEngine
                    // The session's river membership is left alone: only a refresh
                    // starts a new one, so passive seen marks do not drop rows.
                    // The update is atomic: a concurrent writer (refresh's loading
                    // flag, an action error) must not be clobbered by this rebuild.
                    // Read the engine before the update: its lambda may run more than once.
                    val snapshot = syncEngine.snapshot()
                    val principalId = cache.identity.principalId
                    val states = effectiveStates(syncEngine, principalId)
                    val agentChanges = cache.agentChanges()
                    _state.update { current ->
                        current.copy(
                            snapshot = snapshot,
                            principalId = principalId,
                            states = states,
                            agentChanges = agentChanges,
                            sessionAgentChanges = activitySince(agentChanges, sessionStartedAt),
                            rejected = rejected,
                            error = droppedOperationMessage(rejected, expired) ?: current.error,
                        )
                    }
                }
            } catch (error: ApiException) {
                if (!isCurrent(session, cache)) return@withContext
                if (error.isUnauthorized) logout()
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                // The queue is durable; the periodic sync retries without user action.
            }
        }
    }

    fun subscribe(url: String, cadence: String) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.subscribe(SubscriptionBody(url = url, opId = opId("subscribe"), cadence = cadence)) }
                .onSuccess { refresh() }
                .onFailure { _state.update { current -> current.copy(error = it.message) } }
        }
    }

    fun unsubscribe(subscriptionId: Long) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.unsubscribe(subscriptionId, opId("unsubscribe")) }
                .onSuccess { refresh() }
                .onFailure { _state.update { current -> current.copy(error = it.message) } }
        }
    }

    fun setCadence(subscriptionId: Long, cadence: String) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching {
                api.updateSubscription(subscriptionId, SubscriptionPatch(opId = opId("cadence"), cadence = cadence))
            }.onSuccess { refresh() }
            .onFailure { _state.update { current -> current.copy(error = it.message) } }
        }
    }

    fun revert(activityId: Long) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.revertActivity(activityId, opId("revert")) }
                .onSuccess { refresh() }
                .onFailure { _state.update { current -> current.copy(error = it.message) } }
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
        if (!isCurrent(session, cache)) return@withContext null
        val entry = cache.repository(apiFactory(session)).loadEntry(entryId)
        cache.withEngine { engine ->
            if (!isCurrent(session, cache)) return@withEngine null
            val snapshot = engine.snapshot()
            val states = effectiveStates(engine, cache.identity.principalId)
            _state.update { current -> current.copy(snapshot = snapshot, states = states) }
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
        return syncEngine.states(principal)
    }

    private fun opId(action: String) = "$action-${java.util.UUID.randomUUID()}"

    /** Summarises dropped operations without exposing internal operation IDs. */
    private fun droppedOperationMessage(rejected: List<RejectedOperation>, expired: List<String>): String? {
        if (rejected.isEmpty() && expired.isEmpty()) return null
        val expiredCount = rejected.count { it.status == "expired" } + expired.size
        val goneCount = rejected.count { it.status == "gone" }
        val otherCount = rejected.size - rejected.count { it.status == "expired" || it.status == "gone" }
        val parts = mutableListOf<String>()
        if (expiredCount > 0) parts += "$expiredCount expired before they could sync"
        if (goneCount > 0) parts += "$goneCount could not be applied because the article was removed"
        if (otherCount > 0) parts += "$otherCount could not be synced"
        return "Some reading actions could not be synced: " + parts.joinToString(", ")
    }

    companion object {
        private const val KEY_PRINCIPALS = "principal_names"
        private const val AGENT_CHANGES_STALE_MESSAGE =
            "Agent changes could not be refreshed. Reconnect to check automated actions."
        private const val FLUSH_DEBOUNCE_MILLIS = 1500L
    }
}

/** The signed content URL plus the cached HTML fallback for offline reading. */
data class ContentUrlResult(
    val url: String?,
    val expiresAt: String?,
    val cachedHtml: String?,
)

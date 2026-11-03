package ch.lkmc.earnest

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
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
import ch.lkmc.earnest.db.AppDatabase
import ch.lkmc.earnest.db.BriefEntity
import ch.lkmc.earnest.db.ContentEntity
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.time.Instant

data class UiState(
    val snapshot: SyncSnapshot = SyncSnapshot(Revision("", 0)),
    val principalId: Long = 0,
    val states: Map<Long, State> = emptyMap(),
    val cadences: Map<Long, String> = emptyMap(),
    val briefs: List<BriefEntity> = emptyList(),
    val loading: Boolean = false,
    val error: String? = null,
    val rejected: List<RejectedOperation> = emptyList(),
    val expired: List<String> = emptyList(),
)

/**
 * Owns the sync engine and the cached reader state. All engine access is
 * serialized on one dispatcher; the UI only sees [UiState].
 */
class AppViewModel(application: Application) : AndroidViewModel(application) {
    private val secrets = Secrets(application)
    private val db = AppDatabase.get(application)
    private val json = Json { ignoreUnknownKeys = true }
    private val mutex = Mutex()
    private var engine: SyncEngine? = null

    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state.asStateFlow()

    val server: String = secrets.server

    private val _loggedIn = MutableStateFlow(secrets.token() != null)
    val loggedIn: StateFlow<Boolean> = _loggedIn.asStateFlow()

    init {
        if (_loggedIn.value) {
            viewModelScope.launch(Dispatchers.IO) {
                val store = RoomSyncStore(db)
                val syncEngine = SyncEngine(store)
                mutex.withLock {
                    engine = syncEngine
                    _state.value = _state.value.copy(
                        snapshot = syncEngine.snapshot(),
                        principalId = principalId(),
                        states = effectiveStates(syncEngine, principalId()),
                    )
                }
                refresh()
            }
        }
    }

    /** Called after a successful device authorization. */
    fun onLoggedIn() {
        _loggedIn.value = true
        viewModelScope.launch(Dispatchers.IO) {
            val store = RoomSyncStore(db)
            val syncEngine = SyncEngine(store)
            mutex.withLock {
                engine = syncEngine
                _state.value = _state.value.copy(
                    snapshot = syncEngine.snapshot(),
                    principalId = principalId(),
                    states = effectiveStates(syncEngine, principalId()),
                )
            }
            refresh()
        }
    }

    fun logout() {
        secrets.clearToken()
        _loggedIn.value = false
    }

    /** Pulls change pages, flushes the outbox and refreshes the cached briefs. */
    fun refresh() {
        viewModelScope.launch(Dispatchers.IO) {
            val syncEngine = engine ?: return@launch
            _state.value = _state.value.copy(loading = true, error = null)
            try {
                val api = api() ?: return@launch
                mutex.withLock {
                    syncEngine.reload()
                    val repository = SyncRepository(api, syncEngine)
                    repository.pull()
                    val rejected = repository.flush()
                    val expired = syncEngine.expire(Instant.now()).map { it.operation.opId }
                    syncEngine.flush()
                    val me = runCatching { api.me() }.getOrNull()
                    me?.let { savePrincipal(it.principalId) }
                    val briefs = runCatching { api.todaysBriefs() }.getOrDefault(emptyList())
                    cacheBriefs(briefs)
                    _state.value = _state.value.copy(
                        snapshot = syncEngine.snapshot(),
                        principalId = me?.principalId ?: principalId(),
                        states = effectiveStates(syncEngine, me?.principalId ?: principalId()),
                        cadences = cadences(syncEngine),
                        briefs = briefs.map { BriefEntity(it.principalId, it.value, it.rev) },
                        rejected = rejected,
                        expired = expired,
                    )
                }
            } catch (error: ApiException) {
                if (error.isUnauthorized) {
                    secrets.clearToken()
                }
                _state.value = _state.value.copy(error = error.message)
            } catch (error: Exception) {
                _state.value = _state.value.copy(error = error.message)
            } finally {
                _state.value = _state.value.copy(loading = false)
            }
        }
    }

    fun markSeen(entryId: Long) {
        queue(entryId, SyncEngine.REGISTER_SEEN, JsonPrimitive(SyncEngine.VALUE_SEEN), SyncEngine.KIND_PASSIVE, "render")
    }

    fun later(entryId: Long) {
        queue(entryId, SyncEngine.REGISTER_SHELF, JsonPrimitive(SyncEngine.VALUE_LATER), SyncEngine.KIND_EXPLICIT, "later")
    }

    fun done(entryId: Long) {
        queue(entryId, SyncEngine.REGISTER_SHELF, JsonPrimitive(SyncEngine.VALUE_DONE), SyncEngine.KIND_EXPLICIT, "done")
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

    private fun queue(entryId: Long, register: String, value: JsonElement, kind: String, action: String) {
        viewModelScope.launch(Dispatchers.IO) {
            val syncEngine = engine ?: return@launch
            mutex.withLock {
                val principal = principalId()
                val current = syncEngine.state(principal, entryId)
                val base = when (register) {
                    SyncEngine.REGISTER_SHELF -> current.shelf.rev
                    SyncEngine.REGISTER_KEPT -> current.kept.rev
                    else -> current.seen.rev
                }
                val write = StateWrite(
                    opId = "$action-$entryId-${System.currentTimeMillis()}",
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
                )
            }
        }
    }

    fun subscribe(url: String, cadence: String) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.subscribe(SubscriptionBody(url = url, opId = opId("subscribe"), cadence = cadence)) }
                .onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    fun unsubscribe(subscriptionId: Long) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.unsubscribe(subscriptionId, opId("unsubscribe")) }
                .onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    fun setCadence(subscriptionId: Long, cadence: String) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching {
                api.updateSubscription(subscriptionId, SubscriptionPatch(opId = opId("cadence"), cadence = cadence))
            }.onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    fun revert(activityId: Long) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.revertActivity(activityId, opId("revert")) }
                .onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    /** The cached reader content, or null when it has never been opened. */
    fun content(entryId: Long) = db.content().entry(entryId)

    suspend fun cachedContent(entryId: Long): ContentEntity? = db.content().get(entryId)

    /**
     * Fetches the signed content URL and caches the readable content. Returns
     * null when the server cannot be reached and nothing is cached.
     */
    suspend fun openEntry(entryId: Long): ContentUrlResult? = withContext(Dispatchers.IO) {
        val api = api() ?: return@withContext null
        val cached = db.content().get(entryId)
        val content = runCatching { api.content(entryId) }.getOrNull()
        content?.let {
            db.content().save(
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
        val api = api() ?: return@withContext null
        runCatching { api.notification(notificationId).entryIds?.firstOrNull() }.getOrNull()
    }

    fun registerPush() {
        EarnestPushService.register(getApplication())
    }

    fun setShowAgentText(enabled: Boolean) {
        getApplication<Application>().getSharedPreferences("push", Application.MODE_PRIVATE)
            .edit().putBoolean("show_text", enabled).apply()
    }

    fun showAgentText(): Boolean =
        getApplication<Application>().getSharedPreferences("push", Application.MODE_PRIVATE)
            .getBoolean("show_text", false)

    private fun api(): EarnestApi? {
        val token = secrets.token() ?: return null
        return EarnestApi(secrets.server, token)
    }

    private fun principalId(): Long {
        val prefs = getApplication<Application>().getSharedPreferences(PREFS, Application.MODE_PRIVATE)
        return prefs.getLong(KEY_PRINCIPAL, 0L)
    }

    private fun savePrincipal(principalId: Long) {
        getApplication<Application>().getSharedPreferences(PREFS, Application.MODE_PRIVATE)
            .edit().putLong(KEY_PRINCIPAL, principalId).apply()
    }

    private suspend fun cacheBriefs(briefs: List<Annotation>) {
        db.briefs().clear()
        db.briefs().saveAll(briefs.map { BriefEntity(it.principalId, it.value, it.rev) })
    }

    private fun effectiveStates(syncEngine: SyncEngine, principal: Long): Map<Long, State> {
        if (principal == 0L) return emptyMap()
        return syncEngine.snapshot().entries
            .filterNot { it.deleted }
            .associate { it.id to syncEngine.state(principal, it.id) }
    }

    private fun cadences(syncEngine: SyncEngine): Map<Long, String> =
        syncEngine.snapshot().objects
            .filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted && it.raw.isNotEmpty() }
            .associate { obj ->
                val cadence = runCatching { json.parseToJsonElement(obj.raw).jsonObject["cadence"]?.jsonPrimitive?.contentOrNull }
                    .getOrNull() ?: "today"
                obj.id to cadence
            }

    private fun opId(action: String) = "$action-${System.currentTimeMillis()}"

    companion object {
        private const val PREFS = "session"
        private const val KEY_PRINCIPAL = "principal_id"
    }
}

/** The signed content URL plus the cached HTML fallback for offline reading. */
data class ContentUrlResult(
    val url: String?,
    val expiresAt: String?,
    val cachedHtml: String?,
)

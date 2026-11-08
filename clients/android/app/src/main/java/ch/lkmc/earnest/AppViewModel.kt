package ch.lkmc.earnest

import android.app.Application
import androidx.core.content.edit
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import ch.lkmc.earnest.core.Activity
import ch.lkmc.earnest.core.Annotation
import ch.lkmc.earnest.core.ApiException
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.ReadingMode
import ch.lkmc.earnest.core.RejectedOperation
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.StateWrite
import ch.lkmc.earnest.core.SubscriptionBody
import ch.lkmc.earnest.core.SubscriptionPatch
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.core.UnreadCounts
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
    val readerSettings: ReaderSettings = ReaderSettings(),
    val readerSettingsRev: Long = 0,
    val readerSettingsUi: ReaderSettingsUi = ReaderSettingsUi(),
    val unreadCounts: UnreadCounts? = null,
    val unreadCountsSyncedAt: Long? = null,
    val unreadCountsError: String? = null,
)

/** The reader-preferences screen's draft and save state. */
data class ReaderSettingsUi(
    val loading: Boolean = false,
    val saving: Boolean = false,
    val error: String? = null,
    val success: Boolean = false,
    val draft: ReaderSettings? = null,
    val loaded: ReaderSettings? = null,
    val loadedRev: Long = 0,
    val conflict: ReaderSettings? = null,
    val frozen: FrozenReaderSettings? = null,
)

/** A save whose outcome is unknown; a retry must resend the identical body. */
data class FrozenReaderSettings(
    val settings: ReaderSettings,
    val opId: String,
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

    internal val search = SearchModel(viewModelScope) {
        activeAccount("Still signing in; search did not run.")?.let { (cache, session) ->
            val generation = _state.value.snapshot.cursor.generation
            SearchSession(
                api = apiFactory(session),
                isCurrent = { currentOrExpired(session, cache) && _state.value.snapshot.cursor.generation == generation },
                unauthorized = {
                    if (currentOrExpired(session, cache) && sessions.current() == session) {
                        sessions.clearToken()
                        if (sessions.current() == null) sessionExpired()
                    }
                },
            )
        }
    }

    private val _loggedIn = MutableStateFlow(sessions.current() != null)
    val loggedIn: StateFlow<Boolean> = _loggedIn.asStateFlow()

    private val _sessionNotice = MutableStateFlow<String?>(null)
    val sessionNotice: StateFlow<String?> = _sessionNotice.asStateFlow()

    init {
        viewModelScope.launch {
            syncSignals.collect { key ->
                if (key == activeSession?.identity?.cacheKey) reloadFromCache()
            }
        }
        if (_loggedIn.value) onLoggedIn()
    }

    override fun onCleared() {
        search.reset()
        flushScheduler.cancel()
        super.onCleared()
    }

    /** Called only after /v1/me established the authenticated account. */
    fun onLoggedIn() {
        search.reset()
        account = null
        activeSession = null
        _sessionNotice.value = null
        _state.value = UiState()
        viewModelScope.launch(Dispatchers.IO) {
            val session = sessions.current()
            if (session == null) {
                sessionExpired()
                return@launch
            }
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
                    val restored = restoredState(syncEngine, session.identity.principalId)
                    search.invalidateForGeneration(restored.snapshot.cursor.generation)
                    _state.value = restored.copy(
                        accountKey = session.identity.cacheKey,
                        cacheNotice = if (cache.legacyCacheRetained) "An older cache is retained on this device. Its unsent actions need manual recovery because their account is unknown." else null,
                        briefs = cache.db.briefs().get(),
                        principals = cachedPrincipalNames(cache),
                        agentChanges = changes,
                        agentChangesError = if (cache.agentChangesStale) AGENT_CHANGES_STALE_MESSAGE else null,
                        readerSettingsUi = ReaderSettingsUi(
                            loaded = restored.readerSettings,
                            loadedRev = restored.readerSettingsRev,
                        ),
                        unreadCounts = cache.cachedUnreadCounts(),
                        unreadCountsSyncedAt = cache.cachedUnreadCountsAt(),
                    )
                    _loggedIn.value = true
                }
            } catch (error: Exception) {
                if (sessions.current() == session) {
                    _state.value = UiState(error = failureText("Could not restore the account cache", error))
                    _loggedIn.value = true
                } else if (sessions.current() == null) {
                    sessionExpired()
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
                search.invalidateForGeneration(restored.snapshot.cursor.generation)
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
                    readerSettings = restored.readerSettings,
                    readerSettingsRev = restored.readerSettingsRev,
                    unreadCounts = cache.cachedUnreadCounts(),
                    unreadCountsSyncedAt = cache.cachedUnreadCountsAt(),
                ) }
            }
        }
    }

    fun logout() {
        search.reset()
        sessions.clearToken()
        activeSession = null
        account = null
        _state.value = UiState()
        _loggedIn.value = false
    }

    /** Pulls change pages, flushes the outbox and refreshes the cached briefs. */
    fun refresh() {
        viewModelScope.launch(Dispatchers.IO) {
            val (cache, session) = activeAccount("Still signing in; the refresh did not run.") ?: return@launch
            _state.update { current -> current.copy(loading = true, error = null) }
            try {
                val api = apiFactory(session)
                val synced = cache.withSync(api) { repository ->
                    if (!currentOrExpired(session, cache)) return@withSync null
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
                    if (!currentOrExpired(session, cache)) return@withEngine
                    val restored = restoredState(syncEngine, cache.identity.principalId)
                    search.invalidateForGeneration(restored.snapshot.cursor.generation)
                    val changes = changesResult.getOrNull() ?: _state.value.agentChanges
                    _state.update { current -> current.copy(
                        snapshot = restored.snapshot,
                        principalId = restored.principalId,
                        states = restored.states,
                        cadences = restored.cadences,
                        riverEntryIds = restored.riverEntryIds,
                        agentChanges = changes,
                        sessionAgentChanges = activitySince(changes, sessionStartedAt),
                        agentChangesError = changesResult.exceptionOrNull()?.let { failureText("Could not refresh agent changes", it) },
                        briefs = cached,
                        principals = names.ifEmpty { cachedPrincipalNames(cache) },
                        rejected = rejected,
                        error = droppedOperationMessage(rejected, expired),
                        expired = expired,
                    ) }
                }
                refreshUnreadCounts(cache, session)
                refreshWidgets(getApplication())
            } catch (error: ApiException) {
                if (!currentOrExpired(session, cache)) return@launch
                if (error.isUnauthorized && sessions.current() == session) {
                    // Clear only the session that failed: a login that completed
                    // concurrently must not lose its token or get signed out.
                    sessions.clearToken()
                    if (sessions.current() == null) sessionExpired()
                }
                _state.update { current -> current.copy(error = failureText("Could not refresh", error)) }
            } catch (error: Exception) {
                if (currentOrExpired(session, cache)) {
                    _state.update { current -> current.copy(error = failureText("Could not refresh", error)) }
                }
            } finally {
                if (currentOrExpired(session, cache)) _state.update { current -> current.copy(loading = false) }
            }
        }
    }

    /**
     * Explicit load of the account's reader settings. Loading never mutates
     * the account snapshot; it only refreshes the presentation baseline.
     */
    fun loadReaderSettings() {
        viewModelScope.launch(Dispatchers.IO) {
            val (cache, session) = activeAccount("Still signing in; settings did not load.") ?: return@launch
            _state.update { it.copy(readerSettingsUi = it.readerSettingsUi.copy(loading = true, error = null)) }
            try {
                val settings = apiFactory(session).readerSettings()
                if (!currentOrExpired(session, cache)) return@launch
                _state.update { current ->
                    current.copy(
                        readerSettings = settings,
                        readerSettingsRev = settings.rev,
                        readerSettingsUi = current.readerSettingsUi.copy(
                            loading = false,
                            draft = null,
                            loaded = settings,
                            loadedRev = settings.rev,
                            conflict = null,
                            frozen = null,
                            success = false,
                        ),
                    )
                }
            } catch (error: ApiException) {
                if (!currentOrExpired(session, cache)) return@launch
                if (error.isUnauthorized && sessions.current() == session) {
                    sessions.clearToken()
                    if (sessions.current() == null) sessionExpired()
                }
                _state.update { it.copy(readerSettingsUi = it.readerSettingsUi.copy(loading = false, error = failureText("Could not load settings", error))) }
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                if (currentOrExpired(session, cache)) {
                    _state.update { it.copy(readerSettingsUi = it.readerSettingsUi.copy(loading = false, error = failureText("Could not load settings", error))) }
                }
            }
        }
    }

    /** Edits the draft; editing invalidates a frozen retry and keeps the conflict warning. */
    fun updateReaderSettingsDraft(transform: (ReaderSettings) -> ReaderSettings) {
        _state.update { current ->
            val ui = current.readerSettingsUi
            val base = ui.draft ?: ui.loaded ?: current.readerSettings
            val next = transform(base)
            val edited = next != base
            current.copy(
                readerSettingsUi = ui.copy(
                    draft = next,
                    frozen = if (edited) null else ui.frozen,
                    error = null,
                    success = false,
                ),
            )
        }
    }

    /** Closes the conflict warning without discarding the dirty draft. */
    fun dismissReaderSettingsConflict() {
        _state.update { it.copy(readerSettingsUi = it.readerSettingsUi.copy(conflict = null)) }
    }

    /** Discards the draft and loads the newest known settings (reload, not overwrite). */
    fun reloadReaderSettings() {
        _state.update {
            it.copy(readerSettingsUi = it.readerSettingsUi.copy(
                loading = true, error = null, draft = null, conflict = null, frozen = null, success = false,
            ))
        }
        loadReaderSettings()
    }

    /** Retry the ambiguous save with the identical frozen body and op ID. */
    fun retryReaderSettingsSave() {
        val frozen = _state.value.readerSettingsUi.frozen ?: return
        _state.update {
            it.copy(readerSettingsUi = it.readerSettingsUi.copy(draft = frozen.settings, error = null, success = false))
        }
        saveReaderSettings()
    }

    /**
     * Saves the draft. A draft older than the account's newest known revision
     * warns and reloads instead of overwriting. An ambiguous failure freezes
     * the exact body and op ID so [retryReaderSettingsSave] can resend it.
     */
    fun saveReaderSettings() {
        val ui = _state.value.readerSettingsUi
        val draft = ui.draft ?: ui.loaded ?: return
        val frozen = ui.frozen?.takeIf { it.settings == draft }
        viewModelScope.launch(Dispatchers.IO) {
            val (cache, session) = activeAccount("Still signing in; settings did not save.") ?: return@launch
            val newer = cache.withEngine { engine ->
                val current = cache.readerSettings(engine)
                if (current.rev > ui.loadedRev) current else null
            }
            if (newer != null && currentOrExpired(session, cache)) {
                _state.update {
                    it.copy(readerSettingsUi = it.readerSettingsUi.copy(
                        conflict = newer,
                        saving = false,
                        error = null,
                    ))
                }
                return@launch
            }
            val opId = frozen?.opId ?: "settings-${java.util.UUID.randomUUID()}"
            _state.update {
                it.copy(readerSettingsUi = it.readerSettingsUi.copy(
                    saving = true, error = null, success = false, conflict = null,
                    frozen = FrozenReaderSettings(draft, opId),
                ))
            }
            try {
                val updated = apiFactory(session).updateReaderSettings(draft, opId)
                if (!currentOrExpired(session, cache)) return@launch
                cache.mergeReaderSettings(updated)
                SyncAccess.committed.tryEmit(session.identity.cacheKey)
                _state.update { current ->
                    val settingsUi = current.readerSettingsUi
                    current.copy(
                        readerSettings = updated,
                        readerSettingsRev = updated.rev,
                        readerSettingsUi = settingsUi.copy(
                            saving = false,
                            success = true,
                            error = null,
                            draft = null,
                            loaded = updated,
                            loadedRev = updated.rev,
                            conflict = null,
                            frozen = null,
                        ),
                    )
                }
            } catch (error: ApiException) {
                if (!currentOrExpired(session, cache)) return@launch
                // A 401 invalidates the token: never replay or publish the save.
                if (error.isUnauthorized) {
                    _state.update {
                        it.copy(readerSettingsUi = it.readerSettingsUi.copy(saving = false, frozen = null, error = failureText("Could not save settings", error)))
                    }
                    if (sessions.current() == session) {
                        sessions.clearToken()
                        if (sessions.current() == null) sessionExpired()
                    }
                    return@launch
                }
                val deterministic = error.status in 400..499
                _state.update {
                    it.copy(readerSettingsUi = it.readerSettingsUi.copy(
                        saving = false,
                        frozen = if (deterministic) null else it.readerSettingsUi.frozen,
                        error = failureText("Could not save settings", error),
                    ))
                }
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                if (currentOrExpired(session, cache)) {
                    _state.update {
                        it.copy(readerSettingsUi = it.readerSettingsUi.copy(
                            saving = false,
                            frozen = it.readerSettingsUi.frozen,
                            error = failureText("Could not save settings", error),
                        ))
                    }
                }
            }
        }
    }

    /**
     * Fetches exact unread counts only in unread-count mode. A failure keeps
     * the last synced counts (never zero) and reports that they are stale.
     */
    private suspend fun refreshUnreadCounts(cache: SessionCache, session: AuthenticatedSession) {
        val settings = cache.withEngine { cache.readerSettings(it) }
        if (settings.readingMode != ReadingMode.UNREAD_COUNTS) return
        try {
            val counts = apiFactory(session).unreadCounts()
            if (!currentOrExpired(session, cache)) return
            cache.storeUnreadCounts(counts)
            _state.update {
                it.copy(unreadCounts = counts, unreadCountsSyncedAt = System.currentTimeMillis(), unreadCountsError = null)
            }
        } catch (error: kotlinx.coroutines.CancellationException) {
            throw error
        } catch (error: Exception) {
            if (!currentOrExpired(session, cache)) return
            _state.update { it.copy(unreadCountsError = failureText("Could not refresh unread counts", error)) }
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
            val (cache, session) = activeAccount("Still signing in; the action was not saved.") ?: return@launch
            try {
                cache.withEngine { syncEngine ->
                    if (!currentOrExpired(session, cache)) return@withEngine
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
                    search.invalidateForGeneration(restored.snapshot.cursor.generation)
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
                if (currentOrExpired(session, cache)) {
                    _state.update { current -> current.copy(error = failureText("Could not save the action", error)) }
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
            if (!currentOrExpired(session, cache)) return@withContext
            try {
                // withSync serialises the flush against pull cycles; its engine
                // sections take SyncAccess.mutex only around load-mutate-save,
                // so the batch request never blocks a queued action.
                val synced = cache.withSync(apiFactory(session)) { repository ->
                    if (!currentOrExpired(session, cache)) return@withSync null
                    val rejected = repository.flush()
                    val expired = repository.expire(Instant.now()).map { it.operation.opId }
                    rejected to expired
                } ?: return@withContext
                val (rejected, expired) = synced
                cache.withEngine { syncEngine ->
                    if (!currentOrExpired(session, cache)) return@withEngine
                    // The session's river membership is left alone: only a refresh
                    // starts a new one, so passive seen marks do not drop rows.
                    // The update is atomic: a concurrent writer (refresh's loading
                    // flag, an action error) must not be clobbered by this rebuild.
                    // Read the engine before the update: its lambda may run more than once.
                    val snapshot = syncEngine.snapshot()
                    search.invalidateForGeneration(snapshot.cursor.generation)
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
                if (!currentOrExpired(session, cache)) return@withContext
                if (error.isUnauthorized && sessions.current() == session) {
                    // Clear only the session that failed: a login that completed
                    // concurrently must not lose its token or get signed out.
                    sessions.clearToken()
                    if (sessions.current() == null) sessionExpired()
                }
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
                .onFailure { _state.update { current -> current.copy(error = failureText("Could not subscribe", it)) } }
        }
    }

    fun unsubscribe(subscriptionId: Long) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.unsubscribe(subscriptionId, opId("unsubscribe")) }
                .onSuccess { refresh() }
                .onFailure { _state.update { current -> current.copy(error = failureText("Could not unsubscribe", it)) } }
        }
    }

    fun setCadence(subscriptionId: Long, cadence: String) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching {
                api.updateSubscription(subscriptionId, SubscriptionPatch(opId = opId("cadence"), cadence = cadence))
            }.onSuccess { refresh() }
            .onFailure { _state.update { current -> current.copy(error = failureText("Could not save the cadence", it)) } }
        }
    }

    fun revert(activityId: Long) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching { api.revertActivity(activityId, opId("revert")) }
                .onSuccess { refresh() }
                .onFailure { _state.update { current -> current.copy(error = failureText("Could not revert the change", it)) } }
        }
    }

    /**
     * Fetches the signed content URL and caches the readable content. Returns
     * null when the server cannot be reached and nothing is cached.
     *
     * [agentSelected] marks an entry an agent surfaced (a notification, a deep
     * link or an agent-change notice). Its images wait for a click so that
     * automatic image loading cannot tell a publisher which entry an agent
     * chose (docs/research/llm.md, "Agent selection stays unobservable").
     */
    suspend fun openEntry(entryId: Long, agentSelected: Boolean = false): ContentUrlResult? = withContext(Dispatchers.IO) {
        val (cache, session) = activeAccount("Still signing in; the entry was not loaded.") ?: return@withContext null
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
        val url = runCatching { api.contentUrl(entryId, mode = if (agentSelected) "click" else null) }.getOrNull()
        val fallback = content?.html ?: cached?.html
        if (!currentOrExpired(session, cache)) return@withContext null
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
        val (cache, session) = activeAccount("Still signing in; the entry was not loaded.") ?: return@withContext null
        val entry = runCatching { apiFactory(session).notification(notificationId).entryIds?.firstOrNull() }.getOrNull()
        entry.takeIf { currentOrExpired(session, cache) }
    }

    internal suspend fun loadEntry(entryId: Long): ch.lkmc.earnest.core.CachedEntry? = withContext(Dispatchers.IO) {
        val (cache, session) = activeAccount("Still signing in; the entry was not loaded.") ?: return@withContext null
        val entry = cache.repository(apiFactory(session)).loadEntry(entryId)
        cache.withEngine { engine ->
            if (!currentOrExpired(session, cache)) return@withEngine null
            val snapshot = engine.snapshot()
            search.invalidateForGeneration(snapshot.cursor.generation)
            val states = effectiveStates(engine, cache.identity.principalId)
            _state.update { current -> current.copy(snapshot = snapshot, states = states) }
            entry
        }
    }

    private fun isCurrent(session: AuthenticatedSession, cache: SessionCache): Boolean =
        account === cache && activeSession == session && sessions.current() == session

    /**
     * [isCurrent] plus the side effect a dropped session needs: when the store
     * lost the token (a background sync hit 401), the UI signs out instead of
     * dropping the action silently.
     */
    private fun currentOrExpired(session: AuthenticatedSession, cache: SessionCache): Boolean {
        if (isCurrent(session, cache)) return true
        if (sessions.current() == null) sessionExpired()
        return false
    }

    /**
     * The live account for a foreground action, or null when it cannot run. A
     * dropped token signs the UI out; missing handles mean sign-in is still
     * restoring and surface [pendingError] instead of a silent drop. A newer
     * session in the store leaves its own owner to report.
     */
    private fun activeAccount(pendingError: String): Pair<SessionCache, AuthenticatedSession>? {
        val cache = account
        val session = activeSession
        if (cache != null && session != null && isCurrent(session, cache)) return cache to session
        if (sessions.current() == null) sessionExpired()
        else if (cache == null || session == null) _state.update { it.copy(error = pendingError) }
        return null
    }

    /** The stored session is gone: drop the account state and show sign-in. */
    private fun sessionExpired() {
        search.reset()
        account = null
        activeSession = null
        _state.value = UiState()
        if (_loggedIn.value) _sessionNotice.value = "Your session expired. Sign in again."
        _loggedIn.value = false
    }

    private fun api(): EarnestApi? =
        activeAccount("Still signing in; the action was not saved.")?.let { apiFactory(it.second) }

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

/** Names the failed action; a blank message still leaves a readable error. */
private fun failureText(action: String, error: Throwable): String =
    error.message?.takeIf { it.isNotBlank() }?.let { "$action: $it" } ?: "$action."

/** The signed content URL plus the cached HTML fallback for offline reading. */
data class ContentUrlResult(
    val url: String?,
    val expiresAt: String?,
    val cachedHtml: String?,
)

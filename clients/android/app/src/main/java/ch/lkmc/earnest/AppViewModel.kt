package ch.lkmc.earnest

import android.app.Application
import androidx.compose.ui.graphics.ImageBitmap
import androidx.core.content.edit
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import ch.lkmc.earnest.core.Activity
import ch.lkmc.earnest.core.Annotation
import ch.lkmc.earnest.core.ApiException
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.KeptImportReport
import ch.lkmc.earnest.core.MAX_KEPT_IMPORT_BYTES
import ch.lkmc.earnest.core.MAX_OPML_IMPORT_BYTES
import ch.lkmc.earnest.core.OpmlSummary
import ch.lkmc.earnest.core.summarizeKeptImport
import ch.lkmc.earnest.core.summarizeOpml
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
import kotlinx.coroutines.CoroutineDispatcher
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
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

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
    val keptImport: KeptImportUi = KeptImportUi(),
    val opml: OpmlUi = OpmlUi(),
    val unreadCounts: UnreadCounts? = null,
    val unreadCountsSyncedAt: Long? = null,
    val unreadCountsError: String? = null,
)

/**
 * The reader-preferences screen's draft and save state.
 *
 * [epoch] is the ownership epoch of the reset that published this state: a
 * login, logout, session expiry or restore stamps a fresh one, so a request
 * verified for an older scope can never publish into its replacement.
 * [loadRequest] and [saveRequest] identify the request that currently owns
 * each busy flag; final publications are fenced by that identity rather than
 * a shared counter a rejected or stale request could invalidate.
 */
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
    val epoch: Long = 0,
    val loadRequest: Long = 0,
    val saveRequest: Long = 0,
    val countsRequest: Long = 0,
)

/** A save whose outcome is unknown; a retry must resend the identical body. */
data class FrozenReaderSettings(
    val settings: ReaderSettings,
    val opId: String,
)

/**
 * The starred-article import's transient state. The picked file's bytes stay
 * with the view model so the report's retry repeats the same import.
 */
data class KeptImportUi(
    val busy: Boolean = false,
    val error: String? = null,
    val report: KeptImportReport? = null,
)

/** The OPML import and export's transient state. */
data class OpmlUi(
    val busy: Boolean = false,
    val error: String? = null,
    val summary: OpmlSummary? = null,
)

/**
 * Publishes account-scoped reader state. Foreground and background writers
 * share the reload-through-flush critical section in [SessionCache].
 */
class AppViewModel internal constructor(
    application: Application,
    private val sessions: SessionStore,
    private val syncSignals: Flow<String> = SyncAccess.committed,
    private val readerDispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val apiFactory: (AuthenticatedSession) -> EarnestApi,
) : AndroidViewModel(application) {
    constructor(application: Application) : this(application, Secrets(application), apiFactory = {
        EarnestApi(it.identity.server, it.token)
    })

    @Volatile private var account: SessionCache? = null
    @Volatile private var activeSession: AuthenticatedSession? = null
    @Volatile private var keptImportDocument: ByteArray? = null
    // Entries the reader explicitly marked unread. The passive seen heuristic
    // from scrolling never overrides that choice (the web client's rule);
    // opening the article reads it again.
    private val markedUnread: MutableSet<Long> = java.util.concurrent.ConcurrentHashMap.newKeySet()
    // The cadence this client last saved per subscription, so an undo can
    // leave one a later change replaced alone.
    private val savedCadence = java.util.concurrent.ConcurrentHashMap<Long, String>()
    private val mediaCache = MediaCache(getApplication())
    @Volatile private var mediaClient: Pair<AuthenticatedSession, EarnestApi>? = null
    private var sessionStartedAt = Instant.now()
    private var changesSince = Instant.EPOCH.toString()
    private val json = Json { ignoreUnknownKeys = true }
    private val flushScheduler by lazy {
        DebouncedFlush(viewModelScope, FLUSH_DEBOUNCE_MILLIS) { flushOutbox() }
    }

    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state.asStateFlow()

    // Reader-settings requests bind to the account, ownership epoch and
    // snapshot generation captured before dispatch. A logout, login or known
    // restore bumps the epoch, so a queued or in-flight request cannot publish
    // or write under a scope it was not verified for.
    private val readerEpoch = AtomicLong()
    private val readerLoadId = AtomicLong()
    private val readerSaveId = AtomicLong()
    private val readerCountsId = AtomicLong()

    internal val search = SearchModel(viewModelScope) {
        activeAccount("Still signing in; search did not run.")?.let { (cache, session) ->
            val generation = _state.value.snapshot.cursor.generation
            SearchSession(
                api = apiFactory(session),
                isCurrent = { currentOrExpired(session, cache) && _state.value.snapshot.cursor.generation == generation },
                unauthorized = {
                    if (currentOrExpired(session, cache)) expireSessionIfCurrent(session)
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
        readerEpoch.incrementAndGet()
        search.reset()
        flushScheduler.cancel()
        mediaCache.close()
        super.onCleared()
    }

    /** Called only after /v1/me established the authenticated account. */
    fun onLoggedIn() {
        val epoch = readerEpoch.incrementAndGet()
        search.reset()
        account = null
        activeSession = null
        mediaClient = null
        keptImportDocument = null
        markedUnread.clear()
        savedCadence.clear()
        _sessionNotice.value = null
        _state.value = UiState(readerSettingsUi = ReaderSettingsUi(epoch = epoch))
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
                            loaded = restored.readerSettingsUi.loaded,
                            loadedRev = restored.readerSettingsRev,
                            epoch = epoch,
                        ),
                        unreadCounts = cache.cachedUnreadCounts(restored.snapshot.cursor.generation),
                        unreadCountsSyncedAt = cache.cachedUnreadCountsAt(restored.snapshot.cursor.generation),
                    )
                    _loggedIn.value = true
                }
            } catch (error: Exception) {
                if (sessions.current() == session) {
                    _state.value = UiState(
                        error = failureText("Could not restore the account cache", error),
                        readerSettingsUi = ReaderSettingsUi(epoch = epoch),
                    )
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
                // Generation changes are not revision changes: a restore drops
                // the draft and fences requests verified for the old baseline.
                val readerRestore = restored.snapshot.cursor.generation != _state.value.snapshot.cursor.generation
                val restoreEpoch = if (readerRestore) readerEpoch.incrementAndGet() else 0L
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
                    unreadCounts = cache.cachedUnreadCounts(restored.snapshot.cursor.generation),
                    unreadCountsSyncedAt = cache.cachedUnreadCountsAt(restored.snapshot.cursor.generation),
                    unreadCountsError = if (readerRestore) null else previous.unreadCountsError,
                ).withProjectedReader(restored, readerRestore, restoreEpoch) }
            }
        }
    }

    fun logout() {
        search.reset()
        sessions.clearToken()
        activeSession = null
        mediaClient = null
        markedUnread.clear()
        savedCadence.clear()
        account = null
        keptImportDocument = null
        _state.value = UiState(readerSettingsUi = ReaderSettingsUi(epoch = readerEpoch.incrementAndGet()))
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
                    val changes = optionalMetadata { cache.refreshAgentChanges(api, changesSince) }
                    changes.getOrNull()?.let {
                        cache.preferences.edit {
                            putString(SessionCache.KEY_CHANGES_SINCE, sessionStartedAt.toString())
                        }
                    }
                    Triple(rejected, expired, changes)
                } ?: return@launch
                val (rejected, expired, changesResult) = synced
                val briefs = optionalMetadata { api.todaysBriefs() }.getOrNull()
                if (briefs != null) cacheBriefs(cache, briefs)
                val cached = briefs?.map { BriefEntity(it.principalId, it.value, it.rev) } ?: cache.db.briefs().get()
                val names = principalNames(api)
                savePrincipalNames(cache, names)
                cache.withEngine { syncEngine ->
                    if (!currentOrExpired(session, cache)) return@withEngine
                    val restored = restoredState(syncEngine, cache.identity.principalId)
                    search.invalidateForGeneration(restored.snapshot.cursor.generation)
                    val readerRestore = restored.snapshot.cursor.generation != _state.value.snapshot.cursor.generation
                    val restoreEpoch = if (readerRestore) readerEpoch.incrementAndGet() else 0L
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
                        unreadCounts = cache.cachedUnreadCounts(restored.snapshot.cursor.generation),
                        unreadCountsSyncedAt = cache.cachedUnreadCountsAt(restored.snapshot.cursor.generation),
                        unreadCountsError = if (readerRestore) null else current.unreadCountsError,
                        principals = names.ifEmpty { cachedPrincipalNames(cache) },
                        rejected = rejected,
                        error = droppedOperationMessage(rejected, expired),
                        expired = expired,
                    ).withProjectedReader(restored, readerRestore, restoreEpoch) }
                }
                refreshUnreadCounts(cache, session)
                refreshWidgets(getApplication())
            } catch (error: ApiException) {
                if (!currentOrExpired(session, cache)) return@launch
                if (error.isUnauthorized && sessions.current() == session) {
                    // Clear only the session that failed: a login that completed
                    // concurrently must not lose its token or get signed out.
                    expireSessionIfCurrent(session)
                }
                _state.update { current -> current.copy(error = failureText("Could not refresh", error)) }
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
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
     * the account snapshot; it only refreshes the presentation baseline. The
     * verified session, ownership epoch and generation are captured before
     * dispatch, so a result cannot publish under a scope it was not fetched
     * for, and a same-generation result older than the known revision cannot
     * regress it. A draft edited while the load is in flight is preserved.
     */
    fun loadReaderSettings() {
        val intent = _state.value
        val expected = intent.readerSettingsUi.epoch
        val (cache, session) = activeAccount("Still signing in; settings did not load.") ?: return
        loadReaderSettings(expected, session, cache, intent.snapshot.cursor.generation)
    }

    /**
     * Dispatches a load for the scope the caller captured from the same state
     * as [expected]; the claim and every publication are fenced by the state-
     * resident epoch and request identity, never re-read from a replacement.
     */
    private fun loadReaderSettings(
        expected: Long,
        session: AuthenticatedSession,
        cache: SessionCache,
        generation: String,
        reset: Boolean = false,
    ) {
        val request = readerLoadId.incrementAndGet()
        publishReader(expected, session, cache, generation) {
            it.copy(readerSettingsUi = it.readerSettingsUi.copy(
                loading = true,
                error = null,
                success = if (reset) false else it.readerSettingsUi.success,
                loadRequest = request,
                draft = if (reset) null else it.readerSettingsUi.draft,
                conflict = if (reset) null else it.readerSettingsUi.conflict,
                frozen = if (reset) null else it.readerSettingsUi.frozen,
            ))
        }
        viewModelScope.launch(readerDispatcher) {
            if (!loadOwner(session, cache, expected, request)) return@launch
            try {
                val settings = apiFactory(session).readerSettings()
                if (!loadOwner(session, cache, expected, request)) return@launch
                publishReader(expected, session, cache, generation, { it.loadRequest == request }) { current ->
                    val canonical = if (settings.rev > current.readerSettingsRev) settings else current.readerSettings
                    val canonicalRev = maxOf(settings.rev, current.readerSettingsRev)
                    val ui = current.readerSettingsUi
                    current.copy(
                        readerSettings = canonical,
                        readerSettingsRev = canonicalRev,
                        readerSettingsUi = ui.copy(
                            loading = false,
                            // A dirty draft keeps the baseline it was edited on.
                            loaded = if (ui.draft == null) canonical else ui.loaded,
                            loadedRev = if (ui.draft == null) canonicalRev else ui.loadedRev,
                            conflict = null,
                            success = false,
                        ),
                    )
                }
            } catch (error: ApiException) {
                if (!loadOwner(session, cache, expected, request)) return@launch
                if (error.isUnauthorized && sessions.current() == session) {
                    expireSessionIfCurrent(session)
                }
                publishReader(expected, session, cache, generation, { it.loadRequest == request }) {
                    it.copy(readerSettingsUi = it.readerSettingsUi.copy(loading = false, error = failureText("Could not load settings", error)))
                }
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                if (loadOwner(session, cache, expected, request)) {
                    publishReader(expected, session, cache, generation, { it.loadRequest == request }) {
                        it.copy(readerSettingsUi = it.readerSettingsUi.copy(loading = false, error = failureText("Could not load settings", error)))
                    }
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
        val intent = _state.value
        val expected = intent.readerSettingsUi.epoch
        val (cache, session) = activeAccount("Still signing in; settings did not load.") ?: return
        loadReaderSettings(expected, session, cache, intent.snapshot.cursor.generation, reset = true)
    }

    /**
     * Retries the ambiguous save with the identical frozen body and op ID.
     * The draft publication and the dispatch below share the scope captured
     * from the same state as the frozen body, so a reset in between cannot
     * lend the retry a replacement account's state.
     */
    fun retryReaderSettingsSave() {
        val intent = _state.value
        val ui = intent.readerSettingsUi
        val frozen = ui.frozen ?: return
        val expected = ui.epoch
        val generation = intent.snapshot.cursor.generation
        val (cache, session) = activeAccount("Still signing in; settings did not save.") ?: return
        publishReader(expected, session, cache, generation) {
            it.copy(readerSettingsUi = it.readerSettingsUi.copy(draft = frozen.settings, error = null, success = false))
        }
        saveReaderSettings(frozen.settings, ui.loadedRev, frozen.opId, expected, session, cache, generation)
    }

    /**
     * Saves the draft. A draft older than the account's newest known revision
     * warns and reloads instead of overwriting. An ambiguous failure freezes
     * the exact body and op ID so [retryReaderSettingsSave] can resend it.
     *
     * The verified session, ownership epoch and snapshot generation are
     * captured before dispatch and the busy state is claimed atomically, so a
     * queued request cannot bind the captured draft to a later account. Every
     * await and publication is fenced; the acknowledgement is merged under the
     * captured generation and the canonical stored settings are published, so
     * an older acknowledgement can never regress a newer known revision.
     */
    fun saveReaderSettings() {
        val intent = _state.value
        val ui = intent.readerSettingsUi
        val expected = ui.epoch
        val generation = intent.snapshot.cursor.generation
        val (cache, session) = activeAccount("Still signing in; settings did not save.") ?: return
        if (ui.loaded == null || generation.isEmpty()) {
            publishReader(expected, session, cache, generation) {
                it.copy(readerSettingsUi = it.readerSettingsUi.copy(error = "Sync or load your account settings before saving."))
            }
            return
        }
        val draft = ui.draft ?: ui.loaded
        val opId = (ui.frozen?.takeIf { it.settings == draft }?.opId)
            ?: "settings-${java.util.UUID.randomUUID()}"
        saveReaderSettings(draft, ui.loadedRev, opId, expected, session, cache, generation)
    }

    /**
     * Claims the busy flag atomically so a second save while one is active is
     * rejected before dispatch, then runs the request. [baseRev] is
     * the baseline the draft was edited on, captured from the same state as
     * the body. Every await, publication and the merge's flush is fenced by
     * the state-resident epoch and [saveRequest], so a rejected or stale
     * request can neither borrow a replacement scope nor invalidate a newer
     * owner's active request.
     */
    private fun saveReaderSettings(
        draft: ReaderSettings,
        baseRev: Long,
        opId: String,
        expected: Long,
        session: AuthenticatedSession,
        cache: SessionCache,
        generation: String,
    ) {
        val request = readerSaveId.incrementAndGet()
        val claimed = AtomicBoolean()
        publishReader(expected, session, cache, generation, { !it.saving && it.loaded != null && generation.isNotEmpty() }) {
            claimed.set(true)
            it.copy(readerSettingsUi = it.readerSettingsUi.copy(
                saving = true, error = null, success = false, conflict = null,
                frozen = FrozenReaderSettings(draft, opId), saveRequest = request,
            ))
        }
        if (!claimed.get()) return
        viewModelScope.launch(readerDispatcher) {
            if (!saveOwner(session, cache, expected, request)) return@launch
            try {
                val newer = cache.withEngine { engine ->
                    if (!saveOwner(session, cache, expected, request)) return@withEngine null
                    // Newer-known settings include a GET result, which is
                    // published to the state without touching the snapshot.
                    val knownRev = _state.value.readerSettingsRev
                    val stored = cache.readerSettings(engine)
                    when {
                        stored.rev > baseRev -> stored
                        knownRev > baseRev -> _state.value.readerSettings
                        else -> null
                    }
                }
                if (!saveOwner(session, cache, expected, request)) return@launch
                if (newer != null) {
                    publishReader(expected, session, cache, generation, { it.saveRequest == request }) {
                        it.copy(readerSettingsUi = it.readerSettingsUi.copy(
                            conflict = newer, saving = false, error = null, frozen = null,
                        ))
                    }
                    return@launch
                }
                val updated = apiFactory(session).updateReaderSettings(draft, opId)
                if (!saveOwner(session, cache, expected, request)) return@launch
                val canonical = cache.mergeReaderSettings(generation, updated) {
                    readerEpoch.get() == expected && isCurrent(session, cache) && _state.value.readerSettingsUi.let {
                        it.epoch == expected && it.saveRequest == request
                    }
                }
                if (!saveOwner(session, cache, expected, request)) return@launch
                if (canonical == null) {
                    // The snapshot was restored while the save was in flight;
                    // the new generation owns the baseline and the captured
                    // body must not be replayed against it.
                    publishReader(expected, session, cache, generation, { it.saveRequest == request }) {
                        it.copy(readerSettingsUi = it.readerSettingsUi.copy(
                            saving = false, frozen = null,
                            error = "The account settings were restored. Review them and save again.",
                        ))
                    }
                    return@launch
                }
                SyncAccess.committed.tryEmit(session.identity.cacheKey)
                publishReader(expected, session, cache, generation, { it.saveRequest == request }) { current ->
                    val settingsUi = current.readerSettingsUi
                    val editedDraft = settingsUi.draft?.takeIf { it != draft }
                    val needsReload = editedDraft != null && canonical != updated
                    current.copy(
                        readerSettings = canonical,
                        readerSettingsRev = canonical.rev,
                        readerSettingsUi = settingsUi.copy(
                            saving = false,
                            success = canonical == updated && editedDraft == null,
                            error = when {
                                needsReload -> "Newer settings are active. Reload before saving your draft."
                                canonical != updated -> "Newer settings are active. Review them before saving again."
                                else -> null
                            },
                            draft = editedDraft,
                            // A stale acknowledgement cannot rebase unsaved
                            // edits over fields changed by a newer writer.
                            loaded = if (needsReload) settingsUi.loaded else canonical,
                            loadedRev = if (needsReload) settingsUi.loadedRev else canonical.rev,
                            conflict = canonical.takeIf { needsReload },
                            frozen = null,
                        ),
                    )
                }
            } catch (error: ApiException) {
                if (!saveOwner(session, cache, expected, request)) return@launch
                // A 401 invalidates the token: never replay or publish the save.
                if (error.isUnauthorized) {
                    publishReader(expected, session, cache, generation, { it.saveRequest == request }) {
                        it.copy(readerSettingsUi = it.readerSettingsUi.copy(saving = false, frozen = null, error = failureText("Could not save settings", error)))
                    }
                    expireSessionIfCurrent(session)
                    return@launch
                }
                val deterministic = error.status in 400..499
                publishReader(expected, session, cache, generation, { it.saveRequest == request }) {
                    it.copy(readerSettingsUi = it.readerSettingsUi.copy(
                        saving = false,
                        frozen = if (deterministic) null else it.readerSettingsUi.frozen,
                        error = failureText("Could not save settings", error),
                    ))
                }
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                if (saveOwner(session, cache, expected, request)) {
                    publishReader(expected, session, cache, generation, { it.saveRequest == request }) {
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
     * Imports the starred articles of a Google Reader JSON export. [document]
     * is the picked file's raw bytes; the view model keeps them so the report's
     * retry can repeat the import without the file being picked again. The
     * result is published only while the captured account still owns the UI.
     */
    fun importKept(document: ByteArray, subscribeMissing: Boolean) {
        val (cache, session) = activeAccount("Still signing in; the import did not run.") ?: return
        if (document.size > MAX_KEPT_IMPORT_BYTES) {
            keptImportDocument = null
            _state.update { current ->
                current.copy(keptImport = KeptImportUi(error = "The export is larger than 64 MiB."))
            }
            return
        }
        keptImportDocument = document
        _state.update { current ->
            current.copy(keptImport = current.keptImport.copy(busy = true, error = null, report = null))
        }
        viewModelScope.launch(readerDispatcher) {
            try {
                val report = apiFactory(session).importKept(document, subscribeMissing)
                // Keep the bytes only while the report offers a retry; an
                // export can be 64 MiB.
                if (summarizeKeptImport(report).skipped.isEmpty()) keptImportDocument = null
                if (!currentOrExpired(session, cache)) return@launch
                _state.update { current ->
                    current.copy(keptImport = current.keptImport.copy(busy = false, report = report))
                }
            } catch (error: ApiException) {
                // A failed import shows an error, not a retry, so its file goes.
                keptImportDocument = null
                if (!currentOrExpired(session, cache)) return@launch
                _state.update { current ->
                    current.copy(keptImport = current.keptImport.copy(busy = false, error = failureText("Could not import that file", error)))
                }
                if (error.isUnauthorized) expireSessionIfCurrent(session)
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                keptImportDocument = null
                if (currentOrExpired(session, cache)) {
                    _state.update { current ->
                        current.copy(keptImport = current.keptImport.copy(busy = false, error = failureText("Could not import that file", error)))
                    }
                }
            }
        }
    }

    /** Repeats the last import with `missing_feeds=subscribe`. */
    fun importKeptAgain() {
        val document = keptImportDocument ?: return
        importKept(document, subscribeMissing = true)
    }

    /** Surfaces a picked export the client could not read; no import ran. */
    fun keptImportReadFailed(message: String) {
        keptImportDocument = null
        _state.update { current ->
            current.copy(keptImport = KeptImportUi(error = message))
        }
    }

    /**
     * Imports an OPML subscription export. Every outline reports its own
     * outcome in the summary; a refresh picks the results up afterwards.
     */
    fun importOpml(document: ByteArray) {
        val (cache, session) = activeAccount("Still signing in; the import did not run.") ?: return
        if (document.size > MAX_OPML_IMPORT_BYTES) {
            _state.update { current -> current.copy(opml = OpmlUi(error = "The OPML file is larger than 8 MiB.")) }
            return
        }
        _state.update { current -> current.copy(opml = OpmlUi(busy = true)) }
        viewModelScope.launch(readerDispatcher) {
            try {
                val results = apiFactory(session).importOpml(document)
                if (!currentOrExpired(session, cache)) return@launch
                _state.update { current -> current.copy(opml = OpmlUi(summary = summarizeOpml(results))) }
                refresh()
            } catch (error: ApiException) {
                if (currentOrExpired(session, cache)) {
                    _state.update { current -> current.copy(opml = OpmlUi(error = failureText("Could not import that file", error))) }
                }
                if (error.isUnauthorized) expireSessionIfCurrent(session)
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                if (currentOrExpired(session, cache)) {
                    _state.update { current -> current.copy(opml = OpmlUi(error = failureText("Could not import that file", error))) }
                }
            }
        }
    }

    /** Surfaces a picked OPML file the client could not read or write; no import ran. */
    fun opmlFileFailed(message: String) {
        _state.update { current -> current.copy(opml = OpmlUi(error = message)) }
    }

    /** Dismisses the import report and any stale error with it. */
    fun dismissOpmlReport() {
        _state.update { current -> current.copy(opml = OpmlUi()) }
    }

    /** The account's subscriptions as OPML, or null when the export failed. */
    suspend fun exportOpml(): String? = withContext(Dispatchers.IO) {
        val (cache, session) = activeAccount("Still signing in; the export did not run.") ?: return@withContext null
        try {
            val xml = apiFactory(session).exportOpml()
            if (currentOrExpired(session, cache)) {
                // A stale failure must not outlive the export that succeeds.
                _state.update { current -> current.copy(opml = current.opml.copy(error = null)) }
            }
            xml
        } catch (error: ApiException) {
            if (currentOrExpired(session, cache)) {
                _state.update { current -> current.copy(opml = current.opml.copy(error = failureText("Could not export subscriptions", error))) }
            }
            if (error.isUnauthorized) expireSessionIfCurrent(session)
            null
        } catch (error: kotlinx.coroutines.CancellationException) {
            throw error
        } catch (error: Exception) {
            if (currentOrExpired(session, cache)) {
                _state.update { current -> current.copy(opml = current.opml.copy(error = failureText("Could not export subscriptions", error))) }
            }
            null
        }
    }

    /**
     * Fetches exact unread counts only in unread-count mode. A failure keeps
     * the last synced counts (never zero) and reports that they are stale.
     */
    private suspend fun refreshUnreadCounts(cache: SessionCache, session: AuthenticatedSession) {
        val intent = _state.value
        if (intent.readerSettings.readingMode != ReadingMode.UNREAD_COUNTS) return
        val expected = intent.readerSettingsUi.epoch
        val generation = intent.snapshot.cursor.generation
        if (generation.isEmpty()) return
        val request = readerCountsId.incrementAndGet()
        publishReader(expected, session, cache, generation) {
            it.copy(readerSettingsUi = it.readerSettingsUi.copy(countsRequest = request))
        }
        // Pure: also called after acquiring the durable cache lock.
        fun ownsIntent(): Boolean = readerEpoch.get() == expected && isCurrent(session, cache) && _state.value.let {
            it.readerSettingsUi.epoch == expected && it.readerSettingsUi.countsRequest == request &&
                it.snapshot.cursor.generation == generation && it.readerSettings.readingMode == ReadingMode.UNREAD_COUNTS
        }
        if (!ownsIntent()) return
        val pending = cache.beginUnreadCountsRequest(generation, ::ownsIntent) ?: return
        fun owns(): Boolean = ownsIntent() && cache.ownsUnreadCounts(pending)
        try {
            val counts = apiFactory(session).unreadCounts()
            if (!cache.mergeUnreadCounts(pending, counts, ::owns) || !owns()) return
            val syncedAt = cache.cachedUnreadCountsAt(generation)
            publishReader(expected, session, cache, generation, { it.countsRequest == request && cache.ownsUnreadCounts(pending) }) {
                if (it.readerSettings.readingMode != ReadingMode.UNREAD_COUNTS) it else
                    it.copy(unreadCounts = counts, unreadCountsSyncedAt = syncedAt, unreadCountsError = null)
            }
        } catch (error: kotlinx.coroutines.CancellationException) {
            throw error
        } catch (error: Exception) {
            if (!owns()) return
            if (error is ApiException && error.isUnauthorized) {
                expireSessionIfCurrent(session)
                return
            }
            publishReader(expected, session, cache, generation, { it.countsRequest == request && cache.ownsUnreadCounts(pending) }) {
                it.copy(unreadCountsError = failureText("Could not refresh unread counts", error))
            }
        }
    }

    /** Marks the entry read, as opening an article does, and releases the mark-unread guard. */
    fun markSeen(entryId: Long) {
        markedUnread -= entryId
        queue(entryId, "seen", SyncEngine.KIND_PASSIVE) { state ->
            listOf(RegisterIntent(SyncEngine.REGISTER_SEEN, JsonPrimitive(SyncEngine.VALUE_SEEN), state.seen.value))
        }
    }

    /**
     * Records reading progress from scrolling past a row. Returns false, and
     * writes nothing, for an entry the reader explicitly marked unread: the
     * passive heuristic never overrides that choice until the article is
     * opened.
     */
    fun markScrolledPast(entryId: Long): Boolean {
        if (entryId in markedUnread) return false
        markSeen(entryId)
        return true
    }

    internal fun later(entryId: Long, onSaved: (ReversibleAction) -> Unit = {}) {
        act(entryId, EntryActionName.LATER, SyncEngine.REGISTER_SHELF, JsonPrimitive(SyncEngine.VALUE_LATER), onSaved)
    }

    internal fun done(entryId: Long, onSaved: (ReversibleAction) -> Unit = {}) {
        act(entryId, EntryActionName.DONE, SyncEngine.REGISTER_SHELF, JsonPrimitive(SyncEngine.VALUE_DONE), onSaved)
    }

    internal fun keep(entryId: Long, onSaved: (ReversibleAction) -> Unit = {}) {
        act(entryId, EntryActionName.KEEP, SyncEngine.REGISTER_KEPT, JsonPrimitive(true), onSaved)
    }

    internal fun unkeep(entryId: Long, onSaved: (ReversibleAction) -> Unit = {}) {
        act(entryId, EntryActionName.UNKEEP, SyncEngine.REGISTER_KEPT, JsonPrimitive(false), onSaved)
    }

    /**
     * Marks a read article unread again. A Done shelf is cleared with it (the
     * server rule the engine enforces), and [undo] restores both registers.
     * Scrolling past the row never marks it read again (see [markScrolledPast]).
     */
    internal fun markUnread(entryId: Long, onSaved: (ReversibleAction) -> Unit = {}) {
        markedUnread += entryId
        queue(
            entryId, EntryActionName.UNREAD.id, SyncEngine.KIND_EXPLICIT, EntryActionName.UNREAD, onSaved,
            onNotSaved = { markedUnread -= entryId }, build = ::unreadIntents,
        )
    }

    /** Writes back the registers [action] wrote and nothing has changed since. */
    internal fun undo(action: ReversibleAction, onSaved: (ReversibleAction) -> Unit = {}) {
        queue(
            action.entryId, "undo", SyncEngine.KIND_EXPLICIT,
            onSaved = { saved ->
                // The entry is read again (or the restore was moot because
                // the register moved on), so the mark-unread guard goes with
                // it. A failed undo keeps the guard.
                if (SyncEngine.REGISTER_SEEN in action.written) markedUnread -= action.entryId
                onSaved(saved)
            },
        ) { state -> undoIntents(action, state) }
    }

    private fun act(entryId: Long, name: EntryActionName, register: String, value: JsonElement, onSaved: (ReversibleAction) -> Unit) {
        queue(entryId, name.id, SyncEngine.KIND_EXPLICIT, name, onSaved) { state ->
            listOf(RegisterIntent(register, value, valueOf(state, register)))
        }
    }

    /**
     * Queues the register writes [build] derives from the entry's current
     * state, as one action. [undoable] names the action for its undo snackbar.
     * [onSaved] reports success only after the writes are durably queued,
     * carrying the values undo must restore; [onNotSaved] reports an action
     * that never reached the durable queue.
     */
    private fun queue(
        entryId: Long,
        opId: String,
        kind: String,
        undoable: EntryActionName? = null,
        onSaved: (ReversibleAction) -> Unit = {},
        onNotSaved: () -> Unit = {},
        build: (ch.lkmc.earnest.core.State) -> List<RegisterIntent>,
    ) {
        viewModelScope.launch(Dispatchers.IO) {
            var saved = false
            val active = activeAccount("Still signing in; the action was not saved.")
            if (active == null) {
                onNotSaved()
                return@launch
            }
            val (cache, session) = active
            try {
                cache.withEngine { syncEngine ->
                    if (!currentOrExpired(session, cache)) return@withEngine
                    val principal = cache.identity.principalId
                    if (principal == 0L || syncEngine.snapshot().cursor.generation.isEmpty()) {
                        _state.update { current -> current.copy(error = "Still signing in; the action was not saved.") }
                        return@withEngine
                    }
                    val current = syncEngine.state(principal, entryId)
                    val intents = build(current)
                    intents.forEachIndexed { index, intent ->
                        val write = StateWrite(
                            opId = "$opId-$entryId-${java.util.UUID.randomUUID()}-$index",
                            principal = principal,
                            entryId = entryId,
                            register = intent.register,
                            value = intent.value,
                            base = Revision(syncEngine.snapshot().cursor.generation, revOf(current, intent.register)),
                            occurredAt = Instant.now().toString(),
                            kind = kind,
                        )
                        syncEngine.queue(write)
                    }
                    syncEngine.flush()
                    saved = true
                    onSaved(ReversibleAction(
                        entryId = entryId,
                        message = undoable?.message.orEmpty(),
                        prior = intents.associate { it.register to it.prior },
                        written = intents.associate { it.register to it.value },
                    ))
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
                    flushScheduler.request()
                }
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                if (currentOrExpired(session, cache)) {
                    // A failure after the flush is a refresh problem, not a
                    // save problem: the writes are already durable.
                    val what = if (saved) "The action was saved, but the view could not refresh" else "Could not save the action"
                    _state.update { current -> current.copy(error = failureText(what, error)) }
                }
            } finally {
                if (!saved) onNotSaved()
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
                    expireSessionIfCurrent(session)
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

    fun setCadence(subscriptionId: Long, cadence: String, onSaved: () -> Unit = {}) {
        viewModelScope.launch(Dispatchers.IO) {
            val api = api() ?: return@launch
            runCatching {
                api.updateSubscription(subscriptionId, SubscriptionPatch(opId = opId("cadence"), cadence = cadence))
            }.onSuccess {
                savedCadence[subscriptionId] = cadence
                onSaved()
                refresh()
            }
            .onFailure { _state.update { current -> current.copy(error = failureText("Could not save the cadence", it)) } }
        }
    }

    /**
     * Restores the cadence [written] replaced, leaving one alone that a later
     * change of this client's replaced it with. The snapshot cannot be the
     * reference: it refreshes only after a change round-trips, so undoing on a
     * slow link would skip the restore entirely. Returns whether it started.
     */
    fun undoCadence(subscriptionId: Long, restore: String, written: String): Boolean {
        if (savedCadence[subscriptionId] != written) return false
        setCadence(subscriptionId, restore)
        return true
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

    /**
     * The verified reader scope still owns this account. The epoch is checked
     * both on the counter and inside the published state: a replacement state
     * stamped with a newer epoch can never satisfy an older intent, so the
     * check cannot ABA back into a dead scope. [currentOrExpired] adds the
     * sign-out side effect a dropped session needs.
     */
    private fun readerCurrent(session: AuthenticatedSession, cache: SessionCache, expected: Long): Boolean =
        readerEpoch.get() == expected && _state.value.readerSettingsUi.epoch == expected &&
            currentOrExpired(session, cache)

    /** [readerCurrent] plus ownership of the in-flight load request. */
    private fun loadOwner(session: AuthenticatedSession, cache: SessionCache, expected: Long, request: Long): Boolean =
        readerCurrent(session, cache, expected) && _state.value.readerSettingsUi.loadRequest == request

    /** [readerCurrent] plus ownership of the in-flight save request. */
    private fun saveOwner(session: AuthenticatedSession, cache: SessionCache, expected: Long, request: Long): Boolean =
        readerCurrent(session, cache, expected) && _state.value.readerSettingsUi.saveRequest == request

    /**
     * Applies [transform] only while the captured epoch, account and snapshot
     * generation still own the state and [owns] accepts the current request
     * identity. The checks run inside the CAS update, so a reset or restore
     * can never lend the publication a new scope.
     */
    private fun publishReader(
        expected: Long,
        session: AuthenticatedSession,
        cache: SessionCache,
        generation: String,
        owns: (ReaderSettingsUi) -> Boolean = { true },
        transform: (UiState) -> UiState,
    ) {
        _state.update { current ->
            val ui = current.readerSettingsUi
            if (readerEpoch.get() == expected && ui.epoch == expected && owns(ui) &&
                isCurrent(session, cache) && current.snapshot.cursor.generation == generation
            ) transform(current) else current
        }
    }

    /**
     * Projects cached reader settings into the UI after a cache restore. A new
     * generation drops the draft and the pending request: revisions across
     * generations are incomparable, so the restored baseline always wins. The
     * same generation publishes the newer known revision and keeps a dirty
     * draft, whose baseline only advances once the draft is gone.
     */
    private fun UiState.withProjectedReader(restored: UiState, restore: Boolean, restoreEpoch: Long): UiState =
        if (restore) {
            copy(
                readerSettings = restored.readerSettings,
                readerSettingsRev = restored.readerSettingsRev,
                readerSettingsUi = ReaderSettingsUi(
                    loaded = restored.readerSettingsUi.loaded,
                    loadedRev = restored.readerSettingsRev,
                    epoch = restoreEpoch,
                ),
            )
        } else {
            val canonical = if (restored.readerSettingsRev > readerSettingsRev) restored.readerSettings else readerSettings
            val canonicalRev = maxOf(restored.readerSettingsRev, readerSettingsRev)
            copy(
                readerSettings = canonical,
                readerSettingsRev = canonicalRev,
                readerSettingsUi = if (readerSettingsUi.draft == null) {
                    readerSettingsUi.copy(
                        loaded = canonical.takeIf { restored.readerSettingsUi.loaded != null || readerSettingsUi.loaded != null },
                        loadedRev = canonicalRev,
                    )
                } else {
                    readerSettingsUi
                },
            )
        }

    /** Credential comparison and removal are one operation across all writers. */
    private fun expireSessionIfCurrent(session: AuthenticatedSession) {
        if (sessions.clearToken(session) && sessions.current() == null) sessionExpired()
    }

    /** The stored session is gone: drop the account state and show sign-in. */
    private fun sessionExpired() {
        search.reset()
        account = null
        activeSession = null
        mediaClient = null
        markedUnread.clear()
        savedCadence.clear()
        keptImportDocument = null
        _state.value = UiState(readerSettingsUi = ReaderSettingsUi(epoch = readerEpoch.incrementAndGet()))
        if (_loggedIn.value) _sessionNotice.value = "Your session expired. Sign in again."
        _loggedIn.value = false
    }

    private fun api(): EarnestApi? =
        activeAccount("Still signing in; the action was not saved.")?.let { apiFactory(it.second) }

    /**
     * A stored feed icon or entry thumbnail for the live account, or null
     * when none exists or it cannot be fetched. The images are decorative, so
     * failures report nothing to the UI.
     */
    internal suspend fun mediaBitmap(path: String): ImageBitmap? {
        val cache = account ?: return null
        val session = activeSession ?: return null
        if (!isCurrent(session, cache)) return null
        val api = mediaApi(session)
        return mediaCache.load(session.identity.cacheKey, path) { api.media(it) }
    }

    /**
     * One API client for a session's images: each EarnestApi builds its own
     * HTTP client, and a list shows dozens of images, which should share
     * pooled connections rather than open one each.
     */
    private fun mediaApi(session: AuthenticatedSession): EarnestApi {
        mediaClient?.let { (owner, api) -> if (owner === session) return api }
        return apiFactory(session).also { mediaClient = session to it }
    }

    /** Cache fallback must not turn cancellation or expired credentials into success. */
    private inline fun <T> optionalMetadata(fetch: () -> T): Result<T> = try {
        Result.success(fetch())
    } catch (error: Exception) {
        if (error is kotlinx.coroutines.CancellationException || (error is ApiException && error.isUnauthorized)) throw error
        Result.failure(error)
    }

    /** Maps principal IDs to the names agents chose for themselves. */
    private suspend fun principalNames(api: EarnestApi): Map<Long, String> =
        optionalMetadata { api.tokens() }.getOrDefault(emptyList())
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

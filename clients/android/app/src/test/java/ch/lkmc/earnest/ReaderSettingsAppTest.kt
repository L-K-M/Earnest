package ch.lkmc.earnest

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.ReaderFont
import ch.lkmc.earnest.core.ReaderOverride
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.ReaderTheme
import ch.lkmc.earnest.core.ReadingMode
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.River
import ch.lkmc.earnest.core.SyncObject
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.cancel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull
import kotlin.coroutines.CoroutineContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.SQLiteMode
import java.util.concurrent.CopyOnWriteArrayList

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@SQLiteMode(SQLiteMode.Mode.NATIVE)
class ReaderSettingsAppTest {
    private val app get() = ApplicationProvider.getApplicationContext<Application>()
    private val identity = AccountIdentity("https://nas.test", 1, 1)
    private data class Call(val method: String, val path: String, val body: String, val authorization: String? = null)
    private val calls = CopyOnWriteArrayList<Call>()
    private val putSeen = CompletableDeferred<Call>()

    private class Sessions(initial: AuthenticatedSession?) : SessionStore {
        private val stored = java.util.concurrent.atomic.AtomicReference(initial)
        var value: AuthenticatedSession?
            get() = stored.get()
            set(value) { stored.set(value) }
        var beforeClear: (() -> Unit)? = null
        val clearAttempted = CompletableDeferred<Unit>()
        override fun current() = stored.get()
        override fun clearToken() {
            beforeClear?.invoke()
            stored.set(null)
            clearAttempted.complete(Unit)
        }
        override fun clearToken(expected: AuthenticatedSession): Boolean {
            beforeClear?.invoke()
            try {
                while (true) {
                    val current = stored.get()
                    if (current != expected) return false
                    if (stored.compareAndSet(current, null)) return true
                }
            } finally { clearAttempted.complete(Unit) }
        }
    }

    private var getBody = settingsJson(rev = 1)
    private var putRev = 2L
    private var countsBody = """{"by_subscription":{"3":4},"later":1,"kept":2,"total":7}"""
    private var countsStatus = 200

    @Before fun setup() { Dispatchers.setMain(UnconfinedTestDispatcher()) }
    @After fun teardown() { Dispatchers.resetMain() }

    private fun settingsJson(
        rev: Long = 1,
        mode: ReadingMode = ReadingMode.RIVER,
        measure: Long = 66,
    ): String = """{"reading_mode":"${mode.name.lowercase()}","today_decay_days":2,"single_key_shortcuts":true,""" +
        """"theme":"system","measure":$measure,"font":"serif","feed_overrides":{},"rev":$rev}"""

    private fun defaultRespond(request: Request): Pair<Int, String> {
        val path = request.url.encodedPath
        return when (path) {
            "/v1/settings" -> if (request.method == "PUT") {
                val raw = request.body?.let { body ->
                    okio.Buffer().also { body.writeTo(it) }.readUtf8()
                }.orEmpty()
                val echoed = JsonObject(Json.parseToJsonElement(raw).jsonObject.toMutableMap().apply {
                    put("rev", JsonPrimitive(putRev))
                })
                200 to echoed.toString()
            } else 200 to getBody
            "/v1/changes" -> {
                val rev = request.url.queryParameter("rev")?.toLongOrNull() ?: 0
                200 to """{"changes":[],"next":{"generation":"g1","rev":$rev},"next_cursor":"g1:$rev"}"""
            }
            "/v1/entries:counts" -> countsStatus to countsBody
            "/v1/mutes" -> 200 to """[{"id":7,"kind":"author","value":"spam","rev":3}]"""
            "/v1/tokens", "/v1/briefs/today", "/v1/activity/agent-changes" -> 200 to "[]"
            else -> 200 to "[]"
        }
    }

    private var respond: (Request) -> Pair<Int, String> = ::defaultRespond

    private fun api(token: String = "token") = EarnestApi(identity.server, token, OkHttpClient.Builder().addInterceptor { chain ->
        val request = chain.request()
        val call = Call(request.method, request.url.encodedPath, request.body?.let { body ->
            okio.Buffer().also { body.writeTo(it) }.readUtf8()
        }.orEmpty(), request.header("Authorization"))
        calls += call
        if (call.method == "PUT") putSeen.complete(call)
        val (code, body) = respond(request)
        Response.Builder().request(request).protocol(Protocol.HTTP_1_1).code(code).message("test")
            .body(body.toResponseBody()).build()
    }.build())

    private suspend fun model(
        sessions: Sessions = Sessions(AuthenticatedSession(identity, "token")),
        signals: MutableSharedFlow<String> = MutableSharedFlow(extraBufferCapacity = 1),
        dispatcher: CoroutineDispatcher = Dispatchers.IO,
        setup: suspend (SessionCache) -> Unit = {},
    ): AppViewModel {
        val cache = SessionCache(app, identity)
        cache.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        setup(cache)
        val model = AppViewModel(app, sessions, signals, readerDispatcher = dispatcher) { api(it.token) }
        withTimeout(15000) { model.state.first { !it.loading && calls.any { it.path == "/v1/tokens" } } }
        return model
    }

    private fun puts(): List<Call> = calls.filter { it.method == "PUT" && it.path == "/v1/settings" }

    private class PausedRequestDispatcher : CoroutineDispatcher() {
        @Volatile var pauseNext = false
        val held = CompletableDeferred<Runnable>()
        override fun dispatch(context: CoroutineContext, block: Runnable) {
            if (pauseNext) {
                pauseNext = false
                check(held.complete(block))
            } else Dispatchers.IO.dispatch(context, block)
        }
    }

    @Test fun queuedSaveCannotBindAnOldDraftToTheNextAccount() = runBlocking(Dispatchers.IO) {
        val dispatcher = PausedRequestDispatcher()
        val sessions = Sessions(AuthenticatedSession(identity, "token-a"))
        val model = model(sessions = sessions, dispatcher = dispatcher)
        model.loadReaderSettings()
        withTimeout(15_000) { model.state.first { it.readerSettingsUi.loadedRev == 1L && !it.readerSettingsUi.loading } }
        model.updateReaderSettingsDraft { it.copy(feedOverrides = mapOf("42" to ReaderOverride(theme = ReaderTheme.EINK))) }
        dispatcher.pauseNext = true
        model.saveReaderSettings()
        val queued = withTimeout(5_000) { dispatcher.held.await() }
        try {
            model.logout()
            val next = AccountIdentity(identity.server, 2, 2)
            sessions.value = AuthenticatedSession(next, "token-b")
            model.onLoggedIn()
            withTimeout(15_000) { model.state.first { it.accountKey == next.cacheKey } }
        } finally {
            queued.run()
        }
        assertNull("old draft was sent under the next account", withTimeoutOrNull(500) { putSeen.await() })
    }

    @Test fun busySaveCannotBeDispatchedAgainBeforeItsQueuedRequestStarts() = runBlocking(Dispatchers.IO) {
        val dispatcher = PausedRequestDispatcher()
        val model = model(dispatcher = dispatcher)
        model.loadReaderSettings()
        withTimeout(15_000) { model.state.first { it.readerSettingsUi.loadedRev == 1L && !it.readerSettingsUi.loading } }
        model.updateReaderSettingsDraft { it.copy(measure = 68) }
        dispatcher.pauseNext = true
        model.saveReaderSettings()
        val queued = withTimeout(5_000) { dispatcher.held.await() }
        try {
            assertTrue(model.state.value.readerSettingsUi.saving)
            model.saveReaderSettings()
            assertNull("a duplicate save bypassed the busy claim", withTimeoutOrNull(500) { putSeen.await() })
        } finally { queued.run() }
        withTimeout(15_000) { model.state.first { it.readerSettingsUi.success } }
        assertEquals(1, puts().size)
    }

    @Test fun retryCannotBorrowTheNextAccountsStateAfterReset() = runBlocking(Dispatchers.IO) {
        var failures = 1
        respond = { request ->
            if (request.method == "PUT" && failures-- > 0) 503 to "{}" else defaultRespond(request)
        }
        val sessions = Sessions(AuthenticatedSession(identity, "token-a"))
        val model = model(sessions = sessions)
        model.loadReaderSettings()
        withTimeout(15_000) { model.state.first { it.readerSettingsUi.loadedRev == 1L && !it.readerSettingsUi.loading } }
        model.updateReaderSettingsDraft { it.copy(feedOverrides = mapOf("42" to ReaderOverride(theme = ReaderTheme.EINK))) }
        model.saveReaderSettings()
        withTimeout(15_000) { model.state.first { it.readerSettingsUi.error != null && !it.readerSettingsUi.saving } }
        val attempts = puts().size
        val next = AccountIdentity(identity.server, 2, 2)
        val nextCache = SessionCache(app, next)
        nextCache.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        nextCache.mergeReaderSettings("g1", ReaderSettings(rev = 1)) { true }
        val observer = CoroutineScope(UnconfinedTestDispatcher())
        val switched = CompletableDeferred<Unit>()
        var switching = false
        observer.launch {
            model.state.collect { state ->
                if (!switching && state.readerSettingsUi.error == null && state.readerSettingsUi.frozen != null) {
                    switching = true
                    model.logout()
                    sessions.value = AuthenticatedSession(next, "token-b")
                    model.onLoggedIn()
                    // Complete the replacement before the synchronous retry
                    // caller can continue after publishing its draft.
                    runBlocking(Dispatchers.IO) {
                        // StateFlow batches notifications until this callback
                        // returns; read its value rather than awaiting a second
                        // collector notification from inside that batch.
                        withTimeout(15_000) {
                            while (model.state.value.accountKey != next.cacheKey) delay(1)
                        }
                    }
                    model.updateReaderSettingsDraft { it.copy(measure = 70) }
                    switched.complete(Unit)
                }
            }
        }
        try {
            model.retryReaderSettingsSave()
            withTimeout(15_000) { switched.await() }
            val unexpected = withTimeoutOrNull(500) {
                while (puts().size == attempts) delay(1)
                puts().last()
            }
            assertNull("the old retry mutated the next account", unexpected)
        } finally { observer.cancel() }
    }

    @Test fun loadCompletionPreservesEditsMadeWhileItWasPending() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadReaderSettings()
        withTimeout(15_000) { model.state.first { it.readerSettingsUi.loadedRev == 1L && !it.readerSettingsUi.loading } }
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        getBody = settingsJson(rev = 9, measure = 70)
        respond = { request ->
            if (request.method == "GET" && request.url.encodedPath == "/v1/settings") {
                entered.complete(Unit)
                runBlocking { release.await() }
            }
            defaultRespond(request)
        }
        model.loadReaderSettings()
        try {
            withTimeout(5_000) { entered.await() }
            model.updateReaderSettingsDraft { it.copy(measure = 69) }
        } finally { release.complete(Unit) }
        withTimeout(15_000) { model.state.first { !it.readerSettingsUi.loading } }
        assertEquals(69L, model.state.value.readerSettingsUi.draft?.measure)
        assertTrue("loading must not PUT preferences", puts().isEmpty())
    }

    @Test fun olderLoadCannotRegressNewerSyncedPreferences() = runBlocking(Dispatchers.IO) {
        val signals = MutableSharedFlow<String>(extraBufferCapacity = 1)
        val model = model(signals = signals)
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        getBody = settingsJson(rev = 2, measure = 64)
        respond = { request ->
            if (request.method == "GET" && request.url.encodedPath == "/v1/settings") {
                entered.complete(Unit)
                runBlocking { release.await() }
            }
            defaultRespond(request)
        }
        model.loadReaderSettings()
        try {
            withTimeout(5_000) { entered.await() }
            SessionCache(app, identity).mergeReaderSettings("g1", ReaderSettings(measure = 72, rev = 7)) { true }
            signals.emit(identity.cacheKey)
            withTimeout(15_000) { model.state.first { it.readerSettingsRev == 7L } }
        } finally { release.complete(Unit) }
        withTimeout(15_000) { model.state.first { !it.readerSettingsUi.loading } }
        assertEquals(7L, model.state.value.readerSettingsRev)
        assertEquals(72L, model.state.value.readerSettings.measure)
        assertEquals(7L, model.state.value.readerSettingsUi.loadedRev)
    }

    @Test fun olderAcknowledgementPublishesCanonicalStoredPreferences() = runBlocking(Dispatchers.IO) {
        val signals = MutableSharedFlow<String>(extraBufferCapacity = 1)
        val model = model(signals = signals)
        model.loadReaderSettings()
        withTimeout(15_000) { model.state.first { it.readerSettingsUi.loadedRev == 1L && !it.readerSettingsUi.loading } }
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        respond = { request ->
            if (request.method == "PUT") {
                entered.complete(Unit)
                runBlocking { release.await() }
            }
            defaultRespond(request)
        }
        model.updateReaderSettingsDraft { it.copy(measure = 68) }
        model.saveReaderSettings()
        try {
            withTimeout(5_000) { entered.await() }
            SessionCache(app, identity).mergeReaderSettings("g1", ReaderSettings(measure = 70, rev = 7)) { true }
            signals.emit(identity.cacheKey)
            withTimeout(15_000) { model.state.first { it.readerSettingsRev == 7L } }
        } finally { release.complete(Unit) }
        withTimeout(15_000) { model.state.first { !it.readerSettingsUi.saving } }
        assertEquals(7L, model.state.value.readerSettingsRev)
        assertEquals(70L, model.state.value.readerSettings.measure)
        assertEquals(7L, model.state.value.readerSettingsUi.loadedRev)
        assertFalse("an older acknowledgement is not the active preferences", model.state.value.readerSettingsUi.success)
    }

    @Test fun olderAcknowledgementCannotRebaseInFlightEditsOverNewerAccountFields() = runBlocking(Dispatchers.IO) {
        val signals = MutableSharedFlow<String>(extraBufferCapacity = 1)
        val model = model(signals = signals)
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        respond = { request ->
            if (request.method == "PUT") {
                entered.complete(Unit)
                runBlocking { release.await() }
            }
            defaultRespond(request)
        }
        model.updateReaderSettingsDraft { it.copy(measure = 68) }
        model.saveReaderSettings()
        try {
            withTimeout(5_000) { entered.await() }
            model.updateReaderSettingsDraft { it.copy(measure = 69) }
            SessionCache(app, identity).mergeReaderSettings("g1", ReaderSettings(theme = ReaderTheme.DARK, rev = 7)) { true }
            signals.emit(identity.cacheKey)
            withTimeout(15_000) { model.state.first { it.readerSettingsRev == 7L } }
        } finally { release.complete(Unit) }
        withTimeout(15_000) { model.state.first { !it.readerSettingsUi.saving } }
        val before = puts().size
        model.saveReaderSettings()
        val unexpected = withTimeoutOrNull(500) {
            while (puts().size == before) delay(1)
            puts().last()
        }
        assertNull("in-flight edits must not silently overwrite newer account fields", unexpected)
        assertEquals(69L, model.state.value.readerSettingsUi.draft?.measure)
        assertEquals(1L, model.state.value.readerSettingsUi.loadedRev)
        assertEquals(7L, model.state.value.readerSettingsUi.conflict?.rev)
        assertFalse(model.state.value.readerSettingsUi.success)
    }

    @Test fun restoreDropsFrozenDraftAndFencesItsOldAcknowledgement() = runBlocking(Dispatchers.IO) {
        val signals = MutableSharedFlow<String>(extraBufferCapacity = 1)
        val model = model(signals = signals)
        model.loadReaderSettings()
        withTimeout(15_000) { model.state.first { it.readerSettingsUi.loadedRev == 1L && !it.readerSettingsUi.loading } }
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        respond = { request ->
            if (request.method == "PUT") {
                entered.complete(Unit)
                runBlocking { release.await() }
            }
            defaultRespond(request)
        }
        model.updateReaderSettingsDraft { it.copy(measure = 68) }
        model.saveReaderSettings()
        try {
            withTimeout(5_000) { entered.await() }
            val cache = SessionCache(app, identity)
            cache.withEngine { it.beginBootstrap(Revision("g2", 0)) }
            cache.mergeReaderSettings("g2", ReaderSettings(measure = 70, rev = 1)) { true }
            signals.emit(identity.cacheKey)
            withTimeout(15_000) { model.state.first { it.snapshot.cursor.generation == "g2" } }
        } finally { release.complete(Unit) }
        assertNull("pre-restore acknowledgement repopulated state", withTimeoutOrNull(500) {
            model.state.first { it.readerSettingsUi.success || it.readerSettingsRev == 2L }
        })
        assertNull(model.state.value.readerSettingsUi.frozen)
        assertNull(model.state.value.readerSettingsUi.draft)
        assertEquals(70L, model.state.value.readerSettings.measure)
        val stored = SessionCache(app, identity).withEngine { SessionCache(app, identity).readerSettings(it) }
        assertEquals(1L, stored.rev)
        assertEquals(70L, stored.measure)
    }

    @Test fun saveMergesAcknowledgedSettingsIntoSnapshotPreservingMutesAndCursor() = runBlocking(Dispatchers.IO) {
        val model = model { cache ->
            cache.withEngine { it.beginBootstrap(Revision("g1", 5)) }
        }
        model.loadReaderSettings()
        withTimeout(15000) { model.state.first { !it.readerSettingsUi.loading } }
        model.updateReaderSettingsDraft {
            it.copy(measure = 68, feedOverrides = mapOf("42" to ReaderOverride(theme = ReaderTheme.EINK)))
        }
        model.saveReaderSettings()
        withTimeout(15000) { model.state.first { it.readerSettingsUi.success } }

        val put = puts().single()
        val body = Json.parseToJsonElement(put.body).jsonObject
        assertEquals("68", body.getValue("measure").jsonPrimitive.content)
        assertEquals(ReaderTheme.EINK.name.lowercase(), body.getValue("feed_overrides").jsonObject.getValue("42").jsonObject.getValue("theme").jsonPrimitive.content)
        assertTrue(body.containsKey("op_id"))
        assertFalse(body.containsKey("rev"))

        val restored = SessionCache(app, identity).withEngine { engine ->
            engine.snapshot().let { snapshot ->
                Triple(
                    engine.snapshot().objects.firstOrNull { it.kind == River.SETTINGS }?.rev,
                    engine.snapshot().objects.firstOrNull { it.kind == River.MUTE }?.id,
                    snapshot.cursor,
                )
            }
        }
        assertEquals(2L, restored.first)
        assertEquals(7L, restored.second)
        assertEquals(Revision("g1", 5), restored.third)
        assertEquals(68L, model.state.value.readerSettings.measure)
        assertEquals(ReaderTheme.EINK, model.state.value.readerSettings.feedOverrides.getValue("42").theme)
    }

    @Test fun ownAcknowledgementKeepsNewEditsDirtyAndSaveable() = runBlocking(Dispatchers.IO) {
        val model = model()
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        respond = { request ->
            if (request.method == "PUT") {
                entered.complete(Unit)
                runBlocking { release.await() }
            }
            defaultRespond(request)
        }
        model.updateReaderSettingsDraft { it.copy(measure = 68) }
        model.saveReaderSettings()
        try {
            withTimeout(5_000) { entered.await() }
            model.updateReaderSettingsDraft { it.copy(measure = 69) }
        } finally { release.complete(Unit) }
        withTimeout(15_000) { model.state.first { !it.readerSettingsUi.saving } }
        assertEquals(69L, model.state.value.readerSettingsUi.draft?.measure)
        assertEquals(2L, model.state.value.readerSettingsUi.loadedRev)
        assertFalse("the newly edited draft has not been saved", model.state.value.readerSettingsUi.success)
        putRev = 3
        model.saveReaderSettings()
        withTimeout(15_000) { model.state.first { it.readerSettingsUi.success && !it.readerSettingsUi.saving } }
        assertEquals("69", Json.parseToJsonElement(puts().last().body).jsonObject.getValue("measure").jsonPrimitive.content)
        assertNull(model.state.value.readerSettingsUi.draft)
        assertEquals(3L, model.state.value.readerSettingsUi.loadedRev)
    }

    @Test fun ambiguousSaveRetryResendsTheIdenticalFrozenBodyAndOpId() = runBlocking(Dispatchers.IO) {
        var failures = 1
        respond = { request ->
            if (request.url.encodedPath == "/v1/settings" && request.method == "PUT" && failures-- > 0) {
                503 to "{}"
            } else defaultRespond(request)
        }
        val model = model()
        model.loadReaderSettings()
        withTimeout(15000) { model.state.first { !it.readerSettingsUi.loading } }
        model.updateReaderSettingsDraft { it.copy(measure = 68) }
        model.saveReaderSettings()
        withTimeout(15000) { model.state.first { it.readerSettingsUi.error != null } }
        assertNotNull(model.state.value.readerSettingsUi.frozen)

        model.retryReaderSettingsSave()
        withTimeout(15000) { model.state.first { it.readerSettingsUi.success } }
        val attempts = puts()
        assertEquals(2, attempts.size)
        assertEquals(attempts[0].body, attempts[1].body)
        val firstOpId = Json.parseToJsonElement(attempts[0].body).jsonObject.getValue("op_id").jsonPrimitive.content
        val secondOpId = Json.parseToJsonElement(attempts[1].body).jsonObject.getValue("op_id").jsonPrimitive.content
        assertEquals(firstOpId, secondOpId)
    }

    @Test fun editingTheDraftInvalidatesTheFrozenRetry() = runBlocking(Dispatchers.IO) {
        var failures = 1
        respond = { request ->
            if (request.url.encodedPath == "/v1/settings" && request.method == "PUT" && failures-- > 0) {
                503 to "{}"
            } else defaultRespond(request)
        }
        val model = model()
        model.loadReaderSettings()
        withTimeout(15000) { model.state.first { !it.readerSettingsUi.loading } }
        model.updateReaderSettingsDraft { it.copy(measure = 68) }
        model.saveReaderSettings()
        withTimeout(15000) { model.state.first { it.readerSettingsUi.error != null } }
        val frozenOpId = Json.parseToJsonElement(puts().single().body).jsonObject.getValue("op_id").jsonPrimitive.content

        model.updateReaderSettingsDraft { it.copy(measure = 69) }
        assertNull(model.state.value.readerSettingsUi.frozen)
        model.saveReaderSettings()
        withTimeout(15000) { model.state.first { it.readerSettingsUi.success } }
        val retriedOpId = Json.parseToJsonElement(puts().last().body).jsonObject.getValue("op_id").jsonPrimitive.content
        assertFalse("an edited draft reused the frozen op ID", frozenOpId == retriedOpId)
    }

    @Test fun unauthorizedSaveIsNeverRetriedAndSignsOut() = runBlocking(Dispatchers.IO) {
        respond = { request ->
            if (request.url.encodedPath == "/v1/settings" && request.method == "PUT") 401 to """{"error":"unauthorized"}"""
            else defaultRespond(request)
        }
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        val model = model(sessions = sessions)
        model.loadReaderSettings()
        withTimeout(15000) { model.state.first { !it.readerSettingsUi.loading } }
        model.updateReaderSettingsDraft { it.copy(measure = 68) }
        model.saveReaderSettings()
        withTimeout(15000) { model.loggedIn.first { !it } }
        assertFalse(model.loggedIn.value)
        assertNull(sessions.current())
        val attempts = puts().size
        model.retryReaderSettingsSave()
        delay(300)
        assertEquals("a 401 must never be replayed", attempts, puts().size)
    }

    @Test fun newerKnownSettingsWarnAndReloadInsteadOfOverwriting() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadReaderSettings()
        withTimeout(15000) { model.state.first { !it.readerSettingsUi.loading } }
        model.updateReaderSettingsDraft { it.copy(measure = 68) }
        // A background sync acknowledges newer settings than the loaded draft.
        assertNotNull(SessionCache(app, identity).mergeReaderSettings("g1", ReaderSettings(measure = 70, rev = 6)) { true })
        model.saveReaderSettings()
        withTimeout(15000) { model.state.first { it.readerSettingsUi.conflict != null } }
        assertTrue("the stale draft must not be published", puts().isEmpty())
        assertEquals(6L, model.state.value.readerSettingsUi.conflict?.rev)

        getBody = settingsJson(rev = 6, measure = 70)
        model.reloadReaderSettings()
        withTimeout(15000) { model.state.first { !it.readerSettingsUi.loading } }
        assertNull(model.state.value.readerSettingsUi.conflict)
        assertNull(model.state.value.readerSettingsUi.draft)
        assertEquals(6L, model.state.value.readerSettingsUi.loadedRev)
        assertTrue(puts().isEmpty())
    }

    @Test fun offlineLaunchReadsSettingsAndCountsFromTheAccountSnapshot() = runBlocking(Dispatchers.IO) {
        val cache = SessionCache(app, identity)
        cache.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        cache.mergeReaderSettings("g1", ReaderSettings(readingMode = ReadingMode.UNREAD_COUNTS, measure = 69, font = ReaderFont.SANS, rev = 4)) { true }
        val countsRequest = checkNotNull(cache.beginUnreadCountsRequest("g1") { true })
        cache.mergeUnreadCounts(countsRequest, ch.lkmc.earnest.core.UnreadCounts(mapOf(3L to 4L), 1, 2, 7)) { true }
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        val model = AppViewModel(app, sessions, MutableSharedFlow(extraBufferCapacity = 1)) {
            EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
                calls += Call(chain.request().method, chain.request().url.encodedPath, "")
                throw java.io.IOException("offline")
            }.build())
        }
        withTimeout(15000) { model.state.first { it.accountKey != null } }
        assertEquals(69L, model.state.value.readerSettings.measure)
        assertEquals(ReaderFont.SANS, model.state.value.readerSettings.font)
        assertEquals(4L, model.state.value.readerSettingsRev)
        assertEquals(7L, model.state.value.unreadCounts?.total)
        assertNotNull(model.state.value.unreadCountsSyncedAt)
        assertFalse("offline launch must not PUT settings", calls.any { it.method == "PUT" })
    }

    @Test fun loadDoesNotMutateTheSnapshotOrPublish() = runBlocking(Dispatchers.IO) {
        val model = model()
        val before = SessionCache(app, identity).withEngine { engine ->
            engine.snapshot().objects.firstOrNull { it.kind == River.SETTINGS }?.rev
        }
        getBody = settingsJson(rev = 9, measure = 71)
        model.loadReaderSettings()
        withTimeout(15000) { model.state.first { !it.readerSettingsUi.loading } }
        val after = SessionCache(app, identity).withEngine { engine ->
            engine.snapshot().objects.firstOrNull { it.kind == River.SETTINGS }?.rev
        }
        assertEquals("loading must not write the snapshot", before, after)
        assertTrue("loading must not publish", puts().isEmpty())
    }

    @Test fun unreadCountsAreOnlyFetchedInUnreadCountMode() = runBlocking(Dispatchers.IO) {
        val model = model()
        assertFalse("river mode must not request counts", calls.any { it.path == "/v1/entries:counts" })
        assertNull(model.state.value.unreadCounts)

        getBody = settingsJson(rev = 2, mode = ReadingMode.UNREAD_COUNTS)
        model.refresh()
        withTimeout(15000) { model.state.first { it.unreadCounts != null } }
        assertTrue(calls.any { it.path == "/v1/entries:counts" })
        assertEquals(7L, model.state.value.unreadCounts?.total)
        assertEquals(4L, model.state.value.unreadCounts?.bySubscription?.getValue(3L))
        assertEquals(7L, SessionCache(app, identity).cachedUnreadCounts("g1")?.total)
    }

    @Test fun unreadCountFailureKeepsTheLastSyncedCounts() = runBlocking(Dispatchers.IO) {
        getBody = settingsJson(rev = 2, mode = ReadingMode.UNREAD_COUNTS)
        val model = model()
        assertEquals(7L, model.state.value.unreadCounts?.total)

        countsStatus = 503
        model.refresh()
        withTimeout(15000) { model.state.first { it.unreadCountsError != null } }
        assertEquals("a failed refresh must not zero the last synced counts", 7L, model.state.value.unreadCounts?.total)
    }

    @Test fun missingSyncedBaselineCannotPublishAssumedDefaults() = runBlocking(Dispatchers.IO) {
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        respond = { request ->
            if (request.method == "GET" && request.url.encodedPath == "/v1/settings") {
                503 to """{"error":"offline"}"""
            } else defaultRespond(request)
        }
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        val model = AppViewModel(app, sessions, MutableSharedFlow(extraBufferCapacity = 1)) { api(it.token) }
        withTimeout(15_000) { model.state.first { !it.loading && calls.any { call -> call.path == "/v1/tokens" } } }
        assertFalse(model.state.value.snapshot.objects.any { it.kind == River.SETTINGS })
        model.updateReaderSettingsDraft { it.copy(measure = 70) }
        model.saveReaderSettings()
        val unexpected = withTimeoutOrNull(500) {
            while (puts().isEmpty()) delay(1)
            puts().first()
        }
        assertNull("saving without a baseline must not overwrite unseen account preferences", unexpected)
    }

    @Test fun countResponseCannotAdoptARestoredGeneration() = runBlocking(Dispatchers.IO) {
        val signals = MutableSharedFlow<String>(extraBufferCapacity = 1)
        val model = model(signals = signals)
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        getBody = settingsJson(rev = 2, mode = ReadingMode.UNREAD_COUNTS)
        respond = { request ->
            if (request.url.encodedPath == "/v1/entries:counts") {
                entered.complete(Unit)
                runBlocking { release.await() }
            }
            defaultRespond(request)
        }
        model.refresh()
        try {
            withTimeout(5_000) { entered.await() }
            val cache = SessionCache(app, identity)
            cache.withEngine { it.beginBootstrap(Revision("g2", 0)) }
            cache.mergeReaderSettings("g2", ReaderSettings(readingMode = ReadingMode.UNREAD_COUNTS, rev = 1)) { true }
            signals.emit(identity.cacheKey)
            withTimeout(15_000) { model.state.first { it.snapshot.cursor.generation == "g2" } }
        } finally { release.complete(Unit) }
        withTimeout(15_000) { model.state.first { !it.loading } }
        assertNull(model.state.value.unreadCounts)
        assertNull(SessionCache(app, identity).cachedUnreadCounts("g2"))
    }

    @Test fun foregroundRestoreDiscardsCountsWhenEndpointIsUnavailable() = runBlocking(Dispatchers.IO) {
        getBody = settingsJson(rev = 2, mode = ReadingMode.UNREAD_COUNTS)
        val model = model()
        assertEquals(7L, model.state.value.unreadCounts?.total)
        getBody = settingsJson(rev = 1, mode = ReadingMode.UNREAD_COUNTS)
        countsStatus = 503
        respond = { request ->
            val path = request.url.encodedPath
            when {
                path == "/v1/changes" && request.url.queryParameter("generation") == "g1" ->
                    410 to """{"error":"restored"}"""
                path == "/v1/bootstrap" -> 200 to """{"generation":"g2","rev":0}"""
                path.startsWith("/v1/bootstrap/") ->
                    200 to """{"items":[],"next_id":0,"next_principal":0,"next_view":"","has_more":false}"""
                path == "/v1/changes" ->
                    200 to """{"changes":[],"next":{"generation":"g2","rev":0},"next_cursor":"g2:0"}"""
                else -> defaultRespond(request)
            }
        }
        model.refresh()
        withTimeout(15_000) { model.state.first { it.snapshot.cursor.generation == "g2" && !it.loading } }
        assertNull("unavailable restored counts must not reuse the old generation", model.state.value.unreadCounts)
        assertNull(model.state.value.unreadCountsSyncedAt)
        assertNotNull(model.state.value.unreadCountsError)
    }

    @Test fun oldForegroundCountResponseCannotReplaceANewerBackgroundSnapshot() = runBlocking(Dispatchers.IO) {
        val signals = MutableSharedFlow<String>(extraBufferCapacity = 1)
        val model = model(signals = signals)
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        getBody = settingsJson(rev = 2, mode = ReadingMode.UNREAD_COUNTS)
        respond = { request ->
            if (request.url.encodedPath == "/v1/entries:counts") {
                entered.complete(Unit)
                runBlocking { release.await() }
            }
            defaultRespond(request)
        }
        model.refresh()
        try {
            withTimeout(5_000) { entered.await() }
            val background = SessionCache(app, identity)
            val newer = ch.lkmc.earnest.core.UnreadCounts(emptyMap(), 0, 0, 0)
            val countsRequest = checkNotNull(background.beginUnreadCountsRequest("g1") { true })
            assertTrue(background.mergeUnreadCounts(countsRequest, newer) { true })
            signals.emit(identity.cacheKey)
            withTimeout(15_000) { model.state.first { it.unreadCounts?.total == 0L } }
        } finally { release.complete(Unit) }
        withTimeout(15_000) { model.state.first { !it.loading } }
        assertEquals("an older foreground HTTP result must not regress a worker commit", 0L, model.state.value.unreadCounts?.total)
        assertEquals(0L, SessionCache(app, identity).cachedUnreadCounts("g1")?.total)
    }

    @Test fun owningCountUnauthorizedResponseExpiresTheSession() = runBlocking(Dispatchers.IO) {
        val sessions = Sessions(AuthenticatedSession(identity, "token-a"))
        val model = model(sessions = sessions)
        getBody = settingsJson(rev = 2, mode = ReadingMode.UNREAD_COUNTS)
        countsStatus = 401
        model.refresh()
        withTimeout(15_000) { model.loggedIn.first { !it } }
        assertNull(sessions.current())
        assertNull(model.state.value.readerSettingsUi.draft)
        assertNull(model.state.value.unreadCounts)
    }

    @Test fun loginRacingCountTokenRemovalKeepsTheFreshSession() = runBlocking(Dispatchers.IO) {
        val sessions = Sessions(AuthenticatedSession(identity, "token-a"))
        val model = model(sessions = sessions)
        val fresh = AuthenticatedSession(identity, "fresh-token")
        getBody = settingsJson(rev = 2, mode = ReadingMode.UNREAD_COUNTS)
        respond = { request ->
            if (request.url.encodedPath == "/v1/entries:counts" && request.header("Authorization") == "Bearer token-a") {
                401 to """{"error":"expired"}"""
            } else defaultRespond(request)
        }
        sessions.beforeClear = {
            sessions.value = fresh
            model.onLoggedIn()
        }
        model.refresh()
        withTimeout(5_000) { sessions.clearAttempted.await() }
        assertEquals("expiry must atomically remove only its own credentials", fresh, sessions.current())
        withTimeout(15_000) { model.state.first { !it.loading && it.unreadCounts != null } }
        assertTrue(model.loggedIn.value)
    }

    @Test fun staleCountUnauthorizedResponseCannotExpireTheNextAccount() = runBlocking(Dispatchers.IO) {
        val sessions = Sessions(AuthenticatedSession(identity, "token-a"))
        val model = model(sessions = sessions)
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        val returned = CompletableDeferred<Unit>()
        getBody = settingsJson(rev = 2, mode = ReadingMode.UNREAD_COUNTS)
        respond = { request ->
            if (request.url.encodedPath == "/v1/entries:counts" && request.header("Authorization") == "Bearer token-a") {
                entered.complete(Unit)
                runBlocking { release.await() }
                returned.complete(Unit)
                401 to """{"error":"expired"}"""
            } else defaultRespond(request)
        }
        model.refresh()
        try {
            withTimeout(5_000) { entered.await() }
            model.logout()
            val next = AccountIdentity(identity.server, 2, 2)
            val nextCache = SessionCache(app, next)
            nextCache.withEngine { it.beginBootstrap(Revision("g1", 0)) }
            nextCache.mergeReaderSettings("g1", ReaderSettings(rev = 1)) { true }
            getBody = settingsJson(rev = 3)
            sessions.value = AuthenticatedSession(next, "token-b")
            model.onLoggedIn()
            withTimeout(15_000) { model.state.first { it.accountKey == next.cacheKey && !it.loading } }
        } finally { release.complete(Unit) }
        withTimeout(5_000) { returned.await() }
        delay(100)
        assertEquals("token-b", sessions.current()?.token)
        assertTrue(model.loggedIn.value)
        assertNull(model.state.value.unreadCounts)
    }

    @Test fun savePublishesNothingAfterTheAccountIsLost() = runBlocking(Dispatchers.IO) {
        val putEntered = CompletableDeferred<Unit>()
        val releasePut = CompletableDeferred<Unit>()
        respond = { request ->
            if (request.url.encodedPath == "/v1/settings" && request.method == "PUT") {
                putEntered.complete(Unit)
                runBlocking { releasePut.await() }
            }
            defaultRespond(request)
        }
        val model = model()
        model.loadReaderSettings()
        withTimeout(15000) { model.state.first { !it.readerSettingsUi.loading } }
        model.updateReaderSettingsDraft { it.copy(measure = 68) }
        val before = SessionCache(app, identity).withEngine { engine -> engine.snapshot().objects.firstOrNull { it.kind == River.SETTINGS }?.rev }
        model.saveReaderSettings()
        withTimeout(15000) { putEntered.await() }
        // The session is lost while the save is in flight.
        model.logout()
        releasePut.complete(Unit)
        withTimeout(15000) { model.state.first { !it.readerSettingsUi.saving } }
        delay(300)
        val after = SessionCache(app, identity).withEngine { engine -> engine.snapshot().objects.firstOrNull { it.kind == River.SETTINGS }?.rev }
        assertEquals("a lost account must not merge the acknowledged save", before, after)
    }
}

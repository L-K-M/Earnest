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
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeout
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
    private data class Call(val method: String, val path: String, val body: String)
    private val calls = CopyOnWriteArrayList<Call>()

    private class Sessions(@Volatile var value: AuthenticatedSession?) : SessionStore {
        override fun current() = value
        override fun clearToken() { value = null }
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

    private fun api() = EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
        val request = chain.request()
        calls += Call(request.method, request.url.encodedPath, request.body?.let { body ->
            okio.Buffer().also { body.writeTo(it) }.readUtf8()
        }.orEmpty())
        val (code, body) = respond(request)
        Response.Builder().request(request).protocol(Protocol.HTTP_1_1).code(code).message("test")
            .body(body.toResponseBody()).build()
    }.build())

    private suspend fun model(
        sessions: Sessions = Sessions(AuthenticatedSession(identity, "token")),
        setup: suspend (SessionCache) -> Unit = {},
    ): AppViewModel {
        val cache = SessionCache(app, identity)
        cache.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        setup(cache)
        val model = AppViewModel(app, sessions, MutableSharedFlow(extraBufferCapacity = 1)) { api() }
        withTimeout(15000) { model.state.first { !it.loading && calls.any { it.path == "/v1/tokens" } } }
        return model
    }

    private fun puts(): List<Call> = calls.filter { it.method == "PUT" && it.path == "/v1/settings" }

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
        assertTrue(SessionCache(app, identity).mergeReaderSettings(ReaderSettings(measure = 70, rev = 6)))
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
        cache.mergeReaderSettings(ReaderSettings(measure = 69, font = ReaderFont.SANS, rev = 4))
        cache.storeUnreadCounts(ch.lkmc.earnest.core.UnreadCounts(mapOf(3L to 4L), 1, 2, 7))
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
        assertEquals(7L, SessionCache(app, identity).cachedUnreadCounts()?.total)
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

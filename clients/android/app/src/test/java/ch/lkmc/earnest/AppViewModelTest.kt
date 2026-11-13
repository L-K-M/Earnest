package ch.lkmc.earnest

import android.app.Application
import android.content.SharedPreferences
import androidx.core.content.edit
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.*
import kotlinx.coroutines.*
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.test.TestCoroutineScheduler
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.After
import org.junit.Assert.*
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
class AppViewModelTest {
    private val app get() = ApplicationProvider.getApplicationContext<Application>()
    private val identity = AccountIdentity("https://nas.test", 1, 1)
    private val calls = CopyOnWriteArrayList<String>()
    private val payloads = CopyOnWriteArrayList<String>()
    private val contentUrlQueries = CopyOnWriteArrayList<String?>()
    @Volatile private var frontPageOverride: String? = null
    @Volatile private var frontPageStatus: Int = 200
    @Volatile private var frontPageGate: (() -> Unit)? = null
    private class Sessions(initial: AuthenticatedSession?) : SessionStore {
        private val stored = java.util.concurrent.atomic.AtomicReference(initial)
        var value: AuthenticatedSession?
            get() = stored.get()
            set(value) { stored.set(value) }
        @Volatile var onRead: (() -> Unit)? = null
        @Volatile var onClear: (() -> Unit)? = null
        override fun current(): AuthenticatedSession? {
            val callback = onRead
            onRead = null
            callback?.invoke()
            return stored.get()
        }
        override fun clearToken() { stored.set(null); onClear?.invoke() }
        override fun clearToken(expected: AuthenticatedSession): Boolean {
            while (true) {
                val current = stored.get()
                if (current != expected) return false
                if (stored.compareAndSet(current, null)) {
                    onClear?.invoke()
                    return true
                }
            }
        }
    }
    @Before fun setup() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
    }
    @After fun teardown() { Dispatchers.resetMain() }

    private fun api(agentChangesStatus: Int = 200, unauthorizedPath: String? = null, cancelledPath: String? = null,
                    onRequest: (String) -> Unit = {}) = EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
        val path = chain.request().url.encodedPath
        onRequest(path)
        calls += path
        chain.request().body?.takeIf { !it.isOneShot() }?.let { body ->
            val buffer = okio.Buffer()
            body.writeTo(buffer)
            payloads += buffer.readUtf8()
        }
        if (path.endsWith("/content-url")) contentUrlQueries += chain.request().url.query
        val body = when (path) {
            "/v1/front-page" -> frontPageOverride ?: frontPageJson("rev-1", "daily")
            "/v1/entries/2/content-url" -> """{"url":"https://content.test/signed","expires_at":"2026-11-03T01:00:00Z"}"""
            "/v1/settings" -> """{"today_decay_days":2,"rev":0}"""
            "/v1/changes" -> """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
            "/v1/activity/agent-changes" -> """[{"id":7,"principal_id":9,"principal_name":"Morning Agent","action":"state.updated","target":{"entry_id":2,"register":"shelf"},"before":null,"after":{"shelf":{"value":"done"}},"generation":"g1","created_at":"${java.time.Instant.now()}","next_action":"revert"}]"""
            "/v1/entries/2" -> """{"id":2,"subscription_id":3,"feed_id":5,"seq":4,"first_seen_at":"2026-11-03T00:00:00Z","title":"Notification headline","url":"https://news.test/article","author":"Author","rev":2,"deleted":false}"""
            "/v1/subscriptions/3" -> """{"id":3,"feed_id":5,"title":"Feed","url":"https://feed.test/rss","site_url":"https://feed.test","folder_id":0,"cadence":"today","extract":"none","rev":1,"deleted":false,"private":false,"muted":false,"initial_through_seq":0,"initial_fetch_pending":false,"health":{"next_fetch_at":"2026-11-10T00:00:00Z","consecutive_failures":0,"http_status":200,"posts_per_day":1.0,"firehose":false}}"""
            "/v1/opml:import" -> """[{"path":[],"deferred":true,"subscription":{"id":3,"feed_id":5,"title":"Feed","url":"https://feed.test/rss","site_url":"https://feed.test","folder_id":0,"cadence":"today","extract":"none","rev":1,"deleted":false,"private":false,"muted":false,"initial_through_seq":0,"initial_fetch_pending":false,"health":{"next_fetch_at":"2026-11-10T00:00:00Z","consecutive_failures":0,"http_status":200,"posts_per_day":1.0,"firehose":false}}}]"""
            else -> "[]"
        }
        if (path == "/v1/front-page") frontPageGate?.invoke()
        val status = when (path) {
            unauthorizedPath -> 401
            "/v1/activity/agent-changes" -> agentChangesStatus
            "/v1/front-page" -> frontPageStatus
            else -> 200
        }
        val responseBody = if (path == cancelledPath) object : okhttp3.ResponseBody() {
            override fun contentType(): okhttp3.MediaType? = null
            override fun contentLength() = -1L
            override fun source(): okio.BufferedSource = throw kotlinx.coroutines.CancellationException("metadata cancelled")
        } else body.toResponseBody()
        Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(status).message("test")
            .body(responseBody).build()
    }.build())

    private fun frontPageJson(revision: String, period: String): String {
        val entry = """{"id":2,"subscription_id":3,"feed_id":5,"seq":4,"first_seen_at":"2026-11-03T00:00:00Z","published_at":"2026-11-02T12:00:00Z","title":"Front headline","url":"https://news.test/article","author":"Author","rev":2,"deleted":false,"state":{"principal":1,"entry_id":2,"seen":{"value":"unseen","rev":1,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"passive"},"shelf":{"value":"none","rev":1,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"passive"},"kept":{"value":false,"rev":1,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"passive"},"state_rev":1}}"""
        return """{"period":"$period","window_start":"2026-11-02T10:00:00Z","window_end":"2026-11-03T10:00:00Z","selected_at":"2026-11-03T10:00:00Z","revision":"$revision","sections":[{"name":"Across your feeds","stories":[{"id":"story-1","entry":$entry,"source_title":"Source A","published_at":"2026-11-02T12:00:00Z","feed_count":1,"excerpt":"Excerpt","coverage":[{"entry":$entry,"source_title":"Source A"}]}]}]}"""
    }

    private suspend fun model(): AppViewModel {
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) { api() }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        return model
    }

    /** Waits for [condition], naming [what] when time runs out. */
    private suspend fun await(what: String, condition: () -> Boolean) {
        withTimeoutOrNull(15_000) {
            while (!condition()) delay(10)
        } ?: throw AssertionError("timed out waiting for $what")
    }

    private suspend fun corruptSnapshot() = SyncAccess.mutex.withLock {
        // A save callback can run before withEngine records its final version.
        // Serialize the fixture writer so it cannot make a healthy cached
        // engine adopt the corrupted row's version and bypass the disk failure.
        SessionCache(app, identity).db.snapshot().save(ch.lkmc.earnest.db.SyncSnapshotEntity(json = "invalid snapshot"))
    }

    @Test fun agentSelectedEntriesOpenWithClickToLoadImages() = runBlocking(Dispatchers.IO) {
        val model = model()
        assertEquals("https://content.test/signed", model.openEntry(2)?.url)
        assertEquals("https://content.test/signed", model.openEntry(2, agentSelected = true)?.url)
        // Your own browsing gets proxied images (the server default); an
        // agent-surfaced entry must not load remote images automatically.
        assertEquals(listOf<String?>(null, "mode=click"), contentUrlQueries.toList())
    }

    @Test fun retentionRecoveryRejectionsReachTheUi() = runBlocking(Dispatchers.IO) {
        val cache = SessionCache(app, identity)
        cache.withEngine {
            it.beginBootstrap(Revision("g1", 0))
            it.queue(StateWrite("gone", 1, 2, "shelf", kotlinx.serialization.json.JsonPrimitive("later"), Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit"))
        }
        var recovered = false
        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) {
            EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
                val path = chain.request().url.encodedPath
                val (code, body) = when {
                    path == "/v1/settings" -> 200 to """{"today_decay_days":2,"rev":0}"""
                    path == "/v1/changes" && !recovered -> 410 to "{}"
                    path == "/v1/bootstrap" -> { recovered = true; 200 to """{"generation":"g1","rev":0}""" }
                    path.startsWith("/v1/bootstrap/") -> 200 to """{"items":[],"next_id":0,"next_principal":0,"next_view":"","has_more":false}"""
                    path == "/v1/changes" -> 200 to """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
                    path == "/v1/state:batch" -> 200 to kotlinx.serialization.json.Json.encodeToString(kotlinx.serialization.builtins.ListSerializer(Result.serializer()), listOf(Result("gone", "gone", SyncEngine.initialState(1, 2), false)))
                    else -> 200 to "[]"
                }
                Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(code).message("test")
                    .body(body.toResponseBody()).build()
            }.build())
        }
        withTimeout(15000) { model.state.first { !it.loading && it.rejected.isNotEmpty() } }
        assertEquals(listOf(RejectedOperation("gone", "gone")), model.state.value.rejected)
        assertTrue(model.state.value.error.orEmpty().contains("could not be synced"))
    }

    @Test fun cacheFailureReportsAnErrorWithoutShelfSuccess() = runBlocking(Dispatchers.IO) {
        val model = model()
        corruptSnapshot()
        val saved = java.util.concurrent.atomic.AtomicInteger()
        model.later(2) { saved.incrementAndGet() }
        withTimeout(15000) { model.state.first { it.error?.startsWith("Could not save the action") == true } }
        assertEquals(0, saved.get())
    }

    @Test fun messageLessFailuresStillNameTheFailedAction() = runBlocking(Dispatchers.IO) {
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        var factory: () -> EarnestApi = { api() }
        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) { factory() }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }

        factory = {
            EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { throw java.io.IOException() }.build())
        }
        model.subscribe("https://feed.test/rss", "daily")
        withTimeout(15000) { model.state.first { it.error != null } }
        val error = model.state.value.error
        assertNotNull(error)
        assertFalse("error showed null: $error", error!!.contains("null"))
        assertEquals("Could not subscribe.", error)

        // A present message stays visible after the action text.
        factory = {
            EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { throw java.io.IOException("feed unreachable") }.build())
        }
        model.refresh()
        withTimeout(15000) { model.state.first { it.error == "Could not refresh: feed unreachable" } }
        assertEquals("Could not refresh: feed unreachable", model.state.value.error)
    }

    @Test fun operationIdsRemainUniqueWithinTheSameMillisecond() = runBlocking(Dispatchers.IO) {
        val model = model()
        val method = AppViewModel::class.java.getDeclaredMethod("opId", String::class.java).apply { isAccessible = true }
        val ids = List(1000) { method.invoke(model, "cadence") as String }
        assertEquals("resource mutations reused a deduplication ID", ids.size, ids.toSet().size)
    }

    @Test fun shelfSuccessIsReportedOnlyAfterDurableQueueing() = runBlocking(Dispatchers.IO) {
        val model = model()
        val saved = java.util.concurrent.atomic.AtomicInteger()
        SyncAccess.mutex.lock()
        try {
            model.later(2) { saved.incrementAndGet() }
            assertEquals("success reported before the cache write", 0, saved.get())
        } finally {
            SyncAccess.mutex.unlock()
        }
        withTimeout(15000) { while (saved.get() == 0) delay(10) }
        assertEquals("later", SessionCache(app, identity).withEngine { it.snapshot().outbox.single().operation.value.jsonPrimitive.content })
    }

    @Test fun shelfFailureDoesNotReportSuccess() = runBlocking(Dispatchers.IO) {
        val model = model()
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("", 0)) }
        val saved = java.util.concurrent.atomic.AtomicInteger()
        model.done(2) { saved.incrementAndGet() }
        withTimeout(15000) { model.state.first { it.error?.contains("action was not saved") == true } }
        assertEquals("failed action reported success", 0, saved.get())
    }

    @Test fun markUnreadClearsDoneAndUndoRestoresBothRegisters() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadEntry(2)
        SessionCache(app, identity).withEngine {
            it.queue(StateWrite("seed-seen", 1, 2, SyncEngine.REGISTER_SEEN, kotlinx.serialization.json.JsonPrimitive("seen"), Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit"))
            it.queue(StateWrite("seed-shelf", 1, 2, SyncEngine.REGISTER_SHELF, kotlinx.serialization.json.JsonPrimitive("done"), Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit"))
        }
        val action = java.util.concurrent.atomic.AtomicReference<ReversibleAction>()
        model.markUnread(2) { action.set(it) }
        withTimeout(15000) { while (action.get() == null) delay(10) }
        var state = SessionCache(app, identity).withEngine { it.state(1, 2) }
        assertEquals(kotlinx.serialization.json.JsonPrimitive("unseen"), state.seen.value)
        assertEquals(kotlinx.serialization.json.JsonPrimitive("none"), state.shelf.value)

        val undone = java.util.concurrent.atomic.AtomicInteger()
        model.undo(action.get(), onSaved = { undone.incrementAndGet() })
        withTimeout(15000) { while (undone.get() == 0) delay(10) }
        state = SessionCache(app, identity).withEngine { it.state(1, 2) }
        assertEquals(kotlinx.serialization.json.JsonPrimitive("seen"), state.seen.value)
        assertEquals(kotlinx.serialization.json.JsonPrimitive("done"), state.shelf.value)
    }

    @Test fun keepIsUndoableWithTheValuesItReplaced() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadEntry(2)
        val action = java.util.concurrent.atomic.AtomicReference<ReversibleAction>()
        model.keep(2) { action.set(it) }
        withTimeout(15000) { while (action.get() == null) delay(10) }
        assertEquals("Kept", action.get().message)
        assertEquals(kotlinx.serialization.json.JsonPrimitive(false), action.get().prior[SyncEngine.REGISTER_KEPT])
        assertEquals(kotlinx.serialization.json.JsonPrimitive(true), action.get().written[SyncEngine.REGISTER_KEPT])
        assertEquals(kotlinx.serialization.json.JsonPrimitive(true), SessionCache(app, identity).withEngine { it.state(1, 2) }.kept.value)

        val undone = java.util.concurrent.atomic.AtomicInteger()
        model.undo(action.get(), onSaved = { undone.incrementAndGet() })
        withTimeout(15000) { while (undone.get() == 0) delay(10) }
        assertEquals(kotlinx.serialization.json.JsonPrimitive(false), SessionCache(app, identity).withEngine { it.state(1, 2) }.kept.value)
    }

    @Test fun undoLeavesAnActionAnotherWriteChanged() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadEntry(2)
        val first = java.util.concurrent.atomic.AtomicReference<ReversibleAction>()
        model.later(2) { first.set(it) }
        withTimeout(15000) { while (first.get() == null) delay(10) }
        val second = java.util.concurrent.atomic.AtomicReference<ReversibleAction>()
        model.done(2) { second.set(it) }
        withTimeout(15000) { while (second.get() == null) delay(10) }

        val undone = java.util.concurrent.atomic.AtomicInteger()
        model.undo(first.get(), onSaved = { undone.incrementAndGet() })
        withTimeout(15000) { while (undone.get() == 0) delay(10) }
        // The Later action's write is gone from the article; undo must not
        // drag it out of Done back to no shelf.
        assertEquals(kotlinx.serialization.json.JsonPrimitive("done"), SessionCache(app, identity).withEngine { it.state(1, 2) }.shelf.value)
    }

    @Test fun markUnreadSticksThroughScrollingPast() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadEntry(2)
        // Opening the article reads it.
        model.markSeen(2)
        withTimeout(15000) {
            while (SessionCache(app, identity).withEngine { it.state(1, 2) }.seen.value != kotlinx.serialization.json.JsonPrimitive("seen")) delay(10)
        }
        val action = java.util.concurrent.atomic.AtomicReference<ReversibleAction>()
        model.markUnread(2) { action.set(it) }
        withTimeout(15000) { while (action.get() == null) delay(10) }

        // Scrolling past the row afterwards never re-marks it read.
        assertFalse(model.markScrolledPast(2))
        delay(250)
        assertEquals(kotlinx.serialization.json.JsonPrimitive("unseen"), SessionCache(app, identity).withEngine { it.state(1, 2) }.seen.value)

        // Opening the article reads it again.
        model.markSeen(2)
        withTimeout(15000) {
            while (SessionCache(app, identity).withEngine { it.state(1, 2) }.seen.value != kotlinx.serialization.json.JsonPrimitive("seen")) delay(10)
        }

        // Opening released the guard: after another writer leaves the row
        // unread again, scrolling past it marks it read.
        SessionCache(app, identity).withEngine {
            it.queue(StateWrite("external-unread", 1, 2, SyncEngine.REGISTER_SEEN, kotlinx.serialization.json.JsonPrimitive("unseen"), Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit"))
        }
        withTimeout(15000) {
            while (SessionCache(app, identity).withEngine { it.state(1, 2) }.seen.value != kotlinx.serialization.json.JsonPrimitive("unseen")) delay(10)
        }
        assertTrue(model.markScrolledPast(2))
        withTimeout(15000) {
            while (SessionCache(app, identity).withEngine { it.state(1, 2) }.seen.value != kotlinx.serialization.json.JsonPrimitive("seen")) delay(10)
        }
    }

    @Test fun signingOutClearsTheMarkUnreadGuard() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadEntry(2)
        val action = java.util.concurrent.atomic.AtomicReference<ReversibleAction>()
        model.markUnread(2) { action.set(it) }
        withTimeout(15000) { while (action.get() == null) delay(10) }
        assertFalse(model.markScrolledPast(2))
        model.logout()
        assertTrue(model.markScrolledPast(2))
    }

    @Test fun undoReleasesTheMarkUnreadGuard() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadEntry(2)
        val action = java.util.concurrent.atomic.AtomicReference<ReversibleAction>()
        model.markUnread(2) { action.set(it) }
        withTimeout(15000) { while (action.get() == null) delay(10) }
        assertFalse(model.markScrolledPast(2))

        val undone = java.util.concurrent.atomic.AtomicInteger()
        model.undo(action.get(), onSaved = { undone.incrementAndGet() })
        withTimeout(15000) { while (undone.get() == 0) delay(10) }
        assertTrue(model.markScrolledPast(2))
    }

    @Test fun failedMarkUnreadReleasesTheGuard() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadEntry(2)
        corruptSnapshot()
        val action = java.util.concurrent.atomic.AtomicReference<ReversibleAction>()
        model.markUnread(2) { action.set(it) }
        // The guard is released in queue()'s finally once the failed write is
        // known not to have landed; the release is the completion signal, not
        // the shared error text, which a concurrent flush may replace.
        await("the failed save to release the guard") { model.markScrolledPast(2) }
        assertNull("a failed save must not report success", action.get())
        // queue() publishes the error before its finally releases the guard.
        assertNotNull("a failed save must surface an error", model.state.value.error)
    }

    @Test fun failedUndoKeepsTheMarkUnreadGuard() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadEntry(2)
        val action = java.util.concurrent.atomic.AtomicReference<ReversibleAction>()
        model.markUnread(2) { action.set(it) }
        await("the mark-unread save") { action.get() != null }
        corruptSnapshot()

        val undone = java.util.concurrent.atomic.AtomicInteger()
        val failed = java.util.concurrent.atomic.AtomicInteger()
        model.undo(action.get(), onSaved = { undone.incrementAndGet() }, onNotSaved = { failed.incrementAndGet() })
        // The failure callback is the completion signal; the shared error text
        // is not, because the debounced outbox flush fails on the same corrupt
        // snapshot and replaces the text while this would wait for it.
        await("the failed undo to report") { failed.get() > 0 }
        assertEquals(0, undone.get())
        // queue() publishes the error before its finally runs onNotSaved.
        assertNotNull("a failed undo must surface an error", model.state.value.error)
        // The article is still unread: the guard stands.
        assertFalse(model.markScrolledPast(2))
    }

    @Test fun scrollingPastMarksUnseenRowsRead() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadEntry(2)
        assertTrue(model.markScrolledPast(2))
        withTimeout(15000) {
            while (SessionCache(app, identity).withEngine { it.state(1, 2) }.seen.value != kotlinx.serialization.json.JsonPrimitive("seen")) delay(10)
        }
    }

    @Test fun cadenceUndoLeavesOneChangedSinceAlone() = runBlocking(Dispatchers.IO) {
        val model = model()
        // The user changes the cadence twice in a row.
        val first = java.util.concurrent.atomic.AtomicInteger()
        model.setCadence(3, "week") { first.incrementAndGet() }
        withTimeout(15000) { while (first.get() == 0) delay(10) }
        val second = java.util.concurrent.atomic.AtomicInteger()
        model.setCadence(3, "now") { second.incrementAndGet() }
        withTimeout(15000) { while (second.get() == 0) delay(10) }

        // The first change's undo must not drag the feed back past the second.
        calls.clear()
        assertFalse(model.undoCadence(3, restore = "today", written = "week"))
        delay(250)
        assertTrue("undo clobbered the later change", calls.none { it.startsWith("/v1/subscriptions/") })

        // The second change's undo restores exactly what it replaced:
        // "now" had replaced "week".
        payloads.clear()
        assertTrue(model.undoCadence(3, restore = "week", written = "now"))
        withTimeout(15000) { while (payloads.none { it.contains("\"cadence\":\"week\"") }) delay(10) }
    }

    @Test fun cadenceUndoLeavesAForeignChangeAlone() = runBlocking(Dispatchers.IO) {
        val model = model()
        val saved = java.util.concurrent.atomic.AtomicInteger()
        model.setCadence(3, "now") { saved.incrementAndGet() }
        withTimeout(15000) { while (saved.get() == 0) delay(10) }

        // Someone else changed the feed after us; the refreshed snapshot
        // carries a third value the guard must leave alone.
        val cache = SessionCache(app, identity)
        cache.withEngine {
            it.mergeObject(ch.lkmc.earnest.core.SyncObject(
                kind = SyncEngine.OBJECT_SUBSCRIPTION, id = 3, name = "Feed", rev = 2,
                raw = """{"cadence":"week"}""",
            ))
        }
        model.refresh()
        withTimeout(15000) {
            while (model.state.value.snapshot.objects.none { it.id == 3L && ch.lkmc.earnest.core.River.cadence(it) == "week" }) delay(10)
        }
        calls.clear()
        assertFalse("undo clobbered a foreign change", model.undoCadence(3, restore = "today", written = "now"))
        delay(250)
        assertTrue(calls.none { it.startsWith("/v1/subscriptions/") })

        // A snapshot still showing what the change replaced (a slow refresh)
        // must not block the restore.
        cache.withEngine {
            it.mergeObject(ch.lkmc.earnest.core.SyncObject(
                kind = SyncEngine.OBJECT_SUBSCRIPTION, id = 3, name = "Feed", rev = 3,
                raw = """{"cadence":"today"}""",
            ))
        }
        model.refresh()
        withTimeout(15000) {
            while (model.state.value.snapshot.objects.none { it.id == 3L && ch.lkmc.earnest.core.River.cadence(it) == "today" }) delay(10)
        }
        payloads.clear()
        assertTrue(model.undoCadence(3, restore = "today", written = "now"))
        withTimeout(15000) { while (payloads.none { it.contains("\"cadence\":\"today\"") }) delay(10) }
    }

    @Test fun opmlImportPublishesASummaryAndRefreshes() = runBlocking(Dispatchers.IO) {
        val model = model()
        calls.clear()
        model.importOpml("""<opml version="2.0"/>""".toByteArray())
        withTimeout(15000) { model.state.first { it.opml.summary != null } }
        assertEquals(1, model.state.value.opml.summary?.subscriptions)
        assertEquals(0, model.state.value.opml.summary?.errors?.size)
        withTimeout(15000) { while (calls.none { it == "/v1/changes" }) delay(10) }
    }

    @Test fun opmlImportFailureIsReportedHonestly() = runBlocking(Dispatchers.IO) {
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        var factory: () -> EarnestApi = { api() }
        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) { factory() }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        factory = {
            EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { throw java.io.IOException() }.build())
        }
        model.importOpml("""<opml/>""".toByteArray())
        withTimeout(15000) { model.state.first { it.opml.error != null } }
        assertEquals("Could not import that file.", model.state.value.opml.error)
    }

    @Test fun exportFailureIsReportedHonestly() = runBlocking(Dispatchers.IO) {
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        var factory: () -> EarnestApi = { api() }
        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) { factory() }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        factory = {
            EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { throw java.io.IOException() }.build())
        }
        assertNull(model.exportOpml())
        assertEquals("Could not export subscriptions.", model.state.value.opml.error)
    }

    @Test fun aSuccessfulExportClearsAPreviousError() = runBlocking(Dispatchers.IO) {
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        var factory: () -> EarnestApi = { api() }
        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) { factory() }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        var failing = true
        factory = {
            EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
                if (failing) throw java.io.IOException()
                Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
                    .code(200).message("test").body("""<opml version="2.0"><body/></opml>""".toResponseBody()).build()
            }.build())
        }
        assertNull(model.exportOpml())
        assertEquals("Could not export subscriptions.", model.state.value.opml.error)
        failing = false
        assertEquals("""<opml version="2.0"><body/></opml>""", model.exportOpml())
        assertNull(model.state.value.opml.error)
    }

    @Test fun unauthorizedExportExpiresTheOwningSession() = runBlocking(Dispatchers.IO) {
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        val model = AppViewModel(app, sessions) { api(unauthorizedPath = "/v1/opml") }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        assertNull(model.exportOpml())
        withTimeout(15000) { while (sessions.value != null) delay(10) }
    }

    @Test fun concurrentImageLoadsShareOneApiClient() = runBlocking(Dispatchers.IO) {
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val created = java.util.concurrent.atomic.AtomicInteger()
        val imageClients = java.util.concurrent.ConcurrentHashMap.newKeySet<Int>()
        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) {
            val clientID = created.incrementAndGet()
            api(onRequest = { path -> if (path.endsWith("/thumbnail")) imageClients += clientID })
        }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        // Metadata may still be loading. Count clients used by the images,
        // and use distinct URLs so cache coalescing cannot hide extra clients.
        kotlinx.coroutines.coroutineScope {
            repeat(8) { index -> launch { model.mediaBitmap("/v1/entries/${index + 2}/thumbnail?v=1") } }
        }
        assertEquals(8, calls.count { it.endsWith("/thumbnail") })
        assertEquals("a list's image loads must share one client", 1, imageClients.size)
    }

    @Test fun notificationMissingFromSnapshotFetchesMetadata() = runBlocking(Dispatchers.IO) {
        val model = model()
        val entry = model.loadEntry(2)
        assertNotNull("notification entry was absent from cache", entry)
        assertEquals("Notification headline", entry?.title)
        assertEquals("Notification headline", model.state.value.snapshot.entries.single().title)
    }

    @Test fun backgroundSyncCommitReloadsUiState() = runBlocking(Dispatchers.IO) {
        val signals = MutableSharedFlow<String>(extraBufferCapacity = 1)
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token")), signals) { api() }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        assertTrue(model.state.value.snapshot.entries.isEmpty())

        // A background writer commits to the same account cache, then publishes.
        SessionCache(app, identity).withEngine { engine ->
            engine.applyPage(
                listOf(SyncChange(1, entry = CachedEntry(5, title = "Pulled by background sync"))),
                Revision("g1", 1),
            )
            engine.flush()
        }
        signals.emit(identity.cacheKey)
        withTimeout(15000) {
            model.state.first { state ->
                state.snapshot.entries.any { it.id == 5L && it.title == "Pulled by background sync" }
            }
        }
        assertEquals(setOf(5L), model.state.value.states.keys)

        // A commit signalled for a different account leaves this account alone.
        SessionCache(app, identity).withEngine { engine ->
            engine.mergeEntry(CachedEntry(6, title = "Not signalled"))
            engine.flush()
        }
        signals.emit("other-account")
        delay(300)
        assertNull(model.state.value.snapshot.entries.firstOrNull { it.id == 6L })
    }

    @Test fun refreshFetchesAgentChangesForTodayAndSessionEnd() = runBlocking(Dispatchers.IO) {
        val model = model()
        assertTrue("refresh never requested agent changes", calls.contains("/v1/activity/agent-changes"))
        assertEquals("Morning Agent", model.state.value.agentChanges.single().principalName)
        assertEquals(listOf(7L), model.state.value.sessionAgentChanges.map { it.id })
    }

    @Test fun accountSwitchBeforeContentRequestCannotWriteOtherAccountsContent() = runBlocking(Dispatchers.IO) {
        val sessionA = AuthenticatedSession(identity, "account-a")
        val sessionB = AuthenticatedSession(AccountIdentity(identity.server, 2, 2), "account-b")
        val sessions = Sessions(sessionA)
        val cacheA = SessionCache(app, sessionA.identity)
        val cacheB = SessionCache(app, sessionB.identity)
        for (cache in listOf(cacheA, cacheB)) {
            cache.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        }
        var offline = false
        val model = AppViewModel(app, sessions) { session ->
            EarnestApi(session.identity.server, session.token, OkHttpClient.Builder().addInterceptor { chain ->
                if (offline) throw java.io.IOException("offline")
                val path = chain.request().url.encodedPath
                calls += "${session.token}:$path"
                val body = when (path) {
                    "/v1/settings" -> """{"today_decay_days":2,"rev":0}"""
                    "/v1/changes" -> """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
                    "/v1/entries/2/content" -> """{"entry_id":2,"html":"<p>${session.token}</p>","text":"${session.token}","sanitizer_version":1,"source":"feed"}"""
                    "/v1/entries/2/content-url" -> """{"url":"https://content.test/${session.token}","expires_at":"2026-11-03T01:00:00Z"}"""
                    else -> "[]"
                }
                Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(200).message("test")
                    .body(body.toResponseBody()).build()
            }.build())
        }
        suspend fun awaitRefresh(session: AuthenticatedSession) {
            withTimeout(15000) {
                model.state.first {
                    it.accountKey == session.identity.cacheKey && !it.loading &&
                        calls.contains("${session.token}:/v1/activity/agent-changes")
                }
            }
        }
        awaitRefresh(sessionA)

        // Switch while openEntry has A's cache and session, before its session-store read returns.
        sessions.onRead = {
            sessions.value = sessionB
            model.onLoggedIn()
            runBlocking { awaitRefresh(sessionB) }
        }
        assertNull(model.openEntry(2))
        assertNull("B's content was persisted in A's database", cacheA.db.content().get(2))
        assertNull(cacheB.db.content().get(2))
        assertFalse(calls.any { it.endsWith("/content") || it.endsWith("/content-url") })

        offline = true
        sessions.value = sessionA
        model.onLoggedIn()
        withTimeout(15000) { model.state.first { it.accountKey == identity.cacheKey && it.error != null } }
        assertNull("A must not see B's content offline", model.openEntry(2))

        offline = false
        assertEquals("https://content.test/account-a", model.openEntry(2)?.url)
        offline = true
        val cached = model.openEntry(2)
        assertEquals("<p>account-a</p>", cached?.cachedHtml)
        assertNull(cached?.url)
    }

    @Test fun clearedTokenSignsOutOnRefresh() = runBlocking(Dispatchers.IO) {
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val model = AppViewModel(app, sessions) { api() }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        sessions.clearToken()
        model.refresh()
        withTimeout(15000) { model.loggedIn.first { !it } }
        assertFalse(model.loggedIn.value)
        assertEquals("Your session expired. Sign in again.", model.sessionNotice.value)
    }

    @Test fun cancelledMetadataDoesNotContinueTheRefresh() = runBlocking(Dispatchers.IO) {
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        var cancelledPath: String? = null
        val model = AppViewModel(app, sessions) { api(cancelledPath = cancelledPath) }
        withTimeout(15_000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        val before = calls.size
        cancelledPath = "/v1/activity/agent-changes"
        model.refresh()
        withTimeout(15_000) { model.state.first { !it.loading && calls.drop(before).contains(cancelledPath) } }
        assertFalse(calls.drop(before).contains("/v1/briefs/today"))
        assertFalse(calls.drop(before).contains("/v1/tokens"))
        assertNotNull(sessions.current())
    }

    @Test fun unauthorizedBriefsExpiresTheOwningSession() = runBlocking(Dispatchers.IO) {
        assertOptionalMetadataExpiry("/v1/briefs/today")
    }

    @Test fun unauthorizedPrincipalMetadataExpiresTheOwningSession() = runBlocking(Dispatchers.IO) {
        assertOptionalMetadataExpiry("/v1/tokens")
    }

    private suspend fun assertOptionalMetadataExpiry(path: String) {
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        var unauthorizedPath: String? = null
        val model = AppViewModel(app, sessions) { api(unauthorizedPath = unauthorizedPath) }
        withTimeout(15_000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        unauthorizedPath = path
        model.refresh()
        // Clearing credentials and publishing the signed-out UI are separate
        // transitions. Await the flag itself, not an earlier state emission.
        withTimeout(15_000) { model.loggedIn.first { !it } }
        assertNull("optional metadata must not hide an owning 401", sessions.current())
        assertFalse(model.loggedIn.value)
    }

    @Test fun unauthorizedAgentChangesExpiresTheOwningSession() = runBlocking(Dispatchers.IO) {
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        var status = 200
        val model = AppViewModel(app, sessions) { api(agentChangesStatus = status) }
        withTimeout(15_000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        status = 401
        model.refresh()
        withTimeout(15_000) { model.state.first { !model.loggedIn.value || (!it.loading && it.agentChangesError != null) } }
        assertNull("an optional metadata fetch must not swallow owning authentication failure", sessions.current())
        assertFalse(model.loggedIn.value)
    }

    @Test fun reLoginDuringExpiryIsNotSignedOut() = runBlocking(Dispatchers.IO) {
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val expireRefresh = java.util.concurrent.atomic.AtomicBoolean()
        val model = AppViewModel(app, sessions) {
            api(unauthorizedPath = if (expireRefresh.get()) "/v1/changes" else null)
        }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        val fresh = AuthenticatedSession(identity, "fresh-token")
        // Only the explicit refresh fails. A late background front-page load
        // must not consume the re-login hook before the operation under test.
        sessions.onClear = { sessions.value = fresh }
        expireRefresh.set(true)
        model.refresh()
        withTimeout(15000) { model.state.first { it.error != null } }
        assertTrue(model.loggedIn.value)
        assertEquals(fresh, sessions.current())
        assertNull(model.sessionNotice.value)
    }

    @Test fun unauthorizedFlushSignsOutTheExpiredSession() = runBlocking(Dispatchers.IO) {
        val scheduler = TestCoroutineScheduler()
        Dispatchers.setMain(UnconfinedTestDispatcher(scheduler))
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        // Background metadata may finish after initial loading. Only the
        // outbox request should expire this session in a flush-specific test.
        val model = AppViewModel(app, sessions) { api(unauthorizedPath = "/v1/state:batch") }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        val saved = java.util.concurrent.atomic.AtomicInteger()
        model.later(2) { saved.incrementAndGet() }
        withTimeout(15000) { while (saved.get() == 0) delay(10) }
        withTimeout(15000) {
            while (!calls.contains("/v1/state:batch")) {
                scheduler.advanceUntilIdle()
                delay(10)
            }
        }
        withTimeout(15000) { model.loggedIn.first { !it } }
        assertNull(sessions.current())
        assertEquals("Your session expired. Sign in again.", model.sessionNotice.value)
    }

    @Test fun reLoginDuringFlushExpiryIsNotSignedOut() = runBlocking(Dispatchers.IO) {
        val scheduler = TestCoroutineScheduler()
        Dispatchers.setMain(UnconfinedTestDispatcher(scheduler))
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        // Background metadata may finish after initial loading. Only the
        // outbox request should expire this session in a flush-specific test.
        val model = AppViewModel(app, sessions) { api(unauthorizedPath = "/v1/state:batch") }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        val fresh = AuthenticatedSession(identity, "fresh-token")
        // The flush fails with a 401 while the login flow stores a new session.
        sessions.onClear = { sessions.value = fresh }
        val saved = java.util.concurrent.atomic.AtomicInteger()
        model.later(2) { saved.incrementAndGet() }
        withTimeout(15000) { while (saved.get() == 0) delay(10) }
        withTimeout(15000) {
            while (sessions.value != fresh) {
                scheduler.advanceUntilIdle()
                delay(10)
            }
        }
        assertTrue(calls.contains("/v1/state:batch"))
        assertTrue(model.loggedIn.value)
        assertEquals(fresh, sessions.current())
        assertNull(model.sessionNotice.value)
    }

    @Test fun clearedTokenSignsOutOnQueuedAction() = runBlocking(Dispatchers.IO) {
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val model = AppViewModel(app, sessions) { api() }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        sessions.clearToken()
        val saved = java.util.concurrent.atomic.AtomicInteger()
        model.later(2) { saved.incrementAndGet() }
        withTimeout(15000) { model.loggedIn.first { !it } }
        assertEquals("Your session expired. Sign in again.", model.sessionNotice.value)
        assertEquals("dropped swipe still reported success", 0, saved.get())
        assertTrue(SessionCache(app, identity).withEngine { it.snapshot().outbox.isEmpty() })
    }

    @Test fun clearedTokenSignsOutOnEntryLoad() = runBlocking(Dispatchers.IO) {
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val model = AppViewModel(app, sessions) { api() }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        sessions.clearToken()
        assertNull(model.loadEntry(2))
        withTimeout(15000) { model.loggedIn.first { !it } }
        assertFalse(model.loggedIn.value)
        assertEquals("Your session expired. Sign in again.", model.sessionNotice.value)
    }

    @Test fun queuedActionsFlushShortlyAfterTheLastAction() = runBlocking(Dispatchers.IO) {
        val scheduler = TestCoroutineScheduler()
        Dispatchers.setMain(UnconfinedTestDispatcher(scheduler))
        val model = model()
        val saved = java.util.concurrent.atomic.AtomicInteger()
        model.later(2) { saved.incrementAndGet() }
        withTimeout(15000) { while (saved.get() == 0) delay(10) }
        assertFalse("the outbox flushed before the debounce", calls.contains("/v1/state:batch"))
        // queue() reports the save before it schedules the debounced flush on
        // another thread, so keep advancing the clock until the flush runs
        // instead of advancing once and racing the scheduling.
        withTimeout(15000) {
            while (!calls.contains("/v1/state:batch")) {
                scheduler.advanceUntilIdle()
                delay(10)
            }
        }
    }

    @Test @Config(application = GateApplication::class)
    fun flushDoesNotClobberAConcurrentRefreshLoadingFlag() = runBlocking(Dispatchers.IO) {
        val scheduler = TestCoroutineScheduler()
        Dispatchers.setMain(UnconfinedTestDispatcher(scheduler))
        val gateApp = ApplicationProvider.getApplicationContext<GateApplication>()

        withContext(Dispatchers.IO) { SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) } }

        val batchEntered = CompletableDeferred<Unit>()
        val releaseBatch = CompletableDeferred<Unit>()
        val refreshChangesEntered = CompletableDeferred<Unit>()
        val releaseRefresh = CompletableDeferred<Unit>()
        val blockChanges = java.util.concurrent.atomic.AtomicBoolean(false)
        val api = EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
            val path = chain.request().url.encodedPath
            calls += path
            val body = when (path) {
                "/v1/settings" -> """{"today_decay_days":2,"rev":0}"""
                "/v1/changes" -> {
                    if (blockChanges.get()) {
                        refreshChangesEntered.complete(Unit)
                        runBlocking { releaseRefresh.await() }
                    }
                    """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
                }
                "/v1/state:batch" -> {
                    batchEntered.complete(Unit)
                    // Hold the batch response until the gate is armed, so the final
                    // rebuild cannot read the cached changes before the hook exists.
                    runBlocking { releaseBatch.await() }
                    val requestBody = chain.request().body?.let { body ->
                        val buffer = okio.Buffer()
                        body.writeTo(buffer)
                        buffer.readUtf8()
                    }.orEmpty()
                    val opId = Regex("\"op_id\":\"([^\"]+)\"").find(requestBody)?.groupValues?.get(1).orEmpty()
                    """[{"op_id":"$opId","status":"applied","state":{"principal":1,"entry_id":2,"seen":{"value":"unseen","rev":0,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"agent"},"shelf":{"value":"none","rev":0,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"agent"},"kept":{"value":false,"rev":0,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"agent"},"state_rev":0},"replay":false}]"""
                }
                else -> "[]"
            }
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(200).message("test")
                .body(body.toResponseBody()).build()
        }.build())

        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) { api }
        try {
            withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }

            val saved = java.util.concurrent.atomic.AtomicInteger()
            model.later(2) { saved.incrementAndGet() }
            withTimeout(15000) { while (saved.get() == 0) delay(10) }

            // Wait for the debounce to fire and the flush to reach its batch request.
            withTimeout(15000) {
                while (!batchEntered.isCompleted) {
                    scheduler.advanceUntilIdle()
                    delay(10)
                }
            }

            // The flush is past queueing and blocked on its batch response. Arm the
            // gate and inject a small payload only the final rebuild will publish.
            val activity = """{"id":42,"principal_id":9,"principal_name":"Morning Agent","action":"state.updated","target":{"entry_id":2,"register":"shelf"},"before":null,"after":{"shelf":{"value":"done"}},"generation":"g1","created_at":"2026-11-03T00:00:00Z","next_action":"revert"}"""
            SessionCache(app, identity).preferences.edit { putString("agent_changes", "[$activity]") }
            blockChanges.set(true)
            gateApp.agentChangesGate = {
                model.refresh()
                // The flush holds the engine lock here, so refresh sets its loading
                // flag and then waits for the lock instead of publishing first.
                runBlocking { withTimeout(15000) { model.state.first { it.loading } } }
            }
            releaseBatch.complete(Unit)

            // /changes is requested only after the flush published and released the
            // engine lock, so refresh's loading flag must still be set.
            withTimeout(15000) { refreshChangesEntered.await() }
            assertTrue("the flush clobbered refresh's loading flag", model.state.value.loading)
            assertEquals("the flush's rebuilt agent changes were lost", listOf(42L), model.state.value.agentChanges.map { it.id })
        } finally {
            gateApp.agentChangesGate = null
            releaseBatch.complete(Unit)
            releaseRefresh.complete(Unit)
        }

        withTimeout(15000) { model.state.first { !it.loading } }
        Unit
    }

    @Test fun frontPageLoadsAndCachesWithoutWritingReadState() = runBlocking(Dispatchers.IO) {
        frontPageOverride = frontPageJson("rev-1", "daily")
        val model = model()
        await("front page") { model.state.value.frontPage.page?.revision == "rev-1" }
        assertEquals("rev-1", SessionCache(app, identity).cachedFrontPage(FrontPagePeriod.DAILY)?.revision)
        assertNotNull(SessionCache(app, identity).cachedFrontPageAt(FrontPagePeriod.DAILY))
        assertTrue(SessionCache(app, identity).withEngine { it.snapshot().outbox }.isEmpty())
        assertTrue(calls.none { it == "/v1/state:batch" })
        Unit
    }

    @Test fun backgroundRefreshOffersNewCoverageWithoutReplacingTheVisit() = runBlocking(Dispatchers.IO) {
        frontPageOverride = frontPageJson("rev-1", "daily")
        val model = model()
        await("front page") { model.state.value.frontPage.page?.revision == "rev-1" }
        val orderBefore = model.state.value.frontPage.page?.sections?.map { section -> section.stories.map { it.id } }

        frontPageOverride = frontPageJson("rev-2", "daily")
        model.refresh()
        await("pending revision") { model.state.value.frontPage.pendingRevision == "rev-2" }
        assertEquals("rev-1", model.state.value.frontPage.page?.revision)
        assertEquals(orderBefore, model.state.value.frontPage.page?.sections?.map { section -> section.stories.map { it.id } })

        model.refreshFrontPage()
        await("explicit refresh") { model.state.value.frontPage.page?.revision == "rev-2" }
        assertNull(model.state.value.frontPage.pendingRevision)
        Unit
    }

    @Test fun pendingCoverageCanBeAppliedAfterTheNetworkDrops() = runBlocking(Dispatchers.IO) {
        frontPageOverride = frontPageJson("rev-1", "daily")
        val model = model()
        await("front page") { model.state.value.frontPage.page?.revision == "rev-1" }
        frontPageOverride = frontPageJson("rev-2", "daily")
        model.refresh()
        await("pending coverage") { model.state.value.frontPage.pendingRevision == "rev-2" }
        val requests = calls.count { it == "/v1/front-page" }
        frontPageStatus = 503
        model.refreshFrontPage()
        await("applied coverage") { !model.state.value.frontPage.loading && model.state.value.frontPage.pendingRevision == null }
        assertEquals("rev-2", model.state.value.frontPage.page?.revision)
        assertEquals(requests, calls.count { it == "/v1/front-page" })
        assertTrue(SessionCache(app, identity).withEngine { it.snapshot().outbox }.isEmpty())
        Unit
    }

    @Test fun applyingAnOfferPreservesCoverageAlreadyInFlight() = runBlocking(Dispatchers.IO) {
        frontPageOverride = frontPageJson("rev-1", "daily")
        val model = model()
        await("front page") { model.state.value.frontPage.page?.revision == "rev-1" }
        frontPageOverride = frontPageJson("rev-2", "daily")
        model.refresh()
        await("first offer") { model.state.value.frontPage.pendingRevision == "rev-2" }
        val entered = java.util.concurrent.CountDownLatch(1)
        val release = java.util.concurrent.CountDownLatch(1)
        frontPageOverride = frontPageJson("rev-3", "daily")
        frontPageGate = { entered.countDown(); check(release.await(10, java.util.concurrent.TimeUnit.SECONDS)) }
        try {
            model.refresh()
            assertTrue(entered.await(5, java.util.concurrent.TimeUnit.SECONDS))
            model.refreshFrontPage()
            assertEquals("rev-2", model.state.value.frontPage.page?.revision)
        } finally { release.countDown(); frontPageGate = null }
        await("newer offer") { model.state.value.frontPage.pendingRevision == "rev-3" }
        assertEquals("rev-2", model.state.value.frontPage.page?.revision)
        Unit
    }

    @Test fun unchangedCoverageRefreshesItsReadIndicator() = runBlocking(Dispatchers.IO) {
        frontPageOverride = frontPageJson("rev-1", "daily")
        val model = model()
        await("front page") { model.state.value.frontPage.page?.revision == "rev-1" }
        frontPageOverride = frontPageJson("rev-1", "daily").replace("\"unseen\"", "\"seen\"")
        model.refresh()
        val cache = SessionCache(app, identity)
        await("updated read indicator") {
            model.state.value.frontPage.page?.sections?.first()?.stories?.first()
                ?.coverage?.first()?.entry?.state?.seen?.value == JsonPrimitive("seen")
        }
        assertEquals(JsonPrimitive("seen"), model.state.value.frontPage.page?.sections?.first()?.stories?.first()
            ?.coverage?.first()?.entry?.state?.seen?.value)
        assertNull(model.state.value.frontPage.pendingRevision)
        assertTrue(cache.withEngine { it.snapshot().outbox }.isEmpty())
        Unit
    }

    @Test fun frontPageOfflineKeepsTheCachedPageAndTime() = runBlocking(Dispatchers.IO) {
        frontPageOverride = frontPageJson("rev-1", "daily")
        val model = model()
        await("front page") { model.state.value.frontPage.page?.revision == "rev-1" }
        val updatedAt = model.state.value.frontPage.updatedAt
        assertNotNull(updatedAt)

        frontPageStatus = 500
        model.refreshFrontPage()
        await("offline notice") { model.state.value.frontPage.error == "Offline. Showing the last updated page." }
        assertEquals("rev-1", model.state.value.frontPage.page?.revision)
        assertEquals(updatedAt, model.state.value.frontPage.updatedAt)

        frontPageStatus = 200
        model.refreshFrontPage()
        await("online again") { model.state.value.frontPage.error == null }
        assertEquals("rev-1", model.state.value.frontPage.page?.revision)
        Unit
    }

    @Test fun logoutClearsTheFrontPageCache() = runBlocking(Dispatchers.IO) {
        frontPageOverride = frontPageJson("rev-1", "daily")
        val model = model()
        await("front page") { model.state.value.frontPage.page?.revision == "rev-1" }
        model.logout()
        assertNull(SessionCache(app, identity).cachedFrontPage(FrontPagePeriod.DAILY))
        assertNull(model.state.value.frontPage.page)
        Unit
    }

    @Test fun switchingFrontPagePeriodsLoadsTheOtherWindow() = runBlocking(Dispatchers.IO) {
        frontPageOverride = frontPageJson("rev-1", "daily")
        val model = model()
        await("daily front page") { model.state.value.frontPage.page?.revision == "rev-1" }

        frontPageOverride = frontPageJson("rev-week", "weekly")
        model.openFrontPage(FrontPagePeriod.WEEKLY)
        await("weekly front page") { model.state.value.frontPage.page?.revision == "rev-week" }
        assertEquals(FrontPagePeriod.WEEKLY, model.state.value.frontPage.period)
        assertEquals("rev-week", SessionCache(app, identity).cachedFrontPage(FrontPagePeriod.WEEKLY)?.revision)
        assertTrue(SessionCache(app, identity).withEngine { it.snapshot().outbox }.isEmpty())
        Unit
    }
}

/**
 * Test application that gates the flush's agent-changes read. The final rebuild
 * is the only reader after the test arms [agentChangesGate]; the one-shot
 * callback lets the test interleave refresh's loading flag.
 */
class GateApplication : Application() {
    @Volatile var agentChangesGate: (() -> Unit)? = null

    override fun getSharedPreferences(name: String, mode: Int): SharedPreferences {
        val delegate = super.getSharedPreferences(name, mode)
        val app = this
        return object : SharedPreferences by delegate {
            override fun getString(key: String?, defValue: String?): String? {
                if (key == "agent_changes") {
                    val gate = app.agentChangesGate
                    app.agentChangesGate = null
                    gate?.invoke()
                }
                return delegate.getString(key, defValue)
            }
        }
    }
}

package ch.lkmc.earnest

import android.app.Application
import androidx.core.content.edit
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.*
import kotlinx.coroutines.*
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.coroutines.flow.first
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
    private val contentUrlQueries = CopyOnWriteArrayList<String?>()
    private class Sessions(@Volatile var value: AuthenticatedSession?) : SessionStore {
        @Volatile var onRead: (() -> Unit)? = null
        override fun current(): AuthenticatedSession? {
            val callback = onRead
            onRead = null
            callback?.invoke()
            return value
        }
        override fun clearToken() { value = null }
    }
    @Before fun setup() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
    }
    @After fun teardown() { Dispatchers.resetMain() }

    private fun api() = EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
        val path = chain.request().url.encodedPath
        calls += path
        if (path.endsWith("/content-url")) contentUrlQueries += chain.request().url.query
        val body = when (path) {
            "/v1/entries/2/content-url" -> """{"url":"https://content.test/signed","expires_at":"2026-11-03T01:00:00Z"}"""
            "/v1/settings" -> """{"today_decay_days":2,"rev":0}"""
            "/v1/changes" -> """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
            "/v1/activity/agent-changes" -> """[{"id":7,"principal_id":9,"principal_name":"Morning Agent","action":"state.updated","target":{"entry_id":2,"register":"shelf"},"before":null,"after":{"shelf":{"value":"done"}},"generation":"g1","created_at":"${java.time.Instant.now()}","next_action":"revert"}]"""
            "/v1/entries/2" -> """{"id":2,"subscription_id":3,"feed_id":5,"seq":4,"first_seen_at":"2026-11-03T00:00:00Z","title":"Notification headline","url":"https://news.test/article","author":"Author","rev":2,"deleted":false}"""
            else -> "[]"
        }
        Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(200).message("test")
            .body(body.toResponseBody()).build()
    }.build())

    private suspend fun model(): AppViewModel {
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) { api() }
        withTimeout(15000) { model.state.first { !it.loading && calls.contains("/v1/tokens") } }
        return model
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
        SessionCache(app, identity).db.snapshot().save(ch.lkmc.earnest.db.SyncSnapshotEntity(json = "invalid snapshot"))
        val saved = java.util.concurrent.atomic.AtomicInteger()
        model.later(2) { saved.incrementAndGet() }
        withTimeout(15000) { model.state.first { it.error?.startsWith("Could not save the action") == true } }
        assertEquals(0, saved.get())
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

    @Test fun notificationMissingFromSnapshotFetchesMetadata() = runBlocking(Dispatchers.IO) {
        val model = model()
        val entry = model.loadEntry(2)
        assertNotNull("notification entry was absent from cache", entry)
        assertEquals("Notification headline", entry?.title)
        assertEquals("Notification headline", model.state.value.snapshot.entries.single().title)
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

    @Test fun queuedActionsFlushShortlyAfterTheLastAction() = runBlocking(Dispatchers.IO) {
        val scheduler = TestCoroutineScheduler()
        Dispatchers.setMain(UnconfinedTestDispatcher(scheduler))
        val model = model()
        val saved = java.util.concurrent.atomic.AtomicInteger()
        model.later(2) { saved.incrementAndGet() }
        withTimeout(15000) { while (saved.get() == 0) delay(10) }
        assertFalse("the outbox flushed before the debounce", calls.contains("/v1/state:batch"))
        scheduler.advanceUntilIdle()
        withTimeout(15000) { while (!calls.contains("/v1/state:batch")) delay(10) }
    }

    @Test fun flushDoesNotClobberAConcurrentRefreshLoadingFlag() = runBlocking(Dispatchers.IO) {
        val scheduler = TestCoroutineScheduler()
        Dispatchers.setMain(UnconfinedTestDispatcher(scheduler))

        withContext(Dispatchers.IO) { SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) } }

        val batchEntered = CompletableDeferred<Unit>()
        val refreshChangesEntered = CompletableDeferred<Unit>()
        val releaseRefresh = CompletableDeferred<Unit>()
        val blockRefresh = java.util.concurrent.atomic.AtomicBoolean(false)
        val api = EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
            val path = chain.request().url.encodedPath
            calls += path
            val body = when (path) {
                "/v1/settings" -> """{"today_decay_days":2,"rev":0}"""
                "/v1/changes" -> {
                    if (blockRefresh.get()) {
                        refreshChangesEntered.complete(Unit)
                        runBlocking { releaseRefresh.await() }
                    }
                    """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
                }
                "/v1/state:batch" -> {
                    batchEntered.complete(Unit)
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

            // A large agent-changes payload makes the flush's state rebuild slow enough to interleave.
            val activity = """{"id":1,"principal_id":9,"principal_name":"Morning Agent","action":"state.updated","target":{"entry_id":2,"register":"shelf"},"before":null,"after":{"shelf":{"value":"done"}},"generation":"g1","created_at":"2026-11-03T00:00:00Z","next_action":"revert"}"""
            val changes = (1..50000).joinToString(prefix = "[", postfix = "]", separator = ",") { activity }
            SessionCache(app, identity).preferences.edit { putString("agent_changes", changes) }

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

            // Let the flush read the state and start its slow rebuild, then refresh.
            delay(50)
            blockRefresh.set(true)
            model.refresh()
            withTimeout(15000) { refreshChangesEntered.await() }
            assertTrue("the flush clobbered refresh's loading flag", model.state.value.loading)
        } finally {
            releaseRefresh.complete(Unit)
        }

        withTimeout(15000) { model.state.first { !it.loading } }
        Unit
    }
}

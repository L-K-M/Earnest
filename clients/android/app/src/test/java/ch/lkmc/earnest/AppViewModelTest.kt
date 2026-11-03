package ch.lkmc.earnest

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.first
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
@SQLiteMode(SQLiteMode.Mode.LEGACY)
class AppViewModelTest {
    private val app get() = ApplicationProvider.getApplicationContext<Application>()
    private val identity = AccountIdentity("https://nas.test", 1, 1)
    private val calls = CopyOnWriteArrayList<String>()
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
        val body = when (path) {
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
}

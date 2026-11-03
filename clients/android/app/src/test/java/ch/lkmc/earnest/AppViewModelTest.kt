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
    private class Sessions(var value: AuthenticatedSession?) : SessionStore {
        override fun current() = value
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
}

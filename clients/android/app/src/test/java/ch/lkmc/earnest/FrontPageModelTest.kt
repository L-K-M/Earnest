package ch.lkmc.earnest

import android.app.Application
import androidx.lifecycle.ViewModelStore
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
import java.util.concurrent.atomic.AtomicInteger

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@SQLiteMode(SQLiteMode.Mode.NATIVE)
class FrontPageModelTest {
    private val app get() = ApplicationProvider.getApplicationContext<Application>()
    private val identity = AccountIdentity("https://front.test", 1, 1)
    private val revision = AtomicInteger(1)
    private val store = ViewModelStore()

    @Before fun setup() { Dispatchers.setMain(UnconfinedTestDispatcher()) }
    @After fun teardown() { store.clear(); Dispatchers.resetMain() }

    private suspend fun model(): AppViewModel {
        val cache = SessionCache(app, identity)
        cache.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val sessions = object : SessionStore {
            var value: AuthenticatedSession? = AuthenticatedSession(identity, "token")
            override fun current() = value
            override fun clearToken() { value = null }
            override fun clearToken(expected: AuthenticatedSession): Boolean {
                if (value != expected) return false
                value = null
                return true
            }
        }
        val client = OkHttpClient.Builder().addInterceptor { chain ->
            val url = chain.request().url
            val period = url.queryParameter("period") ?: "daily"
            val body = when (url.encodedPath) {
                "/v1/front-page" -> """{"period":"$period","window_start":"2026-11-11T10:00:00Z","window_end":"2026-11-12T10:00:00Z","selected_at":"2026-11-12T10:00:00Z","revision":"$period-${revision.get()}","sections":[]}"""
                "/v1/settings" -> """{"today_decay_days":2,"rev":0}"""
                "/v1/changes" -> """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
                else -> "[]"
            }
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(200).message("test")
                .body(body.toResponseBody()).build()
        }.build()
        val model = AppViewModel(app, sessions) { EarnestApi(identity.server, "token", client) }
        store.put("front", model)
        withTimeout(15_000) { model.state.first { !it.loading } }
        return model
    }

    @Test fun firstResponseAndExplicitRefreshPublishWithoutReadingAnything() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.openFrontPage(FrontPagePeriod.DAILY)
        val first = withTimeout(5_000) { model.state.first { it.frontPage.page != null } }
        assertEquals("daily-1", first.frontPage.page?.revision)
        revision.set(2)
        model.refreshFrontPage()
        val refreshed = withTimeout(5_000) { model.state.first { it.frontPage.page?.revision == "daily-2" } }
        assertFalse(refreshed.frontPage.loading)
        assertNull(refreshed.frontPage.pendingRevision)
        assertTrue(SessionCache(app, identity).withEngine { it.snapshot().outbox.isEmpty() })
    }

    @Test fun switchingWindowsReplacesTheVisiblePage() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.openFrontPage(FrontPagePeriod.DAILY)
        withTimeout(5_000) { model.state.first { it.frontPage.page != null } }
        model.openFrontPage(FrontPagePeriod.WEEKLY)
        val weekly = withTimeout(5_000) { model.state.first { it.frontPage.page?.period == FrontPagePeriod.WEEKLY } }
        assertEquals("weekly-1", weekly.frontPage.page?.revision)
        assertEquals(FrontPagePeriod.WEEKLY, weekly.frontPage.period)
        assertTrue(SessionCache(app, identity).withEngine { it.snapshot().outbox.isEmpty() })
    }
}

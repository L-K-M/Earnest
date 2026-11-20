package ch.lkmc.earnest

import android.app.Application
import androidx.activity.OnBackPressedDispatcher
import androidx.activity.compose.LocalOnBackPressedDispatcherOwner
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.SyncEngine
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.annotation.SQLiteMode
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/**
 * Rotating the device, or Android restoring the app, recreates the screen and
 * reloads the open article. Only opening an article marks it read, so that
 * reload must leave an article you marked unread unread. These tests run the
 * real screen and view model and read the stored state.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w360dp-h640dp")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@SQLiteMode(SQLiteMode.Mode.NATIVE)
class RotationReadStateTest {
    @get:Rule val compose = createComposeRule()
    private val app get() = ApplicationProvider.getApplicationContext<Application>()
    private val identity = AccountIdentity("https://nas.test", 1, 1)
    private val paths = CopyOnWriteArrayList<String>()

    private class Sessions(initial: AuthenticatedSession) : SessionStore {
        private val stored = AtomicReference<AuthenticatedSession?>(initial)
        override fun current() = stored.get()
        override fun clearToken() { stored.set(null) }
        override fun clearToken(expected: AuthenticatedSession) = stored.compareAndSet(expected, null)
    }

    private fun api() = EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
        val request = chain.request()
        val path = request.url.encodedPath
        paths += path
        val body = when (path) {
            "/v1/settings" -> """{"today_decay_days":2,"rev":0}"""
            "/v1/changes" -> """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
            "/v1/front-page" -> FRONT_PAGE
            "/v1/entries/$ENTRY" -> ENTRY_JSON
            "/v1/subscriptions/3" -> SUBSCRIPTION
            "/v1/entries/$ENTRY/content" -> """{"entry_id":$ENTRY,"html":"<p>Article body</p>","text":"Article body","sanitizer_version":1,"source":"feed"}"""
            "/v1/notifications/$NOTIFICATION" -> """{"id":$NOTIFICATION,"principal_id":1,"muted":false,"note":"","entry_ids":[$ENTRY],"category":"digest","status":"sent","budget":null,"why":null,"generation":"g1","created_at":"2026-11-03T00:00:00Z","replay":false}"""
            else -> "[]"
        }
        // Without a signed URL the reader shows the stored copy.
        val status = if (path.endsWith("/content-url")) 404 else 200
        Response.Builder().request(request).protocol(Protocol.HTTP_1_1).code(status).message("test")
            .body(body.toResponseBody()).build()
    }.build())

    /** A signed-in model whose account cache is still restoring, as on a cold start. */
    private fun model(): AppViewModel {
        runBlocking(Dispatchers.IO) { SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) } }
        return AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) { api() }
    }

    private fun AppViewModel.awaitAccount() = compose.waitUntil(TIMEOUT_MS) { state.value.accountKey != null }

    @Composable
    private fun Screen(model: AppViewModel, initialEntry: Long? = null, notificationId: Long? = null, onBack: (OnBackPressedDispatcher?) -> Unit = {}) {
        onBack(LocalOnBackPressedDispatcherOwner.current?.onBackPressedDispatcher)
        EarnestTheme(ThemeSetting.LIGHT) {
            val state by model.state.collectAsState()
            MainScaffold(model, state, ThemeSetting.LIGHT, {}, initialEntry, notificationId, false, {})
        }
    }

    private fun seen(): Boolean = runBlocking(Dispatchers.IO) {
        SessionCache(app, identity).withEngine { it.state(1, ENTRY) }.seen.value == JsonPrimitive(SyncEngine.VALUE_SEEN)
    }

    // waitUntil alone does not run the main looper, so a state change made
    // in runOnIdle would never reach the screen; settle it on every poll.
    private fun awaitSeen() = compose.waitUntil(TIMEOUT_MS) { compose.waitForIdle(); seen() }

    private fun articleLoads() = paths.count { it == "/v1/entries/$ENTRY/content" }

    private fun markUnread(model: AppViewModel) {
        val saved = AtomicBoolean()
        model.markUnread(ENTRY) { saved.set(true) }
        compose.waitUntil(TIMEOUT_MS) { saved.get() }
        assertFalse("mark unread must have been saved", seen())
    }

    /**
     * Recreates the screen and, when [reloads] says the article is open,
     * waits for the reader to load it again. A stray read mark is queued
     * before that load finishes, so the article must stay unread a while.
     */
    private fun recreateAndExpectUnread(restoration: StateRestorationTester, reloads: Boolean, why: String) {
        val loads = articleLoads()
        restoration.emulateSavedInstanceStateRestore()
        if (reloads) compose.waitUntil(TIMEOUT_MS) { articleLoads() > loads } else compose.waitForIdle()
        val deadline = System.currentTimeMillis() + SETTLE_MS
        while (System.currentTimeMillis() < deadline) {
            compose.waitForIdle()
            assertFalse(why, seen())
            Thread.sleep(50)
        }
        if (!reloads) assertEquals("$why: the article must not open again", loads, articleLoads())
    }

    @Test fun anArticleMarkedUnreadInTheReaderStaysUnreadAfterRotation() {
        val model = model()
        val restoration = StateRestorationTester(compose)
        restoration.setContent { Screen(model) }
        model.awaitAccount()
        compose.waitUntil(TIMEOUT_MS) { compose.onAllNodesWithText(TITLE).fetchSemanticsNodes().isNotEmpty() }
        compose.onAllNodesWithText(TITLE).onFirst().performClick()
        awaitSeen()

        markUnread(model)
        recreateAndExpectUnread(restoration, reloads = true, "rotating must not read the open article again")
    }

    @Test fun anArticleMarkedUnreadInReadInFeedStaysUnreadUntilYouExpandItAgain() {
        app.setArticleLayout(ArticleLayout.READING)
        val model = model()
        val restoration = StateRestorationTester(compose)
        restoration.setContent { Screen(model) }
        model.awaitAccount()
        // The article is cached, as after a sync.
        runBlocking(Dispatchers.IO) { model.loadEntry(ENTRY) }
        compose.onNodeWithText("Articles").performClick()
        compose.onNodeWithText(TITLE).performClick()
        compose.onNodeWithText("Collapse").assertExists()
        awaitSeen()

        markUnread(model)
        recreateAndExpectUnread(restoration, reloads = true, "rotating must not read the expanded article again")

        compose.onNodeWithText("Collapse").performScrollTo().performClick()
        compose.onNodeWithText(TITLE).performClick()
        awaitSeen()
    }

    @Test fun aLinkOpensItsArticleOnceAcrossRotations() {
        val model = model()
        var back: OnBackPressedDispatcher? = null
        var link by mutableStateOf<Long?>(ENTRY)
        val restoration = StateRestorationTester(compose)
        // The link arrives before the account cache has restored.
        restoration.setContent { Screen(model, initialEntry = link, onBack = { back = it }) }
        awaitSeen()
        assertFalse(model.state.value.error.orEmpty(), model.state.value.error.orEmpty().startsWith("Still signing in"))

        markUnread(model)
        recreateAndExpectUnread(restoration, reloads = true, "rotating must not read the linked article again")

        compose.runOnIdle { back!!.onBackPressed() }
        recreateAndExpectUnread(restoration, reloads = false, "rotating after leaving must not reopen the link")

        // A later intent without the link, such as a notification, then the
        // same link again: that is a new open.
        compose.runOnIdle { link = null }
        compose.runOnIdle { link = ENTRY }
        awaitSeen()
    }

    @Test fun aNotificationOpensItsArticleOnceAcrossRotations() {
        val model = model()
        val restoration = StateRestorationTester(compose)
        restoration.setContent { Screen(model, notificationId = NOTIFICATION) }
        awaitSeen()

        markUnread(model)
        recreateAndExpectUnread(restoration, reloads = true, "rotating must not read the notified article again")
    }

    private companion object {
        const val TIMEOUT_MS = 15_000L
        const val SETTLE_MS = 1_500L
        const val ENTRY = 2L
        const val NOTIFICATION = 9L
        const val TITLE = "A headline worth reading"
        const val ENTRY_JSON = """{"id":$ENTRY,"subscription_id":3,"feed_id":5,"seq":4,"first_seen_at":"2026-11-03T00:00:00Z","title":"$TITLE","url":"https://news.test/article","author":"Author","rev":2,"deleted":false}"""
        const val SUBSCRIPTION = """{"id":3,"feed_id":5,"title":"Feed","url":"https://feed.test/rss","site_url":"https://feed.test","folder_id":0,"cadence":"today","extract":"none","rev":1,"deleted":false,"private":false,"muted":false,"initial_through_seq":0,"initial_fetch_pending":false,"health":{"next_fetch_at":"2026-11-10T00:00:00Z","consecutive_failures":0,"http_status":200,"posts_per_day":1.0,"firehose":false}}"""
        const val STATE = """{"principal":1,"entry_id":$ENTRY,"seen":{"value":"unseen","rev":1,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"passive"},"shelf":{"value":"none","rev":1,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"passive"},"kept":{"value":false,"rev":1,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"passive"},"state_rev":1}"""
        const val FRONT_ENTRY = """{"id":$ENTRY,"subscription_id":3,"feed_id":5,"seq":4,"first_seen_at":"2026-11-03T00:00:00Z","published_at":"2026-11-02T12:00:00Z","title":"$TITLE","url":"https://news.test/article","author":"Author","rev":2,"deleted":false,"state":$STATE}"""
        const val FRONT_PAGE = """{"period":"daily","window_start":"2026-11-02T10:00:00Z","window_end":"2026-11-03T10:00:00Z","selected_at":"2026-11-03T10:00:00Z","revision":"rev-1","sections":[{"name":"Across your feeds","stories":[{"id":"story-1","entry":$FRONT_ENTRY,"source_title":"Source A","published_at":"2026-11-02T12:00:00Z","feed_count":1,"excerpt":"Excerpt","coverage":[{"entry":$FRONT_ENTRY,"source_title":"Source A"}]}]}]}"""
    }
}

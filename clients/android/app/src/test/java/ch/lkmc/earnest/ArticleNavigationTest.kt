package ch.lkmc.earnest

import android.app.Application
import androidx.activity.OnBackPressedDispatcher
import androidx.activity.compose.LocalOnBackPressedDispatcherOwner
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeLeft
import androidx.compose.ui.test.swipeRight
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
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.annotation.SQLiteMode
import java.util.concurrent.CopyOnWriteArrayList

/** Sideways swipes in the reader move through the list the article was opened from. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w360dp-h640dp")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@SQLiteMode(SQLiteMode.Mode.NATIVE)
class ArticleNavigationTest {
    @get:Rule val compose = createComposeRule()
    private val app get() = ApplicationProvider.getApplicationContext<Application>()
    private val identity = AccountIdentity("https://nas.test", 1, 1)
    private val requests = CopyOnWriteArrayList<String>()
    private var back: OnBackPressedDispatcher? = null
    private lateinit var model: AppViewModel

    private class Sessions(private var stored: AuthenticatedSession?) : SessionStore {
        override fun current() = stored
        override fun clearToken() { stored = null }
        override fun clearToken(expected: AuthenticatedSession) = (stored == expected).also { if (it) stored = null }
    }

    private fun api() = EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
        val request = chain.request()
        val path = request.url.encodedPath
        requests += "$path?${request.url.encodedQuery.orEmpty()}"
        val id = Regex("/v1/entries/(\\d+)").find(path)?.groupValues?.get(1)?.toLong()
        val body = when {
            path == "/v1/settings" -> """{"today_decay_days":2,"rev":0}"""
            path == "/v1/changes" -> """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
            path == "/v1/front-page" -> frontPage()
            path == "/v1/subscriptions/3" -> SUBSCRIPTION
            path.endsWith("/content") -> """{"entry_id":$id,"html":"<p>Body $id</p>","text":"Body $id","sanitizer_version":1,"source":"feed"}"""
            id != null && path == "/v1/entries/$id" -> entryJson(id)
            else -> "[]"
        }
        val status = if (path.endsWith("/content-url")) 404 else 200
        Response.Builder().request(request).protocol(Protocol.HTTP_1_1).code(status).message("test")
            .body(body.toResponseBody()).build()
    }.build())

    @Before fun start() {
        app.setArticleLayout(ArticleLayout.HEADLINES)
        runBlocking(Dispatchers.IO) { SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) } }
        model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) { api() }
    }

    private fun show(
        restoration: StateRestorationTester? = null,
        onRegistry: (androidx.compose.runtime.saveable.SaveableStateRegistry) -> Unit = {},
    ) {
        val content = @androidx.compose.runtime.Composable {
            androidx.compose.runtime.saveable.LocalSaveableStateRegistry.current?.let(onRegistry)
            back = LocalOnBackPressedDispatcherOwner.current?.onBackPressedDispatcher
            EarnestTheme(ThemeSetting.LIGHT) {
                val state by model.state.collectAsState()
                MainScaffold(model, state, ThemeSetting.LIGHT, {}, null, null, 0, false, {})
            }
        }
        if (restoration != null) restoration.setContent(content) else compose.setContent(content)
        await { model.state.value.accountKey != null }
        runBlocking(Dispatchers.IO) { ENTRIES.forEach { model.loadEntry(it) } }
        compose.waitForIdle()
    }

    private fun await(condition: () -> Boolean) = compose.waitUntil(TIMEOUT_MS) { compose.waitForIdle(); condition() }

    private fun seen(id: Long) = runBlocking(Dispatchers.IO) {
        SessionCache(app, identity).withEngine { it.state(1, id) }.seen.value == JsonPrimitive(SyncEngine.VALUE_SEEN)
    }

    private fun awaitArticle(id: Long) {
        await { seen(id) && compose.onAllNodesWithText(title(id)).fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("Article").assertExists()
        ENTRIES.filter { it != id }.forEach { compose.onNodeWithText(title(it)).assertDoesNotExist() }
    }

    private fun openFromArticles(id: Long) {
        compose.onNodeWithText("Articles").performClick()
        compose.onNodeWithText(title(id)).performClick()
        awaitArticle(id)
    }

    private fun swipeToNext() = compose.onNode(hasText("Article headline", substring = true)).performTouchInput { swipeLeft() }
    private fun swipeToPrevious() = compose.onNode(hasText("Article headline", substring = true)).performTouchInput { swipeRight() }

    @Test fun swipingSidewaysMovesThroughTheListTheArticleCameFrom() {
        show()
        openFromArticles(4)
        swipeToNext()
        awaitArticle(3)
        swipeToNext()
        awaitArticle(2)
        swipeToPrevious()
        awaitArticle(3)
        assertFalse("an article nobody opened must stay unread", seen(1))
    }

    @Test fun backAfterSwipingReturnsStraightToTheList() {
        show()
        openFromArticles(4)
        swipeToNext()
        awaitArticle(3)
        swipeToNext()
        awaitArticle(2)
        compose.runOnIdle { checkNotNull(back).onBackPressed() }
        await { compose.onAllNodesWithText("Unread only").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("Article").assertDoesNotExist()
        compose.onNodeWithText(title(4)).assertExists()
    }

    @Test fun leavingAnArticleMarkedUnreadBySwipeKeepsItUnread() {
        show()
        openFromArticles(4)
        val saved = java.util.concurrent.atomic.AtomicBoolean()
        model.markUnread(4) { saved.set(true) }
        await { saved.get() }
        swipeToNext()
        awaitArticle(3)
        assertFalse("turning the page is not reopening the article left behind", seen(4))
    }

    @Test fun aRunOfSwipesDoesNotGrowTheSavedState() {
        lateinit var registry: androidx.compose.runtime.saveable.SaveableStateRegistry
        show(onRegistry = { registry = it })
        openFromArticles(4)
        swipeToNext()
        awaitArticle(3)
        fun savedSize() = compose.runOnIdle { registry.performSave().toString().length }
        val before = savedSize()
        repeat(6) {
            swipeToPrevious()
            awaitArticle(4)
            swipeToNext()
            awaitArticle(3)
        }
        val after = savedSize()
        assertTrue("twelve swipes grew the saved state from $before to $after", after - before < 64)
    }

    @Test fun swipingPastTheEndStaysOnTheArticle() {
        show()
        openFromArticles(1)
        swipeToNext()
        awaitArticle(1)
        assertFalse(seen(2))
    }

    @Test fun readArticlesStayInTheSequenceOfAnUnreadOnlyList() {
        show()
        compose.onNodeWithText("Articles").performClick()
        compose.onNodeWithText("Unread only").performClick()
        compose.onNodeWithText(title(4)).performClick()
        awaitArticle(4)
        swipeToNext()
        awaitArticle(3)
        swipeToPrevious()
        awaitArticle(4)
    }

    @Test fun theSequenceSurvivesRecreation() {
        val restoration = StateRestorationTester(compose)
        show(restoration)
        openFromArticles(4)
        restoration.emulateSavedInstanceStateRestore()
        awaitArticle(4)
        swipeToNext()
        awaitArticle(3)
        restoration.emulateSavedInstanceStateRestore()
        awaitArticle(3)
        swipeToNext()
        awaitArticle(2)
    }

    @Test fun frontPageArticlesFollowItsStoryOrder() {
        show()
        await { compose.onAllNodesWithText(title(2)).fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText(title(2)).performClick()
        awaitArticle(2)
        swipeToNext()
        awaitArticle(4)
    }

    @Test fun neighboursFromAListLoadLikeATappedArticle() {
        show()
        openFromArticles(4)
        swipeToNext()
        awaitArticle(3)
        await { requests.any { it.startsWith("/v1/entries/3/content-url") } }
        assertTrue(requests.filter { it.startsWith("/v1/entries/3/content-url") }.none { "click" in it })
    }

    private companion object {
        const val TIMEOUT_MS = 15_000L
        val ENTRIES = listOf(4L, 3L, 2L, 1L)
        fun title(id: Long) = "Article headline $id"
        const val SUBSCRIPTION = """{"id":3,"feed_id":5,"title":"Feed","url":"https://feed.test/rss","site_url":"https://feed.test","folder_id":0,"cadence":"today","extract":"none","rev":1,"deleted":false,"private":false,"muted":false,"initial_through_seq":0,"initial_fetch_pending":false,"health":{"next_fetch_at":"2026-11-10T00:00:00Z","consecutive_failures":0,"http_status":200,"posts_per_day":1.0,"firehose":false}}"""
        fun entryJson(id: Long) = """{"id":$id,"subscription_id":3,"feed_id":5,"seq":$id,"first_seen_at":"2026-11-03T00:00:00Z","title":"${title(id)}","url":"https://news.test/$id","author":"Author","rev":2,"deleted":false}"""

        fun story(id: Long): String {
            val state = """{"principal":1,"entry_id":$id,"seen":{"value":"unseen","rev":1,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"passive"},"shelf":{"value":"none","rev":1,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"passive"},"kept":{"value":false,"rev":1,"occurred_at":"2026-11-03T00:00:00Z","actor":1,"source":"passive"},"state_rev":1}"""
            val entry = entryJson(id).dropLast(1) + ""","published_at":"2026-11-02T12:00:00Z","state":$state}"""
            return """{"id":"story-$id","entry":$entry,"source_title":"Source","published_at":"2026-11-02T12:00:00Z","feed_count":1,"excerpt":"","coverage":[{"entry":$entry,"source_title":"Source"}]}"""
        }

        fun frontPage() = """{"period":"daily","window_start":"2026-11-02T10:00:00Z","window_end":"2026-11-03T10:00:00Z","selected_at":"2026-11-03T10:00:00Z","revision":"rev-1","sections":[{"name":"Across your feeds","stories":[${story(2)},${story(4)}]}]}"""
    }
}

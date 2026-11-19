package ch.lkmc.earnest

import android.content.Context
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.material3.Text
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeDown
import androidx.compose.ui.test.swipeUp
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.SyncEngine
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * Only opening an article marks it read (docs/research/decisions.md,
 * 2026-11-12). Scrolling a list down and back up must leave every row's
 * read state alone, in both list layouts.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w360dp-h640dp")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ScrollReadStateTest {
    @get:Rule val compose = createComposeRule()
    private val context get() = ApplicationProvider.getApplicationContext<Context>()
    private val entries = (1L..40L).map { CachedEntry(it, title = "Headline $it") }
    private val states = entries.associate { it.id to SyncEngine.initialState(1, it.id) }
    private val listState = LazyListState()
    // Every callback that can change an article's state or open it.
    private val actions = mutableListOf<String>()

    @Before fun resetPreference() { context.getSharedPreferences("appearance", Context.MODE_PRIVATE).edit().clear().commit() }
    @After fun clearPreference() { context.getSharedPreferences("appearance", Context.MODE_PRIVATE).edit().clear().commit() }

    private fun showList(layout: ArticleLayout) {
        compose.setContent {
            EarnestTheme(ThemeSetting.DARK) {
                EntryListScreen(
                    title = "All articles", entries = entries, states = states, principalId = 1,
                    subscriptionTitles = emptyMap(), subscriptionIcons = emptyMap(), briefs = emptyList(),
                    principalNames = emptyMap(), entryTitles = emptyMap(), agentChanges = emptyList(),
                    sessionChanges = emptyList(), agentChangesError = null, onRevert = { actions += "revert $it" },
                    onOpen = { actions += "open $it" }, onOpenAgentSelected = { actions += "open $it" },
                    onLater = { actions += "later $it" }, onDone = { actions += "done $it" },
                    onKeep = { actions += "keep $it" }, onUnkeep = { actions += "unkeep $it" },
                    onMarkUnread = { actions += "unread $it" },
                    accountKey = "scroll-read", layoutOverride = layout, listState = listState,
                    onLoadContent = { actions += "load $it"; ContentUrlResult(null, null, "Body $it") },
                    inlineContent = { content, _, _ -> Text(content.cachedHtml.orEmpty(), Modifier.height(1200.dp)) },
                )
            }
        }
        compose.waitForIdle()
    }

    /** Swipes until [reached] holds, so the test does not depend on how far one swipe travels. */
    private fun swipeUntil(what: String, swipe: () -> Unit, reached: () -> Boolean) {
        repeat(MAX_SWIPES) {
            if (compose.runOnIdle(reached)) return
            swipe()
        }
        compose.runOnIdle { assertTrue("the list must $what within $MAX_SWIPES swipes", reached()) }
    }

    /** Scrolls the first rows out through the top edge, then back to the top. */
    private fun scrollDownAndBackUp() {
        swipeUntil("scroll past its first rows", { compose.onRoot().performTouchInput { swipeUp() } }) {
            listState.firstVisibleItemIndex > 5
        }
        swipeUntil("return to its top", { compose.onRoot().performTouchInput { swipeDown() } }) {
            listState.firstVisibleItemIndex == 0 && listState.firstVisibleItemScrollOffset == 0
        }
    }

    @Test fun scrollingHeadlinesDownAndBackUpKeepsThemUnread() {
        showList(ArticleLayout.HEADLINES)
        scrollDownAndBackUp()
        compose.runOnIdle { assertEquals("scrolling must not mark or open articles", emptyList<String>(), actions) }
    }

    @Test fun scrollingPastAnInlineArticleMarksOnlyTheOneYouOpened() {
        showList(ArticleLayout.READING)
        compose.onNodeWithText("Headline 2").performClick()
        compose.onNodeWithText("Body 2").assertExists()
        scrollDownAndBackUp()
        // Collapsing reflows the rows below; that must not mark them either.
        compose.onNodeWithText("Collapse").performScrollTo().performClick()
        compose.onNodeWithText("Body 2").assertDoesNotExist()
        scrollDownAndBackUp()
        compose.runOnIdle { assertEquals("only the opened article may be read", listOf("load 2"), actions) }
    }

    private companion object {
        const val MAX_SWIPES = 20
    }
}

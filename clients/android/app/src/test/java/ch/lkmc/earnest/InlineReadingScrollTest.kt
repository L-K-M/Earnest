package ch.lkmc.earnest

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyListState
import androidx.activity.OnBackPressedDispatcher
import androidx.activity.compose.LocalOnBackPressedDispatcherOwner
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.CachedEntry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w400dp-h800dp")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class InlineReadingScrollTest {
    @get:Rule val compose = createComposeRule()
    private val listState = LazyListState()
    private var back: OnBackPressedDispatcher? = null
    private val viewportWidth = mutableIntStateOf(400)

    private fun openTallArticle(selected: Long = 7, onViewport: (InlineArticleState) -> Unit = {}) {
        compose.setContent {
            back = LocalOnBackPressedDispatcherOwner.current?.onBackPressedDispatcher
            EarnestTheme(ThemeSetting.DARK) {
                // Read the width only during measurement: resizing must not
                // recompose the screen through this test's parent scope.
                Box(Modifier.layout { measurable, constraints ->
                    val width = viewportWidth.intValue.dp.roundToPx()
                    val child = measurable.measure(constraints.copy(minWidth = width, maxWidth = width))
                    layout(constraints.maxWidth, child.height) { child.place(0, 0) }
                }) {
                    EntryListScreen(
                        title = "Articles", entries = (1L..30L).map { CachedEntry(it, title = "Headline $it") },
                        states = emptyMap(), principalId = 1, subscriptionTitles = emptyMap(), subscriptionIcons = emptyMap(),
                        briefs = emptyList(), principalNames = emptyMap(), entryTitles = emptyMap(), agentChanges = emptyList(),
                        sessionChanges = emptyList(), agentChangesError = null, onRevert = {}, onOpenAgentSelected = {},
                        onOpen = {}, onLater = {}, onDone = {}, onSeen = {}, onKeep = {}, onUnkeep = {}, onMarkUnread = {},
                        accountKey = "inline-scroll", layoutOverride = ArticleLayout.READING, listState = listState,
                        onLoadContent = { ContentUrlResult(null, null, "Body $it") },
                        inlineContent = { _, _, viewport ->
                            SideEffect { onViewport(viewport) }
                            Box(Modifier.fillMaxWidth().height(1400.dp).testTag("fixture-body"))
                        },
                    )
                }
            }
        }
        compose.runOnIdle { listState.requestScrollToItem(selected.toInt() - 1) }
        compose.onNodeWithText("Headline $selected").performClick()
        compose.waitForIdle()
        compose.runOnIdle {
            // Keep the tall body as the first visible item while its footer
            // and the next headline fit inside the 800dp test viewport.
            listState.requestScrollToItem(selected.toInt() - 1, with(compose.density) { 1000.dp.roundToPx() })
        }
        compose.waitForIdle()
    }

    @Test fun openingTheNextArticleKeepsItsHeadlineInPlace() {
        openTallArticle()
        compose.onNodeWithText("Headline 8").assertIsDisplayed()
        val before = compose.onNodeWithTag("headline-8", useUnmergedTree = true).fetchSemanticsNode().boundsInRoot.top
        compose.onNodeWithText("Headline 8").performClick()
        compose.onNodeWithText("Headline 8").assertIsDisplayed()
        val after = compose.onNodeWithTag("headline-8", useUnmergedTree = true).fetchSemanticsNode().boundsInRoot.top
        assertEquals("collapsing the preceding body must not scroll past the selected article", before, after, 1f)
    }

    @Test fun openingNearTheStartDoesNotOverscrollOrLoseTheTappedHeadline() {
        openTallArticle(selected = 1)
        compose.onNodeWithText("Headline 2").assertIsDisplayed().performClick()
        compose.onNodeWithText("Headline 2").assertIsDisplayed()
        compose.runOnIdle {
            assertEquals(0, listState.firstVisibleItemIndex)
            assertEquals(0, listState.firstVisibleItemScrollOffset)
        }
    }

    @Test fun backFromInsideAnArticleCollapsesToItsOwnHeadline() {
        openTallArticle()
        compose.runOnIdle { checkNotNull(back).onBackPressed() }
        compose.onNodeWithText("Headline 7").assertIsDisplayed()
        compose.onNodeWithTag("fixture-body").assertDoesNotExist()
    }

    @Test fun backAfterScrollingPastTheArticlePreservesTheCurrentRowAndOffset() {
        openTallArticle()
        compose.runOnIdle { listState.requestScrollToItem(20, 33) }
        compose.onNodeWithTag("fixture-body").assertDoesNotExist()
        val before = compose.runOnIdle { listState.layoutInfo.visibleItemsInfo.first() }
        assertEquals(21L, before.key)
        compose.runOnIdle { checkNotNull(back).onBackPressed() }
        compose.runOnIdle {
            val after = listState.layoutInfo.visibleItemsInfo.first()
            assertEquals(before.key, after.key)
            assertEquals(before.offset, after.offset)
        }
    }

    @Test fun widthOnlyResizesInvalidateTheRetainedMeasurement() {
        var viewport: InlineArticleState? = null
        openTallArticle { viewport = it }
        compose.runOnIdle { checkNotNull(viewport).reportHeight(2400) }
        compose.runOnIdle { viewportWidth.intValue = 360 }
        compose.runOnIdle { assertEquals("width changes must be observed without an unrelated recomposition", 0, checkNotNull(viewport).pixels) }
        compose.onNodeWithText("Headline 7").assertIsDisplayed()
    }

    @Test fun lazyItemDisposalDoesNotForgetTheOpenedArticlesMeasurement() {
        val viewports = mutableListOf<InlineArticleState>()
        openTallArticle { viewports += it }
        val viewport = viewports.last()
        compose.runOnIdle { viewport.reportHeight(2400); viewport.showCachedCopy() }
        compose.runOnIdle { listState.requestScrollToItem(25) }
        compose.onNodeWithTag("fixture-body").assertDoesNotExist()
        compose.runOnIdle { listState.requestScrollToItem(6) }
        compose.onNodeWithTag("fixture-body").assertExists()
        compose.runOnIdle {
            assertSame(viewport, viewports.last())
            assertEquals(2400, viewport.pixels)
            assertTrue(viewport.offline)
        }
    }

    @Test fun collapsingFromTheFooterReturnsToThatArticlesHeadline() {
        openTallArticle()
        compose.onNodeWithText("Collapse").assertIsDisplayed().performClick()
        compose.onNodeWithText("Headline 7").assertIsDisplayed()
        compose.runOnIdle {
            assertEquals("collapse must not reuse an offset deep inside the removed body", 6, listState.firstVisibleItemIndex)
            assertEquals(0, listState.firstVisibleItemScrollOffset)
        }
    }
}

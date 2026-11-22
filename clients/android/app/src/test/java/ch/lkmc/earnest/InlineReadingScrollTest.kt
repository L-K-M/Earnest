package ch.lkmc.earnest

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyListState
import androidx.activity.OnBackPressedDispatcher
import androidx.activity.compose.LocalOnBackPressedDispatcherOwner
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.Composable
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeLeft
import androidx.compose.ui.test.swipeRight
import androidx.compose.ui.test.swipeUp
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.SyncEngine
import kotlinx.serialization.json.JsonPrimitive
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
                        onOpen = {}, onLater = {}, onDone = {}, onKeep = {}, onUnkeep = {}, onMarkUnread = {},
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

    @Test fun readerRoundTripKeepsTheListAndArticleOffsets() {
        val showingList = androidx.compose.runtime.mutableStateOf(true)
        var loads = 0
        lateinit var viewport: InlineArticleState
        lateinit var listState: LazyListState
        compose.setContent {
            val readingState = rememberFeedReadingState("account")
            SideEffect { listState = readingState.list }
            val holder = androidx.compose.runtime.saveable.rememberSaveableStateHolder()
            if (showingList.value) holder.SaveableStateProvider("articles") {
                EntryListScreen(
                    title = "Articles", entries = (1L..30L).map { CachedEntry(it, title = "Headline $it") },
                    states = emptyMap(), principalId = 1, subscriptionTitles = emptyMap(), subscriptionIcons = emptyMap(),
                    briefs = emptyList(), principalNames = emptyMap(), entryTitles = emptyMap(), agentChanges = emptyList(),
                    sessionChanges = emptyList(), agentChangesError = null, onRevert = {}, onOpenAgentSelected = {},
                    onOpen = { showingList.value = false }, onLater = {}, onDone = {}, onKeep = {}, onUnkeep = {}, onMarkUnread = {},
                    accountKey = "account", layoutOverride = ArticleLayout.READING, readingState = readingState,
                    onLoadContent = { loads++; ContentUrlResult(null, null, "Body") },
                    inlineContent = { _, _, state ->
                        SideEffect { viewport = state }
                        Box(Modifier.fillMaxWidth().height(1400.dp))
                    },
                )
            }
        }
        compose.runOnIdle { listState.requestScrollToItem(8) }
        compose.onNodeWithText("Headline 9").performClick()
        compose.runOnIdle {
            viewport.reportHeight(3800)
            listState.requestScrollToItem(8, with(compose.density) { 1000.dp.roundToPx() })
        }
        val before = compose.runOnIdle { listState.firstVisibleItemScrollOffset }
        compose.onNodeWithText("Open reader").performClick()
        compose.runOnIdle { showingList.value = true }
        compose.waitForIdle()
        compose.runOnIdle {
            assertEquals(1, loads)
            assertEquals(8, listState.firstVisibleItemIndex)
            assertEquals(before, listState.firstVisibleItemScrollOffset)
            assertEquals(3800, viewport.pixels)
        }
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

    /** Reading articles with bodies sized as the real inline WebView sizes them. */
    private fun showMeasuredArticles(
        restoration: StateRestorationTester? = null,
        entries: () -> List<CachedEntry> = { ARTICLES },
        states: () -> Map<Long, State> = { emptyMap() },
        layout: () -> ArticleLayout = { ArticleLayout.READING },
        unreadOnly: Boolean? = null,
        onLater: (Long) -> Unit = {},
        onExpand: (Long) -> Unit = {},
        onViewport: (InlineArticleState) -> Unit = {},
        onList: (LazyListState) -> Unit = {},
    ) {
        val content = @Composable {
            val reading = rememberFeedReadingState("measured")
            SideEffect { onList(reading.list) }
            EarnestTheme(ThemeSetting.DARK) {
                EntryListScreen(
                    title = "Articles", entries = entries(), states = states(), unreadOnly = unreadOnly,
                    principalId = 1, subscriptionTitles = emptyMap(), subscriptionIcons = emptyMap(),
                    briefs = emptyList(), principalNames = emptyMap(), entryTitles = emptyMap(), agentChanges = emptyList(),
                    sessionChanges = emptyList(), agentChangesError = null, onRevert = {}, onOpenAgentSelected = {},
                    onOpen = {}, onLater = onLater, onDone = {}, onKeep = {}, onUnkeep = {}, onMarkUnread = {},
                    accountKey = "measured", layoutOverride = layout(), readingState = reading, onExpand = onExpand,
                    onLoadContent = { ContentUrlResult(null, null, "Body $it") },
                    inlineContent = { _, _, viewport ->
                        SideEffect { onViewport(viewport) }
                        Box(Modifier.fillMaxWidth().testTag("measured-body").layout { measurable, constraints ->
                            val height = viewport.heightFor(viewport.contentPx, 120)
                            val child = measurable.measure(constraints.copy(minHeight = 0, maxHeight = 0))
                            layout(constraints.maxWidth, height) { child.place(0, 0) }
                        })
                    },
                )
            }
        }
        if (restoration != null) restoration.setContent(content) else compose.setContent(content)
    }

    private fun settle(viewport: InlineArticleState, height: Int) {
        compose.runOnIdle {
            viewport.reportHeight(height)
            viewport.documentLoaded()
        }
        // Let the report reach the settle timer before advancing its clock.
        compose.waitForIdle()
        compose.mainClock.advanceTimeBy(INLINE_SETTLE_MS + 100)
        compose.waitForIdle()
    }

    @Test fun recreationResumesTheSamePlaceInAFullArticle() {
        val restoration = StateRestorationTester(compose)
        lateinit var viewport: InlineArticleState
        lateinit var list: LazyListState
        showMeasuredArticles(restoration, onViewport = { viewport = it }, onList = { list = it })
        compose.onNodeWithText("Headline 2").performClick()
        compose.waitForIdle()
        settle(viewport, 300_000)
        compose.runOnIdle { list.requestScrollToItem(1, 150_000) }
        compose.waitForIdle()
        val before = compose.runOnIdle { (list.firstVisibleItemScrollOffset - viewport.headerPx) / 300_000f }

        val old = viewport
        restoration.emulateSavedInstanceStateRestore()
        compose.waitForIdle()
        compose.runOnIdle {
            assertTrue("the body must have been recreated", viewport !== old)
            assertEquals("an unmeasured body must wait on its headline", 1, list.firstVisibleItemIndex)
            assertEquals(0, list.firstVisibleItemScrollOffset)
        }
        // The other width of a foldable: the same article at another height.
        settle(viewport, 200_000)
        compose.runOnIdle {
            assertEquals(1, list.firstVisibleItemIndex)
            val after = (list.firstVisibleItemScrollOffset - viewport.headerPx) / 200_000f
            assertEquals("the reading place must survive a new height", before, after, 0.001f)
        }
    }

    @Test fun scrollingWhileTheBodyLoadsAbandonsTheSavedPlace() {
        val restoration = StateRestorationTester(compose)
        lateinit var viewport: InlineArticleState
        lateinit var list: LazyListState
        showMeasuredArticles(restoration, onViewport = { viewport = it }, onList = { list = it })
        compose.onNodeWithText("Headline 2").performClick()
        compose.waitForIdle()
        settle(viewport, 300_000)
        compose.runOnIdle { list.requestScrollToItem(1, 150_000) }
        compose.waitForIdle()
        restoration.emulateSavedInstanceStateRestore()
        compose.waitForIdle()
        compose.onRoot().performTouchInput { swipeUp() }
        compose.waitForIdle()
        val chosen = compose.runOnIdle { list.firstVisibleItemIndex to list.firstVisibleItemScrollOffset }
        settle(viewport, 300_000)
        compose.runOnIdle { assertEquals(chosen, list.firstVisibleItemIndex to list.firstVisibleItemScrollOffset) }
    }

    @Test fun losingTheBodyDoesNotSkipTheRowsBelowIt() {
        val layout = mutableStateOf(ArticleLayout.READING)
        lateinit var viewport: InlineArticleState
        lateinit var list: LazyListState
        showMeasuredArticles(layout = { layout.value }, onViewport = { viewport = it }, onList = { list = it })
        compose.onNodeWithText("Headline 2").performClick()
        compose.waitForIdle()
        settle(viewport, 100_000)
        compose.runOnIdle { list.requestScrollToItem(1, 60_000) }
        compose.waitForIdle()
        // A split window turns into the side-by-side reader without recreation.
        compose.runOnIdle { layout.value = ArticleLayout.HEADLINES }
        compose.waitForIdle()
        compose.runOnIdle { assertEquals(1, list.firstVisibleItemIndex) }
    }

    @Test fun anArticleLeavingItsShelfDoesNotSkipTheRowsBelowIt() {
        val entries = mutableStateOf(ARTICLES)
        lateinit var viewport: InlineArticleState
        lateinit var list: LazyListState
        showMeasuredArticles(entries = { entries.value }, onViewport = { viewport = it }, onList = { list = it })
        compose.onNodeWithText("Headline 2").performClick()
        compose.waitForIdle()
        settle(viewport, 100_000)
        compose.runOnIdle { list.requestScrollToItem(1, 60_000) }
        compose.waitForIdle()
        compose.runOnIdle { entries.value = ARTICLES.filter { it.id != 2L } }
        compose.waitForIdle()
        compose.runOnIdle { assertEquals(1, list.firstVisibleItemIndex) }
        compose.onNodeWithText("Headline 3").assertIsDisplayed()
    }

    @Test fun swipingTheExpandedArticleExpandsTheNeighbourAtTheTop() {
        val expanded = mutableListOf<Long>()
        val later = mutableListOf<Long>()
        lateinit var list: LazyListState
        showMeasuredArticles(onExpand = { expanded += it }, onLater = { later += it }, onList = { list = it })
        compose.onNodeWithText("Headline 2").performClick()
        compose.onNodeWithText("Headline 2").performTouchInput { swipeLeft() }
        compose.waitForIdle()
        compose.runOnIdle {
            assertEquals(listOf(2L, 3L), expanded)
            assertTrue("the open article's swipe is not Later or Done", later.isEmpty())
            assertEquals(2, list.firstVisibleItemIndex)
            assertEquals(0, list.firstVisibleItemScrollOffset)
        }
        compose.onNodeWithText("Headline 3").performTouchInput { swipeRight() }
        compose.waitForIdle()
        compose.runOnIdle { assertEquals(listOf(2L, 3L, 2L), expanded) }
    }

    @Test fun swipingBackInAnUnreadListReturnsToTheArticleJustRead() {
        val states = mutableStateOf(emptyMap<Long, State>())
        val expanded = mutableListOf<Long>()
        lateinit var list: LazyListState
        showMeasuredArticles(
            states = { states.value }, unreadOnly = true, onList = { list = it },
            onExpand = { id ->
                expanded += id
                val initial = SyncEngine.initialState(1, id)
                states.value += id to initial.copy(seen = initial.seen.copy(value = JsonPrimitive(SyncEngine.VALUE_SEEN)))
            },
        )
        compose.onNodeWithText("Headline 2").performClick()
        compose.onNodeWithText("Headline 2").performTouchInput { swipeLeft() }
        compose.waitForIdle()
        compose.onNodeWithText("Headline 2").assertDoesNotExist()
        compose.onNodeWithText("Headline 3").assertIsDisplayed().performTouchInput { swipeRight() }
        compose.waitForIdle()
        compose.runOnIdle { assertEquals(listOf(2L, 3L, 2L), expanded) }
        compose.onNodeWithText("Headline 2").assertIsDisplayed()
    }

    @Test fun theSwipesAreAlsoAccessibilityActions() {
        val expanded = mutableListOf<Long>()
        showMeasuredArticles(onExpand = { expanded += it })
        compose.onNodeWithText("Headline 2").performClick()
        val actions = compose.onNodeWithText("Headline 2").fetchSemanticsNode().config[SemanticsActions.CustomActions]
        assertEquals(listOf("Previous article", "Next article"), actions.map { it.label })
        compose.runOnUiThread { actions.last().action() }
        compose.runOnIdle { assertEquals(listOf(2L, 3L), expanded) }
    }

    private companion object {
        val ARTICLES = (1L..60L).map { CachedEntry(it, title = "Headline $it") }
    }
}

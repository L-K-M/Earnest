package ch.lkmc.earnest

import android.view.View as AndroidWidget
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.TouchInjectionScope
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import android.webkit.WebView
import ch.lkmc.earnest.core.ReaderFont
import ch.lkmc.earnest.core.ReaderTheme
import ch.lkmc.earnest.core.ReaderTypography
import org.junit.Assert.assertEquals
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
class ArticleSwipeTest {
    @get:Rule val compose = createComposeRule()
    private val turns = mutableListOf<ArticleDirection>()
    private var clicks = 0
    private lateinit var swipe: ArticleSwipeState
    private val list = LazyListState()

    @Composable
    private fun boundSwipe(available: Set<ArticleDirection> = ArticleDirection.entries.toSet()): ArticleSwipeState {
        val state = rememberArticleSwipeState()
        SideEffect {
            swipe = state
            state.bind(
                neighbour = { if (it in available) ArticleStep(it.ordinal + 1L) else null },
                turn = { turns += ArticleDirection.entries[(it.entryId - 1).toInt()] },
            )
        }
        return state
    }

    private fun showHeadline(
        available: Set<ArticleDirection> = ArticleDirection.entries.toSet(),
        layout: LayoutDirection = LayoutDirection.Ltr,
    ) {
        compose.setContent {
            CompositionLocalProvider(LocalLayoutDirection provides layout) {
                Box(Modifier.fillMaxSize().articleSwipe(boundSwipe(available))) {
                    Box(Modifier.fillMaxSize().testTag(AREA).clickable { clicks++ })
                }
            }
        }
    }

    private fun showPage() {
        compose.setContent {
            val state = boundSwipe()
            Box(Modifier.fillMaxSize().articleSwipe(state)) {
                Box(Modifier.fillMaxSize().testTag(AREA).articlePage(state) { null }.clickable { clicks++ })
            }
        }
    }

    private fun showRealPage(onView: (WebView?) -> Unit) {
        compose.setContent {
            val state = boundSwipe()
            Box(Modifier.fillMaxSize().articleSwipe(state)) {
                Reader(
                    url = null, cachedHtml = "<p>Body</p>",
                    typography = ReaderTypography(ReaderTheme.DARK, 68, ReaderFont.SERIF),
                    modifier = Modifier.fillMaxSize().testTag(AREA), onWebView = onView, swipe = state,
                )
            }
        }
    }

    private fun touch(block: TouchInjectionScope.() -> Unit) = compose.onNodeWithTag(AREA).performTouchInput(block)

    private fun TouchInjectionScope.drag(dx: Float, dy: Float = 0f, steps: Int = 10) {
        repeat(steps) { moveBy(Offset(dx / steps, dy / steps)) }
    }

    private fun expectTurns(vararg expected: ArticleDirection) =
        compose.runOnIdle { assertEquals(expected.toList(), turns) }

    @Test fun leftIsNextAndRightIsPrevious() {
        showHeadline()
        touch { down(center); drag(-200f); up() }
        expectTurns(ArticleDirection.NEXT)
        touch { down(center); drag(200f); up() }
        expectTurns(ArticleDirection.NEXT, ArticleDirection.PREVIOUS)
        assertEquals("a swipe must not also tap what it started on", 0, clicks)
    }

    @Test fun rightToLeftLayoutsMirrorTheDirections() {
        showHeadline(layout = LayoutDirection.Rtl)
        touch { down(center); drag(200f); up() }
        expectTurns(ArticleDirection.NEXT)
    }

    @Test fun aShortDragSettlesBack() {
        showHeadline()
        touch { down(center); drag(-50f); up() }
        expectTurns()
        compose.runOnIdle { assertEquals(0f, swipe.offset) }
    }

    @Test fun draggingBackCancels() {
        showHeadline()
        touch { down(center); drag(-200f); drag(190f); up() }
        expectTurns()
    }

    @Test fun aCancelledTouchNeverTurns() {
        showHeadline()
        touch { down(center); drag(-200f); cancel() }
        expectTurns()
        compose.runOnIdle { assertEquals(0f, swipe.offset) }
    }

    @Test fun aSecondFingerCancels() {
        showHeadline()
        touch {
            down(0, center)
            repeat(5) { moveBy(0, Offset(-30f, 0f)) }
            down(1, center + Offset(0f, 100f))
            repeat(5) { moveBy(0, Offset(-30f, 0f)) }
            up(0)
            up(1)
        }
        expectTurns()
    }

    @Test fun aDiagonalDragIsNotASwipe() {
        showHeadline()
        touch { down(center); drag(-200f, 150f); up() }
        expectTurns()
    }

    @Test fun holdingBeforeDraggingIsNotASwipe() {
        showHeadline()
        touch {
            down(center)
            advanceEventTime(viewConfiguration.longPressTimeoutMillis + 100)
            move()
            drag(-200f)
            up()
        }
        expectTurns()
    }

    @Test fun theLastArticleHasNoNext() {
        showHeadline(available = setOf(ArticleDirection.PREVIOUS))
        touch { down(center); drag(-200f); up() }
        expectTurns()
    }

    @Test fun aPageThatScrollsSidewaysKeepsItsDrag() {
        showPage()
        touch { down(center); drag(-250f) }
        compose.runOnIdle { assertEquals("a scrolling code block must not move the article", 0f, swipe.offset) }
        touch { up() }
        expectTurns()
    }

    @Test fun aPageThatCannotScrollSidewaysTurns() {
        showPage()
        touch { down(center); drag(-30f) }
        compose.runOnIdle { swipe.pageRefusedScroll() }
        touch { drag(-200f); up() }
        expectTurns(ArticleDirection.NEXT)
    }

    @Test fun aQuickFlickTurnsWhenThePageReportsAfterTheFingerLifts() {
        showPage()
        touch { down(center); drag(-200f, steps = 2); up() }
        compose.runOnUiThread { swipe.pageRefusedScroll() }
        expectTurns(ArticleDirection.NEXT)
    }

    @Test fun aReportFromAnEarlierGestureDoesNotCarryOver() {
        showPage()
        touch { down(center); drag(0f, 80f) }
        compose.runOnIdle { swipe.pageRefusedScroll() }
        touch { up() }
        touch { down(center); drag(-250f); up() }
        expectTurns()
    }

    @Test fun theWebViewsClampedOverscrollIsThePagesRefusal() {
        var page: WebView? = null
        showRealPage { page = it }
        touch { down(center); drag(-30f) }
        compose.runOnIdle { checkNotNull(page).reportOverscroll(clampedX = true) }
        touch { drag(-200f); up() }
        expectTurns(ArticleDirection.NEXT)
    }

    @Test fun verticalOverscrollIsNotARefusal() {
        var page: WebView? = null
        showRealPage { page = it }
        touch { down(center); drag(-30f) }
        compose.runOnIdle { checkNotNull(page).reportOverscroll(clampedX = false) }
        touch { drag(-200f); up() }
        expectTurns()
    }

    @Test fun aPageScrolledSidewaysInThisGestureDoesNotTurnAtItsEdge() {
        var page: WebView? = null
        showRealPage { page = it }
        touch { down(center); drag(-30f) }
        compose.runOnIdle {
            val view = checkNotNull(page)
            view.scrollTo(40, 0)
            view.reportOverscroll(clampedX = true)
        }
        touch { drag(-200f); up() }
        expectTurns()
    }

    @Test fun inAListSwipesTurnAndVerticalDragsStillScroll() {
        compose.setContent {
            val state = boundSwipe()
            LazyColumn(Modifier.fillMaxSize().testTag(AREA), state = list) {
                item { Box(Modifier.fillMaxWidth().height(5_000.dp).articleSwipe(state)) }
            }
        }
        // One event that passes the slop on both axes; a list watching only y would take it.
        touch { down(center); moveBy(Offset(-80f, 24f)); drag(-150f, 20f); up() }
        expectTurns(ArticleDirection.NEXT)
        compose.runOnIdle { assertEquals(0, list.firstVisibleItemScrollOffset) }
        touch { down(center); drag(0f, -300f); up() }
        compose.runOnIdle { assertTrue("vertical drags must still scroll", list.firstVisibleItemScrollOffset > 0) }
        expectTurns(ArticleDirection.NEXT)
    }

    private fun WebView.reportOverscroll(clampedX: Boolean) {
        val boolean = Boolean::class.javaPrimitiveType
        val int = Int::class.javaPrimitiveType
        AndroidWidget::class.java.getDeclaredMethod("onOverScrolled", int, int, boolean, boolean)
            .apply { isAccessible = true }
            .invoke(this, scrollX, scrollY, clampedX, false)
    }

    private companion object {
        const val AREA = "area"
    }
}

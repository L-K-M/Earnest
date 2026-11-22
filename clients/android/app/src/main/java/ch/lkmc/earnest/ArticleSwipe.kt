package ch.lkmc.earnest

import android.os.SystemClock
import android.webkit.WebView
import androidx.compose.animation.core.animate
import androidx.compose.animation.core.spring
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.absoluteOffset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.hapticfeedback.HapticFeedback
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.PointerType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlin.math.abs
import kotlin.math.roundToInt

internal val ARTICLE_SWIPE_DISTANCE = 72.dp

// A swipe must be clearly sideways; a diagonal drag belongs to scrolling.
private const val SIDEWAYS_SLOPE = 2f

// The article follows at a fraction of the finger, less when nothing is next.
private const val FOLLOW = 0.35f
private const val FOLLOW_AT_END = 0.12f
private const val MAX_FOLLOW_DISTANCES = 3f

// Chromium reports a refused scroll a frame or so behind the touch that caused
// it, so the report for a very quick flick can land after the finger lifts.
private const val REFUSAL_GRACE_MS = 100L

/**
 * One sideways drag over an article.
 *
 * Article pages run without JavaScript, so the app cannot ask what lies under
 * the finger. A drag that starts on the page only becomes a swipe once the
 * WebView reports that nothing on the page could scroll that way
 * ([pageRefusedScroll]). A code block or table that can still scroll, and a
 * text selection, never produce that report and keep their gesture.
 */
@Stable
internal class ArticleSwipeState(private val scope: CoroutineScope, private val haptics: HapticFeedback) {
    var offset by mutableFloatStateOf(0f)
        private set
    var direction by mutableStateOf<ArticleDirection?>(null)
        private set
    var available by mutableStateOf(false)
        private set
    var armed by mutableStateOf(false)
        private set

    internal var rightToLeft = false
    private var neighbour: (ArticleDirection) -> ArticleStep? = { null }
    private var turn: (ArticleStep) -> Unit = {}
    private var settling: Job? = null

    // The current gesture, written before anything below it sees the touch.
    private var onPage = false
    private var page: WebView? = null
    private var pageScrollX = 0
    private var refused = false
    private var lateFlick: LateFlick? = null

    private class LateFlick(val direction: ArticleDirection, val until: Long)

    fun bind(neighbour: (ArticleDirection) -> ArticleStep?, turn: (ArticleStep) -> Unit) {
        this.neighbour = neighbour
        this.turn = turn
    }

    /** The WebView could not scroll sideways any further. */
    fun pageRefusedScroll() {
        // At the edge of a page this gesture scrolled, it refuses only the rest
        // of that scroll. Dragging back must not turn that into a swipe.
        if (!pageIsStill()) return
        refused = true
        val flick = lateFlick ?: return
        lateFlick = null
        if (SystemClock.uptimeMillis() <= flick.until) commit(flick.direction)
    }

    internal fun begin() {
        onPage = false
        page = null
        refused = false
        lateFlick = null
    }

    internal fun beginOnPage(view: WebView?) {
        onPage = true
        page = view
        pageScrollX = view?.scrollX ?: 0
    }

    /** Takes a sideways drag from an enclosing vertical list before it can cancel the page's touch. */
    internal fun holdPage() {
        page?.parent?.requestDisallowInterceptTouchEvent(true)
    }

    internal fun mayTurn(): Boolean = !onPage || (refused && pageIsStill())

    private fun pageIsStill() = page?.let { it.scrollX == pageScrollX } ?: true

    internal fun startDrag(toward: ArticleDirection) {
        settling?.cancel()
        direction = toward
        available = neighbour(toward) != null
        armed = false
    }

    internal fun drag(travel: Float, distance: Float) {
        val toward = direction ?: return
        val shown = travel.coerceIn(0f, distance * MAX_FOLLOW_DISTANCES)
        offset = physicalSign(toward) * shown * if (available) FOLLOW else FOLLOW_AT_END
        val nowArmed = available && travel >= distance
        if (nowArmed && !armed) haptics.performHapticFeedback(HapticFeedbackType.GestureThresholdActivate)
        armed = nowArmed
    }

    internal fun release() {
        val toward = direction ?: return
        if (armed) commit(toward) else settle()
    }

    internal fun releaseBeforeReport(toward: ArticleDirection) {
        lateFlick = LateFlick(toward, SystemClock.uptimeMillis() + REFUSAL_GRACE_MS)
    }

    private fun commit(toward: ArticleDirection) {
        neighbour(toward)?.let { step ->
            // The article that arrives slides in from the side it was pulled from.
            offset = -offset
            turn(step)
        }
        settle()
    }

    internal fun settle() {
        direction = null
        armed = false
        settling?.cancel()
        settling = scope.launch {
            animate(offset, 0f, animationSpec = spring(dampingRatio = 0.9f, stiffness = 500f)) { value, _ -> offset = value }
        }
    }

    /** The screen x direction the finger moves in to go [toward]. */
    internal fun physicalSign(toward: ArticleDirection): Int = if (rightToLeft) toward.step else -toward.step
}

@Composable
internal fun rememberArticleSwipeState(): ArticleSwipeState {
    val scope = rememberCoroutineScope()
    val haptics = LocalHapticFeedback.current
    val rightToLeft = LocalLayoutDirection.current == LayoutDirection.Rtl
    val state = remember(scope, haptics) { ArticleSwipeState(scope, haptics) }
    SideEffect { state.rightToLeft = rightToLeft }
    return state
}

/**
 * Turns articles on a sideways drag and moves the content with it.
 *
 * It watches on the Initial pass, ahead of the WebView and any scroll
 * container. Once a drag is a swipe it consumes the rest, which cancels the
 * page's touch and any click beneath it.
 */
internal fun Modifier.articleSwipe(state: ArticleSwipeState): Modifier = this
    .pointerInput(state) {
        val distance = ARTICLE_SWIPE_DISTANCE.toPx()
        val slop = viewConfiguration.touchSlop
        val longPress = viewConfiguration.longPressTimeoutMillis
        awaitEachGesture {
            val down = awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
            state.begin()
            // A consumed down stopped a fling; a mouse drag selects text.
            if (down.isConsumed || down.type == PointerType.Mouse) return@awaitEachGesture
            var toward: ArticleDirection? = null
            var dragging = false
            try {
                while (true) {
                    val event = awaitPointerEvent(PointerEventPass.Initial)
                    if (event.changes.size > 1) return@awaitEachGesture
                    val change = event.changes.first()
                    // Compose delivers a cancelled touch, such as system Back, as a consumed up.
                    if (change.isConsumed) return@awaitEachGesture
                    val moved = change.position - down.position
                    if (toward == null) {
                        val x = abs(moved.x)
                        val y = abs(moved.y)
                        when {
                            // A still finger sends no events, so the hold shows on the first move after it.
                            change.uptimeMillis - down.uptimeMillis >= longPress -> return@awaitEachGesture
                            x > slop && x > y * SIDEWAYS_SLOPE -> {
                                toward = ArticleDirection.entries.first { moved.x * state.physicalSign(it) > 0 }
                                state.holdPage()
                            }
                            y > slop || !change.pressed -> return@awaitEachGesture
                        }
                    }
                    val locked = toward ?: continue
                    val travel = moved.x * state.physicalSign(locked)
                    if (!dragging && state.mayTurn()) {
                        dragging = true
                        state.startDrag(locked)
                    }
                    if (dragging) {
                        state.drag(travel, distance)
                        change.consume()
                    }
                    if (!change.pressed) {
                        if (dragging) {
                            dragging = false
                            state.release()
                        } else if (travel >= distance) {
                            state.releaseBeforeReport(locked)
                        }
                        return@awaitEachGesture
                    }
                }
            } finally {
                if (dragging) state.settle()
            }
        }
    }
    .absoluteOffset { IntOffset(state.offset.roundToInt(), 0) }

/** Marks the page's area inside an [articleSwipe], where the WebView decides who owns a drag. */
internal fun Modifier.articlePage(state: ArticleSwipeState, webView: () -> WebView?): Modifier = pointerInput(state) {
    awaitEachGesture {
        awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
        state.beginOnPage(webView())
    }
}

/** Names where a drag leads, at the edge it uncovers. */
@Composable
internal fun BoxScope.ArticleSwipeHint(state: ArticleSwipeState) {
    val direction = state.direction ?: return
    val scheme = MaterialTheme.colorScheme
    Surface(
        modifier = Modifier
            .align(if (direction == ArticleDirection.NEXT) Alignment.CenterEnd else Alignment.CenterStart)
            .padding(horizontal = ReaderSpace.md)
            // Transient; the same moves are accessibility actions on the headline.
            .clearAndSetSemantics {},
        shape = RoundedCornerShape(percent = 50),
        color = if (state.armed) scheme.primary else scheme.surfaceContainerHighest,
        contentColor = if (state.armed) scheme.onPrimary else scheme.onSurface,
    ) {
        Text(
            if (state.available) direction.label else direction.endLabel,
            style = MaterialTheme.typography.labelLarge,
            modifier = Modifier.padding(horizontal = ReaderSpace.md, vertical = ReaderSpace.sm),
        )
    }
}

/** The swipes, for readers who cannot make them. */
internal fun articleActions(
    neighbour: (ArticleDirection) -> ArticleStep?,
    turn: (ArticleStep) -> Unit,
): List<CustomAccessibilityAction> = ArticleDirection.entries.mapNotNull { direction ->
    neighbour(direction)?.let { step ->
        CustomAccessibilityAction(direction.label) {
            turn(step)
            true
        }
    }
}

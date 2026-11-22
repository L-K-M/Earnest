package ch.lkmc.earnest

import android.content.Context
import android.webkit.WebView
import androidx.compose.foundation.background
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.dp
import androidx.core.content.edit
import ch.lkmc.earnest.core.ReaderTypography

/** Device-local presentation, separate from the account's unread/river mode. */
internal enum class ArticleLayout(val label: String) {
    HEADLINES("Headlines"),
    READING("Read in feed"),
}

private const val LAYOUT_PREFS = "appearance"
private const val LAYOUT_KEY = "article_layout"

internal fun Context.articleLayout(): ArticleLayout {
    val name = getSharedPreferences(LAYOUT_PREFS, Context.MODE_PRIVATE).getString(LAYOUT_KEY, null)
    return ArticleLayout.entries.firstOrNull { it.name == name } ?: ArticleLayout.READING
}

internal fun Context.setArticleLayout(layout: ArticleLayout) {
    getSharedPreferences(LAYOUT_PREFS, Context.MODE_PRIVATE).edit { putString(LAYOUT_KEY, layout.name) }
}

@Composable
internal fun ArticleLayoutControl(layout: ArticleLayout, onLayout: (ArticleLayout) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
            ArticleLayout.entries.forEachIndexed { index, option ->
                SegmentedButton(
                    selected = layout == option,
                    onClick = { onLayout(option) },
                    shape = SegmentedButtonDefaults.itemShape(index, ArticleLayout.entries.size),
                    colors = SegmentedButtonDefaults.colors(
                        activeContainerColor = MaterialTheme.colorScheme.surfaceContainerHigh,
                        activeContentColor = MaterialTheme.colorScheme.primary,
                    ),
                    label = { Text(option.label, style = MaterialTheme.typography.labelLarge) },
                )
            }
        }
        Text(
            if (layout == ArticleLayout.READING) "Tap a headline to read without leaving this feed." else "Tap a headline to open the reader.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 8.dp, bottom = 4.dp),
        )
    }
}

private const val MIN_INLINE_HEIGHT_DP = 120

// Chromium grows a document in steps and reports its size a frame or more
// after the load ends. A height that holds this long is the article's.
internal const val INLINE_SETTLE_MS = 300L

/**
 * How a saved reading place relates to the body it points into: not applied
 * since that body was last measured, waiting on the headline for its height,
 * given up because the reader scrolled, or in effect.
 */
internal enum class ResumeState { UNPLACED, WAITING, ABANDONED, PLACED }

/** Owned by the list, not its disposable lazy item or native WebView. */
internal class InlineArticleState {
    /** The height to hold while a WebView is missing or still laying out. */
    var pixels by mutableIntStateOf(0)
        private set

    /** The live WebView's natural height. */
    var contentPx by mutableIntStateOf(0)
        private set
    var loaded by mutableStateOf(false)
        private set

    /** The live height is final, so it may be shorter than [pixels]. */
    var settled by mutableStateOf(false)
        private set
    var offline by mutableStateOf(false)
        private set
    // Observed on its own: a WebView can report a height again before the
    // composition that saw the last one forgotten.
    var resume by mutableStateOf(ResumeState.UNPLACED)

    /** The expanded row's height above the body. */
    var headerPx = 0
    private var measurement: Measurement? = null

    fun configure(widthPx: Int, typography: ReaderTypography, zoom: Int, density: Float, fontScale: Float) {
        if (widthPx <= 0) return
        val next = Measurement(widthPx, typography, zoom, density, fontScale)
        if (measurement == next) return
        // The first measurement describes the body already being laid out.
        if (measurement != null) invalidate()
        measurement = next
    }

    /** Forgets the measured height; the place in the article is kept elsewhere. */
    fun invalidate() {
        pixels = 0
        settled = false
        resume = ResumeState.UNPLACED
    }

    fun documentReleased() {
        contentPx = 0
        loaded = false
        settled = false
    }

    fun rendererLost() {
        documentReleased()
        invalidate()
    }

    fun reportHeight(height: Int) {
        if (height <= 0) return
        contentPx = height
        if (settled || height > pixels) pixels = height
    }

    fun documentLoaded() { loaded = true }

    fun settle() {
        settled = true
        pixels = contentPx
        resume = ResumeState.PLACED
    }

    /** The body's height, given the [natural] height measured for the live WebView. */
    fun heightFor(natural: Int, minimum: Int): Int =
        if (settled && natural > 0) maxOf(natural, minimum) else maxOf(natural, pixels, minimum)

    fun showCachedCopy() { offline = true }
    fun showLiveCopy() { offline = false }

    private data class Measurement(
        val widthPx: Int,
        val typography: ReaderTypography,
        val zoom: Int,
        val density: Float,
        val fontScale: Float,
    )
}

/**
 * A single, explicitly opened article at its full height; the list scrolls it.
 * Merely showing headlines never fetches content.
 */
@Composable
internal fun InlineArticleContent(
    content: ContentUrlResult,
    typography: ReaderTypography,
    state: InlineArticleState,
    swipe: ArticleSwipeState? = null,
) {
    val context = LocalContext.current
    val background = Color(readerBackground(typography, isSystemInDarkTheme()))
    val attached = remember(state) { arrayOfNulls<WebView>(1) }
    Column {
        if (state.offline || content.url == null) {
            Text(
                "Cached copy",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
        Reader(
            url = content.url,
            cachedHtml = content.cachedHtml,
            typography = typography,
            textZoom = context.readerZoom(),
            modifier = Modifier
                .fillMaxWidth()
                .testTag("inline-article-content")
                .background(background)
                .layout { measurable, constraints ->
                    // Report a size rather than constrain to one: Constraints cannot
                    // carry the height of a long article, and a layout result can.
                    val page = measurable.measure(constraints.copy(minHeight = 0, maxHeight = Constraints.Infinity))
                    layout(page.width, state.heightFor(page.height, MIN_INLINE_HEIGHT_DP.dp.roundToPx())) {
                        page.place(0, 0)
                    }
                },
            onFallback = state::showCachedCopy,
            onLiveContent = state::showLiveCopy,
            onWebView = { view ->
                if (attached[0] !== view) {
                    attached[0] = view
                    state.documentReleased()
                }
            },
            onContentHeight = state::reportHeight,
            onPageLoaded = state::documentLoaded,
            onRendererLost = state::rendererLost,
            swipe = swipe,
        )
    }
}

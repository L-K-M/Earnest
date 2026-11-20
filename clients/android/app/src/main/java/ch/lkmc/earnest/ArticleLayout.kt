package ch.lkmc.earnest

import android.content.Context
import android.webkit.WebView
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.platform.testTag
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

// Cap native pixels, not dp, so dense displays cannot multiply the inline
// allocation. Longer documents page inside the same native viewport.
internal const val MAX_INLINE_HEIGHT_PX = 4096
private const val MIN_INLINE_HEIGHT_DP = 120

internal fun inlineHeightPx(pixels: Int, density: Float): Int {
    val minimum = kotlin.math.ceil(MIN_INLINE_HEIGHT_DP * density.toDouble()).toInt().coerceAtMost(MAX_INLINE_HEIGHT_PX)
    return pixels.coerceIn(minimum, MAX_INLINE_HEIGHT_PX)
}

/** Owned by the list, not its disposable lazy item or native WebView. */
internal class InlineArticleState {
    var pixels by mutableIntStateOf(0)
        private set
    var offline by mutableStateOf(false)
        private set
    var scrollY by mutableIntStateOf(0)
    private var measurement: Measurement? = null

    fun configure(widthPx: Int, typography: ReaderTypography, zoom: Int, density: Float, fontScale: Float) {
        if (widthPx <= 0) return
        val next = Measurement(widthPx, typography, zoom, density, fontScale)
        if (measurement != next) {
            measurement = next
            pixels = 0
            scrollY = 0
        }
    }

    fun reportHeight(height: Int) { if (height > 0) pixels = height }
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

/** A single, explicitly opened article. Merely showing headlines never fetches content. */
@Composable
internal fun InlineArticleContent(content: ContentUrlResult, typography: ReaderTypography, state: InlineArticleState) {
    val context = LocalContext.current
    val density = LocalDensity.current
    val windowHeight = LocalWindowInfo.current.containerSize.height
    // Keep the page controls within reach and bound Chromium's backing surface.
    val heightPx = inlineHeightPx(state.pixels, density.density)
        .coerceAtMost((windowHeight * 2 / 3).coerceAtLeast(1))
    val height = with(density) { heightPx.toDp() }
    var webView by remember { mutableStateOf<WebView?>(null) }
    val lastOffset = (state.pixels - heightPx).coerceAtLeast(0)
    val canGoBack by remember(state) { derivedStateOf { state.scrollY > 0 } }
    val canGoForward by remember(state, lastOffset) { derivedStateOf { state.scrollY < lastOffset } }
    fun turnPage(direction: Int) {
        val view = webView ?: return
        // A little overlap keeps a line cut by the edge readable on the next page.
        val step = (view.height * 9 / 10).coerceAtLeast(1)
        val target = (view.scrollY.toLong() + direction * step).coerceIn(0L, lastOffset.toLong())
        view.scrollTo(0, target.toInt())
        state.scrollY = view.scrollY
    }
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
            modifier = Modifier.fillMaxWidth().height(height).testTag("inline-article-content"),
            onFallback = state::showCachedCopy,
            onLiveContent = state::showLiveCopy,
            onWebView = { view ->
                if (webView !== view) {
                    webView?.setOnScrollChangeListener(null)
                    webView = view
                    view?.setOnScrollChangeListener { _, _, y, _, _ -> state.scrollY = y }
                }
            },
            onContentHeight = { pixels ->
                state.reportHeight(pixels)
                webView?.scrollTo(0, state.scrollY)
            },
        )
        if (lastOffset > 0 && webView != null) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
                TextButton(onClick = { turnPage(-1) }, enabled = canGoBack) { Text("Previous page") }
                TextButton(onClick = { turnPage(1) }, enabled = canGoForward) { Text("Next page") }
            }
        }
    }
}

package ch.lkmc.earnest

import android.content.Context
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
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

// Bound the native view's allocation for very long documents. The dedicated
// reader remains available and owns scrolling beyond this inline preview.
internal const val MAX_INLINE_HEIGHT_DP = 8000

internal fun inlineHeightDp(pixels: Int, density: Float): Int =
    kotlin.math.ceil(pixels / density.toDouble()).toInt().coerceIn(120, MAX_INLINE_HEIGHT_DP)

/** A single, explicitly opened article. Merely showing headlines never fetches content. */
@Composable
internal fun InlineArticleContent(content: ContentUrlResult, typography: ReaderTypography) {
    val context = LocalContext.current
    val density = LocalDensity.current
    var pixels by remember(content) { mutableIntStateOf(0) }
    var offline by remember(content) { mutableStateOf(false) }
    val height = inlineHeightDp(pixels, density.density)
    Column {
        if (offline) {
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
            modifier = Modifier.fillMaxWidth().height(height.dp).testTag("inline-article-content"),
            onFallback = { offline = true },
            onContentHeight = { pixels = it },
        )
        if (pixels / density.density > MAX_INLINE_HEIGHT_DP) {
            Text(
                "Long article. Open the reader to continue.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(16.dp),
            )
        }
    }
}

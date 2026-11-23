package ch.lkmc.earnest

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import java.util.Locale

/**
 * The seven letter-mark colours, in the order of the web client's
 * `data-swatch` rules. They keep these values in every colour theme, so a
 * feed is recognisable on both clients and in both light and dark.
 */
internal val FeedSwatches = listOf(
    Color(0xFFDD5602),
    Color(0xFFAE7705),
    Color(0xFFB23A1A),
    Color(0xFF8E3B68),
    Color(0xFF916E9D),
    Color(0xFF3E8AAB),
    Color(0xFF428E87),
)

private const val HASH_FACTOR = 31
private const val UNKNOWN_LETTER = "?"
private const val MARK_CORNER_PERCENT = 30
private const val MARK_LETTER_SCALE = 0.54f
private const val MARK_ICON_SCALE = 0.72f

/** The same hash as the web client's `feedSwatch`, over code points. */
internal fun feedSwatch(title: String): Int {
    var hash = 0
    var index = 0
    while (index < title.length) {
        val point = title.codePointAt(index)
        hash = (hash * HASH_FACTOR + point) % FeedSwatches.size
        index += Character.charCount(point)
    }
    return hash
}

/** A title's first character in capitals, surrogate pairs included. */
internal fun feedLetter(title: String): String {
    val trimmed = title.trim()
    if (trimmed.isEmpty()) return UNKNOWN_LETTER
    return trimmed.substring(0, trimmed.offsetByCodePoints(0, 1)).uppercase(Locale.getDefault())
}

/**
 * A feed's mark: its stored icon, or a coloured letter until that arrives and
 * whenever there is none. Decorative, as the feed's name always sits beside it.
 */
@Composable
internal fun FeedAvatar(title: String, iconUrl: String?, modifier: Modifier = Modifier, size: Dp = 40.dp) {
    val box = modifier.size(size)
    if (iconUrl.isNullOrEmpty()) {
        LetterMark(title, box, size)
        return
    }
    MediaImage(
        path = iconUrl,
        modifier = box,
        shape = RoundedCornerShape(MARK_CORNER_PERCENT),
        placeholder = { LetterMark(title, it, size) },
        // A favicon is often transparent or near black: it needs its own paper.
        frame = { frame, picture ->
            Box(
                frame.background(Color.White, RoundedCornerShape(MARK_CORNER_PERCENT)),
                contentAlignment = Alignment.Center,
            ) { picture(Modifier.size(size * MARK_ICON_SCALE)) }
        },
    )
}

@Composable
private fun LetterMark(title: String, modifier: Modifier, size: Dp) {
    val scheme = MaterialTheme.colorScheme
    val monochrome = scheme.isMonochrome
    val swatch = remember(title) { FeedSwatches[feedSwatch(title)] }
    val letter = remember(title) { feedLetter(title) }
    val shape = RoundedCornerShape(MARK_CORNER_PERCENT)
    val paint = if (monochrome) Modifier.border(1.dp, scheme.onSurface, shape) else Modifier.background(swatch, shape)
    // The mark is a graphic: its letter follows the mark's size, not the text size.
    val letterSize = with(LocalDensity.current) { (size * MARK_LETTER_SCALE).toSp() }
    Box(modifier.then(paint).clearAndSetSemantics {}, contentAlignment = Alignment.Center) {
        Text(
            letter,
            color = if (monochrome) scheme.onSurface else Color.White,
            fontFamily = FontFamily.Serif,
            fontWeight = FontWeight.Bold,
            fontSize = letterSize,
            lineHeight = letterSize,
            textAlign = TextAlign.Center,
            maxLines = 1,
            softWrap = false,
        )
    }
}

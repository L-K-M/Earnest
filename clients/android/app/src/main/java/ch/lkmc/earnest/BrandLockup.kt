package ch.lkmc.earnest

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp

/** The wordmark's own teal, for light surfaces. */
private val BrandInk = Color(0xFF07566E)

/** The same hue lightened for dark surfaces, where the wordmark's teal is unreadable. */
private val BrandInkOnDark = Color(0xFF8BD0EF)

/**
 * The colour for the wordmark's lettering on [scheme]'s background. Monochrome
 * themes such as e-ink keep their black or white primary.
 */
internal fun brandInk(scheme: ColorScheme): Color = when {
    scheme.primary == Color.Black || scheme.primary == Color.White -> scheme.primary
    scheme.background.luminance() < 0.5f -> BrandInkOnDark
    else -> BrandInk
}

/** The app icon beside the wordmark's lettering, drawn in the brand ink for the theme. */
@Composable
internal fun BrandLockup(modifier: Modifier = Modifier) {
    val wordmark = painterResource(R.drawable.brand_wordmark)
    Row(modifier = modifier.semantics { contentDescription = "Earnest" }, verticalAlignment = Alignment.CenterVertically) {
        Image(
            painter = painterResource(R.drawable.brand_icon),
            contentDescription = null,
            modifier = Modifier.size(56.dp),
        )
        Spacer(Modifier.width(8.dp))
        Image(
            painter = wordmark,
            contentDescription = null,
            colorFilter = ColorFilter.tint(brandInk(MaterialTheme.colorScheme)),
            modifier = Modifier
                .height(33.dp)
                .aspectRatio(wordmark.intrinsicSize.width / wordmark.intrinsicSize.height),
        )
    }
}

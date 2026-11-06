package ch.lkmc.earnest

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp

/** The app icon beside the wordmark's lettering, tinted for the theme because the lettering's own dark teal is unreadable on dark backgrounds. */
@Composable
internal fun BrandLockup(modifier: Modifier = Modifier) {
    val wordmark = painterResource(R.drawable.brand_wordmark)
    Row(modifier = modifier, verticalAlignment = Alignment.CenterVertically) {
        Image(
            painter = painterResource(R.drawable.brand_icon),
            contentDescription = null,
            modifier = Modifier.size(56.dp),
        )
        Spacer(Modifier.width(8.dp))
        Image(
            painter = wordmark,
            contentDescription = null,
            colorFilter = ColorFilter.tint(MaterialTheme.colorScheme.primary),
            modifier = Modifier
                .height(33.dp)
                .aspectRatio(wordmark.intrinsicSize.width / wordmark.intrinsicSize.height),
        )
    }
}

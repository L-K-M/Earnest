package ch.lkmc.earnest

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import ch.lkmc.earnest.core.ReaderTheme
import ch.lkmc.earnest.core.ReaderTypography

/**
 * The article's own colours. The chrome around the WebView is drawn on the
 * same canvas, so the page has no seam under the title or above the actions.
 */
internal data class ReaderCanvas(val scheme: ColorScheme, val tones: Tones)

private const val QUIET_INK = 0.28f
private const val OUTLINE_INK = 0.45f
private const val HAIRLINE_INK = 0.85f
private const val LOW_LIFT = 0.03f
private const val LIFT = 0.06f
private const val HIGH_LIFT = 0.09f
private const val HIGHEST_LIFT = 0.12f

internal fun readerCanvas(typography: ReaderTypography, chrome: ThemeSetting?, systemDark: Boolean): ReaderCanvas {
    val paper = Color(readerBackground(typography, systemDark))
    val ink = Color(readerForeground(typography, systemDark))
    val monochrome = chrome == ThemeSetting.E_INK || typography.theme == ReaderTheme.EINK
    val setting = when {
        monochrome && paper == Color.White -> ThemeSetting.E_INK
        paper == Color.Black -> ThemeSetting.BLACK
        paper == Color.White -> ThemeSetting.LIGHT
        else -> ThemeSetting.DARK
    }
    // Accents come from the palette made for this canvas's brightness.
    val base = colorScheme(setting, systemDark)
    val scheme = base.copy(
        background = paper,
        surface = paper,
        onBackground = ink,
        onSurface = ink,
        onSurfaceVariant = if (base.isMonochrome) ink else lerp(ink, paper, QUIET_INK),
        outline = lerp(ink, paper, OUTLINE_INK),
        outlineVariant = if (base.isMonochrome) base.outlineVariant else lerp(ink, paper, HAIRLINE_INK),
        surfaceContainerLowest = paper,
        surfaceContainerLow = lerp(paper, ink, LOW_LIFT),
        surfaceContainer = lerp(paper, ink, LIFT),
        surfaceContainerHigh = lerp(paper, ink, HIGH_LIFT),
        surfaceContainerHighest = lerp(paper, ink, HIGHEST_LIFT),
    )
    return ReaderCanvas(scheme, tones(setting, systemDark))
}

@Composable
internal fun rememberReaderCanvas(typography: ReaderTypography, chrome: ThemeSetting?): ReaderCanvas {
    val systemDark = isSystemInDarkTheme()
    return remember(typography, chrome, systemDark) { readerCanvas(typography, chrome, systemDark) }
}

@Composable
internal fun ReaderCanvasSurface(canvas: ReaderCanvas, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    CompositionLocalProvider(LocalTones provides canvas.tones) {
        MaterialTheme(colorScheme = canvas.scheme) {
            Surface(modifier, color = canvas.scheme.surface, contentColor = canvas.scheme.onSurface, content = content)
        }
    }
}

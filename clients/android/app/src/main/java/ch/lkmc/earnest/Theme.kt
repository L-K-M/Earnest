package ch.lkmc.earnest

import android.content.Context
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.platform.LocalView
import androidx.core.content.edit

/** The reading themes: system, light, dark, true black and e-ink. */
enum class ThemeSetting(val label: String) {
    SYSTEM("System"),
    LIGHT("Light"),
    DARK("Dark"),
    BLACK("True black"),
    E_INK("E-ink"),
}

private const val THEME_PREFS = "appearance"
private const val THEME_KEY = "theme"

fun Context.themeSetting(): ThemeSetting {
    val prefs = getSharedPreferences(THEME_PREFS, Context.MODE_PRIVATE)
    val name = prefs.getString(THEME_KEY, ThemeSetting.SYSTEM.name) ?: ThemeSetting.SYSTEM.name
    return runCatching { ThemeSetting.valueOf(name) }.getOrDefault(ThemeSetting.SYSTEM)
}

fun Context.setThemeSetting(setting: ThemeSetting) {
    getSharedPreferences(THEME_PREFS, Context.MODE_PRIVATE).edit { putString(THEME_KEY, setting.name) }
}

// Earnest's reading palette: a warm paper surface with ink text and the
// wordmark's teal accent in light mode, and a dark ink surface with the cool
// brand highlight in dark mode. The stock Material 3 purple scheme is not
// used anywhere.
internal val LightColors = lightColorScheme(
    primary = Color(0xFF07566E),
    onPrimary = Color(0xFFFFFFFF),
    primaryContainer = Color(0xFFD3E4EA),
    onPrimaryContainer = Color(0xFF0A2A33),
    secondary = Color(0xFF526050),
    onSecondary = Color(0xFFFFFFFF),
    secondaryContainer = Color(0xFFD5E5D3),
    onSecondaryContainer = Color(0xFF1F2B1F),
    tertiary = Color(0xFF7A5A34),
    onTertiary = Color(0xFFFFFFFF),
    background = Color(0xFFF8F6EF),
    onBackground = Color(0xFF282620),
    surface = Color(0xFFF8F6EF),
    onSurface = Color(0xFF282620),
    surfaceVariant = Color(0xFFE4DFD1),
    onSurfaceVariant = Color(0xFF4A463C),
    outline = Color(0xFFB9B5AA),
    outlineVariant = Color(0xFFD8D3C5),
    error = Color(0xFFB3261E),
    onError = Color(0xFFFFFFFF),
)

internal val DarkColors = darkColorScheme(
    primary = Color(0xFF8BD0EF),
    onPrimary = Color(0xFF0A2A33),
    primaryContainer = Color(0xFF07566E),
    onPrimaryContainer = Color(0xFFD3E4EA),
    secondary = Color(0xFFB9C8B4),
    onSecondary = Color(0xFF1F2B1F),
    secondaryContainer = Color(0xFF3A4739),
    onSecondaryContainer = Color(0xFFD5E5D3),
    tertiary = Color(0xFFE8C9A0),
    onTertiary = Color(0xFF3A2A14),
    background = Color(0xFF141413),
    onBackground = Color(0xFFE8E4DA),
    surface = Color(0xFF141413),
    onSurface = Color(0xFFE8E4DA),
    surfaceVariant = Color(0xFF2A2A28),
    onSurfaceVariant = Color(0xFFC8C4B8),
    outline = Color(0xFF5A5A54),
    outlineVariant = Color(0xFF3A3A36),
    error = Color(0xFFFFB4AB),
    onError = Color(0xFF4E1410),
)

/** The bar icon contrast: DARK icons on a light background, LIGHT on a dark one. */
internal enum class BarIcons { DARK, LIGHT }

private const val LIGHT_BACKGROUND_LUMINANCE = 0.5f

/** The reading palette for a setting, falling back to the system when it is SYSTEM. */
internal fun colorScheme(setting: ThemeSetting, systemDark: Boolean): ColorScheme =
    when (setting) {
        ThemeSetting.SYSTEM -> if (systemDark) DarkColors else LightColors
        ThemeSetting.LIGHT -> LightColors
        ThemeSetting.DARK -> DarkColors
        ThemeSetting.BLACK -> DarkColors.copy(
            background = Color.Black,
            surface = Color.Black,
            surfaceVariant = Color(0xFF111111),
            onBackground = Color(0xFFE8E4DA),
            onSurface = Color(0xFFE8E4DA),
        )
        // E-ink is deliberately monochrome and high contrast.
        ThemeSetting.E_INK -> LightColors.copy(
            primary = Color.Black,
            onPrimary = Color.White,
            primaryContainer = Color(0xFFE8E8E8),
            onPrimaryContainer = Color.Black,
            secondary = Color.Black,
            onSecondary = Color.White,
            secondaryContainer = Color(0xFFE8E8E8),
            onSecondaryContainer = Color.Black,
            tertiary = Color.Black,
            onTertiary = Color.White,
            background = Color.White,
            onBackground = Color.Black,
            surface = Color.White,
            onSurface = Color.Black,
            surfaceVariant = Color(0xFFF2F2F2),
            onSurfaceVariant = Color.Black,
            outline = Color.Black,
            outlineVariant = Color(0xFFD0D0D0),
        )
    }

/** Bar icons must contrast with the background the app draws under the bars. */
internal fun barIcons(scheme: ColorScheme): BarIcons =
    if (scheme.background.luminance() >= LIGHT_BACKGROUND_LUMINANCE) BarIcons.DARK else BarIcons.LIGHT

internal fun ComponentActivity.applyBarIcons(icons: BarIcons) {
    // Before Android 15, the manifest theme leaves opaque bars. Use the same
    // edge-to-edge policy on every supported API, then match the app's ink.
    val style = SystemBarStyle.auto(android.graphics.Color.TRANSPARENT, android.graphics.Color.TRANSPARENT) {
        icons == BarIcons.LIGHT
    }
    enableEdgeToEdge(statusBarStyle = style, navigationBarStyle = style)
}

@Composable
fun EarnestTheme(setting: ThemeSetting, content: @Composable () -> Unit) {
    val scheme = colorScheme(setting, isSystemInDarkTheme())
    val view = LocalView.current
    // The app draws edge to edge, and the in-app theme can differ from the
    // system's, so the bars must follow the effective scheme.
    SideEffect {
        if (view.isInEditMode) return@SideEffect
        val activity = view.context as? ComponentActivity ?: return@SideEffect
        activity.applyBarIcons(barIcons(scheme))
    }
    MaterialTheme(colorScheme = scheme, content = content)
}

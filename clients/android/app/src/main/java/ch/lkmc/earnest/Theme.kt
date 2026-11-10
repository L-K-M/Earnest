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
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.staticCompositionLocalOf
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
private const val READER_ZOOM_KEY = "reader_zoom"

fun Context.themeSetting(): ThemeSetting {
    val prefs = getSharedPreferences(THEME_PREFS, Context.MODE_PRIVATE)
    val name = prefs.getString(THEME_KEY, ThemeSetting.SYSTEM.name) ?: ThemeSetting.SYSTEM.name
    return runCatching { ThemeSetting.valueOf(name) }.getOrDefault(ThemeSetting.SYSTEM)
}

fun Context.setThemeSetting(setting: ThemeSetting) {
    getSharedPreferences(THEME_PREFS, Context.MODE_PRIVATE).edit { putString(THEME_KEY, setting.name) }
}

/** The reader's text zoom, device-local like the chrome theme. */
fun Context.readerZoom(): Int =
    getSharedPreferences(THEME_PREFS, Context.MODE_PRIVATE).getInt(READER_ZOOM_KEY, 100)
        .coerceIn(MIN_READER_ZOOM, MAX_READER_ZOOM)

fun Context.setReaderZoom(zoom: Int) {
    getSharedPreferences(THEME_PREFS, Context.MODE_PRIVATE).edit {
        putInt(READER_ZOOM_KEY, zoom.coerceIn(MIN_READER_ZOOM, MAX_READER_ZOOM))
    }
}

// Earnest's reading palette: a warm paper surface with ink text and the
// wordmark's teal accent in light mode, and a dark ink surface with the cool
// brand highlight in dark mode. The warm secondary and tertiary roles come
// from the app icon, so the stock Material 3 purple scheme is not used
// anywhere.
internal val LightColors = lightColorScheme(
    primary = Color(0xFF07566E),
    onPrimary = Color(0xFFFFFFFF),
    primaryContainer = Color(0xFFD3E4EA),
    onPrimaryContainer = Color(0xFF0A2A33),
    secondary = Color(0xFFAF4303),
    onSecondary = Color(0xFFFFFFFF),
    secondaryContainer = Color(0xFFFFDCC8),
    onSecondaryContainer = Color(0xFF3A1500),
    tertiary = Color(0xFF8E3B68),
    onTertiary = Color(0xFFFFFFFF),
    tertiaryContainer = Color(0xFFFFD8EA),
    onTertiaryContainer = Color(0xFF3A0A24),
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
    surfaceContainerLowest = Color(0xFFFFFFFF),
    surfaceContainerLow = Color(0xFFF5F1E8),
    surfaceContainer = Color(0xFFF1ECE1),
    surfaceContainerHigh = Color(0xFFECE6DA),
    surfaceContainerHighest = Color(0xFFE6E0D3),
    surfaceBright = Color(0xFFF8F6EF),
    surfaceDim = Color(0xFFDDD8CC),
    inverseSurface = Color(0xFF32302A),
    inverseOnSurface = Color(0xFFF4F0E6),
    inversePrimary = Color(0xFF8BD0EF),
)

internal val DarkColors = darkColorScheme(
    primary = Color(0xFF8BD0EF),
    onPrimary = Color(0xFF0A2A33),
    primaryContainer = Color(0xFF07566E),
    onPrimaryContainer = Color(0xFFD3E4EA),
    secondary = Color(0xFFFFB68E),
    onSecondary = Color(0xFF552000),
    secondaryContainer = Color(0xFF6E2E07),
    onSecondaryContainer = Color(0xFFFFDCC8),
    tertiary = Color(0xFFDA7FAD),
    onTertiary = Color(0xFF3A0A24),
    tertiaryContainer = Color(0xFF5E2246),
    onTertiaryContainer = Color(0xFFFFD8EA),
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
    surfaceContainerLowest = Color(0xFF0E0E0D),
    surfaceContainerLow = Color(0xFF1A1A18),
    surfaceContainer = Color(0xFF1E1E1C),
    surfaceContainerHigh = Color(0xFF282826),
    surfaceContainerHighest = Color(0xFF33332F),
    surfaceBright = Color(0xFF3A3A36),
    surfaceDim = Color(0xFF141413),
    inverseSurface = Color(0xFFE8E4DA),
    inverseOnSurface = Color(0xFF32302A),
    inversePrimary = Color(0xFF07566E),
)

/**
 * The icon's colours, tuned per theme so each tone reaches the graphics
 * contrast floor on its surfaces. Never used for body text.
 */
internal data class Tones(
    val orange: Color,
    val amber: Color,
    val plum: Color,
    val purple: Color,
    val blue: Color,
    val green: Color,
)

internal val LightTones = Tones(
    orange = Color(0xFFDD5602),
    amber = Color(0xFFAE7705),
    plum = Color(0xFF8E3B68),
    purple = Color(0xFF916E9D),
    blue = Color(0xFF3E8AAB),
    green = Color(0xFF428E87),
)

internal val DarkTones = Tones(
    orange = Color(0xFFFE712E),
    amber = Color(0xFFFFB428),
    plum = Color(0xFFDA7FAD),
    purple = Color(0xFFB38FC0),
    blue = Color(0xFF5AA5C7),
    green = Color(0xFF5EA9A2),
)

/** E-ink resolves every tone to black so the theme stays monochrome. */
private val EInkTones = Tones(
    orange = Color.Black,
    amber = Color.Black,
    plum = Color.Black,
    purple = Color.Black,
    blue = Color.Black,
    green = Color.Black,
)

/** The tones for a setting, falling back to the system when it is SYSTEM. */
internal fun tones(setting: ThemeSetting, systemDark: Boolean): Tones =
    when (setting) {
        ThemeSetting.SYSTEM -> if (systemDark) DarkTones else LightTones
        ThemeSetting.LIGHT -> LightTones
        ThemeSetting.DARK -> DarkTones
        ThemeSetting.BLACK -> DarkTones
        ThemeSetting.E_INK -> EInkTones
    }

internal val LocalTones = staticCompositionLocalOf { LightTones }

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
            surfaceContainerLowest = Color.Black,
            surfaceContainerLow = Color(0xFF0A0A0A),
            surfaceContainer = Color(0xFF111111),
            surfaceContainerHigh = Color(0xFF1A1A1A),
            surfaceContainerHighest = Color(0xFF222222),
            surfaceDim = Color.Black,
            surfaceBright = Color(0xFF2A2A2A),
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
            tertiaryContainer = Color(0xFFE8E8E8),
            onTertiaryContainer = Color.Black,
            background = Color.White,
            onBackground = Color.Black,
            surface = Color.White,
            onSurface = Color.Black,
            surfaceVariant = Color(0xFFF2F2F2),
            onSurfaceVariant = Color.Black,
            outline = Color.Black,
            outlineVariant = Color(0xFFD0D0D0),
            surfaceContainerLowest = Color.White,
            surfaceContainerLow = Color.White,
            surfaceContainer = Color.White,
            surfaceContainerHigh = Color(0xFFF2F2F2),
            surfaceContainerHighest = Color(0xFFE8E8E8),
            surfaceDim = Color(0xFFE8E8E8),
            surfaceBright = Color.White,
            inverseSurface = Color.Black,
            inverseOnSurface = Color.White,
            // A snackbar's action sits on the black inverse surface.
            inversePrimary = Color.White,
        )
    }

/** Bar icons must contrast with the background the app draws under the bars. */
internal fun barIcons(scheme: ColorScheme): BarIcons =
    if (scheme.background.luminance() >= LIGHT_BACKGROUND_LUMINANCE) BarIcons.DARK else BarIcons.LIGHT

/**
 * How much of a destination's tone tints its selected navigation indicator.
 * The light tones sit just above 3:1 on the light containers, so more than
 * 8% would push the selected icon below that floor; the dark tones have room
 * for a stronger tint.
 */
internal fun indicatorBlend(scheme: ColorScheme): Float =
    if (scheme.background.luminance() >= LIGHT_BACKGROUND_LUMINANCE) 0.08f else 0.18f

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
    val systemDark = isSystemInDarkTheme()
    val scheme = colorScheme(setting, systemDark)
    val tone = tones(setting, systemDark)
    val view = LocalView.current
    // The app draws edge to edge, and the in-app theme can differ from the
    // system's, so the bars must follow the effective scheme.
    SideEffect {
        if (view.isInEditMode) return@SideEffect
        val activity = view.context as? ComponentActivity ?: return@SideEffect
        activity.applyBarIcons(barIcons(scheme))
    }
    CompositionLocalProvider(LocalTones provides tone) {
        MaterialTheme(colorScheme = scheme, content = content)
    }
}

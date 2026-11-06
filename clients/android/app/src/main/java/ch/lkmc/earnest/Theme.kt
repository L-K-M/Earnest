package ch.lkmc.earnest

import android.content.Context
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
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

/** Material 3 tonal spot schemes generated from the #07566E seed; light primary and surfaceTint are the wordmark's exact teal. */
private val BrandLight = lightColorScheme(
    primary = Color(0xFF07566E),
    onPrimary = Color(0xFFFFFFFF),
    primaryContainer = Color(0xFFBDE9FF),
    onPrimaryContainer = Color(0xFF004D64),
    secondary = Color(0xFF4D616B),
    onSecondary = Color(0xFFFFFFFF),
    secondaryContainer = Color(0xFFD0E6F2),
    onSecondaryContainer = Color(0xFF354A53),
    tertiary = Color(0xFF5D5B7D),
    onTertiary = Color(0xFFFFFFFF),
    tertiaryContainer = Color(0xFFE3DFFF),
    onTertiaryContainer = Color(0xFF454364),
    background = Color(0xFFF6FAFD),
    onBackground = Color(0xFF171C1F),
    surface = Color(0xFFF6FAFD),
    onSurface = Color(0xFF171C1F),
    surfaceVariant = Color(0xFFDCE4E9),
    onSurfaceVariant = Color(0xFF40484C),
    outline = Color(0xFF70787D),
    outlineVariant = Color(0xFFC0C8CD),
    inverseSurface = Color(0xFF2C3134),
    inverseOnSurface = Color(0xFFEDF1F5),
    inversePrimary = Color(0xFF8BD0EF),
    surfaceTint = Color(0xFF07566E),
    surfaceDim = Color(0xFFD6DBDE),
    surfaceBright = Color(0xFFF6FAFD),
    surfaceContainerLowest = Color(0xFFFFFFFF),
    surfaceContainerLow = Color(0xFFF0F4F8),
    surfaceContainer = Color(0xFFEAEEF2),
    surfaceContainerHigh = Color(0xFFE4E9EC),
    surfaceContainerHighest = Color(0xFFDFE3E7),
)

private val BrandDark = darkColorScheme(
    primary = Color(0xFF8BD0EF),
    onPrimary = Color(0xFF003546),
    primaryContainer = Color(0xFF004D64),
    onPrimaryContainer = Color(0xFFBDE9FF),
    secondary = Color(0xFFB4CAD6),
    onSecondary = Color(0xFF1E333C),
    secondaryContainer = Color(0xFF354A53),
    onSecondaryContainer = Color(0xFFD0E6F2),
    tertiary = Color(0xFFC6C2EA),
    onTertiary = Color(0xFF2E2D4D),
    tertiaryContainer = Color(0xFF454364),
    onTertiaryContainer = Color(0xFFE3DFFF),
    background = Color(0xFF0F1417),
    onBackground = Color(0xFFDFE3E7),
    surface = Color(0xFF0F1417),
    onSurface = Color(0xFFDFE3E7),
    surfaceVariant = Color(0xFF40484C),
    onSurfaceVariant = Color(0xFFC0C8CD),
    outline = Color(0xFF8A9297),
    outlineVariant = Color(0xFF40484C),
    inverseSurface = Color(0xFFDFE3E7),
    inverseOnSurface = Color(0xFF2C3134),
    inversePrimary = Color(0xFF106682),
    surfaceTint = Color(0xFF8BD0EF),
    surfaceDim = Color(0xFF0F1417),
    surfaceBright = Color(0xFF353A3D),
    surfaceContainerLowest = Color(0xFF0A0F11),
    surfaceContainerLow = Color(0xFF171C1F),
    surfaceContainer = Color(0xFF1B2023),
    surfaceContainerHigh = Color(0xFF262B2D),
    surfaceContainerHighest = Color(0xFF303538),
)

@Composable
fun EarnestTheme(setting: ThemeSetting, content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val scheme = when (setting) {
        ThemeSetting.SYSTEM -> if (dark) BrandDark else BrandLight
        ThemeSetting.LIGHT -> BrandLight
        ThemeSetting.DARK -> BrandDark
        ThemeSetting.BLACK -> BrandDark.copy(background = Color.Black, surface = Color.Black, surfaceVariant = Color(0xFF111111))
        ThemeSetting.E_INK -> lightColorScheme(
            primary = Color.Black,
            onPrimary = Color.White,
            background = Color.White,
            onBackground = Color.Black,
            surface = Color.White,
            onSurface = Color.Black,
            surfaceVariant = Color(0xFFF2F2F2),
        )
    }
    MaterialTheme(colorScheme = scheme, content = content)
}

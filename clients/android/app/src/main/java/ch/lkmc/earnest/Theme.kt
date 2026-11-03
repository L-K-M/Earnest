package ch.lkmc.earnest

import android.content.Context
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext

/** The reading themes: system, light, dark, true black and e-ink. */
enum class ThemeSetting { SYSTEM, LIGHT, DARK, BLACK, E_INK }

private const val THEME_PREFS = "appearance"
private const val THEME_KEY = "theme"

fun Context.themeSetting(): ThemeSetting {
    val prefs = getSharedPreferences(THEME_PREFS, Context.MODE_PRIVATE)
    val name = prefs.getString(THEME_KEY, ThemeSetting.SYSTEM.name) ?: ThemeSetting.SYSTEM.name
    return runCatching { ThemeSetting.valueOf(name) }.getOrDefault(ThemeSetting.SYSTEM)
}

fun Context.setThemeSetting(setting: ThemeSetting) {
    getSharedPreferences(THEME_PREFS, Context.MODE_PRIVATE).edit().putString(THEME_KEY, setting.name).apply()
}

@Composable
fun EarnestTheme(content: @Composable () -> Unit) {
    val context = LocalContext.current
    val setting = context.themeSetting()
    val dark = isSystemInDarkTheme()
    val scheme = when (setting) {
        ThemeSetting.SYSTEM -> if (dark) darkColorScheme() else lightColorScheme()
        ThemeSetting.LIGHT -> lightColorScheme()
        ThemeSetting.DARK -> darkColorScheme()
        ThemeSetting.BLACK -> darkColorScheme(background = Color.Black, surface = Color.Black, surfaceVariant = Color(0xFF111111))
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

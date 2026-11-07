package ch.lkmc.earnest

import android.content.Context
import androidx.activity.ComponentActivity
import androidx.compose.ui.graphics.Color
import androidx.core.view.WindowCompat
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class ThemeTest {
    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Test fun defaultIsSystem() {
        assertEquals(ThemeSetting.SYSTEM, context.themeSetting())
    }

    @Test fun everySettingRoundTrips() {
        ThemeSetting.entries.forEach { setting ->
            context.setThemeSetting(setting)
            assertEquals(setting, context.themeSetting())
        }
    }

    @Test fun labelsNameTheTheme() {
        assertEquals("True black", ThemeSetting.BLACK.label)
        assertEquals("E-ink", ThemeSetting.E_INK.label)
    }

    @Test fun lightPaletteIsWarmPaperWithInkAndTeal() {
        assertEquals(Color(0xFFF8F6EF), LightColors.background)
        assertEquals(Color(0xFF282620), LightColors.onBackground)
        assertEquals(Color(0xFF07566E), LightColors.primary)
    }

    @Test fun darkPaletteIsInkWithCoolHighlight() {
        assertEquals(Color(0xFF141413), DarkColors.background)
        assertEquals(Color(0xFFE8E4DA), DarkColors.onBackground)
        assertEquals(Color(0xFF8BD0EF), DarkColors.primary)
    }

    @Test fun lightThemesShowDarkBarIcons() {
        assertEquals(BarIcons.DARK, barIcons(colorScheme(ThemeSetting.LIGHT, systemDark = false)))
        assertEquals(BarIcons.DARK, barIcons(colorScheme(ThemeSetting.E_INK, systemDark = false)))
    }

    @Test fun darkThemesShowLightBarIcons() {
        assertEquals(BarIcons.LIGHT, barIcons(colorScheme(ThemeSetting.DARK, systemDark = false)))
        assertEquals(BarIcons.LIGHT, barIcons(colorScheme(ThemeSetting.BLACK, systemDark = false)))
    }

    @Suppress("DEPRECATION") // These colours are configurable only before enforced edge-to-edge.
    @Config(sdk = [29, 34, 35])
    @Test fun windowBarsFollowThemeChanges() {
        val controller = Robolectric.buildActivity(ComponentActivity::class.java)
        controller.get().setTheme(android.R.style.Theme_Material_Light_NoActionBar)
        controller.setup()
        try {
            val activity = controller.get()
            val window = activity.window
            val insets = WindowCompat.getInsetsController(window, window.decorView)
            activity.applyBarIcons(BarIcons.DARK)
            assertTrue(insets.isAppearanceLightStatusBars)
            assertTrue(insets.isAppearanceLightNavigationBars)
            assertEquals(android.graphics.Color.TRANSPARENT, window.statusBarColor)
            assertEquals(android.graphics.Color.TRANSPARENT, window.navigationBarColor)

            activity.applyBarIcons(BarIcons.LIGHT)
            assertFalse(insets.isAppearanceLightStatusBars)
            assertFalse(insets.isAppearanceLightNavigationBars)
        } finally {
            controller.pause().stop().destroy()
        }
    }

    @Test fun systemThemeFollowsSystemDarkMode() {
        assertEquals(BarIcons.DARK, barIcons(colorScheme(ThemeSetting.SYSTEM, systemDark = false)))
        assertEquals(BarIcons.LIGHT, barIcons(colorScheme(ThemeSetting.SYSTEM, systemDark = true)))
    }
}

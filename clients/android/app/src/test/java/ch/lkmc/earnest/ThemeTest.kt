package ch.lkmc.earnest

import android.content.Context
import androidx.activity.ComponentActivity
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.luminance
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

    @Test fun tonesFollowTheSetting() {
        assertEquals(LightTones, tones(ThemeSetting.LIGHT, systemDark = false))
        assertEquals(DarkTones, tones(ThemeSetting.DARK, systemDark = false))
        assertEquals(DarkTones, tones(ThemeSetting.BLACK, systemDark = false))
        val eInk = tones(ThemeSetting.E_INK, systemDark = false)
        listOf(eInk.orange, eInk.amber, eInk.plum, eInk.purple, eInk.blue, eInk.green).forEach {
            assertEquals(Color.Black, it)
        }
    }

    @Test fun systemTonesFollowSystemDarkMode() {
        assertEquals(LightTones, tones(ThemeSetting.SYSTEM, systemDark = false))
        assertEquals(DarkTones, tones(ThemeSetting.SYSTEM, systemDark = true))
    }

    @Test fun lightSchemeLeavesTheStockPurpleAndSageBehind() {
        assertEquals(Color(0xFFF1ECE1), LightColors.surfaceContainer)
        assertEquals(Color(0xFFFFDCC8), LightColors.secondaryContainer)
    }

    /** WCAG 2 relative luminance contrast: (lighter + 0.05) / (darker + 0.05). */
    private fun contrast(a: Color, b: Color): Float {
        val la = a.luminance()
        val lb = b.luminance()
        return (maxOf(la, lb) + 0.05f) / (minOf(la, lb) + 0.05f)
    }

    @Test fun tonesContrastWithTheirSurfaces() {
        listOf(
            LightTones.orange,
            LightTones.amber,
            LightTones.plum,
            LightTones.purple,
            LightTones.blue,
            LightTones.green,
        ).forEach { tone ->
            assertTrue("light tone $tone on background", contrast(tone, LightColors.background) >= 3f)
            assertTrue("light tone $tone on surfaceContainer", contrast(tone, LightColors.surfaceContainer) >= 3f)
            val indicator = lerp(LightColors.surfaceContainer, tone, indicatorBlend(LightColors))
            assertTrue("light tone $tone on its indicator", contrast(tone, indicator) >= 3f)
        }

        listOf(
            DarkTones.orange,
            DarkTones.amber,
            DarkTones.plum,
            DarkTones.purple,
            DarkTones.blue,
            DarkTones.green,
        ).forEach { tone ->
            assertTrue("dark tone $tone on background", contrast(tone, DarkColors.background) >= 4.5f)
            val indicator = lerp(DarkColors.surfaceContainer, tone, indicatorBlend(DarkColors))
            assertTrue("dark tone $tone on its indicator", contrast(tone, indicator) >= 3f)
        }

        assertTrue("keep text on light background", contrast(LightTones.plum, LightColors.background) >= 4.5f)
        assertTrue("keep text on dark background", contrast(DarkTones.plum, DarkColors.background) >= 4.5f)
    }
}

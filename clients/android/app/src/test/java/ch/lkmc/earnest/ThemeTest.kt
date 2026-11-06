package ch.lkmc.earnest

import android.content.Context
import androidx.compose.ui.graphics.Color
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
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
}

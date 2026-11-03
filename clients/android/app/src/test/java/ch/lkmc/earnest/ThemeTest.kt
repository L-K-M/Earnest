package ch.lkmc.earnest

import android.content.Context
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
}

package ch.lkmc.earnest

import androidx.compose.runtime.MutableState
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createComposeRule
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * The section and the open Settings screen must survive the saved-state
 * round trip a rotation or process death performs. Reverting either
 * rememberSaveable call in EarnestApp to a plain remember fails here.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class NavRestorationTest {
    @get:Rule
    val compose = createComposeRule()

    @Test fun sectionAndSettingsSurviveRecreation() {
        val restoration = StateRestorationTester(compose)
        var section: MutableState<View>? = null
        var settings: MutableState<Boolean>? = null
        restoration.setContent {
            section = rememberSection()
            settings = rememberSettingsOpen()
        }
        compose.runOnIdle {
            section?.value = View.SEARCH
            settings?.value = true
        }
        restoration.emulateSavedInstanceStateRestore()
        compose.runOnIdle {
            assertEquals(View.SEARCH, section?.value)
            assertEquals(true, settings?.value)
        }
    }
}

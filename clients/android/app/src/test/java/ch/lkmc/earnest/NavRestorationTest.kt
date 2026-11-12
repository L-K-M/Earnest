package ch.lkmc.earnest

import androidx.compose.runtime.MutableState
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createComposeRule
import ch.lkmc.earnest.core.FrontPagePeriod
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
        var section: MutableState<SectionSelection>? = null
        var settings: MutableState<Boolean>? = null
        restoration.setContent {
            section = rememberSection()
            settings = rememberSettingsOpen()
        }
        compose.runOnIdle {
            section?.value = SectionSelection(View.SEARCH, FrontPagePeriod.WEEKLY)
            settings?.value = true
        }
        restoration.emulateSavedInstanceStateRestore()
        compose.runOnIdle {
            assertEquals(SectionSelection(View.SEARCH, FrontPagePeriod.WEEKLY), section?.value)
            assertEquals(true, settings?.value)
        }
    }

    @Test fun aRestoredWeeklyFrontPageKeepsItsWindow() {
        val restoration = StateRestorationTester(compose)
        var section: MutableState<SectionSelection>? = null
        restoration.setContent { section = rememberSection() }
        compose.runOnIdle { section?.value = SectionSelection(View.FRONT, FrontPagePeriod.WEEKLY) }
        restoration.emulateSavedInstanceStateRestore()
        compose.runOnIdle {
            assertEquals(SectionSelection(View.FRONT, FrontPagePeriod.WEEKLY), section?.value)
        }
    }
}

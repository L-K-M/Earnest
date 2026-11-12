package ch.lkmc.earnest

import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.SaverScope
import ch.lkmc.earnest.core.FrontPagePeriod
import org.junit.Assert.assertEquals
import org.junit.Test

class SectionSaverTest {
    private val scope = SaverScope { true }

    @Test fun everySelectionSurvivesSavedStateRoundTrip() {
        val selections = View.entries.flatMap { view ->
            FrontPagePeriod.entries.map { period -> SectionSelection(view, period) }
        }
        for (selection in selections) {
            val saved = with(SectionSaver) { scope.save(mutableStateOf(selection)) }
            // The persisted format is the enum name and window; changing it
            // would silently reset a reader's section after an app update.
            assertEquals("${selection.view.name}|${selection.period.wireName}", saved)
            assertEquals(selection, SectionSaver.restore(requireNotNull(saved))?.value)
        }
    }

    @Test fun legacyTodayRestoresTheDailyFrontPage() {
        assertEquals(
            SectionSelection(View.FRONT, FrontPagePeriod.DAILY),
            SectionSaver.restore("TODAY")?.value,
        )
    }

    @Test fun legacyEditionRestoresTheWeeklyFrontPage() {
        assertEquals(
            SectionSelection(View.FRONT, FrontPagePeriod.WEEKLY),
            SectionSaver.restore("EDITION")?.value,
        )
    }

    @Test fun legacyListViewsRestoreWithTheDefaultWindow() {
        assertEquals(SectionSelection(View.LATER), SectionSaver.restore("LATER")?.value)
        assertEquals(SectionSelection(View.SEARCH), SectionSaver.restore("SEARCH")?.value)
    }

    @Test fun anUnknownSelectionRestoresTheDailyFrontPage() {
        assertEquals(
            SectionSelection(View.FRONT, FrontPagePeriod.DAILY),
            SectionSaver.restore("RetiredSection")?.value,
        )
    }

    @Test fun aMalformedWindowFallsBackToDaily() {
        assertEquals(SectionSelection(View.FRONT), SectionSaver.restore("FRONT|fortnight")?.value)
        assertEquals(SectionSelection(View.FRONT), SectionSaver.restore("")?.value)
    }
}

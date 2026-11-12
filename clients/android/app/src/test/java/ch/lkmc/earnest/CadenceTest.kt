package ch.lkmc.earnest

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class CadenceTest {
    @Test fun labelsNameTheThreeCadences() {
        assertEquals("Now", Cadence.NOW.label)
        assertEquals("Today", Cadence.TODAY.label)
        assertEquals("This week", Cadence.WEEK.label)
    }

    @Test fun hintsExplainThatCadenceDoesNotFilterTheFrontPage() {
        Cadence.entries.forEach { assertTrue(it.hint.isNotBlank()) }
        assertTrue(Cadence.WEEK.hint.contains("front page", ignoreCase = true))
    }

    @Test fun onlyTheCurrentCadenceIsSelected() {
        val selection = cadenceSelection("week")
        assertEquals(setOf(Cadence.WEEK), selection.filterValues { it }.keys)
        assertEquals(3, selection.size)
    }

    @Test fun missingOrUnknownCadenceSelectsToday() {
        assertEquals(Cadence.TODAY, cadenceOf(null))
        assertEquals(Cadence.TODAY, cadenceOf(""))
        assertEquals(Cadence.TODAY, cadenceOf("daily"))
        assertTrue(cadenceSelection(null)[Cadence.TODAY] == true)
    }

    @Test fun eachCadenceValueRoundTrips() {
        Cadence.entries.forEach { assertEquals(it, cadenceOf(it.value)) }
    }
}

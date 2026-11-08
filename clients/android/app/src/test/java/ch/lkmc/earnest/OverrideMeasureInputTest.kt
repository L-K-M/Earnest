package ch.lkmc.earnest

import org.junit.Assert.*
import org.junit.Test

class OverrideMeasureInputTest {
    @Test fun typingValuesDigitByDigitRetainsIntermediateText() {
        for (value in listOf("62", "66", "72")) {
            var input = OverrideMeasureInput.fromValue(null)
            value.indices.forEach { index ->
                val partial = value.take(index + 1)
                input = input.edit(partial)
                assertEquals(partial, input.text)
            }
            assertEquals(value.toLong(), input.measure)
            assertTrue(input.canApply)
        }
    }

    @Test fun invalidNumbersCannotSilentlyApplyAsInheritance() {
        for (text in listOf("6", "61", "73", "100", "bad", "1.5")) {
            val input = OverrideMeasureInput.fromValue(66).edit(text)
            assertEquals(text, input.text)
            assertNull(input.measure)
            assertFalse("invalid input must not become a blank inherited override", input.canApply)
        }
    }

    @Test fun clearingTheFieldExplicitlyInherits() {
        val input = OverrideMeasureInput.fromValue(66).edit("")
        assertEquals("", input.text)
        assertNull(input.measure)
        assertTrue(input.canApply)
    }

    @Test fun existingMeasuresRemainEditable() {
        val input = OverrideMeasureInput.fromValue(66)
        assertEquals("66", input.text)
        assertEquals(66L, input.measure)
        assertTrue(input.canApply)
    }
}

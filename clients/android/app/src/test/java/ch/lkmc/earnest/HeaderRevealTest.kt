package ch.lkmc.earnest

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class HeaderRevealTest {
    @Test fun headersInsertedAboveAnUnscrolledListAreRevealed() {
        // Two header items arrived above the first entry, which still sits at the top.
        assertTrue(shouldRevealHeaders(firstVisibleIndex = 2, firstVisibleOffset = 0, headerCount = 2))
    }

    @Test fun aReaderWhoScrolledIsLeftWhereTheyAre() {
        assertFalse(shouldRevealHeaders(firstVisibleIndex = 5, firstVisibleOffset = 0, headerCount = 2))
        assertFalse(shouldRevealHeaders(firstVisibleIndex = 2, firstVisibleOffset = 40, headerCount = 2))
    }

    @Test fun nothingToRevealWithoutHeadersOrWhenAlreadyAtTheTop() {
        assertFalse(shouldRevealHeaders(firstVisibleIndex = 0, firstVisibleOffset = 0, headerCount = 0))
        assertFalse(shouldRevealHeaders(firstVisibleIndex = 0, firstVisibleOffset = 0, headerCount = 2))
    }
}

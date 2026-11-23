package ch.lkmc.earnest

import androidx.compose.ui.graphics.Color
import org.junit.Assert.assertEquals
import org.junit.Test

class FeedAvatarTest {
    // Expected values come from running the web client's feedSwatch, so one
    // feed keeps one colour on both clients.
    @Test fun swatchesMatchTheWebClient() {
        mapOf(
            "The Morning Ledger" to 1,
            "Field Notes" to 6,
            "The Quiet Engineer" to 3,
            "Kitchen Science" to 2,
            "Night Sky Weekly" to 5,
            "Ars Botanica" to 2,
            "" to 0,
        ).forEach { (title, swatch) -> assertEquals(title, swatch, feedSwatch(title)) }
    }

    @Test fun theSwatchOrderMatchesTheWebStylesheet() {
        assertEquals(
            listOf(0xFFDD5602, 0xFFAE7705, 0xFFB23A1A, 0xFF8E3B68, 0xFF916E9D, 0xFF3E8AAB, 0xFF428E87).map { Color(it) },
            FeedSwatches,
        )
    }

    @Test fun theLetterIsTheFirstWholeCharacter() {
        assertEquals("T", feedLetter("the Morning Ledger"))
        assertEquals("Ü", feedLetter("  ünter"))
        assertEquals("🦊", feedLetter("🦊 Den"))
        assertEquals("?", feedLetter("   "))
    }
}

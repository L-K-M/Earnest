package ch.lkmc.earnest

import ch.lkmc.earnest.core.CachedEntry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.ZoneId

class ReaderHeaderTest {
    private val zone = ZoneId.of("UTC")

    @Test fun publishedDateIsShownUnderTheTitle() {
        val entry = CachedEntry(1, title = "Title", publishedAt = "2026-11-03T12:00:00Z", firstSeenAt = "2026-11-01T00:00:00Z")
        assertEquals("Nov 3, 2026", entryDateLabel(entry, zone))
    }

    @Test fun firstSeenIsTheFallbackDate() {
        val entry = CachedEntry(1, title = "Title", publishedAt = null, firstSeenAt = "2026-11-01T08:30:00Z")
        assertEquals("Nov 1, 2026", entryDateLabel(entry, zone))
    }

    @Test fun missingDatesShowNothing() {
        assertEquals("", entryDateLabel(CachedEntry(1), zone))
        assertEquals("", entryDateLabel(CachedEntry(1, publishedAt = "bad", firstSeenAt = "bad"), zone))
    }

    @Test fun openOriginalUsesTheEntryUrl() {
        val entry = CachedEntry(1, url = "https://example.test/article")
        assertEquals("https://example.test/article", originalUrl(entry))
        assertNull(originalUrl(CachedEntry(1, url = "")))
        assertNull(originalUrl(CachedEntry(1, url = "javascript:alert(1)")))
    }
}

package ch.lkmc.earnest

import org.junit.Assert.assertEquals
import org.junit.Test

class LinksTest {
    @Test fun onlyCanonicalInternalIdsAreAccepted() {
        val cases = mapOf(
            "earnest://entry/42" to 42L,
            "earnest://entry/0" to null,
            "earnest://entry/-1" to null,
            "earnest://entry/01" to null,
            "earnest://entry/42?url=https://example.com" to null,
            "earnest://entry/42/other" to null,
            "earnest://entry/9223372036854775808" to null,
            "https://entry/42" to null,
            "earnest://other/42" to null,
        )
        cases.forEach { (url, expected) -> assertEquals(url, expected, parseEntryLink(url)) }
    }
}

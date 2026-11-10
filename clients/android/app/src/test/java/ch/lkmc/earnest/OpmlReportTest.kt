package ch.lkmc.earnest

import ch.lkmc.earnest.core.OpmlSummary
import org.junit.Assert.assertEquals
import org.junit.Test

class OpmlReportTest {
    @Test fun theSummaryNamesWhatWasImported() {
        assertEquals(
            "Imported 3 feeds and 2 folders.",
            opmlSummaryLine(OpmlSummary(subscriptions = 3, folders = 2, deferred = 0, errors = emptyList())),
        )
        assertEquals(
            "Imported 1 feed.",
            opmlSummaryLine(OpmlSummary(subscriptions = 1, folders = 0, deferred = 2, errors = emptyList())),
        )
        assertEquals(
            "Imported 1 folder.",
            opmlSummaryLine(OpmlSummary(subscriptions = 0, folders = 1, deferred = 0, errors = emptyList())),
        )
    }

    @Test fun anEmptyImportSaysSo() {
        assertEquals(
            "Nothing was imported.",
            opmlSummaryLine(OpmlSummary(subscriptions = 0, folders = 0, deferred = 0, errors = emptyList())),
        )
    }

    @Test fun failuresAreCountedAlongsideTheImports() {
        assertEquals(
            "Imported 1 feed. 2 outlines failed.",
            opmlSummaryLine(OpmlSummary(subscriptions = 1, folders = 0, deferred = 0, errors = listOf("a", "b"))),
        )
        assertEquals(
            "Nothing was imported. 1 outline failed.",
            opmlSummaryLine(OpmlSummary(subscriptions = 0, folders = 0, deferred = 0, errors = listOf("a"))),
        )
    }
}

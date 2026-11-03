package ch.lkmc.earnest

import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.SyncSnapshot
import org.junit.Assert.assertEquals
import org.junit.Test

class WidgetDataTest {
    private fun entry(id: Long, seq: Long, deleted: Boolean = false) =
        CachedEntry(id = id, seq = seq, title = "Headline $id", deleted = deleted)

    @Test fun newestLiveHeadlinesComeFirst() {
        val snapshot = SyncSnapshot(
            Revision("g1", 1),
            entries = listOf(entry(1, 1), entry(2, 2), entry(3, 3), entry(4, 4), entry(5, 5), entry(6, 6)),
        )
        assertEquals(listOf(6L, 5L, 4L, 3L, 2L), WidgetData.headlines(snapshot).map { it.id })
    }

    @Test fun deletedEntriesAreSkipped() {
        val snapshot = SyncSnapshot(
            Revision("g1", 1),
            entries = listOf(
                entry(1, 1),
                entry(2, 2),
                entry(3, 3, deleted = true),
                entry(4, 4),
                entry(5, 5),
                entry(6, 6),
                entry(7, 7),
            ),
        )
        assertEquals(listOf(7L, 6L, 5L, 4L, 2L), WidgetData.headlines(snapshot).map { it.id })
    }

    @Test fun fewerThanFiveReturnsEverything() {
        val snapshot = SyncSnapshot(Revision("g1", 1), entries = listOf(entry(1, 1), entry(2, 2)))
        assertEquals(listOf(2L, 1L), WidgetData.headlines(snapshot).map { it.id })
    }

    @Test fun emptySnapshotReturnsNoHeadlines() {
        assertEquals(emptyList<CachedEntry>(), WidgetData.headlines(SyncSnapshot(Revision("g1", 1))))
    }
}

package ch.lkmc.earnest

import org.junit.Assert.*
import org.junit.Test

class PassiveSeenTest {
    private fun PassiveSeenTracker.observe(viewport: SeenViewport) = observe(viewport, scrolling = true)

    @Test fun layoutChangesDoNotMarkRowsAndResetTheVisibleBaseline() {
        val tracker = PassiveSeenTracker()
        tracker.observe(SeenViewport(0, mapOf(1L to 0, 2L to 1)), scrolling = false)
        assertTrue(tracker.observe(SeenViewport(1, mapOf(2L to 1, 3L to 2)), scrolling = false).isEmpty())
        assertEquals(setOf(2L), tracker.observe(SeenViewport(2, mapOf(3L to 2)), scrolling = true))
    }

    @Test fun rowsThatPreviouslyLeftTheBottomAreNotMarkedOnLaterScrolls() {
        val tracker = PassiveSeenTracker()
        tracker.observe(SeenViewport(0, mapOf(1L to 0, 2L to 1)))
        tracker.observe(SeenViewport(0, mapOf(1L to 0)), scrolling = false)
        assertEquals(setOf(1L), tracker.observe(SeenViewport(3, mapOf(4L to 3))))
    }

    @Test fun rowsVisibleOnFirstCompositionStayUnseen() {
        val tracker = PassiveSeenTracker()
        val passed = tracker.observe(SeenViewport(firstVisibleIndex = 0, visible = mapOf(1L to 0, 2L to 1)))
        assertTrue("first composition must not mark rows seen", passed.isEmpty())
    }

    @Test fun composedRowsAreSeenWhenTheyLeaveThroughTheTop() {
        val tracker = PassiveSeenTracker()
        tracker.observe(SeenViewport(firstVisibleIndex = 0, visible = mapOf(1L to 0, 2L to 1, 3L to 2)))
        assertEquals(
            setOf(1L, 2L, 3L),
            tracker.observe(SeenViewport(firstVisibleIndex = 3, visible = mapOf(4L to 3, 5L to 4))),
        )
    }

    @Test fun rowsThatLeaveThroughTheBottomStayUnseen() {
        val tracker = PassiveSeenTracker()
        tracker.observe(SeenViewport(firstVisibleIndex = 0, visible = mapOf(1L to 0, 2L to 1, 3L to 2)))
        tracker.observe(SeenViewport(firstVisibleIndex = 2, visible = mapOf(3L to 2, 4L to 3)))
        val passed = tracker.observe(SeenViewport(firstVisibleIndex = 0, visible = mapOf(1L to 0, 2L to 1)))
        assertTrue("rows below the viewport did not leave through the top", passed.isEmpty())
    }

    @Test fun rowsNeverComposedStayUnseen() {
        val tracker = PassiveSeenTracker()
        tracker.observe(SeenViewport(firstVisibleIndex = 0, visible = mapOf(1L to 0)))
        assertEquals(
            setOf(1L),
            tracker.observe(SeenViewport(firstVisibleIndex = 3, visible = mapOf(4L to 3))),
        )
    }

    @Test fun rowsAreReportedOnlyOnce() {
        val tracker = PassiveSeenTracker()
        tracker.observe(SeenViewport(firstVisibleIndex = 0, visible = mapOf(1L to 0, 2L to 1)))
        assertEquals(setOf(1L, 2L), tracker.observe(SeenViewport(firstVisibleIndex = 2, visible = mapOf(3L to 2))))
        assertTrue(tracker.observe(SeenViewport(firstVisibleIndex = 2, visible = mapOf(3L to 2))).isEmpty())
    }

    @Test fun headerIndicesCountTowardsTheTopEdge() {
        val tracker = PassiveSeenTracker()
        tracker.observe(SeenViewport(firstVisibleIndex = 1, visible = mapOf(1L to 1, 2L to 2)))
        assertEquals(
            setOf(1L),
            tracker.observe(SeenViewport(firstVisibleIndex = 2, visible = mapOf(2L to 2, 3L to 3))),
        )
    }
}

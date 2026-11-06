package ch.lkmc.earnest

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class SwipeActionTest {
    @Test fun releaseBelowTheLaterThresholdDoesNothing() {
        assertNull(swipeTargetFor(0f, 96f, 240f))
        assertNull(swipeTargetFor(95.9f, 96f, 240f))
    }

    @Test fun aShortSwipeRevealsLater() {
        assertEquals(SwipeTarget.LATER, swipeTargetFor(96f, 96f, 240f))
        assertEquals(SwipeTarget.LATER, swipeTargetFor(239f, 96f, 240f))
    }

    @Test fun aLongSwipeRevealsDone() {
        assertEquals(SwipeTarget.DONE, swipeTargetFor(240f, 96f, 240f))
    }

    @Test fun theSnackbarNamesTheAction() {
        assertEquals("Later", SwipeTarget.LATER.label)
        assertEquals("Done", SwipeTarget.DONE.label)
        assertEquals("Moved to Later", SwipeTarget.LATER.message)
        assertEquals("Moved to Done", SwipeTarget.DONE.message)
    }
}

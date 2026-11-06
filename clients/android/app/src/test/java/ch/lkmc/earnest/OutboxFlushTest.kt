package ch.lkmc.earnest

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class OutboxFlushTest {
    @Test fun flushRunsShortlyAfterTheRequest() = runTest {
        var flushes = 0
        val scheduler = DebouncedFlush(this, 1500) { flushes++ }
        scheduler.request()
        testScheduler.advanceTimeBy(1499)
        assertEquals(0, flushes)
        testScheduler.advanceUntilIdle()
        assertEquals(1, flushes)
    }

    @Test fun aBurstCollapsesIntoOneFlush() = runTest {
        var flushes = 0
        val scheduler = DebouncedFlush(this, 1500) { flushes++ }
        repeat(5) {
            scheduler.request()
            testScheduler.advanceTimeBy(400)
        }
        assertEquals("an earlier request flushed before the last one", 0, flushes)
        testScheduler.advanceUntilIdle()
        assertEquals(1, flushes)
    }

    @Test fun cancelStopsThePendingFlush() = runTest {
        var flushes = 0
        val scheduler = DebouncedFlush(this, 1500) { flushes++ }
        scheduler.request()
        scheduler.cancel()
        testScheduler.advanceTimeBy(2000)
        assertEquals(0, flushes)
    }
}

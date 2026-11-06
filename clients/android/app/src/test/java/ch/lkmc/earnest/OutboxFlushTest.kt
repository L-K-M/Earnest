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

    @Test fun aNewRequestDoesNotCancelAnInFlightFlush() = runTest {
        val flushEntered = kotlinx.coroutines.CompletableDeferred<Unit>()
        val releaseFlush = kotlinx.coroutines.CompletableDeferred<Unit>()
        val counter = java.util.concurrent.atomic.AtomicInteger()
        val completed = mutableListOf<Int>()
        val scheduler = DebouncedFlush(this, 1500) {
            val index = counter.incrementAndGet()
            if (index == 1) flushEntered.complete(Unit)
            releaseFlush.await()
            completed += index
        }
        scheduler.request()
        testScheduler.advanceUntilIdle()
        flushEntered.await()
        scheduler.request()
        releaseFlush.complete(Unit)
        testScheduler.advanceUntilIdle()
        assertEquals(listOf(1, 2), completed)
    }
}

package ch.lkmc.earnest

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * Sends the durable outbox shortly after the last request, collapsing a burst
 * of actions into one flush. Cancelling the pending flush never loses intent:
 * the queue is persisted and the periodic sync sends it later.
 */
internal class DebouncedFlush(
    private val scope: CoroutineScope,
    private val delayMillis: Long,
    private val flush: suspend () -> Unit,
) {
    private var job: Job? = null

    fun request() {
        job?.cancel()
        job = scope.launch {
            delay(delayMillis)
            flush()
        }
    }

    fun cancel() {
        job?.cancel()
        job = null
    }
}

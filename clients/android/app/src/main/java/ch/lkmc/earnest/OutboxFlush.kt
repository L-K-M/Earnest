package ch.lkmc.earnest

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

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
            // Once the debounce fires, the send runs to completion: a later request
            // must not abort a batch the server may already be applying. That
            // request schedules its own flush, and the engine lock serialises both.
            withContext(NonCancellable) { flush() }
        }
    }

    fun cancel() {
        job?.cancel()
        job = null
    }
}

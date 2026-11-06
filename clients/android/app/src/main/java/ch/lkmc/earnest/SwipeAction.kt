package ch.lkmc.earnest

/** The action a swipe applies, named for the gesture label and the snackbar. */
enum class SwipeTarget(val label: String, val message: String) {
    LATER("Later", "Moved to Later"),
    DONE("Done", "Moved to Done"),
}

/** A release past [doneThresholdPx] is Done, past [laterThresholdPx] is Later. */
internal fun swipeTargetFor(draggedPx: Float, laterThresholdPx: Float, doneThresholdPx: Float): SwipeTarget? =
    when {
        draggedPx >= doneThresholdPx -> SwipeTarget.DONE
        draggedPx >= laterThresholdPx -> SwipeTarget.LATER
        else -> null
    }

package ch.lkmc.earnest

/** The action a swipe applies, named for the gesture label. */
enum class SwipeTarget(val label: String) {
    LATER("Later"),
    DONE("Done"),
}

/** A release past [doneThresholdPx] is Done, past [laterThresholdPx] is Later. */
internal fun swipeTargetFor(draggedPx: Float, laterThresholdPx: Float, doneThresholdPx: Float): SwipeTarget? =
    when {
        draggedPx >= doneThresholdPx -> SwipeTarget.DONE
        draggedPx >= laterThresholdPx -> SwipeTarget.LATER
        else -> null
    }

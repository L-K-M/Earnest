package ch.lkmc.earnest

/** One viewport observation: the first visible row index and every row on screen. */
internal data class SeenViewport(
    val firstVisibleIndex: Int,
    val visible: Map<Long, Int>,
)

/**
 * Passive seen tracking. A row becomes seen once it has been on screen and
 * then leaves through the top edge; rows composed for the first time, or
 * never composed, stay unseen.
 */
internal class PassiveSeenTracker {
    private var visible = emptyMap<Long, Int>()
    private val passed = mutableSetOf<Long>()

    /** Only scrolling, not layout changes, can move a visible row past the reader. */
    fun observe(viewport: SeenViewport, scrolling: Boolean): Set<Long> {
        val seen = if (scrolling) visible.filter { (id, index) ->
            id !in viewport.visible && index < viewport.firstVisibleIndex && passed.add(id)
        }.keys.toSet() else emptySet()
        visible = viewport.visible
        return seen
    }
}

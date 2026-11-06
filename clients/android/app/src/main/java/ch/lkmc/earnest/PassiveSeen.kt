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
    private val composed = mutableSetOf<Long>()
    private val positions = mutableMapOf<Long, Int>()
    private val passed = mutableSetOf<Long>()

    /** Returns entries that composed and then scrolled out through the top edge. */
    fun observe(viewport: SeenViewport): Set<Long> {
        val seen = composed.filter { id ->
            val index = positions[id]
            id !in viewport.visible && index != null && index < viewport.firstVisibleIndex && passed.add(id)
        }.toSet()
        viewport.visible.forEach { (id, index) ->
            composed += id
            positions[id] = index
        }
        return seen
    }
}

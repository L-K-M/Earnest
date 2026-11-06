package ch.lkmc.earnest

/**
 * Briefs and agent notices load after the first rows. A lazy list keeps its
 * anchor on the first entry when items are inserted above it, so the
 * headers would stay out of view. Reveal them only for a reader who has not
 * scrolled: the first entry still sits exactly at the top.
 */
internal fun shouldRevealHeaders(firstVisibleIndex: Int, firstVisibleOffset: Int, headerCount: Int): Boolean =
    headerCount > 0 && firstVisibleIndex == headerCount && firstVisibleOffset == 0

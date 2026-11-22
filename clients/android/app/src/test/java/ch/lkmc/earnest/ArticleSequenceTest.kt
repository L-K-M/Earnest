package ch.lkmc.earnest

import ch.lkmc.earnest.core.SearchMatch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ArticleSequenceTest {
    private fun sequence(vararg ids: Long, opened: Long = ids.first()) =
        ArticleSequence.around(ids.map { ArticleStep(it) }, ArticleStep(opened))

    @Test fun neighboursFollowTheListOrder() {
        val sequence = sequence(9, 7, 5, opened = 7)
        assertEquals(5L, sequence.neighbour(7, ArticleDirection.NEXT)?.entryId)
        assertEquals(9L, sequence.neighbour(7, ArticleDirection.PREVIOUS)?.entryId)
        assertNull(sequence.neighbour(5, ArticleDirection.NEXT))
        assertNull(sequence.neighbour(9, ArticleDirection.PREVIOUS))
    }

    @Test fun anArticleOutsideTheSequenceHasNoNeighbours() {
        assertNull(sequence(9, 7, 5).neighbour(4, ArticleDirection.NEXT))
        assertNull(sequence(9, 7, 5).neighbour(null, ArticleDirection.NEXT))
        assertNull(ArticleSequence.NONE.neighbour(1, ArticleDirection.NEXT))
    }

    @Test fun unavailableArticlesAreSkipped() {
        assertEquals(5L, sequence(9, 7, 5).neighbour(9, ArticleDirection.NEXT) { it != 7L }?.entryId)
    }

    @Test fun aLongListIsBoundedAroundTheOpenedArticle() {
        val ids = LongArray(10_000) { it + 1L }
        val sequence = sequence(*ids, opened = 4_000)
        assertEquals(STEPS_EACH_WAY * 2 + 1, sequence.steps.size)
        assertEquals(4_001L, sequence.neighbour(4_000, ArticleDirection.NEXT)?.entryId)
        assertEquals(3_999L, sequence.neighbour(4_000, ArticleDirection.PREVIOUS)?.entryId)
    }

    private fun match(id: Long, kind: String, untrusted: Boolean = false) =
        SearchMatch(kind, id, 3, 5, id, "Title $id", "", "", 1.0, untrusted)

    @Test fun searchKeepsItsDisplayedOrderAndEachResultsImagePrivacy() {
        val article = match(1, "entry")
        val note = match(2, "agent_note")
        val untrusted = match(3, "entry", untrusted = true)
        val sequence = listOf(note, article, untrusted).articleSequence(article)
        assertEquals(
            listOf(ArticleStep(1), ArticleStep(3, agentPick = true), ArticleStep(2, agentPick = true)),
            sequence.steps,
        )
    }

    @Test fun anArticleListedTwiceKeepsThePositionThatWasTapped() {
        val note = match(1, "agent_note")
        val sequence = listOf(match(1, "entry"), match(2, "entry"), note).articleSequence(note)
        assertEquals(listOf(ArticleStep(2), ArticleStep(1, agentPick = true)), sequence.steps)
    }
}

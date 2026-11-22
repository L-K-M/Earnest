package ch.lkmc.earnest

import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import ch.lkmc.earnest.core.FrontPage
import ch.lkmc.earnest.core.SearchMatch

internal enum class ArticleDirection(val step: Int, val label: String, val endLabel: String) {
    PREVIOUS(-1, "Previous article", "No previous article"),
    NEXT(1, "Next article", "No next article"),
}

/** [agentPick] keeps click-to-load images for an article an agent surfaced. */
internal data class ArticleStep(val entryId: Long, val agentPick: Boolean = false)

/**
 * The order of the list an article was opened from, frozen at that moment:
 * reading removes rows from an Unread only list, and that must not remove
 * them from behind the reader.
 */
internal class ArticleSequence private constructor(val steps: List<ArticleStep>) {
    operator fun contains(entryId: Long) = steps.any { it.entryId == entryId }

    /** The closest article in [direction] that [available] still allows. */
    fun neighbour(current: Long?, direction: ArticleDirection, available: (Long) -> Boolean = { true }): ArticleStep? {
        var index = steps.indexOfFirst { it.entryId == current }
        if (index < 0) return null
        while (true) {
            index += direction.step
            val step = steps.getOrNull(index) ?: return null
            if (available(step.entryId)) return step
        }
    }

    companion object {
        val NONE = ArticleSequence(emptyList())

        /** Bounded around [opened], because the sequence travels in the saved-state bundle. */
        fun around(ordered: List<ArticleStep>, opened: ArticleStep): ArticleSequence {
            // An article can be listed twice; its position is the one that was tapped.
            val unique = ordered.filter { it.entryId != opened.entryId || it == opened }.distinctBy { it.entryId }
            val index = unique.indexOf(opened)
            if (index < 0) return NONE
            val from = (index - STEPS_EACH_WAY).coerceAtLeast(0)
            val to = (index + STEPS_EACH_WAY + 1).coerceAtMost(unique.size)
            return ArticleSequence(unique.subList(from, to).toList())
        }

        // Entry ids are positive, so the sign can carry agentPick in a plain LongArray.
        val StateSaver: Saver<MutableState<ArticleSequence>, LongArray> = Saver(
            save = { state ->
                val steps = state.value.steps
                LongArray(steps.size) { if (steps[it].agentPick) -steps[it].entryId else steps[it].entryId }
            },
            restore = { saved ->
                mutableStateOf(ArticleSequence(saved.map { ArticleStep(kotlin.math.abs(it), agentPick = it < 0) }))
            },
        )
    }
}

internal const val STEPS_EACH_WAY = 500

/** Stories in page order, each by its headline article, or by [opened] for the story it was tapped in. */
internal fun FrontPage.articleSequence(opened: Long): ArticleSequence {
    val steps = sections.flatMap { section ->
        section.stories.mapNotNull { it.visibleStory { entry -> !entry.deleted } }.map { story ->
            ArticleStep(if (story.coverage.any { it.entry.id == opened }) opened else story.entry.id)
        }
    }
    return ArticleSequence.around(steps, ArticleStep(opened))
}

internal fun SearchMatch.articleStep() = ArticleStep(entryId, agentPick = kind == "agent_note" || untrusted)

/** Search lists publisher articles before agent notes. */
internal fun List<SearchMatch>.articleSequence(opened: SearchMatch): ArticleSequence = ArticleSequence.around(
    (filter { it.kind == "entry" } + filter { it.kind == "agent_note" }).map { it.articleStep() },
    opened.articleStep(),
)

/** Survives recreation, and never carries one account's articles into another. */
@Composable
internal fun rememberArticleSequence(accountKey: String?): MutableState<ArticleSequence> {
    var owner by rememberSaveable { mutableStateOf(accountKey) }
    val sequence = rememberSaveable(saver = ArticleSequence.StateSaver) { mutableStateOf(ArticleSequence.NONE) }
    if (accountKey != null && owner != accountKey) {
        if (owner != null) sequence.value = ArticleSequence.NONE
        owner = accountKey
    }
    return sequence
}

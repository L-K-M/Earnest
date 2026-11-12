package ch.lkmc.earnest

import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.Entry
import ch.lkmc.earnest.core.FrontPageCoverage
import ch.lkmc.earnest.core.FrontPagePeriod
import ch.lkmc.earnest.core.FrontPageStory
import ch.lkmc.earnest.core.Register
import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.SyncEngine
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class FrontPageTest {
    private fun state(seen: String, shelf: String) = State(
        principal = 1,
        entryId = 2,
        seen = Register(JsonPrimitive(seen), 1, "2026-11-03T00:00:00Z", 1, "passive"),
        shelf = Register(JsonPrimitive(shelf), 1, "2026-11-03T00:00:00Z", 1, "passive"),
        kept = Register(JsonPrimitive(false), 1, "2026-11-03T00:00:00Z", 1, "passive"),
        stateRev = 1,
    )

    private fun entry(id: Long, deleted: Boolean = false, state: State = state("unseen", "none")) = Entry(
        id = id,
        subscriptionId = 3,
        feedId = 5,
        seq = id,
        firstSeenAt = "2026-11-03T00:00:00Z",
        title = "Headline $id",
        url = "https://news.test/$id",
        author = "Author",
        rev = 1,
        deleted = deleted,
        state = state,
    )

    private fun coverage(id: Long, deleted: Boolean = false) = FrontPageCoverage(entry(id, deleted), "Source $id")

    private fun story(
        id: String = "story-1",
        entry: Entry = entry(2),
        coverage: List<FrontPageCoverage> = listOf(coverage(2), coverage(3)),
    ) = FrontPageStory(
        id = id,
        entry = entry,
        sourceTitle = "Source 2",
        publishedAt = "2026-11-02T12:00:00Z",
        feedCount = coverage.size,
        excerpt = "Excerpt",
        coverage = coverage,
    )

    @Test fun allArticlesAreNewestFirstAndSkipDeleted() {
        val entries = listOf(
            CachedEntry(1, seq = 1),
            CachedEntry(2, seq = 3),
            CachedEntry(3, seq = 2, deleted = true),
            CachedEntry(4, seq = 4),
        )
        assertEquals(listOf(4L, 2L, 1L), allArticles(entries).map { it.id })
    }

    @Test fun aStoryWithUnseenCoverageIsNotRead() {
        val story = story(coverage = listOf(coverage(2), coverage(3)))
        assertFalse(story.isRead(emptyMap(), 1))
    }

    @Test fun aStoryIsReadWhenAllCoverageIsSeen() {
        val story = story(
            coverage = listOf(
                coverage(2).copy(entry = entry(2, state = state("seen", "none"))),
                coverage(3).copy(entry = entry(3, state = state("seen", "none"))),
            ),
        )
        assertTrue(story.isRead(emptyMap(), 1))
    }

    @Test fun aStoryStaysUnreadWhenOneCoverageArticleIsUnseen() {
        val story = story(
            coverage = listOf(
                coverage(2).copy(entry = entry(2, state = state("seen", "none"))),
                coverage(3).copy(entry = entry(3, state = state("unseen", "none"))),
            ),
        )
        assertFalse(story.isRead(emptyMap(), 1))
    }

    @Test fun aDoneShelfAlsoCountsAsRead() {
        val story = story(
            coverage = listOf(
                coverage(2).copy(entry = entry(2, state = state("unseen", "done"))),
                coverage(3).copy(entry = entry(3, state = state("seen", "none"))),
            ),
        )
        assertTrue(story.isRead(emptyMap(), 1))
    }

    @Test fun localStateWinsOverTheServerProjection() {
        val story = story(coverage = listOf(coverage(2)))
        val locallySeen = mapOf(2L to state("seen", "none"))
        assertTrue(story.isRead(locallySeen, 1))

        val serverSeen = story.copy(
            coverage = listOf(coverage(2).copy(entry = entry(2, state = state("seen", "none")))),
        )
        assertFalse(serverSeen.isRead(mapOf(2L to state("unseen", "none")), 1))
    }

    @Test fun emptyCoverageIsNeverRead() {
        val story = story(coverage = emptyList())
        assertFalse(story.isRead(emptyMap(), 1))
    }

    @Test fun aDeletedRepresentativeFallsBackToTheNewestSurvivingCoverage() {
        val story = story(
            entry = entry(2, deleted = true),
            coverage = listOf(coverage(2, deleted = true), coverage(3)),
        )
        assertEquals(3L, story.representative()?.entry?.id)
    }

    @Test fun theStoryEntryIsTheRepresentativeWhenItSurvives() {
        val story = story(coverage = listOf(coverage(2), coverage(3)))
        assertEquals(2L, story.representative()?.entry?.id)
    }

    @Test fun aStoryWithNoSurvivingCoverageHasNoRepresentative() {
        val story = story(coverage = listOf(coverage(2, deleted = true)))
        assertNull(story.representative())
    }

    @Test fun windowNamesAndLabelsMatchTheTabs() {
        assertEquals("Today", frontPageWindowName(FrontPagePeriod.DAILY))
        assertEquals("Past 24 hours", frontPageWindowLabel(FrontPagePeriod.DAILY))
        assertEquals("This week", frontPageWindowName(FrontPagePeriod.WEEKLY))
        assertEquals("Past 7 days", frontPageWindowLabel(FrontPagePeriod.WEEKLY))
        assertEquals("daily", FrontPagePeriod.DAILY.wireName)
        assertEquals("weekly", FrontPagePeriod.WEEKLY.wireName)
    }

    @Test fun initialStateIsUnseenAndUnshelved() {
        val initial = SyncEngine.initialState(1, 2)
        assertEquals(JsonPrimitive("unseen"), initial.seen.value)
        assertEquals(JsonPrimitive("none"), initial.shelf.value)
    }
}

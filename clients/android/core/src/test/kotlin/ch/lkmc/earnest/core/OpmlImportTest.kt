package ch.lkmc.earnest.core

import org.junit.Assert.assertEquals
import org.junit.Test

class OpmlImportTest {
    @Test
    fun summaryCountsEachOutcome() {
        val results = listOf(
            OpmlResult(path = listOf(1), deferred = true, subscription = null),
            OpmlResult(path = listOf(1, 2), deferred = false, folder = null, error = "invalid feed URL"),
            OpmlResult(path = listOf(3), deferred = false),
        )
        val summary = summarizeOpml(results)
        assertEquals(0, summary.subscriptions)
        assertEquals(0, summary.folders)
        assertEquals(1, summary.deferred)
        assertEquals(listOf("invalid feed URL"), summary.errors)
    }

    @Test
    fun summaryCountsSubscriptionsAndFolders() {
        val subscription = Subscription(
            initialThroughSeq = 0,
            initialFetchPending = false,
            id = 3,
            feedId = 5,
            title = "Feed",
            url = "https://feed.test/rss",
            siteUrl = "",
            folderId = 0,
            cadence = "today",
            extract = "none",
            rev = 1,
            deleted = false,
            `private` = false,
            health = FeedHealth(
                nextFetchAt = "2026-11-10T01:00:00Z",
                consecutiveFailures = 0,
                httpStatus = 200,
                postsPerDay = 1.0,
                firehose = false,
            ),
        )
        val results = listOf(
            OpmlResult(path = emptyList(), subscription = subscription),
            OpmlResult(path = listOf(1), folder = Folder(id = 1, name = "News", rev = 1, deleted = false)),
        )
        val summary = summarizeOpml(results)
        assertEquals(1, summary.subscriptions)
        assertEquals(1, summary.folders)
        assertEquals(0, summary.deferred)
        assertEquals(emptyList<String>(), summary.errors)
    }
}

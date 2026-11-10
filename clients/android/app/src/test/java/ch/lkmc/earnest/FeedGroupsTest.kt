package ch.lkmc.earnest

import ch.lkmc.earnest.core.FeedHealth
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class FeedGroupsTest {
    private fun sub(id: Long, folderId: Long = 0) =
        SyncObject(kind = SyncEngine.OBJECT_SUBSCRIPTION, id = id, folderId = folderId, name = "Feed $id", rev = 1)

    private fun folder(id: Long, name: String) =
        SyncObject(kind = SyncEngine.OBJECT_FOLDER, id = id, name = name, rev = 1)

    private fun health(
        errorClass: String? = null,
        errorMessage: String? = null,
        consecutiveFailures: Long = 0,
        lastAttemptAt: String? = null,
    ) = FeedHealth(
        lastAttemptAt = lastAttemptAt,
        nextFetchAt = "2026-11-10T01:00:00Z",
        errorClass = errorClass,
        errorMessage = errorMessage,
        consecutiveFailures = consecutiveFailures,
        httpStatus = 200,
        postsPerDay = 1.0,
        firehose = false,
    )

    @Test fun withoutFoldersTheListStaysFlat() {
        assertEquals(
            listOf(FeedGroup("unfiled", null, listOf(sub(1), sub(2)))),
            feedGroups(listOf(sub(1), sub(2)), emptyList()),
        )
    }

    @Test fun subscriptionsGroupUnderTheirFoldersByName() {
        val groups = feedGroups(
            listOf(sub(1), sub(2, folderId = 7), sub(3, folderId = 5)),
            listOf(folder(7, "Zoo"), folder(5, "Apple")),
        )
        assertEquals(listOf("unfiled" to "No folder", "folder-5" to "Apple", "folder-7" to "Zoo"), groups.map { it.key to it.title })
        assertEquals(listOf(listOf(1L), listOf(3L), listOf(2L)), groups.map { group -> group.subscriptions.map { it.id } })
    }

    @Test fun folderNamesSortCaseInsensitively() {
        val groups = feedGroups(
            listOf(sub(1, folderId = 5), sub(2, folderId = 6), sub(3, folderId = 7)),
            listOf(folder(5, "apple"), folder(6, "Zoo"), folder(7, "Banana")),
        )
        assertEquals(listOf("apple", "Banana", "Zoo"), groups.map { it.title })
    }

    @Test fun sameNamedFoldersStaySeparateSections() {
        // Reverse folder-id input order: the tie-break is the folder id, not
        // the order subscriptions happen to arrive in.
        val groups = feedGroups(
            listOf(sub(2, folderId = 7), sub(1, folderId = 5)),
            listOf(folder(7, "News"), folder(5, "News")),
        )
        assertEquals(listOf("folder-5", "folder-7"), groups.map { it.key })
    }

    @Test fun aFolderNamedNoFolderKeepsItsOwnSection() {
        val groups = feedGroups(
            listOf(sub(1), sub(2, folderId = 5)),
            listOf(folder(5, "No folder")),
        )
        assertEquals(listOf("unfiled", "folder-5"), groups.map { it.key })
        assertEquals(listOf("No folder", "No folder"), groups.map { it.title })
    }

    @Test fun aSubscriptionWhoseFolderIsGoneIsUnfiled() {
        val groups = feedGroups(listOf(sub(1, folderId = 9)), listOf(folder(5, "Apple")))
        assertEquals(listOf("No folder"), groups.map { it.title })
    }

    @Test fun emptySectionsAreDropped() {
        val groups = feedGroups(listOf(sub(1, folderId = 5)), listOf(folder(5, "Apple")))
        assertEquals(listOf("Apple"), groups.map { it.title })
    }

    @Test fun aHealthyFeedReportsNoProblem() {
        assertNull(feedProblem(null))
        assertNull(feedProblem(health()))
    }

    @Test fun aFailingFetchReportsItsDetailAndLastAttempt() {
        val problem = feedProblem(health(errorMessage = "connection refused", consecutiveFailures = 3, lastAttemptAt = "2026-11-10T00:00:00Z"))
        assertEquals("connection refused", problem?.detail)
        assertEquals(3L, problem?.consecutiveFailures)
        assertEquals("2026-11-10T00:00:00Z", problem?.triedAt)
    }

    @Test fun theErrorClassIsTheDetailWithoutAMessage() {
        assertEquals("HttpError", feedProblem(health(errorClass = "HttpError"))?.detail)
    }

    @Test fun failuresWithoutAnyDetailStillReport() {
        assertEquals("unknown error", feedProblem(health(consecutiveFailures = 2))?.detail)
    }
}

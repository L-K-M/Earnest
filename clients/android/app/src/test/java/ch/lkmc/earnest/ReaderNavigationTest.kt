package ch.lkmc.earnest

import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.SaverScope
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ReaderNavigationTest {
    private fun folder(id: Long, name: String, deleted: Boolean = false) =
        SyncObject(SyncEngine.OBJECT_FOLDER, id, name = name, rev = 1, deleted = deleted)
    private fun feed(id: Long, folderId: Long, deleted: Boolean = false) =
        SyncObject(SyncEngine.OBJECT_SUBSCRIPTION, id, folderId = folderId, name = "Feed $id", rev = 1, deleted = deleted)
    private val objects = listOf(folder(7, "Engineering"), folder(8, "Empty"), folder(9, "Deleted", true), feed(1, 7), feed(2, 7), feed(3, 0), feed(4, 9), feed(5, 7, true))
    private val entries = listOf(2L, 1L, 3L, 4L, 5L).map { CachedEntry(it, subscriptionId = it, seq = it) }

    @Test fun phonesHaveFourPrimaryDestinationsAndShelvesStayAvailable() {
        assertEquals(listOf(View.FRONT, View.ALL, View.KEPT, View.FEEDS), primaryDestinations)
        assertEquals("Articles", View.ALL.navigationLabel)
        assertTrue(View.entries.containsAll(listOf(View.LATER, View.DONE, View.ACTIVITY)))
    }

    @Test fun folderFilteringUsesLiveSubscriptionsAndPreservesListOrder() {
        assertEquals(listOf(2L, 1L), FeedSelection.Folder(7).filter(entries, objects).map { it.id })
        assertEquals(listOf(2L), FeedSelection.Subscription(2).filter(entries, objects).map { it.id })
        assertEquals(entries, FeedSelection.All.filter(entries, objects))
    }

    @Test fun unfiledIncludesSubscriptionsWhoseFolderWasDeleted() {
        assertEquals(listOf(3L, 4L), FeedSelection.Folder(0).filter(entries, objects).map { it.id })
    }

    @Test fun emptyMissingAndDeletedSourcesDoNotBroadenToAllArticles() {
        listOf(FeedSelection.Folder(8), FeedSelection.Folder(9), FeedSelection.Folder(99), FeedSelection.Subscription(5), FeedSelection.Subscription(99)).forEach {
            assertTrue("$it should not leak another source's articles", it.filter(entries, objects).isEmpty())
        }
        assertEquals("Folder unavailable", FeedSelection.Folder(9).title(objects))
        assertEquals("Feed unavailable", FeedSelection.Subscription(5).title(objects))
    }

    @Test fun navigationRetainsEmptyFoldersAndSortsThemByName() {
        val groups = navigationFeedGroups(objects)
        assertEquals(listOf("No folder", "Empty", "Engineering"), groups.map { it.title })
        assertTrue(groups[1].subscriptions.isEmpty())
        assertEquals(listOf(1L, 2L), groups[2].subscriptions.map { it.id })
    }

    @Test fun selectionRoundTripsLargeIdsWithoutConfusingFeedAndFolderNamespaces() {
        val scope = SaverScope { true }
        val largeId = 9_007_199_254_740_993L
        listOf(FeedSelection.All, FeedSelection.Folder(0), FeedSelection.Folder(largeId), FeedSelection.Subscription(largeId)).forEach { selection ->
            val saved = with(FeedSelectionSaver) { scope.save(mutableStateOf<FeedSelection>(selection)) }
            assertEquals(selection, FeedSelectionSaver.restore(requireNotNull(saved))?.value)
        }
        listOf("", "folder:-1", "feed:0", "feed:word", "unknown:7").forEach {
            assertEquals(FeedSelection.All, FeedSelectionSaver.restore(it)?.value)
        }
    }
}

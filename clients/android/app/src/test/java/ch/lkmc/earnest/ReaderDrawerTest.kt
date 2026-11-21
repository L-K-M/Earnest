package ch.lkmc.earnest

import androidx.compose.runtime.MutableState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performClick
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject
import ch.lkmc.earnest.core.UnreadCounts
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ReaderDrawerTest {
    @get:Rule val compose = createComposeRule()

    @Test fun foldersSelectTheirArticlesAndExpandToIndividualFeeds() {
        val selections = mutableListOf<FeedSelection>()
        val objects = listOf(
            SyncObject(SyncEngine.OBJECT_FOLDER, 7, name = "Engineering", rev = 1),
            SyncObject(SyncEngine.OBJECT_SUBSCRIPTION, 3, folderId = 7, name = "Systems journal", rev = 1),
        )
        compose.setContent {
            EarnestTheme(ThemeSetting.LIGHT) {
                ReaderDrawer(View.ALL, FeedSelection.All, objects, onView = {}, onFeed = { selections += it }, onSettings = {})
            }
        }
        compose.onNodeWithTag("reader-drawer").performScrollToNode(hasText("Engineering"))
        compose.onNodeWithText("Engineering").performClick()
        assertEquals(listOf(FeedSelection.Folder(7)), selections)
        compose.onNodeWithContentDescription("Expand Engineering").performClick()
        compose.onNodeWithTag("reader-drawer").performScrollToNode(hasText("Systems journal"))
        compose.onNodeWithText("Systems journal").performClick()
        assertEquals(listOf(FeedSelection.Folder(7), FeedSelection.Subscription(3)), selections)
    }

    @Test fun foldersShowTheirFeedsSummedUnreadCountsAndFeedsShowTheirOwn() {
        val objects = listOf(
            SyncObject(SyncEngine.OBJECT_FOLDER, 7, name = "Engineering", rev = 1),
            SyncObject(SyncEngine.OBJECT_SUBSCRIPTION, 3, folderId = 7, name = "Systems journal", rev = 1),
            SyncObject(SyncEngine.OBJECT_SUBSCRIPTION, 4, folderId = 7, name = "Compiler notes", rev = 1),
            SyncObject(SyncEngine.OBJECT_SUBSCRIPTION, 5, folderId = 7, name = "Quiet blog", rev = 1),
            SyncObject(SyncEngine.OBJECT_SUBSCRIPTION, 6, name = "Unfiled feed", rev = 1),
        )
        val counts = mutableStateOf<UnreadCounts?>(null)
        compose.setContent {
            EarnestTheme(ThemeSetting.LIGHT) {
                ReaderDrawer(View.ALL, FeedSelection.All, objects, onView = {}, onFeed = {}, onSettings = {}, unreadCounts = counts.value)
            }
        }
        compose.onAllNodesWithContentDescription("unread", substring = true, useUnmergedTree = true).assertCountEquals(0)
        compose.runOnIdle {
            counts.value = UnreadCounts(bySubscription = mapOf(3L to 5L, 4L to 7L, 6L to 2L), total = 14)
        }
        compose.onNode(hasText("All articles") and hasContentDescription("14 unread")).assertIsDisplayed()
        compose.onNodeWithTag("reader-drawer").performScrollToNode(hasText("No folder"))
        compose.onNode(hasText("No folder") and hasContentDescription("2 unread")).assertIsDisplayed()
        compose.onNodeWithTag("reader-drawer").performScrollToNode(hasText("Engineering"))
        compose.onNodeWithContentDescription("12 unread", useUnmergedTree = true).assertIsDisplayed()
        compose.onNodeWithContentDescription("Expand Engineering").performClick()
        val drawer = compose.onNodeWithTag("reader-drawer")
        drawer.performScrollToNode(hasText("Systems journal"))
        compose.onNode(hasText("Systems journal") and hasContentDescription("5 unread")).assertIsDisplayed()
        drawer.performScrollToNode(hasText("Compiler notes"))
        compose.onNode(hasText("Compiler notes") and hasContentDescription("7 unread")).assertIsDisplayed()
        drawer.performScrollToNode(hasText("Quiet blog"))
        compose.onAllNodesWithContentDescription("0 unread", useUnmergedTree = true).assertCountEquals(0)
        compose.runOnIdle { counts.value = UnreadCounts() }
        compose.onNodeWithTag("reader-drawer").performScrollToNode(hasText("All articles"))
        compose.onAllNodesWithContentDescription("unread", substring = true, useUnmergedTree = true).assertCountEquals(0)
    }

    @Test fun drawerShowsNoCountsWithoutUnreadCounts() {
        val objects = listOf(
            SyncObject(SyncEngine.OBJECT_FOLDER, 7, name = "Engineering", rev = 1),
            SyncObject(SyncEngine.OBJECT_SUBSCRIPTION, 3, folderId = 7, name = "Systems journal", rev = 1),
        )
        compose.setContent {
            EarnestTheme(ThemeSetting.LIGHT) {
                ReaderDrawer(View.ALL, FeedSelection.All, objects, onView = {}, onFeed = {}, onSettings = {})
            }
        }
        compose.onNodeWithContentDescription("Expand Engineering").performClick()
        compose.onNodeWithTag("reader-drawer").performScrollToNode(hasText("Systems journal"))
        compose.onAllNodesWithContentDescription("unread", substring = true, useUnmergedTree = true).assertCountEquals(0)
    }

    @Test fun secondaryShelvesRemainReachableFromTheDrawer() {
        val selected = mutableListOf<View>()
        compose.setContent {
            EarnestTheme(ThemeSetting.LIGHT) {
                ReaderDrawer(View.ALL, FeedSelection.All, emptyList(), onView = { selected += it }, onFeed = {}, onSettings = {})
            }
        }
        listOf("Later", "Done", "Activity").forEach {
            compose.onNodeWithTag("reader-drawer").performScrollToNode(hasText(it))
            compose.onNodeWithText(it).performClick()
        }
        assertEquals(listOf(View.LATER, View.DONE, View.ACTIVITY), selected)
    }

    @Test fun folderSelectionSurvivesRecreationButResetsWhenTheAccountChanges() {
        val restoration = StateRestorationTester(compose)
        val account = mutableStateOf<String?>("account-a")
        var selection: MutableState<FeedSelection>? = null
        restoration.setContent { selection = rememberFeedSelection(account.value) }
        compose.runOnIdle { selection!!.value = FeedSelection.Folder(7) }
        restoration.emulateSavedInstanceStateRestore()
        compose.runOnIdle { assertEquals(FeedSelection.Folder(7), selection!!.value) }
        compose.runOnIdle { account.value = null }
        restoration.emulateSavedInstanceStateRestore()
        compose.runOnIdle { account.value = "account-a" }
        compose.runOnIdle { assertEquals("cache loading must not discard the restored folder", FeedSelection.Folder(7), selection!!.value) }
        compose.runOnIdle { account.value = "account-b" }
        compose.runOnIdle { assertEquals(FeedSelection.All, selection!!.value) }
    }
}

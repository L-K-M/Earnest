package ch.lkmc.earnest

import androidx.activity.OnBackPressedDispatcher
import androidx.activity.compose.LocalOnBackPressedDispatcherOwner
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertHeightIsAtLeast
import androidx.compose.ui.test.assertIsNotDisplayed
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w320dp-h800dp")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class MobileDesignTest {
    @get:Rule val compose = createComposeRule()

    private fun content(fontScale: Float = 1f, body: @Composable () -> Unit) {
        compose.setContent {
            CompositionLocalProvider(LocalDensity provides Density(LocalDensity.current.density, fontScale)) {
                EarnestTheme(ThemeSetting.LIGHT) {
                    Surface(Modifier.width(320.dp)) { body() }
                }
            }
        }
    }

    @Test fun readerToolsStayInsideANarrowPhoneAtLargeFontSize() {
        content(2f) {
            ReaderHeader(
                entry = CachedEntry(1, title = "An article worth reading", url = "https://example.test/article"),
                source = "A favorite publication", isRead = true, offlineCopy = true,
                onOpenOriginal = {}, onCopyLink = {}, onShare = {}, onMarkUnread = {},
                onFind = {}, onSmallerText = {}, onLargerText = {},
            )
        }
        val tools = compose.onNodeWithContentDescription("Reader options")
        tools.assertIsDisplayed()
        assertTrue("reader options must not overflow at 200% text", tools.getUnclippedBoundsInRoot().right <= 320.dp)
        tools.performClick()
        compose.onNodeWithText("Share").assertIsDisplayed()
    }

    @Test fun settingsSaveStaysReachableWhileTheFormScrolls() {
        content(2f) {
            SettingsScreen(
                model = viewModel(),
                state = UiState(snapshot = SyncSnapshot(Revision("generation", 1)), readerSettingsUi = ReaderSettingsUi(loaded = ReaderSettings())),
                onBack = {},
            )
        }
        compose.onNodeWithTag("reader-settings-save").assertIsDisplayed()
        compose.onNodeWithText("LLM services").performScrollTo()
        compose.onNodeWithTag("reader-settings-save").assertIsDisplayed()
    }

    @Test fun connectActionsRemainReachableAtLargeFontSize() {
        content(2f) { LoginScreen(onLoggedIn = {}) }
        compose.onNodeWithText("Start device sign in").performScrollTo().assertIsDisplayed()
    }

    @Test fun feedCadencesWrapWithoutHidingTheLastOption() {
        content(2f) {
            FeedRow(
                subscription = SyncObject(SyncEngine.OBJECT_SUBSCRIPTION, 1, name = "A thoughtful publication", raw = """{"url":"https://example.test/feed","cadence":"day"}""", rev = 1),
                unreadMode = false, unreadCount = null, onCadence = { _, _ -> }, onOpen = {}, onUnsubscribe = {},
            )
        }
        Cadence.entries.forEach { cadence ->
            val chip = compose.onNodeWithText(cadence.label)
            chip.assertIsDisplayed()
            // Allow nonlinear Android font scaling: the 20sp label line
            // still needs 12dp of breathing room inside the chip surface.
            val minimum = with(Density(compose.density.density, 2f)) { 20.sp.toDp() } + 12.dp
            chip.assertHeightIsAtLeast(minimum)
            assertTrue("${cadence.label} must fit on a narrow phone", chip.getUnclippedBoundsInRoot().right <= 320.dp)
        }
        compose.onNodeWithContentDescription("Feed options for A thoughtful publication").assertIsDisplayed()
    }

    @Test fun searchUsesFocusedNavigationAndSystemBackRestoresThePreviousDestination() {
        var back: OnBackPressedDispatcher? = null
        content {
            back = LocalOnBackPressedDispatcherOwner.current?.onBackPressedDispatcher
            MainScaffold(
                model = viewModel(), state = UiState(), theme = ThemeSetting.LIGHT,
                onThemeChange = {}, initialEntry = null, notificationId = null,
                notificationsDenied = false, onRequestNotifications = {},
            )
        }
        compose.onNodeWithText("Feeds").performClick()
        compose.onNodeWithContentDescription("Search").performClick()
        compose.onNodeWithContentDescription("Back").assertIsDisplayed()
        // The closed drawer remains composed off screen, but the bottom
        // navigation must not be displayed during a focused search.
        compose.onNodeWithText("Kept").assertIsNotDisplayed()
        compose.runOnIdle { back!!.onBackPressed() }
        compose.onNodeWithContentDescription("Folders and feeds").assertIsDisplayed()
        compose.onNodeWithText("Your sources").assertIsDisplayed()
        compose.onAllNodesWithText("Kept").assertCountEquals(2)
    }

    @Test fun cancellingASwipeRestoresTheArticleWithoutApplyingAnAction() {
        var actions = 0
        content {
            EntryListScreen(
                title = "All articles", entries = listOf(CachedEntry(1, title = "A headline to swipe")),
                states = emptyMap(), principalId = 1, subscriptionTitles = emptyMap(), subscriptionIcons = emptyMap(),
                briefs = emptyList(), principalNames = emptyMap(), entryTitles = emptyMap(), agentChanges = emptyList(), sessionChanges = emptyList(), agentChangesError = null,
                onRevert = {}, onOpen = {}, onOpenAgentSelected = {}, onLater = { actions++ }, onDone = { actions++ }, onSeen = {}, onKeep = {}, onUnkeep = {}, onMarkUnread = {},
                layoutOverride = ArticleLayout.HEADLINES,
            )
        }
        val headline = compose.onNodeWithTag("headline-1", useUnmergedTree = true)
        val original = headline.getUnclippedBoundsInRoot().left
        headline.performTouchInput {
            down(center)
            moveBy(Offset(-100f, 0f))
            moveBy(Offset(-20f, 0f))
            cancel()
        }
        compose.waitForIdle()
        assertEquals("a cancelled gesture must restore the row", original.value, headline.getUnclippedBoundsInRoot().left.value, 1f)
        assertEquals(0, actions)
    }
}

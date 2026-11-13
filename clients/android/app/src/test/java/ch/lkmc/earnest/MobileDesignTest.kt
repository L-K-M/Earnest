package ch.lkmc.earnest

import androidx.activity.OnBackPressedDispatcher
import androidx.activity.compose.LocalOnBackPressedDispatcherOwner
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assertHeightIsAtLeast
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.assertIsNotDisplayed
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isPopup
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.core.SearchMatch
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

    private fun content(fontScale: Float = 1f, width: Dp = 320.dp, body: @Composable () -> Unit) {
        compose.setContent {
            CompositionLocalProvider(LocalDensity provides Density(LocalDensity.current.density, fontScale)) {
                EarnestTheme(ThemeSetting.LIGHT) {
                    Surface(Modifier.width(width)) { body() }
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

    @Test
    @Config(sdk = [35], qualifiers = "w600dp-h320dp")
    fun allThemesRemainReachableInAShortWindowWithLargeText() {
        var selected: ThemeSetting? = null
        content(2f) { ThemeDialog(ThemeSetting.SYSTEM, {}, { selected = it }) }
        compose.onNodeWithText("E-ink").performScrollTo().performClick()
        compose.runOnIdle { assertEquals(ThemeSetting.E_INK, selected) }
    }

    @Test fun theFeedIconOpensItsSourceAndTheMenuDoesNot() {
        var opened = 0
        content {
            FeedRow(
                subscription = SyncObject(SyncEngine.OBJECT_SUBSCRIPTION, 1, name = "A source", raw = """{"url":"https://example.test/feed"}""", rev = 1),
                unreadMode = false, unreadCount = null, onCadence = { _, _ -> }, onOpen = { opened++ }, onUnsubscribe = {},
            )
        }
        compose.onNodeWithTag("feed-icon-1", useUnmergedTree = true).performTouchInput { down(center); up() }
        compose.runOnIdle { assertEquals(1, opened) }
        compose.onNodeWithContentDescription("Feed options for A source").performClick()
        compose.runOnIdle { assertEquals("opening the menu must not open the source", 1, opened) }
    }

    @Test fun aSettingsSwitchCanBeToggledThroughItsLabel() {
        var checked = false
        content {
            var value by remember { mutableStateOf(false) }
            SwitchRow("Show only unread", "Show only unread", value, { value = it; checked = it })
        }
        compose.onNodeWithText("Show only unread").performClick()
        compose.runOnIdle { assertTrue("the full setting row must be tappable", checked) }
        compose.onNodeWithContentDescription("Show only unread").assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Switch)).assertHeightIsAtLeast(48.dp)
    }

    @Test fun searchFiltersExposeTheirExpandedState() {
        content { SearchScreen(viewModel<AppViewModel>().search, null, SyncSnapshot(Revision("generation", 1)), {}) }
        val filters = compose.onNodeWithText("Filters")
        filters.assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Collapsed"))
        filters.performClick()
        filters.assert(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Expanded"))
    }

    @Test fun settingsLoadsCannotDiscardAnInFlightSave() {
        content {
            SettingsScreen(viewModel(), UiState(snapshot = SyncSnapshot(Revision("generation", 1)), readerSettingsUi = ReaderSettingsUi(loaded = ReaderSettings(), saving = true)), {})
        }
        compose.onAllNodesWithText("Load")[0].assertIsNotEnabled()
        compose.onAllNodesWithText("Reload")[0].assertIsNotEnabled()
    }

    @Test fun importCheckboxCanBeToggledThroughItsLabel() {
        content {
            SettingsScreen(viewModel(), UiState(snapshot = SyncSnapshot(Revision("generation", 1)), readerSettingsUi = ReaderSettingsUi(loaded = ReaderSettings())), {})
        }
        compose.onNodeWithText("Also subscribe to feeds you don't follow").performScrollTo().performClick().assertIsOn().assertHeightIsAtLeast(48.dp)
    }

    @Test fun searchResultsKeepButtonSemanticsAndOpenTheirArticle() {
        var opened: Long? = null
        val note = SearchMatch("agent_note", 9, 1, 1, 1, "A useful note", "https://example.test/article", "A plain-text note", 1.0, true, agentName = "Assistant")
        content { SearchResult(note, null) { opened = it.entryId } }
        compose.onNodeWithText("A useful note").assert(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Button)).performClick()
        compose.runOnIdle { assertEquals(9L, opened) }
    }

    @Test fun reloadingAFailedSaveRequiresAnExplicitDiscard() {
        val settings = ReaderSettings()
        content {
            SettingsScreen(
                model = viewModel(),
                state = UiState(
                    snapshot = SyncSnapshot(Revision("generation", 1)),
                    readerSettingsUi = ReaderSettingsUi(loaded = settings, draft = settings, frozen = FrozenReaderSettings(settings, "retry-operation")),
                ),
                onBack = {},
            )
        }
        compose.onAllNodesWithText("Reload")[0].performClick()
        compose.onNodeWithText("Reload settings?").assertIsDisplayed()
        compose.onNodeWithText("Keep editing").performClick()
        compose.onAllNodesWithText("Retry")[0].assertIsDisplayed()
        compose.onAllNodesWithText("Reload")[0].performClick()
        compose.onNodeWithText("Discard draft").performClick()
        compose.onNodeWithText("Reload settings?").assertDoesNotExist()
    }

    @Test fun closingSettingsStillShowsAnIncomingArticle() {
        var incoming by mutableStateOf<Long?>(null)
        content {
            MainScaffold(
                model = viewModel(),
                state = UiState(snapshot = SyncSnapshot(Revision("generation", 1), entries = listOf(CachedEntry(7, title = "An incoming article")))),
                theme = ThemeSetting.LIGHT, onThemeChange = {}, initialEntry = incoming, notificationId = null,
                notificationsDenied = false, onRequestNotifications = {},
            )
        }
        compose.onNodeWithContentDescription("More options").performClick()
        compose.onNode(hasText("Settings") and hasAnyAncestor(isPopup())).performClick()
        compose.runOnIdle { incoming = 7 }
        compose.onNodeWithContentDescription("Back").performClick()
        compose.onNodeWithContentDescription("Find in article").assertExists()
    }

    @Test fun anIncomingArticleCanOpenWhileCompactSearchIsActive() {
        var incoming by mutableStateOf<Long?>(null)
        content {
            MainScaffold(
                model = viewModel(),
                state = UiState(snapshot = SyncSnapshot(Revision("generation", 1), entries = listOf(CachedEntry(7, title = "An incoming article")))),
                theme = ThemeSetting.LIGHT, onThemeChange = {}, initialEntry = incoming, notificationId = null,
                notificationsDenied = false, onRequestNotifications = {},
            )
        }
        compose.onNodeWithContentDescription("Search").performClick()
        compose.runOnIdle { incoming = 7 }
        // External links navigate from a LaunchedEffect, after the state
        // mutation's frame rather than inside a synchronous click handler.
        compose.waitUntil(5_000) {
            compose.onAllNodesWithContentDescription("Find in article").fetchSemanticsNodes().isNotEmpty()
        }
        compose.onNodeWithContentDescription("Find in article").assertExists()
        compose.onNodeWithContentDescription("Back").performClick()
        compose.onNodeWithText("Find something worth revisiting").assertExists()
    }

    @Test
    @Config(sdk = [35], qualifiers = "w1200dp-h800dp")
    fun wideSearchAndBackKeepTheOpenArticle() {
        var back: OnBackPressedDispatcher? = null
        val title = "An article to return to"
        content(width = 1200.dp) {
            back = LocalOnBackPressedDispatcherOwner.current?.onBackPressedDispatcher
            MainScaffold(
                model = viewModel(),
                state = UiState(snapshot = SyncSnapshot(Revision("generation", 1), entries = listOf(CachedEntry(7, title = title)))),
                theme = ThemeSetting.LIGHT, onThemeChange = {}, initialEntry = null, notificationId = null,
                notificationsDenied = false, onRequestNotifications = {},
            )
        }
        compose.onNodeWithText("Articles").performClick()
        compose.onNodeWithText(title).performClick()
        compose.onNodeWithContentDescription("Find in article").assertExists()
        compose.onNodeWithContentDescription("Search").performClick()
        compose.onNodeWithContentDescription("Find in article").assertExists()
        compose.onNodeWithContentDescription("Back").performClick()
        compose.onNodeWithContentDescription("Find in article").assertExists()
        compose.onNodeWithContentDescription("Search").performClick()
        compose.runOnIdle { checkNotNull(back).onBackPressed() }
        compose.onNodeWithContentDescription("Folders and feeds").assertExists()
        compose.onNodeWithContentDescription("Find in article").assertExists()
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

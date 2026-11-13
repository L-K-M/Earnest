package ch.lkmc.earnest

import android.content.Context
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.Text
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.core.app.ApplicationProvider
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.SyncEngine
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Before
import org.junit.After
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ArticleLayoutTest {
    @get:Rule val compose = createComposeRule()
    private val context get() = ApplicationProvider.getApplicationContext<Context>()
    private val opened = mutableListOf<Long>()
    private val loaded = mutableListOf<Long>()
    private val kept = mutableListOf<Long>()
    private val account = mutableStateOf<String?>("account-a")
    private var failNextLoad = false

    @Before fun resetPreference() { context.getSharedPreferences("appearance", Context.MODE_PRIVATE).edit().clear().commit() }
    @After fun clearPreference() { context.getSharedPreferences("appearance", Context.MODE_PRIVATE).edit().clear().commit() }

    @androidx.compose.runtime.Composable
    private fun list(layoutOverride: ArticleLayout? = null) {
        EarnestTheme(ThemeSetting.LIGHT) {
            EntryListScreen(
                title = "All articles",
                entries = listOf(CachedEntry(1, title = "First headline"), CachedEntry(2, title = "Second headline")),
                states = emptyMap(), principalId = 1, subscriptionTitles = emptyMap(), subscriptionIcons = emptyMap(),
                briefs = emptyList(), principalNames = emptyMap(), entryTitles = emptyMap(), agentChanges = emptyList(),
                sessionChanges = emptyList(), agentChangesError = null, onRevert = {}, onOpenAgentSelected = {},
                onOpen = { opened += it }, onLater = {}, onDone = {}, onSeen = {}, onKeep = { kept += it }, onUnkeep = {}, onMarkUnread = {},
                accountKey = account.value,
                layoutOverride = layoutOverride,
                onLoadContent = {
                    loaded += it
                    if (failNextLoad) {
                        failNextLoad = false
                        throw java.io.IOException("Offline")
                    }
                    ContentUrlResult(null, null, "Article body $it")
                },
                inlineContent = { content, _, _ -> Text(content.cachedHtml.orEmpty()) },
            )
        }
    }

    @Test fun readAndUnreadHeadlinesUseTheSameGutter() {
        val unread = SyncEngine.initialState(1, 1)
        val read = unread.copy(seen = unread.seen.copy(value = JsonPrimitive(SyncEngine.VALUE_SEEN)))
        compose.setContent {
            EarnestTheme(ThemeSetting.LIGHT) {
                Column {
                    EntryRow(CachedEntry(1, title = "Unread headline"), unread, "A feed", onClick = {})
                    EntryRow(CachedEntry(2, title = "Read headline"), read, "A feed", onClick = {})
                }
            }
        }
        val unreadLeft = compose.onNodeWithText("Unread headline", useUnmergedTree = true).fetchSemanticsNode().boundsInRoot.left
        val readLeft = compose.onNodeWithText("Read headline", useUnmergedTree = true).fetchSemanticsNode().boundsInRoot.left
        assertEquals("read status must not indent the headline", readLeft, unreadLeft, 0.5f)
    }

    @Test fun articleActionsHaveADiscoverableMenuInsteadOfASeparateButtonRow() {
        compose.setContent {
            EarnestTheme(ThemeSetting.LIGHT) {
                EntryRow(CachedEntry(1, title = "A headline"), SyncEngine.initialState(1, 1), "A feed", onClick = { opened += 1L }, onKeep = { kept += 1L })
            }
        }
        compose.onNodeWithContentDescription("Article actions").performClick()
        compose.onNodeWithText("Keep").performClick()
        assertEquals(listOf(1L), kept)
        assertTrue("a row action must not also open the article", opened.isEmpty())
    }

    @Test fun readingInFeedLoadsOnlyTheArticleYouExplicitlyExpand() {
        compose.setContent { list() }
        assertTrue(loaded.isEmpty())
        compose.onNodeWithText("First headline").performClick()
        compose.onNodeWithText("Article body 1").assertExists()
        assertEquals(listOf(1L), loaded)
        assertTrue("inline reading must not navigate away", opened.isEmpty())
        compose.onNodeWithText("Second headline").performScrollTo().performClick()
        compose.onNodeWithText("Article body 2").assertExists()
        compose.onNodeWithText("Article body 1").assertDoesNotExist()
        assertEquals(listOf(1L, 2L), loaded)
        compose.onNodeWithText("Collapse").performClick()
        compose.onNodeWithText("Article body 2").assertDoesNotExist()
    }

    @Test fun headlinesOpenTheDedicatedReaderAndTheChoiceIsRemembered() {
        compose.setContent { list() }
        compose.onNodeWithText("Headlines").performClick()
        compose.onNodeWithText("First headline").performClick()
        assertEquals(listOf(1L), opened)
        assertTrue(loaded.isEmpty())
        assertEquals(ArticleLayout.HEADLINES, context.articleLayout())
    }

    @Test fun inlineExpansionSurvivesRotationAndDoesNotCrossAccounts() {
        val restoration = StateRestorationTester(compose)
        restoration.setContent { list() }
        compose.onNodeWithText("First headline").performClick()
        compose.onNodeWithText("Article body 1").assertExists()
        restoration.emulateSavedInstanceStateRestore()
        compose.onNodeWithText("Article body 1").assertExists()
        compose.runOnIdle { account.value = null }
        restoration.emulateSavedInstanceStateRestore()
        compose.runOnIdle { account.value = "account-a" }
        compose.onNodeWithText("Article body 1").assertExists()
        val calls = loaded.size
        compose.runOnIdle { account.value = "account-b" }
        compose.onNodeWithText("Article body 1").assertDoesNotExist()
        assertEquals("changing account must not reopen an old selection", calls, loaded.size)
    }

    @Test fun theFullReaderRemainsAvailableFromAnExpandedArticle() {
        compose.setContent { list() }
        compose.onNodeWithText("First headline").performClick()
        compose.onNodeWithText("Open reader").performClick()
        assertEquals(listOf(1L), opened)
    }

    @Test fun failedInlineLoadsOfferAnExplicitRetry() {
        failNextLoad = true
        compose.setContent { list() }
        compose.onNodeWithText("First headline").performClick()
        compose.onNodeWithText("Offline").assertExists()
        compose.onNodeWithText("Try again").performClick()
        compose.onNodeWithText("Article body 1").assertExists()
        assertEquals(listOf(1L, 1L), loaded)
    }

    @Test fun wideScreensKeepTheSplitReaderInsteadOfInlineExpansion() {
        compose.setContent { list(ArticleLayout.HEADLINES) }
        compose.onNodeWithText("Read in feed").assertDoesNotExist()
        compose.onNodeWithText("First headline").performClick()
        assertEquals(listOf(1L), opened)
        assertTrue(loaded.isEmpty())
        assertEquals("a wide window must not change the phone preference", ArticleLayout.READING, context.articleLayout())
    }

    @Test fun readerToolsFitInOneRowAndRemainReachable() {
        var zooms = 0
        compose.setContent {
            EarnestTheme(ThemeSetting.LIGHT) {
                ReaderHeader(
                    entry = CachedEntry(1, title = "A headline", url = "https://example.test/article"),
                    source = "A long feed name that should not push reader actions off screen", isRead = true, offlineCopy = false,
                    onOpenOriginal = {}, onCopyLink = {}, onShare = {}, onMarkUnread = {}, onFind = {}, onSmallerText = {}, onLargerText = { zooms++ },
                )
            }
        }
        compose.onNodeWithContentDescription("Find in article").assertExists()
        compose.onNodeWithText("Smaller text").assertDoesNotExist()
        compose.onNodeWithContentDescription("Reader options").performClick()
        compose.onNodeWithText("Share").assertExists()
        compose.onNodeWithText("Copy link").assertExists()
        compose.onNodeWithText("Mark unread").assertExists()
        compose.onNodeWithText("Larger text").performClick()
        assertEquals(1, zooms)
    }

    @Test fun retainedHeightOnlyResetsWhenTheReadingLayoutChanges() {
        val state = InlineArticleState()
        val typography = ch.lkmc.earnest.core.ReaderTypography(
            ch.lkmc.earnest.core.ReaderTheme.DARK, 68, ch.lkmc.earnest.core.ReaderFont.SERIF,
        )
        state.configure(1080, typography, 100, 3f, 1f)
        state.reportHeight(2400)
        state.showCachedCopy()
        state.configure(1080, typography, 100, 3f, 1f)
        assertEquals(2400, state.pixels)
        state.configure(0, typography, 100, 3f, 1f)
        assertEquals("an unmeasured lazy item must not erase the retained height", 2400, state.pixels)
        state.reportHeight(0)
        assertEquals(2400, state.pixels)
        state.configure(720, typography, 100, 3f, 1f)
        assertEquals(0, state.pixels)
        assertTrue("resizing must not erase the cached-copy notice", state.offline)
        state.reportHeight(1800)
        state.configure(720, typography, 150, 3f, 1f)
        assertEquals(0, state.pixels)
        state.reportHeight(1600)
        state.configure(720, typography.copy(measure = 62), 150, 3f, 1f)
        assertEquals(0, state.pixels)
        state.reportHeight(1600)
        state.configure(720, typography.copy(measure = 62), 150, 2f, 2f)
        assertEquals(0, state.pixels)
    }

    @Test fun inlineAllocationHasTheSamePixelBudgetOnDenseScreens() {
        listOf(1f, 2f, 3f, 4f).forEach { density ->
            assertEquals(4096, inlineHeightPx(Int.MAX_VALUE, density))
            assertEquals((120 * density).toInt(), inlineHeightPx(0, density))
        }
    }

    @Test fun inlineSizeIsBoundedWithoutChangingTheDeviceZoom() {
        assertEquals(360, inlineHeightPx(0, 3f))
        assertEquals(1201, inlineHeightPx(1201, 3f))
        assertEquals(MAX_INLINE_HEIGHT_PX, inlineHeightPx(Int.MAX_VALUE, 1f))
        context.setReaderZoom(150)
        context.setArticleLayout(ArticleLayout.HEADLINES)
        assertEquals(150, context.readerZoom())
    }
}

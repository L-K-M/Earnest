package ch.lkmc.earnest

import androidx.activity.OnBackPressedDispatcher
import androidx.activity.compose.LocalOnBackPressedDispatcherOwner
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.lifecycle.viewmodel.compose.viewModel
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.SyncSnapshot
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w400dp-h800dp")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class DrawerBackTest {
    @get:Rule val compose = createComposeRule()

    private var back: OnBackPressedDispatcher? = null

    private fun show() {
        compose.setContent {
            back = LocalOnBackPressedDispatcherOwner.current?.onBackPressedDispatcher
            EarnestTheme(ThemeSetting.LIGHT) {
                MainScaffold(
                    model = viewModel(),
                    state = UiState(
                        snapshot = SyncSnapshot(
                            Revision("generation", 1),
                            entries = listOf(CachedEntry(7, title = "An article to expand")),
                        ),
                    ),
                    theme = ThemeSetting.LIGHT, onThemeChange = {}, initialEntry = null, notificationId = null,
                    intentDelivery = 0, notificationsDenied = false, onRequestNotifications = {},
                )
            }
        }
    }

    private fun openDrawer() {
        compose.onNodeWithContentDescription("Folders and feeds").performClick()
        compose.waitForIdle()
        compose.onNodeWithTag("reader-drawer").assertIsDisplayed()
    }

    private fun pressBack() {
        compose.runOnIdle {
            val dispatcher = checkNotNull(back)
            // With no callback to take it, Back would leave the app.
            assertTrue("an open drawer must take Back", dispatcher.hasEnabledCallbacks())
            dispatcher.onBackPressed()
        }
        compose.waitForIdle()
    }

    @Test fun backClosesTheDrawerInsteadOfLeavingTheApp() {
        show()
        openDrawer()
        pressBack()
        compose.onNodeWithTag("reader-drawer").assertIsNotDisplayed()
    }

    @Test fun backClosesTheDrawerBeforeItCollapsesTheArticleBeneath() {
        show()
        compose.onNodeWithText("Articles").performClick()
        compose.onNodeWithText("An article to expand").performClick()
        openDrawer()
        pressBack()
        compose.onNodeWithTag("reader-drawer").assertIsNotDisplayed()
        compose.onNodeWithText("Collapse").assertExists()
    }
}

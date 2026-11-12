package ch.lkmc.earnest

import android.app.Application
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.onLast
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.ReaderSettings
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class SettingsNavigationTest {
    @get:Rule val compose = createComposeRule()

    private fun openSettings(dirty: Boolean) {
        val sessions = object : SessionStore {
            override fun current(): AuthenticatedSession? = null
            override fun clearToken() = Unit
            override fun clearToken(expected: AuthenticatedSession) = false
        }
        val model = AppViewModel(ApplicationProvider.getApplicationContext<Application>(), sessions) {
            error("This navigation fixture must not make network requests")
        }
        val loaded = ReaderSettings()
        val state = UiState(readerSettingsUi = ReaderSettingsUi(
            loaded = loaded,
            draft = if (dirty) loaded.copy(todayDecayDays = 3) else null,
        ))
        compose.setContent {
            EarnestTheme(ThemeSetting.LIGHT) {
                MainScaffold(model, state, ThemeSetting.LIGHT, {}, null, null, false, {})
            }
        }
        compose.onNodeWithContentDescription("More options").performClick()
        compose.onNodeWithText("Settings").performClick()
    }

    @Test fun backFromTheCompleteSettingsScreenProtectsUnsavedChanges() {
        openSettings(dirty = true)
        // The app toolbar is last in the semantics tree and used to bypass
        // the guard owned by the settings screen beneath it.
        compose.onAllNodesWithContentDescription("Back").onLast().performClick()
        compose.onNodeWithText("Discard unsaved changes?").assertIsDisplayed()
        compose.onNodeWithText("Keep editing").performClick()
        compose.onNodeWithText("Reading & appearance").assertIsDisplayed()
        compose.onAllNodesWithContentDescription("Back").onLast().performClick()
        compose.onNode(hasText("Discard") and hasAnyAncestor(isDialog())).performClick()
        compose.onNodeWithContentDescription("Folders and feeds").assertIsDisplayed()
    }

    @Test fun settingsHasOneBackAndNoUnguardedReaderNavigation() {
        openSettings(dirty = false)
        compose.onAllNodesWithContentDescription("Back").assertCountEquals(1)
        compose.onNodeWithContentDescription("Search").assertDoesNotExist()
        compose.onNodeWithText("Kept").assertDoesNotExist()
        compose.onNodeWithContentDescription("Back").performClick()
        compose.onNodeWithText("Discard unsaved changes?").assertDoesNotExist()
        compose.onNodeWithContentDescription("Folders and feeds").assertIsDisplayed()
    }
}

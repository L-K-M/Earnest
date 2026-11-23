package ch.lkmc.earnest

import androidx.compose.material3.Surface
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import ch.lkmc.earnest.core.Activity
import ch.lkmc.earnest.core.ActivityGroup
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w360dp-h800dp")
class ActivityScreenTest {
    @get:Rule val compose = createComposeRule()

    private val group = ActivityGroup(
        principalId = 9,
        principalName = "Morning Agent",
        activities = listOf(
            Activity(
                id = 1, principalId = 9, principalName = "Morning Agent", action = "state.updated",
                target = Json.parseToJsonElement("""{"entry_id":2,"register":"shelf"}"""),
                before = null, after = Json.parseToJsonElement("""{"shelf":{"value":"later"}}"""),
                generation = "g1", createdAt = "2026-11-03T00:00:00Z", nextAction = "revert",
            ),
        ),
    )

    private fun show(groups: List<ActivityGroup>, load: ActivityLoad, onRetry: () -> Unit = {}) {
        compose.setContent {
            EarnestTheme(ThemeSetting.LIGHT) {
                Surface { ActivityScreen(groups, load, mapOf(2L to "A headline"), onRevert = {}, onRetry = onRetry) }
            }
        }
    }

    @Test fun aFailedRefreshKeepsTheLastListAndSaysSo() {
        var retries = 0
        show(listOf(group), ActivityLoad.FAILED) { retries++ }
        compose.onNodeWithText("Could not update. This is the last list that loaded.").assertIsDisplayed()
        compose.onNodeWithText("Morning Agent", substring = true).assertIsDisplayed()
        compose.onNodeWithText("A headline", substring = true).assertIsDisplayed()
        compose.onNodeWithText("Try again").performClick()
        assertEquals(1, retries)
    }

    @Test fun aFirstLoadThatFailsOffersTryAgain() {
        show(emptyList(), ActivityLoad.FAILED)
        compose.onNodeWithText("Activity did not load").assertIsDisplayed()
        compose.onNodeWithText("Try again").assertIsDisplayed()
    }

    @Test fun anAnsweredEmptyListIsQuiet() {
        show(emptyList(), ActivityLoad.LOADED)
        compose.onNodeWithText("All quiet").assertIsDisplayed()
    }
}

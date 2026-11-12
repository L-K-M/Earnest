package ch.lkmc.earnest

import android.app.Application
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.LlmDiscoveredModel
import ch.lkmc.earnest.core.LlmProvider
import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmSettings
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AiSettingsUiTest {
    @get:Rule val compose = createComposeRule()

    private fun show(discoveryError: String? = null) {
        // Network/state transitions have separate AppViewModel tests. This
        // fixture exercises the actual Compose picker at a phone-sized width.
        val sessions = object : SessionStore {
            override fun current(): AuthenticatedSession? = null
            override fun clearToken() = Unit
            override fun clearToken(expected: AuthenticatedSession) = false
        }
        val model = AppViewModel(ApplicationProvider.getApplicationContext<Application>(), sessions) {
            error("This UI fixture must not make network requests")
        }
        val loaded = LlmSettings(revision = 3, providers = listOf(
            LlmProvider("venice", LlmProviderKind.OPENAI, "https://api.venice.ai/api/v1"),
        ))
        val discovery = LlmDiscoveryUi(
            0, loaded.toDraft().providers[0].discoveryFingerprint(), 3, 1,
            models = if (discoveryError == null) listOf(LlmDiscoveredModel(
                id = "text-embedding-qwen3-8b", name = "Qwen3", chat = false, embedding = true,
                embeddingPer1m = "0.012500", dimensions = 4096, supportsCustomDimensions = true,
            )) else null,
            error = discoveryError,
        )
        val state = UiState(llmSettingsUi = LlmSettingsUi(loaded = loaded, loadedRevision = 3, discovery = discovery))
        compose.setContent {
            MaterialTheme {
                Box(Modifier.size(320.dp, 640.dp).verticalScroll(rememberScrollState())) {
                    LlmServicesSection(model, state)
                }
            }
        }
    }

    private fun openEmbeddingPicker() {
        compose.onAllNodesWithText("Choose")[1].performScrollTo().performClick()
        compose.onNodeWithText("Browse venice models").performClick()
    }

    @Test fun choosingADefaultOpensTheProviderCatalogueDirectly() {
        show()
        openEmbeddingPicker()
        compose.onNodeWithText("Qwen3 (text-embedding-qwen3-8b)").assertIsDisplayed()
        compose.onNodeWithText("Use").assertIsEnabled().performClick()
        compose.onNodeWithText("Default embedding model").assertExists()
    }

    @Test fun failedDiscoveryKeepsManualEntryAndUnknownDimensionCheckReachable() {
        show("Discovery is not supported")
        openEmbeddingPicker()
        compose.onNodeWithText("Add model ID manually").performTextInput("manual-embedding")
        compose.onNodeWithText("Add", substring = false).performClick()
        compose.onNodeWithText("Embedding per 1M (USD)").performScrollTo().performTextInput("0.01")
        compose.onNodeWithText("Check model").performScrollTo().assertIsDisplayed().assertIsEnabled()
    }
}

package ch.lkmc.earnest

import androidx.compose.ui.test.hasText
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import android.app.Application
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.performTextClearance
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.LlmDiscoveredModel
import ch.lkmc.earnest.core.LlmDefaultRoute
import ch.lkmc.earnest.core.LlmModel
import ch.lkmc.earnest.core.LlmProvider
import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmSettings
import ch.lkmc.earnest.core.LlmSettingsDefaults
import ch.lkmc.earnest.core.LlmTaskRoute
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

    private fun show(
        discoveryError: String? = null,
        models: List<LlmModel> = emptyList(),
        tasks: List<LlmTaskRoute> = emptyList(),
        defaults: LlmSettingsDefaults? = null,
        baseUrl: String = "https://api.venice.ai/api/v1",
    ): AppViewModel {
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
            LlmProvider("venice", LlmProviderKind.OPENAI, baseUrl),
        ), models = models, tasks = tasks, defaults = defaults)
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
        return model
    }

    private fun openEmbeddingPicker() {
        compose.onAllNodesWithText("Choose")[1].performScrollTo().performClick()
        compose.onNodeWithText("Browse venice models").performClick()
    }

    @Test fun aProviderShowsItsFieldsOnlyWhenSelected() {
        show()
        compose.onNodeWithText("Base URL").assertDoesNotExist()
        compose.onNodeWithText("venice").performScrollTo().performClick()
        compose.onNodeWithText("Base URL").performScrollTo().assertIsDisplayed()
    }

    @Test fun aProviderFoldSaysWhatATapDoes() {
        show()
        compose.onNode(hasText("venice") and hasClickLabel("Expand")).performScrollTo().performClick()
        compose.onNode(hasText("venice") and hasClickLabel("Collapse")).assertExists()
    }

    @Test fun aProviderWithAProblemOffersNoFoldThatCannotWork() {
        show(baseUrl = "")
        compose.onNodeWithText("Base URL").assertExists()
        compose.onNode(SemanticsMatcher.expectValue(SemanticsProperties.StateDescription, "Expanded")).assertDoesNotExist()
    }

    @Test fun choosingADefaultOpensTheProviderCatalogueDirectly() {
        show()
        openEmbeddingPicker()
        compose.onNodeWithText("Qwen3 (text-embedding-qwen3-8b)").assertIsDisplayed()
        compose.onNodeWithText("Use").assertIsEnabled().performClick()
        compose.onNodeWithText("Default embedding model").assertExists()
    }

    @Test fun anExistingTextOnlyModelRequiresInlineCompletionForAnEmbeddingDefault() {
        show(models = listOf(
            LlmModel("text-embedding-qwen3-8b", inputPer1m = "1", outputPer1m = "2", provider = "venice"),
        ))
        openEmbeddingPicker()
        compose.onNodeWithText("Qwen3 (text-embedding-qwen3-8b)").assertIsDisplayed()
        // The saved row has no embedding geometry, so it cannot be reused
        // silently: the row opens the pane instead of offering a quick Use.
        compose.onNodeWithText("Configure").performScrollTo().performClick()
        compose.onNodeWithText("Saved rates per 1M:", substring = true).assertExists()
        compose.onNodeWithText("Use for Embed").performScrollTo().assertIsEnabled().performClick()
        compose.onNodeWithText("Default embedding model").assertExists()
    }

    @Test fun theDefaultTextPickerHidesAnEmbeddingOnlyModel() {
        show(models = listOf(
            LlmModel(
                "vec", inputPer1m = "0", outputPer1m = "0", embeddingPer1m = "0.02",
                dimensions = 1536, truncatable = true, provider = "venice",
            ),
        ))
        compose.onAllNodesWithText("Choose")[0].performScrollTo().performClick()
        compose.onNodeWithText("None").assertExists()
        compose.onNodeWithText("venice/vec").assertDoesNotExist()
        compose.onNodeWithText("Close").performClick()
        // The embedding picker still offers it: the geometry is real.
        compose.onAllNodesWithText("Choose")[1].performScrollTo().performClick()
        compose.onNodeWithText("venice/vec").assertExists()
    }

    @Test fun legacyDefaultRemainsVisibleWhenEditingItsRoute() {
        show(models = listOf(LlmModel("legacy", inputPer1m = "1", outputPer1m = "0", provider = "venice")),
             defaults = LlmSettingsDefaults(text = LlmDefaultRoute("venice", "legacy")))
        compose.onAllNodesWithText("Choose")[0].performScrollTo().performClick()
        compose.onNodeWithText("venice/legacy").assertExists()
    }

    @Test fun legacyCustomModelRemainsVisibleWhenEditingItsRoute() {
        show(models = listOf(LlmModel("legacy", inputPer1m = "1", outputPer1m = "0", provider = "venice")),
             tasks = listOf(LlmTaskRoute("blurb", "venice", "legacy")))
        compose.onNodeWithText("Advanced custom feature routes").performScrollTo().performClick()
        compose.onNodeWithText("Edit custom route").performScrollTo().performClick()
        compose.onNodeWithText("legacy").assertExists()
        compose.onNodeWithText("No models are ready", substring = true).assertDoesNotExist()
    }

    private fun editModelDimensions(model: LlmModel) {
        show(models = listOf(model))
        val pricingToggle = compose.onAllNodesWithText("Edit")
        pricingToggle[pricingToggle.fetchSemanticsNodes().lastIndex].performScrollTo().performClick()
        val modelEdit = compose.onAllNodesWithText("Edit")
        modelEdit[modelEdit.fetchSemanticsNodes().lastIndex].performScrollTo().performClick()
        compose.onNodeWithText("Embedding dimensions (0-1024, wider with truncation or custom width)")
            .performScrollTo().performTextClearance()
    }

    @Test fun editingAChatModelCanLeaveEmbeddingDimensionsBlank() {
        editModelDimensions(LlmModel("chat", inputPer1m = "1", outputPer1m = "2", provider = "venice"))
        compose.onNodeWithText("Apply").assertIsEnabled().performClick()
        compose.onNodeWithText("Edit model").assertDoesNotExist()
    }

    @Test fun editingAChatModelRejectsIncompatibleEmbeddingDimensions() {
        editModelDimensions(LlmModel("chat", inputPer1m = "1", outputPer1m = "2", provider = "venice"))
        compose.onNodeWithText("Embedding dimensions (0-1024, wider with truncation or custom width)")
            .performTextInput("4096")
        compose.onNodeWithText("Apply").assertIsNotEnabled()
        compose.onNodeWithText("Embedding dimensions (0-1024, wider with truncation or custom width)")
            .performTextClearance()
        compose.onNodeWithText("Embedding dimensions (0-1024, wider with truncation or custom width)")
            .performTextInput("1024")
        compose.onNodeWithText("Apply").assertIsEnabled()
    }

    @Test fun editingAnEmbeddingModelRequiresItsWidth() {
        editModelDimensions(LlmModel("embed", embeddingPer1m = "1", dimensions = 1024, provider = "venice"))
        compose.onNodeWithText("Apply").assertIsNotEnabled()
        compose.onNodeWithText("Model embed: embedding dimensions are missing.").assertExists()
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

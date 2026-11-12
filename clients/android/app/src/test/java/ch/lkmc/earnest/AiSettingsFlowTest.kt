package ch.lkmc.earnest

import ch.lkmc.earnest.core.LlmModel
import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmDefaultRoute
import ch.lkmc.earnest.core.LlmSettingsDefaults
import ch.lkmc.earnest.core.LlmTaskMode
import ch.lkmc.earnest.core.LlmTaskRoute
import org.junit.Assert.assertEquals
import org.junit.Test

class AiSettingsFlowTest {
    @Test
    fun disablingAndEnablingPreservesACustomRoute() {
        val defaults = LlmSettingsDefaults(text = LlmDefaultRoute("default-provider", "default-model"))
        val custom = LlmTaskRoute("blurb", "custom-provider", "custom-model", LlmTaskMode.CUSTOM)
        val disabled = toggleTaskRoute(custom, false, defaults)
        val enabled = toggleTaskRoute(disabled, true, defaults)
        assertEquals(LlmTaskMode.CUSTOM, enabled.mode)
        assertEquals(LlmDefaultRoute("custom-provider", "custom-model"), enabled.effectiveRoute(defaults))
    }

    @Test
    fun renamingThroughAnEmptyNameKeepsScopedMetadataAndGlobalFallbacksSeparate() {
        val provider = LlmProviderDraft("venice", LlmProviderKind.OPENAI, "https://api.venice.ai/api/v1")
        val original = LlmSettingsDraft(1, providers = listOf(provider), models = listOf(
            LlmModel(name = "shared", provider = "venice"), LlmModel(name = "shared"),
        ), defaults = LlmSettingsDefaults(text = LlmDefaultRoute("venice", "shared")))
        val cleared = original.editProvider(0, provider.copy(name = ""))
        val renamed = cleared.editProvider(0, cleared.providers[0].copy(name = "renamed"))
        assertEquals(1, renamed.models.count { it.provider.isEmpty() })
        assertEquals(1, renamed.models.count { it.provider == "renamed" })
        assertEquals("renamed", renamed.defaults.text.provider)
    }

    @Test
    fun anEmptyDefaultDoesNotSilentlyTurnInheritanceIntoACustomRoute() {
        val route = LlmTaskRoute("blurb", "previous-provider", "previous-model", LlmTaskMode.DEFAULT)
        val saved = route.forSave(LlmSettingsDefaults())
        assertEquals(LlmTaskMode.DEFAULT, saved.mode)
        assertEquals("", saved.provider)
        assertEquals("", saved.model)
    }

    @Test
    fun togglingAnInheritedRouteKeepsFollowingTheDefault() {
        val previous = LlmTaskRoute("blurb", "old", "old-model", LlmTaskMode.DEFAULT)
        val defaults = LlmSettingsDefaults(text = LlmDefaultRoute("new", "new-model"))
        val enabled = toggleTaskRoute(toggleTaskRoute(previous, false, defaults), true, defaults)
        assertEquals(LlmTaskMode.DEFAULT, enabled.mode)
        assertEquals(defaults.text, enabled.effectiveRoute(defaults))
    }
}

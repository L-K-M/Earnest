package ch.lkmc.earnest

import ch.lkmc.earnest.core.LlmDefaultRoute
import ch.lkmc.earnest.core.LlmModel
import ch.lkmc.earnest.core.LlmProvider
import ch.lkmc.earnest.core.LlmSettings
import ch.lkmc.earnest.core.LlmTask
import ch.lkmc.earnest.core.LlmTaskMode
import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmSettingsDefaults
import ch.lkmc.earnest.core.LlmTaskRoute
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class LlmSettingsDraftTest {
    private val tasks = listOf(
        LlmTaskRoute("blurb", "local", "local-chat"),
        LlmTaskRoute("summary", "other", "other-chat"),
        LlmTaskRoute("ask"),
    )

    @Test
    fun aBlankOldProviderNameCannotClaimDisabledTasks() {
        assertEquals(tasks, retargetProvider(tasks, "", "new-name"))
    }

    @Test
    fun clearingAProviderNameCannotTurnRoutesIntoDisabledSentinels() {
        assertEquals(tasks, retargetProvider(tasks, "local", ""))
    }

    @Test
    fun clearAndRetypeKeepsAssignedRoutesWithoutClaimingDisabledTasks() {
        val original = LlmProviderDraft("local", LlmProviderKind.OPENAI, "http://localhost/v1")
        var draft = LlmSettingsDraft(3, providers = listOf(original), tasks = tasks)
        var lastName = original.name
        for (name in listOf("l", "", "n", "new")) {
            // Like queued UI callbacks, the replacement may carry an older snapshot.
            draft = draft.editProvider(0, original.copy(name = name))
            if (name.isNotEmpty()) lastName = name
            assertEquals(name, draft.providers[0].name)
            assertEquals(LlmTaskRoute("blurb", lastName, "local-chat"), draft.tasks[0])
            assertEquals(tasks[1], draft.tasks[1])
            assertEquals(tasks[2], draft.tasks[2])
        }
    }

    @Test
    fun renamingAProviderChangesOnlyItsAssignedRoutes() {
        val renamed = retargetProvider(tasks, "local", "renamed-local")
        assertEquals(LlmTaskRoute("blurb", "renamed-local", "local-chat"), renamed[0])
        assertEquals(tasks[1], renamed[1])
        assertEquals(tasks[2], renamed[2])
    }

    @Test
    fun collidingProviderNameKeepsItsOwnIdentityUntilNameIsUnique() {
        val draft = LlmSettingsDraft(
            revision = 1,
            providers = listOf(
                LlmProviderDraft("local", LlmProviderKind.OPENAI, "http://localhost/v1"),
                LlmProviderDraft("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1"),
            ),
            tasks = listOf(LlmTaskRoute("blurb", "local", "local-chat")),
            models = listOf(LlmModel("local-chat", provider = "local"), LlmModel("gpt", provider = "openai")),
            defaults = LlmSettingsDefaults(text = LlmDefaultRoute("openai", "gpt")),
        )
        val colliding = draft.editProvider(0, draft.providers[0].copy(name = "openai"))
        assertEquals("openai", colliding.providers[0].name)
        assertEquals("local", colliding.providers[0].routedName)
        assertEquals(draft.tasks, colliding.tasks)
        assertEquals(draft.models, colliding.models)
        assertEquals(draft.defaults, colliding.defaults)
        val renamed = colliding.editProvider(0, colliding.providers[0].copy(name = "openai-backup"))
        assertEquals("openai-backup", renamed.tasks[0].provider)
        assertEquals("openai-backup", renamed.models[0].provider)
        assertEquals("openai", renamed.models[1].provider)
        assertEquals(draft.defaults, renamed.defaults)

        val siblingCleared = draft.editProvider(1, draft.providers[1].copy(name = ""))
        val retainedCollision = siblingCleared.editProvider(0, siblingCleared.providers[0].copy(name = "openai"))
        assertEquals("local", retainedCollision.providers[0].routedName)
        assertEquals(siblingCleared.models, retainedCollision.models)
        assertEquals(siblingCleared.tasks, retainedCollision.tasks)
    }

    @Test
    fun removingAnOldCustomProviderKeepsTaskUsingAnotherDefault() {
        val draft = LlmSettingsDraft(
            revision = 1,
            providers = listOf(
                LlmProviderDraft("local", LlmProviderKind.OPENAI, "http://localhost/v1"),
                LlmProviderDraft("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1"),
            ),
            tasks = listOf(LlmTaskRoute("blurb", "local", "old", LlmTaskMode.DEFAULT)),
            defaults = LlmSettingsDefaults(text = LlmDefaultRoute("openai", "gpt")),
        )
        assertEquals(LlmTaskMode.DEFAULT, draft.withoutProvider(0).tasks[0].mode)
    }
}

class LlmSettingsDefaultsDraftTest {
    private val providers = listOf(LlmProviderDraft("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1"))
    private val models = listOf(LlmModel("gpt-5", provider = "openai"))

    private fun settings(
        defaults: LlmSettingsDefaults? = null,
        tasks: List<LlmTaskRoute> = emptyList(),
    ) = LlmSettings(
        revision = 4,
        providers = listOf(LlmProvider("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1")),
        tasks = tasks,
        models = models,
        defaults = defaults,
    )

    @Test
    fun defaultsExistKeepsExplicitModesAndRoutes() {
        val defaults = LlmSettingsDefaults(
            text = LlmDefaultRoute("openai", "gpt-5"),
            embedding = LlmDefaultRoute(),
        )
        val tasks = LlmTask.entries.map { task ->
            when (task) {
                LlmTask.SUMMARY -> LlmTaskRoute("summary", "openai", "gpt-5", LlmTaskMode.DEFAULT)
                LlmTask.EMBED -> LlmTaskRoute("embed", "", "", LlmTaskMode.OFF)
                else -> LlmTaskRoute(task.wireName, "", "", LlmTaskMode.OFF)
            }
        }
        val draft = settings(defaults, tasks).toDraft()
        assertEquals(defaults, draft.defaults)
        assertEquals(LlmTaskMode.DEFAULT, draft.tasks.first { it.task == "summary" }.mode)
        val input = draft.toInput("op-1")
        assertEquals(defaults, input.defaults)
        val summary = input.tasks.first { it.task == "summary" }
        assertEquals("default", summary.mode!!.wireName)
        // The effective route is sent too, for older readers.
        assertEquals("openai", summary.provider)
        assertEquals("gpt-5", summary.model)
        val embed = input.tasks.first { it.task == "embed" }
        assertEquals("off", embed.mode!!.wireName)
        assertEquals("", embed.provider)
    }

    @Test
    fun noDefaultsRetainsExactRoutesAsCustomOrOff() {
        val tasks = LlmTask.entries.map { task ->
            when (task) {
                LlmTask.SUMMARY -> LlmTaskRoute("summary", "openai", "gpt-5")
                else -> LlmTaskRoute(task.wireName, "", "")
            }
        }
        val draft = settings(null, tasks).toDraft()
        assertEquals(LlmSettingsDefaults(), draft.defaults)
        assertEquals(LlmTaskMode.CUSTOM, draft.tasks.first { it.task == "summary" }.mode)
        assertEquals(LlmTaskMode.OFF, draft.tasks.first { it.task == "ask" }.mode)
        // Empty defaults are omitted so the document shape is unchanged.
        assertNull(draft.toInput("op-1").defaults)
        val summary = draft.toInput("op-1").tasks.first { it.task == "summary" }
        assertEquals("custom", summary.mode!!.wireName)
        assertEquals("openai", summary.provider)
    }

    @Test
    fun anEmptyDefaultDoesNotBecomeACustomRoute() {
        val draft = LlmSettingsDraft(
            revision = 1,
            providers = providers,
            tasks = LlmTask.entries.map { LlmTaskRoute(it.wireName, "", "", LlmTaskMode.OFF) },
            models = models,
            defaults = LlmSettingsDefaults(),
        )
        val routed = draft.copy(
            tasks = draft.tasks.map { if (it.task == "summary") LlmTaskRoute("summary", "previous-provider", "previous-model", LlmTaskMode.DEFAULT) else it },
        )
        val saved = routed.toInput("op").tasks.first { it.task == "summary" }
        assertEquals("default", saved.mode!!.wireName)
        assertEquals("", saved.provider)
        assertEquals("", saved.model)
        assertNull(routed.toInput("op").defaults)
    }

    @Test
    fun renamingAProviderMovesScopedModelsAndDefaults() {
        val draft = LlmSettingsDraft(
            revision = 1,
            providers = providers,
            tasks = LlmTask.entries.map { LlmTaskRoute(it.wireName, "", "", LlmTaskMode.OFF) },
            models = listOf(LlmModel("gpt-5", provider = "openai"), LlmModel("global", provider = "")),
            defaults = LlmSettingsDefaults(text = LlmDefaultRoute("openai", "gpt-5")),
        )
        val renamed = draft.editProvider(0, draft.providers[0].copy(name = "azure"))
        assertEquals("azure", renamed.models.first { it.name == "gpt-5" }.provider)
        assertEquals("", renamed.models.first { it.name == "global" }.provider)
        assertEquals("azure", renamed.defaults.text.provider)
    }

    @Test
    fun uniqueProviderNamesAvoidTheDraftsProviders() {
        assertEquals("venice", uniqueProviderName("Venice", emptySet()))
        assertEquals("openai-2", uniqueProviderName("OpenAI", setOf("openai")))
        assertEquals("openai-3", uniqueProviderName("OpenAI", setOf("openai", "openai-2")))
        assertEquals("provider", uniqueProviderName("!!", emptySet()))
    }
}

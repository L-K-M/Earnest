package ch.lkmc.earnest

import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmTaskRoute
import org.junit.Assert.assertEquals
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
}

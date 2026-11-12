package ch.lkmc.earnest

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
    fun renamingAProviderChangesOnlyItsAssignedRoutes() {
        val renamed = retargetProvider(tasks, "local", "renamed-local")
        assertEquals(LlmTaskRoute("blurb", "renamed-local", "local-chat"), renamed[0])
        assertEquals(tasks[1], renamed[1])
        assertEquals(tasks[2], renamed[2])
    }
}

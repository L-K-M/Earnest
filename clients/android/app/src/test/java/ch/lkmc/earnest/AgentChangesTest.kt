package ch.lkmc.earnest

import ch.lkmc.earnest.core.Activity
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test

class AgentChangesTest {
    @Test fun invalidCachedTimestampsDoNotBreakActivityFiltering() {
        val valid = Activity(1, 9, "Agent", "state.updated", null, null, null, "g1", "2026-11-03T00:00:00Z", "revert")
        val changes = listOf(valid.copy(id = 2, createdAt = "bad timestamp"), valid, valid.copy(id = 3, createdAt = "2020-01-01T00:00:00Z"))
        assertEquals(listOf(valid), activitySince(changes, java.time.Instant.parse("2026-11-02T00:00:00Z")))
    }

    @Test fun noticeNamesAgentArticleAndChangedRegisterAsPlainText() {
        val change = Activity(
            1, 9, "<b>Morning Agent</b>", "state.updated",
            Json.parseToJsonElement("""{"entry_id":2,"register":"shelf"}"""),
            null, Json.parseToJsonElement("""{"shelf":{"value":"done"}}"""),
            "g1", "2026-11-03T00:00:00Z", "revert",
        )
        assertEquals(2L, agentChangedEntryId(change))
        assertEquals("<b>Morning Agent</b> moved to Done: Headline", agentChangeText(change, mapOf(2L to "Headline")))
        assertEquals("<b>Morning Agent</b> moved to Done: Entry #2", agentChangeText(change, emptyMap()))
    }
}

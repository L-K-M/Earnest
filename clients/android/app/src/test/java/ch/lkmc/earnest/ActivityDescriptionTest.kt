package ch.lkmc.earnest

import ch.lkmc.earnest.core.Activity
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test

class ActivityDescriptionTest {
    private fun activity(
        action: String,
        target: String? = null,
        before: String? = null,
        after: String? = null,
    ) = Activity(
        id = 1,
        principalId = 9,
        principalName = "Morning Agent",
        action = action,
        target = target?.let { Json.parseToJsonElement(it) },
        before = before?.let { Json.parseToJsonElement(it) },
        after = after?.let { Json.parseToJsonElement(it) },
        generation = "g1",
        createdAt = "2026-11-03T00:00:00Z",
        nextAction = "revert",
    )

    @Test fun stateUpdatesNameTheRegisterAndValue() {
        val later = activity("state.updated", """{"entry_id":2,"register":"shelf"}""", after = """{"shelf":{"value":"later"}}""")
        assertEquals("Moved to Later: Headline", activityDescription(later, mapOf(2L to "Headline")))
        val done = activity("state.updated", """{"entry_id":2,"register":"shelf"}""", after = """{"shelf":{"value":"done"}}""")
        assertEquals("Moved to Done: Headline", activityDescription(done, mapOf(2L to "Headline")))
        val seen = activity("state.updated", """{"entry_id":2,"register":"seen"}""", after = """{"seen":{"value":"seen"}}""")
        assertEquals("Marked seen: Headline", activityDescription(seen, mapOf(2L to "Headline")))
    }

    @Test fun unknownTitleLeavesTheActionReadable() {
        val done = activity("state.updated", """{"entry_id":2,"register":"shelf"}""", after = """{"shelf":{"value":"done"}}""")
        assertEquals("Moved to Done", activityDescription(done, emptyMap()))
    }

    @Test fun annotationsAndNotificationsUsePlainLanguage() {
        val brief = activity("annotation.updated", after = """{"kind":"brief"}""")
        assertEquals("Wrote a brief", activityDescription(brief, emptyMap()))
        val notification = activity("notification.created", """{"id":7}""")
        assertEquals("Sent a notification", activityDescription(notification, emptyMap()))
    }

    @Test fun entryIdFallsBackToTheChangePayload() {
        val activity = activity("notification.created", after = """{"entry_ids":[2]}""")
        assertEquals(2L, activityEntryId(activity))
        assertEquals("Sent a notification: Headline", activityDescription(activity, mapOf(2L to "Headline")))
    }

    @Test fun unknownActionsReplaceDotsWithSpaces() {
        assertEquals("Rule updated", activityDescription(activity("rule.updated"), emptyMap()))
    }
}

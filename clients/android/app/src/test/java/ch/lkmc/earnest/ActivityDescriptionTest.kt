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

    @Test fun annotationKindsTakeTheRightIndefiniteArticle() {
        val note = activity("annotation.updated", after = """{"kind":"note"}""")
        assertEquals("Wrote a note", activityDescription(note, emptyMap()))
        val fallback = activity("annotation.updated", after = """{"value":"hello"}""")
        assertEquals("Wrote an annotation", activityDescription(fallback, emptyMap()))
        val deleted = activity("annotation.deleted", before = """{"kind":"note"}""")
        assertEquals("Deleted a note", activityDescription(deleted, emptyMap()))
    }

    @Test fun activityWithoutAnEntryIdDoesNotRenderNull() {
        val deleted = activity("annotation.deleted", before = """{"kind":"note"}""")
        assertEquals("Morning Agent deleted a note: an entry", agentChangeText(deleted, emptyMap()))
    }

    @Test fun entryIdFallsBackToTheChangePayload() {
        val activity = activity("notification.created", after = """{"entry_ids":[2]}""")
        assertEquals(2L, activityEntryId(activity))
        assertEquals("Sent a notification: Headline", activityDescription(activity, mapOf(2L to "Headline")))
    }

    @Test fun shelfValuesNameTheirEffect() {
        val none = activity("state.updated", """{"entry_id":2,"register":"shelf"}""", after = """{"shelf":{"value":"none"}}""")
        assertEquals("Cleared the shelf: Headline", activityDescription(none, mapOf(2L to "Headline")))
        val unknown = activity("state.updated", """{"entry_id":2,"register":"shelf"}""", after = """{"shelf":{"value":"archived"}}""")
        assertEquals("Changed the shelf: Headline", activityDescription(unknown, mapOf(2L to "Headline")))
        val missing = activity("state.updated", """{"entry_id":2,"register":"shelf"}""", after = """{"shelf":{}}""")
        assertEquals("Changed the shelf: Headline", activityDescription(missing, mapOf(2L to "Headline")))
    }

    @Test fun batchChangesCountTheArticlesTheyTouched() {
        val one = activity("notification.created", after = """{"entry_ids":[2]}""")
        assertEquals("Sent a notification: Headline", activityDescription(one, mapOf(2L to "Headline")))
        assertEquals("Sent a notification", activityDescription(one, emptyMap()))
        assertEquals("Morning Agent sent a notification: Headline", agentChangeText(one, mapOf(2L to "Headline")))
        assertEquals("Morning Agent sent a notification: Entry #2", agentChangeText(one, emptyMap()))

        val two = activity("notification.created", after = """{"entry_ids":[2,3]}""")
        assertEquals("Sent a notification: Headline and 1 more", activityDescription(two, mapOf(2L to "Headline")))
        assertEquals("Sent a notification: 2 articles", activityDescription(two, emptyMap()))
        assertEquals("Morning Agent sent a notification: Headline and 1 more", agentChangeText(two, mapOf(2L to "Headline")))
        assertEquals("Morning Agent sent a notification: 2 articles", agentChangeText(two, emptyMap()))

        val many = activity("notification.created", after = """{"entry_ids":[2,3,4,5,6]}""")
        assertEquals("Sent a notification: Headline and 4 more", activityDescription(many, mapOf(2L to "Headline")))
        assertEquals("Sent a notification: 5 articles", activityDescription(many, emptyMap()))
        assertEquals("Morning Agent sent a notification: 5 articles", agentChangeText(many, emptyMap()))

        val none = activity("notification.created", """{"id":7}""")
        assertEquals("Sent a notification", activityDescription(none, emptyMap()))
        assertEquals("Morning Agent sent a notification: an entry", agentChangeText(none, emptyMap()))
    }

    @Test fun deletedAndCancelledBatchesKeepTheirArticleCounts() {
        for ((action, detail) in listOf("annotation.deleted" to "deleted a note", "notification.cancelled" to "cancelled a notification")) {
            val batch = activity(action, before = """{"kind":"note","entry_ids":[2,3]}""")
            assertEquals("$detail: 2 articles".replaceFirstChar { it.uppercase() }, activityDescription(batch, emptyMap()))
            assertEquals("Morning Agent $detail: 2 articles", agentChangeText(batch, emptyMap()))
            assertEquals("Morning Agent $detail: Headline and 1 more", agentChangeText(batch, mapOf(2L to "Headline")))
        }
    }

    @Test fun unknownActionsReplaceDotsWithSpaces() {
        assertEquals("Rule updated", activityDescription(activity("rule.updated"), emptyMap()))
    }
}

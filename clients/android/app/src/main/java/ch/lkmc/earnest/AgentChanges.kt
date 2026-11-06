package ch.lkmc.earnest

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.Activity
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.longOrNull
import java.time.Instant

internal fun agentChangedEntryId(activity: Activity): Long? = activityEntryId(activity)

/** The article an activity touched, from its target or its change payload. */
internal fun activityEntryId(activity: Activity): Long? {
    val target = (activity.target as? JsonObject)?.get("entry_id") as? JsonPrimitive
    if (target != null && (target.longOrNull ?: 0L) > 0) return target.longOrNull
    val ids = (activity.after as? JsonObject)?.get("entry_ids") as? JsonArray
    return (ids?.firstOrNull() as? JsonPrimitive)?.longOrNull?.takeIf { it > 0 }
}

internal fun activitySince(changes: List<Activity>, since: Instant): List<Activity> =
    changes.filter { change ->
        runCatching { Instant.parse(change.createdAt) }.getOrNull()?.let { it >= since } == true
    }

/** Readable action text, lower case for embedding after an agent name. */
internal fun activityDetail(activity: Activity): String = when (activity.action) {
    "state.updated" -> {
        val register = ((activity.target as? JsonObject)?.get("register") as? JsonPrimitive)?.contentOrNull
        val changed = (register?.let { (activity.after as? JsonObject)?.get(it) } as? JsonObject)
        val value = (changed?.get("value") as? JsonPrimitive)?.contentOrNull
        when (register) {
            "seen" -> if (value == "seen") "marked seen" else "marked unread"
            "shelf" -> when (value) {
                "later" -> "moved to Later"
                "done" -> "moved to Done"
                else -> "cleared the shelf"
            }
            "kept" -> if (value == "true") "kept" else "unkept"
            else -> "changed ${register ?: "state"}"
        }
    }
    "annotation.updated" -> "wrote ${withIndefiniteArticle(annotationKind(activity.after, activity.before))}"
    "annotation.deleted" -> "deleted ${withIndefiniteArticle(annotationKind(activity.before))}"
    "annotation.reverted" -> "reverted an annotation"
    "position.updated" -> "moved its read position"
    "notification.created" -> "sent a notification"
    "notification.cancelled" -> "cancelled a notification"
    else -> activity.action.replace('.', ' ')
}

/** One activity row: the action plus the article title when one is known. */
internal fun activityDescription(activity: Activity, titles: Map<Long, String>): String {
    val detail = activityDetail(activity)
    val title = activityEntryId(activity)?.let(titles::get)?.takeIf { it.isNotBlank() }
    val text = if (title != null) "$detail: $title" else detail
    return text.replaceFirstChar { it.uppercase() }
}

/** Server provenance and register values are text, never publisher or agent HTML. */
internal fun agentChangeText(activity: Activity, titles: Map<Long, String>): String {
    val id = activityEntryId(activity)
    val title = id?.let(titles::get)?.takeIf { it.isNotBlank() } ?: id?.let { "Entry #$it" } ?: "an entry"
    val name = activity.principalName.ifBlank { "Agent ${activity.principalId}" }
    return "$name ${activityDetail(activity)}: $title"
}

private fun annotationKind(vararg values: JsonElement?): String =
    values.firstNotNullOfOrNull { (it as? JsonObject)?.get("kind") as? JsonPrimitive }
        ?.contentOrNull?.takeIf { it.isNotBlank() } ?: "annotation"

/** The article matches the kind's initial letter; the fallback is vowel-initial. */
private fun withIndefiniteArticle(kind: String): String =
    if (kind.first().lowercaseChar() in "aeiou") "an $kind" else "a $kind"

@Composable
internal fun AgentChangesNotice(
    title: String,
    changes: List<Activity>,
    entryTitles: Map<Long, String>,
    error: String?,
    onOpen: (Long) -> Unit,
    onRevert: (Long) -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(title, style = MaterialTheme.typography.titleSmall)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        changes.forEach { change ->
            Text(agentChangeText(change, entryTitles))
            agentChangedEntryId(change)?.let { id ->
                TextButton(onClick = { onOpen(id) }) { Text("Open article") }
            }
            if (change.nextAction == "revert") {
                TextButton(onClick = { onRevert(change.id) }) { Text("Revert") }
            } else {
                Text(change.reason?.takeIf { it.isNotBlank() }?.let { "Cannot revert: $it" } ?: "Cannot revert this change.")
            }
        }
    }
}

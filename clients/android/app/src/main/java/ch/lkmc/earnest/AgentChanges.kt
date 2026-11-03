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
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.longOrNull
import java.time.Instant

internal fun agentChangedEntryId(activity: Activity): Long? =
    ((activity.target as? JsonObject)?.get("entry_id") as? JsonPrimitive)?.longOrNull

internal fun activitySince(changes: List<Activity>, since: Instant): List<Activity> =
    changes.filter { change ->
        runCatching { Instant.parse(change.createdAt) }.getOrNull()?.let { it >= since } == true
    }

/** Server provenance and register values are text, never publisher or agent HTML. */
internal fun agentChangeText(activity: Activity, titles: Map<Long, String>): String {
    val id = agentChangedEntryId(activity)
    val title = id?.let(titles::get)?.takeIf { it.isNotBlank() } ?: "Entry #$id"
    val register = ((activity.target as? JsonObject)?.get("register") as? JsonPrimitive)?.contentOrNull
    val changed = register?.let { (activity.after as? JsonObject)?.get(it) as? JsonObject }
    val value = (changed?.get("value") as? JsonPrimitive)?.contentOrNull
    val action = when (register) {
        "shelf" -> when (value) {
            "done" -> "moved to Done"
            "later" -> "moved to Later"
            "none" -> "cleared the shelf of"
            else -> "changed the shelf of"
        }
        "seen" -> if (value == "seen") "marked seen" else "marked unseen"
        "kept" -> if (value == "true") "kept" else "unkept"
        else -> "changed"
    }
    val name = activity.principalName.ifBlank { "Agent ${activity.principalId}" }
    return "$name $action: $title"
}

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

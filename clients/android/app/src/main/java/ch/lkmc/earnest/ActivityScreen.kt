package ch.lkmc.earnest

import androidx.compose.material3.HorizontalDivider
import androidx.compose.ui.semantics.heading
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.semantics

@Composable
internal fun ActivityScreen(
    groups: List<ch.lkmc.earnest.core.ActivityGroup>,
    entryTitles: Map<Long, String>,
    onRevert: (Long) -> Unit,
) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(horizontal = ReaderSpace.gutter, vertical = ReaderSpace.md), verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
        if (groups.isEmpty()) {
            item { QuietState("No activity loaded", "Agent changes from the past seven days appear here. Refresh to check for updates.", icon = Icons.Filled.Info) }
        }
        groups.forEach { group ->
            item(key = "group-${group.principalId}") {
                Text(
                    group.principalName,
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.padding(top = ReaderSpace.md, bottom = ReaderSpace.xs).semantics { heading() },
                )
            }
            val activities = group.activities.orEmpty()
            items(activities, key = { it.id }) { activity ->
                Column(Modifier.fillMaxWidth().padding(vertical = ReaderSpace.xs), verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
                    Text(activityDescription(activity, entryTitles), style = MaterialTheme.typography.bodyLarge)
                    Text(
                        activity.createdAt.relativeTime(),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    val reason = activity.reason
                    if (activity.nextAction == "revert") {
                        TextButton(onClick = { onRevert(activity.id) }) { Text("Revert") }
                    } else if (!reason.isNullOrBlank()) {
                        Text(
                            reason,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                }
            }
        }
    }
}

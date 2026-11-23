package ch.lkmc.earnest

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.Activity
import ch.lkmc.earnest.core.ActivityGroup

private val AgentMarkSize = 36.dp
private val RailCenter = AgentMarkSize / 2
private val RailDotRadius = 4.dp
private val RailDotTop = ReaderSpace.gutter
private val RailWidth = 2.dp

/** What each agent changed in the past seven days, newest within each agent. */
@Composable
internal fun ActivityScreen(
    groups: List<ActivityGroup>,
    load: ActivityLoad,
    entryTitles: Map<Long, String>,
    onRevert: (Long) -> Unit,
    onRetry: () -> Unit,
) {
    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = ReaderSpace.gutter, end = ReaderSpace.gutter, bottom = ReaderSpace.xl),
    ) {
        if (groups.isEmpty()) {
            item(key = "activity-state") { ActivityPlaceholder(load, onRetry) }
            return@LazyColumn
        }
        if (load == ActivityLoad.FAILED) {
            item(key = "activity-stale") {
                NoticeBanner(
                    "Could not update. This is the last list that loaded.",
                    tone = NoticeTone.ERROR,
                    actionLabel = "Try again",
                    onAction = onRetry,
                )
            }
        }
        groups.forEach { group ->
            val activities = group.activities.orEmpty()
            item(key = "group-${group.principalId}") { AgentHeading(group.principalName, activities.size) }
            itemsIndexed(activities, key = { _, activity -> activity.id }) { index, activity ->
                ChangeRow(activity, entryTitles, last = index == activities.lastIndex, onRevert = onRevert)
            }
        }
    }
}

@Composable
private fun ActivityPlaceholder(load: ActivityLoad, onRetry: () -> Unit) {
    when (load) {
        ActivityLoad.LOADING -> LoadingState("Loading agent activity…")
        ActivityLoad.FAILED -> QuietState(
            "Activity did not load",
            "Check your connection and try again.",
            icon = EarnestIcons.Offline,
            tone = LocalTones.current.amber,
            actionLabel = "Try again",
            onAction = onRetry,
        )
        ActivityLoad.LOADED -> QuietState(
            "All quiet",
            "When an agent changes something, you see it here for seven days and can revert what is reversible.",
            icon = EarnestIcons.Sparkle,
            tone = LocalTones.current.purple,
        )
    }
}

@Composable
private fun AgentHeading(name: String, changes: Int) {
    val colors = toneColors(MaterialTheme.colorScheme, LocalTones.current.purple)
    Row(
        Modifier.fillMaxWidth().padding(top = ReaderSpace.md),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
    ) {
        Box(Modifier.size(AgentMarkSize).background(colors.container, CircleShape), contentAlignment = Alignment.Center) {
            Icon(EarnestIcons.Sparkle, contentDescription = null, tint = colors.ink, modifier = Modifier.size(20.dp))
        }
        Column(Modifier.weight(1f)) {
            Text(name, style = MaterialTheme.typography.titleMedium, modifier = Modifier.semantics { heading() })
            Text(
                "$changes ${if (changes == 1) "change" else "changes"}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun ChangeRow(activity: Activity, entryTitles: Map<Long, String>, last: Boolean, onRevert: (Long) -> Unit) {
    val scheme = MaterialTheme.colorScheme
    val rail = scheme.outlineVariant
    val dot = LocalTones.current.purple
    Column(
        Modifier.fillMaxWidth()
            .drawBehind {
                val fromStart = RailCenter.toPx()
                val x = if (layoutDirection == LayoutDirection.Rtl) size.width - fromStart else fromStart
                val dotY = RailDotTop.toPx()
                drawLine(rail, Offset(x, 0f), Offset(x, if (last) dotY else size.height), RailWidth.toPx(), StrokeCap.Round)
                drawCircle(dot, RailDotRadius.toPx(), Offset(x, dotY))
            }
            .padding(start = AgentMarkSize + ReaderSpace.sm, top = ReaderSpace.xs, bottom = ReaderSpace.xxs),
        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
    ) {
        Text(activityDescription(activity, entryTitles), style = MaterialTheme.typography.bodyLarge)
        Text(activity.createdAt.relativeTime(), style = MaterialTheme.typography.bodySmall, color = scheme.onSurfaceVariant)
        if (activity.nextAction == "revert") {
            QuietButton("Revert", { onRevert(activity.id) })
            return@Column
        }
        activity.reason?.takeIf { it.isNotBlank() }?.let {
            Text(it, style = MaterialTheme.typography.bodySmall, color = scheme.onSurfaceVariant)
        }
    }
}

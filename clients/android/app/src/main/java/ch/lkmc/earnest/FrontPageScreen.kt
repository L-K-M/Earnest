package ch.lkmc.earnest

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.Image
import androidx.lifecycle.viewmodel.compose.viewModel
import ch.lkmc.earnest.core.FrontPage
import ch.lkmc.earnest.core.Entry
import ch.lkmc.earnest.core.FrontPageCoverage
import ch.lkmc.earnest.core.FrontPagePeriod
import ch.lkmc.earnest.core.FrontPageStory
import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.db.BriefEntity
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** The tab's short name; the label beneath it is the window's span. */
internal fun frontPageWindowName(period: FrontPagePeriod): String = when (period) {
    FrontPagePeriod.DAILY -> "Today"
    FrontPagePeriod.WEEKLY -> "This week"
}

/** The tab's explanatory label, matching the server's window. */
internal fun frontPageWindowLabel(period: FrontPagePeriod): String = when (period) {
    FrontPagePeriod.DAILY -> "Past 24 hours"
    FrontPagePeriod.WEEKLY -> "Past 7 days"
}

/**
 * The newspaper front page. The server owns selection and ranking; this
 * screen renders the sections and stories in the order it received them.
 * A story's headline opens its representative article, coverage links open
 * the other originals, and nothing here marks an article read.
 */
@Composable
internal fun FrontPageScreen(
    state: FrontPageUi,
    states: Map<Long, State>,
    principalId: Long,
    briefs: List<BriefEntity>,
    principalNames: Map<Long, String>,
    entryTitles: Map<Long, String>,
    agentChanges: List<ch.lkmc.earnest.core.Activity>,
    sessionChanges: List<ch.lkmc.earnest.core.Activity>,
    agentChangesError: String?,
    onRevert: (Long) -> Unit,
    onOpen: (Long) -> Unit,
    onOpenAgentSelected: (Long) -> Unit,
    onPeriodChange: (FrontPagePeriod) -> Unit,
    onRefresh: () -> Unit,
    visible: (Entry) -> Boolean = { !it.deleted },
    image: @Composable (String?, Modifier) -> Unit = { path, modifier -> FrontImage(path, modifier) },
) {
    val page = state.page
    if (page == null) {
        // Without a page there is nothing to scroll, so the masthead stays put.
        Column(Modifier.fillMaxSize()) {
            FrontPageMasthead(
                state = state,
                onPeriodChange = onPeriodChange,
                onRefresh = onRefresh,
                modifier = Modifier.padding(horizontal = 16.dp),
            )
            when {
                state.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        CircularProgressIndicator()
                        Text("Loading the front page…", style = MaterialTheme.typography.bodyMedium)
                    }
                }
                state.error != null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    Column(
                        Modifier.padding(24.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Text("Front page did not load", style = MaterialTheme.typography.titleMedium)
                        Text(state.error.orEmpty(), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
                        TextButton(onClick = onRefresh, modifier = Modifier.testTag("front-try-again")) { Text("Try again") }
                    }
                }
                else -> FrontPageEmpty(Modifier.fillMaxSize().padding(horizontal = 24.dp), state.period)
            }
        }
    } else {
        FrontPageGrid(
            state = state,
            states = states,
            principalId = principalId,
            briefs = briefs,
            principalNames = principalNames,
            entryTitles = entryTitles,
            agentChanges = agentChanges,
            sessionChanges = sessionChanges,
            agentChangesError = agentChangesError,
            onRevert = onRevert,
            onOpen = onOpen,
            onOpenAgentSelected = onOpenAgentSelected,
            onPeriodChange = onPeriodChange,
            onRefresh = onRefresh,
            visible = visible,
            image = image,
        )
    }
}

@Composable
private fun FrontPageMasthead(
    state: FrontPageUi,
    onPeriodChange: (FrontPagePeriod) -> Unit,
    onRefresh: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val page = state.page
    Column(modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                page?.let {
                    val date = frontPageDate(it)
                    if (date.isNotBlank()) Text(date, style = MaterialTheme.typography.titleMedium)
                }
                state.updatedAt?.let {
                    Text("Updated ${it.relativeTime()}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            TextButton(onClick = onRefresh, enabled = !state.loading, modifier = Modifier.testTag("front-refresh")) { Text("Refresh") }
        }
        PrimaryTabRow(selectedTabIndex = state.period.ordinal) {
            FrontPagePeriod.entries.forEach { period ->
                Tab(
                    selected = state.period == period,
                    onClick = { onPeriodChange(period) },
                    text = {
                        Column {
                            Text(frontPageWindowName(period), style = MaterialTheme.typography.labelLarge)
                            Text(
                                frontPageWindowLabel(period),
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    },
                )
            }
        }
        if (state.loading && page != null) {
            Text("Updating…", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        state.pendingRevision?.let {
            Row(
                Modifier.fillMaxWidth().padding(top = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "New coverage available",
                    style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = onRefresh, modifier = Modifier.testTag("front-apply")) { Text("Refresh") }
            }
        }
        state.error?.let { error ->
            if (page != null) {
                Text(
                    error,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.padding(top = 8.dp),
                )
            }
        }
    }
}

@Composable
private fun FrontPageGrid(
    state: FrontPageUi,
    states: Map<Long, State>,
    principalId: Long,
    briefs: List<BriefEntity>,
    principalNames: Map<Long, String>,
    entryTitles: Map<Long, String>,
    agentChanges: List<ch.lkmc.earnest.core.Activity>,
    sessionChanges: List<ch.lkmc.earnest.core.Activity>,
    agentChangesError: String?,
    onRevert: (Long) -> Unit,
    onOpen: (Long) -> Unit,
    onOpenAgentSelected: (Long) -> Unit,
    onPeriodChange: (FrontPagePeriod) -> Unit,
    onRefresh: () -> Unit,
    visible: (Entry) -> Boolean,
    image: @Composable (String?, Modifier) -> Unit,
) {
    val page = state.page ?: return
    val sections = page.sections.map { section ->
        section.copy(stories = section.stories.mapNotNull { it.visibleStory(visible) })
    }.filter { it.stories.isNotEmpty() }
    val leadId = sections.firstOrNull()?.stories?.firstOrNull()?.id
    val gridState = rememberLazyGridState()
    var expandedCoverage by remember { mutableStateOf(setOf<String>()) }
    var expandedCitations by remember { mutableStateOf(setOf<String>()) }
    val daily = state.period == FrontPagePeriod.DAILY

    LazyVerticalGrid(
        columns = GridCells.Adaptive(minSize = 280.dp),
        state = gridState,
        modifier = Modifier.fillMaxSize().testTag("front-grid"),
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 24.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        // The masthead scrolls away with the stories instead of trapping them
        // beneath fixed phone chrome.
        item(span = { GridItemSpan(maxLineSpan) }, key = "front-masthead") {
            FrontPageMasthead(state, onPeriodChange, onRefresh)
        }
        if (daily && briefs.isNotEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }, key = "briefs") {
                Column(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    briefs.forEach { brief ->
                        Text(
                            briefLabel(principalNames[brief.principalId], brief.principalId, brief.value),
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                }
            }
        }
        if (daily && (agentChanges.isNotEmpty() || agentChangesError != null)) {
            item(span = { GridItemSpan(maxLineSpan) }, key = "agent-changes") {
                AgentChangesNotice(
                    "While you were out",
                    agentChanges,
                    entryTitles,
                    agentChangesError,
                    onOpenAgentSelected,
                    onRevert,
                )
            }
        }
        if (sections.isEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }, key = "empty") {
                FrontPageEmpty(Modifier.fillMaxWidth(), state.period)
            }
        }
        sections.forEach { section ->
            item(span = { GridItemSpan(maxLineSpan) }, key = "section-${section.name}") {
                Text(
                    section.name,
                    style = MaterialTheme.typography.titleMedium,
                    modifier = Modifier.fillMaxWidth().padding(top = 16.dp, bottom = 8.dp)
                        .testTag("section-${section.name}"),
                )
            }
            section.stories.forEach { story ->
                val lead = story.id == leadId
                item(
                    span = { if (lead) GridItemSpan(maxLineSpan) else GridItemSpan(1) },
                    key = story.id,
                ) {
                    FrontStory(
                        story = story,
                        lead = lead,
                        states = states,
                        principalId = principalId,
                        expandedCoverage = story.id in expandedCoverage,
                        expandedCitations = story.id in expandedCitations,
                        onToggleCoverage = { expandedCoverage = expandedCoverage.toggle(story.id) },
                        onToggleCitations = { expandedCitations = expandedCitations.toggle(story.id) },
                        onOpen = onOpen,
                        image = image,
                    )
                }
            }
        }
        if (daily && (sessionChanges.isNotEmpty() || agentChangesError != null)) {
            item(span = { GridItemSpan(maxLineSpan) }, key = "session-changes") {
                AgentChangesNotice(
                    "Agent changes this session",
                    sessionChanges,
                    entryTitles,
                    agentChangesError,
                    onOpenAgentSelected,
                    onRevert,
                )
            }
        }
    }
}

@Composable
private fun FrontPageEmpty(modifier: Modifier = Modifier, period: FrontPagePeriod) {
    Column(
        modifier.padding(vertical = 24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text("No stories in this window", style = MaterialTheme.typography.titleMedium)
        Text(
            "Nothing matched the ${frontPageWindowLabel(period).lowercase()}.",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun FrontStory(
    story: FrontPageStory,
    lead: Boolean,
    states: Map<Long, State>,
    principalId: Long,
    expandedCoverage: Boolean,
    expandedCitations: Boolean,
    onToggleCoverage: () -> Unit,
    onToggleCitations: () -> Unit,
    onOpen: (Long) -> Unit,
    image: @Composable (String?, Modifier) -> Unit,
) {
    val representative = story.representative() ?: return
    val coverage = story.coverage.filterNot { it.entry.deleted }
    val others = coverage.filter { it.entry.id != representative.entry.id }
    val entry = representative.entry
    val title = entry.title.ifBlank { "Untitled" }
    val read = story.isRead(states, principalId)
    Card(
        modifier = Modifier.fillMaxWidth().testTag(story.id),
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
        elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
    ) {
        Column(
            Modifier
                .fillMaxWidth()
                .clickable { onOpen(entry.id) }
                .padding(16.dp),
        ) {
            val imageUrl = story.imageUrl
            if (imageUrl != null) {
                image(
                    imageUrl,
                    Modifier.fillMaxWidth()
                        .height(if (lead) 200.dp else 120.dp)
                        .clip(RoundedCornerShape(if (lead) 12.dp else 8.dp)),
                )
                Spacer(Modifier.height(8.dp))
            } else {
                // Still ask for the absent picture, but never reserve its height.
                image(null, Modifier)
            }
            Text(
                title,
                style = if (lead) MaterialTheme.typography.titleLarge else MaterialTheme.typography.titleMedium,
                maxLines = if (lead) 4 else 3,
                overflow = TextOverflow.Ellipsis,
            )
            Column(Modifier.padding(top = 4.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        listOfNotNull(
                            representative.sourceTitle.takeIf { it.isNotBlank() },
                            story.publishedAt.relativeTime().takeIf { it.isNotBlank() },
                        ).joinToString(" · "),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    if (read) {
                        Icon(
                            Icons.Filled.CheckCircle,
                            contentDescription = null,
                            tint = LocalTones.current.green,
                            modifier = Modifier.size(16.dp),
                        )
                        Spacer(Modifier.width(4.dp))
                        Text("Read", style = MaterialTheme.typography.labelMedium, color = LocalTones.current.green)
                    }
                }
                if (story.feedCount > 1) {
                    Text(
                        "Covered by ${story.feedCount} feeds",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            Spacer(Modifier.height(8.dp))
            StoryText(
                story = story,
                coverage = coverage,
                expandedCitations = expandedCitations,
                onToggleCitations = onToggleCitations,
                onOpen = onOpen,
            )
            if (others.isNotEmpty()) {
                TextButton(onClick = onToggleCoverage) {
                    Text(if (expandedCoverage) "Hide other sources" else "Other sources (${others.size})")
                }
                if (expandedCoverage) {
                    others.forEach { item ->
                        TextButton(onClick = { onOpen(item.entry.id) }) {
                            Text(
                                "${item.sourceTitle} · ${item.entry.title.ifBlank { "Untitled" }}",
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun StoryText(
    story: FrontPageStory,
    coverage: List<FrontPageCoverage>,
    expandedCitations: Boolean,
    onToggleCitations: () -> Unit,
    onOpen: (Long) -> Unit,
) {
    val summary = story.summary
    when {
        summary != null -> {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                summary.claims.forEach { claim -> Text(claim.text, style = MaterialTheme.typography.bodyMedium) }
                Text(
                    "Summary of linked coverage · ${summary.generatedAt.relativeTime()}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                summary.excerptNotice?.let {
                    Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                val citations = summary.claims.flatMap { it.citations.orEmpty() }
                if (citations.isNotEmpty()) {
                    TextButton(onClick = onToggleCitations) {
                        Text(if (expandedCitations) "Hide citations" else "Citations")
                    }
                    if (expandedCitations) {
                        citations.forEach { citation ->
                            Column(Modifier.padding(start = 8.dp, bottom = 4.dp)) {
                                Text(
                                    if (citation.verified) "Verified citation" else "Citation not found in the article",
                                    style = MaterialTheme.typography.labelSmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                citation.passages.orEmpty().forEach { passage ->
                                    val target = coverage.firstOrNull { it.entry.id == citation.entryId }
                                    if (target != null) {
                                        TextButton(onClick = { onOpen(target.entry.id) }) { Text(passage) }
                                    } else {
                                        Text(passage, style = MaterialTheme.typography.bodySmall)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        story.excerpt.isNotBlank() -> {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(story.excerpt, style = MaterialTheme.typography.bodyMedium)
                Text(
                    "Excerpt from ${story.sourceTitle}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        else -> Text(
            "No summary or excerpt is available.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/**
 * A stored front-page image. Absent and failed images collapse to text-only;
 * the modifier is applied only once a bitmap exists.
 */
@Composable
internal fun FrontImage(path: String?, modifier: Modifier = Modifier, model: AppViewModel = viewModel()) {
    if (path.isNullOrEmpty()) return
    val accountKey by remember(model) { model.state.map { it.accountKey }.distinctUntilChanged() }
        .collectAsState(remember(model) { model.state.value.accountKey })
    val bitmap by produceState<ImageBitmap?>(null, path, accountKey) {
        value = null
        value = model.mediaBitmap(path)
    }
    val image = bitmap ?: return
    Image(
        bitmap = image,
        contentDescription = null,
        modifier = modifier,
        contentScale = ContentScale.Fit,
    )
}

/** Removed originals cannot leave their excerpt, photograph or claims behind. */
internal fun FrontPageStory.visibleStory(visible: (Entry) -> Boolean): FrontPageStory? {
    val members = coverage.filter { !it.entry.deleted && visible(it.entry) }
    val representative = members.firstOrNull { it.entry.id == entry.id } ?: members.firstOrNull() ?: return null
    val sameOriginal = representative.entry.id == entry.id
    val live = members.map { it.entry.id }.toSet()
    return copy(
        entry = representative.entry,
        sourceTitle = if (sameOriginal) sourceTitle else representative.sourceTitle,
        publishedAt = if (sameOriginal) publishedAt else representative.entry.publishedAt ?: representative.entry.firstSeenAt,
        coverage = members,
        feedCount = if (members.size == coverage.size) feedCount else members.map { it.entry.feedId }.distinct().size,
        imageUrl = imageUrl.takeIf { sameOriginal },
        excerpt = if (sameOriginal) excerpt else "",
        summary = summary?.takeIf { summary ->
            summary.claims.all { claim -> claim.citations.orEmpty().all { it.entryId in live } }
        },
    )
}

/**
 * The coverage member the headline opens: the story's own entry when it is
 * still present, otherwise the newest surviving member.
 */
internal fun FrontPageStory.representative(): FrontPageCoverage? {
    val coverage = coverage.filterNot { it.entry.deleted }
    return coverage.firstOrNull { it.entry.id == entry.id } ?: coverage.firstOrNull()
}

/**
 * A story is read only when every article in its coverage has been seen or
 * marked Done. A partially read story stays unread.
 */
internal fun FrontPageStory.isRead(states: Map<Long, State>, principalId: Long): Boolean {
    val coverage = coverage.filterNot { it.entry.deleted }
    return coverage.isNotEmpty() && coverage.all { item ->
        val state = states[item.entry.id] ?: item.entry.state ?: SyncEngine.initialState(principalId, item.entry.id)
        state.seen.value == JsonPrimitive(SyncEngine.VALUE_SEEN) ||
            state.shelf.value == JsonPrimitive(SyncEngine.VALUE_DONE)
    }
}

private fun Set<String>.toggle(value: String): Set<String> =
    if (contains(value)) this - value else this + value

private fun frontPageDate(page: FrontPage): String {
    val instant = (page.selectedAt.takeIf { it.isNotBlank() } ?: page.windowEnd).let(::parseInstant)
    return instant?.atZone(ZoneId.systemDefault())?.format(DateTimeFormatter.ofPattern("MMMM d, yyyy")) ?: ""
}

private fun parseInstant(value: String): Instant? = runCatching { Instant.parse(value) }.getOrNull()

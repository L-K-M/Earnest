package ch.lkmc.earnest

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.Image
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
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
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import ch.lkmc.earnest.core.Entry
import ch.lkmc.earnest.core.FrontPage
import ch.lkmc.earnest.core.FrontPageCoverage
import ch.lkmc.earnest.core.FrontPagePeriod
import ch.lkmc.earnest.core.FrontPageStory
import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.db.BriefEntity
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.JsonPrimitive

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
        // Keep long errors and large text reachable even in a short window.
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
            FrontPageMasthead(
                state = state,
                onPeriodChange = onPeriodChange,
                onRefresh = onRefresh,
                modifier = Modifier.padding(horizontal = ReaderSpace.gutter),
            )
            when {
                state.loading -> LoadingState("Loading the front page…")
                state.error != null -> QuietState(
                    "Front page did not load",
                    state.error.orEmpty(),
                    icon = EarnestIcons.Offline,
                    tone = LocalTones.current.amber,
                    actionLabel = "Try again",
                    onAction = onRefresh,
                    actionModifier = Modifier.testTag("front-try-again"),
                )
                else -> FrontPageEmpty(state.period, onPeriodChange, Modifier.fillMaxWidth().padding(horizontal = ReaderSpace.lg))
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

/** The dateline, the window and anything waiting to change the page. */
@Composable
private fun FrontPageMasthead(
    state: FrontPageUi,
    onPeriodChange: (FrontPagePeriod) -> Unit,
    onRefresh: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val scheme = MaterialTheme.colorScheme
    val page = state.page
    val quiet = MaterialTheme.typography.bodySmall
    Column(modifier.fillMaxWidth().padding(bottom = ReaderSpace.xxs), verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                page?.let {
                    val date = frontPageDate(it)
                    if (date.isNotBlank()) Text(date, style = StoryType.secondary, modifier = Modifier.semantics { heading() })
                }
                state.updatedAt?.let {
                    Text("Updated ${it.relativeTime()}", style = quiet, color = scheme.onSurfaceVariant)
                }
                if (state.loading && page != null) {
                    Text(
                        "Updating…",
                        style = quiet,
                        color = scheme.onSurfaceVariant,
                        modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                    )
                }
            }
            QuietButton("Refresh", onRefresh, Modifier.testTag("front-refresh"), enabled = !state.loading, icon = EarnestIcons.Refresh)
        }
        ChoiceGroup(
            options = FrontPagePeriod.entries,
            selected = state.period,
            onSelect = onPeriodChange,
            label = ::frontPageWindowName,
            supporting = ::frontPageWindowLabel,
        )
        state.pendingRevision?.let {
            NoticeBanner(
                "New coverage available",
                actionLabel = "Refresh",
                onAction = onRefresh,
                icon = EarnestIcons.FrontPage,
                actionModifier = Modifier.testTag("front-apply"),
            )
        }
        if (page != null) state.error?.let { NoticeBanner(it, tone = NoticeTone.ERROR, icon = EarnestIcons.Offline) }
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
        contentPadding = PaddingValues(start = ReaderSpace.gutter, end = ReaderSpace.gutter, top = ReaderSpace.xs, bottom = ReaderSpace.xl),
        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.lg),
        verticalArrangement = Arrangement.spacedBy(ReaderSpace.md),
    ) {
        // The masthead scrolls away with the stories instead of trapping them
        // beneath fixed phone chrome.
        item(span = { GridItemSpan(maxLineSpan) }, key = "front-masthead") {
            FrontPageMasthead(state, onPeriodChange, onRefresh)
        }
        if (daily && briefs.isNotEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }, key = "briefs") {
                AgentBriefs(briefs, principalNames)
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
                FrontPageEmpty(state.period, onPeriodChange)
            }
        }
        sections.forEach { section ->
            item(span = { GridItemSpan(maxLineSpan) }, key = "section-${section.name}") { SectionRule(section.name) }
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
        if (sections.isNotEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }, key = "front-end") { FrontPageEnd() }
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
private fun FrontPageEmpty(period: FrontPagePeriod, onPeriodChange: (FrontPagePeriod) -> Unit, modifier: Modifier = Modifier) {
    QuietState(
        title = "No stories in this window",
        message = "Nothing matched the ${frontPageWindowLabel(period).lowercase()}.",
        icon = EarnestIcons.FrontPage,
        tone = LocalTones.current.orange,
        actionLabel = "See this week".takeIf { period == FrontPagePeriod.DAILY },
        onAction = { onPeriodChange(FrontPagePeriod.WEEKLY) },
        modifier = modifier,
    )
}

@Composable
private fun AgentBriefs(briefs: List<BriefEntity>, principalNames: Map<Long, String>) {
    val colors = toneColors(MaterialTheme.colorScheme, LocalTones.current.purple)
    Surface(shape = MaterialTheme.shapes.large, color = colors.container, contentColor = MaterialTheme.colorScheme.onSurface) {
        Row(
            Modifier.fillMaxWidth().padding(ReaderSpace.gutter),
            horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
                briefs.forEach { brief ->
                    Text(
                        briefLabel(principalNames[brief.principalId], brief.principalId, brief.value),
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
            }
            Icon(EarnestIcons.Sparkle, contentDescription = null, tint = colors.ink, modifier = Modifier.size(20.dp))
        }
    }
}

/** A newspaper's section rule: the name, then a hairline to the margin. */
@Composable
private fun SectionRule(name: String) {
    val scheme = MaterialTheme.colorScheme
    Row(
        Modifier.fillMaxWidth().padding(top = ReaderSpace.xs),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
    ) {
        Text(
            name,
            style = MaterialTheme.typography.titleSmall,
            color = scheme.primary,
            modifier = Modifier.weight(1f, fill = false).semantics { heading() }.testTag("section-$name"),
        )
        HorizontalDivider(Modifier.weight(1f), color = scheme.outlineVariant)
    }
}

@Composable
private fun FrontPageEnd() {
    Column(
        Modifier.fillMaxWidth().padding(ReaderSpace.md),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
    ) {
        Icon(
            painterResource(R.drawable.ic_notification),
            contentDescription = null,
            tint = MaterialTheme.colorScheme.outline,
            modifier = Modifier.size(28.dp),
        )
        Text(
            "You have reached the end of the front page.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

private val LeadImageHeight = 208.dp
private val SideImageSize = 92.dp
private const val LEAD_HEADLINE_LINES = 5
private const val SIDE_HEADLINE_LINES = 4
private const val LEAD_EXCERPT_LINES = 5
private const val SIDE_EXCERPT_LINES = 3

/** The lead has the width and the large picture; the others set theirs beside the headline. */
@OptIn(ExperimentalLayoutApi::class)
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
    val read = story.isRead(states, principalId)
    val imageUrl = story.imageUrl
    val scheme = MaterialTheme.colorScheme
    Column(
        Modifier.fillMaxWidth().testTag(story.id).clickable { onOpen(entry.id) },
        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
    ) {
        // An absent picture is still asked for, but never reserves its height.
        if (imageUrl == null) image(null, Modifier)
        if (lead && imageUrl != null) {
            image(imageUrl, Modifier.fillMaxWidth().height(LeadImageHeight).clip(MaterialTheme.shapes.large))
        }
        Row(horizontalArrangement = Arrangement.spacedBy(ReaderSpace.md)) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs)) {
                StoryByline(representative.sourceTitle, story.publishedAt.relativeTime(), read)
                Text(
                    entry.title.ifBlank { "Untitled" },
                    style = if (lead) StoryType.lead else StoryType.secondary,
                    color = if (read) scheme.onSurfaceVariant else scheme.onSurface,
                    modifier = Modifier.semantics { heading() },
                    maxLines = if (lead) LEAD_HEADLINE_LINES else SIDE_HEADLINE_LINES,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (!lead && imageUrl != null) image(imageUrl, Modifier.size(SideImageSize).clip(MaterialTheme.shapes.medium))
        }
        StoryText(
            story = story,
            coverage = coverage,
            excerptLines = if (lead) LEAD_EXCERPT_LINES else SIDE_EXCERPT_LINES,
            expandedCitations = expandedCitations,
            onToggleCitations = onToggleCitations,
            onOpen = onOpen,
        )
        if (story.feedCount > 1 || others.isNotEmpty()) {
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
                itemVerticalAlignment = Alignment.CenterVertically,
            ) {
                if (story.feedCount > 1) {
                    TagPill(
                        "Covered by ${story.feedCount} feeds",
                        icon = EarnestIcons.Articles,
                        colors = toneColors(scheme, LocalTones.current.orange),
                    )
                }
                if (others.isNotEmpty()) {
                    QuietButton(if (expandedCoverage) "Hide other sources" else "Other sources (${others.size})", onToggleCoverage)
                }
            }
            AnimatedVisibility(visible = expandedCoverage) {
                Column {
                    others.forEach { item ->
                        StoryLink("${item.sourceTitle} · ${item.entry.title.ifBlank { "Untitled" }}") { onOpen(item.entry.id) }
                    }
                }
            }
        }
        HorizontalDivider(Modifier.padding(top = ReaderSpace.xs), color = scheme.outlineVariant)
    }
}

@Composable
private fun StoryByline(source: String, time: String, read: Boolean) {
    val scheme = MaterialTheme.colorScheme
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xxs)) {
        Text(
            listOfNotNull(source.takeIf { it.isNotBlank() }, time.takeIf { it.isNotBlank() }).joinToString(" · "),
            style = MaterialTheme.typography.labelMedium,
            color = scheme.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        if (read) {
            Icon(EarnestIcons.DoneSelected, contentDescription = null, tint = scheme.primary, modifier = Modifier.size(16.dp))
            Text("Read", style = MaterialTheme.typography.labelMedium, color = scheme.primary)
        }
    }
}

/** A line of text that opens an article. Long ones wrap from the start edge. */
@Composable
private fun StoryLink(text: String, onClick: () -> Unit) {
    Text(
        text,
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier.fillMaxWidth().heightIn(min = TouchTarget)
            .clickable(role = Role.Button, onClick = onClick)
            .padding(vertical = ReaderSpace.sm),
    )
}

@Composable
private fun StoryText(
    story: FrontPageStory,
    coverage: List<FrontPageCoverage>,
    excerptLines: Int,
    expandedCitations: Boolean,
    onToggleCitations: () -> Unit,
    onOpen: (Long) -> Unit,
) {
    val quiet = MaterialTheme.typography.labelMedium
    val quietInk = MaterialTheme.colorScheme.onSurfaceVariant
    val summary = story.summary
    if (summary == null) {
        if (story.excerpt.isBlank()) {
            Text("No summary or excerpt is available.", style = quiet, color = quietInk)
            return
        }
        Column(verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs)) {
            Text(
                story.excerpt,
                style = MaterialTheme.typography.bodyMedium,
                maxLines = excerptLines,
                overflow = TextOverflow.Ellipsis,
            )
            Text("Excerpt from ${story.sourceTitle}", style = quiet, color = quietInk)
        }
        return
    }
    Column(verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs)) {
        summary.claims.forEach { claim -> Text(claim.text, style = MaterialTheme.typography.bodyMedium) }
        Text("Summary of linked coverage · ${summary.generatedAt.relativeTime()}", style = quiet, color = quietInk)
        summary.excerptNotice?.let { Text(it, style = quiet, color = quietInk) }
        val citations = summary.claims.flatMap { it.citations.orEmpty() }
        if (citations.isEmpty()) return@Column
        QuietButton(if (expandedCitations) "Hide citations" else "Citations", onToggleCitations)
        if (!expandedCitations) return@Column
        citations.forEach { citation ->
            Column(Modifier.padding(start = ReaderSpace.sm, bottom = ReaderSpace.xxs)) {
                Text(if (citation.verified) "Verified citation" else "Citation not found in the article", style = quiet, color = quietInk)
                val target = coverage.firstOrNull { it.entry.id == citation.entryId }
                citation.passages.orEmpty().forEach { passage ->
                    if (target != null) {
                        StoryLink(passage) { onOpen(target.entry.id) }
                    } else {
                        Text(passage, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
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
        contentScale = ContentScale.Crop,
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

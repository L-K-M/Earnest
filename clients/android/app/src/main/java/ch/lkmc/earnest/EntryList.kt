package ch.lkmc.earnest

import androidx.activity.compose.BackHandler
import androidx.compose.animation.core.animate
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import ch.lkmc.earnest.ReaderFilterChip as FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.TextMeasurer
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.constrainHeight
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.UnreadCounts
import ch.lkmc.earnest.core.typographyFor
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import kotlin.math.roundToInt

/** The feed owns one loaded article while its pane is off screen. Native
 * views still follow the lazy item's lifetime and large bodies stay out of saved state.
 *
 * [place] is how far into the expanded body the top of the list is, as a
 * fraction of its height. Folding, rotating and process restore change or
 * forget that height, and a pixel offset would then land many rows away. */
internal class FeedReadingState(
    val selection: MutableState<Long?>,
    val place: MutableState<Float?>,
    val sequence: MutableState<ArticleSequence>,
    val list: androidx.compose.foundation.lazy.LazyListState,
    val reader: ReaderLoadState,
    val viewport: InlineArticleState,
)

@Composable
internal fun rememberFeedReadingState(accountKey: String?): FeedReadingState {
    var owner by rememberSaveable { mutableStateOf(accountKey) }
    val selection = rememberSaveable { mutableStateOf<Long?>(null) }
    val sequence = rememberArticleSequence(accountKey)
    if (accountKey != null && owner != accountKey) {
        if (owner != null) selection.value = null
        owner = accountKey
    }
    val place = rememberSaveable(selection.value) { mutableStateOf<Float?>(null) }
    val list = key(owner) { rememberLazyListState() }
    return remember(selection.value, accountKey, list, place) {
        FeedReadingState(selection, place, sequence, list, ReaderLoadState(), InlineArticleState())
    }
}

private data class InlineScrollAnchor(val index: Int, val offset: Int)

/** How many pixels of [entryId]'s row are above the top of the list, when that row is the first on screen. */
private fun androidx.compose.foundation.lazy.LazyListState.depthInto(entryId: Long): Int? =
    layoutInfo.visibleItemsInfo.firstOrNull()?.takeIf { it.key == entryId && it.offset < 0 }?.let { -it.offset }

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun EntryListScreen(
    title: String,
    entries: List<CachedEntry>,
    states: Map<Long, ch.lkmc.earnest.core.State>,
    principalId: Long,
    subscriptionTitles: Map<Long, String>,
    subscriptionIcons: Map<Long, String>,
    briefs: List<ch.lkmc.earnest.db.BriefEntity>,
    principalNames: Map<Long, String>,
    entryTitles: Map<Long, String>,
    agentChanges: List<ch.lkmc.earnest.core.Activity>,
    sessionChanges: List<ch.lkmc.earnest.core.Activity>,
    agentChangesError: String?,
    unreadCounts: UnreadCounts? = null,
    unreadSyncedAt: Long? = null,
    unreadMode: Boolean = false,
    unreadOnly: Boolean? = null,
    onUnreadOnlyChange: (Boolean) -> Unit = {},
    onRevert: (Long) -> Unit,
    onOpen: (Long) -> Unit,
    onOpenAgentSelected: (Long) -> Unit,
    onLater: (Long) -> Unit,
    onDone: (Long) -> Unit,
    onKeep: (Long) -> Unit,
    onUnkeep: (Long) -> Unit,
    onMarkUnread: (Long) -> Unit,
    accountKey: String? = null,
    layoutOverride: ArticleLayout? = null,
    typography: (CachedEntry) -> ch.lkmc.earnest.core.ReaderTypography = { ch.lkmc.earnest.core.ReaderSettings().typographyFor(it.feedId) },
    // Expanding an article in Read in feed opens it. onLoadContent runs
    // again when a rotation or process restore reloads it, so it must not
    // stand in for this.
    onExpand: (Long) -> Unit = {},
    onLoadContent: suspend (Long) -> ContentUrlResult? = { null },
    swipe: ArticleSwipeState = rememberArticleSwipeState(),
    inlineContent: @Composable (ContentUrlResult, CachedEntry, InlineArticleState) -> Unit = { content, entry, viewport ->
        InlineArticleContent(content, typography(entry), viewport, swipe)
    },
    readingState: FeedReadingState = rememberFeedReadingState(accountKey),
    listState: androidx.compose.foundation.lazy.LazyListState = readingState.list,
) {
    val context = LocalContext.current
    var preferredLayout by remember { mutableStateOf(context.articleLayout()) }
    val layout = layoutOverride ?: preferredLayout
    var expandedId by readingState.selection
    var retry by remember { mutableStateOf(0) }
    val selectedEntry = remember(entries, expandedId) { expandedId?.let { id -> entries.firstOrNull { it.id == id } } }
    val expandedEntry = selectedEntry?.takeIf { layout == ArticleLayout.READING }
    val expanded = expandedEntry?.id
    // Keep the open article in the list when opening it marks it read.
    val visibleEntries = remember(entries, states, unreadOnly, expanded) {
        if (unreadOnly == true) entries.filter { it.id == expanded || isUnread(states[it.id]) } else entries
    }
    val inlineReader = readingState.reader
    val inlineViewport = readingState.viewport
    var listWidth by remember { androidx.compose.runtime.mutableIntStateOf(0) }
    val density = LocalDensity.current
    val expandedTypography = expandedEntry?.let(typography)
    val textZoom = context.readerZoom()
    val measuredListWidth = listWidth
    var place by readingState.place
    var sequence by readingState.sequence
    val headerCount = (if (briefs.isNotEmpty()) 1 else 0) +
        (if (agentChanges.isNotEmpty() || agentChangesError != null) 1 else 0)
    val itemsBeforeArticles = headerCount + (if (unreadMode) 1 else 0) +
        (if (layoutOverride == null || unreadOnly != null) 1 else 0)
    // Where an article's row is, or will be once it is the expanded one: an
    // expanded article off screen, or one about to be, has no layout info.
    val rowIndexOf = { id: Long ->
        itemsBeforeArticles + entries.asSequence().takeWhile { it.id != id }
            .count { unreadOnly != true || isUnread(states[it.id]) }
    }
    val currentRowIndexOf by rememberUpdatedState(rowIndexOf)
    var scrollAnchor by remember { mutableStateOf<InlineScrollAnchor?>(null) }
    // Read while composing, so losing the measurement runs the effect below again.
    val resumeState = inlineViewport.resume
    val lastExpanded = remember { arrayOfNulls<Long>(1) }
    // The article a swipe is bringing to the top. Near the end of a list its
    // row can only get there once the body has made the list tall enough.
    val revealing = remember { arrayOfNulls<Long>(1) }
    SideEffect {
        val previous = lastExpanded[0]
        lastExpanded[0] = expanded
        if (previous != null && expanded == null && scrollAnchor == null) {
            // The body left without a collapse: the layout went side by side or
            // the article left this shelf. Its deep offset would skip many rows.
            if (listState.depthInto(previous) != null) {
                listState.requestScrollToItem(listState.firstVisibleItemIndex)
            }
            inlineViewport.invalidate()
        }
        expandedTypography?.let { typography ->
            val previousHeight = inlineViewport.pixels
            inlineViewport.configure(measuredListWidth, typography, textZoom, density.density, density.fontScale)
            if (previousHeight > 0 && inlineViewport.pixels == 0) {
                // Reflow temporarily returns to the measuring height. A deep
                // body offset must stay on this article, not jump to later rows.
                listState.layoutInfo.visibleItemsInfo.firstOrNull { it.key == expanded && it.offset < 0 }?.let {
                    listState.requestScrollToItem(it.index)
                }
            }
        }
        if (selectedEntry != null && resumeState == ResumeState.UNPLACED && place != null) {
            // Wait on the headline until the body has a height to resume into.
            inlineViewport.resume = ResumeState.WAITING
            listState.requestScrollToItem(rowIndexOf(selectedEntry.id))
        }
    }
    LaunchedEffect(inlineViewport, listState) {
        snapshotFlow { listState.isScrollInProgress }.collect { scrolling ->
            if (!scrolling) return@collect
            revealing[0] = null
            if (inlineViewport.resume == ResumeState.WAITING) {
                inlineViewport.resume = ResumeState.ABANDONED
                place = null
            }
        }
    }
    LaunchedEffect(inlineViewport, listState, expanded) {
        val id = expanded ?: return@LaunchedEffect
        snapshotFlow { Triple(inlineViewport.loaded, inlineViewport.contentPx, inlineViewport.settled) }
            .collectLatest { (loaded, height, settled) ->
                if (height <= 0 || settled) return@collectLatest
                if (revealing[0] == id) listState.requestScrollToItem(currentRowIndexOf(id))
                if (!loaded) return@collectLatest
                delay(INLINE_SETTLE_MS)
                val header = inlineViewport.headerPx
                val held = inlineViewport.pixels
                val depth = listState.depthInto(id)
                val resumeAt = when {
                    inlineViewport.resume == ResumeState.WAITING -> place
                    // A shorter body, such as the cached copy, would end above the reader.
                    depth != null && height < held && depth > header + height -> (depth - header).toFloat() / held
                    else -> null
                }
                inlineViewport.settle()
                revealing[0] = null
                resumeAt?.let {
                    listState.requestScrollToItem(currentRowIndexOf(id), header + (it.coerceIn(0f, 1f) * height).roundToInt())
                }
            }
    }
    LaunchedEffect(inlineViewport, listState, expanded) {
        val id = expanded ?: return@LaunchedEffect
        snapshotFlow {
            // Until the body settles, its offsets do not describe the reader's place.
            if (inlineViewport.settled && inlineViewport.pixels > 0) {
                listState.layoutInfo.visibleItemsInfo.firstOrNull()?.let { it.key to it.offset }
            } else null
        }.collect { top ->
            val (key, offset) = top ?: return@collect
            val intoBody = -offset - inlineViewport.headerPx
            place = if (key == id && intoBody > 0) (intoBody.toFloat() / inlineViewport.pixels).coerceAtMost(1f) else null
        }
    }
    LaunchedEffect(expanded, accountKey, retry) {
        if (expanded != null && inlineReader.content == null) inlineReader.load(expanded, accountKey, onLoadContent)
    }
    fun snapshotOrder(id: Long) {
        sequence = ArticleSequence.around(visibleEntries.map { ArticleStep(it.id) }, ArticleStep(id))
    }
    fun changeExpansion(id: Long?) {
        if (id != null) snapshotOrder(id)
        revealing[0] = null
        val anchor = id ?: expanded
        scrollAnchor = listState.layoutInfo.visibleItemsInfo.firstOrNull { it.key == anchor }?.let { item ->
            // Keep a tapped headline in place; collapsing inside a body
            // returns to its header rather than reusing a deep body offset.
            // In the unread filter the previously expanded (now read) row
            // disappears when another article opens. Account for it above the tap.
            val previousIndex = if (unreadOnly == true && id != null && expanded != null &&
                !isUnread(states[expanded])) visibleEntries.indexOfFirst { it.id == expanded } else -1
            val losesRowAbove = previousIndex >= 0 && visibleEntries.indexOfFirst { it.id == id } > previousIndex
            InlineScrollAnchor(item.index - (if (losesRowAbove) 1 else 0), -item.offset.coerceAtLeast(0))
        }
        expandedId = id
        if (id != null) onExpand(id)
    }
    fun openReader(id: Long) {
        // From the expanded article the reader continues the order being swiped through.
        if (id != expanded || id !in sequence) snapshotOrder(id)
        onOpen(id)
    }
    val neighbour = { direction: ArticleDirection ->
        sequence.neighbour(expanded, direction) { id -> entries.any { it.id == id } }
    }
    val turn = { step: ArticleStep ->
        scrollAnchor = InlineScrollAnchor(rowIndexOf(step.entryId), 0)
        revealing[0] = step.entryId
        expandedId = step.entryId
        onExpand(step.entryId)
    }
    SideEffect { swipe.bind(neighbour, turn) }
    LaunchedEffect(expanded, scrollAnchor) {
        scrollAnchor?.let { anchor ->
            // Request against the new composition. An earlier request can
            // be consumed by the old tall row before it has collapsed.
            listState.requestScrollToItem(anchor.index, anchor.offset)
            scrollAnchor = null
        }
    }
    BackHandler(enabled = expanded != null) { changeExpansion(null) }
    LaunchedEffect(headerCount) {
        if (shouldRevealHeaders(listState.firstVisibleItemIndex, listState.firstVisibleItemScrollOffset, headerCount)) {
            listState.scrollToItem(0)
        }
    }
    Box(Modifier.fillMaxSize()) {
    LazyColumn(state = listState, modifier = Modifier.fillMaxSize().onSizeChanged { listWidth = it.width }, contentPadding = PaddingValues(bottom = 16.dp)) {
        if (layoutOverride == null || unreadOnly != null) {
            item(key = "reading-layout") {
                if (layoutOverride == null) ArticleLayoutControl(layout, onLayout = {
                    changeExpansion(null)
                    preferredLayout = it
                    context.setArticleLayout(it)
                })
                if (unreadOnly != null) {
                    FilterChip(
                        selected = unreadOnly,
                        onClick = { onUnreadOnlyChange(!unreadOnly) },
                        label = { Text("Unread only") },
                        modifier = Modifier.padding(horizontal = ReaderSpace.gutter),
                    )
                }
            }
        }
        if (unreadMode) {
            item(key = "unread-counts") {
                val text = when {
                    unreadCounts == null -> "Unread counts unavailable"
                    unreadSyncedAt != null -> "Unread ${unreadCounts.total} · last synced ${unreadSyncedAt.relativeTime()}"
                    else -> "Unread ${unreadCounts.total}"
                }
                Text(text, modifier = Modifier.fillMaxWidth().padding(16.dp), style = MaterialTheme.typography.bodyMedium)
            }
        }
        if (briefs.isNotEmpty()) {
            item(key = "briefs") {
                Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    briefs.forEach { brief ->
                        Text(
                            briefLabel(principalNames[brief.principalId], brief.principalId, brief.value),
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                }
            }
        }
        if (agentChanges.isNotEmpty() || agentChangesError != null) {
            item(key = "agent-changes") {
                AgentChangesNotice("While you were out", agentChanges, entryTitles, agentChangesError, onOpenAgentSelected, onRevert)
            }
        }
        items(visibleEntries, key = { it.id }) { entry ->
            val effective = states[entry.id] ?: SyncEngine.initialState(principalId, entry.id)
            Surface(
                modifier = Modifier.fillMaxWidth(),
                color = MaterialTheme.colorScheme.surface,
            ) {
                val isExpanded = expanded == entry.id
                Column(if (isExpanded) Modifier.articleSwipe(swipe) else Modifier) {
                    SwipeActionRow(
                        // On the open article a sideways drag turns to the next one.
                        enabled = !isExpanded,
                        modifier = if (isExpanded) Modifier.onSizeChanged { inlineViewport.headerPx = it.height } else Modifier,
                        onLater = { onLater(entry.id) },
                        onDone = { onDone(entry.id) },
                    ) {
                        EntryRow(
                            entry = entry,
                            state = effective,
                            source = subscriptionTitles[entry.subscriptionId].orEmpty(),
                            sourceIcon = subscriptionIcons[entry.subscriptionId],
                            readingInFeed = layout == ArticleLayout.READING,
                            expanded = isExpanded,
                            articleActions = if (isExpanded) articleActions(neighbour, turn) else emptyList(),
                            onClick = {
                                if (layout == ArticleLayout.READING) {
                                    changeExpansion(if (isExpanded) null else entry.id)
                                } else openReader(entry.id)
                            },
                            onOpenReader = { openReader(entry.id) },
                            onKeep = { onKeep(entry.id) },
                            onUnkeep = { onUnkeep(entry.id) },
                            onLater = { onLater(entry.id) },
                            onDone = { onDone(entry.id) },
                            onMarkUnread = { onMarkUnread(entry.id) },
                        )
                    }
                    if (isExpanded) {
                        when {
                            inlineReader.loading -> Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
                                CircularProgressIndicator(Modifier.size(24.dp))
                            }
                            inlineReader.content != null -> inlineContent(inlineReader.content!!, entry, inlineViewport)
                            else -> Column(Modifier.padding(horizontal = 16.dp)) {
                                Text(
                                    inlineReader.error ?: "Could not load this article. Connect and try again, or open a cached article.",
                                    color = MaterialTheme.colorScheme.error,
                                    style = MaterialTheme.typography.bodySmall,
                                )
                                TextButton(onClick = { retry++ }) { Text("Try again") }
                            }
                        }
                        FlowRow(Modifier.fillMaxWidth().padding(horizontal = ReaderSpace.md, vertical = ReaderSpace.xs), horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
                            FilledTonalButton(onClick = { openReader(entry.id) }) { Text("Open reader") }
                            TextButton(onClick = { changeExpansion(null) }) { Text("Collapse") }
                        }
                    }
                    HorizontalDivider(Modifier.padding(horizontal = ReaderSpace.gutter), color = MaterialTheme.colorScheme.outlineVariant)
                }
            }
        }
        item(key = "end") {
            if (visibleEntries.isEmpty()) {
                val message = if (unreadOnly == true) "Turn off Unread only to see read articles." else when (title) {
                    "Kept" -> "Keep an article from its menu to find it here. Your favorites stay close at hand."
                    "Later" -> "Save an article for Later from its menu, or give it a short swipe to the left."
                    "Done" -> "Articles you mark Done will appear here. You can always mark them unread again."
                    else -> "No cached articles for $title. Refresh to check for new articles, or add a source in Feeds."
                }
                QuietState(if (title == "Kept") "Your favorites belong here" else "Nothing here yet", message, icon = if (title == "Kept") Icons.Filled.Star else Icons.AutoMirrored.Filled.List)
            } else {
                Text("End of cached articles.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.fillMaxWidth().padding(ReaderSpace.lg))
            }
            if (sessionChanges.isNotEmpty() || agentChangesError != null) {
                AgentChangesNotice("Agent changes this session", sessionChanges, entryTitles, agentChangesError, onOpenAgentSelected, onRevert)
            }
        }
    }
    ArticleSwipeHint(swipe)
    }
}

private fun isUnread(state: ch.lkmc.earnest.core.State?): Boolean =
    state == null || (state.seen.value == JsonPrimitive(SyncEngine.VALUE_UNSEEN) &&
        state.shelf.value != JsonPrimitive(SyncEngine.VALUE_DONE))

private const val HEADLINE_MAX_LINES = 4

/**
 * Sizes a list headline for its unread (semibold) rendition in every state.
 * The regular weight is narrower and can wrap to fewer lines, so a row sized
 * by the shown weight would shrink as its article is read and move every row
 * below it; a read headline leaves its last reserved line empty instead.
 */
private fun Modifier.unreadHeadlineHeight(text: String, style: TextStyle, measurer: TextMeasurer) =
    layout { measurable, constraints ->
        val placeable = measurable.measure(constraints)
        val unread = measurer.measure(
            text,
            style.copy(fontWeight = FontWeight.SemiBold),
            overflow = TextOverflow.Ellipsis,
            maxLines = HEADLINE_MAX_LINES,
            constraints = Constraints(maxWidth = constraints.maxWidth),
        )
        val height = constraints.constrainHeight(maxOf(placeable.height, unread.size.height))
        layout(placeable.width, height) { placeable.place(0, 0) }
    }

@Composable
internal fun EntryRow(
    entry: CachedEntry,
    state: ch.lkmc.earnest.core.State,
    source: String,
    sourceIcon: String? = null,
    onClick: () -> Unit,
    readingInFeed: Boolean = false,
    expanded: Boolean = false,
    articleActions: List<CustomAccessibilityAction> = emptyList(),
    onOpenReader: () -> Unit = onClick,
    onKeep: () -> Unit = {},
    onUnkeep: () -> Unit = {},
    onLater: () -> Unit = {},
    onDone: () -> Unit = {},
    onMarkUnread: () -> Unit = {},
) {
    val unread = isUnread(state)
    val kept = state.kept.value == JsonPrimitive(true)
    var menuOpen by remember { mutableStateOf(false) }
    Column(
        Modifier.fillMaxWidth().clickable(
            onClickLabel = when {
                expanded -> "Collapse article"
                readingInFeed -> "Read in feed"
                else -> "Open reader"
            },
            onClick = onClick,
        )
            .semantics {
                stateDescription = if (unread) "Unread" else "Read"
                if (articleActions.isNotEmpty()) customActions = articleActions
            }
            .padding(start = ReaderSpace.gutter, end = ReaderSpace.xs, top = ReaderSpace.xs, bottom = ReaderSpace.gutter),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(6.dp).background(if (unread) LocalTones.current.orange else Color.Transparent, RoundedCornerShape(3.dp)))
            Spacer(Modifier.width(6.dp))
            if (sourceIcon != null) {
                MediaImage(sourceIcon, Modifier.size(16.dp), RoundedCornerShape(4.dp))
                Spacer(Modifier.width(6.dp))
            }
            Text(
                source.ifBlank { "Feed" },
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Spacer(Modifier.width(8.dp))
            Text(entry.firstSeenAt.relativeTime(), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            if (kept) Icon(Icons.Filled.Star, contentDescription = "Kept", tint = LocalTones.current.plum, modifier = Modifier.padding(start = 6.dp).size(16.dp))
            Box {
                IconButton(onClick = { menuOpen = true }) { Icon(Icons.Filled.MoreVert, contentDescription = "Article actions") }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    DropdownMenuItem(text = { Text("Open reader") }, onClick = { menuOpen = false; onOpenReader() })
                    DropdownMenuItem(text = { Text(if (kept) "Unkeep" else "Keep") }, onClick = { menuOpen = false; if (kept) onUnkeep() else onKeep() })
                    DropdownMenuItem(text = { Text("Later") }, onClick = { menuOpen = false; onLater() })
                    DropdownMenuItem(text = { Text("Done") }, onClick = { menuOpen = false; onDone() })
                    if (canMarkUnread(state)) {
                        DropdownMenuItem(text = { Text("Mark unread") }, onClick = { menuOpen = false; onMarkUnread() })
                    }
                }
            }
        }
        Row(Modifier.padding(end = 8.dp), verticalAlignment = Alignment.Top) {
            val headline = entry.title.ifBlank { "Untitled" }
            val headlineStyle = MaterialTheme.typography.titleMedium
            Text(
                headline,
                style = headlineStyle,
                fontWeight = if (unread) FontWeight.SemiBold else FontWeight.Normal,
                maxLines = HEADLINE_MAX_LINES,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f).testTag("headline-${entry.id}").then(
                    if (unread) Modifier else Modifier.unreadHeadlineHeight(headline, headlineStyle, rememberTextMeasurer()),
                ),
            )
            entry.thumbnailUrl?.let { thumbnail ->
                Spacer(Modifier.width(12.dp))
                MediaImage(thumbnail, Modifier.size(72.dp), RoundedCornerShape(16.dp))
            }
        }
        if (readingInFeed && expanded) {
            Row(Modifier.padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(if (expanded) "Reading in feed" else "Read in feed", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
                Icon(
                    if (expanded) Icons.Filled.KeyboardArrowUp else Icons.Filled.KeyboardArrowDown,
                    contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(16.dp),
                )
            }
        }
    }
}

/** A short swipe reveals Later, a long swipe reveals Done; release to apply. */
@Composable
private fun SwipeActionRow(
    enabled: Boolean,
    onLater: () -> Unit,
    onDone: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    val density = LocalDensity.current
    val laterThreshold = with(density) { 96.dp.toPx() }
    val doneThreshold = with(density) { 240.dp.toPx() }
    var offset by remember { androidx.compose.runtime.mutableFloatStateOf(0f) }
    var settling by remember { mutableStateOf<kotlinx.coroutines.Job?>(null) }
    val scope = rememberCoroutineScope()
    val haptics = LocalHapticFeedback.current
    fun settle() {
        settling?.cancel()
        settling = scope.launch {
            animate(offset, 0f, animationSpec = spring(dampingRatio = 0.85f, stiffness = 500f)) { value, _ -> offset = value }
        }
    }
    Box(modifier.fillMaxWidth()) {
        val target = swipeTargetFor(-offset, laterThreshold, doneThreshold)
        // Below the Later threshold the reveal already promises Later, so it
        // keeps the Later tone rather than fading to neutral.
        val reveal = lerp(
            MaterialTheme.colorScheme.surface,
            if (target == SwipeTarget.DONE) LocalTones.current.green else LocalTones.current.amber,
            0.18f,
        )
        Box(
            Modifier
                .matchParentSize()
                .background(reveal),
            contentAlignment = Alignment.CenterEnd,
        ) {
            Text(
                target?.label ?: SwipeTarget.LATER.label,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.padding(end = 24.dp),
            )
        }
        // The opaque foreground keeps the reveal hidden until the row moves.
        Box(
            Modifier
                .offset { IntOffset(offset.roundToInt(), 0) }
                .background(MaterialTheme.colorScheme.surface)
                .pointerInput(enabled) {
                    if (!enabled) return@pointerInput
                    detectHorizontalDragGestures(
                        onDragStart = { settling?.cancel() },
                        onHorizontalDrag = { change, dragAmount ->
                            change.consume()
                            val previous = swipeTargetFor(-offset, laterThreshold, doneThreshold)
                            offset = (offset + dragAmount).coerceIn(-doneThreshold * 1.2f, 0f)
                            val next = swipeTargetFor(-offset, laterThreshold, doneThreshold)
                            if (next != null && next != previous) haptics.performHapticFeedback(HapticFeedbackType.GestureThresholdActivate)
                        },
                        onDragCancel = { settle() },
                        onDragEnd = {
                            when (swipeTargetFor(-offset, laterThreshold, doneThreshold)) {
                                SwipeTarget.DONE -> onDone()
                                SwipeTarget.LATER -> onLater()
                                null -> Unit
                            }
                            settle()
                        },
                    )
                },
        ) {
            content()
        }
    }
}

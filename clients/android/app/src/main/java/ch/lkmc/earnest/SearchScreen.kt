package ch.lkmc.earnest

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.SearchMatch
import ch.lkmc.earnest.core.SearchMode
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncSnapshot

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun SearchScreen(
    search: SearchModel,
    accountKey: String?,
    snapshot: SyncSnapshot,
    onOpen: (SearchMatch) -> Unit,
) {
    val state by search.state.collectAsState()
    var confirmingDelete by remember(accountKey) { mutableStateOf(false) }
    var filtersExpanded by rememberSaveable(accountKey) { mutableStateOf(false) }
    val keyboard = LocalSoftwareKeyboardController.current
    val focus = LocalFocusManager.current
    val feeds = remember(snapshot) {
        snapshot.objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }.map { it.id to it.name }
    }
    val folders = remember(snapshot) {
        snapshot.objects.filter { it.kind == SyncEngine.OBJECT_FOLDER && !it.deleted }.map { it.id to it.name }
    }
    LaunchedEffect(accountKey) { if (accountKey != null) search.loadMetadata() }
    val submit = {
        keyboard?.hide()
        focus.clearFocus()
        search.submit()
    }
    val filterCount = listOf(
        state.filter.subscriptionId != null,
        state.filter.folderId != null,
        state.filter.limit != null,
        state.filter.mode == SearchMode.HYBRID && state.embeddingStatus == "enabled",
    ).count { it }

    val sources = remember(feeds) { feeds.toMap() }
    val hybridAvailable = state.embeddingStatus == "enabled"

    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
        LazyColumn(
            Modifier.widthIn(max = ReaderSpace.contentWidth).fillMaxSize().imePadding(),
            contentPadding = PaddingValues(
                start = ReaderSpace.gutter,
                end = ReaderSpace.gutter,
                top = ReaderSpace.xxs,
                bottom = ReaderSpace.xl,
            ),
            verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
        ) {
            item("query") {
                Column(verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
                    QueryField(state.query, search::setQuery, submit)
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
                        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
                        itemVerticalAlignment = Alignment.CenterVertically,
                    ) {
                        PrimaryButton("Search", submit, enabled = state.query.isNotBlank())
                        SecondaryButton(
                            if (filterCount == 0) "Filters" else "Filters ($filterCount)",
                            onClick = { filtersExpanded = !filtersExpanded },
                            modifier = Modifier.heightIn(min = QueryActionHeight)
                                .semantics { stateDescription = if (filtersExpanded) "Expanded" else "Collapsed" },
                            icon = if (filtersExpanded) EarnestIcons.ChevronUp else EarnestIcons.ChevronDown,
                        )
                    }
                    AnimatedVisibility(visible = filtersExpanded) {
                        SettingsGroup("Filter by") {
                            val modes = if (hybridAvailable) listOf(SearchMode.FULLTEXT to "Full text", SearchMode.HYBRID to "Hybrid") else listOf(SearchMode.FULLTEXT to "Full text")
                            SearchChoice("Mode", if (hybridAvailable) state.filter.mode else SearchMode.FULLTEXT, modes) { search.setFilter(state.filter.copy(mode = it)) }
                            SearchChoice("Feed", state.filter.subscriptionId, listOf<Pair<Long?, String>>(null to "All feeds") + feeds) { search.setFilter(state.filter.copy(subscriptionId = it)) }
                            SearchChoice("Folder", state.filter.folderId, listOf<Pair<Long?, String>>(null to "All folders") + folders) { search.setFilter(state.filter.copy(folderId = it)) }
                            val sizes = (listOf<Long?>(null, 20, 50, 100, 200) + state.filter.limit).distinct()
                            SearchChoice("Page size", state.filter.limit, sizes.map { it to (it?.toString() ?: "Server default") }) { search.setFilter(state.filter.copy(limit = it)) }
                        }
                    }
                    if (state.embeddingStatus == "paused") SearchHint("Hybrid search is paused. Full-text search is available.")
                    if (state.filter.mode == SearchMode.HYBRID && hybridAvailable) {
                        SearchHint("Hybrid search can make an embedding call and searches a bounded window. Use Full text to page further.")
                    }
                }
            }
            if (state.savedSearches.isNotEmpty()) {
                item("saved") {
                    Column {
                        SectionHeader("Saved searches", action = {
                            if (state.selectedSavedId != null) {
                                QuietButton("New saved search", onClick = { search.selectSaved(null) }, enabled = !state.saving)
                            }
                        })
                        LazyRow(horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
                            items(state.savedSearches, key = { "saved:${it.id}" }) { saved ->
                                ReaderFilterChip(
                                    selected = state.selectedSavedId == saved.id,
                                    onClick = { if (!state.saving) search.selectSaved(saved) },
                                    enabled = !state.saving,
                                    label = { Text(saved.name) },
                                )
                            }
                        }
                    }
                }
            }
            state.metadataError?.let { error ->
                item("metadata-error") {
                    NoticeBanner(
                        error,
                        tone = NoticeTone.ERROR,
                        actionLabel = if (!state.loadingMetadata) "Retry search setup" else null,
                        onAction = search::loadMetadata,
                    )
                }
            }
            if (state.loadingMetadata) {
                item("metadata-loading") {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
                        CircularProgressIndicator(Modifier.size(SmallSpinner), strokeWidth = 2.dp)
                        SearchHint("Loading search setup…")
                    }
                }
            }
            if (state.submittedQuery.isNotEmpty()) {
                item("results-heading") {
                    Text(
                        "Results for “${state.submittedQuery}”",
                        style = MaterialTheme.typography.titleLarge,
                        modifier = Modifier.padding(top = ReaderSpace.sm).semantics { heading() },
                    )
                }
                val articles = state.matches.filter { it.kind == "entry" }
                val notes = state.matches.filter { it.kind == "agent_note" }
                if (articles.isNotEmpty()) {
                    item("articles-heading") { SectionHeader("Articles") }
                    items(articles, key = { "match:${it.key}" }) { match -> SearchResult(match, sources[match.subscriptionId], onOpen) }
                }
                if (notes.isNotEmpty()) {
                    item("notes-heading") { SectionHeader("Agent notes") }
                    items(notes, key = { "match:${it.key}" }) { match -> SearchResult(match, null, onOpen) }
                }
                if (!state.loading && state.matches.isEmpty() && state.error == null && state.status == null) {
                    item("empty") {
                        QuietState("No matches yet", "Try a different phrase, or broaden your feed and folder filters.", icon = EarnestIcons.Search, tone = LocalTones.current.blue)
                    }
                }
            } else {
                item("help") {
                    QuietState(
                        "Find something worth revisiting",
                        "Search publisher content and separately labelled agent notes. Search needs a connection to your server.",
                        icon = EarnestIcons.Search,
                        tone = LocalTones.current.blue,
                    )
                }
            }
            if (state.loading) item("loading") { LoadingState("Searching your feeds…") }
            state.status?.let { status ->
                item("status") { NoticeBanner("Hybrid search is $status.", actionLabel = "Use full-text search", onAction = search::useFullText) }
            }
            state.error?.let { error -> item("error") { NoticeBanner(error, tone = NoticeTone.ERROR) } }
            state.notice?.let { notice -> item("notice") { NoticeBanner(notice) } }
            if (state.nextCursor != null) {
                item("more") {
                    SecondaryButton("More search results", search::loadMore, Modifier.fillMaxWidth(), enabled = !state.loading)
                }
            }
            if (state.submittedQuery.isNotEmpty()) {
                item("save") {
                    SettingsGroup("Save this search") {
                        SearchHint("Keep this query and its submitted filters as a reusable view.")
                        OutlinedTextField(
                            value = state.savedName,
                            onValueChange = search::setSavedName,
                            label = { Text("Saved search name") },
                            singleLine = true,
                            enabled = !state.saving,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
                            SecondaryButton("Save as new", search::saveNew, enabled = !state.saving && state.savedName.isNotBlank(), busy = state.saving)
                            if (state.selectedSavedId != null) {
                                QuietButton("Rename", search::rename, enabled = !state.saving && state.savedName.isNotBlank())
                                QuietButton("Delete", { confirmingDelete = true }, enabled = !state.saving, color = MaterialTheme.colorScheme.error)
                            }
                        }
                        if (state.saving) SearchHint("Saving search change…")
                    }
                }
            }
        }
    }
    if (confirmingDelete) {
        AlertDialog(
            onDismissRequest = { confirmingDelete = false },
            title = { Text("Delete this saved search?") },
            text = { Text("Only the saved view is removed. Your articles stay in Earnest.") },
            confirmButton = {
                QuietButton("Delete", { confirmingDelete = false; search.delete() }, color = MaterialTheme.colorScheme.error)
            },
            dismissButton = { QuietButton("Cancel", { confirmingDelete = false }) },
        )
    }
}

private val SmallSpinner = 18.dp

// Filters stands as tall as Search beside it.
private val QueryActionHeight = 56.dp
private const val SNIPPET_LINES = 3

/** The field takes focus when Search opens empty; returning to results leaves the keyboard down. */
@Composable
private fun QueryField(query: String, onQuery: (String) -> Unit, onSubmit: () -> Unit) {
    val scheme = MaterialTheme.colorScheme
    val requester = remember { FocusRequester() }
    var focusOnOpen by rememberSaveable { mutableStateOf(query.isEmpty()) }
    OutlinedTextField(
        value = query,
        onValueChange = onQuery,
        placeholder = { Text("Search your feeds") },
        leadingIcon = { Icon(EarnestIcons.Search, contentDescription = null) },
        trailingIcon = {
            if (query.isNotEmpty()) ActionIconButton(EarnestIcons.Close, "Clear search", { onQuery("") })
        },
        shape = CircleShape,
        colors = OutlinedTextFieldDefaults.colors(
            focusedContainerColor = scheme.surfaceContainerHigh,
            unfocusedContainerColor = scheme.surfaceContainerHigh,
            unfocusedBorderColor = if (scheme.isMonochrome) scheme.outline else Color.Transparent,
        ),
        singleLine = true,
        modifier = Modifier.fillMaxWidth().focusRequester(requester),
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
        keyboardActions = KeyboardActions(onSearch = { onSubmit() }),
    )
    LaunchedEffect(Unit) {
        if (!focusOnOpen) return@LaunchedEffect
        requester.requestFocus()
        focusOnOpen = false
    }
}

@Composable
private fun SearchHint(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@Composable
internal fun SearchResult(match: SearchMatch, source: String?, onOpen: (SearchMatch) -> Unit) {
    val scheme = MaterialTheme.colorScheme
    Column(Modifier.fillMaxWidth()) {
        Column(
            Modifier.fillMaxWidth()
                .clickable(onClickLabel = "Open article", role = Role.Button, onClick = { onOpen(match) })
                .padding(vertical = ReaderSpace.sm),
            verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
        ) {
            if (match.kind == "agent_note") {
                TagPill(
                    "Agent note · ${match.agentName?.takeIf(String::isNotEmpty) ?: "Unknown agent"}",
                    icon = EarnestIcons.Sparkle,
                    colors = toneColors(scheme, LocalTones.current.purple),
                )
            } else {
                Text(source ?: "Article", style = MaterialTheme.typography.labelMedium, color = scheme.onSurfaceVariant)
            }
            Text(match.title.ifEmpty { "Untitled" }, style = StoryType.row)
            Text(
                match.snippet,
                style = MaterialTheme.typography.bodyMedium,
                color = scheme.onSurfaceVariant,
                maxLines = SNIPPET_LINES,
                overflow = TextOverflow.Ellipsis,
            )
        }
        HorizontalDivider(color = scheme.outlineVariant)
    }
}

/** A filter as a full-width row: what it filters, then its current value. */
@Composable
private fun <T> SearchChoice(label: String, value: T, choices: List<Pair<T, String>>, onSelect: (T) -> Unit) {
    val scheme = MaterialTheme.colorScheme
    var expanded by remember { mutableStateOf(false) }
    Box(Modifier.fillMaxWidth()) {
        Row(
            Modifier.fillMaxWidth().heightIn(min = TouchTarget)
                .clickable(role = Role.DropdownList, onClick = { expanded = true }),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
        ) {
            Text(label, style = MaterialTheme.typography.titleSmall)
            Text(
                choices.firstOrNull { it.first == value }?.second ?: "Unavailable selection ($value)",
                style = MaterialTheme.typography.bodyMedium,
                color = scheme.onSurfaceVariant,
                textAlign = TextAlign.End,
                modifier = Modifier.weight(1f),
            )
            Icon(EarnestIcons.ChevronDown, contentDescription = null, tint = scheme.onSurfaceVariant)
        }
        AppMenu(expanded = expanded, onDismiss = { expanded = false }) {
            choices.forEach { (choice, title) ->
                AppMenuItem(title, { expanded = false; onSelect(choice) }, selected = choice == value)
            }
        }
    }
}

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
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilledTonalButton
import ch.lkmc.earnest.ReaderFilterChip as FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
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
    var savedExpanded by rememberSaveable(accountKey) { mutableStateOf(false) }
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

    LazyColumn(
        Modifier.fillMaxSize().imePadding(),
        contentPadding = PaddingValues(start = ReaderSpace.gutter, end = ReaderSpace.gutter, top = ReaderSpace.sm, bottom = ReaderSpace.xl),
        verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
    ) {
        item("query") {
            Column(verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
                OutlinedTextField(
                    value = state.query,
                    onValueChange = search::setQuery,
                    label = { Text("Search your feeds") },
                    leadingIcon = { Icon(Icons.Filled.Search, contentDescription = null) },
                    trailingIcon = {
                        if (state.query.isNotEmpty()) IconButton(onClick = { search.setQuery("") }) { Icon(Icons.Filled.Close, contentDescription = "Clear search") }
                    },
                    shape = RoundedCornerShape(24.dp),
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                    keyboardActions = KeyboardActions(onSearch = { submit() }),
                )
                FlowRow(horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
                    Button(onClick = submit, enabled = state.query.isNotBlank()) { Text("Search") }
                    FilledTonalButton(onClick = { filtersExpanded = !filtersExpanded }, modifier = Modifier.semantics { stateDescription = if (filtersExpanded) "Expanded" else "Collapsed" }) {
                        Text(if (filterCount == 0) "Filters" else "Filters ($filterCount)")
                        Icon(if (filtersExpanded) Icons.Filled.KeyboardArrowUp else Icons.Filled.KeyboardArrowDown, contentDescription = null)
                    }
                }
                AnimatedVisibility(visible = filtersExpanded) {
                    Column(verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs)) {
                        val modes = if (state.embeddingStatus == "enabled") listOf(SearchMode.FULLTEXT to "Full text", SearchMode.HYBRID to "Hybrid") else listOf(SearchMode.FULLTEXT to "Full text")
                        SearchChoice("Mode", if (state.embeddingStatus == "enabled") state.filter.mode else SearchMode.FULLTEXT, modes) { search.setFilter(state.filter.copy(mode = it)) }
                        SearchChoice("Feed", state.filter.subscriptionId, listOf<Pair<Long?, String>>(null to "All feeds") + feeds) { search.setFilter(state.filter.copy(subscriptionId = it)) }
                        SearchChoice("Folder", state.filter.folderId, listOf<Pair<Long?, String>>(null to "All folders") + folders) { search.setFilter(state.filter.copy(folderId = it)) }
                        val sizes = (listOf<Long?>(null, 20, 50, 100, 200) + state.filter.limit).distinct()
                        SearchChoice("Page size", state.filter.limit, sizes.map { it to (it?.toString() ?: "Server default") }) { search.setFilter(state.filter.copy(limit = it)) }
                    }
                }
                if (state.embeddingStatus == "paused") Text("Hybrid search is paused. Full-text search is available.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (state.filter.mode == SearchMode.HYBRID && state.embeddingStatus == "enabled") Text("Hybrid search can make an embedding call and searches a bounded window. Use Full text to page further.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (state.savedSearches.isNotEmpty()) {
            item("saved-heading") {
                TextButton(onClick = { savedExpanded = !savedExpanded }, modifier = Modifier.semantics { heading(); stateDescription = if (savedExpanded) "Expanded" else "Collapsed" }) {
                    Text("Saved searches (${state.savedSearches.size})")
                    Icon(if (savedExpanded) Icons.Filled.KeyboardArrowUp else Icons.Filled.KeyboardArrowDown, contentDescription = null)
                }
            }
            if (savedExpanded) {
                items(state.savedSearches, key = { "saved:${it.id}" }) { saved ->
                    FilterChip(selected = state.selectedSavedId == saved.id, onClick = { if (!state.saving) search.selectSaved(saved) }, enabled = !state.saving, label = { Text(saved.name) })
                }
                item("new-saved") { TextButton(onClick = { search.selectSaved(null) }, enabled = !state.saving) { Text("New saved search") } }
            }
        }
        state.metadataError?.let { error ->
            item("metadata-error") { NoticeBanner(error, tone = NoticeTone.ERROR, actionLabel = if (!state.loadingMetadata) "Retry search setup" else null, onAction = search::loadMetadata) }
        }
        if (state.loadingMetadata) item("metadata-loading") {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
                CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                Text("Loading search setup…", style = MaterialTheme.typography.bodySmall)
            }
        }
        if (state.submittedQuery.isNotEmpty()) {
            item("results-heading") {
                Text("Results for “${state.submittedQuery}”", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(top = ReaderSpace.sm).semantics { heading() })
            }
            val articles = state.matches.filter { it.kind == "entry" }
            val notes = state.matches.filter { it.kind == "agent_note" }
            if (articles.isNotEmpty()) {
                item("articles-heading") { SearchHeading("Articles") }
                items(articles, key = { "match:${it.key}" }) { match -> SearchResult(match, feeds.firstOrNull { it.first == match.subscriptionId }?.second, onOpen) }
            }
            if (notes.isNotEmpty()) {
                item("notes-heading") { SearchHeading("Agent notes") }
                items(notes, key = { "match:${it.key}" }) { match -> SearchResult(match, null, onOpen) }
            }
            if (!state.loading && state.matches.isEmpty() && state.error == null && state.status == null) {
                item("empty") { QuietState("No matches yet", "Try a different phrase, or broaden your feed and folder filters.", icon = Icons.Filled.Search) }
            }
        } else {
            item("help") { QuietState("Find something worth revisiting", "Search publisher content and separately labelled agent notes. Search needs a connection to your server.", icon = Icons.Filled.Search) }
        }
        if (state.loading) item("loading") { LoadingState("Searching your feeds…") }
        state.status?.let { status -> item("status") { NoticeBanner("Hybrid search is $status.", actionLabel = "Use full-text search", onAction = search::useFullText) } }
        state.error?.let { error -> item("error") { NoticeBanner(error, tone = NoticeTone.ERROR) } }
        state.notice?.let { notice -> item("notice") { NoticeBanner(notice) } }
        if (state.nextCursor != null) item("more") {
            FilledTonalButton(onClick = search::loadMore, enabled = !state.loading, modifier = Modifier.fillMaxWidth()) { Text("More search results") }
        }
        if (state.submittedQuery.isNotEmpty()) item("save") {
            SettingsGroup("Save this search") {
                Text("Keep this query and its submitted filters as a reusable view.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                OutlinedTextField(value = state.savedName, onValueChange = search::setSavedName, label = { Text("Saved search name") }, singleLine = true, enabled = !state.saving, modifier = Modifier.fillMaxWidth())
                FlowRow(horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
                    Button(onClick = search::saveNew, enabled = !state.saving && state.savedName.isNotBlank()) { Text("Save as new") }
                    if (state.selectedSavedId != null) {
                        TextButton(onClick = search::rename, enabled = !state.saving && state.savedName.isNotBlank()) { Text("Rename") }
                        TextButton(onClick = { confirmingDelete = true }, enabled = !state.saving) { Text("Delete", color = MaterialTheme.colorScheme.error) }
                    }
                }
                if (state.saving) Text("Saving search change…", style = MaterialTheme.typography.bodySmall)
            }
        }
    }
    if (confirmingDelete) {
        AlertDialog(
            onDismissRequest = { confirmingDelete = false },
            title = { Text("Delete this saved search?") },
            text = { Text("Only the saved view is removed. Your articles stay in Earnest.") },
            confirmButton = { TextButton(onClick = { confirmingDelete = false; search.delete() }) { Text("Delete") } },
            dismissButton = { TextButton(onClick = { confirmingDelete = false }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun SearchHeading(text: String) {
    Text(text, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.semantics { heading() })
}

@Composable
internal fun SearchResult(match: SearchMatch, source: String?, onOpen: (SearchMatch) -> Unit) {
    Column(Modifier.fillMaxWidth().clickable(onClickLabel = "Open article", role = Role.Button, onClick = { onOpen(match) }).padding(vertical = ReaderSpace.sm), verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
        Text(
            if (match.kind == "agent_note") "Agent note · ${match.agentName?.takeIf(String::isNotEmpty) ?: "Unknown agent"}" else source ?: "Article",
            style = MaterialTheme.typography.labelMedium,
            color = if (match.kind == "agent_note") LocalTones.current.plum else MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(match.title.ifEmpty { "Untitled" }, style = MaterialTheme.typography.titleMedium)
        Text(match.snippet, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 4, overflow = TextOverflow.Ellipsis)
    }
    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
}

@Composable
private fun <T> SearchChoice(label: String, value: T, choices: List<Pair<T, String>>, onSelect: (T) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    Box {
        TextButton(onClick = { expanded = true }) { Text("$label: ${choices.firstOrNull { it.first == value }?.second ?: "Unavailable selection ($value)"}") }
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            choices.forEach { (choice, title) -> DropdownMenuItem(text = { Text(title) }, onClick = { expanded = false; onSelect(choice) }) }
        }
    }
}

package ch.lkmc.earnest

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
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
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.SearchMatch
import ch.lkmc.earnest.core.SearchMode
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncSnapshot

@Composable
internal fun SearchScreen(
    search: SearchModel,
    accountKey: String?,
    snapshot: SyncSnapshot,
    onOpen: (SearchMatch) -> Unit,
) {
    val state by search.state.collectAsState()
    var confirmingDelete by remember { mutableStateOf(false) }
    val feeds = remember(snapshot) {
        snapshot.objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }.map { it.id to it.name }
    }
    val folders = remember(snapshot) {
        snapshot.objects.filter { it.kind == SyncEngine.OBJECT_FOLDER && !it.deleted }.map { it.id to it.name }
    }
    LaunchedEffect(accountKey) { if (accountKey != null) search.loadMetadata() }

    LazyColumn(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        item("query") {
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                OutlinedTextField(
                    value = state.query,
                    onValueChange = search::setQuery,
                    label = { Text("Search your feeds") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                    keyboardActions = KeyboardActions(onSearch = { search.submit() }),
                )
                TextButton(onClick = search::submit, enabled = state.query.isNotBlank()) { Text("Search") }
                val modes = if (state.embeddingStatus == "enabled") {
                    listOf(SearchMode.FULLTEXT to "Full text", SearchMode.HYBRID to "Hybrid")
                } else listOf(SearchMode.FULLTEXT to "Full text")
                SearchChoice("Mode", if (state.embeddingStatus == "enabled") state.filter.mode else SearchMode.FULLTEXT, modes) {
                    search.setFilter(state.filter.copy(mode = it))
                }
                SearchChoice("Feed", state.filter.subscriptionId, listOf<Pair<Long?, String>>(null to "All feeds") + feeds) {
                    search.setFilter(state.filter.copy(subscriptionId = it))
                }
                SearchChoice("Folder", state.filter.folderId, listOf<Pair<Long?, String>>(null to "All folders") + folders) {
                    search.setFilter(state.filter.copy(folderId = it))
                }
                val sizes = (listOf<Long?>(null, 20, 50, 100, 200) + state.filter.limit).distinct()
                SearchChoice("Page size", state.filter.limit, sizes.map { it to (it?.toString() ?: "Server default") }) {
                    search.setFilter(state.filter.copy(limit = it))
                }
                if (state.embeddingStatus == "paused") {
                    Text("Hybrid search is paused. Full-text search is available.", style = MaterialTheme.typography.bodySmall)
                }
                if (state.filter.mode == SearchMode.HYBRID && state.embeddingStatus == "enabled") {
                    Text("Hybrid search can make an embedding call and searches a bounded window. Use Full text to page further.", style = MaterialTheme.typography.bodySmall)
                }
            }
        }
        if (state.savedSearches.isNotEmpty()) {
            item("saved-heading") { SearchHeading("Saved searches") }
            items(state.savedSearches, key = { "saved:${it.id}" }) { saved ->
                TextButton(onClick = { search.selectSaved(saved) }, enabled = !state.saving) {
                    Text(saved.name + if (state.selectedSavedId == saved.id) " (selected)" else "")
                }
            }
            item("new-saved") { TextButton(onClick = { search.selectSaved(null) }, enabled = !state.saving) { Text("New saved search") } }
        }
        state.metadataError?.let { error ->
            item("metadata-error") {
                Column(Modifier.padding(horizontal = 16.dp)) {
                    Text(error, color = MaterialTheme.colorScheme.error)
                    TextButton(onClick = search::loadMetadata, enabled = !state.loadingMetadata) { Text("Retry search setup") }
                }
            }
        }
        if (state.loadingMetadata) item("metadata-loading") { Text("Loading search setup…", Modifier.padding(horizontal = 16.dp)) }
        if (state.submittedQuery.isNotEmpty()) {
            item("articles-heading") { SearchHeading("Articles") }
            items(state.matches.filter { it.kind == "entry" }, key = { "match:${it.key}" }) { match ->
                SearchResult(match, onOpen)
            }
            item("notes-heading") { SearchHeading("Agent notes") }
            items(state.matches.filter { it.kind == "agent_note" }, key = { "match:${it.key}" }) { match ->
                SearchResult(match, onOpen)
            }
            if (!state.loading && state.matches.isEmpty() && state.error == null && state.status == null) {
                item("empty") { Text("No matches.", Modifier.padding(horizontal = 16.dp)) }
            }
        } else {
            item("help") { Text("Search publisher content and separately labelled agent notes.", Modifier.padding(horizontal = 16.dp)) }
        }
        if (state.loading) item("loading") { CircularProgressIndicator(Modifier.padding(16.dp)) }
        state.status?.let { status ->
            item("status") {
                Column(Modifier.padding(horizontal = 16.dp)) {
                    Text("Hybrid search is $status.")
                    TextButton(onClick = search::useFullText) { Text("Use full-text search") }
                }
            }
        }
        state.error?.let { error -> item("error") { Text(error, Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.error) } }
        state.notice?.let { notice -> item("notice") { Text(notice, Modifier.padding(horizontal = 16.dp)) } }
        if (state.nextCursor != null) {
            item("more") { TextButton(onClick = search::loadMore, enabled = !state.loading) { Text("More search results") } }
        }
        if (state.submittedQuery.isNotEmpty()) {
            item("save") {
                Column(Modifier.padding(16.dp)) {
                    OutlinedTextField(
                        value = state.savedName,
                        onValueChange = search::setSavedName,
                        label = { Text("Saved search name") },
                        singleLine = true,
                        enabled = !state.saving,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Row {
                        TextButton(onClick = search::saveNew, enabled = !state.saving) { Text("Save as new") }
                        if (state.selectedSavedId != null) {
                            TextButton(onClick = search::rename, enabled = !state.saving) { Text("Rename") }
                            TextButton(onClick = { confirmingDelete = true }, enabled = !state.saving) { Text("Delete") }
                        }
                    }
                    if (state.saving) Text("Saving search change…")
                }
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
    Text(text, Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.titleSmall)
}

@Composable
private fun SearchResult(match: SearchMatch, onOpen: (SearchMatch) -> Unit) {
    TextButton(onClick = { onOpen(match) }, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(match.title.ifEmpty { "Untitled" }, style = MaterialTheme.typography.titleMedium)
            if (match.kind == "agent_note") {
                Text("Agent note · ${match.agentName?.takeIf(String::isNotEmpty) ?: "Unknown agent"}", style = MaterialTheme.typography.labelMedium)
            }
            Text(match.snippet, style = MaterialTheme.typography.bodyMedium)
        }
    }
}

@Composable
private fun <T> SearchChoice(label: String, value: T, choices: List<Pair<T, String>>, onSelect: (T) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    Box {
        TextButton(onClick = { expanded = true }) {
            Text("$label: ${choices.firstOrNull { it.first == value }?.second ?: "Unavailable selection ($value)"}")
        }
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            choices.forEach { (choice, title) ->
                DropdownMenuItem(text = { Text(title) }, onClick = { expanded = false; onSelect(choice) })
            }
        }
    }
}

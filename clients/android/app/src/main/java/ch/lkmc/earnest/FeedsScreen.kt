package ch.lkmc.earnest

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import ch.lkmc.earnest.ReaderFilterChip as FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.MAX_OPML_IMPORT_BYTES
import ch.lkmc.earnest.core.OpmlSummary
import ch.lkmc.earnest.core.River
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject
import ch.lkmc.earnest.core.UnreadCounts
import ch.lkmc.earnest.core.health
import ch.lkmc.earnest.core.iconUrl
import ch.lkmc.earnest.core.readAtMost
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

@Composable
@OptIn(ExperimentalLayoutApi::class)
internal fun FeedsScreen(
    model: AppViewModel,
    state: UiState,
    unreadCounts: UnreadCounts?,
    unreadSyncedAt: Long?,
    unreadMode: Boolean,
    onSubscribe: () -> Unit,
    onFolder: (Long) -> Unit,
    onFeed: (Long) -> Unit,
    onUnsubscribe: (Long) -> Unit,
    onCadence: (Long, String) -> Unit,
) {
    val opml = state.opml
    var readingOpml by remember(state.accountKey) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val importPicker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        val accountAtPick = state.accountKey
        readingOpml = true
        scope.launch {
            // Read at most one byte past the limit, so a huge file picked by
            // mistake is refused without loading it all into memory.
            val document = withContext(Dispatchers.IO) {
                runCatching {
                    context.contentResolver.openInputStream(uri)?.use { readAtMost(it, MAX_OPML_IMPORT_BYTES) }
                }.getOrNull()
            }
            readingOpml = false
            // The file was picked for one account; never import it into an
            // account that signed in while it was being read.
            if (model.state.value.accountKey != accountAtPick) return@launch
            when {
                document == null -> model.opmlFileFailed("Could not read that file.")
                document.size > MAX_OPML_IMPORT_BYTES -> model.opmlFileFailed("The OPML file is larger than 8 MiB.")
                else -> model.importOpml(document)
            }
        }
    }
    val exportPicker = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("text/x-opml")) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        val accountAtPick = state.accountKey
        readingOpml = true
        scope.launch {
            try {
                val xml = model.exportOpml() ?: return@launch
                if (model.state.value.accountKey != accountAtPick) return@launch
                val written = withContext(Dispatchers.IO) {
                    runCatching {
                        // "wt" truncates: a reused document must not keep the
                        // tail of whatever it held before.
                        context.contentResolver.openOutputStream(uri, "wt")?.use { it.write(xml.toByteArray()) } != null
                    }.getOrDefault(false)
                }
                if (!written) model.opmlFileFailed("Could not write that file.")
            } finally {
                readingOpml = false
            }
        }
    }
    val subscriptions = state.snapshot.objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = ReaderSpace.xl)) {
        item(key = "feeds-intro") {
            Column(Modifier.padding(horizontal = ReaderSpace.gutter)) {
                ScreenIntro("Your sources", "Follow your interests, at your pace.")
                Button(onClick = onSubscribe, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) {
                    Icon(Icons.Filled.Add, contentDescription = null, modifier = Modifier.size(20.dp))
                    Spacer(Modifier.width(ReaderSpace.xs))
                    Text("Subscribe by URL")
                }
            }
        }
        if (unreadMode) {
            item(key = "unread-synced") {
                val text = when {
                    unreadCounts == null -> "Unread counts unavailable"
                    unreadSyncedAt != null -> "Counts as last synced ${unreadSyncedAt.relativeTime()}"
                    else -> "Counts as last synced (time unavailable)"
                }
                Text(
                    text,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
            }
        }
        item {
            FlowRow(Modifier.padding(horizontal = ReaderSpace.md, vertical = ReaderSpace.xs), horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
                TextButton(
                    onClick = { importPicker.launch(arrayOf("text/x-opml", "application/xml", "text/xml", "*/*")) },
                    enabled = !opml.busy && !readingOpml,
                ) { Text("Import OPML") }
                TextButton(
                    onClick = { exportPicker.launch("earnest.opml") },
                    enabled = !opml.busy && !readingOpml,
                ) { Text("Export OPML") }
            }
        }
        if (opml.busy || readingOpml) {
            item {
                LoadingState("Working on OPML…")
            }
        }
        opml.error?.let { message ->
            item {
                NoticeBanner(message, tone = NoticeTone.ERROR, modifier = Modifier.padding(ReaderSpace.md))
            }
        }
        if (subscriptions.isEmpty()) {
            item(key = "no-feeds") {
                QuietState("A home for your interests", "Subscribe to a favorite site, or import an OPML file from another reader. Your articles will appear here after syncing.", icon = FeedIcon)
            }
        }
        val folders = state.snapshot.objects.filter { it.kind == SyncEngine.OBJECT_FOLDER && !it.deleted }
        feedGroups(subscriptions, folders).forEach { group ->
            group.title?.let { title ->
                item(key = group.key) {
                    TextButton(
                        onClick = { onFolder(group.key.removePrefix("folder-").toLongOrNull() ?: 0L) },
                        modifier = Modifier.padding(horizontal = 12.dp, vertical = 4.dp),
                    ) {
                        Icon(FolderIcon, contentDescription = null, modifier = Modifier.size(20.dp))
                        Spacer(Modifier.width(8.dp))
                        Text(title, style = MaterialTheme.typography.titleSmall)
                    }
                }
            }
            items(group.subscriptions, key = { it.id }) { subscription ->
                FeedRow(
                    subscription = subscription,
                    unreadMode = unreadMode,
                    unreadCount = unreadCounts?.bySubscription?.get(subscription.id),
                    onCadence = onCadence,
                    onOpen = { onFeed(subscription.id) },
                    onUnsubscribe = onUnsubscribe,
                )
            }
        }
    }

    state.opml.summary?.let { summary ->
        OpmlReportDialog(summary = summary, onDismiss = model::dismissOpmlReport)
    }
}

@Composable
private fun OpmlReportDialog(summary: OpmlSummary, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("OPML import") },
        text = {
            Column(
                modifier = Modifier.verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(opmlSummaryLine(summary), style = MaterialTheme.typography.bodyMedium)
                if (summary.deferred > 0) {
                    Text(
                        "${summary.deferred} ${if (summary.deferred == 1) "feed is" else "feeds are"} fetched in the background soon.",
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
                summary.errors.forEach { error ->
                    Text(error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Close") } },
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun FeedRow(
    subscription: SyncObject,
    unreadMode: Boolean,
    unreadCount: Long?,
    onCadence: (Long, String) -> Unit,
    onOpen: () -> Unit,
    onUnsubscribe: (Long) -> Unit,
) {
    // The icon path and fetch health live in the subscription's raw JSON.
    // Parse them once per revision, not on every redraw of the list.
    val iconUrl = remember(subscription.raw) { subscription.iconUrl() }
    val health = remember(subscription.raw) { subscription.health() }
    var menuOpen by remember { mutableStateOf(false) }
    var confirmUnsubscribe by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth().padding(horizontal = ReaderSpace.gutter, vertical = ReaderSpace.md)) {
        Row(Modifier.fillMaxWidth().heightIn(min = 48.dp).clickable(onClickLabel = "Read feed", onClick = onOpen), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
            Surface(modifier = Modifier.testTag("feed-icon-${subscription.id}"), shape = RoundedCornerShape(14.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh) {
                if (iconUrl != null) {
                    MediaImage(iconUrl, Modifier.padding(10.dp).size(24.dp), RoundedCornerShape(4.dp))
                } else {
                    Icon(FeedIcon, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(10.dp).size(24.dp))
                }
            }
            Column(Modifier.weight(1f).padding(vertical = ReaderSpace.xs)) {
                Text(subscription.name.ifBlank { "Untitled feed" }, style = MaterialTheme.typography.titleMedium)
                Text(subscription.raw.url(), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Box {
                IconButton(onClick = { menuOpen = true }) { Icon(Icons.Filled.MoreVert, contentDescription = "Feed options for ${subscription.name}") }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    DropdownMenuItem(text = { Text("Read") }, onClick = { menuOpen = false; onOpen() })
                    DropdownMenuItem(text = { Text("Unsubscribe", color = MaterialTheme.colorScheme.error) }, onClick = { menuOpen = false; confirmUnsubscribe = true })
                }
            }
        }
        feedProblem(health)?.let { problem ->
            Text(
                "Fetch failing: ${problem.detail}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
            )
            problem.triedAt?.let {
                Text(
                    "Tried ${it.relativeTime()}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        if (unreadMode) {
            Text(
                unreadCount?.let { "Unread $it" } ?: "Unread unavailable",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        FlowRow(
            horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
        ) {
            val selection = cadenceSelection(River.cadence(subscription))
            Cadence.entries.forEach { cadence ->
                CadenceChip(
                    cadence = cadence,
                    selected = selection[cadence] == true,
                    onSelect = { onCadence(subscription.id, cadence.value) },
                )
            }
        }
        Text(
            cadenceOf(River.cadence(subscription)).hint,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
    HorizontalDivider(Modifier.padding(horizontal = ReaderSpace.gutter), color = MaterialTheme.colorScheme.outlineVariant)
    if (confirmUnsubscribe) {
        AlertDialog(
            onDismissRequest = { confirmUnsubscribe = false },
            title = { Text("Unsubscribe from ${subscription.name}?") },
            text = { Text("You will stop receiving new articles from this feed.") },
            confirmButton = { TextButton(onClick = { confirmUnsubscribe = false; onUnsubscribe(subscription.id) }) { Text("Unsubscribe", color = MaterialTheme.colorScheme.error) } },
            dismissButton = { TextButton(onClick = { confirmUnsubscribe = false }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun CadenceChip(cadence: Cadence, selected: Boolean, onSelect: () -> Unit) {
    FilterChip(
        selected = selected,
        onClick = onSelect,
        label = { Text(cadence.label) },
        modifier = Modifier.semantics {
            this.selected = selected
            stateDescription = if (selected) "Selected" else "Not selected"
        },
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun SubscribeDialog(
    onDismiss: () -> Unit,
    onSubscribe: (String, String) -> Unit,
) {
    var url by remember { mutableStateOf("") }
    var selectedCadence by remember { mutableStateOf(Cadence.TODAY.value) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Subscribe by URL") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
                Text("Paste the address of a site or its RSS feed.", style = MaterialTheme.typography.bodyMedium)
                OutlinedTextField(url, { url = it }, label = { Text("Feed URL") }, singleLine = true, modifier = Modifier.fillMaxWidth(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done))
                Text("Delivery pace", style = MaterialTheme.typography.titleSmall)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Cadence.entries.forEach { cadence ->
                        CadenceChip(
                            cadence = cadence,
                            selected = cadence.value == selectedCadence,
                            onSelect = { selectedCadence = cadence.value },
                        )
                    }
                }
                Text(
                    cadenceOf(selectedCadence).hint,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
        confirmButton = {
            Button(onClick = { onSubscribe(url.trim(), selectedCadence) }, enabled = url.isNotBlank()) { Text("Subscribe") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

private fun String.url(): String = runCatching {
    kotlinx.serialization.json.Json.parseToJsonElement(this)
        .jsonObject["url"]?.jsonPrimitive?.content ?: ""
}.getOrDefault("")

package ch.lkmc.earnest

import androidx.compose.ui.platform.LocalDensity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
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
import kotlinx.serialization.json.Json
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
    val objects = state.snapshot.objects
    val subscriptions = remember(objects) { objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted } }
    val groups = remember(objects) {
        if (subscriptions.isEmpty()) {
            emptyList()
        } else {
            feedGroups(subscriptions, objects.filter { it.kind == SyncEngine.OBJECT_FOLDER && !it.deleted })
        }
    }
    val fileBusy = opml.busy || readingOpml
    val listState = rememberLazyListState()
    val atTop by remember { derivedStateOf { listState.firstVisibleItemIndex == 0 } }

    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
        LazyColumn(
            Modifier.widthIn(max = ReaderSpace.contentWidth).fillMaxSize(),
            state = listState,
            contentPadding = PaddingValues(
                start = ReaderSpace.md,
                end = ReaderSpace.md,
                bottom = if (subscriptions.isEmpty()) ReaderSpace.xl else SubscribeClearance,
            ),
        ) {
            item(key = "feeds-intro") {
                Column(Modifier.fillMaxWidth().padding(horizontal = ReaderSpace.xxs)) {
                    SectionHeader("Your sources")
                    if (unreadMode) UnreadSyncNote(unreadCounts, unreadSyncedAt)
                    FlowRow(
                        Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
                    ) {
                        QuietButton(
                            "Import OPML",
                            onClick = { importPicker.launch(arrayOf("text/x-opml", "application/xml", "text/xml", "*/*")) },
                            enabled = !fileBusy,
                            icon = EarnestIcons.Import,
                        )
                        QuietButton(
                            "Export OPML",
                            onClick = { exportPicker.launch("earnest.opml") },
                            enabled = !fileBusy,
                            icon = EarnestIcons.Export,
                        )
                    }
                }
            }
            if (fileBusy) item(key = "opml-busy") { LoadingState("Working on OPML…") }
            opml.error?.let { message ->
                item(key = "opml-error") {
                    NoticeBanner(message, Modifier.padding(vertical = ReaderSpace.xs), tone = NoticeTone.ERROR)
                }
            }
            if (subscriptions.isEmpty()) {
                item(key = "no-feeds") {
                    QuietState(
                        "A home for your interests",
                        "Subscribe to a favorite site, or import an OPML file from another reader. Your articles will appear here after syncing.",
                        icon = EarnestIcons.FeedsSelected,
                        tone = LocalTones.current.green,
                        actionLabel = "Subscribe by URL",
                        onAction = onSubscribe,
                    )
                }
            }
            groups.forEach { group ->
                item(key = "heading-${group.key}") {
                    val title = group.title
                    if (title == null) {
                        Spacer(Modifier.height(ReaderSpace.sm))
                    } else {
                        FolderHeading(
                            title,
                            group.subscriptions.size,
                            onOpen = { onFolder(group.key.removePrefix("folder-").toLongOrNull() ?: 0L) },
                        )
                    }
                }
                itemsIndexed(group.subscriptions, key = { _, feed -> feed.id }) { index, subscription ->
                    if (index > 0) Spacer(Modifier.height(GroupGap))
                    FeedRow(
                        subscription = subscription,
                        unreadMode = unreadMode,
                        unreadCount = unreadCounts?.bySubscription?.get(subscription.id),
                        onCadence = onCadence,
                        onOpen = { onFeed(subscription.id) },
                        onUnsubscribe = onUnsubscribe,
                        shape = groupedShape(index, group.subscriptions.size),
                    )
                }
            }
        }
        // The screen's one action, in thumb reach. The empty state carries it instead.
        if (subscriptions.isNotEmpty()) {
            ExtendedFloatingActionButton(
                text = { Text("Subscribe by URL") },
                // Material clears the label's semantics, expanded or not: the icon names the button.
                icon = { Icon(EarnestIcons.Add, contentDescription = "Subscribe by URL") },
                onClick = onSubscribe,
                // At large text the spelled-out button would cover the rows beneath it.
                expanded = atTop && LocalDensity.current.fontScale <= SUBSCRIBE_LABEL_MAX_FONT_SCALE,
                containerColor = MaterialTheme.colorScheme.primary,
                contentColor = MaterialTheme.colorScheme.onPrimary,
                modifier = Modifier.align(Alignment.BottomEnd).padding(ReaderSpace.md),
            )
        }
    }

    state.opml.summary?.let { summary ->
        OpmlReportDialog(summary = summary, onDismiss = model::dismissOpmlReport)
    }
}

private const val SUBSCRIBE_LABEL_MAX_FONT_SCALE = 1.3f
private val GroupGap = 2.dp
private val SubscribeClearance = 88.dp
private val FolderHeadingHeight = 56.dp
private val FeedMark = 40.dp
private val InlineIcon = 18.dp

@Composable
private fun UnreadSyncNote(counts: UnreadCounts?, syncedAt: Long?) {
    val text = when {
        counts == null -> "Unread counts unavailable"
        syncedAt != null -> "Counts as last synced ${syncedAt.relativeTime()}"
        else -> "Counts as last synced (time unavailable)"
    }
    Text(
        text,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(bottom = ReaderSpace.xs),
    )
}

@Composable
private fun FolderHeading(title: String, feeds: Int, onOpen: () -> Unit) {
    val scheme = MaterialTheme.colorScheme
    Row(
        Modifier.fillMaxWidth().padding(top = ReaderSpace.sm)
            .clip(MaterialTheme.shapes.medium)
            .clickable(onClickLabel = "Read folder", role = Role.Button, onClick = onOpen)
            .heightIn(min = FolderHeadingHeight)
            .padding(horizontal = ReaderSpace.sm),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
    ) {
        Icon(EarnestIcons.FolderSelected, contentDescription = null, tint = toneColors(scheme, LocalTones.current.green).ink)
        Text(title, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f).semantics { heading() })
        Text(feedCount(feeds), style = MaterialTheme.typography.bodySmall, color = scheme.onSurfaceVariant)
        Icon(EarnestIcons.ChevronForward, contentDescription = null, tint = scheme.onSurfaceVariant)
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
                verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
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
        confirmButton = { QuietButton("Close", onDismiss) },
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
    shape: Shape = MaterialTheme.shapes.large,
) {
    // The icon, address, pace and fetch health live in the subscription's raw
    // JSON. Parse them once per revision, not on every redraw of the list.
    val iconUrl = remember(subscription.raw) { subscription.iconUrl() }
    val address = remember(subscription.raw) { shortAddress(subscription.raw.url()) }
    val cadence = remember(subscription.raw) { cadenceOf(River.cadence(subscription)) }
    val problem = remember(subscription.raw) { feedProblem(subscription.health()) }
    val name = subscription.name.ifBlank { "Untitled feed" }
    var menuOpen by remember { mutableStateOf(false) }
    var confirmUnsubscribe by remember { mutableStateOf(false) }
    val scheme = MaterialTheme.colorScheme
    Surface(
        Modifier.fillMaxWidth(),
        shape = shape,
        color = scheme.groupContainer,
        border = if (scheme.isMonochrome) BorderStroke(1.dp, scheme.outlineVariant) else null,
    ) {
        Column(
            Modifier.clickable(onClickLabel = "Read feed", onClick = onOpen)
                .padding(start = ReaderSpace.md, top = ReaderSpace.xs, bottom = ReaderSpace.xs),
        ) {
            Row(
                Modifier.fillMaxWidth().heightIn(min = TouchTarget),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
            ) {
                FeedAvatar(name, iconUrl, Modifier.testTag("feed-icon-${subscription.id}"), FeedMark)
                Column(Modifier.weight(1f)) {
                    Text(name, style = MaterialTheme.typography.titleMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    Text(
                        address,
                        style = MaterialTheme.typography.bodySmall,
                        color = scheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Box {
                    ActionIconButton(EarnestIcons.More, "Feed options for ${subscription.name}", onClick = { menuOpen = true })
                    AppMenu(expanded = menuOpen, onDismiss = { menuOpen = false }) {
                        AppMenuItem("Read", onClick = { menuOpen = false; onOpen() }, icon = EarnestIcons.Book)
                        AppMenuItem(
                            "Unsubscribe",
                            onClick = { menuOpen = false; confirmUnsubscribe = true },
                            icon = EarnestIcons.Delete,
                            role = MenuRole.DESTRUCTIVE,
                        )
                    }
                }
            }
            Column(Modifier.fillMaxWidth().padding(start = FeedMark + ReaderSpace.sm, end = ReaderSpace.md)) {
                FlowRow(
                    horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
                    itemVerticalAlignment = Alignment.CenterVertically,
                ) {
                    CadenceMenu(name, cadence, onSelect = { onCadence(subscription.id, it.value) })
                    if (unreadMode) {
                        Text(
                            unreadCount?.let { "Unread $it" } ?: "Unread unavailable",
                            style = MaterialTheme.typography.labelMedium,
                            color = scheme.onSurfaceVariant,
                        )
                    }
                }
                problem?.let { FeedTrouble(it) }
            }
        }
    }
    if (confirmUnsubscribe) {
        AlertDialog(
            onDismissRequest = { confirmUnsubscribe = false },
            title = { Text("Unsubscribe from $name?") },
            text = { Text("You will stop receiving new articles from this feed.") },
            confirmButton = {
                QuietButton(
                    "Unsubscribe",
                    onClick = { confirmUnsubscribe = false; onUnsubscribe(subscription.id) },
                    color = scheme.error,
                )
            },
            dismissButton = { QuietButton("Cancel", onClick = { confirmUnsubscribe = false }) },
        )
    }
}

/** The pace as one quiet pill; its menu says what each pace means. */
@Composable
private fun CadenceMenu(feed: String, current: Cadence, onSelect: (Cadence) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        AssistChip(
            onClick = { open = true },
            label = { Text(current.label) },
            trailingIcon = { Icon(EarnestIcons.ChevronDown, contentDescription = null, modifier = Modifier.size(InlineIcon)) },
            modifier = Modifier.semantics {
                contentDescription = "Delivery pace for $feed"
                stateDescription = current.label
            },
        )
        AppMenu(expanded = open, onDismiss = { open = false }) {
            Cadence.entries.forEach { cadence ->
                AppMenuItem(
                    cadence.label,
                    onClick = {
                        open = false
                        if (cadence != current) onSelect(cadence)
                    },
                    supporting = cadence.hint,
                    selected = cadence == current,
                )
            }
        }
    }
}

@Composable
private fun FeedTrouble(problem: FeedProblem) {
    val scheme = MaterialTheme.colorScheme
    Row(
        Modifier.padding(bottom = ReaderSpace.xs),
        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
    ) {
        Icon(EarnestIcons.Alert, contentDescription = null, tint = scheme.error, modifier = Modifier.size(InlineIcon))
        Column {
            Text("Fetch failing: ${problem.detail}", style = MaterialTheme.typography.bodySmall, color = scheme.error)
            problem.triedAt?.let {
                Text("Tried ${it.relativeTime()}", style = MaterialTheme.typography.bodySmall, color = scheme.onSurfaceVariant)
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SubscribeSheet(onDismiss: () -> Unit, onSubscribe: (String, String) -> Unit) {
    var url by rememberSaveable { mutableStateOf("") }
    var pace by rememberSaveable { mutableStateOf(Cadence.TODAY.value) }
    val field = remember { FocusRequester() }
    val submit = { if (url.isNotBlank()) onSubscribe(url.trim(), pace) }
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
    ) {
        Column(Modifier.fillMaxWidth().imePadding()) {
            Column(
                Modifier.fillMaxWidth().weight(1f, fill = false)
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = ReaderSpace.lg),
                verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
            ) {
                Text("Subscribe by URL", style = MaterialTheme.typography.headlineSmall, modifier = Modifier.semantics { heading() })
                Text(
                    "Paste the address of a site or its RSS feed.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedTextField(
                    value = url,
                    onValueChange = { url = it },
                    label = { Text("Feed URL") },
                    leadingIcon = { Icon(EarnestIcons.Feeds, contentDescription = null) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth().focusRequester(field),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done),
                    keyboardActions = KeyboardActions(onDone = { submit() }),
                )
                SectionHeader("Delivery pace")
                Column(Modifier.fillMaxWidth().selectableGroup()) {
                    Cadence.entries.forEach { cadence ->
                        OptionRow(
                            label = cadence.label,
                            selected = cadence.value == pace,
                            onSelect = { pace = cadence.value },
                            description = cadence.hint,
                        )
                    }
                }
            }
            // Outside the scroll, so the keyboard never covers it.
            PrimaryButton(
                "Subscribe",
                onClick = submit,
                modifier = Modifier.fillMaxWidth().padding(horizontal = ReaderSpace.lg, vertical = ReaderSpace.sm),
                enabled = url.isNotBlank(),
            )
        }
    }
    LaunchedEffect(Unit) { field.requestFocus() }
}

/** An address as people read it: no scheme, no www, no trailing slash. */
private fun shortAddress(url: String): String =
    url.substringAfter("://").removePrefix("www.").trimEnd('/')

private fun String.url(): String = runCatching {
    Json.parseToJsonElement(this).jsonObject["url"]?.jsonPrimitive?.content ?: ""
}.getOrDefault("")

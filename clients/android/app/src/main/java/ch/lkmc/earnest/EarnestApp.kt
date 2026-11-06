package ch.lkmc.earnest

import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.adaptive.layout.AnimatedPane
import androidx.compose.material3.adaptive.layout.ListDetailPaneScaffoldRole
import androidx.compose.material3.adaptive.navigation.NavigableListDetailPaneScaffold
import androidx.compose.material3.adaptive.navigation.rememberListDetailPaneScaffoldNavigator
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteScaffold
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.core.net.toUri
import androidx.lifecycle.viewmodel.compose.viewModel
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.River
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncSnapshot
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlin.math.roundToInt

private enum class View(val label: String, val icon: ImageVector) {
    TODAY("Today", Icons.Filled.DateRange),
    EDITION("Edition", Icons.AutoMirrored.Filled.List),
    LATER("Later", Icons.Filled.Notifications),
    KEPT("Kept", Icons.Filled.Star),
    DONE("Done", Icons.Filled.CheckCircle),
    FEEDS("Feeds", Icons.Filled.Home),
    ACTIVITY("Activity", Icons.Filled.Info),
}

@Composable
internal fun EarnestApp(initialEntry: Long?, notificationId: Long?) {
    val model: AppViewModel = viewModel()
    val loggedIn by model.loggedIn.collectAsState()
    val sessionNotice by model.sessionNotice.collectAsState()
    val context = LocalContext.current
    val activity = context as? MainActivity
    var theme by remember { mutableStateOf(context.themeSetting()) }
    var notificationsDenied by remember { mutableStateOf(activity != null && Notifications.permissionNeeded(activity)) }
    // The post-sign-in prompt and system settings change the permission
    // outside this screen, so re-check whenever the activity resumes.
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner, activity) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                notificationsDenied = activity != null && Notifications.permissionNeeded(activity)
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }
    EarnestTheme(theme) {
        if (!loggedIn) {
            LoginScreen(onLoggedIn = model::onLoggedIn, notice = sessionNotice)
            return@EarnestTheme
        }
        val state by model.state.collectAsState()
        MainScaffold(
            model = model,
            state = state,
            theme = theme,
            onThemeChange = { setting ->
                theme = setting
                context.setThemeSetting(setting)
            },
            initialEntry = initialEntry,
            notificationId = notificationId,
            notificationsDenied = notificationsDenied,
            onRequestNotifications = {
                activity?.requestNotificationPermission { granted -> notificationsDenied = !granted }
            },
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class, androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi::class)
@Composable
private fun MainScaffold(
    model: AppViewModel,
    state: UiState,
    theme: ThemeSetting,
    onThemeChange: (ThemeSetting) -> Unit,
    initialEntry: Long?,
    notificationId: Long?,
    notificationsDenied: Boolean,
    onRequestNotifications: () -> Unit,
) {
    val navigator = rememberListDetailPaneScaffoldNavigator<Long>()
    var view by remember { mutableStateOf(View.TODAY) }
    val readerEntry = state.snapshot.entries.firstOrNull { it.id == navigator.currentDestination?.contentKey && !it.deleted }
    val reader = remember { ReaderLoadState() }
    var activityGroups by remember { mutableStateOf<List<ch.lkmc.earnest.core.ActivityGroup>>(emptyList()) }
    var showSubscribe by remember { mutableStateOf(false) }
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val selected = navigator.currentDestination?.contentKey

    LaunchedEffect(initialEntry) {
        initialEntry?.let { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, it) }
    }
    LaunchedEffect(notificationId, state.accountKey) {
        if (state.accountKey == null) return@LaunchedEffect
        val id = notificationId ?: return@LaunchedEffect
        model.notificationEntry(id)?.let { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, it) }
    }
    LaunchedEffect(selected, state.accountKey) {
        reader.load(selected, state.accountKey) { id ->
            if (model.loadEntry(id) == null) throw java.io.IOException("This entry is no longer available.")
            model.markSeen(id)
            model.openEntry(id)
        }
    }
    LaunchedEffect(view, state.accountKey) {
        if (view == View.ACTIVITY) {
            val since = Instant.now().minusSeconds(7 * 24 * 3600).toString()
            model.activity(since).collect { page ->
                activityGroups = page?.groups.orEmpty()
            }
        }
    }

    val entries = state.snapshot.entries
    val subscriptionTitles = remember(state.snapshot) {
        state.snapshot.objects
            .filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION }
            .associate { it.id to it.name }
    }
    val lists = remember(state.snapshot, state.states, state.cadences, state.riverEntryIds) {
        val visible = entries.filterNot { it.deleted }
        fun stateOf(id: Long) = state.states[id] ?: SyncEngine.initialState(state.principalId, id)
        val today = todayEntries(state)
        val edition = editionEntries(state)
        val later = visible.filter { stateOf(it.id).shelf.value == kotlinx.serialization.json.JsonPrimitive(SyncEngine.VALUE_LATER) }
        val kept = visible.filter { stateOf(it.id).kept.value == kotlinx.serialization.json.JsonPrimitive(true) }
        val done = visible.filter { stateOf(it.id).shelf.value == kotlinx.serialization.json.JsonPrimitive(SyncEngine.VALUE_DONE) }
        mapOf(
            View.TODAY to today.sortedBy { it.seq },
            View.EDITION to edition.sortedBy { it.seq },
            View.LATER to later.sortedBy { it.seq },
            View.KEPT to kept.sortedBy { it.seq },
            View.DONE to done.sortedBy { it.seq },
        )
    }

    fun undo(entryId: Long, target: SwipeTarget) {
        scope.launch {
            val result = snackbar.showSnackbar(
                message = target.message,
                actionLabel = "Undo",
                duration = SnackbarDuration.Short,
            )
            if (result == SnackbarResult.ActionPerformed) model.undoShelf(entryId)
        }
    }

    NavigationSuiteScaffold(
        navigationSuiteItems = {
            View.entries.forEach { destination ->
                item(
                    selected = view == destination,
                    onClick = {
                        view = destination
                        scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.List) }
                    },
                    icon = { Icon(destination.icon, contentDescription = null) },
                    label = { Text(destination.label) },
                )
            }
        },
    ) {
        Scaffold(
            topBar = {
                EarnestTopBar(
                    view.label,
                    state.loading,
                    model,
                    theme,
                    onThemeChange,
                    notificationsDenied,
                    onRequestNotifications,
                )
            },
            snackbarHost = { SnackbarHost(snackbar) },
        ) { padding ->
            Column(Modifier.fillMaxSize().padding(padding)) {
                state.cacheNotice?.let { Text(it, modifier = Modifier.padding(8.dp)) }
                state.error?.let {
                    Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(8.dp))
                }
                NavigableListDetailPaneScaffold(
                    navigator = navigator,
                    listPane = {
                        AnimatedPane {
                            when (view) {
                                View.FEEDS -> FeedsScreen(
                                    state = state,
                                    onSubscribe = { showSubscribe = true },
                                    onUnsubscribe = { model.unsubscribe(it) },
                                    onCadence = { id, cadence -> model.setCadence(id, cadence) },
                                )
                                View.ACTIVITY -> ActivityScreen(
                                    groups = activityGroups,
                                    entryTitles = state.snapshot.entries.associate { it.id to it.title },
                                    onRevert = { model.revert(it) },
                                )
                                else -> EntryListScreen(
                                    title = view.label,
                                    entries = lists[view].orEmpty(),
                                    states = state.states,
                                    principalId = state.principalId,
                                    subscriptionTitles = subscriptionTitles,
                                    briefs = if (view == View.TODAY) state.briefs else emptyList(),
                                    principalNames = state.principals,
                                    entryTitles = state.snapshot.entries.associate { it.id to it.title },
                                    agentChanges = if (view == View.TODAY) state.agentChanges else emptyList(),
                                    sessionChanges = if (view == View.TODAY) state.sessionAgentChanges else emptyList(),
                                    agentChangesError = if (view == View.TODAY) state.agentChangesError else null,
                                    onRevert = model::revert,
                                    onOpen = { entryId ->
                                        scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, entryId) }
                                    },
                                    onLater = { id -> model.later(id) { undo(id, SwipeTarget.LATER) } },
                                    onDone = { id -> model.done(id) { undo(id, SwipeTarget.DONE) } },
                                    onSeen = model::markSeen,
                                    onKeep = { model.keep(it) },
                                    onUnkeep = { model.unkeep(it) },
                                )
                            }
                        }
                    },
                    detailPane = {
                        AnimatedPane {
                            val entry = readerEntry
                            if (entry == null) {
                                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                                    when {
                                        selected == null -> Text("Select an entry")
                                        reader.loading -> CircularProgressIndicator()
                                        else -> Text(reader.error ?: "Couldn't load this entry. Check your connection and try again.")
                                    }
                                }
                            } else {
                                Column(Modifier.fillMaxSize()) {
                                    val original = originalUrl(entry)
                                    ReaderHeader(
                                        entry = entry,
                                        source = subscriptionTitles[entry.subscriptionId].orEmpty(),
                                        onOpenOriginal = {
                                            original?.let { url ->
                                                runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, url.toUri())) }
                                            }
                                        },
                                    )
                                    val content = reader.content
                                    when {
                                        reader.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                                            CircularProgressIndicator()
                                        }
                                        content == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                                            Text(reader.error ?: "Couldn't load this entry. Check your connection and try again.")
                                        }
                                        else -> Reader(
                                            url = content.url,
                                            cachedHtml = content.cachedHtml,
                                            modifier = Modifier.fillMaxSize(),
                                        )
                                    }
                                }
                            }
                        }
                    },
                )
            }
        }
    }

    if (showSubscribe) {
        SubscribeDialog(
            onDismiss = { showSubscribe = false },
            onSubscribe = { url, cadence ->
                showSubscribe = false
                model.subscribe(url, cadence)
            },
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun EarnestTopBar(
    title: String,
    loading: Boolean,
    model: AppViewModel,
    theme: ThemeSetting,
    onThemeChange: (ThemeSetting) -> Unit,
    notificationsDenied: Boolean,
    onRequestNotifications: () -> Unit,
) {
    var themeOpen by remember { mutableStateOf(false) }
    TopAppBar(
        title = { Text(title) },
        actions = {
            if (notificationsDenied) {
                TextButton(onClick = onRequestNotifications) { Text("Notifications") }
            }
            if (loading) {
                CircularProgressIndicator(
                    Modifier.size(20.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.onSurface,
                )
            } else {
                IconButton(onClick = { model.refresh() }) {
                    Icon(Icons.Filled.Refresh, contentDescription = "Refresh")
                }
            }
            TextButton(onClick = { themeOpen = true }) { Text("Theme") }
        },
    )
    if (themeOpen) {
        ThemeDialog(
            current = theme,
            onDismiss = { themeOpen = false },
            onSelect = { setting ->
                themeOpen = false
                onThemeChange(setting)
            },
        )
    }
}

@Composable
private fun ThemeDialog(
    current: ThemeSetting,
    onDismiss: () -> Unit,
    onSelect: (ThemeSetting) -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Theme") },
        text = {
            Column {
                ThemeSetting.entries.forEach { setting ->
                    Row(
                        Modifier.fillMaxWidth().clickable { onSelect(setting) },
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        RadioButton(selected = setting == current, onClick = { onSelect(setting) })
                        Text(setting.label)
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Close") } },
    )
}

@Composable
private fun ReaderHeader(entry: CachedEntry, source: String, onOpenOriginal: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 16.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                entry.title,
                style = MaterialTheme.typography.titleMedium,
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
            )
            val date = entryDateLabel(entry)
            val meta = listOfNotNull(source.takeIf { it.isNotBlank() }, date.takeIf { it.isNotBlank() })
            if (meta.isNotEmpty()) {
                Text(
                    meta.joinToString(" · "),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        if (originalUrl(entry) != null) {
            TextButton(onClick = onOpenOriginal) { Text("Open original") }
        }
    }
}

@Composable
private fun EntryListScreen(
    title: String,
    entries: List<CachedEntry>,
    states: Map<Long, ch.lkmc.earnest.core.State>,
    principalId: Long,
    subscriptionTitles: Map<Long, String>,
    briefs: List<ch.lkmc.earnest.db.BriefEntity>,
    principalNames: Map<Long, String>,
    entryTitles: Map<Long, String>,
    agentChanges: List<ch.lkmc.earnest.core.Activity>,
    sessionChanges: List<ch.lkmc.earnest.core.Activity>,
    agentChangesError: String?,
    onRevert: (Long) -> Unit,
    onOpen: (Long) -> Unit,
    onLater: (Long) -> Unit,
    onDone: (Long) -> Unit,
    onSeen: (Long) -> Unit,
    onKeep: (Long) -> Unit,
    onUnkeep: (Long) -> Unit,
) {
    val listState = rememberLazyListState()
    MarkSeenOnScroll(entries, principalId, listState, onSeen)
    val headerCount = (if (briefs.isNotEmpty()) 1 else 0) +
        (if (agentChanges.isNotEmpty() || agentChangesError != null) 1 else 0)
    LaunchedEffect(headerCount) {
        if (shouldRevealHeaders(listState.firstVisibleItemIndex, listState.firstVisibleItemScrollOffset, headerCount)) {
            listState.scrollToItem(0)
        }
    }
    LazyColumn(state = listState, modifier = Modifier.fillMaxSize()) {
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
                AgentChangesNotice("While you were out", agentChanges, entryTitles, agentChangesError, onOpen, onRevert)
            }
        }
        items(entries, key = { it.id }) { entry ->
            val effective = states[entry.id] ?: SyncEngine.initialState(principalId, entry.id)
            SwipeActionRow(onLater = { onLater(entry.id) }, onDone = { onDone(entry.id) }) {
                EntryRow(
                    entry = entry,
                    state = effective,
                    source = subscriptionTitles[entry.subscriptionId].orEmpty(),
                    onClick = { onOpen(entry.id) },
                )
            }
            if (effective.kept.value == kotlinx.serialization.json.JsonPrimitive(true)) {
                Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
                    TextButton(onClick = { onUnkeep(entry.id) }) { Text("Unkeep") }
                }
            }
        }
        item(key = "end") {
            val statement = if (entries.isEmpty()) {
                "Nothing new. A rare and beautiful thing."
            } else {
                "That is everything since ${entries.first().firstSeenAt.dayName()}."
            }
            Text(
                statement,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.fillMaxWidth().padding(24.dp),
            )
            if (sessionChanges.isNotEmpty() || agentChangesError != null) {
                AgentChangesNotice("Agent changes this session", sessionChanges, entryTitles, agentChangesError, onOpen, onRevert)
            }
        }
    }
}

@Composable
private fun MarkSeenOnScroll(
    entries: List<CachedEntry>,
    principalId: Long,
    listState: androidx.compose.foundation.lazy.LazyListState,
    onSeen: (Long) -> Unit,
) {
    // Resetting with the principal keeps offline launches from losing marks:
    // rows are marked again once the principal is known.
    val tracker = remember(principalId) { PassiveSeenTracker() }
    LaunchedEffect(listState, entries, principalId) {
        if (principalId == 0L) return@LaunchedEffect
        snapshotFlow {
            SeenViewport(
                firstVisibleIndex = listState.firstVisibleItemIndex,
                visible = listState.layoutInfo.visibleItemsInfo.mapNotNull { item ->
                    (item.key as? Long)?.let { key -> key to item.index }
                }.toMap(),
            )
        }
            .distinctUntilChanged()
            .collect { viewport -> tracker.observe(viewport).forEach(onSeen) }
    }
}

@Composable
private fun EntryRow(
    entry: CachedEntry,
    state: ch.lkmc.earnest.core.State,
    source: String,
    onClick: () -> Unit,
) {
    val unread = state.seen.value == kotlinx.serialization.json.JsonPrimitive(SyncEngine.VALUE_UNSEEN) &&
        state.shelf.value != kotlinx.serialization.json.JsonPrimitive(SyncEngine.VALUE_DONE)
    Column(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(16.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (unread) {
                Box(
                    Modifier
                        .width(8.dp)
                        .height(8.dp)
                        .background(MaterialTheme.colorScheme.primary, MaterialTheme.shapes.small),
                )
                Spacer(Modifier.width(8.dp))
            }
            Text(entry.title, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (source.isNotEmpty()) {
            Text(
                "$source · ${entry.firstSeenAt.relativeTime()}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** A short swipe reveals Later, a long swipe reveals Done; release to apply. */
@Composable
private fun SwipeActionRow(
    onLater: () -> Unit,
    onDone: () -> Unit,
    content: @Composable () -> Unit,
) {
    val density = LocalDensity.current
    val laterThreshold = with(density) { 96.dp.toPx() }
    val doneThreshold = with(density) { 240.dp.toPx() }
    var offset by remember { androidx.compose.runtime.mutableFloatStateOf(0f) }
    val scope = rememberCoroutineScope()
    Box(Modifier.fillMaxWidth()) {
        Box(
            Modifier
                .matchParentSize()
                .background(MaterialTheme.colorScheme.surfaceVariant),
            contentAlignment = Alignment.CenterEnd,
        ) {
            val target = swipeTargetFor(-offset, laterThreshold, doneThreshold)
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
                .pointerInput(Unit) {
                    detectHorizontalDragGestures(
                        onHorizontalDrag = { change, dragAmount ->
                            change.consume()
                            offset = (offset + dragAmount).coerceIn(-doneThreshold * 1.2f, 0f)
                        },
                        onDragEnd = {
                            val target = swipeTargetFor(-offset, laterThreshold, doneThreshold)
                            scope.launch {
                                offset = 0f
                                when (target) {
                                    SwipeTarget.DONE -> onDone()
                                    SwipeTarget.LATER -> onLater()
                                    null -> Unit
                                }
                            }
                        },
                    )
                },
        ) {
            content()
        }
    }
}

@Composable
private fun FeedsScreen(
    state: UiState,
    onSubscribe: () -> Unit,
    onUnsubscribe: (Long) -> Unit,
    onCadence: (Long, String) -> Unit,
) {
    val subscriptions = state.snapshot.objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }
    LazyColumn(Modifier.fillMaxSize()) {
        item {
            TextButton(onClick = onSubscribe) {
                Icon(Icons.Filled.Add, contentDescription = null)
                Text("Subscribe by URL")
            }
        }
        items(subscriptions, key = { it.id }) { subscription ->
            Column(Modifier.fillMaxWidth().padding(16.dp)) {
                Text(subscription.name, style = MaterialTheme.typography.bodyLarge)
                Text(
                    subscription.raw.url(),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Row(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    val selection = cadenceSelection(River.cadence(subscription))
                    Cadence.entries.forEach { cadence ->
                        CadenceChip(
                            cadence = cadence,
                            selected = selection[cadence] == true,
                            onSelect = { onCadence(subscription.id, cadence.value) },
                        )
                    }
                    TextButton(onClick = { onUnsubscribe(subscription.id) }) { Text("Unsubscribe") }
                }
            }
        }
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

@Composable
private fun ActivityScreen(
    groups: List<ch.lkmc.earnest.core.ActivityGroup>,
    entryTitles: Map<Long, String>,
    onRevert: (Long) -> Unit,
) {
    LazyColumn(Modifier.fillMaxSize()) {
        groups.forEach { group ->
            item(key = "group-${group.principalId}") {
                Text(
                    group.principalName,
                    style = MaterialTheme.typography.titleMedium,
                    modifier = Modifier.padding(16.dp),
                )
            }
            val activities = group.activities.orEmpty()
            items(activities, key = { it.id }) { activity ->
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
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
                }
            }
        }
    }
}

@Composable
private fun SubscribeDialog(
    onDismiss: () -> Unit,
    onSubscribe: (String, String) -> Unit,
) {
    var url by remember { mutableStateOf("") }
    var selectedCadence by remember { mutableStateOf(Cadence.TODAY.value) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Subscribe by URL") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(url, { url = it }, label = { Text("Feed URL") }, singleLine = true)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Cadence.entries.forEach { cadence ->
                        CadenceChip(
                            cadence = cadence,
                            selected = cadence.value == selectedCadence,
                            onSelect = { selectedCadence = cadence.value },
                        )
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = { onSubscribe(url, selectedCadence) }, enabled = url.isNotBlank()) { Text("Subscribe") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

private fun String.url(): String = runCatching {
    kotlinx.serialization.json.Json.parseToJsonElement(this)
        .jsonObject["url"]?.jsonPrimitive?.content ?: ""
}.getOrDefault("")

private fun String.dayName(): String = runCatching {
    val instant = Instant.parse(this)
    instant.atZone(ZoneId.systemDefault()).format(DateTimeFormatter.ofPattern("EEEE"))
}.getOrDefault("today")

private fun String.relativeTime(): String = runCatching {
    val instant = Instant.parse(this)
    val minutes = java.time.Duration.between(instant, Instant.now()).toMinutes()
    when {
        minutes < 1 -> "just now"
        minutes < 60 -> "${minutes}m ago"
        minutes < 60 * 24 -> "${minutes / 60}h ago"
        else -> "${minutes / (60 * 24)}d ago"
    }
}.getOrDefault("")

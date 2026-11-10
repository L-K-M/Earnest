package ch.lkmc.earnest

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
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
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.NavigationRailItemDefaults
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.adaptive.layout.AnimatedPane
import androidx.compose.material3.adaptive.layout.ListDetailPaneScaffoldRole
import androidx.compose.material3.adaptive.navigation.NavigableListDetailPaneScaffold
import androidx.compose.material3.adaptive.navigation.rememberListDetailPaneScaffoldNavigator
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteDefaults
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteScaffold
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.compose.runtime.getValue
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
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
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.River
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.core.UnreadCounts
import ch.lkmc.earnest.core.iconUrl
import ch.lkmc.earnest.core.typographyFor
import kotlinx.coroutines.delay
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

/**
 * Poll cadence while the app is visible, a gentler version of the Mac
 * client's two-minute poll. Queued marks flush and new entries arrive
 * without waiting for the fifteen-minute WorkManager sync.
 */
private const val FOREGROUND_REFRESH_MS = 3L * 60 * 1000

private enum class View(val label: String, val icon: ImageVector, val tone: ((Tones) -> Color)?) {
    TODAY("Today", Icons.Filled.DateRange, { it.orange }),
    EDITION("Edition", Icons.AutoMirrored.Filled.List, { it.blue }),
    LATER("Later", Icons.Filled.Notifications, { it.amber }),
    KEPT("Kept", Icons.Filled.Star, { it.plum }),
    DONE("Done", Icons.Filled.CheckCircle, { it.green }),
    FEEDS("Feeds", Icons.Filled.Home, { it.blue }),
    ACTIVITY("Activity", Icons.Filled.Info, { it.plum }),
    SEARCH("Search", Icons.Filled.Search, null),
}

/** The destination's tone for a theme; SEARCH is not in the navigation suite and has none. */
private fun View.toneColor(tones: Tones): Color? = tone?.invoke(tones)

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
            // The login has no scaffold, so a surface gives it the theme's background and text colours.
            Surface(Modifier.fillMaxSize()) { LoginScreen(onLoggedIn = model::onLoggedIn, notice = sessionNotice) }
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
    var showSettings by remember { mutableStateOf(false) }
    val readerEntry = state.snapshot.entries.firstOrNull { it.id == navigator.currentDestination?.contentKey && !it.deleted }
    val reader = remember { ReaderLoadState() }
    var activityGroups by remember { mutableStateOf<List<ch.lkmc.earnest.core.ActivityGroup>>(emptyList()) }
    var showSubscribe by remember { mutableStateOf(false) }
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val selected = navigator.currentDestination?.contentKey
    // Entries reached through an agent's output (a notification, a deep link
    // of unknown origin, an agent-change notice) open with click-to-load
    // images; see AppViewModel.openEntry. The set is saved with the navigator's
    // destination: after a rotation or process death the reader reopens the
    // same entry, and a plain remember would reload it with automatic images.
    var agentSelected by rememberSaveable(saver = AgentSelectedSaver) { mutableStateOf(emptySet<Long>()) }

    val lifecycleOwner = LocalLifecycleOwner.current
    LaunchedEffect(lifecycleOwner) {
        lifecycleOwner.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                delay(FOREGROUND_REFRESH_MS)
                model.refresh()
            }
        }
    }
    LaunchedEffect(initialEntry) {
        initialEntry?.let {
            agentSelected = agentSelected + it
            navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, it)
        }
    }
    LaunchedEffect(notificationId, state.accountKey) {
        if (state.accountKey == null) return@LaunchedEffect
        val id = notificationId ?: return@LaunchedEffect
        model.notificationEntry(id)?.let {
            agentSelected = agentSelected + it
            navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, it)
        }
    }
    LaunchedEffect(selected, state.accountKey) {
        reader.load(selected, state.accountKey) { id ->
            if (model.loadEntry(id) == null) throw java.io.IOException("This entry is no longer available.")
            model.markSeen(id)
            model.openEntry(id, agentSelected = id in agentSelected)
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
    val subscriptionIcons = remember(state.snapshot) {
        state.snapshot.objects
            .filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION }
            .mapNotNull { subscription -> subscription.iconUrl()?.let { subscription.id to it } }
            .toMap()
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

    /** Offers one undo for a completed action; the snackbar names the action. */
    fun offerUndo(message: String, onUndo: () -> Unit) {
        scope.launch {
            val result = snackbar.showSnackbar(
                message = message,
                actionLabel = "Undo",
                duration = SnackbarDuration.Short,
            )
            if (result == SnackbarResult.ActionPerformed) onUndo()
        }
    }

    val undoable = { action: ReversibleAction ->
        offerUndo(action.message) { model.undo(action) }
    }

    // Every destination draws its icon in its own tone. Labels stay ink: the
    // Material default for a selected label is `secondary`, now orange, which
    // would clash with every destination but Today.
    val tones = LocalTones.current
    val scheme = MaterialTheme.colorScheme
    val suiteItemColors = View.entries.mapNotNull { destination ->
        val tone = destination.toneColor(tones) ?: return@mapNotNull null
        val indicator = lerp(scheme.surfaceContainer, tone, indicatorBlend(scheme))
        destination to NavigationSuiteDefaults.itemColors(
            navigationBarItemColors = NavigationBarItemDefaults.colors(
                selectedIconColor = tone,
                unselectedIconColor = tone,
                selectedTextColor = scheme.onSurface,
                indicatorColor = indicator,
            ),
            navigationRailItemColors = NavigationRailItemDefaults.colors(
                selectedIconColor = tone,
                unselectedIconColor = tone,
                selectedTextColor = scheme.onSurface,
                indicatorColor = indicator,
            ),
            navigationDrawerItemColors = NavigationDrawerItemDefaults.colors(
                selectedContainerColor = indicator,
                selectedIconColor = tone,
                unselectedIconColor = tone,
                selectedTextColor = scheme.onSurface,
            ),
        )
    }.toMap()

    NavigationSuiteScaffold(
        navigationSuiteItems = {
            View.entries.filter { it != View.SEARCH }.forEach { destination ->
                item(
                    selected = view == destination,
                    onClick = {
                        view = destination
                        scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.List) }
                    },
                    icon = { Icon(destination.icon, contentDescription = null) },
                    label = { Text(destination.label) },
                    colors = suiteItemColors.getValue(destination),
                )
            }
        },
    ) {
        Scaffold(
            topBar = {
                EarnestTopBar(
                    if (showSettings) "Settings" else view.label,
                    state.loading,
                    model,
                    theme,
                    onThemeChange,
                    notificationsDenied,
                    onRequestNotifications,
                    onSearch = {
                        view = View.SEARCH
                        scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.List) }
                    },
                    onSettings = { showSettings = true },
                )
            },
            snackbarHost = { SnackbarHost(snackbar) },
        ) { padding ->
            Column(Modifier.fillMaxSize().padding(padding)) {
                state.cacheNotice?.let { Text(it, modifier = Modifier.padding(8.dp)) }
                state.error?.let {
                    Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(8.dp))
                }
                if (showSettings) {
                    BackHandler { showSettings = false }
                    SettingsScreen(
                        model = model,
                        state = state,
                        onBack = { showSettings = false },
                        modifier = Modifier.weight(1f),
                    )
                } else {
                NavigableListDetailPaneScaffold(
                    navigator = navigator,
                    listPane = {
                        AnimatedPane {
                            when (view) {
                                View.SEARCH -> SearchScreen(
                                    search = model.search,
                                    accountKey = state.accountKey,
                                    snapshot = state.snapshot,
                                    onOpen = { match ->
                                        if (match.kind == "agent_note" || match.untrusted) agentSelected = agentSelected + match.entryId
                                        scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, match.entryId) }
                                    },
                                )
                                View.FEEDS -> FeedsScreen(
                                    state = state,
                                    unreadCounts = state.unreadCounts,
                                    unreadSyncedAt = state.unreadCountsSyncedAt,
                                    unreadMode = state.readerSettings.readingMode == ch.lkmc.earnest.core.ReadingMode.UNREAD_COUNTS,
                                    onSubscribe = { showSubscribe = true },
                                    onUnsubscribe = { model.unsubscribe(it) },
                                    onCadence = { id, cadence ->
                                        val prior = state.snapshot.objects
                                            .firstOrNull { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && it.id == id }
                                            ?.let(River::cadence)
                                        model.setCadence(id, cadence) {
                                            // Undo only when the cadence this
                                            // change replaced is actually
                                            // known; never guess one.
                                            if (prior != null) {
                                                offerUndo("Cadence set to ${cadenceOf(cadence).label}") { model.setCadence(id, prior) }
                                            }
                                        }
                                    },
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
                                    subscriptionIcons = subscriptionIcons,
                                    briefs = if (view == View.TODAY) state.briefs else emptyList(),
                                    principalNames = state.principals,
                                    entryTitles = state.snapshot.entries.associate { it.id to it.title },
                                    agentChanges = if (view == View.TODAY) state.agentChanges else emptyList(),
                                    sessionChanges = if (view == View.TODAY) state.sessionAgentChanges else emptyList(),
                                    agentChangesError = if (view == View.TODAY) state.agentChangesError else null,
                                    unreadCounts = if (view == View.TODAY) state.unreadCounts else null,
                                    unreadSyncedAt = if (view == View.TODAY) state.unreadCountsSyncedAt else null,
                                    unreadMode = view == View.TODAY && state.readerSettings.readingMode == ch.lkmc.earnest.core.ReadingMode.UNREAD_COUNTS,
                                    onRevert = model::revert,
                                    onOpen = { entryId ->
                                        scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, entryId) }
                                    },
                                    onOpenAgentSelected = { entryId ->
                                        agentSelected = agentSelected + entryId
                                        scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, entryId) }
                                    },
                                    onLater = { id -> model.later(id, undoable) },
                                    onDone = { id -> model.done(id, undoable) },
                                    onSeen = { id -> model.markScrolledPast(id) },
                                    onKeep = { id -> model.keep(id, undoable) },
                                    onUnkeep = { id -> model.unkeep(id, undoable) },
                                    onMarkUnread = { id -> model.markUnread(id, undoable) },
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
                                    val entryState = state.states[entry.id]
                                        ?: SyncEngine.initialState(state.principalId, entry.id)
                                    ReaderHeader(
                                        entry = entry,
                                        source = subscriptionTitles[entry.subscriptionId].orEmpty(),
                                        isRead = canMarkUnread(entryState),
                                        onOpenOriginal = {
                                            original?.let { url ->
                                                if (!openOriginal(context, url)) {
                                                    scope.launch { snackbar.showSnackbar("No app can open this link.") }
                                                }
                                            }
                                        },
                                        onCopyLink = {
                                            original?.let { url ->
                                                // One notice keeps feedback identical on every supported
                                                // version; Android 13+ also shows its own clipboard toast.
                                                val message = if (copyLink(context, url)) "Link copied." else "Could not copy the link."
                                                scope.launch { snackbar.showSnackbar(message) }
                                            }
                                        },
                                        onShare = {
                                            original?.let { url ->
                                                if (!shareLink(context, entry.title, url)) {
                                                    scope.launch { snackbar.showSnackbar("Could not share this link.") }
                                                }
                                            }
                                        },
                                        onMarkUnread = { model.markUnread(entry.id, undoable) },
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
                                            typography = state.readerSettings.typographyFor(entry.feedId),
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
    onSearch: () -> Unit,
    onSettings: () -> Unit,
) {
    var themeOpen by remember { mutableStateOf(false) }
    var menuOpen by remember { mutableStateOf(false) }
    TopAppBar(
        title = { Text(title) },
        actions = {
            IconButton(onClick = onSearch) { Icon(Icons.Filled.Search, contentDescription = "Search") }
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
            // The menu anchors to this Box, beside the button rather than
            // inside the button's content slot.
            Box {
                IconButton(onClick = { menuOpen = true }) {
                    Icon(Icons.Filled.MoreVert, contentDescription = "More options")
                }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text("Settings") },
                        onClick = {
                            menuOpen = false
                            onSettings()
                        },
                    )
                    DropdownMenuItem(
                        text = { Text("Sign out") },
                        onClick = {
                            menuOpen = false
                            model.logout()
                        },
                    )
                }
            }
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

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ReaderHeader(
    entry: CachedEntry,
    source: String,
    isRead: Boolean,
    onOpenOriginal: () -> Unit,
    onCopyLink: () -> Unit,
    onShare: () -> Unit,
    onMarkUnread: () -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 16.dp)) {
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
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (originalUrl(entry) != null) {
                TextButton(onClick = onOpenOriginal) { Text("Open original") }
                TextButton(onClick = onCopyLink) { Text("Copy link") }
                TextButton(onClick = onShare) { Text("Share") }
            }
            if (isRead) {
                TextButton(onClick = onMarkUnread) { Text("Mark unread") }
            }
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
    onRevert: (Long) -> Unit,
    onOpen: (Long) -> Unit,
    onOpenAgentSelected: (Long) -> Unit,
    onLater: (Long) -> Unit,
    onDone: (Long) -> Unit,
    onSeen: (Long) -> Unit,
    onKeep: (Long) -> Unit,
    onUnkeep: (Long) -> Unit,
    onMarkUnread: (Long) -> Unit,
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
        items(entries, key = { it.id }) { entry ->
            val effective = states[entry.id] ?: SyncEngine.initialState(principalId, entry.id)
            SwipeActionRow(onLater = { onLater(entry.id) }, onDone = { onDone(entry.id) }) {
                EntryRow(
                    entry = entry,
                    state = effective,
                    source = subscriptionTitles[entry.subscriptionId].orEmpty(),
                    sourceIcon = subscriptionIcons[entry.subscriptionId],
                    onClick = { onOpen(entry.id) },
                )
            }
            Row(
                Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                if (effective.kept.value == kotlinx.serialization.json.JsonPrimitive(true)) {
                    TextButton(
                        onClick = { onUnkeep(entry.id) },
                        colors = ButtonDefaults.textButtonColors(contentColor = LocalTones.current.plum),
                    ) { Text("Unkeep") }
                } else {
                    TextButton(
                        onClick = { onKeep(entry.id) },
                        colors = ButtonDefaults.textButtonColors(contentColor = LocalTones.current.plum),
                    ) { Text("Keep") }
                }
                if (canMarkUnread(effective)) {
                    TextButton(onClick = { onMarkUnread(entry.id) }) { Text("Mark unread") }
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
                AgentChangesNotice("Agent changes this session", sessionChanges, entryTitles, agentChangesError, onOpenAgentSelected, onRevert)
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
    sourceIcon: String? = null,
    onClick: () -> Unit,
) {
    val unread = state.seen.value == kotlinx.serialization.json.JsonPrimitive(SyncEngine.VALUE_UNSEEN) &&
        state.shelf.value != kotlinx.serialization.json.JsonPrimitive(SyncEngine.VALUE_DONE)
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(16.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (unread) {
                    Box(
                        Modifier
                            .width(8.dp)
                            .height(8.dp)
                            .background(LocalTones.current.orange, MaterialTheme.shapes.small),
                    )
                    Spacer(Modifier.width(8.dp))
                }
                Text(entry.title, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            if (source.isNotEmpty()) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (sourceIcon != null) {
                        MediaImage(sourceIcon, Modifier.size(16.dp), RoundedCornerShape(4.dp))
                        Spacer(Modifier.width(4.dp))
                    }
                    Text(
                        "$source · ${entry.firstSeenAt.relativeTime()}",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
        entry.thumbnailUrl?.let { thumbnail ->
            Spacer(Modifier.width(12.dp))
            MediaImage(thumbnail, Modifier.size(56.dp), RoundedCornerShape(8.dp))
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
    unreadCounts: UnreadCounts?,
    unreadSyncedAt: Long?,
    unreadMode: Boolean,
    onSubscribe: () -> Unit,
    onUnsubscribe: (Long) -> Unit,
    onCadence: (Long, String) -> Unit,
) {
    val subscriptions = state.snapshot.objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }
    LazyColumn(Modifier.fillMaxSize()) {
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
            TextButton(onClick = onSubscribe) {
                Icon(Icons.Filled.Add, contentDescription = null)
                Text("Subscribe by URL")
            }
        }
        items(subscriptions, key = { it.id }) { subscription ->
            Column(Modifier.fillMaxWidth().padding(16.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    subscription.iconUrl()?.let {
                        MediaImage(it, Modifier.size(24.dp), RoundedCornerShape(4.dp))
                        Spacer(Modifier.width(8.dp))
                    }
                    Text(subscription.name, style = MaterialTheme.typography.bodyLarge)
                }
                Text(
                    subscription.raw.url(),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                if (unreadMode) {
                    val count = unreadCounts?.bySubscription?.get(subscription.id)
                    Text(
                        count?.let { "Unread $it" } ?: "Unread unavailable",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
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

private fun Long.relativeTime(): String {
    val minutes = java.time.Duration.between(Instant.ofEpochMilli(this), Instant.now()).toMinutes()
    return when {
        minutes < 1 -> "just now"
        minutes < 60 -> "${minutes}m ago"
        minutes < 60 * 24 -> "${minutes / 60}h ago"
        else -> "${minutes / (60 * 24)}d ago"
    }
}

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

/**
 * Saves the ids of entries an agent surfaced across configuration changes and
 * process death. A LongArray fits the saved-state bundle directly.
 */
internal val AgentSelectedSaver: Saver<MutableState<Set<Long>>, LongArray> = Saver(
    save = { it.value.toLongArray() },
    restore = { mutableStateOf(it.toSet()) },
)

package ch.lkmc.earnest

import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import android.webkit.WebView
import androidx.compose.animation.core.animate
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.material3.Button
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteScaffoldDefaults
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteType
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.semantics.heading
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import ch.lkmc.earnest.ReaderFilterChip as FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.rememberDrawerState
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
import androidx.compose.material3.adaptive.currentWindowAdaptiveInfo
import androidx.compose.material3.adaptive.layout.calculatePaneScaffoldDirective
import androidx.compose.material3.adaptive.layout.ListDetailPaneScaffoldRole
import androidx.compose.material3.adaptive.navigation.NavigableListDetailPaneScaffold
import androidx.compose.material3.adaptive.navigation.rememberListDetailPaneScaffoldNavigator
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteDefaults
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteScaffold
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.lifecycle.repeatOnLifecycle
import androidx.window.core.layout.WindowSizeClass
import androidx.lifecycle.viewmodel.compose.viewModel
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.FrontPagePeriod
import ch.lkmc.earnest.core.MAX_OPML_IMPORT_BYTES
import ch.lkmc.earnest.core.OpmlSummary
import ch.lkmc.earnest.core.River
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.core.UnreadCounts
import ch.lkmc.earnest.core.health
import ch.lkmc.earnest.core.iconUrl
import ch.lkmc.earnest.core.readAtMost
import ch.lkmc.earnest.core.typographyFor
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.time.Instant
import kotlin.math.roundToInt

/**
 * Poll cadence while the app is visible, a gentler version of the Mac
 * client's two-minute poll. Queued marks flush and new entries arrive
 * without waiting for the fifteen-minute WorkManager sync.
 */
private const val FOREGROUND_REFRESH_MS = 3L * 60 * 1000

internal enum class View(val label: String, val icon: ImageVector, val tone: ((Tones) -> Color)?) {
    FRONT("Front page", Icons.Filled.DateRange, { it.orange }),
    ALL("All articles", Icons.AutoMirrored.Filled.List, { it.blue }),
    LATER("Later", LaterIcon, { it.amber }),
    KEPT("Kept", Icons.Filled.Star, { it.plum }),
    DONE("Done", Icons.Filled.CheckCircle, { it.green }),
    FEEDS("Feeds", FeedIcon, { it.blue }),
    ACTIVITY("Activity", Icons.Filled.Info, { it.plum }),
    SEARCH("Search", Icons.Filled.Search, null),
}

/**
 * The selected destination and, for the front page, its window. Saved as one
 * value so a restored Edition selection reopens the weekly front page and a
 * restored Today selection reopens the daily one.
 */
internal data class SectionSelection(
    val view: View = View.FRONT,
    val period: FrontPagePeriod = FrontPagePeriod.DAILY,
)

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

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class, androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi::class)
@Composable
internal fun MainScaffold(
    model: AppViewModel,
    state: UiState,
    theme: ThemeSetting,
    onThemeChange: (ThemeSetting) -> Unit,
    initialEntry: Long?,
    notificationId: Long?,
    notificationsDenied: Boolean,
    onRequestNotifications: () -> Unit,
) {
    // The section and the settings screen survive rotation and process
    // death like the navigator's open article does; without them the reader
    // would reopen into a list the user was not looking at.
    var section by rememberSection()
    var searchReturn by rememberSection()
    var searchDetail by rememberSaveable { mutableStateOf(false) }
    val adaptiveInfo = currentWindowAdaptiveInfo()
    val directive = calculatePaneScaffoldDirective(adaptiveInfo)
    // The newspaper owns the reading area; an original opens on the next
    // pane, with native Back restoring the paper even on a wide display.
    val navigator = rememberListDetailPaneScaffoldNavigator<Long>(
        if (section.view == View.FRONT) directive.copy(maxHorizontalPartitions = 1) else directive,
    )
    var showSettings by rememberSettingsOpen()
    val readerEntry = state.snapshot.entries.firstOrNull { it.id == navigator.currentDestination?.contentKey && !it.deleted }
    val reader = remember { ReaderLoadState() }
    var readerRetry by remember { androidx.compose.runtime.mutableIntStateOf(0) }
    var activityGroups by remember { mutableStateOf<List<ch.lkmc.earnest.core.ActivityGroup>>(emptyList()) }
    var showSubscribe by remember { mutableStateOf(false) }
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val selected = navigator.currentDestination?.contentKey
    val drawerState = rememberDrawerState(DrawerValue.Closed)
    var feedSelection by rememberFeedSelection(state.accountKey)
    val searchListFocused = section.view == View.SEARCH && !searchDetail
    val detailOnly = navigator.currentDestination?.pane == ListDetailPaneScaffoldRole.Detail &&
        (section.view == View.FRONT || directive.maxHorizontalPartitions == 1) && !searchListFocused
    // Entries reached through an agent's output (a notification, a deep link
    // of unknown origin, an agent-change notice) open with click-to-load
    // images; see AppViewModel.openEntry. The set is saved with the navigator's
    // destination: after a rotation or process death the reader reopens the
    // same entry, and a plain remember would reload it with automatic images.
    var agentSelected by rememberSaveable(saver = AgentSelectedSaver) { mutableStateOf(emptySet<Long>()) }

    @Composable
    fun SearchContent() {
        SearchScreen(
            search = model.search,
            accountKey = state.accountKey,
            snapshot = state.snapshot,
            onOpen = { match ->
                searchDetail = true
                if (match.kind == "agent_note" || match.untrusted) agentSelected = agentSelected + match.entryId
                scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, match.entryId) }
            },
        )
        // Register in the list pane after the scaffold's own handler. The
        // detail pane's find handler can still take priority over this one.
        BackHandler(enabled = !detailOnly) { section = searchReturn }
    }

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
            searchDetail = true
            navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, it)
        }
    }
    LaunchedEffect(notificationId, state.accountKey) {
        if (state.accountKey == null) return@LaunchedEffect
        val id = notificationId ?: return@LaunchedEffect
        model.notificationEntry(id)?.let {
            agentSelected = agentSelected + it
            searchDetail = true
            navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, it)
        }
    }
    LaunchedEffect(selected, state.accountKey, readerRetry) {
        reader.load(selected, state.accountKey) { id ->
            if (model.loadEntry(id) == null) throw java.io.IOException("This entry is no longer available.")
            model.markSeen(id)
            model.openEntry(id, agentSelected = id in agentSelected)
        }
    }
    LaunchedEffect(section.view, state.accountKey) {
        if (section.view == View.ACTIVITY) {
            val since = Instant.now().minusSeconds(7 * 24 * 3600).toString()
            model.activity(since).collect { page ->
                activityGroups = page?.groups.orEmpty()
            }
        }
    }
    LaunchedEffect(section.view, section.period, state.accountKey) {
        if (section.view == View.FRONT && state.accountKey != null) {
            model.openFrontPage(section.period)
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
    val lists = remember(state.snapshot, state.states) {
        val visible = entries.filterNot { it.deleted }
        fun stateOf(id: Long) = state.states[id] ?: SyncEngine.initialState(state.principalId, id)
        val later = visible.filter { stateOf(it.id).shelf.value == kotlinx.serialization.json.JsonPrimitive(SyncEngine.VALUE_LATER) }
        val kept = visible.filter { stateOf(it.id).kept.value == kotlinx.serialization.json.JsonPrimitive(true) }
        val done = visible.filter { stateOf(it.id).shelf.value == kotlinx.serialization.json.JsonPrimitive(SyncEngine.VALUE_DONE) }
        mapOf(
            View.ALL to allArticles(entries),
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

    fun selectView(destination: View) {
        model.beginListVisit()
        showSettings = false
        feedSelection = FeedSelection.All
        section = section.copy(view = destination)
        scope.launch {
            navigator.navigateTo(ListDetailPaneScaffoldRole.List)
            drawerState.close()
        }
    }

    fun selectFeed(selection: FeedSelection) {
        model.beginListVisit()
        showSettings = false
        feedSelection = selection
        section = section.copy(view = View.ALL)
        scope.launch {
            navigator.navigateTo(ListDetailPaneScaffoldRole.List)
            drawerState.close()
        }
    }

    // Color identifies the selected destination; the rest stays quiet so
    // navigation does not compete with article content. Labels remain ink.
    val tones = LocalTones.current
    val scheme = MaterialTheme.colorScheme
    val suiteItemColors = View.entries.mapNotNull { destination ->
        val tone = destination.toneColor(tones) ?: return@mapNotNull null
        val indicator = lerp(scheme.surfaceContainer, tone, indicatorBlend(scheme))
        destination to NavigationSuiteDefaults.itemColors(
            navigationBarItemColors = NavigationBarItemDefaults.colors(
                selectedIconColor = tone,
                unselectedIconColor = scheme.onSurfaceVariant,
                selectedTextColor = scheme.onSurface,
                indicatorColor = indicator,
            ),
            navigationRailItemColors = NavigationRailItemDefaults.colors(
                selectedIconColor = tone,
                unselectedIconColor = scheme.onSurfaceVariant,
                selectedTextColor = scheme.onSurface,
                indicatorColor = indicator,
            ),
            navigationDrawerItemColors = NavigationDrawerItemDefaults.colors(
                selectedContainerColor = indicator,
                selectedIconColor = tone,
                unselectedIconColor = scheme.onSurfaceVariant,
                selectedTextColor = scheme.onSurface,
            ),
        )
    }.toMap()

    ModalNavigationDrawer(
        drawerState = drawerState,
        // Closed-drawer edge gestures must not compete with article swipes.
        gesturesEnabled = drawerState.isOpen,
        drawerContent = {
            ReaderDrawer(
                view = section.view,
                selection = feedSelection,
                objects = state.snapshot.objects,
                onView = ::selectView,
                onFeed = ::selectFeed,
                onClose = { scope.launch { drawerState.close() } },
                onSettings = {
                    showSettings = true
                    scope.launch { drawerState.close() }
                },
            )
        },
    ) {
    NavigationSuiteScaffold(
        layoutType = if (showSettings || detailOnly || section.view == View.SEARCH || WindowInsets.isImeVisible) {
            NavigationSuiteType.None
        } else if (
            adaptiveInfo.windowSizeClass.isWidthAtLeastBreakpoint(WindowSizeClass.WIDTH_DP_MEDIUM_LOWER_BOUND) &&
            !adaptiveInfo.windowSizeClass.isHeightAtLeastBreakpoint(WindowSizeClass.HEIGHT_DP_MEDIUM_LOWER_BOUND)
        ) {
            // A phone in landscape needs its scarce height for reading.
            NavigationSuiteType.NavigationRail
        } else NavigationSuiteScaffoldDefaults.calculateFromAdaptiveInfo(adaptiveInfo),
        navigationSuiteItems = {
            primaryDestinations.forEach { destination ->
                item(
                    selected = section.view == destination,
                    onClick = { selectView(destination) },
                    icon = { Icon(destination.icon, contentDescription = null) },
                    label = { Text(destination.navigationLabel, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    colors = suiteItemColors.getValue(destination),
                )
            }
        },
    ) {
        Scaffold(
            topBar = {
                EarnestTopBar(
                    when {
                        showSettings -> "Settings"
                        detailOnly -> "Article"
                        section.view == View.ALL -> feedSelection.title(state.snapshot.objects)
                        else -> section.view.label
                    },
                    state.loading,
                    model,
                    theme,
                    onThemeChange,
                    notificationsDenied,
                    onRequestNotifications,
                    onSearch = {
                        searchReturn = section
                        searchDetail = false
                        section = section.copy(view = View.SEARCH)
                    },
                    onSettings = { showSettings = true },
                    onRefresh = {
                        if (section.view == View.FRONT) model.refreshFrontPage() else model.reloadList()
                    },
                    focused = showSettings || detailOnly || section.view == View.SEARCH,
                    showRefresh = section.view != View.FRONT && !showSettings && !detailOnly && section.view != View.SEARCH,
                    navigationIcon = if (showSettings || detailOnly || section.view == View.SEARCH) Icons.AutoMirrored.Filled.ArrowBack else Icons.Filled.Menu,
                    navigationDescription = if (showSettings || detailOnly || section.view == View.SEARCH) "Back" else "Folders and feeds",
                    onNavigation = {
                        when {
                            showSettings -> showSettings = false
                            detailOnly -> scope.launch { navigator.navigateBack() }
                            section.view == View.SEARCH -> section = searchReturn
                            else -> scope.launch { drawerState.open() }
                        }
                    },
                )
            },
            snackbarHost = { SnackbarHost(snackbar) },
        ) { padding ->
            Column(Modifier.fillMaxSize().padding(padding).consumeWindowInsets(padding).imePadding()) {
                state.cacheNotice?.let { NoticeBanner(it, modifier = Modifier.padding(horizontal = ReaderSpace.md, vertical = ReaderSpace.xxs)) }
                state.error?.let {
                    NoticeBanner(it, tone = NoticeTone.ERROR, modifier = Modifier.padding(horizontal = ReaderSpace.md, vertical = ReaderSpace.xxs))
                }
                val compactSearch = searchListFocused && directive.maxHorizontalPartitions == 1
                val panesCovered = showSettings || compactSearch
                Box(Modifier.weight(1f)) {
                // Keep the adaptive transition bound while focused screens
                // cover its panes. Disposing it can leave history pointing at
                // an article while the scaffold still shows the list.
                NavigableListDetailPaneScaffold(
                    navigator = navigator,
                    modifier = if (panesCovered) Modifier.clearAndSetSemantics {} else Modifier,
                    listPane = {
                        if (!panesCovered) AnimatedPane {
                            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
                            Box(Modifier.widthIn(max = if (section.view == View.FRONT) 1100.dp else ReaderSpace.contentWidth).fillMaxSize()) {
                            when (section.view) {
                                View.FRONT -> FrontPageScreen(
                                    state = state.frontPage,
                                    states = state.states,
                                    principalId = state.principalId,
                                    briefs = state.briefs,
                                    principalNames = state.principals,
                                    entryTitles = state.snapshot.entries.associate { it.id to it.title },
                                    agentChanges = state.agentChanges,
                                    sessionChanges = state.sessionAgentChanges,
                                    agentChangesError = state.agentChangesError,
                                    onRevert = model::revert,
                                    onOpen = { entryId ->
                                        scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, entryId) }
                                    },
                                    onOpenAgentSelected = { entryId ->
                                        agentSelected = agentSelected + entryId
                                        scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, entryId) }
                                    },
                                    onPeriodChange = { period -> section = section.copy(period = period) },
                                    onRefresh = { model.refreshFrontPage() },
                                )
                                View.SEARCH -> SearchContent()
                                View.FEEDS -> FeedsScreen(
                                    model = model,
                                    state = state,
                                    unreadCounts = state.unreadCounts,
                                    unreadSyncedAt = state.unreadCountsSyncedAt,
                                    unreadMode = state.readerSettings.readingMode == ch.lkmc.earnest.core.ReadingMode.UNREAD_COUNTS,
                                    onSubscribe = { showSubscribe = true },
                                    onFolder = { selectFeed(FeedSelection.Folder(it)) },
                                    onFeed = { selectFeed(FeedSelection.Subscription(it)) },
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
                                                offerUndo("Cadence set to ${cadenceOf(cadence).label}") { model.undoCadence(id, prior, cadence) }
                                            }
                                        }
                                    },
                                )
                                View.ACTIVITY -> ActivityScreen(
                                    groups = activityGroups,
                                    entryTitles = state.snapshot.entries.associate { it.id to it.title },
                                    onRevert = { model.revert(it) },
                                )
                                else -> key(section.view, feedSelection) { EntryListScreen(
                                    title = if (section.view == View.ALL) feedSelection.title(state.snapshot.objects) else section.view.label,
                                    entries = if (section.view == View.ALL) feedSelection.filter(lists[section.view].orEmpty(), state.snapshot.objects) else lists[section.view].orEmpty(),
                                    states = state.states,
                                    principalId = state.principalId,
                                    subscriptionTitles = subscriptionTitles,
                                    subscriptionIcons = subscriptionIcons,
                                    briefs = emptyList(),
                                    principalNames = state.principals,
                                    entryTitles = state.snapshot.entries.associate { it.id to it.title },
                                    agentChanges = emptyList(),
                                    sessionChanges = emptyList(),
                                    agentChangesError = null,
                                    unreadCounts = null,
                                    unreadSyncedAt = null,
                                    unreadMode = false,
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
                                    accountKey = state.accountKey,
                                    // Wide screens keep the existing side-by-side reader.
                                    layoutOverride = ArticleLayout.HEADLINES.takeIf { directive.maxHorizontalPartitions > 1 },
                                    typography = { entry -> state.readerSettings.typographyFor(entry.feedId) },
                                    onLoadContent = { id ->
                                        model.markSeen(id)
                                        model.openEntry(id, agentSelected = id in agentSelected)
                                    },
                                ) }
                            }
                            }
                            }
                        }
                    },
                    detailPane = {
                        if (!panesCovered) AnimatedPane {
                            val entry = readerEntry
                            if (entry == null) {
                                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                                    when {
                                        selected == null -> QuietState("Ready when you are", "Choose an article to settle into a good read.")
                                        reader.loading -> LoadingState("Opening article…")
                                        else -> QuietState("This article did not load", reader.error ?: "Check your connection and try again.", actionLabel = "Try again", onAction = { readerRetry++ })
                                    }
                                }
                            } else {
                                BoxWithConstraints(Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
                                val headerMaxHeight = maxHeight * 0.48f
                                Column(Modifier.widthIn(max = ReaderSpace.contentWidth).fillMaxSize()) {
                                    val original = originalUrl(entry)
                                    val entryState = state.states[entry.id]
                                        ?: SyncEngine.initialState(state.principalId, entry.id)
                                    var readerWebView by remember { mutableStateOf<WebView?>(null) }
                                    // Per-article state starts fresh with the
                                    // article itself; the device-local zoom
                                    // survives navigation.
                                    var offlineCopy by remember(entry.id) { mutableStateOf(false) }
                                    var findOpen by remember(entry.id) { mutableStateOf(false) }
                                    var findQuery by remember(entry.id) { mutableStateOf("") }
                                    var findLabel by remember(entry.id) { mutableStateOf("") }
                                    var textZoom by remember { mutableStateOf(context.readerZoom()) }
                                    val closeFind: () -> Unit = {
                                        findOpen = false
                                        findQuery = ""
                                        findLabel = ""
                                        readerWebView?.clearMatches()
                                    }
                                    FindLabelBridge(readerWebView, entry.id) { findLabel = it }
                                    LaunchedEffect(findQuery, readerWebView) {
                                        val view = readerWebView ?: return@LaunchedEffect
                                        if (findQuery.isBlank()) {
                                            view.clearMatches()
                                            findLabel = ""
                                        } else {
                                            view.findAllAsync(findQuery)
                                        }
                                    }
                                    BackHandler(enabled = findOpen, onBack = closeFind)
                                    if (!findOpen) ReaderHeader(
                                        modifier = Modifier.heightIn(max = headerMaxHeight).verticalScroll(rememberScrollState()),
                                        entry = entry,
                                        source = subscriptionTitles[entry.subscriptionId].orEmpty(),
                                        isRead = canMarkUnread(entryState),
                                        offlineCopy = offlineCopy,
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
                                        onFind = {
                                            findOpen = !findOpen
                                            if (!findOpen) closeFind()
                                        },
                                        onSmallerText = {
                                            textZoom = zoomStep(textZoom, false)
                                            context.setReaderZoom(textZoom)
                                        },
                                        onLargerText = {
                                            textZoom = zoomStep(textZoom, true)
                                            context.setReaderZoom(textZoom)
                                        },
                                    )
                                    if (findOpen) {
                                        FindBar(
                                            query = findQuery,
                                            label = findLabel,
                                            onQuery = { findQuery = it },
                                            onPrevious = { readerWebView?.findNext(false) },
                                            onNext = { readerWebView?.findNext(true) },
                                            onClose = closeFind,
                                        )
                                    }
                                    val content = reader.content
                                    when {
                                        reader.loading -> LoadingState("Opening article…")
                                        content == null -> QuietState("This article did not load", reader.error ?: "Check your connection and try again.", actionLabel = "Try again", onAction = { readerRetry++ })
                                        else -> Reader(
                                            url = content.url,
                                            cachedHtml = content.cachedHtml,
                                            typography = state.readerSettings.typographyFor(entry.feedId),
                                            textZoom = textZoom,
                                            modifier = Modifier.fillMaxSize(),
                                            onFallback = { offlineCopy = true },
                                            onLiveContent = { offlineCopy = false },
                                            onWebView = { readerWebView = it },
                                        )
                                    }
                                }
                                }
                            }
                        }
                    },
                )
                if (showSettings) {
                    BackHandler { showSettings = false }
                    SettingsScreen(
                        model = model,
                        state = state,
                        onBack = { showSettings = false },
                        modifier = Modifier.fillMaxSize(),
                    )
                } else if (compactSearch) {
                    SearchContent()
                }
                }
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
    onRefresh: () -> Unit,
    focused: Boolean,
    showRefresh: Boolean,
    navigationIcon: ImageVector,
    navigationDescription: String,
    onNavigation: () -> Unit,
) {
    var themeOpen by remember { mutableStateOf(false) }
    var menuOpen by remember { mutableStateOf(false) }
    TopAppBar(
        title = { Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.titleLarge, modifier = Modifier.semantics { heading() }) },
        colors = TopAppBarDefaults.topAppBarColors(containerColor = MaterialTheme.colorScheme.surface),
        navigationIcon = {
            IconButton(onClick = onNavigation) { Icon(navigationIcon, contentDescription = navigationDescription) }
        },
        actions = {
            if (!focused) IconButton(onClick = onSearch) { Icon(Icons.Filled.Search, contentDescription = "Search") }
            if (loading && showRefresh) {
                Box(Modifier.size(48.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(
                        Modifier.size(20.dp),
                        strokeWidth = 2.dp,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
            } else if (showRefresh) {
                IconButton(onClick = onRefresh) {
                    Icon(Icons.Filled.Refresh, contentDescription = "Refresh")
                }
            }
            // The menu anchors to this Box, beside the button rather than
            // inside the button's content slot.
            Box {
                IconButton(onClick = { menuOpen = true }) {
                    Icon(Icons.Filled.MoreVert, contentDescription = "More options")
                }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text("Theme") },
                        onClick = { menuOpen = false; themeOpen = true },
                    )
                    if (notificationsDenied) {
                        DropdownMenuItem(
                            text = { Text("Enable notifications") },
                            onClick = { menuOpen = false; onRequestNotifications() },
                        )
                    }
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
internal fun ThemeDialog(
    current: ThemeSetting,
    onDismiss: () -> Unit,
    onSelect: (ThemeSetting) -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Theme") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
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
internal fun ReaderHeader(
    entry: CachedEntry,
    source: String,
    isRead: Boolean,
    offlineCopy: Boolean,
    onOpenOriginal: () -> Unit,
    onCopyLink: () -> Unit,
    onShare: () -> Unit,
    onMarkUnread: () -> Unit,
    onFind: () -> Unit,
    onSmallerText: () -> Unit,
    onLargerText: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var menuOpen by remember { mutableStateOf(false) }
    Column(modifier.fillMaxWidth().padding(start = ReaderSpace.gutter, end = ReaderSpace.gutter, top = ReaderSpace.sm)) {
        Text(
            entry.title.ifBlank { "Untitled" },
            style = MaterialTheme.typography.titleLarge,
            modifier = Modifier.semantics { heading() },
            maxLines = 4,
            overflow = TextOverflow.Ellipsis,
        )
        val date = entryDateLabel(entry)
        val meta = listOfNotNull(
            source.takeIf { it.isNotBlank() },
            date.takeIf { it.isNotBlank() },
            "Cached copy".takeIf { offlineCopy },
        )
        if (meta.isNotEmpty()) {
            Text(
                meta.joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        FlowRow(Modifier.fillMaxWidth().padding(top = ReaderSpace.xs), horizontalArrangement = Arrangement.SpaceBetween) {
            if (originalUrl(entry) != null) {
                FilledTonalButton(onClick = onOpenOriginal) { Text("Open original") }
            }
            Row {
            IconButton(onClick = onFind) { Icon(Icons.Filled.Search, contentDescription = "Find in article") }
            Box {
                IconButton(onClick = { menuOpen = true }) { Icon(Icons.Filled.MoreVert, contentDescription = "Reader options") }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    if (originalUrl(entry) != null) {
                        DropdownMenuItem(text = { Text("Share") }, onClick = { menuOpen = false; onShare() })
                        DropdownMenuItem(text = { Text("Copy link") }, onClick = { menuOpen = false; onCopyLink() })
                    }
                    DropdownMenuItem(text = { Text("Smaller text") }, onClick = { menuOpen = false; onSmallerText() })
                    DropdownMenuItem(text = { Text("Larger text") }, onClick = { menuOpen = false; onLargerText() })
                    if (isRead) {
                        DropdownMenuItem(text = { Text("Mark unread") }, onClick = { menuOpen = false; onMarkUnread() })
                    }
                }
            }
            }
        }
    }
}

@Composable
private fun FindBar(
    query: String,
    label: String,
    onQuery: (String) -> Unit,
    onPrevious: () -> Unit,
    onNext: () -> Unit,
    onClose: () -> Unit,
) {
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
        OutlinedTextField(
            value = query,
            onValueChange = onQuery,
            label = { Text("Find in article") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth().focusRequester(focus),
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
            keyboardActions = KeyboardActions(onSearch = { onNext() }),
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(label, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
            IconButton(onClick = onPrevious) { Icon(Icons.Filled.KeyboardArrowUp, contentDescription = "Previous match") }
            IconButton(onClick = onNext) { Icon(Icons.Filled.KeyboardArrowDown, contentDescription = "Next match") }
            IconButton(onClick = onClose) { Icon(Icons.Filled.Close, contentDescription = "Close find") }
        }
    }
}

private data class InlineScrollAnchor(val index: Int, val offset: Int)

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
    onRevert: (Long) -> Unit,
    onOpen: (Long) -> Unit,
    onOpenAgentSelected: (Long) -> Unit,
    onLater: (Long) -> Unit,
    onDone: (Long) -> Unit,
    onSeen: (Long) -> Unit,
    onKeep: (Long) -> Unit,
    onUnkeep: (Long) -> Unit,
    onMarkUnread: (Long) -> Unit,
    accountKey: String? = null,
    layoutOverride: ArticleLayout? = null,
    typography: (CachedEntry) -> ch.lkmc.earnest.core.ReaderTypography = { ch.lkmc.earnest.core.ReaderSettings().typographyFor(it.feedId) },
    onLoadContent: suspend (Long) -> ContentUrlResult? = { null },
    inlineContent: @Composable (ContentUrlResult, CachedEntry, InlineArticleState) -> Unit = { content, entry, viewport ->
        InlineArticleContent(content, typography(entry), viewport)
    },
    listState: androidx.compose.foundation.lazy.LazyListState = rememberLazyListState(),
) {
    val context = LocalContext.current
    var preferredLayout by remember { mutableStateOf(context.articleLayout()) }
    val layout = layoutOverride ?: preferredLayout
    var owner by rememberSaveable { mutableStateOf(accountKey) }
    var expandedId by rememberSaveable { mutableStateOf<Long?>(null) }
    if (accountKey != null && owner != accountKey) {
        if (owner != null) expandedId = null
        owner = accountKey
    }
    var retry by remember { mutableStateOf(0) }
    val expanded = expandedId?.takeIf { id -> entries.any { it.id == id } && layout == ArticleLayout.READING }
    val inlineReader = remember(expanded, accountKey) { ReaderLoadState() }
    val inlineViewport = remember(expanded, accountKey) { InlineArticleState() }
    var listWidth by remember { androidx.compose.runtime.mutableIntStateOf(0) }
    val density = LocalDensity.current
    val expandedTypography = entries.firstOrNull { it.id == expanded }?.let(typography)
    val textZoom = context.readerZoom()
    val measuredListWidth = listWidth
    SideEffect {
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
    }
    LaunchedEffect(expanded, accountKey, retry) {
        inlineReader.load(expanded, accountKey, onLoadContent)
    }
    var scrollAnchor by remember { mutableStateOf<InlineScrollAnchor?>(null) }
    fun changeExpansion(id: Long?) {
        val anchor = id ?: expanded
        scrollAnchor = listState.layoutInfo.visibleItemsInfo.firstOrNull { it.key == anchor }?.let { item ->
            // Keep a tapped headline in place; collapsing inside a body
            // returns to its header rather than reusing a deep body offset.
            InlineScrollAnchor(item.index, -item.offset.coerceAtLeast(0))
        }
        expandedId = id
    }
    LaunchedEffect(expanded, scrollAnchor) {
        scrollAnchor?.let { anchor ->
            // Request against the new composition. An earlier request can
            // be consumed by the old tall row before it has collapsed.
            listState.requestScrollToItem(anchor.index, anchor.offset)
            scrollAnchor = null
        }
    }
    BackHandler(enabled = expanded != null) { changeExpansion(null) }
    MarkSeenOnScroll(entries, principalId, listState, onSeen)
    val headerCount = (if (briefs.isNotEmpty()) 1 else 0) +
        (if (agentChanges.isNotEmpty() || agentChangesError != null) 1 else 0)
    LaunchedEffect(headerCount) {
        if (shouldRevealHeaders(listState.firstVisibleItemIndex, listState.firstVisibleItemScrollOffset, headerCount)) {
            listState.scrollToItem(0)
        }
    }
    LazyColumn(state = listState, modifier = Modifier.fillMaxSize().onSizeChanged { listWidth = it.width }, contentPadding = PaddingValues(bottom = 16.dp)) {
        if (layoutOverride == null) {
            item(key = "reading-layout") {
                ArticleLayoutControl(layout, onLayout = {
                    changeExpansion(null)
                    preferredLayout = it
                    context.setArticleLayout(it)
                })
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
        items(entries, key = { it.id }) { entry ->
            val effective = states[entry.id] ?: SyncEngine.initialState(principalId, entry.id)
            Surface(
                modifier = Modifier.fillMaxWidth(),
                color = MaterialTheme.colorScheme.surface,
            ) {
                Column {
                    SwipeActionRow(onLater = { onLater(entry.id) }, onDone = { onDone(entry.id) }) {
                        EntryRow(
                            entry = entry,
                            state = effective,
                            source = subscriptionTitles[entry.subscriptionId].orEmpty(),
                            sourceIcon = subscriptionIcons[entry.subscriptionId],
                            readingInFeed = layout == ArticleLayout.READING,
                            expanded = expanded == entry.id,
                            onClick = {
                                if (layout == ArticleLayout.READING) {
                                    changeExpansion(if (expanded == entry.id) null else entry.id)
                                } else onOpen(entry.id)
                            },
                            onOpenReader = { onOpen(entry.id) },
                            onKeep = { onKeep(entry.id) },
                            onUnkeep = { onUnkeep(entry.id) },
                            onLater = { onLater(entry.id) },
                            onDone = { onDone(entry.id) },
                            onMarkUnread = { onMarkUnread(entry.id) },
                        )
                    }
                    if (expanded == entry.id) {
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
                            FilledTonalButton(onClick = { onOpen(entry.id) }) { Text("Open reader") }
                            TextButton(onClick = { changeExpansion(null) }) { Text("Collapse") }
                        }
                    }
                    HorizontalDivider(Modifier.padding(horizontal = ReaderSpace.gutter), color = MaterialTheme.colorScheme.outlineVariant)
                }
            }
        }
        item(key = "end") {
            if (entries.isEmpty()) {
                val message = when (title) {
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
internal fun EntryRow(
    entry: CachedEntry,
    state: ch.lkmc.earnest.core.State,
    source: String,
    sourceIcon: String? = null,
    onClick: () -> Unit,
    readingInFeed: Boolean = false,
    expanded: Boolean = false,
    onOpenReader: () -> Unit = onClick,
    onKeep: () -> Unit = {},
    onUnkeep: () -> Unit = {},
    onLater: () -> Unit = {},
    onDone: () -> Unit = {},
    onMarkUnread: () -> Unit = {},
) {
    val unread = state.seen.value == JsonPrimitive(SyncEngine.VALUE_UNSEEN) &&
        state.shelf.value != JsonPrimitive(SyncEngine.VALUE_DONE)
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
            .semantics { stateDescription = if (unread) "Unread" else "Read" }
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
            Text(
                entry.title.ifBlank { "Untitled" },
                style = MaterialTheme.typography.titleMedium,
                fontWeight = if (unread) FontWeight.SemiBold else FontWeight.Normal,
                maxLines = 4,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f).testTag("headline-${entry.id}"),
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
    onLater: () -> Unit,
    onDone: () -> Unit,
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

@Composable
@OptIn(ExperimentalLayoutApi::class)
private fun FeedsScreen(
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

@Composable
private fun ActivityScreen(
    groups: List<ch.lkmc.earnest.core.ActivityGroup>,
    entryTitles: Map<Long, String>,
    onRevert: (Long) -> Unit,
) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(horizontal = ReaderSpace.gutter, vertical = ReaderSpace.md), verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
        if (groups.isEmpty()) {
            item { QuietState("No activity loaded", "Agent changes from the past seven days appear here. Refresh to check for updates.", icon = Icons.Filled.Info) }
        }
        groups.forEach { group ->
            item(key = "group-${group.principalId}") {
                Text(
                    group.principalName,
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.padding(top = ReaderSpace.md, bottom = ReaderSpace.xs).semantics { heading() },
                )
            }
            val activities = group.activities.orEmpty()
            items(activities, key = { it.id }) { activity ->
                Column(Modifier.fillMaxWidth().padding(vertical = ReaderSpace.xs), verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
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
                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
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

internal fun Long.relativeTime(): String {
    val minutes = java.time.Duration.between(Instant.ofEpochMilli(this), Instant.now()).toMinutes()
    return when {
        minutes < 1 -> "just now"
        minutes < 60 -> "${minutes}m ago"
        minutes < 60 * 24 -> "${minutes / 60}h ago"
        else -> "${minutes / (60 * 24)}d ago"
    }
}

internal fun String.relativeTime(): String = runCatching {
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

/**
 * The selected section and front-page window across the saved-state round
 * trip. The persisted value keeps the enum name so an older build's save
 * still restores; the legacy names map onto the front page windows.
 */
internal val SectionSaver: Saver<MutableState<SectionSelection>, String> = Saver(
    save = { "${it.value.view.name}|${it.value.period.wireName}" },
    restore = { encoded ->
        val names = encoded.split('|', limit = 2)
        val viewName = names.first()
        val periodName = names.getOrNull(1).orEmpty()
        val view = when (viewName) {
            "TODAY", "EDITION" -> View.FRONT
            else -> View.entries.firstOrNull { it.name == viewName } ?: View.FRONT
        }
        val period = when {
            viewName == "EDITION" -> FrontPagePeriod.WEEKLY
            else -> FrontPagePeriod.entries.firstOrNull { it.wireName == periodName } ?: FrontPagePeriod.DAILY
        }
        mutableStateOf(SectionSelection(view, period))
    },
)

/**
 * The section the user is looking at, kept across recreation. The wiring is
 * the seam a restoration test drives: reverting it to a plain remember fails
 * there.
 */
@Composable
internal fun rememberSection(): MutableState<SectionSelection> =
    rememberSaveable(saver = SectionSaver) { mutableStateOf(SectionSelection()) }

/** Whether Settings covers the sections; survives recreation like [rememberSection]. */
@Composable
internal fun rememberSettingsOpen(): MutableState<Boolean> =
    rememberSaveable { mutableStateOf(false) }

/**
 * Bridges the reader WebView's find results into [onLabel] for the article
 * [entryId]. It re-registers whenever either changes, so a result always
 * names the current article's matches, even for a reader that reuses one
 * WebView across articles.
 */
@Composable
internal fun FindLabelBridge(webView: WebView?, entryId: Long, onLabel: (String) -> Unit) {
    // The listener outlives recompositions; it must always call the newest
    // sink even when the keys do not change.
    val currentOnLabel by rememberUpdatedState(onLabel)
    DisposableEffect(webView, entryId) {
        val listener = WebView.FindListener { active, matches, done ->
            if (done) currentOnLabel(findCountLabel(active, matches))
        }
        webView?.setFindListener(listener)
        onDispose { webView?.setFindListener(null) }
    }
}

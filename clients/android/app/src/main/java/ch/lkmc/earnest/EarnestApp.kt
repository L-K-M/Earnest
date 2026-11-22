package ch.lkmc.earnest

import androidx.activity.compose.BackHandler
import android.webkit.WebView
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteScaffoldDefaults
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteType
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.rememberDrawerState
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.NavigationRailItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
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
import androidx.compose.runtime.setValue
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.repeatOnLifecycle
import androidx.window.core.layout.WindowSizeClass
import androidx.lifecycle.viewmodel.compose.viewModel
import ch.lkmc.earnest.core.FrontPagePeriod
import ch.lkmc.earnest.core.River
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.iconUrl
import ch.lkmc.earnest.core.typographyFor
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

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
internal fun EarnestApp(initialEntry: Long?, notificationId: Long?, intentDelivery: Int) {
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
            intentDelivery = intentDelivery,
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
    intentDelivery: Int,
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
    val destination = navigator.currentDestination
    // An article opened over another replaces it, as j and k do on the web. A
    // destination per swipe would grow the saved Back history without limit.
    var replacement by rememberSaveable(destination?.contentKey) { mutableStateOf<Long?>(null) }
    val selected = replacement ?: destination?.contentKey
    val readerEntry = state.snapshot.entries.firstOrNull { it.id == selected && !it.deleted }
    val reader = remember { ReaderLoadState() }
    var readerRetry by remember { androidx.compose.runtime.mutableIntStateOf(0) }
    var activityGroups by remember { mutableStateOf<List<ch.lkmc.earnest.core.ActivityGroup>>(emptyList()) }
    var showSubscribe by remember { mutableStateOf(false) }
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val drawerState = rememberDrawerState(DrawerValue.Closed)
    var feedSelection by rememberFeedSelection(state.accountKey)
    var unreadFilterOverride by key(state.accountKey, state.readerSettings.showOnlyUnread) {
        rememberSaveable { mutableStateOf<Boolean?>(null) }
    }
    // Search and the detail pane temporarily cover the current feed.
    val feedReading = key(if (section.view == View.SEARCH) searchReturn.view else section.view, feedSelection) {
        rememberFeedReadingState(state.accountKey)
    }
    val searchListFocused = section.view == View.SEARCH && !searchDetail
    val detailOnly = navigator.currentDestination?.pane == ListDetailPaneScaffoldRole.Detail &&
        (section.view == View.FRONT || directive.maxHorizontalPartitions == 1) && !searchListFocused
    // Entries reached through an agent's output (a notification, a deep link
    // of unknown origin, an agent-change notice) open with click-to-load
    // images; see AppViewModel.openEntry. The set is saved with the navigator's
    // destination: after a rotation or process death the reader reopens the
    // same entry, and a plain remember would reload it with automatic images.
    var agentSelected by rememberSaveable(saver = AgentSelectedSaver) { mutableStateOf(emptySet<Long>()) }
    // The intent delivery whose link or notification this screen opened.
    // A rotation or process restore hands back the same delivery, which
    // must neither return to its article nor mark it read again; tapping
    // the same link once more is a new delivery and opens it.
    var openedDelivery by rememberSaveable { androidx.compose.runtime.mutableIntStateOf(0) }
    // The article you opened that the reader still has to mark read. Saved,
    // so a rotation during its load still marks it once and a rotation
    // afterwards marks nothing.
    var readOnLoad by rememberSaveable { mutableStateOf<Long?>(null) }
    // The list the open article came from, which sideways swipes move through.
    var articleSequence by rememberArticleSequence(state.accountKey)

    /**
     * Opens [entryId] in the reader, which marks it read once it has loaded
     * the entry; an article that cannot load stays unread. The mark belongs
     * to this open, not to every load: a rotation or process restore reloads
     * the open article, and that must not read an article you marked unread.
     * [agentPick] opens it with click-to-load images. [within] is the list it
     * was opened from; a link, notification or agent notice has none.
     */
    fun openArticle(entryId: Long, agentPick: Boolean = false, within: ArticleSequence = ArticleSequence.NONE) {
        articleSequence = within
        if (agentPick) agentSelected = agentSelected + entryId
        // An article the reader already shows is not loaded again.
        if (entryId == selected && readerEntry != null) model.markSeen(entryId) else readOnLoad = entryId
        if (selected != null && destination?.pane == ListDetailPaneScaffoldRole.Detail) {
            replacement = entryId
        } else {
            scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, entryId) }
        }
    }

    @Composable
    fun SearchContent() {
        SearchScreen(
            search = model.search,
            accountKey = state.accountKey,
            snapshot = state.snapshot,
            onOpen = { match ->
                searchDetail = true
                val step = match.articleStep()
                openArticle(step.entryId, step.agentPick, within = model.search.state.value.matches.articleSequence(match))
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
    // A link opens at once; on a cold start the reader loads and marks its
    // article once the account has restored. A notification needs the
    // account to look up its article.
    LaunchedEffect(intentDelivery, state.accountKey) {
        if (intentDelivery == openedDelivery) return@LaunchedEffect
        val entryId = when {
            initialEntry != null -> initialEntry
            notificationId != null -> {
                // An unresolved notification stays pending, so the next
                // account change or recreation tries it again.
                if (state.accountKey == null) return@LaunchedEffect
                model.notificationEntry(notificationId) ?: return@LaunchedEffect
            }
            else -> null
        }
        openedDelivery = intentDelivery
        if (entryId == null) return@LaunchedEffect
        searchDetail = true
        openArticle(entryId, agentPick = true)
    }
    // Runs again after a rotation or process restore; only the load that
    // follows an open marks the article read.
    LaunchedEffect(selected, state.accountKey, readerRetry) {
        reader.load(selected, state.accountKey) { id ->
            if (model.loadEntry(id) == null) throw java.io.IOException("This entry is no longer available.")
            if (readOnLoad == id) {
                readOnLoad = null
                model.markSeen(id)
            }
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
    // Reading an article changes its registers. The chronological feed depends
    // only on entries and subscriptions and can keep its sorted list.
    val listEntries = when (section.view) {
        View.ALL -> remember(entries, state.snapshot.objects, feedSelection) {
            feedSelection.filter(allArticles(entries), state.snapshot.objects)
        }
        View.KEPT, View.LATER, View.DONE -> remember(entries, state.states, section.view) {
            entries.filter { entry ->
                val registers = state.states[entry.id]
                !entry.deleted && when (section.view) {
                    View.KEPT -> registers?.kept?.value == JsonPrimitive(true)
                    View.LATER -> registers?.shelf?.value == JsonPrimitive(SyncEngine.VALUE_LATER)
                    else -> registers?.shelf?.value == JsonPrimitive(SyncEngine.VALUE_DONE)
                }
            }.sortedBy { it.seq }
        }
        else -> emptyList()
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
        gesturesEnabled = drawerState.isOpen && !showSettings,
        drawerContent = {
            Box(if (showSettings) Modifier.clearAndSetSemantics {} else Modifier) {
                ReaderDrawer(
                    view = section.view,
                    selection = feedSelection,
                    objects = state.snapshot.objects,
                    onView = ::selectView,
                    onFeed = ::selectFeed,
                    onClose = { scope.launch { drawerState.close() } },
                    unreadCounts = state.unreadCounts.takeIf {
                        state.readerSettings.readingMode == ch.lkmc.earnest.core.ReadingMode.UNREAD_COUNTS
                    },
                    onSettings = {
                        showSettings = true
                        scope.launch { drawerState.close() }
                    },
                )
            }
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
                if (!showSettings) {
                    EarnestTopBar(
                        when {
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
                        focused = detailOnly || section.view == View.SEARCH,
                        showRefresh = section.view != View.FRONT && !detailOnly && section.view != View.SEARCH,
                        navigationIcon = if (detailOnly || section.view == View.SEARCH) Icons.AutoMirrored.Filled.ArrowBack else Icons.Filled.Menu,
                        navigationDescription = if (detailOnly || section.view == View.SEARCH) "Back" else "Folders and feeds",
                        onNavigation = {
                            when {
                                detailOnly -> scope.launch { navigator.navigateBack() }
                                section.view == View.SEARCH -> section = searchReturn
                                else -> scope.launch { drawerState.open() }
                            }
                        },
                    )
                }
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
                                        val order = state.frontPage.page?.articleSequence(entryId)
                                        openArticle(entryId, within = order ?: ArticleSequence.NONE)
                                    },
                                    onOpenAgentSelected = { entryId -> openArticle(entryId, agentPick = true) },
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
                                    entries = listEntries,
                                    readingState = feedReading,
                                    unreadOnly = if (section.view == View.ALL) unreadFilterOverride ?: state.readerSettings.showOnlyUnread else null,
                                    onUnreadOnlyChange = { unreadFilterOverride = it },
                                    states = state.states,
                                    principalId = state.principalId,
                                    subscriptionTitles = subscriptionTitles,
                                    subscriptionIcons = subscriptionIcons,
                                    briefs = emptyList(),
                                    principalNames = state.principals,
                                    entryTitles = emptyMap(),
                                    agentChanges = emptyList(),
                                    sessionChanges = emptyList(),
                                    agentChangesError = null,
                                    unreadCounts = null,
                                    unreadSyncedAt = null,
                                    unreadMode = false,
                                    onRevert = model::revert,
                                    onOpen = { entryId -> openArticle(entryId, within = feedReading.sequence.value) },
                                    onOpenAgentSelected = { entryId -> openArticle(entryId, agentPick = true) },
                                    onLater = { id -> model.later(id, undoable) },
                                    onDone = { id -> model.done(id, undoable) },
                                    onKeep = { id -> model.keep(id, undoable) },
                                    onUnkeep = { id -> model.unkeep(id, undoable) },
                                    onMarkUnread = { id -> model.markUnread(id, undoable) },
                                    accountKey = state.accountKey,
                                    // Wide screens keep the existing side-by-side reader.
                                    layoutOverride = ArticleLayout.HEADLINES.takeIf { directive.maxHorizontalPartitions > 1 },
                                    typography = { entry -> state.readerSettings.typographyFor(entry.feedId) },
                                    onExpand = { id -> model.markSeen(id) },
                                    onLoadContent = { id -> model.openEntry(id, agentSelected = id in agentSelected) },
                                ) }
                            }
                            }
                            }
                        }
                    },
                    detailPane = {
                        if (!panesCovered) AnimatedPane {
                            val entry = readerEntry
                            val swipe = rememberArticleSwipeState()
                            var findOpen by remember(selected) { mutableStateOf(false) }
                            val neighbour = { direction: ArticleDirection ->
                                articleSequence.neighbour(selected, direction) { id ->
                                    state.snapshot.entries.none { it.id == id && it.deleted }
                                }
                            }
                            val turn = { step: ArticleStep ->
                                openArticle(step.entryId, step.agentPick, within = articleSequence)
                            }
                            SideEffect { swipe.bind(neighbour, turn) }
                            val awaitingLoad = reader.loading || reader.entryId != selected
                            Box(Modifier.fillMaxSize().clipToBounds()) {
                            // Find has a text field, whose own sideways drags move the cursor.
                            Box(if (selected != null && !findOpen) Modifier.fillMaxSize().articleSwipe(swipe) else Modifier.fillMaxSize()) {
                            if (entry == null) {
                                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                                    when {
                                        selected == null -> QuietState("Ready when you are", "Choose an article to settle into a good read.")
                                        awaitingLoad -> LoadingState("Opening article…")
                                        else -> QuietState("This article did not load", reader.error ?: "Check your connection and try again.", actionLabel = "Try again", onAction = { readerRetry++ })
                                    }
                                }
                            } else {
                                BoxWithConstraints(Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
                                val headerMaxHeight = maxHeight * 0.48f
                                // A swipe replaces the article in place; its header scroll, find
                                // and WebView must not carry over to the next one.
                                key(entry.id) {
                                Column(Modifier.widthIn(max = ReaderSpace.contentWidth).fillMaxSize()) {
                                    val original = originalUrl(entry)
                                    val entryState = state.states[entry.id]
                                        ?: SyncEngine.initialState(state.principalId, entry.id)
                                    var readerWebView by remember { mutableStateOf<WebView?>(null) }
                                    // Per-article state starts fresh with the
                                    // article itself; the device-local zoom
                                    // survives navigation.
                                    var offlineCopy by remember(entry.id) { mutableStateOf(false) }
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
                                        articleActions = articleActions(neighbour, turn),
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
                                        awaitingLoad -> LoadingState("Opening article…")
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
                                            swipe = swipe,
                                        )
                                    }
                                }
                                }
                                }
                            }
                            }
                            ArticleSwipeHint(swipe)
                            }
                        }
                    },
                )
                if (showSettings) {
                    SettingsScreen(model = model, state = state, onBack = { showSettings = false }, modifier = Modifier.fillMaxSize())
                } else if (compactSearch) {
                    SearchContent()
                }
                }
            }
        }
    }

    }

    // Composed only while the drawer opens or is open, so it registers after
    // every handler beneath it, however recently one of those was registered.
    if (drawerState.targetValue == DrawerValue.Open) {
        BackHandler { scope.launch { drawerState.close() } }
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

package ch.lkmc.earnest

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.displayCutout
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.union
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.Scaffold
import androidx.compose.material3.ScaffoldDefaults
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.adaptive.currentWindowAdaptiveInfo
import androidx.compose.material3.adaptive.layout.AnimatedPane
import androidx.compose.material3.adaptive.layout.ListDetailPaneScaffoldRole
import androidx.compose.material3.adaptive.layout.calculatePaneScaffoldDirective
import androidx.compose.material3.adaptive.navigation.NavigableListDetailPaneScaffold
import androidx.compose.material3.adaptive.navigation.rememberListDetailPaneScaffoldNavigator
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteScaffold
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteScaffoldDefaults
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteType
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.pulltorefresh.PullToRefreshDefaults
import androidx.compose.material3.pulltorefresh.rememberPullToRefreshState
import androidx.compose.material3.rememberDrawerState
import androidx.compose.material3.rememberTopAppBarState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.window.core.layout.WindowSizeClass
import ch.lkmc.earnest.core.FrontPagePeriod
import ch.lkmc.earnest.core.River
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.iconUrl
import ch.lkmc.earnest.core.typographyFor
import java.time.Instant
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive

/**
 * Poll cadence while the app is visible, a gentler version of the Mac
 * client's two-minute poll. Queued marks flush and new entries arrive
 * without waiting for the fifteen-minute WorkManager sync.
 */
private const val FOREGROUND_REFRESH_MS = 3L * 60 * 1000

internal enum class View(
    val label: String,
    val icon: ImageVector,
    val selectedIcon: ImageVector,
    val tone: ((Tones) -> Color)?,
) {
    FRONT("Front page", EarnestIcons.FrontPage, EarnestIcons.FrontPageSelected, { it.orange }),
    ALL("All articles", EarnestIcons.Articles, EarnestIcons.ArticlesSelected, { it.blue }),
    LATER("Later", EarnestIcons.Later, EarnestIcons.LaterSelected, { it.amber }),
    KEPT("Kept", EarnestIcons.Kept, EarnestIcons.KeptSelected, { it.plum }),
    DONE("Done", EarnestIcons.Done, EarnestIcons.DoneSelected, { it.green }),
    FEEDS("Feeds", EarnestIcons.Feeds, EarnestIcons.FeedsSelected, { it.green }),
    ACTIVITY("Activity", EarnestIcons.Activity, EarnestIcons.ActivitySelected, { it.purple }),
    SEARCH("Search", EarnestIcons.Search, EarnestIcons.Search, null),
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

@OptIn(ExperimentalMaterial3Api::class, ExperimentalMaterial3ExpressiveApi::class, ExperimentalLayoutApi::class, androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi::class)
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
        if (section.view.singlePane) directive.copy(maxHorizontalPartitions = 1) else directive,
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
    var activityLoad by remember { mutableStateOf(ActivityLoad.LOADING) }
    var activityRequest by remember { androidx.compose.runtime.mutableIntStateOf(0) }
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
        (section.view.singlePane || directive.maxHorizontalPartitions == 1) && !searchListFocused
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
    LaunchedEffect(section.view, state.accountKey, activityRequest) {
        if (section.view != View.ACTIVITY || state.accountKey == null) return@LaunchedEffect
        activityLoad = ActivityLoad.LOADING
        val since = Instant.now().minusSeconds(7 * 24 * 3600).toString()
        var answered = false
        model.activity(since).collect { page ->
            // A failed fetch keeps the last list on screen.
            if (page == null) return@collect
            answered = true
            activityGroups = page.groups.orEmpty()
        }
        activityLoad = if (answered) ActivityLoad.LOADED else ActivityLoad.FAILED
    }
    LaunchedEffect(section.view, section.period, state.accountKey) {
        if (section.view == View.FRONT && state.accountKey != null) {
            model.openFrontPage(section.period)
        }
    }

    val entries = state.snapshot.entries
    val entryTitles = remember(entries) { entries.associate { it.id to it.title } }
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

    // Navigation computes the panes from the directive of its moment, so it
    // waits until the new place's directive has been composed.
    var listVisit by remember { androidx.compose.runtime.mutableIntStateOf(0) }
    LaunchedEffect(listVisit) {
        if (listVisit > 0) navigator.navigateTo(ListDetailPaneScaffoldRole.List)
    }

    fun selectView(destination: View) {
        model.beginListVisit()
        showSettings = false
        feedSelection = FeedSelection.All
        section = section.copy(view = destination)
        listVisit++
        scope.launch { drawerState.close() }
    }

    fun selectFeed(selection: FeedSelection) {
        model.beginListVisit()
        showSettings = false
        feedSelection = selection
        section = section.copy(view = View.ALL)
        listVisit++
        scope.launch { drawerState.close() }
    }

    val underlyingView = if (section.view == View.SEARCH) searchReturn.view else section.view
    val windowSize = adaptiveInfo.windowSizeClass
    val shortWindow = !windowSize.isHeightAtLeastBreakpoint(WindowSizeClass.HEIGHT_DP_MEDIUM_LOWER_BOUND)
    val besideList = directive.maxHorizontalPartitions > 1
    val barSize = when {
        section.view == View.SEARCH || shortWindow || besideList -> BarSize.SMALL
        LocalDensity.current.fontScale >= LARGE_TITLE_MAX_FONT_SCALE -> BarSize.SMALL
        else -> BarSize.LARGE
    }
    // A new place opens with its title expanded; Search gives the old one back.
    val barState = key(underlyingView.primary, feedSelection) { rememberTopAppBarState() }
    val scrollBehavior = TopAppBarDefaults.exitUntilCollapsedScrollBehavior(barState)
    val titleExpanded by remember(barState) { derivedStateOf { barState.collapsedFraction == 0f } }

    val readerFills = detailOnly && !showSettings
    val readerTypography = readerEntry?.let { state.readerSettings.typographyFor(it.feedId) }
        ?: state.readerSettings.typographyFor(0)
    val canvas = rememberReaderCanvas(readerTypography, theme)
    if (readerFills) OverrideBarIcons(barIcons(canvas.scheme))

    val navigationType = when {
        showSettings || detailOnly || section.view == View.SEARCH || WindowInsets.isImeVisible -> NavigationSuiteType.None
        // A phone in landscape needs its scarce height for reading.
        shortWindow && windowSize.isWidthAtLeastBreakpoint(WindowSizeClass.WIDTH_DP_MEDIUM_LOWER_BOUND) ->
            NavigationSuiteType.WideNavigationRailCollapsed
        else -> NavigationSuiteScaffoldDefaults.navigationSuiteType(adaptiveInfo)
    }
    val refreshList = {
        if (section.view == View.ACTIVITY) activityRequest++
        model.reloadList()
    }

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
        navigationSuiteType = navigationType,
        navigationItems = {
            primaryDestinations.forEach { destination ->
                DestinationItem(
                    destination = destination,
                    selected = section.view.primary == destination,
                    type = navigationType,
                    onClick = { selectView(destination) },
                )
            }
        },
        navigationItemVerticalArrangement = Arrangement.Center,
    ) {
        Scaffold(
            modifier = if (barSize == BarSize.LARGE && !readerFills && !showSettings) {
                Modifier.nestedScroll(scrollBehavior.nestedScrollConnection)
            } else {
                Modifier
            },
            containerColor = if (readerFills) canvas.scheme.surface else MaterialTheme.colorScheme.background,
            contentColor = MaterialTheme.colorScheme.onBackground,
            contentWindowInsets = ScaffoldDefaults.contentWindowInsets.union(WindowInsets.displayCutout),
            topBar = {
                // Settings and a full-window article each draw their own bar.
                if (!showSettings && !readerFills) {
                    val searching = section.view == View.SEARCH
                    EarnestTopBar(
                        title = screenTitle(section.view, feedSelection, state.snapshot.objects),
                        size = barSize,
                        navigation = if (searching) BarNavigation.BACK else BarNavigation.DRAWER,
                        onNavigation = {
                            if (searching) section = searchReturn else scope.launch { drawerState.open() }
                        },
                        actions = if (searching) null else BarActions(
                            onSearch = {
                                searchReturn = section
                                searchDetail = false
                                section = section.copy(view = View.SEARCH)
                            },
                            onSettings = { showSettings = true },
                            onSignOut = model::logout,
                            theme = theme,
                            onThemeChange = onThemeChange,
                            onRefresh = refreshList.takeIf { section.view != View.FRONT },
                            refreshing = state.loading || (section.view == View.ACTIVITY && activityLoad == ActivityLoad.LOADING),
                            onEnableNotifications = onRequestNotifications.takeIf { notificationsDenied },
                        ),
                        scrollBehavior = scrollBehavior.takeIf { barSize == BarSize.LARGE },
                    )
                }
            },
            snackbarHost = {
                // An article's action bar stays tappable under an Undo notice.
                SnackbarHost(snackbar, Modifier.padding(bottom = if (readerFills && !shortWindow) ReaderActionClearance else 0.dp))
            },
        ) { padding ->
            Column(Modifier.fillMaxSize().padding(padding).consumeWindowInsets(padding).imePadding()) {
                val bannerModifier = Modifier.align(Alignment.CenterHorizontally)
                    .widthIn(max = ReaderSpace.contentWidth)
                    .padding(horizontal = ReaderSpace.md, vertical = ReaderSpace.xxs)
                state.cacheNotice?.let { NoticeBanner(it, bannerModifier) }
                state.error?.let { NoticeBanner(it, bannerModifier, tone = NoticeTone.ERROR) }
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
                            Column(Modifier.widthIn(max = if (section.view == View.FRONT) ReaderSpace.paperWidth else ReaderSpace.contentWidth).fillMaxSize()) {
                            if (section.view in savedShelves) SavedShelfSwitcher(section.view, ::selectView)
                            RefreshableContent(
                                loading = when (section.view) {
                                    View.FRONT -> state.frontPage.loading
                                    View.ACTIVITY -> state.loading || activityLoad == ActivityLoad.LOADING
                                    else -> state.loading
                                },
                                // A downward pull first brings the collapsed title back.
                                enabled = section.view != View.SEARCH && (barSize == BarSize.SMALL || titleExpanded),
                                onRefresh = { if (section.view == View.FRONT) model.refreshFrontPage() else refreshList() },
                                modifier = Modifier.weight(1f),
                            ) {
                            when (section.view) {
                                View.FRONT -> FrontPageScreen(
                                    state = state.frontPage,
                                    states = state.states,
                                    principalId = state.principalId,
                                    briefs = state.briefs,
                                    principalNames = state.principals,
                                    entryTitles = entryTitles,
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
                                    load = activityLoad,
                                    entryTitles = entryTitles,
                                    onRevert = { model.revert(it) },
                                    onRetry = { activityRequest++ },
                                )
                                else -> key(section.view, feedSelection) { EntryListScreen(
                                    title = if (section.view == View.ALL) feedSelection.title(state.snapshot.objects) else section.view.label,
                                    destination = section.view,
                                    loading = state.loading,
                                    onBrowse = { selectView(if (section.view == View.ALL) View.FEEDS else View.ALL) },
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
                            val pane = @Composable {
                                Box(Modifier.fillMaxSize().clipToBounds()) {
                                    // Find has a text field, whose own sideways drags move the cursor.
                                    Box(if (selected != null && !findOpen) Modifier.fillMaxSize().articleSwipe(swipe) else Modifier.fillMaxSize()) {
                                        if (entry == null) {
                                            ReaderPlaceholder(
                                                choosing = selected == null,
                                                awaitingLoad = awaitingLoad,
                                                error = reader.error,
                                                onRetry = { readerRetry++ },
                                                onBack = if (readerFills) ({ scope.launch { navigator.navigateBack() } }) else null,
                                            )
                                        } else {
                                            val entryState = state.states[entry.id]
                                                ?: SyncEngine.initialState(state.principalId, entry.id)
                                            ArticlePane(
                                                entry = entry,
                                                state = entryState,
                                                source = subscriptionTitles[entry.subscriptionId].orEmpty(),
                                                sourceIcon = subscriptionIcons[entry.subscriptionId],
                                                reader = reader,
                                                awaitingLoad = awaitingLoad,
                                                typography = readerTypography,
                                                shelf = readerShelf(
                                                    entryState,
                                                    onKeep = { model.keep(entry.id, undoable) },
                                                    onUnkeep = { model.unkeep(entry.id, undoable) },
                                                    onLater = { model.later(entry.id, undoable) },
                                                    onDone = { model.done(entry.id, undoable) },
                                                ),
                                                swipe = swipe,
                                                articleActions = articleActions(neighbour, turn),
                                                findOpen = findOpen,
                                                onFindOpen = { findOpen = it },
                                                onMarkUnread = { model.markUnread(entry.id, undoable) },
                                                onRetry = { readerRetry++ },
                                                onNotify = { scope.launch { snackbar.showSnackbar(it) } },
                                                onBack = if (readerFills) ({ scope.launch { navigator.navigateBack() } }) else null,
                                                onSettings = if (readerFills) ({ showSettings = true }) else null,
                                            )
                                        }
                                    }
                                    ArticleSwipeHint(swipe)
                                }
                            }
                            if (selected == null) pane() else ReaderCanvasSurface(canvas, Modifier.fillMaxSize(), pane)
                        }
                    },
                )
                if (showSettings) {
                    SettingsScreen(
                        model = model,
                        state = state,
                        onBack = { showSettings = false },
                        modifier = Modifier.fillMaxSize(),
                        appTheme = theme,
                        onAppTheme = onThemeChange,
                    )
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
        SubscribeSheet(
            onDismiss = { showSubscribe = false },
            onSubscribe = { url, cadence ->
                showSubscribe = false
                model.subscribe(url, cadence)
            },
        )
    }
}

/** The paper and the subscription list have nothing to show in an article pane. */
private val View.singlePane: Boolean get() = this == View.FRONT || this == View.FEEDS

/** Above this text size a collapsing title would take the screen from its content. */
private const val LARGE_TITLE_MAX_FONT_SCALE = 1.5f

private val ReaderActionClearance = 72.dp

/** Pull to refresh, showing its indicator only for a pull and not for background syncs. */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun RefreshableContent(
    loading: Boolean,
    enabled: Boolean,
    onRefresh: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable BoxScope.() -> Unit,
) {
    var pulled by remember { mutableStateOf(false) }
    val pullState = rememberPullToRefreshState()
    LaunchedEffect(loading) { if (!loading) pulled = false }
    val refreshing = pulled && loading
    PullToRefreshBox(
        isRefreshing = refreshing,
        onRefresh = {
            pulled = true
            onRefresh()
        },
        modifier = modifier.fillMaxWidth(),
        state = pullState,
        enabled = enabled,
        indicator = {
            PullToRefreshDefaults.LoadingIndicator(
                state = pullState,
                isRefreshing = refreshing,
                modifier = Modifier.align(Alignment.TopCenter),
            )
        },
        content = content,
    )
}

/** The article's pane before there is an article to show. */
@Composable
private fun ReaderPlaceholder(
    choosing: Boolean,
    awaitingLoad: Boolean,
    error: String?,
    onRetry: () -> Unit,
    onBack: (() -> Unit)?,
) {
    Column(Modifier.fillMaxSize()) {
        onBack?.let {
            Row(Modifier.heightIn(min = 56.dp).padding(horizontal = ReaderSpace.xxs), verticalAlignment = Alignment.CenterVertically) {
                ActionIconButton(EarnestIcons.Back, "Back", it)
                Text("Article", style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(horizontal = ReaderSpace.sm))
            }
        }
        Box(Modifier.weight(1f).fillMaxWidth().verticalScroll(rememberScrollState()), contentAlignment = Alignment.Center) {
            when {
                choosing -> QuietState("Ready when you are", "Choose an article to settle into a good read.")
                awaitingLoad -> LoadingState("Opening article…")
                else -> ArticleLoadFailure(error, onRetry)
            }
        }
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

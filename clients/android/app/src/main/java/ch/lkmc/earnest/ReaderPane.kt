package ch.lkmc.earnest

import android.webkit.WebView
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextField
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.ReaderTypography
import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.SyncEngine
import kotlinx.serialization.json.JsonPrimitive

/** Below this height the title and actions fold into the toolbar and its menu. */
private val ShortReaderHeight = 420.dp
private const val HEADER_SHARE = 0.48f
private const val TITLE_LINES = 4
private const val CACHED_COPY = "Cached copy"
private val SourceMarkSize = 20.dp
private val ActionBarHeight = 64.dp
private val ActionIndicatorWidth = 56.dp
private val ActionIndicatorHeight = 32.dp

/**
 * Labels under icons stop growing here, as the system's own bars do, rather
 * than break mid-word. Each item is named to TalkBack in full.
 */
internal const val BAR_LABEL_MAX_FONT_SCALE = 1.3f

internal enum class ReaderChrome { FULL, SHORT }

/** Keep, Later and Done for the open article, and whether each is already so. */
internal data class ReaderShelf(
    val kept: Boolean,
    val later: Boolean,
    val done: Boolean,
    val onToggleKept: () -> Unit,
    val onLater: () -> Unit,
    val onDone: () -> Unit,
) {
    val keepLabel: String get() = if (kept) "Unkeep" else "Keep"
}

internal fun readerShelf(state: State, onKeep: () -> Unit, onUnkeep: () -> Unit, onLater: () -> Unit, onDone: () -> Unit): ReaderShelf {
    val kept = state.kept.value == JsonPrimitive(true)
    return ReaderShelf(
        kept = kept,
        later = state.shelf.value == JsonPrimitive(SyncEngine.VALUE_LATER),
        done = state.shelf.value == JsonPrimitive(SyncEngine.VALUE_DONE),
        onToggleKept = if (kept) onUnkeep else onKeep,
        onLater = onLater,
        onDone = onDone,
    )
}

/** The article and everything drawn on its canvas. */
@Composable
internal fun ArticlePane(
    entry: CachedEntry,
    state: State,
    source: String,
    sourceIcon: String?,
    reader: ReaderLoadState,
    awaitingLoad: Boolean,
    typography: ReaderTypography,
    shelf: ReaderShelf,
    swipe: ArticleSwipeState,
    articleActions: List<CustomAccessibilityAction>,
    findOpen: Boolean,
    onFindOpen: (Boolean) -> Unit,
    onMarkUnread: () -> Unit,
    onRetry: () -> Unit,
    onNotify: (String) -> Unit,
    /** Null beside its list, where Back and the app's options belong to the list's chrome. */
    onBack: (() -> Unit)?,
    appOptions: AppOptions?,
    onActionBar: (shown: Boolean) -> Unit,
) {
    val context = LocalContext.current
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val chrome = if (maxHeight < ShortReaderHeight) ReaderChrome.SHORT else ReaderChrome.FULL
        val headerMaxHeight = maxHeight * HEADER_SHARE
        // A swipe replaces the article in place; its header scroll, find
        // and WebView must not carry over to the next one.
        key(entry.id) {
            Column(Modifier.fillMaxSize()) {
                val original = originalUrl(entry)
                var webView by remember { mutableStateOf<WebView?>(null) }
                // Per-article state starts fresh with the article itself; the
                // device-local zoom survives navigation.
                var offlineCopy by remember { mutableStateOf(false) }
                var findQuery by remember { mutableStateOf("") }
                var findLabel by remember { mutableStateOf("") }
                var textZoom by remember { mutableStateOf(context.readerZoom()) }
                val closeFind = {
                    onFindOpen(false)
                    findQuery = ""
                    findLabel = ""
                    webView?.clearMatches()
                    Unit
                }
                val openOriginalPage = {
                    if (original != null && !openOriginal(context, original)) onNotify("No app can open this link.")
                }
                val changeZoom = { up: Boolean ->
                    textZoom = zoomStep(textZoom, up)
                    context.setReaderZoom(textZoom)
                }
                FindLabelBridge(webView, entry.id) { findLabel = it }
                LaunchedEffect(findQuery, webView) {
                    val view = webView ?: return@LaunchedEffect
                    if (findQuery.isBlank()) {
                        view.clearMatches()
                        findLabel = ""
                    } else {
                        view.findAllAsync(findQuery)
                    }
                }
                BackHandler(enabled = findOpen, onBack = closeFind)
                if (findOpen) {
                    FindBar(
                        query = findQuery,
                        label = findLabel,
                        onQuery = { findQuery = it },
                        onPrevious = { webView?.findNext(false) },
                        onNext = { webView?.findNext(true) },
                        onClose = closeFind,
                    )
                } else {
                    ReaderHeader(
                        entry = entry,
                        source = source,
                        sourceIcon = sourceIcon,
                        isRead = canMarkUnread(state),
                        offlineCopy = offlineCopy,
                        chrome = chrome,
                        shelf = shelf,
                        articleActions = articleActions,
                        titleModifier = Modifier.heightIn(max = headerMaxHeight).verticalScroll(rememberScrollState()),
                        onBack = onBack,
                        appOptions = appOptions,
                        onOpenOriginal = openOriginalPage,
                        onCopyLink = {
                            original?.let { url ->
                                // One notice keeps feedback identical on every supported
                                // version; Android 13+ also shows its own clipboard toast.
                                onNotify(if (copyLink(context, url)) "Link copied." else "Could not copy the link.")
                            }
                        },
                        onShare = {
                            original?.let { url ->
                                if (!shareLink(context, entry.title, url)) onNotify("Could not share this link.")
                            }
                        },
                        onMarkUnread = onMarkUnread,
                        onFind = { onFindOpen(true) },
                        onSmallerText = { changeZoom(false) },
                        onLargerText = { changeZoom(true) },
                    )
                }
                Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.TopCenter) {
                    val content = reader.content
                    when {
                        awaitingLoad -> LoadingState("Opening article…")
                        content == null -> ArticleLoadFailure(
                            reader.error,
                            onRetry,
                            Modifier.verticalScroll(rememberScrollState()),
                        )
                        else -> Reader(
                            url = content.url,
                            cachedHtml = content.cachedHtml,
                            typography = typography,
                            textZoom = textZoom,
                            modifier = Modifier.widthIn(max = ReaderSpace.contentWidth).fillMaxSize(),
                            onFallback = { offlineCopy = true },
                            onLiveContent = { offlineCopy = false },
                            onWebView = { webView = it },
                            swipe = swipe,
                        )
                    }
                }
                if (!findOpen && chrome == ReaderChrome.FULL) {
                    ReaderActionBar(shelf, canOpenOriginal = original != null, onOpenOriginal = openOriginalPage)
                    ReportWhileShown(onActionBar)
                }
            }
        }
    }
}

@Composable
private fun ReportWhileShown(report: (Boolean) -> Unit) {
    val latest by rememberUpdatedState(report)
    DisposableEffect(Unit) {
        latest(true)
        onDispose { latest(false) }
    }
}

@Composable
internal fun ArticleLoadFailure(error: String?, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    QuietState(
        title = "This article did not load",
        message = error ?: "Check your connection and try again.",
        modifier = modifier,
        icon = EarnestIcons.Offline,
        tone = LocalTones.current.amber,
        actionLabel = "Try again",
        onAction = onRetry,
    )
}

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
    articleActions: List<CustomAccessibilityAction> = emptyList(),
    titleModifier: Modifier = Modifier,
    sourceIcon: String? = null,
    chrome: ReaderChrome = ReaderChrome.FULL,
    shelf: ReaderShelf? = null,
    onBack: (() -> Unit)? = null,
    appOptions: AppOptions? = null,
) {
    val scheme = MaterialTheme.colorScheme
    val title = entry.title.ifBlank { "Untitled" }
    val titleSemantics = Modifier.semantics {
        heading()
        customActions = articleActions
    }
    Column(modifier.fillMaxWidth()) {
        Row(
            Modifier.fillMaxWidth().heightIn(min = 56.dp).padding(horizontal = ReaderSpace.xxs),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            onBack?.let { ActionIconButton(EarnestIcons.Back, "Back", it) }
            Column(Modifier.weight(1f).padding(horizontal = ReaderSpace.sm)) {
                if (onBack != null && chrome == ReaderChrome.FULL) {
                    Text("Article", style = MaterialTheme.typography.labelLarge, color = scheme.onSurfaceVariant, maxLines = 1)
                }
                if (chrome == ReaderChrome.SHORT) {
                    Text(
                        title,
                        style = MaterialTheme.typography.titleMedium,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = titleSemantics,
                    )
                    ShortByline(entry, source, offlineCopy)
                }
            }
            ActionIconButton(EarnestIcons.Search, "Find in article", onFind)
            ReaderMenu(
                canOpen = originalUrl(entry) != null,
                canUnread = isRead,
                // A short window has no action bar, so its actions move in here.
                shelf = shelf.takeIf { chrome == ReaderChrome.SHORT },
                onOpenOriginal = onOpenOriginal,
                onShare = onShare,
                onCopyLink = onCopyLink,
                onMarkUnread = onMarkUnread,
                onSmallerText = onSmallerText,
                onLargerText = onLargerText,
                appOptions = appOptions,
            )
        }
        if (chrome == ReaderChrome.FULL) {
            Column(
                titleModifier.align(Alignment.CenterHorizontally).widthIn(max = ReaderSpace.contentWidth).fillMaxWidth()
                    .padding(start = ReaderSpace.md, end = ReaderSpace.md, bottom = ReaderSpace.sm),
                verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
            ) {
                Text(
                    title,
                    style = MaterialTheme.typography.headlineSmall,
                    maxLines = TITLE_LINES,
                    overflow = TextOverflow.Ellipsis,
                    modifier = titleSemantics,
                )
                ArticleByline(entry, source, sourceIcon, offlineCopy)
            }
        }
    }
}

@Composable
private fun ArticleByline(entry: CachedEntry, source: String, sourceIcon: String?, offlineCopy: Boolean) {
    val meta = articleOrigin(entry, source) + listOfNotNull(CACHED_COPY.takeIf { offlineCopy })
    if (meta.isEmpty()) return
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
        if (source.isNotBlank()) FeedAvatar(source, sourceIcon, size = SourceMarkSize)
        Text(
            meta.joinToString(" · "),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f, fill = false),
        )
    }
}

/** One line for a short window, with the cached copy first so that it is never cut off. */
@Composable
private fun ShortByline(entry: CachedEntry, source: String, offlineCopy: Boolean) {
    val meta = listOfNotNull(CACHED_COPY.takeIf { offlineCopy }) + articleOrigin(entry, source)
    if (meta.isEmpty()) return
    Text(
        meta.joinToString(" · "),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
    )
}

private fun articleOrigin(entry: CachedEntry, source: String): List<String> =
    listOf(source, entryDateLabel(entry)).filter { it.isNotBlank() }

@Composable
private fun ReaderMenu(
    canOpen: Boolean,
    canUnread: Boolean,
    shelf: ReaderShelf?,
    onOpenOriginal: () -> Unit,
    onShare: () -> Unit,
    onCopyLink: () -> Unit,
    onMarkUnread: () -> Unit,
    onSmallerText: () -> Unit,
    onLargerText: () -> Unit,
    appOptions: AppOptions?,
) {
    var open by remember { mutableStateOf(false) }
    var choosingTheme by remember { mutableStateOf(false) }
    fun choose(action: () -> Unit): () -> Unit = {
        open = false
        action()
    }
    Box {
        ActionIconButton(EarnestIcons.More, "Reader options", { open = true })
        AppMenu(open, { open = false }) {
            shelf?.let {
                AppMenuItem(it.keepLabel, choose(it.onToggleKept), if (it.kept) EarnestIcons.KeptSelected else EarnestIcons.Kept)
                AppMenuItem("Later", choose(it.onLater), EarnestIcons.Later, enabled = !it.later)
                AppMenuItem("Done", choose(it.onDone), EarnestIcons.Done, enabled = !it.done)
                HorizontalDivider()
            }
            if (canOpen) {
                AppMenuItem("Share", choose(onShare), EarnestIcons.Share)
                AppMenuItem("Copy link", choose(onCopyLink), EarnestIcons.Copy)
                AppMenuItem("Open original", choose(onOpenOriginal), EarnestIcons.External)
                HorizontalDivider()
            }
            AppMenuItem("Smaller text", choose(onSmallerText), EarnestIcons.TextSmaller)
            AppMenuItem("Larger text", choose(onLargerText), EarnestIcons.TextLarger)
            if (canUnread) AppMenuItem("Mark unread", choose(onMarkUnread), EarnestIcons.MarkUnread)
            appOptions?.let {
                HorizontalDivider()
                AppOptionItems(it, dismiss = { open = false }, chooseTheme = { choosingTheme = true })
            }
        }
    }
    if (choosingTheme) appOptions?.let { AppThemeChooser(it) { choosingTheme = false } }
}

@Composable
private fun ReaderActionBar(shelf: ReaderShelf, canOpenOriginal: Boolean, onOpenOriginal: () -> Unit) {
    val tones = LocalTones.current
    val density = LocalDensity.current
    val labels = Density(density.density, density.fontScale.coerceAtMost(BAR_LABEL_MAX_FONT_SCALE))
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        CompositionLocalProvider(LocalDensity provides labels) {
            Row(Modifier.widthIn(max = ReaderSpace.formWidth).fillMaxWidth()) {
                val item = Modifier.weight(1f)
                BarAction(
                    label = if (shelf.kept) "Kept" else "Keep",
                    icon = if (shelf.kept) EarnestIcons.KeptSelected else EarnestIcons.Kept,
                    active = shelf.kept,
                    tone = tones.plum,
                    onClick = shelf.onToggleKept,
                    modifier = item,
                )
                BarAction(
                    label = "Later",
                    icon = if (shelf.later) EarnestIcons.LaterSelected else EarnestIcons.Later,
                    active = shelf.later,
                    tone = tones.amber,
                    onClick = shelf.onLater,
                    modifier = item,
                    enabled = !shelf.later,
                )
                BarAction(
                    label = "Done",
                    icon = if (shelf.done) EarnestIcons.DoneSelected else EarnestIcons.Done,
                    active = shelf.done,
                    tone = tones.green,
                    onClick = shelf.onDone,
                    modifier = item,
                    enabled = !shelf.done,
                )
                BarAction(
                    label = "Open original",
                    icon = EarnestIcons.External,
                    active = false,
                    tone = tones.blue,
                    onClick = onOpenOriginal,
                    modifier = item,
                    enabled = canOpenOriginal,
                )
            }
        }
    }
}

private const val DISABLED_INK = 0.38f

@Composable
private fun BarAction(
    label: String,
    icon: ImageVector,
    active: Boolean,
    tone: Color,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val scheme = MaterialTheme.colorScheme
    val colors = toneColors(scheme, tone)
    val ink = when {
        active -> scheme.onSurface
        enabled -> scheme.onSurfaceVariant
        else -> scheme.onSurface.copy(alpha = DISABLED_INK)
    }
    Column(
        modifier.heightIn(min = ActionBarHeight)
            .selectable(selected = active, enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = ReaderSpace.xxs, vertical = ReaderSpace.xs),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
    ) {
        Surface(
            shape = CircleShape,
            color = if (active) colors.container else Color.Transparent,
            contentColor = if (active) colors.ink else ink,
        ) {
            Box(Modifier.size(ActionIndicatorWidth, ActionIndicatorHeight), contentAlignment = Alignment.Center) {
                Icon(icon, contentDescription = null)
            }
        }
        Text(label, style = MaterialTheme.typography.labelMedium, color = ink, textAlign = TextAlign.Center, maxLines = 2)
    }
}

@Composable
internal fun FindBar(
    query: String,
    label: String,
    onQuery: (String) -> Unit,
    onPrevious: () -> Unit,
    onNext: () -> Unit,
    onClose: () -> Unit,
) {
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }
    val scheme = MaterialTheme.colorScheme
    val canStep = query.isNotBlank() && label.isNotEmpty() && label != NO_MATCHES
    Column(Modifier.fillMaxWidth().padding(horizontal = ReaderSpace.xxs, vertical = ReaderSpace.xxs)) {
        TextField(
            value = query,
            onValueChange = onQuery,
            placeholder = { Text("Find in article", maxLines = 1, overflow = TextOverflow.Ellipsis) },
            leadingIcon = { ActionIconButton(EarnestIcons.Back, "Close find", onClose) },
            singleLine = true,
            shape = CircleShape,
            colors = TextFieldDefaults.colors(
                focusedContainerColor = scheme.surfaceContainerHigh,
                unfocusedContainerColor = scheme.surfaceContainerHigh,
                focusedIndicatorColor = Color.Transparent,
                unfocusedIndicatorColor = Color.Transparent,
            ),
            modifier = Modifier.fillMaxWidth().padding(horizontal = ReaderSpace.xs).focusRequester(focus),
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
            keyboardActions = KeyboardActions(onSearch = { onNext() }),
        )
        Row(
            Modifier.fillMaxWidth().padding(start = ReaderSpace.gutter, end = ReaderSpace.xxs),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                label,
                style = MaterialTheme.typography.labelLarge,
                color = scheme.onSurfaceVariant,
                modifier = Modifier.weight(1f).semantics { liveRegion = LiveRegionMode.Polite },
            )
            ActionIconButton(EarnestIcons.ChevronUp, "Previous match", onPrevious, enabled = canStep)
            ActionIconButton(EarnestIcons.ChevronDown, "Next match", onNext, enabled = canStep)
        }
    }
}

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

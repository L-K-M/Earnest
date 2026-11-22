package ch.lkmc.earnest

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationDrawerItemColors
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject
import ch.lkmc.earnest.core.UnreadCounts
import ch.lkmc.earnest.core.iconUrl

/** Four destinations; the drawer keeps every shelf one tap away as well. */
internal val primaryDestinations = listOf(View.FRONT, View.ALL, View.KEPT, View.FEEDS)

/** The shelves behind the Saved destination, which opens on Kept. */
internal val savedShelves = listOf(View.KEPT, View.LATER, View.DONE)

internal val View.navigationLabel: String
    get() = when (this) {
        View.ALL -> "Articles"
        View.KEPT -> "Saved"
        else -> label
    }

/** The destination that stands for this view; every saved shelf is Saved. */
internal val View.primary: View get() = if (this in savedShelves) View.KEPT else this

internal fun View.navigationIcon(selected: Boolean): ImageVector = when {
    this == View.KEPT && selected -> EarnestIcons.SavedSelected
    this == View.KEPT -> EarnestIcons.Saved
    selected -> selectedIcon
    else -> icon
}

internal fun screenTitle(view: View, selection: FeedSelection, objects: List<SyncObject>): ScreenTitle = when (view) {
    View.ALL -> ScreenTitle(selection.title(objects), (selection as? FeedSelection.Folder)?.let { folderSummary(it, objects) })
    View.KEPT, View.LATER, View.DONE -> ScreenTitle(View.KEPT.navigationLabel)
    View.FEEDS -> ScreenTitle(view.label, subscriptionSummary(objects))
    else -> ScreenTitle(view.label)
}

private fun folderSummary(folder: FeedSelection.Folder, objects: List<SyncObject>): String? {
    val key = if (folder.id == 0L) "unfiled" else "folder-${folder.id}"
    val group = navigationFeedGroups(objects).firstOrNull { it.key == key } ?: return null
    return "Folder · ${feedCount(group.subscriptions.size)}"
}

private fun subscriptionSummary(objects: List<SyncObject>): String? {
    val feeds = objects.count { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }
    if (feeds == 0) return null
    val folders = objects.count { it.kind == SyncEngine.OBJECT_FOLDER && !it.deleted }
    if (folders == 0) return feedCount(feeds)
    return "${feedCount(feeds)} in $folders ${if (folders == 1) "folder" else "folders"}"
}

internal fun feedCount(count: Int): String = "$count ${if (count == 1) "feed" else "feeds"}"

/** Folder and subscription IDs are different namespaces, including after restoration. */
internal sealed interface FeedSelection {
    data object All : FeedSelection
    data class Folder(val id: Long) : FeedSelection
    data class Subscription(val id: Long) : FeedSelection
}

internal fun FeedSelection.title(objects: List<SyncObject>): String = when (this) {
    FeedSelection.All -> View.ALL.label
    is FeedSelection.Folder -> if (id == 0L) "No folder" else objects.firstOrNull {
        it.kind == SyncEngine.OBJECT_FOLDER && it.id == id && !it.deleted
    }?.name ?: "Folder unavailable"
    is FeedSelection.Subscription -> objects.firstOrNull {
        it.kind == SyncEngine.OBJECT_SUBSCRIPTION && it.id == id && !it.deleted
    }?.name ?: "Feed unavailable"
}

/** Keep the parent list's ordering and shelf rules while narrowing its sources. */
internal fun FeedSelection.filter(entries: List<CachedEntry>, objects: List<SyncObject>): List<CachedEntry> {
    if (this == FeedSelection.All) return entries
    val folders = objects.filter { it.kind == SyncEngine.OBJECT_FOLDER && !it.deleted }.map { it.id }.toSet()
    val subscriptions = objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }
    val ids = when (this) {
        FeedSelection.All -> error("All sources returned above")
        is FeedSelection.Subscription -> subscriptions.filter { it.id == id }
        is FeedSelection.Folder -> when {
            id == 0L -> subscriptions.filter { it.folderId == 0L || it.folderId !in folders }
            id in folders -> subscriptions.filter { it.folderId == id }
            else -> emptyList()
        }
    }.map { it.id }.toSet()
    return entries.filter { it.subscriptionId in ids }
}

internal val FeedSelectionSaver: Saver<MutableState<FeedSelection>, String> = Saver(
    save = { state ->
        when (val selection = state.value) {
            FeedSelection.All -> "all"
            is FeedSelection.Folder -> "folder:${selection.id}"
            is FeedSelection.Subscription -> "feed:${selection.id}"
        }
    },
    restore = { encoded ->
        val id = encoded.substringAfter(':', "").toLongOrNull()?.takeIf { it >= 0 }
        val selection = when {
            encoded.startsWith("folder:") && id != null -> FeedSelection.Folder(id)
            encoded.startsWith("feed:") && id != null && id > 0 -> FeedSelection.Subscription(id)
            else -> FeedSelection.All
        }
        mutableStateOf(selection)
    },
)

@Composable
internal fun rememberFeedSelection(accountKey: String?): MutableState<FeedSelection> {
    var owner by rememberSaveable { mutableStateOf(accountKey) }
    val selection = rememberSaveable(saver = FeedSelectionSaver) { mutableStateOf<FeedSelection>(FeedSelection.All) }
    // Cold launches restore navigation before the asynchronous account cache.
    // A transient null owner must not erase the restored folder.
    if (accountKey != null && owner != accountKey) {
        if (owner != null) selection.value = FeedSelection.All
        owner = accountKey
    }
    return selection
}

/** Includes empty folders, so an empty group never vanishes from navigation. */
internal fun navigationFeedGroups(objects: List<SyncObject>): List<FeedGroup> {
    val subscriptions = objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }
    val folders = objects.filter { it.kind == SyncEngine.OBJECT_FOLDER && !it.deleted }
        .sortedWith(compareBy(String.CASE_INSENSITIVE_ORDER) { item: SyncObject -> item.name }.thenBy { it.id })
    val folderIds = folders.map { it.id }.toSet()
    val unfiled = subscriptions.filter { it.folderId == 0L || it.folderId !in folderIds }
    val byFolder = subscriptions.groupBy { it.folderId }
    return buildList {
        if (unfiled.isNotEmpty()) add(FeedGroup("unfiled", "No folder", unfiled))
        folders.forEach { add(FeedGroup("folder-${it.id}", it.name, byFolder[it.id].orEmpty())) }
    }
}

/**
 * A folder's unread total is the sum of its feeds' exact counts, as on the
 * web sidebar. Feeds missing from [counts] contribute nothing.
 */
internal fun FeedGroup.unreadCount(counts: UnreadCounts): Long =
    subscriptions.sumOf { counts.bySubscription[it.id] ?: 0L }

/** Matches the web sidebar: four digits would crowd a phone-width drawer. */
internal fun unreadBadgeText(count: Long): String = if (count > MAX_BADGE_COUNT) "$MAX_BADGE_COUNT+" else count.toString()

private const val MAX_BADGE_COUNT = 999L

@Composable
private fun UnreadBadge(count: Long?) {
    if (count == null || count <= 0L) return
    // No colour of its own: a selected row may be dark on e-paper.
    Text(
        unreadBadgeText(count),
        style = MaterialTheme.typography.labelLarge,
        modifier = Modifier.semantics { contentDescription = "$count unread" },
    )
}

private val DrawerWidth = 336.dp
private val DrawerEdge = ReaderSpace.sm
private val DrawerText = ReaderSpace.sm + ReaderSpace.md
private val DrawerFeedMark = 24.dp
private val BrandMark = 40.dp

@Composable
internal fun ReaderDrawer(
    view: View,
    selection: FeedSelection,
    objects: List<SyncObject>,
    onView: (View) -> Unit,
    onFeed: (FeedSelection) -> Unit,
    onSettings: () -> Unit,
    onClose: () -> Unit = {},
    // Null outside unread-count mode, so river readers never see counts.
    unreadCounts: UnreadCounts? = null,
) {
    val groups = remember(objects) { navigationFeedGroups(objects) }
    var expanded by rememberSaveable { mutableStateOf(emptyList<String>()) }
    ModalDrawerSheet(Modifier.widthIn(max = DrawerWidth)) {
        LazyColumn(
            Modifier.testTag("reader-drawer").weight(1f),
            contentPadding = PaddingValues(bottom = ReaderSpace.xs),
            verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
        ) {
            item(key = "header") { DrawerHeader(onClose) }
            item(key = "view-${View.ALL.name}") {
                DrawerPlace(
                    place = View.ALL,
                    selected = view == View.ALL && selection == FeedSelection.All,
                    unread = unreadCounts?.total,
                    onClick = { onFeed(FeedSelection.All) },
                )
            }
            item(key = "feeds-heading") {
                SectionHeader(
                    "Folders & feeds",
                    Modifier.padding(start = DrawerText, end = DrawerEdge, top = ReaderSpace.sm),
                    action = { QuietButton("Manage", onClick = { onView(View.FEEDS) }) },
                )
            }
            if (groups.isEmpty()) {
                item(key = "no-feeds") {
                    Text(
                        "No feeds yet. Add a feed or import OPML in Feeds.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(horizontal = DrawerText, vertical = ReaderSpace.xs),
                    )
                }
            }
            groups.forEach { group ->
                val open = group.key in expanded
                val folder = FeedSelection.Folder(group.key.removePrefix("folder-").toLongOrNull() ?: 0L)
                item(key = group.key) {
                    DrawerFolder(
                        group = group,
                        selected = view == View.ALL && selection == folder,
                        open = open,
                        unread = unreadCounts?.let { group.unreadCount(it) },
                        onClick = { onFeed(folder) },
                        onToggle = { expanded = if (open) expanded - group.key else expanded + group.key },
                    )
                }
                if (!open) return@forEach
                items(group.subscriptions.sortedBy { it.name.lowercase() }, key = { "feed-${it.id}" }) { subscription ->
                    val feed = FeedSelection.Subscription(subscription.id)
                    DrawerFeed(
                        subscription = subscription,
                        selected = view == View.ALL && selection == feed,
                        unread = unreadCounts?.bySubscription?.get(subscription.id),
                        onClick = { onFeed(feed) },
                        modifier = Modifier.animateItem(),
                    )
                }
            }
            item(key = "places-heading") {
                SectionHeader("Saved & activity", Modifier.padding(horizontal = DrawerText).padding(top = ReaderSpace.sm))
            }
            items(savedShelves + View.ACTIVITY, key = { "view-${it.name}" }) { place ->
                DrawerPlace(place, selected = view == place, onClick = { onView(place) })
            }
        }
        // Settings stays put however long the folder list grows.
        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        NavigationDrawerItem(
            label = { Text("Settings") },
            selected = false,
            icon = { Icon(EarnestIcons.Settings, contentDescription = null) },
            onClick = onSettings,
            modifier = Modifier.padding(horizontal = DrawerEdge, vertical = ReaderSpace.xs),
        )
    }
}

@Composable
private fun DrawerHeader(onClose: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(start = DrawerText, end = DrawerEdge, top = ReaderSpace.sm, bottom = ReaderSpace.md),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
    ) {
        Icon(
            painterResource(R.drawable.ic_notification),
            contentDescription = null,
            tint = toneColors(MaterialTheme.colorScheme, LocalTones.current.orange).ink,
            modifier = Modifier.size(BrandMark),
        )
        Column(Modifier.weight(1f)) {
            Text("Your library", style = MaterialTheme.typography.headlineSmall, modifier = Modifier.semantics { heading() })
            Text(
                "Read by folder or feed",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        ActionIconButton(EarnestIcons.Close, "Close folders", onClose)
    }
}

/** A place in its own tone, the one it lights up in on the navigation bar. */
@Composable
private fun DrawerPlace(place: View, selected: Boolean, onClick: () -> Unit, unread: Long? = null) {
    NavigationDrawerItem(
        label = { Text(place.label, maxLines = 1, overflow = TextOverflow.Ellipsis) },
        selected = selected,
        icon = { Icon(if (selected) place.selectedIcon else place.icon, contentDescription = null) },
        badge = { UnreadBadge(unread) },
        colors = tonedItemColors(place.tone?.invoke(LocalTones.current)),
        onClick = onClick,
        modifier = Modifier.padding(horizontal = DrawerEdge),
    )
}

@Composable
private fun DrawerFolder(
    group: FeedGroup,
    selected: Boolean,
    open: Boolean,
    unread: Long?,
    onClick: () -> Unit,
    onToggle: () -> Unit,
) {
    val secondary = MaterialTheme.colorScheme.onSurfaceVariant
    NavigationDrawerItem(
        label = {
            Column {
                Text(group.title.orEmpty(), maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    feedCount(group.subscriptions.size),
                    style = MaterialTheme.typography.bodySmall,
                    color = if (selected) Color.Unspecified else secondary,
                )
            }
        },
        selected = selected,
        icon = { Icon(if (selected) EarnestIcons.FolderSelected else EarnestIcons.Folder, contentDescription = null) },
        badge = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                UnreadBadge(unread)
                IconButton(onClick = onToggle) {
                    Icon(
                        if (open) EarnestIcons.ChevronUp else EarnestIcons.ChevronDown,
                        contentDescription = "${if (open) "Collapse" else "Expand"} ${group.title}",
                    )
                }
            }
        },
        colors = tonedItemColors(LocalTones.current.green),
        onClick = onClick,
        modifier = Modifier.padding(horizontal = DrawerEdge),
    )
}

@Composable
private fun DrawerFeed(subscription: SyncObject, selected: Boolean, unread: Long?, onClick: () -> Unit, modifier: Modifier) {
    val iconUrl = remember(subscription.raw) { subscription.iconUrl() }
    NavigationDrawerItem(
        label = { Text(subscription.name.ifBlank { "Untitled feed" }, maxLines = 2, overflow = TextOverflow.Ellipsis) },
        selected = selected,
        icon = { FeedAvatar(subscription.name, iconUrl, size = DrawerFeedMark) },
        badge = { UnreadBadge(unread) },
        onClick = onClick,
        modifier = modifier.padding(start = DrawerEdge + ReaderSpace.md, end = DrawerEdge),
    )
}

@Composable
private fun tonedItemColors(tone: Color?): NavigationDrawerItemColors {
    val scheme = MaterialTheme.colorScheme
    val ink = tone?.let { toneColors(scheme, it).ink } ?: scheme.onSurfaceVariant
    return NavigationDrawerItemDefaults.colors(unselectedIconColor = ink)
}

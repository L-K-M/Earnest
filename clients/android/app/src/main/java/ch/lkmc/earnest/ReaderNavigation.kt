package ch.lkmc.earnest

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.path
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject

/** Secondary shelves remain in the drawer, not seven tiny phone tabs. */
internal val primaryDestinations = listOf(View.FRONT, View.ALL, View.KEPT, View.FEEDS)

internal val View.navigationLabel: String get() = if (this == View.ALL) "Articles" else label

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

// A simple folder outline, kept local rather than adding the extended icon pack.
internal val FolderIcon: ImageVector = ImageVector.Builder(
    name = "ReaderFolder", defaultWidth = 24.dp, defaultHeight = 24.dp,
    viewportWidth = 24f, viewportHeight = 24f,
).apply {
    path(fill = SolidColor(Color.Black)) {
        moveTo(3f, 5f)
        lineTo(10f, 5f)
        lineTo(12f, 7f)
        lineTo(21f, 7f)
        lineTo(21f, 20f)
        lineTo(3f, 20f)
        close()
        moveTo(5f, 9f)
        lineTo(5f, 18f)
        lineTo(19f, 18f)
        lineTo(19f, 9f)
        close()
    }
}.build()

@Composable
internal fun ReaderDrawer(
    view: View,
    selection: FeedSelection,
    objects: List<SyncObject>,
    onView: (View) -> Unit,
    onFeed: (FeedSelection) -> Unit,
    onSettings: () -> Unit,
    onClose: () -> Unit = {},
) {
    val groups = navigationFeedGroups(objects)
    var expanded by rememberSaveable { mutableStateOf(emptyList<String>()) }
    ModalDrawerSheet(modifier = Modifier.widthIn(max = 320.dp)) {
        LazyColumn(modifier = Modifier.testTag("reader-drawer"), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            item {
                Row(Modifier.padding(start = 28.dp, end = 12.dp, top = 12.dp, bottom = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("Your library", style = MaterialTheme.typography.titleLarge)
                        Text("Read by folder or feed", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    IconButton(onClick = onClose) { Icon(Icons.Filled.Close, contentDescription = "Close folders") }
                }
            }
            items(listOf(View.ALL), key = { "view-${it.name}" }) { destination ->
                NavigationDrawerItem(
                    label = { Text(destination.label, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    selected = view == destination && (destination != View.ALL || selection == FeedSelection.All),
                    icon = { Icon(destination.icon, contentDescription = null) },
                    onClick = { onView(destination) },
                    modifier = Modifier.padding(horizontal = 12.dp),
                )
            }
            item {
                HorizontalDivider(Modifier.padding(horizontal = 28.dp, vertical = 12.dp))
                Row(Modifier.fillMaxWidth().padding(start = 28.dp, end = 16.dp)) {
                    Text("Folders & feeds", style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f).padding(vertical = 14.dp))
                    TextButton(onClick = { onView(View.FEEDS) }) { Text("Manage") }
                }
            }
            if (groups.isEmpty()) {
                item {
                    Text(
                        "No feeds yet. Add a feed or import OPML in Feeds.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(horizontal = 28.dp, vertical = 12.dp),
                    )
                }
            }
            groups.forEach { group ->
                val folderId = group.key.removePrefix("folder-").toLongOrNull() ?: 0L
                val folder = FeedSelection.Folder(folderId)
                item(key = group.key) {
                    NavigationDrawerItem(
                        label = {
                            Column {
                                Text(group.title.orEmpty(), maxLines = 1, overflow = TextOverflow.Ellipsis)
                                Text(
                                    "${group.subscriptions.size} ${if (group.subscriptions.size == 1) "feed" else "feeds"}",
                                    style = MaterialTheme.typography.labelSmall,
                                )
                            }
                        },
                        selected = view == View.ALL && selection == folder,
                        icon = { Icon(FolderIcon, contentDescription = null) },
                        badge = {
                            IconButton(onClick = {
                                expanded = if (group.key in expanded) expanded - group.key else expanded + group.key
                            }) {
                                Icon(
                                    if (group.key in expanded) Icons.Filled.KeyboardArrowUp else Icons.Filled.KeyboardArrowDown,
                                    contentDescription = "${if (group.key in expanded) "Collapse" else "Expand"} ${group.title}",
                                )
                            }
                        },
                        onClick = { onFeed(folder) },
                        modifier = Modifier.padding(horizontal = 12.dp),
                    )
                }
                if (group.key in expanded) {
                    items(group.subscriptions.sortedBy { it.name.lowercase() }, key = { "feed-${it.id}" }) { subscription ->
                        NavigationDrawerItem(
                            label = { Text(subscription.name, maxLines = 2, overflow = TextOverflow.Ellipsis) },
                            selected = view == View.ALL && selection == FeedSelection.Subscription(subscription.id),
                            icon = { Icon(Icons.AutoMirrored.Filled.List, contentDescription = null, modifier = Modifier.size(20.dp)) },
                            onClick = { onFeed(FeedSelection.Subscription(subscription.id)) },
                            modifier = Modifier.padding(start = 28.dp, end = 12.dp),
                        )
                    }
                }
            }
            item {
                HorizontalDivider(Modifier.padding(horizontal = 28.dp, vertical = 12.dp))
                Text("Saved & activity", style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(horizontal = 28.dp, vertical = 12.dp))
            }
            items(listOf(View.LATER, View.KEPT, View.DONE, View.ACTIVITY), key = { "view-${it.name}" }) { destination ->
                NavigationDrawerItem(
                    label = { Text(destination.label) },
                    selected = view == destination,
                    icon = { Icon(destination.icon, contentDescription = null) },
                    onClick = { onView(destination) },
                    modifier = Modifier.padding(horizontal = 12.dp),
                )
            }
            item {
                HorizontalDivider(Modifier.padding(horizontal = 28.dp, vertical = 12.dp))
                TextButton(onClick = onSettings, modifier = Modifier.padding(start = 16.dp, bottom = 16.dp)) { Text("Settings") }
            }
        }
    }
}

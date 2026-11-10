package ch.lkmc.earnest

import ch.lkmc.earnest.core.FeedHealth
import ch.lkmc.earnest.core.SyncObject

/** A folder section of the Feeds list; the unfiled section has no title when the account has no folders. */
internal data class FeedGroup(val key: String, val title: String?, val subscriptions: List<SyncObject>)

/**
 * Groups subscriptions by folder for the Feeds list: the unfiled ones first,
 * then each folder by name. A subscription whose folder is gone is unfiled.
 * Without folders the list stays flat. Keys are stable per folder id, so a
 * folder named "No folder" or two folders sharing a name stay their own
 * sections.
 */
internal fun feedGroups(subscriptions: List<SyncObject>, folders: List<SyncObject>): List<FeedGroup> {
    val byId = folders.associateBy { it.id }
    if (byId.isEmpty()) return listOf(FeedGroup("unfiled", null, subscriptions))
    val unfiled = subscriptions.filter { it.folderId == 0L || it.folderId !in byId }
    val grouped = subscriptions.filter { it.folderId != 0L && it.folderId in byId }
        .groupBy { it.folderId }
    return (listOf(FeedGroup("unfiled", "No folder", unfiled)) +
        grouped.entries
            .sortedWith(compareBy(String.CASE_INSENSITIVE_ORDER) { byId.getValue(it.key).name })
            .map { (id, rows) -> FeedGroup("folder-$id", byId.getValue(id).name, rows) })
        .filter { it.subscriptions.isNotEmpty() }
}

/** What a failing fetch reports, or null for a feed that fetches fine. */
internal data class FeedProblem(val detail: String, val consecutiveFailures: Long, val triedAt: String?)

/**
 * The server clears the error fields on the fetch that succeeds
 * (internal/service/fetch.go), so they linger only while the feed is failing
 * and a problem is reported whenever they are present.
 */
internal fun feedProblem(health: FeedHealth?): FeedProblem? {
    health ?: return null
    val detail = health.errorMessage ?: health.errorClass
    if (detail == null && health.consecutiveFailures <= 0L) return null
    return FeedProblem(detail ?: "unknown error", health.consecutiveFailures, health.lastAttemptAt)
}

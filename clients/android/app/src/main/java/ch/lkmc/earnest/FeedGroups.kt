package ch.lkmc.earnest

import ch.lkmc.earnest.core.FeedHealth
import ch.lkmc.earnest.core.SyncObject

/** A folder section of the Feeds list; the unfiled section has no title when the account has no folders. */
internal data class FeedGroup(val title: String?, val subscriptions: List<SyncObject>)

/**
 * Groups subscriptions by folder for the Feeds list: the unfiled ones first,
 * then each folder by name. A subscription whose folder is gone is unfiled.
 * Without folders the list stays flat.
 */
internal fun feedGroups(subscriptions: List<SyncObject>, folders: List<SyncObject>): List<FeedGroup> {
    val names = folders.associate { it.id to it.name }
    if (names.isEmpty()) return listOf(FeedGroup(null, subscriptions))
    val unfiled = subscriptions.filter { it.folderId == 0L || it.folderId !in names }
    val grouped = subscriptions.filter { it.folderId != 0L && it.folderId in names }
        .groupBy { names.getValue(it.folderId) }
    return (listOf(FeedGroup("No folder", unfiled)) +
        grouped.toSortedMap().map { (name, rows) -> FeedGroup(name, rows) })
        .filter { it.subscriptions.isNotEmpty() }
}

/** What a failing fetch reports, or null for a feed that fetches fine. */
internal data class FeedProblem(val detail: String, val consecutiveFailures: Long, val triedAt: String?)

internal fun feedProblem(health: FeedHealth?): FeedProblem? {
    health ?: return null
    val detail = health.errorMessage ?: health.errorClass
    if (detail == null && health.consecutiveFailures <= 0L) return null
    return FeedProblem(detail ?: "unknown error", health.consecutiveFailures, health.lastAttemptAt)
}

package ch.lkmc.earnest

import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.River
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.core.State
import kotlinx.serialization.json.Json
import java.time.Instant

/**
 * Every article, newest first. The All articles screen uses the same slice
 * as the API's `view=all&direction=backward`; the reader actions are shared
 * with the other lists.
 */
internal fun allArticles(entries: List<CachedEntry>): List<CachedEntry> =
    entries.filterNot { it.deleted }.sortedByDescending { it.seq }

// A background cache/sync reload must not turn a seen change into removal.
// An intentional reload is fenced to the visit that requested it; a late
// response must not prune articles read in a subsequently chosen list.
internal fun UiState.riverMembersAfter(fresh: UiState, intentionalVisit: Long? = null): Set<Long> {
    if (!fresh.readerSettings.showOnlyUnread || fresh.snapshot.cursor.generation != snapshot.cursor.generation ||
        fresh.principalId != principalId || intentionalVisit == listVisit) return fresh.riverEntryIds
    return riverEntryIds + fresh.riverEntryIds
}

internal fun restoredState(engine: SyncEngine, principal: Long, now: Instant = Instant.now()): UiState {
    val snapshot = engine.snapshot()
    val states = engine.states(principal)
    val cadences = snapshot.objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }
        .associate { it.id to River.cadence(it) }
    val members = initialRiverIds(snapshot, states, now)
    val settings = snapshot.objects.firstOrNull { it.kind == River.SETTINGS && !it.deleted }
    val readerSettings = settings?.let {
        runCatching { Json { ignoreUnknownKeys = true }.decodeFromString(ReaderSettings.serializer(), it.raw) }.getOrNull()
    }
    return UiState(
        snapshot = snapshot,
        principalId = principal,
        states = states,
        cadences = cadences,
        riverEntryIds = members,
        readerSettings = readerSettings ?: ReaderSettings(),
        readerSettingsRev = settings?.rev ?: 0L,
        readerSettingsUi = ReaderSettingsUi(loaded = readerSettings, loadedRev = settings?.rev ?: 0L),
    )
}

internal fun initialRiverIds(
    snapshot: SyncSnapshot,
    states: Map<Long, State>,
    now: Instant = Instant.now(),
): Set<Long> {
    val subscriptions = snapshot.objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION }.associateBy { it.id }
    val resources = snapshot.objects.filter { it.kind == River.MUTE || it.kind == River.SETTINGS }
    return snapshot.entries.filter {
        River.contains(it, states[it.id] ?: SyncEngine.initialState(0, it.id),
            subscriptions[it.subscriptionId], resources, now)
    }.map { it.id }.toSet()
}

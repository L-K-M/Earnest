package ch.lkmc.earnest

import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.River
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.core.State
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

/** Session members survive passive seen writes, while explicit shelf actions remove rows. */
internal fun todayEntries(state: UiState): List<CachedEntry> = sessionEntries(state).filter { state.cadences[it.subscriptionId] != "week" }

internal fun editionEntries(state: UiState): List<CachedEntry> = sessionEntries(state).filter { state.cadences[it.subscriptionId] == "week" }

private fun sessionEntries(state: UiState): List<CachedEntry> = state.snapshot.entries.filter {
    !it.deleted && it.id in state.riverEntryIds &&
        (state.states[it.id] ?: SyncEngine.initialState(state.principalId, it.id)).shelf.value == JsonPrimitive(SyncEngine.VALUE_NONE)
}.sortedBy { it.seq }

internal fun restoredState(engine: SyncEngine, principal: Long, now: Instant = Instant.now()): UiState {
    val snapshot = engine.snapshot()
    val states = engine.states(principal)
    val cadences = snapshot.objects.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }
        .associate { it.id to River.cadence(it) }
    val members = initialRiverIds(snapshot, states, now)
    val settings = snapshot.objects.firstOrNull { it.kind == River.SETTINGS && !it.deleted }
    val readerSettings = settings?.let {
        runCatching { Json { ignoreUnknownKeys = true }.decodeFromString(ReaderSettings.serializer(), it.raw) }.getOrNull()
    } ?: ReaderSettings()
    return UiState(
        snapshot = snapshot,
        principalId = principal,
        states = states,
        cadences = cadences,
        riverEntryIds = members,
        readerSettings = readerSettings,
        readerSettingsRev = settings?.rev ?: 0L,
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

package ch.lkmc.earnest

import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.SyncEngine
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/** Cached membership is independent of passive seen overlays during a session. */
internal fun todayEntries(state: UiState): List<CachedEntry> = state.snapshot.entries.filter {
    !it.deleted && state.cadences[it.subscriptionId] in listOf("now", "today") &&
        (state.states[it.id] ?: SyncEngine.initialState(state.principalId, it.id)).let { effective ->
            it.id in state.todayEntryIds && effective.shelf.value == JsonPrimitive(SyncEngine.VALUE_NONE)
        }
}.sortedBy { it.seq }

internal fun restoredState(engine: SyncEngine, principal: Long): UiState = UiState(
    snapshot = engine.snapshot(),
    principalId = principal,
    states = engine.snapshot().entries.filterNot { it.deleted }.associate { it.id to engine.state(principal, it.id) },
    cadences = engine.snapshot().objects
        .filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted && it.raw.isNotEmpty() }
        .associate { obj ->
            val cadence = runCatching { Json.parseToJsonElement(obj.raw).jsonObject["cadence"]?.jsonPrimitive?.contentOrNull }
                .getOrNull() ?: "today"
            obj.id to cadence
        },
)

internal fun initialTodayIds(
    snapshot: ch.lkmc.earnest.core.SyncSnapshot,
    states: Map<Long, ch.lkmc.earnest.core.State>,
    cadences: Map<Long, String>,
): Set<Long> = snapshot.entries.filter {
    !it.deleted && cadences[it.subscriptionId] in listOf("now", "today") &&
        (states[it.id]?.seen?.value ?: JsonPrimitive(SyncEngine.VALUE_UNSEEN)) == JsonPrimitive(SyncEngine.VALUE_UNSEEN) &&
        (states[it.id]?.shelf?.value ?: JsonPrimitive(SyncEngine.VALUE_NONE)) == JsonPrimitive(SyncEngine.VALUE_NONE)
}.map { it.id }.toSet()


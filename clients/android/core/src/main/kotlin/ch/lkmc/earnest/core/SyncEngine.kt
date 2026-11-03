package ch.lkmc.earnest.core

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.time.Duration
import java.time.Instant

@Serializable
data class CachedEntry(val id: Long, val subscriptionId: Long, val seq: Long, val rev: Long, val deleted: Boolean = false, val data: String = "")

@Serializable
data class CachedObject(val kind: String, val id: Long, val rev: Long, val deleted: Boolean = false, val folderId: Long = 0, val data: String = "")

@Serializable
data class CachedPosition(val principal: Long, val view: String, val rev: Long, val seq: Long)

@Serializable
data class RangeScope(val kind: String, val ids: List<Long> = emptyList())

@Serializable
data class Pending(val write: StateWrite, val scope: RangeScope? = null, val throughSeq: Long = 0, val inFlight: Boolean = false)

@Serializable
data class SyncSnapshot(
    val cursor: Revision,
    val bootstrapping: Boolean = false,
    val entries: List<CachedEntry> = emptyList(),
    val states: List<State> = emptyList(),
    val objects: List<CachedObject> = emptyList(),
    val positions: List<CachedPosition> = emptyList(),
    val outbox: List<Pending> = emptyList(),
)

/** Implementations must commit the entire snapshot atomically. */
interface SyncStore {
    fun load(): SyncSnapshot
    fun save(snapshot: SyncSnapshot)
}

data class SyncChange(
    val rev: Long,
    val entry: CachedEntry? = null,
    val states: List<State> = emptyList(),
    val obj: CachedObject? = null,
    val position: CachedPosition? = null,
)

/** Operations are serialized by the caller, including across store instances. */
class SyncEngine(private val store: SyncStore) {
    private var snapshot = store.load()
    fun snapshot(): SyncSnapshot = snapshot
    private fun commit(next: SyncSnapshot) { store.save(next); snapshot = next }

    fun beginBootstrap(cursor: Revision) {
        val pending = if (snapshot.cursor.generation == cursor.generation) snapshot.outbox else snapshot.outbox.map {
            it.copy(write = it.write.copy(base = Revision(cursor.generation, 0)), inFlight = false)
        }
        commit(SyncSnapshot(cursor = cursor, bootstrapping = true, outbox = pending))
    }

    private fun merge(state: SyncSnapshot, change: SyncChange): SyncSnapshot {
        var next = state
        change.entry?.let { item ->
            val old = next.entries.find { it.id == item.id }
            if (old == null || (!old.deleted && item.rev > old.rev)) next = next.copy(entries = next.entries.filterNot { it.id == item.id } + item)
        }
        change.obj?.let { item ->
            val old = next.objects.find { it.kind == item.kind && it.id == item.id }
            if (old == null || item.rev > old.rev) next = next.copy(objects = next.objects.filterNot { it.kind == item.kind && it.id == item.id } + item)
        }
        change.position?.let { item ->
            val old = next.positions.find { it.principal == item.principal && it.view == item.view }
            if (old == null || item.rev > old.rev) next = next.copy(positions = next.positions.filterNot { it.principal == item.principal && it.view == item.view } + item)
        }
        for (item in change.states) {
            if (next.entries.none { it.id == item.entryId && !it.deleted }) continue
            val old = next.states.find { it.principal == item.principal && it.entryId == item.entryId }
            val merged = if (old == null) item else item.copy(
                seen = if (item.seen.rev > old.seen.rev) item.seen else old.seen,
                shelf = if (item.shelf.rev > old.shelf.rev) item.shelf else old.shelf,
                kept = if (item.kept.rev > old.kept.rev) item.kept else old.kept,
                stateRev = maxOf(old.stateRev, item.stateRev),
            )
            next = next.copy(states = next.states.filterNot { it.principal == item.principal && it.entryId == item.entryId } + merged)
        }
        return next
    }

    fun mergeBootstrap(change: SyncChange) { commit(merge(snapshot, change)) }

    fun finishBootstrap() { commit(snapshot.copy(bootstrapping = false)) }

    fun applyPage(changes: List<SyncChange>, cursor: Revision) {
        require(!snapshot.bootstrapping) { "bootstrap is incomplete" }
        require(cursor.generation == snapshot.cursor.generation) { "generation changed; bootstrap required" }
        var expected = snapshot.cursor.rev
        for (change in changes) {
            if (change.rev <= snapshot.cursor.rev) continue
            require(change.rev == expected + 1) { "incomplete change page" }
            expected = change.rev
        }
        require(cursor.rev == expected) { "incomplete change page" }
        var next = snapshot
        changes.filter { it.rev > snapshot.cursor.rev }.forEach { next = merge(next, it) }
        commit(next.copy(cursor = cursor))
    }

    fun queue(write: StateWrite) {
        require(write.opId.isNotBlank() && !write.opId.endsWith("/clear-done") && write.principal > 0 && write.entryId > 0)
        require(write.base.generation == snapshot.cursor.generation && write.kind in listOf("explicit", "passive"))
        require(when (write.register) {
            "seen" -> write.value == JsonPrimitive("seen") || write.value == JsonPrimitive("unseen")
            "shelf" -> listOf("none", "later", "done").any { write.value == JsonPrimitive(it) }
            "kept" -> write.value == JsonPrimitive(true) || write.value == JsonPrimitive(false)
            else -> false
        })
        Instant.parse(write.occurredAt)
        require(snapshot.outbox.none { it.write.opId == write.opId || it.write.opId == write.opId + "/clear-done" })
        var pending = snapshot.outbox
        if (write.register == "seen" && write.value == JsonPrimitive("unseen") && state(write.principal, write.entryId)?.shelf?.value == JsonPrimitive("done")) {
            val shelfRev = snapshot.states.find { it.principal == write.principal && it.entryId == write.entryId }?.shelf?.rev ?: 0
            pending = coalesce(pending, write.copy(opId = write.opId + "/clear-done", register = "shelf", value = JsonPrimitive("none"), base = write.base.copy(rev = shelfRev)))
        }
        commit(snapshot.copy(outbox = coalesce(pending, write)))
    }

    private fun coalesce(items: List<Pending>, write: StateWrite): List<Pending> {
        val previous = items.lastOrNull { !it.inFlight && it.scope == null && it.write.principal == write.principal && it.write.entryId == write.entryId && it.write.register == write.register }
        return items.filterNot { it === previous } + Pending(if (previous == null) write else write.copy(base = previous.write.base))
    }

    fun queueRange(write: StateWrite, scope: RangeScope, throughSeq: Long) {
        require(write.opId.isNotBlank() && !write.opId.endsWith("/clear-done") && write.principal > 0 && write.base.generation == snapshot.cursor.generation)
        require(write.register == "seen" && write.value == JsonPrimitive("seen") && write.kind in listOf("explicit", "passive") && throughSeq >= 0)
        require(when (scope.kind) { "all" -> scope.ids.isEmpty(); "subscriptions" -> scope.ids.isNotEmpty(); "folder" -> scope.ids.size == 1; else -> false })
        require(scope.ids.all { it > 0 } && snapshot.outbox.none { it.write.opId == write.opId })
        Instant.parse(write.occurredAt)
        commit(snapshot.copy(outbox = snapshot.outbox + Pending(write, scope, throughSeq)))
    }

    fun state(principal: Long, entryId: Long): State? {
        var result = snapshot.states.find { it.principal == principal && it.entryId == entryId } ?: initialState(principal, entryId)
        val entry = snapshot.entries.find { it.id == entryId && !it.deleted } ?: return result
        for (item in snapshot.outbox) {
            val w = item.write
            if (w.principal != principal) continue
            val scope = item.scope
            if (scope != null) {
                val matches = entry.seq <= item.throughSeq && when (scope.kind) {
                    "all" -> true
                    "subscriptions" -> entry.subscriptionId in scope.ids
                    "folder" -> snapshot.objects.any { it.kind == "subscription" && it.id == entry.subscriptionId && !it.deleted && it.folderId in scope.ids }
                    else -> false
                }
                if (matches) result = result.copy(seen = result.seen.copy(value = w.value))
            } else if (w.entryId == entryId) {
                result = when (w.register) {
                    "seen" -> result.copy(seen = result.seen.copy(value = w.value), shelf = if (w.value == JsonPrimitive("unseen") && result.shelf.value == JsonPrimitive("done")) result.shelf.copy(value = JsonPrimitive("none")) else result.shelf)
                    "shelf" -> result.copy(shelf = result.shelf.copy(value = w.value))
                    "kept" -> result.copy(kept = result.kept.copy(value = w.value))
                    else -> result
                }
            }
        }
        return result
    }

    fun markInFlight(opId: String) { commit(snapshot.copy(outbox = snapshot.outbox.map { if (it.write.opId == opId) it.copy(inFlight = true) else it })) }

    fun acknowledge(generation: String, opId: String, states: List<State>) {
        require(generation == snapshot.cursor.generation) { "stale acknowledgement" }
        val pending = snapshot.outbox.find { it.write.opId == opId } ?: return
        require(pending.write.base.generation == generation)
        val next = merge(snapshot, SyncChange(snapshot.cursor.rev, states = states))
        commit(next.copy(outbox = next.outbox.filterNot { it.write.opId == opId }))
    }

    /** Returns dropped items so the caller can report them to the user. */
    fun expire(now: Instant): List<Pending> {
        val expired = snapshot.outbox.filter { Duration.between(Instant.parse(it.write.occurredAt), now) > Duration.ofDays(30) && !it.inFlight }
        if (expired.isNotEmpty()) commit(snapshot.copy(outbox = snapshot.outbox - expired.toSet()))
        return expired
    }
}

private fun initialState(principal: Long, entryId: Long): State {
    fun initial(value: JsonElement) = Register(value, 0, "0001-01-01T00:00:00Z", 0, "")
    return State(principal, entryId, initial(JsonPrimitive("unseen")), initial(JsonPrimitive("none")), initial(JsonPrimitive(false)), 0)
}

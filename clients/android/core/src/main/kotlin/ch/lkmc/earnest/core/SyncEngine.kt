package ch.lkmc.earnest.core

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.time.Duration
import java.time.Instant

/** One cached entry. [raw] keeps the server projection for fields the app does not model. */
@Serializable
data class CachedEntry(
    val id: Long,
    @SerialName("subscription_id") val subscriptionId: Long = 0,
    @SerialName("feed_id") val feedId: Long = 0,
    val seq: Long = 0,
    @SerialName("first_seen_at") val firstSeenAt: String = "",
    @SerialName("published_at") val publishedAt: String? = null,
    @SerialName("updated_at") val updatedAt: String? = null,
    val title: String = "",
    val url: String = "",
    val author: String = "",
    val rev: Long = 0,
    val deleted: Boolean = false,
    val raw: String = "",
    // A metadata 404 hides the entry until a newer server projection arrives.
    val unavailable: Boolean = false,
)

/** A revisioned resource object: subscription, folder, label, annotation or token. */
@Serializable
data class SyncObject(
    val kind: String,
    val id: Long,
    val principal: Long = 0,
    @SerialName("entry_id") val entryId: Long = 0,
    @SerialName("folder_id") val folderId: Long = 0,
    val name: String = "",
    val rev: Long,
    val deleted: Boolean = false,
    val raw: String = "",
)

@Serializable
data class CachedPosition(
    val principal: Long,
    val view: String,
    val seq: Long,
    val rev: Long,
)

/** A tagged outbox item: exactly one of [write] and [range] is present. */
@Serializable
data class Pending(
    val write: StateWrite? = null,
    val range: RangeWrite? = null,
    val inFlight: Boolean = false,
) {
    val operation: StateWrite get() = write ?: requireNotNull(range).write
}

@Serializable
data class SyncSnapshot(
    val cursor: Revision,
    val entries: List<CachedEntry> = emptyList(),
    val states: List<State> = emptyList(),
    val objects: List<SyncObject> = emptyList(),
    val positions: List<CachedPosition> = emptyList(),
    val outbox: List<Pending> = emptyList(),
)

/** Implementations must commit the entire snapshot atomically. */
interface SyncStore {
    fun load(): SyncSnapshot
    fun save(snapshot: SyncSnapshot)

    /**
     * A cheap change token for the persisted snapshot, or -1 when the store
     * cannot tell. Callers compare it before paying for a full [load] decode.
     */
    fun version(): Long = -1

    /** Completes any coalesced write; the default is a no-op for synchronous stores. */
    fun flush() {}
}

/** A change applied to the client cache. Payloads are parsed by the repository. */
@Serializable
data class SyncChange(
    val rev: Long,
    val entry: CachedEntry? = null,
    val states: List<State> = emptyList(),
    val obj: SyncObject? = null,
    val position: CachedPosition? = null,
)

enum class SyncError { INVALID, GENERATION }

/** A protocol-level failure; [error] distinguishes a stale generation from invalid input. */
class SyncException(val error: SyncError, message: String) : IllegalArgumentException(message)

/** Operations are serialized by the caller, including across store instances. */
class SyncEngine(private val store: SyncStore) {
    private var snapshot = store.load()

    fun snapshot(): SyncSnapshot = snapshot

    /** A fresh active set removes mutes deleted before this cache's cursor. */
    fun replaceRiverResources(mutes: List<SyncObject>, settings: SyncObject) {
        commit(snapshot.copy(objects = snapshot.objects.filter { it.kind != River.MUTE && it.kind != River.SETTINGS } + mutes + settings))
    }

    /** Persists the current snapshot when the store coalesces writes. */
    fun flush() = store.flush()

    /** Replaces the in-memory snapshot from the store; pending intent is durable. */
    fun reload() {
        snapshot = store.load()
    }

    private fun commit(next: SyncSnapshot) {
        store.save(next)
        snapshot = next
    }

    /**
     * Begins a bootstrap from the cursor read before paging. The cache is
     * replaced but local intent survives. Intent from an older generation is
     * made stale against the new one rather than rebased onto a revision that
     * would force it to win.
     */
    fun beginBootstrap(cursor: Revision) {
        val pending = if (cursor.generation == snapshot.cursor.generation) {
            snapshot.outbox
        } else {
            snapshot.outbox.map { stale(it, cursor.generation) }
        }
        commit(SyncSnapshot(cursor = cursor, outbox = pending))
    }

    /**
     * Installs a fully paged bootstrap; incomplete pages never replace the live
     * cache. Live intent survives like [beginBootstrap], so operations queued
     * while the replacement was fetched are not lost.
     */
    internal fun installBootstrap(completed: SyncSnapshot) {
        val pending = if (completed.cursor.generation == snapshot.cursor.generation) {
            snapshot.outbox
        } else {
            snapshot.outbox.map { stale(it, completed.cursor.generation) }
        }
        commit(completed.copy(outbox = pending))
    }

    private fun stale(item: Pending, generation: String): Pending {
        val base = Revision(generation, 0)
        return when {
            item.write != null -> item.copy(write = item.write.copy(base = base))
            else -> item.copy(range = item.range?.copy(write = item.range.write.copy(base = base)))
        }
    }

    fun mergeEntry(entry: CachedEntry) {
        val old = snapshot.entries.find { it.id == entry.id }
        if (old == null || (entry.rev > old.rev && (!old.deleted || old.unavailable))) {
            commit(snapshot.copy(entries = snapshot.entries.filterNot { it.id == entry.id } + entry))
        }
    }

    fun mergeState(state: State) {
        val old = snapshot.states.find { it.principal == state.principal && it.entryId == state.entryId }
        val merged = if (old == null) state else mergeRegisterState(old, state)
        commit(snapshot.copy(states = snapshot.states.filterNot { it.principal == state.principal && it.entryId == state.entryId } + merged))
    }

    fun mergeObject(obj: SyncObject) {
        val old = snapshot.objects.find { it.kind == obj.kind && it.id == obj.id }
        if (old == null || obj.rev > old.rev) {
            commit(snapshot.copy(objects = snapshot.objects.filterNot { it.kind == obj.kind && it.id == obj.id } + obj))
        }
    }

    fun mergePosition(position: CachedPosition) {
        val old = snapshot.positions.find { it.principal == position.principal && it.view == position.view }
        if (old == null || position.rev > old.rev) {
            commit(snapshot.copy(positions = snapshot.positions.filterNot { it.principal == position.principal && it.view == position.view } + position))
        }
    }

    fun mergeBootstrap(change: SyncChange) {
        commit(merge(snapshot, change))
    }

    /**
     * Applies one change page and its cursor atomically. A gap or a partial
     * page with the final cursor is rejected before anything is committed, and
     * replaying a page is safe.
     */
    fun applyPage(changes: List<SyncChange>, cursor: Revision) {
        if (cursor.generation != snapshot.cursor.generation) {
            throw SyncException(SyncError.GENERATION, "generation changed; bootstrap required")
        }
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

    private fun merge(state: SyncSnapshot, change: SyncChange): SyncSnapshot {
        var next = state
        change.entry?.let { item ->
            val old = next.entries.find { it.id == item.id }
            if (old == null || (item.rev > old.rev && (!old.deleted || old.unavailable))) {
                next = next.copy(entries = next.entries.filterNot { it.id == item.id } + item)
            }
        }
        change.obj?.let { item ->
            val old = next.objects.find { it.kind == item.kind && it.id == item.id }
            if (old == null || item.rev > old.rev) {
                next = next.copy(objects = next.objects.filterNot { it.kind == item.kind && it.id == item.id } + item)
            }
        }
        change.position?.let { item ->
            val old = next.positions.find { it.principal == item.principal && it.view == item.view }
            if (old == null || item.rev > old.rev) {
                next = next.copy(positions = next.positions.filterNot { it.principal == item.principal && it.view == item.view } + item)
            }
        }
        for (item in change.states) {
            val entry = next.entries.find { it.id == item.entryId }
            if (entry == null || entry.deleted) continue
            val old = next.states.find { it.principal == item.principal && it.entryId == item.entryId }
            val merged = if (old == null) item else mergeRegisterState(old, item)
            next = next.copy(states = next.states.filterNot { it.principal == item.principal && it.entryId == item.entryId } + merged)
        }
        return next
    }

    /**
     * Queues one register intent, coalescing per principal, entry and
     * register. Clearing Done also queues a shelf-none intent that survives a
     * later coalesced seen action, so Done cannot reappear.
     */
    fun queue(write: StateWrite) {
        require(write.opId.isNotBlank() && !write.opId.endsWith(CLEAR_DONE_SUFFIX)) { "invalid operation id" }
        require(write.principal > 0 && write.entryId > 0) { "invalid operation target" }
        if (write.base.generation != snapshot.cursor.generation) {
            throw SyncException(SyncError.GENERATION, "generation changed; bootstrap required")
        }
        require(write.kind == KIND_EXPLICIT || write.kind == KIND_PASSIVE) { "invalid operation kind" }
        require(validValue(write.register, write.value)) { "invalid register value" }
        Instant.parse(write.occurredAt)
        val clearDone = write.register == REGISTER_SEEN && write.value == JsonPrimitive(VALUE_UNSEEN) &&
            state(write.principal, write.entryId).shelf.value == JsonPrimitive(VALUE_DONE)
        require(snapshot.outbox.none { it.operation.opId == write.opId || (clearDone && it.operation.opId == write.opId + CLEAR_DONE_SUFFIX) }) {
            "duplicate operation id"
        }
        var pending = snapshot.outbox
        if (clearDone) {
            val shelf = state(write.principal, write.entryId).shelf
            pending = queueWrite(
                pending,
                write.copy(
                    opId = write.opId + CLEAR_DONE_SUFFIX,
                    register = REGISTER_SHELF,
                    value = JsonPrimitive(VALUE_NONE),
                    base = write.base.copy(rev = shelf.rev),
                )
            )
        }
        pending = queueWrite(pending, write)
        commit(snapshot.copy(outbox = pending))
    }

    private fun queueWrite(items: List<Pending>, write: StateWrite): List<Pending> {
        val previous = items.lastOrNull {
            !it.inFlight && it.write != null &&
                it.write.principal == write.principal &&
                it.write.entryId == write.entryId &&
                it.write.register == write.register
        } ?: return items + Pending(write = write)
        val replacement = write.copy(base = previous.write!!.base)
        return items.filterNot { it === previous } + Pending(write = replacement)
    }

    /** Queues a first-class range with its receipt-resolved boundary and scope. */
    fun queueRange(write: RangeWrite) {
        val operation = write.write
        require(operation.opId.isNotBlank() && !operation.opId.endsWith(CLEAR_DONE_SUFFIX)) { "invalid operation id" }
        require(operation.principal > 0) { "invalid range target" }
        if (operation.base.generation != snapshot.cursor.generation) {
            throw SyncException(SyncError.GENERATION, "generation changed; bootstrap required")
        }
        require(operation.register == REGISTER_SEEN && operation.value == JsonPrimitive(VALUE_SEEN)) { "a range marks seen" }
        require(operation.kind == KIND_EXPLICIT || operation.kind == KIND_PASSIVE) { "invalid operation kind" }
        Instant.parse(operation.occurredAt)
        require(write.throughSeq >= 0) { "invalid range boundary" }
        validateScope(write.scope)
        require(snapshot.outbox.none { it.operation.opId == operation.opId }) { "duplicate operation id" }
        commit(snapshot.copy(outbox = snapshot.outbox + Pending(range = write)))
    }

    /** Effective state: server registers plus the pending overlay. */
    fun state(principal: Long, entryId: Long): State {
        var result = snapshot.states.find { it.principal == principal && it.entryId == entryId }
            ?: initialState(principal, entryId)
        val entry = snapshot.entries.find { it.id == entryId && !it.deleted } ?: return result
        for (item in snapshot.outbox) {
            val pending = item.range
            if (pending != null) {
                val operation = pending.write
                if (operation.principal == principal && rangeContains(pending, entry)) {
                    result = result.copy(seen = result.seen.copy(value = JsonPrimitive(VALUE_SEEN)))
                }
                continue
            }
            val operation = item.write ?: continue
            if (operation.principal != principal || operation.entryId != entryId) continue
            result = when (operation.register) {
                REGISTER_SEEN -> result.copy(
                    seen = result.seen.copy(value = operation.value),
                    shelf = if (operation.value == JsonPrimitive(VALUE_UNSEEN) && result.shelf.value == JsonPrimitive(VALUE_DONE)) {
                        result.shelf.copy(value = JsonPrimitive(VALUE_NONE))
                    } else {
                        result.shelf
                    },
                )
                REGISTER_SHELF -> result.copy(shelf = result.shelf.copy(value = operation.value))
                REGISTER_KEPT -> result.copy(kept = result.kept.copy(value = operation.value))
                else -> result
            }
        }
        return result
    }

    private fun rangeContains(range: RangeWrite, entry: CachedEntry): Boolean {
        if (entry.seq > range.throughSeq) return false
        return when (range.scope.kind) {
            SCOPE_ALL -> true
            SCOPE_SUBSCRIPTIONS -> range.scope.ids.orEmpty().contains(entry.subscriptionId)
            SCOPE_FOLDER -> snapshot.objects.any {
                it.kind == OBJECT_SUBSCRIPTION && !it.deleted && it.id == entry.subscriptionId &&
                    range.scope.ids.orEmpty().contains(it.folderId)
            }
            else -> false
        }
    }

    /**
     * Applies a batch acknowledgement. The generation must be the one the
     * request was sent under; a stale response is rejected whole, before any
     * state merge or outbox removal.
     */
    fun acknowledge(generation: String, results: List<Result>) {
        if (generation != snapshot.cursor.generation) {
            throw SyncException(SyncError.GENERATION, "stale acknowledgement")
        }
        var next = snapshot
        for (result in results) {
            next = merge(next, SyncChange(next.cursor.rev, states = listOf(result.state)))
            if (result.status == STATUS_GONE) {
                next = next.copy(
                    entries = next.entries.map {
                        if (it.id == result.state.entryId) it.copy(deleted = true) else it
                    },
                    states = next.states.filterNot { it.entryId == result.state.entryId },
                )
            }
        }
        val ids = results.map { it.opId }.toSet()
        commit(next.copy(outbox = next.outbox.filterNot { it.operation.opId in ids }))
    }

    /** Applies a range acknowledgement under its request generation. */
    fun acknowledgeRange(opId: String, generation: String, result: RangeResult) {
        if (generation != snapshot.cursor.generation) {
            throw SyncException(SyncError.GENERATION, "stale acknowledgement")
        }
        var next = snapshot
        for (item in result.results.orEmpty()) {
            next = merge(next, SyncChange(next.cursor.rev, states = listOf(item.state)))
            if (item.status == STATUS_GONE) {
                next = next.copy(
                    entries = next.entries.map {
                        if (it.id == item.state.entryId) it.copy(deleted = true) else it
                    },
                    states = next.states.filterNot { it.entryId == item.state.entryId },
                )
            }
        }
        commit(next.copy(outbox = next.outbox.filterNot { it.operation.opId == opId }))
    }

    fun markInFlight(opId: String) {
        commit(snapshot.copy(outbox = snapshot.outbox.map { if (it.operation.opId == opId) it.copy(inFlight = true) else it }))
    }

    fun markQueued(opId: String) {
        commit(snapshot.copy(outbox = snapshot.outbox.map { if (it.operation.opId == opId) it.copy(inFlight = false) else it }))
    }

    /** Drops queue items older than 30 days and returns them for reporting. */
    fun expire(now: Instant): List<Pending> {
        val expired = snapshot.outbox.filter {
            !it.inFlight && Duration.between(Instant.parse(it.operation.occurredAt), now) > Duration.ofDays(EXPIRY_DAYS)
        }
        if (expired.isNotEmpty()) {
            commit(snapshot.copy(outbox = snapshot.outbox - expired.toSet()))
        }
        return expired
    }

    fun entry(id: Long): CachedEntry? = snapshot.entries.find { it.id == id }

    fun findObject(kind: String, id: Long): SyncObject? = snapshot.objects.find { it.kind == kind && it.id == id }

    fun position(principal: Long, view: String): CachedPosition? =
        snapshot.positions.find { it.principal == principal && it.view == view }

    companion object {
        const val REGISTER_SEEN = "seen"
        const val REGISTER_SHELF = "shelf"
        const val REGISTER_KEPT = "kept"
        const val VALUE_UNSEEN = "unseen"
        const val VALUE_SEEN = "seen"
        const val VALUE_NONE = "none"
        const val VALUE_LATER = "later"
        const val VALUE_DONE = "done"
        const val KIND_EXPLICIT = "explicit"
        const val KIND_PASSIVE = "passive"
        const val STATUS_GONE = "gone"
        const val STATUS_EXPIRED = "expired"
        const val STATUS_APPLIED = "applied"
        const val STATUS_SUPERSEDED = "superseded"
        const val SCOPE_ALL = "all"
        const val SCOPE_SUBSCRIPTIONS = "subscriptions"
        const val SCOPE_FOLDER = "folder"
        const val OBJECT_SUBSCRIPTION = "subscription"
        const val OBJECT_FOLDER = "folder"
        const val OBJECT_LABEL = "label"
        const val OBJECT_ANNOTATION = "annotation"
        const val OBJECT_TOKEN = "token"
        const val CLEAR_DONE_SUFFIX = "/clear-done"
        const val EXPIRY_DAYS = 30L

        fun validValue(register: String, value: JsonElement): Boolean = when (register) {
            REGISTER_SEEN -> value == JsonPrimitive(VALUE_SEEN) || value == JsonPrimitive(VALUE_UNSEEN)
            REGISTER_SHELF -> listOf(VALUE_NONE, VALUE_LATER, VALUE_DONE).any { value == JsonPrimitive(it) }
            REGISTER_KEPT -> value == JsonPrimitive(true) || value == JsonPrimitive(false)
            else -> false
        }

        fun validateScope(scope: Scope) {
            require(when (scope.kind) {
                SCOPE_ALL -> scope.ids.isNullOrEmpty()
                SCOPE_SUBSCRIPTIONS -> !scope.ids.isNullOrEmpty()
                SCOPE_FOLDER -> scope.ids?.size == 1
                else -> false
            }) { "invalid range scope" }
            require(scope.ids.orEmpty().all { it > 0 }) { "invalid range scope id" }
        }

        fun initialState(principal: Long, entryId: Long): State {
            fun initial(value: JsonElement) = Register(value, 0, "0001-01-01T00:00:00Z", 0, "")
            return State(
                principal,
                entryId,
                initial(JsonPrimitive(VALUE_UNSEEN)),
                initial(JsonPrimitive(VALUE_NONE)),
                initial(JsonPrimitive(false)),
                0,
            )
        }

        /** Merges each register independently and keeps the highest state revision. */
        fun mergeRegisterState(old: State, next: State): State {
            require(old.principal == next.principal && old.entryId == next.entryId) { "state merge mismatch" }
            return old.copy(
                seen = if (next.seen.rev > old.seen.rev) next.seen else old.seen,
                shelf = if (next.shelf.rev > old.shelf.rev) next.shelf else old.shelf,
                kept = if (next.kept.rev > old.kept.rev) next.kept else old.kept,
                stateRev = maxOf(old.stateRev, next.stateRev),
            )
        }
    }
}

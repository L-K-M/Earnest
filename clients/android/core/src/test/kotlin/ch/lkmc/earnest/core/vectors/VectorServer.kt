package ch.lkmc.earnest.core.vectors

import ch.lkmc.earnest.core.Register
import ch.lkmc.earnest.core.RangeResult
import ch.lkmc.earnest.core.RangeWrite
import ch.lkmc.earnest.core.Result
import ch.lkmc.earnest.core.Scope
import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.StateWrite
import ch.lkmc.earnest.core.SyncEngine
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive
import java.time.Duration
import java.time.Instant

/** Error names shared with the fixture protocol. */
enum class VectorError {
    INVALID,
    GENERATION,
    CURSOR_GONE,
    WITHHELD,
    ATTEMPTED,
}

class VectorException(val error: VectorError) : Exception(error.name)

@Serializable
data class VectorEntry(
    val id: Long,
    val subscriptionId: Long,
    val seq: Long,
    val firstSeenAt: String,
    val publishedAt: String,
    val rev: Long,
    val deleted: Boolean = false,
)

@Serializable
data class VectorObject(
    val kind: String,
    val id: Long,
    val principal: Long = 0,
    val entryId: Long = 0,
    val folderId: Long = 0,
    val name: String = "",
    val rev: Long,
    val deleted: Boolean = false,
)

@Serializable
data class VectorPosition(
    val principal: Long,
    val view: String,
    val seq: Long,
    val rev: Long,
)

@Serializable
data class VectorChange(
    val rev: Long,
    val type: String,
    val entry: VectorEntry? = null,
    val results: List<Result>? = null,
    val range: RangeResult? = null,
    @kotlinx.serialization.SerialName("object") val object_: VectorObject? = null,
    val position: VectorPosition? = null,
)

@Serializable
data class VectorNotification(
    val actor: Long,
    val opId: String,
    val generation: String,
    val status: String,
    val replay: Boolean = false,
    val attemptedAt: String? = null,
)

data class VectorContext(
    val actor: Long,
    var token: String,
    val receipt: Instant,
)

private data class StateKey(val principal: Long, val entryId: Long)
private data class ObjectKey(val kind: String, val id: Long)
private data class PositionKey(val principal: Long, val view: String)
private data class OpKey(val actor: Long, val opId: String)

private class Remembered(
    val principal: Long,
    val entryId: Long,
    val status: String,
    val rangeResult: RangeResult? = null,
)

/**
 * A small in-memory server model used only by the vector gate. It mirrors the
 * settled protocol in docs/sync.md so server steps can be simulated exactly;
 * it performs no I/O and reads no clock.
 */
class VectorServer(val generation: String = "g1") {
    private var rev = 0L
    private var seq = 0L
    private var floor = 0L
    private val entries = linkedMapOf<Long, VectorEntry>()
    private val states = linkedMapOf<StateKey, State>()
    private val operations = linkedMapOf<OpKey, Remembered>()
    private val changes = mutableListOf<VectorChange>()
    private val objects = linkedMapOf<ObjectKey, VectorObject>()
    private val positions = linkedMapOf<PositionKey, VectorPosition>()
    private val notifications = linkedMapOf<OpKey, VectorNotification>()
    private var withheld = false

    fun cursor(): ch.lkmc.earnest.core.Revision = ch.lkmc.earnest.core.Revision(generation, rev)
    fun highestSeq(): Long = seq

    fun entry(id: Long): VectorEntry? = entries[id]

    fun findObject(kind: String, id: Long): VectorObject? = objects[ObjectKey(kind, id)]

    fun position(principal: Long, view: String): VectorPosition? = positions[PositionKey(principal, view)]

    fun log(): List<VectorChange> = changes.toList()

    fun state(principal: Long, entryId: Long): State =
        states[StateKey(principal, entryId)] ?: SyncEngine.initialState(principal, entryId)

    fun putObject(input: VectorObject): VectorObject {
        if (input.id <= 0) throw VectorException(VectorError.INVALID)
        val old = objects[ObjectKey(input.kind, input.id)]
        val kind = when (input.kind) {
            SyncEngine.OBJECT_SUBSCRIPTION -> {
                if (old != null) {
                    if (input.deleted) "subscription.deleted" else "subscription.updated"
                } else {
                    "subscription.created"
                }
            }
            SyncEngine.OBJECT_FOLDER -> if (input.deleted) "folder.deleted" else "folder.updated"
            SyncEngine.OBJECT_LABEL -> {
                if (input.entryId <= 0 || input.name.isEmpty() || input.principal <= 0) throw VectorException(VectorError.INVALID)
                val entry = entries[input.entryId]
                if (entry == null || entry.deleted) throw VectorException(VectorError.INVALID)
                if (old != null && (old.entryId != input.entryId || old.principal != input.principal || old.name != input.name)) {
                    throw VectorException(VectorError.INVALID)
                }
                if (input.deleted) "label.removed" else "label.added"
            }
            SyncEngine.OBJECT_ANNOTATION -> if (input.deleted) "annotation.deleted" else "annotation.updated"
            SyncEngine.OBJECT_TOKEN -> {
                if (!input.deleted) throw VectorException(VectorError.INVALID)
                "token.revoked"
            }
            else -> throw VectorException(VectorError.INVALID)
        }
        val stored = input.copy(rev = rev + 1)
        objects[ObjectKey(stored.kind, stored.id)] = stored
        append(VectorChange(0, kind, object_ = stored))
        return stored
    }

    fun putPosition(input: VectorPosition): VectorPosition {
        if (input.principal <= 0 || input.view.isEmpty() || input.seq > seq) throw VectorException(VectorError.INVALID)
        val stored = input.copy(rev = rev + 1)
        positions[PositionKey(stored.principal, stored.view)] = stored
        append(VectorChange(0, "position.updated", position = stored))
        return stored
    }

    fun ingest(input: VectorEntryInput, receipt: Instant): VectorEntry {
        if (input.id <= 0 || input.subscriptionId <= 0 || input.deleted) throw VectorException(VectorError.INVALID)
        if (entries.containsKey(input.id)) throw VectorException(VectorError.INVALID)
        val published = input.publishedAt?.let(Instant::parse)?.toString() ?: ZERO_TIME.toString()
        var next = VectorEntry(input.id, input.subscriptionId, input.seq, "", published, 0, input.deleted)
        if (next.seq == 0L) next = next.copy(seq = seq + 1)
        if (next.seq <= seq) throw VectorException(VectorError.INVALID)
        next = next.copy(firstSeenAt = receipt.toString(), rev = rev + 1)
        seq = next.seq
        entries[next.id] = next
        append(VectorChange(0, "entry.created", entry = next))
        return next
    }

    fun updateEntry(id: Long, publishedAt: Instant) {
        val old = entries[id] ?: throw VectorException(VectorError.INVALID)
        if (old.deleted) throw VectorException(VectorError.INVALID)
        val next = old.copy(publishedAt = publishedAt.toString(), rev = rev + 1)
        entries[id] = next
        append(VectorChange(0, "entry.updated", entry = next))
    }

    fun deleteEntry(id: Long) {
        val old = entries[id] ?: throw VectorException(VectorError.INVALID)
        if (old.deleted) throw VectorException(VectorError.INVALID)
        val next = old.copy(deleted = true, rev = rev + 1)
        entries[id] = next
        append(VectorChange(0, "entry.deleted", entry = next))
    }

    fun batch(ctx: VectorContext, writes: List<StateWrite>): List<Result> {
        val targets = mutableSetOf<Pair<StateKey, String>>()
        val ids = mutableSetOf<String>()
        for (w in writes) {
            validateWrite(ctx, w, cursor())
            val target = StateKey(w.principal, w.entryId) to w.register
            if (targets.contains(target) || ids.contains(w.opId)) throw VectorException(VectorError.INVALID)
            val old = operations[OpKey(ctx.actor, w.opId)]
            if (old?.rangeResult != null) throw VectorException(VectorError.INVALID)
            targets += target
            ids += w.opId
        }
        val results = mutableListOf<Result>()
        var applied = false
        for (w in writes) {
            val key = OpKey(ctx.actor, w.opId)
            val replay = replay(key)
            if (replay != null) {
                results += replay
                continue
            }
            val result = resolve(ctx, w, rev + 1)
            results += result
            applied = applied || result.status == SyncEngine.STATUS_APPLIED
            operations[key] = Remembered(w.principal, w.entryId, result.status)
        }
        for (i in results.indices) {
            results[i] = results[i].copy(state = state(results[i].state.principal, results[i].state.entryId))
        }
        if (applied) append(VectorChange(0, "state.updated", results = results))
        return results
    }

    fun through(ctx: VectorContext, request: RangeWrite): RangeResult {
        val w = request.write.copy(entryId = 1)
        validateWrite(ctx, w, cursor())
        if (w.register != SyncEngine.REGISTER_SEEN || w.value != JsonPrimitive(SyncEngine.VALUE_SEEN)) {
            throw VectorException(VectorError.INVALID)
        }
        val subs = subscriptions(request.scope)
        val key = OpKey(ctx.actor, w.opId)
        val old = operations[key]
        if (old != null) {
            if (old.rangeResult == null) throw VectorException(VectorError.INVALID)
            val saved = old.rangeResult
            return saved.copy(
                replay = true,
                results = saved.results?.map { result ->
                    val state = state(result.state.principal, result.state.entryId)
                    val status = if (entries[result.state.entryId]?.deleted == true) SyncEngine.STATUS_GONE else result.status
                    result.copy(state = state, status = status, replay = true)
                },
            )
        }
        var result = RangeResult(
            status = SyncEngine.STATUS_APPLIED,
            throughSeq = request.throughSeq,
            subscriptions = subs,
            except = emptyList(),
            results = emptyList(),
            replay = false,
        )
        if (Duration.between(occurredAt(Instant.parse(w.occurredAt), ctx.receipt), ctx.receipt).compareTo(Duration.ofDays(30)) > 0) {
            result = result.copy(status = SyncEngine.STATUS_EXPIRED)
        } else {
            val memberIds = entries.values
                .filter { !it.deleted && it.seq <= result.throughSeq && subs.contains(it.subscriptionId) }
                .map { it.id }
                .sorted()
            val results = mutableListOf<Result>()
            val except = mutableListOf<Long>()
            for (id in memberIds) {
                val item = resolve(ctx, w.copy(entryId = id), rev + 1)
                results += item
                if (item.status != SyncEngine.STATUS_APPLIED) except += id
            }
            result = result.copy(results = results, except = except)
            append(VectorChange(0, "state.range", range = result))
        }
        val saved = result.copy(
            subscriptions = result.subscriptions.orEmpty(),
            except = result.except.orEmpty(),
            results = result.results.orEmpty(),
        )
        operations[key] = Remembered(w.principal, 0, result.status, saved)
        return result
    }

    fun changes(cursor: ch.lkmc.earnest.core.Revision, limit: Int): Pair<List<VectorChange>, ch.lkmc.earnest.core.Revision> {
        if (cursor.generation != generation) throw VectorException(VectorError.GENERATION)
        if (cursor.rev < floor || cursor.rev > rev) throw VectorException(VectorError.CURSOR_GONE)
        if (limit <= 0) throw VectorException(VectorError.INVALID)
        val out = mutableListOf<VectorChange>()
        var next = cursor
        for (change in changes) {
            if (change.rev <= cursor.rev) continue
            out += change
            next = next.copy(rev = change.rev)
            if (out.size == limit) break
        }
        return out to next
    }

    fun retain(rev: Long) {
        if (rev < floor || rev > this.rev) throw VectorException(VectorError.INVALID)
        floor = rev
        changes.removeAll { it.rev <= rev }
    }

    fun checkpoint(): VectorServer {
        val clone = VectorServer(generation)
        clone.rev = rev
        clone.seq = seq
        clone.floor = floor
        clone.entries.putAll(entries)
        clone.states.putAll(states)
        clone.operations.putAll(operations)
        clone.changes += changes
        clone.objects.putAll(objects)
        clone.positions.putAll(positions)
        clone.notifications.putAll(notifications)
        clone.withheld = withheld
        return clone
    }

    fun restore(snapshot: VectorServer, newGeneration: String): VectorServer {
        if (newGeneration.isEmpty() || newGeneration == snapshot.generation) throw VectorException(VectorError.INVALID)
        val clone = snapshot.checkpoint()
        return VectorServer(newGeneration).also {
            it.rev = clone.rev
            it.seq = clone.seq
            it.floor = clone.floor
            it.entries.putAll(clone.entries)
            it.states.putAll(clone.states)
            it.operations.putAll(clone.operations)
            it.changes += clone.changes
            it.objects.putAll(clone.objects)
            it.positions.putAll(clone.positions)
            it.notifications.putAll(clone.notifications)
            it.withheld = true
        }
    }

    fun acceptNotification(actor: Long, opId: String, generation: String): VectorNotification {
        if (generation != this.generation) throw VectorException(VectorError.GENERATION)
        if (actor <= 0 || opId.isEmpty()) throw VectorException(VectorError.INVALID)
        val key = OpKey(actor, opId)
        val old = notifications[key]
        if (old != null) return old.copy(replay = true)
        val created = VectorNotification(actor, opId, generation, "pending")
        notifications[key] = created
        return created
    }

    fun attemptNotification(actor: Long, opId: String, receipt: Instant): VectorNotification {
        val key = OpKey(actor, opId)
        val old = notifications[key] ?: throw VectorException(VectorError.INVALID)
        if (old.status != "pending") throw VectorException(VectorError.ATTEMPTED)
        if (withheld || old.generation != generation) throw VectorException(VectorError.WITHHELD)
        val next = old.copy(status = "uncertain", attemptedAt = receipt.toString())
        notifications[key] = next
        return next
    }

    fun completeNotification(actor: Long, opId: String) {
        val key = OpKey(actor, opId)
        val old = notifications[key] ?: throw VectorException(VectorError.INVALID)
        if (old.status != "uncertain") throw VectorException(VectorError.INVALID)
        notifications[key] = old.copy(status = "delivered")
    }

    fun releaseNotifications() {
        withheld = false
    }

    fun notification(actor: Long, opId: String): VectorNotification? = notifications[OpKey(actor, opId)]

    fun resources(): Resources = Resources(
        entries = entries.values.sortedBy { it.id },
        states = states.values.sortedWith(compareBy({ it.principal }, { it.entryId })),
        objects = objects.values.sortedWith(compareBy({ it.kind }, { it.id })),
        positions = positions.values.sortedWith(compareBy({ it.principal }, { it.view })),
    )

    private fun resolve(ctx: VectorContext, w: StateWrite, revision: Long): Result {
        val entry = entries[w.entryId]
        val state = state(w.principal, w.entryId)
        val result = resolveWrite(ctx, w, entry, state, revision)
        if (result.status == SyncEngine.STATUS_APPLIED) {
            states[StateKey(w.principal, w.entryId)] = result.state
        }
        return result
    }

    private fun resolveWrite(
        ctx: VectorContext,
        w: StateWrite,
        entry: VectorEntry?,
        state: State,
        revision: Long,
    ): Result {
        if (revision == 0L) throw VectorException(VectorError.INVALID)
        validateWrite(ctx, w, cursor().copy(rev = revision - 1))
        if (state.principal != w.principal || state.entryId != w.entryId) throw VectorException(VectorError.INVALID)
        var result = Result(w.opId, SyncEngine.STATUS_APPLIED, state, false)
        if (entry == null || entry.deleted) {
            return result.copy(status = SyncEngine.STATUS_GONE)
        }
        val occurred = occurredAt(Instant.parse(w.occurredAt), ctx.receipt)
        if (Duration.between(occurred, ctx.receipt).compareTo(Duration.ofDays(30)) > 0) {
            return result.copy(status = SyncEngine.STATUS_EXPIRED)
        }
        val source = deriveSource(ctx.token, w.kind)
        var next = Register(w.value, revision, occurred.toString(), ctx.actor, source)
        val current = register(state, w.register)
        if (w.register == SyncEngine.REGISTER_SEEN && w.base.rev != current.rev &&
            current.source == SOURCE_PASSIVE && next.source == SOURCE_PASSIVE && current.value == JsonPrimitive(SyncEngine.VALUE_UNSEEN)
        ) {
            next = next.copy(value = JsonPrimitive(SyncEngine.VALUE_SEEN))
        }
        var base = w.base.rev
        if (current.rev == revision) base = current.rev
        if (!wins(w.register, current, next, base)) {
            return result.copy(status = SyncEngine.STATUS_SUPERSEDED)
        }
        val updated = when (w.register) {
            SyncEngine.REGISTER_SEEN -> {
                var shelf = state.shelf
                if (next.value == JsonPrimitive(SyncEngine.VALUE_UNSEEN) && shelf.value == JsonPrimitive(SyncEngine.VALUE_DONE)) {
                    shelf = next.copy(value = JsonPrimitive(SyncEngine.VALUE_NONE))
                }
                state.copy(seen = next, shelf = shelf)
            }
            SyncEngine.REGISTER_SHELF -> state.copy(shelf = next)
            SyncEngine.REGISTER_KEPT -> state.copy(kept = next)
            else -> throw VectorException(VectorError.INVALID)
        }
        updated.copy(stateRev = revision).also { result = result.copy(state = it) }
        return result.copy(status = SyncEngine.STATUS_APPLIED)
    }

    private fun validateWrite(ctx: VectorContext, w: StateWrite, current: ch.lkmc.earnest.core.Revision) {
        if (w.base.generation != current.generation) throw VectorException(VectorError.GENERATION)
        if (ctx.actor <= 0 || w.opId.isEmpty() || w.principal <= 0 || w.entryId <= 0) throw VectorException(VectorError.INVALID)
        if (!SyncEngine.validValue(w.register, w.value)) throw VectorException(VectorError.INVALID)
        if (w.base.rev > current.rev) throw VectorException(VectorError.INVALID)
        deriveSource(ctx.token, w.kind)
    }

    private fun wins(registerName: String, current: Register, next: Register, base: Long): Boolean {
        if (base == current.rev) return true
        if (registerName == SyncEngine.REGISTER_SEEN && current.source == SOURCE_PASSIVE && next.source == SOURCE_PASSIVE) {
            return next.value == JsonPrimitive(SyncEngine.VALUE_SEEN)
        }
        val currentRank = rank(current.source)
        val nextRank = rank(next.source)
        if (nextRank != currentRank) return nextRank > currentRank
        val currentAt = Instant.parse(current.occurredAt)
        val nextAt = Instant.parse(next.occurredAt)
        if (nextAt != currentAt) return nextAt.isAfter(currentAt)
        return next.rev > current.rev
    }

    private fun replay(key: OpKey): Result? {
        val old = operations[key] ?: return null
        val state = state(old.principal, old.entryId)
        val status = if (entries[old.entryId]?.deleted == true) SyncEngine.STATUS_GONE else old.status
        return Result(key.opId, status, state, true)
    }

    private fun subscriptions(scope: Scope): List<Long> {
        val ids = when (scope.kind) {
            SyncEngine.SCOPE_ALL -> entries.values.map { it.subscriptionId }.toSet()
            SyncEngine.SCOPE_SUBSCRIPTIONS -> {
                if (scope.ids.isNullOrEmpty()) throw VectorException(VectorError.INVALID)
                scope.ids.forEach { if (it <= 0) throw VectorException(VectorError.INVALID) }
                scope.ids.toSet()
            }
            SyncEngine.SCOPE_FOLDER -> {
                val folder = scope.ids?.singleOrNull() ?: throw VectorException(VectorError.INVALID)
                if (folder <= 0) throw VectorException(VectorError.INVALID)
                objects.values.filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted && it.folderId == folder }
                    .map { it.id }
                    .toSet()
            }
            else -> throw VectorException(VectorError.INVALID)
        }
        return ids.sorted()
    }

    fun throughAt(ts: Instant?): Long {
        if (ts == null) return seq
        var through = 0L
        for (entry in entries.values) {
            if (Instant.parse(entry.firstSeenAt) <= ts && entry.seq > through) through = entry.seq
        }
        return through
    }

    private fun append(change: VectorChange) {
        rev += 1
        changes += change.copy(rev = rev)
    }

    private fun register(state: State, name: String): Register = when (name) {
        SyncEngine.REGISTER_SEEN -> state.seen
        SyncEngine.REGISTER_SHELF -> state.shelf
        SyncEngine.REGISTER_KEPT -> state.kept
        else -> throw VectorException(VectorError.INVALID)
    }

    data class Resources(
        val entries: List<VectorEntry>,
        val states: List<State>,
        val objects: List<VectorObject>,
        val positions: List<VectorPosition>,
    )

    companion object {
        private val ZERO_TIME: Instant = Instant.parse("0001-01-01T00:00:00Z")
        const val SOURCE_PASSIVE = "passive"
        const val SOURCE_RULE_OR_AGENT = "rule_or_agent"
        const val SOURCE_DELIBERATE = "deliberate"

        fun occurredAt(client: Instant, receipt: Instant): Instant = when {
            Duration.between(receipt, client) > Duration.ofHours(24) -> receipt
            client.isAfter(receipt) -> receipt
            else -> client
        }

        fun deriveSource(token: String, kind: String): String = when (token) {
            "agent", "rule" -> SOURCE_RULE_OR_AGENT
            "human", "compat" -> if (kind == "passive") SOURCE_PASSIVE else SOURCE_DELIBERATE
            else -> throw VectorException(VectorError.INVALID)
        }

        fun rank(source: String): Int = when (source) {
            SOURCE_DELIBERATE -> 3
            SOURCE_RULE_OR_AGENT -> 2
            SOURCE_PASSIVE -> 1
            else -> 0
        }

        fun googleWrite(
            generation: String,
            opId: String,
            principal: Long,
            entryId: Long,
            action: String,
            ctx: VectorContext,
        ): StateWrite {
            var w = StateWrite(
                opId = opId,
                principal = principal,
                entryId = entryId,
                register = SyncEngine.REGISTER_SEEN,
                value = JsonPrimitive(SyncEngine.VALUE_UNSEEN),
                base = ch.lkmc.earnest.core.Revision(generation, 0),
                occurredAt = ctx.receipt.toString(),
                kind = SyncEngine.KIND_EXPLICIT,
            )
            when (action) {
                "read" -> {
                    w = w.copy(register = SyncEngine.REGISTER_SEEN, value = JsonPrimitive(SyncEngine.VALUE_SEEN), kind = SyncEngine.KIND_PASSIVE)
                }
                "unread", "kept-unread" -> Unit
                "star" -> w = w.copy(register = SyncEngine.REGISTER_KEPT, value = JsonPrimitive(true))
                "unstar" -> w = w.copy(register = SyncEngine.REGISTER_KEPT, value = JsonPrimitive(false))
                else -> throw VectorException(VectorError.INVALID)
            }
            return w
        }
    }
}

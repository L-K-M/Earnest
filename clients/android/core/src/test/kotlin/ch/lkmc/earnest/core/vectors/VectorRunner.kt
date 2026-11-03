package ch.lkmc.earnest.core.vectors

import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.CachedPosition
import ch.lkmc.earnest.core.Pending
import ch.lkmc.earnest.core.RangeResult
import ch.lkmc.earnest.core.RangeWrite
import ch.lkmc.earnest.core.Result
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.Scope
import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.StateWrite
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncError
import ch.lkmc.earnest.core.SyncException
import ch.lkmc.earnest.core.SyncObject
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.core.SyncStore
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import kotlinx.serialization.json.longOrNull
import java.time.Duration
import java.time.Instant

@Serializable
data class VectorEntryInput(
    val id: Long = 0,
    @kotlinx.serialization.SerialName("subscription_id") val subscriptionId: Long = 0,
    val seq: Long = 0,
    @kotlinx.serialization.SerialName("published_at") val publishedAt: String? = null,
    val deleted: Boolean = false,
)

@Serializable
data class VectorRegisterInput(
    val value: JsonElement = JsonNull,
    val rev: Long = 0,
    @kotlinx.serialization.SerialName("occurred_at") val occurredAt: String = "0001-01-01T00:00:00Z",
    val actor: Long = 0,
    val source: String = "",
)

@Serializable
data class VectorStateInput(
    val principal: Long = 0,
    @kotlinx.serialization.SerialName("entry_id") val entryId: Long = 0,
    val seen: VectorRegisterInput = VectorRegisterInput(),
    val shelf: VectorRegisterInput = VectorRegisterInput(),
    val kept: VectorRegisterInput = VectorRegisterInput(),
    @kotlinx.serialization.SerialName("state_rev") val stateRev: Long = 0,
)

@Serializable
data class VectorWriteInput(
    @kotlinx.serialization.SerialName("op_id") val opId: String? = null,
    val principal: Long? = null,
    @kotlinx.serialization.SerialName("entry_id") val entryId: Long? = null,
    val register: String? = null,
    val value: JsonElement? = null,
    @kotlinx.serialization.SerialName("base_rev") val base: Revision? = null,
    @kotlinx.serialization.SerialName("occurred_at") val occurredAt: String? = null,
    val kind: String? = null,
)

@Serializable
data class VectorObjectInput(
    val kind: String,
    val id: Long,
    val principal: Long? = null,
    @kotlinx.serialization.SerialName("entry_id") val entryId: Long? = null,
    @kotlinx.serialization.SerialName("folder_id") val folderId: Long? = null,
    val name: String? = null,
    val rev: Long? = null,
    val deleted: Boolean? = null,
)

@Serializable
data class VectorPositionInput(
    val principal: Long? = null,
    val view: String? = null,
    val seq: Long? = null,
    val rev: Long? = null,
)

@Serializable
data class VectorStep(
    val op: String,
    val server: String? = null,
    val device: String? = null,
    val name: String? = null,
    val at: String? = null,
    val actor: Long? = null,
    val token: String? = null,
    val entry: VectorEntryInput? = null,
    val state: VectorStateInput? = null,
    val write: VectorWriteInput? = null,
    val writes: List<VectorWriteInput>? = null,
    val scope: Scope? = null,
    @kotlinx.serialization.SerialName("through_seq") val through: Long? = null,
    val ts: String? = null,
    val action: String? = null,
    val generation: String? = null,
    val cursor: Revision? = null,
    val rev: Long? = null,
    @kotlinx.serialization.SerialName("object") val objectInput: VectorObjectInput? = null,
    val position: VectorPositionInput? = null,
    val principal: Long? = null,
    @kotlinx.serialization.SerialName("entry_id") val entryId: Long? = null,
    @kotlinx.serialization.SerialName("op_id") val opId: String? = null,
    val count: Int? = null,
    val repeat: Int? = null,
    val expect: JsonElement = JsonNull,
    val error: String? = null,
)

@Serializable
data class VectorFixture(
    val version: Int,
    val name: String,
    val decision: String,
    val setup: List<VectorStep> = emptyList(),
    val operations: List<VectorStep> = emptyList(),
    val final: List<VectorStep> = emptyList(),
)

private class SavedResponse(
    val generation: String,
    val results: List<Result>? = null,
    val rangeResult: RangeResult? = null,
    val opId: String,
)

private class CapturedPage(
    val changes: List<ch.lkmc.earnest.core.SyncChange>,
    val next: Revision,
)

private class InMemoryStore(private var data: SyncSnapshot) : SyncStore {
    override fun load(): SyncSnapshot = data
    override fun save(snapshot: SyncSnapshot) {
        data = snapshot
    }
}

/**
 * Executes the shared fixtures against the real [SyncEngine]. Server steps run
 * on an in-memory model; client steps run through the engine. Expected values
 * are recursive subsets, arrays are exact and ordered.
 */
class VectorRunner {
    private val json = Json { ignoreUnknownKeys = false }

    data class StepResult(val value: JsonElement?, val error: VectorError?)

    fun execute(step: VectorStep): StepResult = try {
        StepResult(executeInner(step), null)
    } catch (error: VectorException) {
        StepResult(partial(step), error.error)
    } catch (error: SyncException) {
        StepResult(partial(step), if (error.error == SyncError.GENERATION) VectorError.GENERATION else VectorError.INVALID)
    } catch (error: IllegalArgumentException) {
        StepResult(partial(step), VectorError.INVALID)
    }

    private fun partial(step: VectorStep): JsonElement? {
        val model = servers[step.server ?: "main"]
        val client = step.device?.let { clients[it] }
        return partialResult(step, model, client)
    }

    /**
     * The fixture protocol returns the operation's zero or partial result
     * alongside a protocol error; expectations can assert it.
     */
    private fun partialResult(step: VectorStep, model: VectorServer?, client: SyncEngine?): JsonElement? = when (step.op) {
        "client_apply" -> client?.let { json.encodeToJsonElement(Revision.serializer(), it.snapshot().cursor) }
        "attempt_notification" -> model?.notification(step.actor ?: 1, step.opId.orEmpty())?.let {
            buildJsonObject { put("status", JsonPrimitive(it.status)) }
        }
        "changes", "capture_changes" -> buildJsonObject {
            put("changes", JsonNull)
            put("types", JsonNull)
            put("revs", JsonNull)
            put("next", json.encodeToJsonElement(Revision.serializer(), Revision("", 0)))
        }
        "client_ack", "accept_notification" -> buildJsonObject { }
        else -> null
    }

    private val servers = linkedMapOf<String, VectorServer>()
    private val clients = linkedMapOf<String, SyncEngine>()
    private val snapshots = linkedMapOf<String, VectorServer>()
    private val prepared = linkedMapOf<String, VectorEntryInput>()
    private val pages = linkedMapOf<String, CapturedPage>()
    private val responses = linkedMapOf<String, SavedResponse>()

    fun decode(text: String): VectorFixture = json.decodeFromString(VectorFixture.serializer(), text)

    private fun executeInner(step: VectorStep): JsonElement {
        val server = step.server ?: "main"
        val model = servers.getOrPut(server) { VectorServer("g1") }
        val ctx = VectorContext(
            actor = step.actor ?: 1L,
            token = step.token ?: "human",
            receipt = step.at?.let(Instant::parse) ?: DEFAULT_RECEIPT,
        )
        val principal = step.principal ?: 1L
        val client = step.device?.let { clients[it] }
        return when (step.op) {
            "ingest" -> model.ingest(entry(step), ctx.receipt).wire()
            "repeat_ingest" -> {
                repeat(step.repeat ?: 0) { index ->
                    val input = entry(step).copy(id = entry(step).id + index)
                    model.ingest(input, ctx.receipt)
                }
                buildJsonObject {
                    put("rev", JsonPrimitive(model.cursor().rev))
                    put("seq", JsonPrimitive(model.highestSeq()))
                }
            }
            "prepare_ingest" -> {
                prepared[step.name.orEmpty()] = entry(step)
                buildJsonObject {
                    put("seq", JsonPrimitive(model.highestSeq()))
                    put("rev", JsonPrimitive(model.cursor().rev))
                }
            }
            "commit_ingest" -> {
                val input = prepared.remove(step.name.orEmpty()) ?: throw VectorException(VectorError.INVALID)
                model.ingest(input, ctx.receipt).wire()
            }
            "update_entry" -> {
                model.updateEntry(step.entryId ?: 0, step.entry?.publishedAt?.let(Instant::parse) ?: ZERO_TIME)
                buildJsonObject { }
            }
            "delete" -> {
                model.deleteEntry(step.entryId ?: 0)
                buildJsonObject { }
            }
            "write" -> model.batch(ctx, listOf(defaults(step.write!!, principal, step, model))).single().wire()
            "batch", "cluster" -> {
                val writes = step.writes.orEmpty().map { defaults(it, principal, step, model) }
                JsonArray(model.batch(ctx, writes).map { it.wire() })
            }
            "range" -> {
                val through = step.through ?: model.throughAt(step.ts?.let(Instant::parse))
                val write = rangeDefaults(step.write ?: VectorWriteInput(), principal, step, model)
                model.through(ctx, RangeWrite(write, step.scope ?: Scope(SyncEngine.SCOPE_ALL), through)).wire()
            }
            "google" -> {
                ctx.token = "compat"
                val write = VectorServer.googleWrite(
                    model.generation,
                    step.opId.orEmpty(),
                    principal,
                    step.entryId ?: 0,
                    step.action.orEmpty(),
                    ctx,
                )
                model.batch(ctx, listOf(write)).single().wire()
            }
            "google_batch" -> {
                ctx.token = "compat"
                val writes = (0 until (step.repeat ?: 0)).map { index ->
                    VectorServer.googleWrite(
                        model.generation,
                        "${step.opId}-$index",
                        principal,
                        (step.entryId ?: 0) + index,
                        step.action.orEmpty(),
                        ctx,
                    )
                }
                val results = model.batch(ctx, writes)
                buildJsonObject {
                    put("applied", JsonPrimitive(results.count { it.status == SyncEngine.STATUS_APPLIED }))
                    put("superseded", JsonPrimitive(results.count { it.status == SyncEngine.STATUS_SUPERSEDED }))
                    put("gone", JsonPrimitive(results.count { it.status == SyncEngine.STATUS_GONE }))
                    put("expired", JsonPrimitive(results.count { it.status == SyncEngine.STATUS_EXPIRED }))
                }
            }
            "changes", "capture_changes" -> {
                val cursor = (step.cursor ?: Revision("", 0)).let { if (it.generation.isEmpty()) it.copy(generation = model.generation) else it }
                val (changes, next) = model.changes(cursor, 100000)
                if (step.op == "capture_changes") {
                    pages[step.name.orEmpty()] = CapturedPage(changes.map { it.toChange() }, next)
                }
                buildJsonObject {
                    put("changes", JsonArray(changes.map { it.wire() }))
                    put("types", JsonArray(changes.map { JsonPrimitive(it.type) }))
                    put("revs", JsonArray(changes.map { JsonPrimitive(it.rev) }))
                    put("next", json.encodeToJsonElement(Revision.serializer(), next))
                }
            }
            "retain" -> {
                model.retain(step.rev ?: 0)
                buildJsonObject { }
            }
            "checkpoint" -> {
                snapshots[step.name.orEmpty()] = model.checkpoint()
                buildJsonObject { put("rev", JsonPrimitive(model.cursor().rev)) }
            }
            "restore" -> {
                val snapshot = snapshots[step.name.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                servers[server] = model.restore(snapshot, step.generation.orEmpty())
                buildJsonObject { }
            }
            "object" -> {
                val input = step.objectInput!!
                model.putObject(
                    VectorObject(
                        kind = input.kind,
                        id = input.id,
                        principal = input.principal ?: 0,
                        entryId = input.entryId ?: 0,
                        folderId = input.folderId ?: 0,
                        name = input.name.orEmpty(),
                        rev = input.rev ?: 0,
                        deleted = input.deleted ?: false,
                    ),
                ).wire()
            }
            "position" -> {
                val input = step.position!!
                model.putPosition(
                    VectorPosition(
                        principal = input.principal ?: 0,
                        view = input.view.orEmpty(),
                        seq = input.seq ?: 0,
                        rev = input.rev ?: 0,
                    ),
                ).wire()
            }
            "accept_notification" -> {
                val notification = model.acceptNotification(ctx.actor, step.opId.orEmpty(), step.generation ?: model.generation)
                buildJsonObject {
                    put("status", JsonPrimitive(notification.status))
                    put("replay", JsonPrimitive(notification.replay))
                }
            }
            "attempt_notification" -> {
                val notification = model.attemptNotification(ctx.actor, step.opId.orEmpty(), ctx.receipt)
                buildJsonObject { put("status", JsonPrimitive(notification.status)) }
            }
            "complete_notification" -> {
                model.completeNotification(ctx.actor, step.opId.orEmpty())
                buildJsonObject { }
            }
            "release_notifications" -> {
                model.releaseNotifications()
                buildJsonObject { }
            }
            "bootstrap_begin" -> {
                val engine = clients.getOrPut(step.device.orEmpty()) { SyncEngine(InMemoryStore(SyncSnapshot(model.cursor()))) }
                engine.beginBootstrap(model.cursor())
                json.encodeToJsonElement(Revision.serializer(), engine.snapshot().cursor)
            }
            "bootstrap_page" -> {
                val engine = clients[step.device.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                val resources = model.resources()
                resources.entries.forEach { engine.mergeEntry(it.cached()) }
                resources.objects.forEach { engine.mergeObject(it.syncObject()) }
                resources.positions.forEach { engine.mergePosition(CachedPosition(it.principal, it.view, it.seq, it.rev)) }
                resources.states.forEach { engine.mergeState(it) }
                buildJsonObject { }
            }
            "client_pull" -> {
                val engine = clients[step.device.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                val cursor = engine.snapshot().cursor
                val (changes, next) = model.changes(cursor, 100000)
                engine.applyPage(changes.map { it.toChange() }, next)
                json.encodeToJsonElement(Revision.serializer(), engine.snapshot().cursor)
            }
            "client_apply" -> {
                val engine = clients[step.device.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                val page = pages[step.name.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                val count = step.count
                if (count != null && (count < 0 || count > page.changes.size)) throw VectorException(VectorError.INVALID)
                val changes = if (count == null) page.changes else page.changes.take(count)
                engine.applyPage(changes, page.next)
                json.encodeToJsonElement(Revision.serializer(), engine.snapshot().cursor)
            }
            "client_queue" -> {
                val engine = clients[step.device.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                engine.queue(defaults(step.write!!, principal, step, model))
                buildJsonObject { }
            }
            "client_queue_range" -> {
                val engine = clients[step.device.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                val through = step.through ?: model.throughAt(step.ts?.let(Instant::parse))
                val write = rangeDefaults(step.write ?: VectorWriteInput(), principal, step, model)
                engine.queueRange(RangeWrite(write, step.scope ?: Scope(SyncEngine.SCOPE_ALL), through))
                buildJsonObject { }
            }
            "client_merge_state" -> {
                val engine = clients[step.device.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                engine.mergeState(step.state?.toState() ?: throw VectorException(VectorError.INVALID))
                buildJsonObject { }
            }
            "client_submit" -> {
                val engine = clients[step.device.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                val outbox = engine.snapshot().outbox
                val head = outbox.firstOrNull() ?: throw VectorException(VectorError.INVALID)
                val generation = engine.snapshot().cursor.generation
                val range = head.range
                if (range != null) {
                    val result = model.through(ctx, range)
                    responses[step.name.orEmpty()] = SavedResponse(generation, rangeResult = result, opId = range.write.opId)
                    result.wire()
                } else {
                    val results = model.batch(ctx, listOf(head.operation))
                    responses[step.name.orEmpty()] = SavedResponse(generation, results = results, opId = head.operation.opId)
                    results.single().wire()
                }
            }
            "client_ack" -> {
                val engine = clients[step.device.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                val saved = responses[step.name.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                val range = saved.rangeResult
                if (range != null) {
                    engine.acknowledgeRange(saved.opId, saved.generation, range)
                } else {
                    engine.acknowledge(saved.generation, saved.results ?: emptyList())
                }
                buildJsonObject { }
            }
            "client_flush" -> {
                val engine = clients[step.device.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                val out = mutableListOf<JsonElement>()
                while (true) {
                    val pending = engine.snapshot().outbox
                    val head = pending.firstOrNull() ?: break
                    val range = head.range
                    if (range != null) {
                        val result = model.through(ctx, range)
                        engine.acknowledgeRange(range.write.opId, range.write.base.generation, result)
                        out += result.wire()
                        continue
                    }
                    val writes = mutableListOf<StateWrite>()
                    for (item in engine.snapshot().outbox) {
                        if (item.range != null) break
                        writes += item.operation
                    }
                    if (writes.isEmpty()) break
                    val results = model.batch(ctx, writes)
                    engine.acknowledge(writes.first().base.generation, results)
                    out += results.map { it.wire() }
                }
                JsonArray(out)
            }
            "assert_state" -> {
                val state = if (step.device != null) {
                    clients[step.device] ?: throw VectorException(VectorError.INVALID)
                    clients[step.device]!!.state(principal, step.entryId ?: 0)
                } else {
                    model.state(principal, step.entryId ?: 0)
                }
                buildJsonObject {
                    put("state", json.encodeToJsonElement(State.serializer(), state))
                    put("unread", JsonPrimitive(state.unread()))
                    put("google_read", JsonPrimitive(state.googleRead()))
                }
            }
            "assert_entry" -> {
                val id = step.entryId ?: 0
                val clientEntry = if (step.device != null) clients[step.device]!!.entry(id) else null
                val serverEntry = if (step.device == null) model.entry(id) else null
                val firstSeen = clientEntry?.firstSeenAt ?: serverEntry?.firstSeenAt
                buildJsonObject {
                    put("entry", clientEntry?.wireEntry() ?: serverEntry?.wireEntry() ?: JsonNull)
                    put("exists", JsonPrimitive(clientEntry != null || serverEntry != null))
                    put("age_seconds", firstSeen?.let {
                        JsonPrimitive(maxOf(0L, Duration.between(Instant.parse(it), ctx.receipt).seconds).toDouble())
                    } ?: JsonNull)
                }
            }
            "assert_object" -> {
                val input = step.objectInput!!
                val obj = if (step.device != null) {
                    clients[step.device]!!.findObject(input.kind, input.id)?.let {
                        json.encodeToJsonElement(SyncObject.serializer(), it)
                    }
                } else {
                    model.findObject(input.kind, input.id)?.wire()
                }
                buildJsonObject {
                    put("object", obj ?: JsonNull)
                    put("exists", JsonPrimitive(obj != null))
                }
            }
            "assert_position" -> {
                val view = step.position?.view.orEmpty()
                val position = if (step.device != null) {
                    clients[step.device]!!.position(principal, view)
                } else {
                    model.position(principal, view)?.let { CachedPosition(it.principal, it.view, it.seq, it.rev) }
                }
                buildJsonObject {
                    put("position", position?.let { json.encodeToJsonElement(CachedPosition.serializer(), it) } ?: JsonNull)
                    put("exists", JsonPrimitive(position != null))
                }
            }
            "assert_client" -> {
                val engine = clients[step.device.orEmpty()] ?: throw VectorException(VectorError.INVALID)
                buildJsonObject {
                    put("outbox_size", JsonPrimitive(engine.snapshot().outbox.size))
                    put("cursor", json.encodeToJsonElement(Revision.serializer(), engine.snapshot().cursor))
                }
            }
            "assert_log" -> {
                val log = model.log()
                var gapless = true
                var last = 0L
                log.forEach { if (it.rev != last + 1) gapless = false; last = it.rev }
                buildJsonObject {
                    put("rev", JsonPrimitive(model.cursor().rev))
                    put("seq", JsonPrimitive(model.highestSeq()))
                    put("generation", JsonPrimitive(model.generation))
                    put("revs", JsonArray(log.map { JsonPrimitive(it.rev) }))
                    put("types", JsonArray(log.map { JsonPrimitive(it.type) }))
                    put("gapless", JsonPrimitive(gapless))
                    put("count", JsonPrimitive(log.size))
                }
            }
            else -> throw VectorException(VectorError.INVALID)
        }
    }

    private fun entry(step: VectorStep): VectorEntryInput = step.entry ?: VectorEntryInput(0)

    private fun defaults(write: VectorWriteInput, principal: Long, step: VectorStep, model: VectorServer): StateWrite {
        val generation = model.generation
        return StateWrite(
            opId = write.opId.orEmpty(),
            principal = write.principal ?: principal,
            entryId = write.entryId ?: step.entryId ?: 0,
            register = write.register ?: "",
            value = write.value ?: JsonNull,
            base = Revision(write.base?.generation ?: generation, write.base?.rev ?: 0),
            occurredAt = write.occurredAt ?: DEFAULT_RECEIPT.toString(),
            kind = write.kind ?: SyncEngine.KIND_EXPLICIT,
        )
    }

    private fun rangeDefaults(write: VectorWriteInput, principal: Long, step: VectorStep, model: VectorServer): StateWrite {
        val generation = model.generation
        return StateWrite(
            opId = write.opId.orEmpty(),
            principal = write.principal ?: principal,
            entryId = write.entryId ?: step.entryId ?: 0,
            register = write.register ?: SyncEngine.REGISTER_SEEN,
            value = write.value ?: JsonPrimitive(SyncEngine.VALUE_SEEN),
            base = Revision(write.base?.generation ?: generation, write.base?.rev ?: 0),
            occurredAt = write.occurredAt ?: DEFAULT_RECEIPT.toString(),
            kind = write.kind ?: SyncEngine.KIND_EXPLICIT,
        )
    }

    private fun VectorStateInput.toState(): State = State(
        principal = principal,
        entryId = entryId,
        seen = seen.toRegister(),
        shelf = shelf.toRegister(),
        kept = kept.toRegister(),
        stateRev = stateRev,
    )

    private fun VectorRegisterInput.toRegister(): ch.lkmc.earnest.core.Register = ch.lkmc.earnest.core.Register(
        value = value,
        rev = rev,
        occurredAt = occurredAt,
        actor = actor,
        source = source,
    )

    private fun VectorEntry.wire(): JsonObject = buildJsonObject {
        put("id", JsonPrimitive(id))
        put("subscription_id", JsonPrimitive(subscriptionId))
        put("seq", JsonPrimitive(seq))
        put("first_seen_at", JsonPrimitive(firstSeenAt))
        put("published_at", JsonPrimitive(publishedAt))
        put("rev", JsonPrimitive(rev))
        put("deleted", JsonPrimitive(deleted))
    }

    private fun VectorEntryInput.cached(): CachedEntry = CachedEntry(
        id = id,
        subscriptionId = subscriptionId,
        seq = seq,
        firstSeenAt = "",
        publishedAt = publishedAt,
        rev = 0,
        deleted = deleted,
    )

    private fun VectorEntry.cached(): CachedEntry = CachedEntry(
        id = id,
        subscriptionId = subscriptionId,
        seq = seq,
        firstSeenAt = firstSeenAt,
        publishedAt = publishedAt,
        rev = rev,
        deleted = deleted,
    )

    private fun VectorObject.syncObject(): SyncObject = SyncObject(
        kind = kind,
        id = id,
        principal = principal,
        entryId = entryId,
        folderId = folderId,
        name = name,
        rev = rev,
        deleted = deleted,
    )

    private fun VectorObject.wire(): JsonObject = buildJsonObject {
        put("kind", JsonPrimitive(kind))
        put("id", JsonPrimitive(id))
        put("principal", JsonPrimitive(principal))
        put("entry_id", JsonPrimitive(entryId))
        put("folder_id", JsonPrimitive(folderId))
        put("name", JsonPrimitive(name))
        put("rev", JsonPrimitive(rev))
        put("deleted", JsonPrimitive(deleted))
    }

    private fun VectorPosition.wire(): JsonObject = buildJsonObject {
        put("principal", JsonPrimitive(principal))
        put("view", JsonPrimitive(view))
        put("seq", JsonPrimitive(seq))
        put("rev", JsonPrimitive(rev))
    }

    private fun Result.wire(): JsonObject = json.encodeToJsonElement(Result.serializer(), this) as JsonObject

    private fun RangeResult.wire(): JsonObject = json.encodeToJsonElement(RangeResult.serializer(), this) as JsonObject

    private fun VectorChange.wire(): JsonObject = json.encodeToJsonElement(VectorChange.serializer(), this) as JsonObject

    private fun VectorChange.toChange(): ch.lkmc.earnest.core.SyncChange = ch.lkmc.earnest.core.SyncChange(
        rev = rev,
        entry = entry?.cached(),
        states = results?.map { it.state } ?: range?.results?.map { it.state }.orEmpty(),
        obj = object_?.syncObject(),
        position = position?.let { CachedPosition(it.principal, it.view, it.seq, it.rev) },
    )

    private fun State.unread(): Boolean =
        seen.value == JsonPrimitive(SyncEngine.VALUE_UNSEEN) && shelf.value != JsonPrimitive(SyncEngine.VALUE_DONE)

    private fun State.googleRead(): Boolean =
        seen.value == JsonPrimitive(SyncEngine.VALUE_SEEN) || shelf.value == JsonPrimitive(SyncEngine.VALUE_DONE)

    private fun CachedEntry.wireEntry(): JsonObject = buildJsonObject {
        put("id", JsonPrimitive(id))
        put("subscription_id", JsonPrimitive(subscriptionId))
        put("seq", JsonPrimitive(seq))
        put("first_seen_at", JsonPrimitive(firstSeenAt))
        put("published_at", JsonPrimitive(publishedAt ?: ""))
        put("rev", JsonPrimitive(rev))
        put("deleted", JsonPrimitive(deleted))
    }

    private fun VectorEntry.wireEntry(): JsonObject = buildJsonObject {
        put("id", JsonPrimitive(id))
        put("subscription_id", JsonPrimitive(subscriptionId))
        put("seq", JsonPrimitive(seq))
        put("first_seen_at", JsonPrimitive(firstSeenAt))
        put("published_at", JsonPrimitive(publishedAt))
        put("rev", JsonPrimitive(rev))
        put("deleted", JsonPrimitive(deleted))
    }

    companion object {
        val DEFAULT_RECEIPT: Instant = Instant.parse("2026-11-02T12:00:00Z")
        private val ZERO_TIME: Instant = Instant.parse("0001-01-01T00:00:00Z")
    }
}

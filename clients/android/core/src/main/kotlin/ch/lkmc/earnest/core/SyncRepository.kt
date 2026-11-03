package ch.lkmc.earnest.core

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull

/** A server-rejected operation the UI should report to the user. */
data class RejectedOperation(
    val opId: String,
    val status: String,
)

/**
 * Coordinates HTTP pages and the durable sync engine. Call off the UI thread.
 * A 410 from the change feed re-bootstraps and then replays the outbox once,
 * so intent queued before the retention gap is not lost.
 */
class SyncRepository(
    private val api: EarnestApi,
    private val engine: SyncEngine,
) {
    private val json = Json { ignoreUnknownKeys = true }

    suspend fun bootstrap() {
        val cursor = api.bootstrap()
        engine.beginBootstrap(cursor)
        for (kind in BOOTSTRAP_KINDS) {
            var afterId = 0L
            var afterPrincipal = 0L
            var afterView = ""
            do {
                val page = api.bootstrapPage(kind, afterId, afterPrincipal, afterView)
                page.items.orEmpty().forEach { engine.mergeBootstrap(resource(kind, it)) }
                afterId = page.nextId
                afterPrincipal = page.nextPrincipal
                afterView = page.nextView
                require(!page.hasMore || afterId != 0L || afterPrincipal != 0L || afterView.isNotEmpty()) {
                    "bootstrap cursor did not advance"
                }
            } while (page.hasMore)
        }
        engine.flush()
    }

    suspend fun pull() {
        if (engine.snapshot().cursor.generation.isEmpty()) bootstrap()
        while (true) {
            val cursor = engine.snapshot().cursor
            val page = try {
                api.changes(cursor.generation, cursor.rev)
            } catch (error: ApiException) {
                if (!error.isGone) throw error
                bootstrap()
                flush()
                continue
            }
            val changes = page.changes.orEmpty()
            engine.applyPage(changes.map(::change), page.next)
            engine.flush()
            if (changes.isEmpty()) return
        }
    }

    /**
     * Sends the outbox in order: consecutive register writes become one batch,
     * ranges go individually. Responses are acknowledged only under the
     * generation the request was sent in.
     */
    suspend fun flush(): List<RejectedOperation> {
        val rejected = mutableListOf<RejectedOperation>()
        while (true) {
            val item = engine.snapshot().outbox.firstOrNull() ?: return rejected
            val generation = item.operation.base.generation
            val range = item.range
            if (range != null) {
                engine.markInFlight(range.write.opId)
                val result = api.through(ThroughBody(range.write, range.scope, range.throughSeq))
                engine.acknowledgeRange(range.write.opId, generation, result)
                if (result.status == SyncEngine.STATUS_EXPIRED || result.status == SyncEngine.STATUS_GONE) {
                    rejected += RejectedOperation(range.write.opId, result.status)
                }
                continue
            }

            val writes = mutableListOf<StateWrite>()
            while (writes.size < BATCH_LIMIT) {
                val next = engine.snapshot().outbox.firstOrNull() ?: break
                if (next.range != null) break
                if (next.operation.base.generation != generation) break
                writes += next.operation
                engine.markInFlight(next.operation.opId)
            }
            require(writes.isNotEmpty()) { "outbox advanced without a request" }
            val results = api.batch(writes)
            require(results.size == writes.size) { "batch acknowledgement size mismatch" }
            engine.acknowledge(generation, results)
            results.filter { it.status == SyncEngine.STATUS_GONE || it.status == SyncEngine.STATUS_EXPIRED }
                .forEach { rejected += RejectedOperation(it.opId, it.status) }
        }
        engine.flush()
    }

    /** Drops queue items older than 30 days and returns them for reporting. */
    fun expire(now: java.time.Instant): List<Pending> = engine.expire(now)

    private fun resource(kind: String, value: JsonElement): SyncChange {
        val item = value.jsonObject
        return when (kind) {
            "entries" -> SyncChange(0, entry = cachedEntry(item, value))
            "state" -> SyncChange(0, states = listOf(json.decodeFromJsonElement(State.serializer(), value)))
            "positions" -> SyncChange(
                0,
                position = CachedPosition(
                    long(item, "principal"),
                    string(item, "view"),
                    long(item, "seq"),
                    long(item, "rev"),
                ),
            )
            "subscriptions" -> SyncChange(
                0,
                obj = SyncObject(
                    kind = SyncEngine.OBJECT_SUBSCRIPTION,
                    id = long(item, "id"),
                    folderId = long(item, "folder_id"),
                    name = string(item, "title"),
                    rev = long(item, "rev"),
                    deleted = boolean(item, "deleted"),
                    raw = value.toString(),
                ),
            )
            "folders" -> SyncChange(
                0,
                obj = SyncObject(
                    kind = SyncEngine.OBJECT_FOLDER,
                    id = long(item, "id"),
                    name = string(item, "name"),
                    rev = long(item, "rev"),
                    deleted = boolean(item, "deleted"),
                    raw = value.toString(),
                ),
            )
            "labels" -> SyncChange(
                0,
                obj = SyncObject(
                    kind = OBJECT_LABEL_DEFINITION,
                    id = long(item, "id"),
                    name = string(item, "name"),
                    rev = long(item, "rev"),
                    deleted = boolean(item, "deleted"),
                    raw = value.toString(),
                ),
            )
            "label_memberships" -> SyncChange(
                0,
                obj = SyncObject(
                    kind = SyncEngine.OBJECT_LABEL,
                    id = long(item, "id"),
                    principal = long(item, "principal"),
                    entryId = long(item, "entry_id"),
                    name = string(item, "name"),
                    rev = long(item, "rev"),
                    deleted = boolean(item, "deleted"),
                    raw = value.toString(),
                ),
            )
            else -> error("unknown bootstrap resource: $kind")
        }
    }

    private fun change(record: ChangeRecord): SyncChange {
        val payload = record.payload.jsonObject
        val entry = payload["entry"]?.let { cachedEntry(it.jsonObject, it) }
        val position = payload["position"]?.let {
            val item = it.jsonObject
            CachedPosition(long(item, "principal"), string(item, "view"), long(item, "seq"), long(item, "rev"))
        }
        val obj = payload["object"]?.let { parseObject(it) }
        val results = payload["results"] ?: payload["range"]?.jsonObject?.get("results")
        val states = results?.takeUnless { it is JsonNull }?.let { element ->
            json.decodeFromJsonElement(
                kotlinx.serialization.builtins.ListSerializer(Result.serializer()),
                element,
            )
        }.orEmpty().map { it.state }
        return SyncChange(record.rev, entry, states, obj, position)
    }

    /** Parses a protocol object and keeps its native companion projection in [SyncObject.raw]. */
    private fun parseObject(value: JsonElement): SyncObject {
        val item = value.jsonObject
        val kind = string(item, "kind")
        val companion = when (kind) {
            SyncEngine.OBJECT_SUBSCRIPTION -> item["subscription"]
            SyncEngine.OBJECT_FOLDER -> item["folder"]
            SyncEngine.OBJECT_LABEL -> item["label"]
            SyncEngine.OBJECT_ANNOTATION -> item["annotation"]
            else -> null
        }
        return SyncObject(
            kind = kind,
            id = long(item, "id"),
            principal = long(item, "principal"),
            entryId = long(item, "entry_id"),
            folderId = long(item, "folder_id"),
            name = string(item, "name"),
            rev = long(item, "rev"),
            deleted = boolean(item, "deleted"),
            raw = companion?.toString() ?: value.toString(),
        )
    }

    private fun cachedEntry(item: JsonObject, raw: JsonElement): CachedEntry = CachedEntry(
        id = long(item, "id"),
        subscriptionId = long(item, "subscription_id"),
        feedId = long(item, "feed_id"),
        seq = long(item, "seq"),
        firstSeenAt = string(item, "first_seen_at"),
        publishedAt = item["published_at"]?.jsonPrimitive?.contentOrNull,
        updatedAt = item["updated_at"]?.jsonPrimitive?.contentOrNull,
        title = string(item, "title"),
        url = string(item, "url"),
        author = string(item, "author"),
        rev = long(item, "rev"),
        deleted = boolean(item, "deleted"),
        raw = raw.toString(),
    )

    private fun long(item: JsonObject, key: String): Long = item[key]?.jsonPrimitive?.longOrNull ?: 0L

    private fun string(item: JsonObject, key: String): String = item[key]?.jsonPrimitive?.contentOrNull ?: ""

    private fun boolean(item: JsonObject, key: String): Boolean = item[key]?.jsonPrimitive?.booleanOrNull ?: false

    companion object {
        private val BOOTSTRAP_KINDS = listOf(
            "entries",
            "subscriptions",
            "folders",
            "labels",
            "label_memberships",
            "positions",
            "state",
        )
        private const val OBJECT_LABEL_DEFINITION = "label_definition"
        private const val BATCH_LIMIT = 100
    }
}

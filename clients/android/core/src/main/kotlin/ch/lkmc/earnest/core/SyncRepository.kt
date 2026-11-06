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
import java.io.IOException

/** A server-rejected operation the UI should report to the user. */
data class RejectedOperation(
    val opId: String,
    val status: String,
)

/**
 * Runs one read-through-commit section against the durable engine. The app
 * implements this over its snapshot lock so the repository's HTTP waits never
 * serialize behind other writers; a bound engine answers every section itself.
 */
interface EngineSession {
    suspend operator fun <T> invoke(section: suspend (SyncEngine) -> T): T
}

/**
 * Coordinates HTTP pages and the durable sync engine. Call off the UI thread.
 * A 410 from the change feed re-bootstraps and then replays the outbox once,
 * so intent queued before the retention gap is not lost.
 */
class SyncRepository(
    private val api: EarnestApi,
    private val session: EngineSession,
) {
    /** Binds one engine for callers that already own its serialization. */
    constructor(api: EarnestApi, engine: SyncEngine) : this(
        api,
        object : EngineSession {
            override suspend fun <T> invoke(section: suspend (SyncEngine) -> T): T = section(engine)
        },
    )

    private val json = Json { ignoreUnknownKeys = true }

    suspend fun bootstrap() {
        val cursor = api.bootstrap()
        // Stage the replacement so an interrupted bootstrap retains its old cursor and cache.
        val base = session { it.snapshot() }
        val staged = SyncEngine(object : SyncStore {
            private var data = base
            override fun load() = data
            override fun save(snapshot: SyncSnapshot) { data = snapshot }
        })
        staged.beginBootstrap(cursor)
        for (kind in BOOTSTRAP_KINDS) {
            var afterId = 0L
            var afterPrincipal = 0L
            var afterView = ""
            do {
                val page = api.bootstrapPage(kind, afterId, afterPrincipal, afterView)
                page.items.orEmpty().forEach { staged.mergeBootstrap(resource(kind, it)) }
                afterId = page.nextId
                afterPrincipal = page.nextPrincipal
                afterView = page.nextView
                require(!page.hasMore || afterId != 0L || afterPrincipal != 0L || afterView.isNotEmpty()) {
                    "bootstrap cursor did not advance"
                }
            } while (page.hasMore)
        }
        riverResources(RiverRefresh.REQUIRED)?.let { (mutes, settings) ->
            staged.replaceRiverResources(mutes, settings)
        }
        session { it.installBootstrap(staged.snapshot()); it.flush() }
    }

    /** Pulls pages and reports operations rejected during retention-gap recovery. */
    suspend fun pull(): List<RejectedOperation> {
        val rejected = mutableListOf<RejectedOperation>()
        if (session { it.snapshot().cursor.generation }.isEmpty()) {
            bootstrap()
        } else {
            riverResources(RiverRefresh.ALLOW_CACHED)?.let { (mutes, settings) ->
                session { it.replaceRiverResources(mutes, settings); it.flush() }
            }
        }
        while (true) {
            val cursor = session { it.snapshot().cursor }
            val page = try {
                api.changes(cursor.generation, cursor.rev)
            } catch (error: ApiException) {
                if (!error.isGone) throw error
                bootstrap()
                rejected += flush()
                continue
            }
            val changes = page.changes.orEmpty().map { change(it) }
            session { it.applyPage(changes, page.next); it.flush() }
            if (changes.isEmpty()) return rejected
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
            val item = session { it.snapshot().outbox.firstOrNull() } ?: return rejected
            val generation = item.operation.base.generation
            val range = item.range
            if (range != null) {
                session { it.markInFlight(range.write.opId); it.flush() }
                val result = api.through(ThroughBody(range.write, range.scope, range.throughSeq))
                session { it.acknowledgeRange(range.write.opId, generation, result); it.flush() }
                if (result.status == SyncEngine.STATUS_EXPIRED || result.status == SyncEngine.STATUS_GONE) {
                    rejected += RejectedOperation(range.write.opId, result.status)
                }
                continue
            }

            val writes = session { engine ->
                val writes = mutableListOf<StateWrite>()
                val targets = mutableSetOf<Triple<Long, Long, String>>()
                val ids = mutableSetOf<String>()
                for (next in engine.snapshot().outbox.take(BATCH_LIMIT)) {
                    if (next.range != null) break
                    val operation = next.operation
                    if (operation.base.generation != generation) break
                    if (!targets.add(Triple(operation.principal, operation.entryId, operation.register))) break
                    if (!ids.add(operation.opId)) break
                    writes += operation
                    engine.markInFlight(operation.opId)
                }
                engine.flush()
                writes
            }
            require(writes.isNotEmpty()) { "outbox advanced without a request" }
            val results = api.batch(writes)
            require(results.size == writes.size) { "batch acknowledgement size mismatch" }
            session { it.acknowledge(generation, results); it.flush() }
            results.filter { it.status == SyncEngine.STATUS_GONE || it.status == SyncEngine.STATUS_EXPIRED }
                .forEach { rejected += RejectedOperation(it.opId, it.status) }
        }
    }

    /** Drops queue items older than 30 days and returns them for reporting. */
    // Expire must persist like every other mutation: without the flush the
    // dropped operations reload from the store after process death.
    suspend fun expire(now: java.time.Instant): List<Pending> = session { engine -> engine.expire(now).also { engine.flush() } }

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

    private suspend fun change(record: ChangeRecord): SyncChange {
        val payload = record.payload.jsonObject
        val entry = payload["entry"]?.let { hydrate(cachedEntry(it.jsonObject, it)) }
        val position = payload["position"]?.let {
            val item = it.jsonObject
            CachedPosition(long(item, "principal"), string(item, "view"), long(item, "seq"), long(item, "rev"))
        }
        val obj = payload["mute"]?.let { riverResource(River.MUTE, it) }
            ?: payload["settings"]?.let { riverResource(River.SETTINGS, it) }
            ?: payload["object"]?.let { parseObject(it, payload) }
        val results = payload["results"] ?: payload["range"]?.jsonObject?.get("results")
        val states = results?.takeUnless { it is JsonNull }?.let { element ->
            json.decodeFromJsonElement(
                kotlinx.serialization.builtins.ListSerializer(Result.serializer()),
                element,
            )
        }.orEmpty().map { it.state }
        return SyncChange(record.rev, entry, states, obj, position)
    }

    private enum class RiverRefresh { REQUIRED, ALLOW_CACHED }

    /** Fetches the active river resources; a transient failure keeps the cached set. */
    private suspend fun riverResources(mode: RiverRefresh): Pair<List<SyncObject>, SyncObject>? {
        val (mutes, settings) = try {
            api.mutes() to api.settings()
        } catch (error: IOException) {
            if (mode == RiverRefresh.REQUIRED || (error is ApiException && error.status !in 500..599)) throw error

            // Keep both cached resources on a transient failure. The change feed below
            // still replays their updates, and the next pull retries the refresh.
            return null
        }
        return mutes.map { riverResource(River.MUTE, it) } to riverResource(River.SETTINGS, settings)
    }

    private fun riverResource(kind: String, value: JsonElement): SyncObject {
        val item = value.jsonObject
        return SyncObject(kind, if (kind == River.SETTINGS) 1 else long(item, "id"),
            rev = long(item, "rev"), deleted = boolean(item, "deleted"), raw = value.toString())
    }

    /** Parses a protocol object and keeps its native companion projection in [SyncObject.raw]. */
    private fun parseObject(value: JsonElement, payload: JsonObject): SyncObject {
        val item = value.jsonObject
        val kind = string(item, "kind")
        val companion = when (kind) {
            SyncEngine.OBJECT_SUBSCRIPTION -> payload["subscription"]
            SyncEngine.OBJECT_FOLDER -> payload["folder"]
            SyncEngine.OBJECT_LABEL -> payload["label"]
            SyncEngine.OBJECT_ANNOTATION -> payload["annotation"]
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

    /** Resolves a deep link absent from the snapshot, hiding a 404 until a newer projection. */
    suspend fun loadEntry(id: Long): CachedEntry? {
        val cached = session { it.snapshot().entries.firstOrNull { entry -> entry.id == id } }
        if (cached != null) return cached.takeUnless { it.deleted }

        val entry = hydrate(CachedEntry(id))
        session { it.mergeEntry(entry); it.flush() }
        return entry.takeUnless { it.deleted }
    }

    private suspend fun hydrate(entry: CachedEntry): CachedEntry {
        if (entry.deleted) return entry
        return try {
            val value = json.encodeToJsonElement(Entry.serializer(), api.entry(entry.id))
            cachedEntry(value.jsonObject, value)
        } catch (error: ApiException) {
            if (error.status != 404) throw error
            entry.copy(deleted = true, unavailable = true)
        }
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

package ch.lkmc.earnest.core

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.*
import java.net.URLEncoder
import java.nio.charset.StandardCharsets

@Serializable
private data class BootstrapPage(val items: List<JsonElement>?, val next_id: Long, val next_principal: Long, val next_view: String, val has_more: Boolean)

data class RejectedOperation(val opId: String, val status: String)

/** Coordinates HTTP pages and the durable sync engine. Call off the UI thread. */
class SyncRepository(private val api: EarnestApi, private val engine: SyncEngine) {
    private val json = Json { ignoreUnknownKeys = true }

    fun bootstrap() {
        val cursor = json.decodeFromJsonElement<Revision>(api.request("GET", "/v1/bootstrap"))
        engine.beginBootstrap(cursor)
        for (kind in listOf("entries", "subscriptions", "folders", "labels", "label_memberships", "positions", "state")) {
            var id = 0L
            var principal = 0L
            var view = ""
            do {
                val path = "/v1/bootstrap/$kind?after_id=$id&after_principal=$principal&after_view=${URLEncoder.encode(view, StandardCharsets.UTF_8)}"
                val page = json.decodeFromJsonElement<BootstrapPage>(api.request("GET", path))
                page.items.orEmpty().forEach { engine.mergeBootstrap(resource(kind, it)) }
                if (page.has_more) require(page.next_id != id || page.next_principal != principal || page.next_view != view) { "bootstrap cursor did not advance" }
                id = page.next_id
                principal = page.next_principal
                view = page.next_view
            } while (page.has_more)
        }
        engine.finishBootstrap()
    }

    fun pull() {
        if (engine.snapshot().bootstrapping || engine.snapshot().cursor.generation.isEmpty()) bootstrap()
        while (true) {
            val cursor = engine.snapshot().cursor
            val generation = URLEncoder.encode(cursor.generation, StandardCharsets.UTF_8)
            val page = try {
                json.decodeFromJsonElement<ChangesPage>(api.request("GET", "/v1/changes?generation=$generation&rev=${cursor.rev}"))
            } catch (error: ApiException) {
                if (error.status != 410) throw error
                bootstrap()
                continue
            }
            val changes = page.changes.orEmpty()
            engine.applyPage(changes.map(::change), page.next)
            if (changes.isEmpty()) return
        }
    }

    /** Returns server-rejected operations for display to the user. */
    fun flush(): List<RejectedOperation> {
        val rejected = mutableListOf<RejectedOperation>()
        while (engine.snapshot().outbox.isNotEmpty()) {
            val item = engine.snapshot().outbox.first()
            engine.markInFlight(item.write.opId)
            val generation = item.write.base.generation
            val results = if (item.scope == null) {
                val body = json.encodeToJsonElement(listOf(item.write))
                json.decodeFromJsonElement<List<Result>>(api.request("POST", "/v1/state:batch", body)).also {
                    require(it.size == 1 && it.single().opId == item.write.opId) { "unexpected batch acknowledgement" }
                }
            } else {
                val body = buildJsonObject {
                    put("write", json.encodeToJsonElement(item.write))
                    put("scope", json.encodeToJsonElement(item.scope))
                    put("through_seq", item.throughSeq)
                }
                val response = api.request("POST", "/v1/state:through", body).jsonObject
                val status = response.getValue("status").jsonPrimitive.content
                if (status == "gone" || status == "expired") rejected += RejectedOperation(item.write.opId, status)
                response["results"]?.takeUnless { it is JsonNull }?.let { json.decodeFromJsonElement<List<Result>>(it) }.orEmpty()
            }
            rejected += results.filter { it.status == "gone" || it.status == "expired" }.map { RejectedOperation(it.opId, it.status) }
            engine.acknowledge(generation, item.write.opId, results.map { it.state })
        }
        return rejected
    }

    private fun resource(kind: String, value: JsonElement): SyncChange {
        val item = value.jsonObject
        fun number(name: String): Long = item[name]?.jsonPrimitive?.longOrNull ?: 0
        fun boolean(name: String): Boolean = item[name]?.jsonPrimitive?.booleanOrNull ?: false
        val rev = number("rev")
        return when (kind) {
            "entries" -> SyncChange(rev, entry = CachedEntry(number("id"), number("subscription_id"), number("seq"), rev, boolean("deleted"), value.toString()))
            "state" -> SyncChange(rev, states = listOf(json.decodeFromJsonElement(value)))
            "positions" -> SyncChange(rev, position = CachedPosition(number("principal"), item.getValue("view").jsonPrimitive.content, rev, number("seq")))
            else -> {
                val type = when (kind) { "subscriptions" -> "subscription"; "folders" -> "folder"; "labels" -> "label_definition"; "label_memberships" -> "label"; else -> error("unknown bootstrap resource") }
                val id = number("id")
                SyncChange(rev, obj = CachedObject(type, id, rev, boolean("deleted"), number("folder_id"), value.toString()))
            }
        }
    }

    private fun change(record: ChangeRecord): SyncChange {
        val payload = record.payload.jsonObject
        val entry = payload["entry"]?.let { resource("entries", it).entry }
        val position = payload["position"]?.let { resource("positions", it).position }
        val obj = payload["object"]?.let {
            val o = it.jsonObject
            CachedObject(o.getValue("kind").jsonPrimitive.content, o.getValue("id").jsonPrimitive.long, o.getValue("rev").jsonPrimitive.long, o["deleted"]?.jsonPrimitive?.booleanOrNull ?: false, o["folder_id"]?.jsonPrimitive?.longOrNull ?: 0, it.toString())
        }
        val results = payload["results"] ?: payload["range"]?.jsonObject?.get("results")
        val states = results?.takeUnless { it is JsonNull }?.jsonArray?.map { json.decodeFromJsonElement<Result>(it).state }.orEmpty()
        require(entry != null || position != null || obj != null || results != null) { "unsupported change payload: ${record.type}" }
        return SyncChange(record.rev, entry, states, obj, position)
    }
}

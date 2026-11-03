package ch.lkmc.earnest.core

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException

@Serializable
data class Revision(val generation: String, val rev: Long)

@Serializable
data class Register(val value: JsonElement, val rev: Long, @SerialName("occurred_at") val occurredAt: String, val actor: Long, val source: String)

@Serializable
data class State(val principal: Long, @SerialName("entry_id") val entryId: Long, val seen: Register, val shelf: Register, val kept: Register, @SerialName("state_rev") val stateRev: Long)

@Serializable
data class StateWrite(@SerialName("op_id") val opId: String, val principal: Long, @SerialName("entry_id") val entryId: Long, val register: String, val value: JsonElement, @SerialName("base_rev") val base: Revision, @SerialName("occurred_at") val occurredAt: String, val kind: String)

@Serializable
data class Result(@SerialName("op_id") val opId: String, val status: String, val state: State, val replay: Boolean)

@Serializable
data class ChangeRecord(val rev: Long, val type: String, val payload: JsonElement)

@Serializable
data class ChangesPage(val changes: List<ChangeRecord>?, val next: Revision, @SerialName("next_cursor") val nextCursor: String)

class ApiException(val status: Int, message: String) : IOException(message)

/** HTTP transport for the repository; callers must keep bearer tokens out of logs. */
class EarnestApi(baseUrl: String, private val token: String? = null, client: OkHttpClient = OkHttpClient()) {
    private val base = baseUrl.toHttpUrl().also {
        require(it.isHttps && it.username.isEmpty() && it.password.isEmpty() && it.encodedPath == "/" && it.query == null && it.fragment == null) { "server must be an HTTPS origin" }
    }.toString().trimEnd('/')
    private val client = client.newBuilder().followRedirects(false).followSslRedirects(false).build()
    private val json = Json { ignoreUnknownKeys = true }

    fun request(method: String, path: String, body: JsonElement? = null): JsonElement {
        require(path.startsWith("/v1/"))
        val requestBody = body?.toString()?.toRequestBody("application/json".toMediaType())
        val builder = Request.Builder().url(base + path).method(method, requestBody)
        token?.let { builder.header("Authorization", "Bearer $it") }
        client.newCall(builder.build()).execute().use { response ->
            val text = response.body?.string().orEmpty()
            if (!response.isSuccessful) throw ApiException(response.code, "Earnest request failed (${response.code})")
            return if (text.isBlank()) kotlinx.serialization.json.JsonNull else json.parseToJsonElement(text)
        }
    }
}

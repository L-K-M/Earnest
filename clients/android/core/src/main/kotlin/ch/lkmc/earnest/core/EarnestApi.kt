package ch.lkmc.earnest.core

import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.Call
import okhttp3.Callback
import okhttp3.Response
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import kotlin.coroutines.resumeWithException
import java.io.IOException
import java.util.concurrent.TimeUnit

/** HTTP failure carrying the status and the problem `error` code, when present. */
class ApiException(
    val status: Int,
    val code: String?,
    message: String,
) : IOException(message) {
    val isGone: Boolean get() = status == 410
    val isUnauthorized: Boolean get() = status == 401
}

/**
 * HTTP transport for the native API. Callers must keep bearer tokens out of
 * logs. Every method requires an HTTPS origin without credentials, path,
 * query or fragment, so a server URL cannot smuggle a request elsewhere.
 */
class EarnestApi(
    baseUrl: String,
    private val token: String? = null,
    client: OkHttpClient = OkHttpClient(),
) {
    private val origin: HttpUrl = baseUrl.trim().trimEnd('/').toHttpUrl().also {
        require(it.isHttps && it.username.isEmpty() && it.password.isEmpty()) { "server must be an HTTPS origin" }
        require(it.encodedPath == "/" && it.query == null && it.fragment == null) { "server must be an HTTPS origin" }
    }
    private val http = client.newBuilder()
        .followRedirects(false)
        .followSslRedirects(false)
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .build()
    private val json = Json { ignoreUnknownKeys = true }
    // Search bodies always state the mode and omit absent filters; the server
    // treats a missing mode as lexical, but an explicit one keeps the wire
    // self-describing.
    private val encoder = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        explicitNulls = false
    }

    suspend fun get(url: HttpUrl): JsonElement = request("GET", url, null)

    suspend fun post(url: HttpUrl, body: JsonElement? = null): JsonElement = request("POST", url, body)

    suspend fun patch(url: HttpUrl, body: JsonElement): JsonElement = request("PATCH", url, body)

    suspend fun put(url: HttpUrl, body: JsonElement): JsonElement = request("PUT", url, body)

    suspend fun delete(url: HttpUrl): JsonElement = request("DELETE", url, null)

    private suspend fun request(method: String, url: HttpUrl, body: JsonElement?): JsonElement {
        val requestBody = body?.let { it.toString().toRequestBody(JSON_MEDIA_TYPE) }
        val builder = Request.Builder().url(url).method(method, requestBody)
        token?.let { builder.header("Authorization", "Bearer $it") }
        builder.header("Accept", "application/json")
        val call = http.newCall(builder.build())
        return suspendCancellableCoroutine { continuation ->
            continuation.invokeOnCancellation { call.cancel() }
            call.enqueue(object : Callback {
                override fun onFailure(call: Call, e: IOException) {
                    continuation.resumeWithException(e)
                }

                override fun onResponse(call: Call, response: Response) {
                    val result = runCatching {
                        response.use {
                            val text = it.body?.string().orEmpty()
                            if (!it.isSuccessful) throw problem(it.code, text)
                            if (text.isBlank()) JsonNull else json.parseToJsonElement(text)
                        }
                    }
                    continuation.resumeWith(result)
                }
            })
        }
    }

    private fun problem(status: Int, text: String): ApiException {
        val code = runCatching {
            json.parseToJsonElement(text).jsonObject["error"]?.jsonPrimitive?.content
        }.getOrNull()
        return ApiException(status, code, "Earnest request failed ($status)${code?.let { ": $it" }.orEmpty()}")
    }

    private inline fun <reified T> JsonElement.decode(): T = json.decodeFromString(toString())

    private fun url(path: String, block: (HttpUrl.Builder) -> Unit = {}): HttpUrl {
        val builder = origin.newBuilder().encodedPath(path)
        block(builder)
        return builder.build()
    }

    suspend fun bootstrap(): Revision = get(url("/v1/bootstrap")).decode()

    suspend fun bootstrapPage(
        kind: String,
        afterId: Long = 0,
        afterPrincipal: Long = 0,
        afterView: String = "",
    ): BootstrapPage {
        val url = url("/v1/bootstrap/$kind") {
            if (afterId != 0L) it.addQueryParameter("after_id", afterId.toString())
            if (afterPrincipal != 0L) it.addQueryParameter("after_principal", afterPrincipal.toString())
            if (afterView.isNotEmpty()) it.addQueryParameter("after_view", afterView)
        }
        return get(url).decode()
    }

    suspend fun changes(generation: String, rev: Long, limit: Int = 1000): ChangesPage {
        val url = url("/v1/changes") {
            it.addQueryParameter("generation", generation)
            it.addQueryParameter("rev", rev.toString())
            it.addQueryParameter("limit", limit.toString())
        }
        return get(url).decode()
    }

    suspend fun batch(writes: List<StateWrite>): List<Result> =
        post(url("/v1/state:batch"), json.encodeToJsonElement(kotlinx.serialization.builtins.ListSerializer(StateWrite.serializer()), writes)).decode()

    suspend fun through(body: ThroughBody): RangeResult =
        post(url("/v1/state:through"), json.encodeToJsonElement(ThroughBody.serializer(), body)).decode()

    suspend fun entries(
        view: String? = null,
        cadence: String? = null,
        subscriptionId: Long? = null,
        folderId: Long? = null,
        labelId: Long? = null,
        principalId: Long? = null,
        unreadOnly: Boolean? = null,
        afterSeq: Long? = null,
        beforeSeq: Long? = null,
        limit: Int = 50,
        direction: String? = null,
        cursor: String? = null,
    ): EntriesPage {
        val url = url("/v1/entries") {
            view?.let { value -> it.addQueryParameter("view", value) }
            cadence?.let { value -> it.addQueryParameter("cadence", value) }
            subscriptionId?.let { value -> it.addQueryParameter("subscription_id", value.toString()) }
            folderId?.let { value -> it.addQueryParameter("folder_id", value.toString()) }
            labelId?.let { value -> it.addQueryParameter("label_id", value.toString()) }
            principalId?.let { value -> it.addQueryParameter("principal_id", value.toString()) }
            unreadOnly?.let { value -> it.addQueryParameter("unread_only", value.toString()) }
            afterSeq?.let { value -> it.addQueryParameter("after_seq", value.toString()) }
            beforeSeq?.let { value -> it.addQueryParameter("before_seq", value.toString()) }
            it.addQueryParameter("limit", limit.toString())
            direction?.let { value -> it.addQueryParameter("direction", value) }
            cursor?.let { value -> it.addQueryParameter("cursor", value) }
        }
        return get(url).decode()
    }

    suspend fun entry(id: Long): Entry = get(url("/v1/entries/$id")).decode()

    suspend fun content(id: Long): Content = get(url("/v1/entries/$id/content")).decode()

    /**
     * Issues a signed content URL. [mode] selects the server's image
     * handling: omitted (or "proxied") serves inline images through the
     * image proxy; "click" rewrites each image into a click-to-load link.
     * In-app display needs proxied images, so callers leave [mode] unset;
     * the parameter remains for surfaces that deliberately want
     * click-to-load.
     */
    suspend fun contentUrl(id: Long, mode: String? = null): ContentUrl =
        get(url("/v1/entries/$id/content-url") { mode?.let { value -> it.addQueryParameter("mode", value) } }).decode()

    suspend fun mutes(): List<JsonElement> = get(url("/v1/mutes")).decode()

    suspend fun settings(): JsonElement = get(url("/v1/settings"))

    /**
     * Exact unread counts in one aggregate request. Callers must gate this to
     * unread-count reading mode and must never page entries to count them.
     */
    suspend fun unreadCounts(): UnreadCounts = get(url("/v1/entries:counts")).decode()

    /** The typed reader slice of `/v1/settings`; [settings] stays raw for sync. */
    suspend fun readerSettings(): ReaderSettings = get(url("/v1/settings")).decode()

    /**
     * Replaces the reader slice. The supplied [opId] is sent verbatim (no
     * implicit new ID), [ReaderSettings.rev] is never sent, and the returned
     * settings carry the server's new revision.
     */
    suspend fun updateReaderSettings(settings: ReaderSettings, opId: String): ReaderSettings {
        validateReaderSettings(settings, opId)
        val body = ReaderSettingsBody(
            readingMode = settings.readingMode,
            todayDecayDays = settings.todayDecayDays,
            singleKeyShortcuts = settings.singleKeyShortcuts,
            theme = settings.theme,
            measure = settings.measure,
            font = settings.font,
            feedOverrides = settings.feedOverrides,
            opId = opId,
        )
        return put(url("/v1/settings"), encoder.encodeToJsonElement(ReaderSettingsBody.serializer(), body)).decode()
    }

    suspend fun subscriptions(): List<Subscription> = get(url("/v1/subscriptions")).decode()

    suspend fun subscribe(body: SubscriptionBody): Subscription =
        post(url("/v1/subscriptions"), json.encodeToJsonElement(SubscriptionBody.serializer(), body)).decode()

    suspend fun updateSubscription(id: Long, body: SubscriptionPatch): Subscription =
        patch(url("/v1/subscriptions/$id"), json.encodeToJsonElement(SubscriptionPatch.serializer(), body)).decode()

    suspend fun unsubscribe(id: Long, opId: String): Empty =
        delete(url("/v1/subscriptions/$id") { it.addQueryParameter("op_id", opId) }).decode()

    suspend fun folders(): List<Folder> = get(url("/v1/folders")).decode()

    suspend fun createFolder(body: NameBody): Folder =
        post(url("/v1/folders"), json.encodeToJsonElement(NameBody.serializer(), body)).decode()

    suspend fun renameFolder(id: Long, body: NameBody): Folder =
        patch(url("/v1/folders/$id"), json.encodeToJsonElement(NameBody.serializer(), body)).decode()

    suspend fun deleteFolder(id: Long, opId: String): Empty =
        delete(url("/v1/folders/$id") { it.addQueryParameter("op_id", opId) }).decode()

    suspend fun labels(): List<Label> = get(url("/v1/labels")).decode()

    suspend fun createLabel(body: NameBody): Label =
        post(url("/v1/labels"), json.encodeToJsonElement(NameBody.serializer(), body)).decode()

    suspend fun renameLabel(id: Long, body: NameBody): Label =
        patch(url("/v1/labels/$id"), json.encodeToJsonElement(NameBody.serializer(), body)).decode()

    suspend fun deleteLabel(id: Long, opId: String): Label =
        delete(url("/v1/labels/$id") { it.addQueryParameter("op_id", opId) }).decode()

    suspend fun addLabel(id: Long, entryId: Long, opId: String): LabelMembership =
        put(url("/v1/labels/$id/entries/$entryId"), json.encodeToJsonElement(OpBody.serializer(), OpBody(opId))).decode()

    suspend fun removeLabel(id: Long, entryId: Long, opId: String): LabelMembership =
        delete(url("/v1/labels/$id/entries/$entryId") { it.addQueryParameter("op_id", opId) }).decode()

    suspend fun positions(): List<Position> = get(url("/v1/positions")).decode()

    suspend fun position(view: String): Position = get(url("/v1/positions") { it.addPathSegment(view) }).decode()

    suspend fun putPosition(view: String, body: PutPosition): Position =
        put(url("/v1/positions") { it.addPathSegment(view) }, json.encodeToJsonElement(PutPosition.serializer(), body)).decode()

    suspend fun todaysBriefs(): List<Annotation> = get(url("/v1/briefs/today")).decode()

    suspend fun activity(
        principalId: Long? = null,
        action: String? = null,
        since: String? = null,
        afterId: Long? = null,
        limit: Int = 100,
        grouped: Boolean = false,
    ): ActivityPage {
        val url = url("/v1/activity") {
            principalId?.let { value -> it.addQueryParameter("principal_id", value.toString()) }
            action?.let { value -> it.addQueryParameter("action", value) }
            since?.let { value -> it.addQueryParameter("since", value) }
            afterId?.let { value -> it.addQueryParameter("after_id", value.toString()) }
            it.addQueryParameter("limit", limit.toString())
            it.addQueryParameter("grouped", grouped.toString())
        }
        return get(url).decode()
    }

    suspend fun agentChanges(since: String, afterId: Long = 0, limit: Int = 1000): List<Activity> =
        get(url("/v1/activity/agent-changes") {
            it.addQueryParameter("since", since)
            it.addQueryParameter("after_id", afterId.toString())
            it.addQueryParameter("limit", limit.toString())
        }).decode<List<Activity>?>().orEmpty()

    suspend fun revertActivity(id: Long, opId: String): Empty =
        post(url("/v1/activity/$id:revert"), json.encodeToJsonElement(OpBody.serializer(), OpBody(opId))).decode()

    suspend fun notification(id: Long): Notification = get(url("/v1/notifications/$id")).decode()

    suspend fun deviceStart(clientName: String): DeviceResult =
        post(url("/v1/devices/start"), json.encodeToJsonElement(DeviceStart.serializer(), DeviceStart(clientName))).decode()

    suspend fun devicePoll(deviceCode: String): DeviceToken =
        post(url("/v1/devices/poll"), json.encodeToJsonElement(DevicePoll.serializer(), DevicePoll(deviceCode))).decode()

    suspend fun registerDevice(body: DeviceBody): Device =
        post(url("/v1/devices"), json.encodeToJsonElement(DeviceBody.serializer(), body)).decode()

    suspend fun devices(): List<Device> = get(url("/v1/devices")).decode()

    suspend fun deleteDevice(id: Long, opId: String): Empty =
        delete(url("/v1/devices/$id") { it.addQueryParameter("op_id", opId) }).decode()

    suspend fun me(): MeResult = get(url("/v1/me")).decode()

    suspend fun tokens(): List<Token> = get(url("/v1/tokens")).decode()

    /**
     * Runs a search page. [filter] carries the mode and optional narrowing;
     * [cursor] is the exclusive cursor returned by the preceding page.
     */
    suspend fun search(query: String, filter: SearchFilter = SearchFilter(), cursor: String? = null): SearchPage {
        val url = url("/v1/search") {
            it.addQueryParameter("q", query)
            it.addQueryParameter("mode", filter.mode.wireName)
            filter.subscriptionId?.let { value -> it.addQueryParameter("subscription_id", value.toString()) }
            filter.folderId?.let { value -> it.addQueryParameter("folder_id", value.toString()) }
            filter.limit?.let { value -> it.addQueryParameter("limit", value.toString()) }
            cursor?.let { value -> it.addQueryParameter("cursor", value) }
        }
        return get(url).decode()
    }

    suspend fun savedSearches(): List<SavedSearch> = get(url("/v1/saved-searches")).decode()

    suspend fun createSavedSearch(name: String, query: String, filter: SearchFilter): SavedSearch =
        post(url("/v1/saved-searches"), encoder.encodeToJsonElement(SavedSearchBody.serializer(), SavedSearchBody(name, query, filter))).decode()

    suspend fun renameSavedSearch(id: Long, name: String): Empty =
        patch(url("/v1/saved-searches/$id"), encoder.encodeToJsonElement(SavedSearchRename.serializer(), SavedSearchRename(name))).decode()

    suspend fun deleteSavedSearch(id: Long): Empty = delete(url("/v1/saved-searches/$id")).decode()

    suspend fun pipelineStatus(): JsonElement = get(url("/v1/pipeline"))

    companion object {
        private val JSON_MEDIA_TYPE = "application/json".toMediaType()
    }
}

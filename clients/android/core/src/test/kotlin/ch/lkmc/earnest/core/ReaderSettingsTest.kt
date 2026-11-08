package ch.lkmc.earnest.core

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.SerializationException
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.test.assertFailsWith
import java.util.concurrent.CopyOnWriteArrayList

class ReaderSettingsTest {
    private val json = Json { ignoreUnknownKeys = true }

    private fun client(respond: (Request) -> Response): Pair<EarnestApi, List<Request>> {
        val requests = CopyOnWriteArrayList<Request>()
        val http = OkHttpClient.Builder().addInterceptor { chain ->
            val request = chain.request()
            requests += request
            respond(request)
        }.build()
        return EarnestApi("https://earnest.test", "secret", http) to requests
    }

    private fun response(request: Request, status: Int = 200, body: String = ""): Response =
        Response.Builder()
            .request(request)
            .protocol(Protocol.HTTP_1_1)
            .code(status)
            .message("test")
            .body(body.toResponseBody("application/json".toMediaType()))
            .build()

    private fun Request.bodyString(): String {
        val buffer = Buffer()
        body?.writeTo(buffer)
        return buffer.readUtf8()
    }

    private fun Request.bodyObject(): JsonObject = Json.parseToJsonElement(bodyString()).jsonObject

    private val defaultsJSON =
        """{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{},"rev":0}"""

    @Test
    fun defaultsMatchServerDefaults() {
        val settings = ReaderSettings()
        assertEquals(ReadingMode.RIVER, settings.readingMode)
        assertEquals(2L, settings.todayDecayDays)
        assertTrue(settings.singleKeyShortcuts)
        assertEquals(ReaderTheme.SYSTEM, settings.theme)
        assertEquals(66L, settings.measure)
        assertEquals(ReaderFont.SERIF, settings.font)
        assertEquals(emptyMap<String, ReaderOverride>(), settings.feedOverrides)
        assertEquals(0L, settings.rev)

        val decoded = json.decodeFromString(ReaderSettings.serializer(), defaultsJSON)
        assertEquals(settings, decoded)
    }

    @Test
    fun readerSettingsDecodesLargeRevAndOverrides() = runBlocking {
        val (api, requests) = client { response(it, body = """
            {"reading_mode":"unread_counts","today_decay_days":3,"single_key_shortcuts":false,
             "theme":"black","measure":72,"font":"dyslexic",
             "feed_overrides":{"42":{"theme":"eink","measure":64,"font":"sans"}},
             "rev":9223372036854775807}
        """.trimIndent()) }
        val settings = api.readerSettings()
        assertEquals(listOf("GET"), requests.map { it.method })
        assertEquals("/v1/settings", requests.single().url.encodedPath)
        assertEquals(ReadingMode.UNREAD_COUNTS, settings.readingMode)
        assertEquals(3L, settings.todayDecayDays)
        assertFalse(settings.singleKeyShortcuts)
        assertEquals(ReaderTheme.BLACK, settings.theme)
        assertEquals(72L, settings.measure)
        assertEquals(ReaderFont.DYSLEXIC, settings.font)
        assertEquals(ReaderOverride(ReaderTheme.EINK, 64L, ReaderFont.SANS), settings.feedOverrides["42"])
        assertEquals(Long.MAX_VALUE, settings.rev)
    }

    @Test
    fun putBodyIncludesEveryFieldAndNeverRev() = runBlocking {
        val (api, requests) = client {
            response(it, body = """{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{},"rev":9}""")
        }
        val settings = ReaderSettings(
            readingMode = ReadingMode.UNREAD_COUNTS,
            todayDecayDays = 7,
            singleKeyShortcuts = false,
            theme = ReaderTheme.DARK,
            measure = 64,
            font = ReaderFont.SANS,
            feedOverrides = mapOf("42" to ReaderOverride(ReaderTheme.EINK, 63L, ReaderFont.DYSLEXIC)),
            rev = 9223372036854775807,
        )
        val updated = api.updateReaderSettings(settings, "op-1")
        assertEquals(9L, updated.rev)

        val request = requests.single()
        assertEquals("PUT", request.method)
        assertEquals("/v1/settings", request.url.encodedPath)
        assertEquals("Bearer secret", request.header("Authorization"))
        val body = request.bodyObject()
        assertEquals(
            setOf("reading_mode", "today_decay_days", "single_key_shortcuts", "theme", "measure", "font", "feed_overrides", "op_id"),
            body.keys,
        )
        assertFalse("PUT body must not carry rev", body.containsKey("rev"))
        assertEquals("unread_counts", body.getValue("reading_mode").jsonPrimitive.content)
        assertEquals("7", body.getValue("today_decay_days").jsonPrimitive.content)
        assertEquals("false", body.getValue("single_key_shortcuts").jsonPrimitive.content)
        assertEquals("dark", body.getValue("theme").jsonPrimitive.content)
        assertEquals("64", body.getValue("measure").jsonPrimitive.content)
        assertEquals("sans", body.getValue("font").jsonPrimitive.content)
        assertEquals("op-1", body.getValue("op_id").jsonPrimitive.content)
        assertEquals(
            ReaderOverride(ReaderTheme.EINK, 63L, ReaderFont.DYSLEXIC),
            json.decodeFromString(
                ReaderOverride.serializer(),
                body.getValue("feed_overrides").jsonObject.getValue("42").toString(),
            ),
        )
    }

    @Test
    fun suppliedOpIdIsPreservedOnEveryRetry() = runBlocking {
        val (api, requests) = client {
            response(it, body = """{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{},"rev":1}""")
        }
        api.updateReaderSettings(ReaderSettings(), "offline-op")
        api.updateReaderSettings(ReaderSettings(), "offline-op")
        assertEquals(2, requests.size)
        for (request in requests) {
            assertEquals("offline-op", request.bodyObject().getValue("op_id").jsonPrimitive.content)
        }
    }

    @Test
    fun partialOverridesInheritAndEncodeOnlyPresentMembers() = runBlocking {
        val decoded = json.decodeFromString(
            ReaderSettings.serializer(),
            """{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{"42":{"theme":"eink"}},"rev":4}""",
        )
        val override = decoded.feedOverrides.getValue("42")
        assertEquals(ReaderTheme.EINK, override.theme)
        assertNull(override.measure)
        assertNull(override.font)

        val (api, requests) = client {
            response(it, body = """{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{"42":{"theme":"eink"}},"rev":5}""")
        }
        api.updateReaderSettings(decoded, "op-1")
        val body = requests.single().bodyObject()
        assertEquals(setOf("theme"), body.getValue("feed_overrides").jsonObject.getValue("42").jsonObject.keys)
    }

    @Test
    fun emptyMapClearsOverrides() = runBlocking {
        val settings = ReaderSettings(feedOverrides = mapOf("42" to ReaderOverride(ReaderTheme.EINK)), rev = 3)
        val (api, requests) = client {
            response(it, body = """{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{},"rev":4}""")
        }
        api.updateReaderSettings(settings.copy(feedOverrides = emptyMap()), "op-1")
        val body = requests.single().bodyObject()
        assertTrue(body.containsKey("feed_overrides"))
        assertTrue("feed_overrides must be an empty object", body.getValue("feed_overrides").jsonObject.isEmpty())
    }

    @Test
    fun enumWireNamesMatchTheServer() {
        assertEquals("\"river\"", json.encodeToString(ReadingMode.RIVER))
        assertEquals("\"unread_counts\"", json.encodeToString(ReadingMode.UNREAD_COUNTS))
        for ((value, wire) in listOf(
            ReaderTheme.SYSTEM to "system", ReaderTheme.LIGHT to "light", ReaderTheme.DARK to "dark",
            ReaderTheme.BLACK to "black", ReaderTheme.EINK to "eink",
        )) {
            assertEquals("\"$wire\"", json.encodeToString(value))
            assertEquals(value, json.decodeFromString(ReaderTheme.serializer(), "\"$wire\""))
        }
        for ((value, wire) in listOf(
            ReaderFont.SERIF to "serif", ReaderFont.SANS to "sans", ReaderFont.DYSLEXIC to "dyslexic",
        )) {
            assertEquals("\"$wire\"", json.encodeToString(value))
            assertEquals(value, json.decodeFromString(ReaderFont.serializer(), "\"$wire\""))
        }
        assertFailsWith<SerializationException> {
            json.decodeFromString(ReaderTheme.serializer(), "\"sepia\"")
        }
    }

    @Test
    fun unauthorizedIsAnApiException() = runBlocking {
        val (api, requests) = client {
            response(it, status = 401, body = """{"type":"about:blank","title":"Unauthorized","status":401,"error":"unauthorized"}""")
        }
        val error = runCatching { api.updateReaderSettings(ReaderSettings(), "op-1") }.exceptionOrNull() as ApiException
        assertEquals(401, error.status)
        assertTrue(error.isUnauthorized)
        assertEquals("unauthorized", error.code)
        assertEquals(listOf("PUT"), requests.map { it.method })
    }

    @Test
    fun invalidValuesFailBeforeAnyRequest() = runBlocking {
        val (api, requests) = client { error("unexpected request") }
        for (settings in listOf(
            ReaderSettings(measure = 61),
            ReaderSettings(measure = 73),
            ReaderSettings(todayDecayDays = 0),
            ReaderSettings(todayDecayDays = 366),
            ReaderSettings(feedOverrides = mapOf("0" to ReaderOverride())),
            ReaderSettings(feedOverrides = mapOf("+42" to ReaderOverride())),
            ReaderSettings(feedOverrides = mapOf("42" to ReaderOverride(measure = 73))),
        )) {
            assertFailsWith<IllegalArgumentException> { api.updateReaderSettings(settings, "op-1") }
        }
        assertFailsWith<IllegalArgumentException> { api.updateReaderSettings(ReaderSettings(), "") }
        assertTrue(requests.isEmpty())
    }

    @Test
    fun opIdLimitsCountUtf8Bytes() = runBlocking {
        val (api, requests) = client { response(it, body = defaultsJSON) }
        val maximum = "é".repeat(512)
        api.updateReaderSettings(ReaderSettings(), maximum)
        assertEquals(maximum, requests.single().bodyObject().getValue("op_id").jsonPrimitive.content)
        assertFailsWith<IllegalArgumentException> {
            api.updateReaderSettings(ReaderSettings(), "é".repeat(513))
        }
        assertEquals(1, requests.size)
    }

    @Test
    fun typographyInheritsAccountWideAndPerMemberOverride() {
        val settings = ReaderSettings(
            theme = ReaderTheme.LIGHT,
            measure = 64,
            font = ReaderFont.SERIF,
            feedOverrides = mapOf(
                "42" to ReaderOverride(theme = ReaderTheme.EINK),
                "7" to ReaderOverride(measure = 70, font = ReaderFont.DYSLEXIC),
            ),
        )
        assertEquals(ReaderTypography(ReaderTheme.LIGHT, 64, ReaderFont.SERIF), settings.typographyFor(null))
        assertEquals(ReaderTypography(ReaderTheme.LIGHT, 64, ReaderFont.SERIF), settings.typographyFor(0))
        assertEquals(ReaderTypography(ReaderTheme.EINK, 64, ReaderFont.SERIF), settings.typographyFor(42))
        assertEquals(ReaderTypography(ReaderTheme.LIGHT, 70, ReaderFont.DYSLEXIC), settings.typographyFor(7))
        assertEquals(ReaderTypography(ReaderTheme.LIGHT, 64, ReaderFont.SERIF), settings.typographyFor(99))
    }

    @Test
    fun unreadCountsDecodeDecimalKeysAndInt64() {
        val counts = json.decodeFromString(
            UnreadCounts.serializer(),
            """{"by_subscription":{"3":0,"9223372036854775807":9223372036854775807},"later":2,"kept":3,"total":5}""",
        )
        assertEquals(0L, counts.bySubscription.getValue(3L))
        assertEquals(Long.MAX_VALUE, counts.bySubscription.getValue(Long.MAX_VALUE))
        assertEquals(2L, counts.later)
        assertEquals(3L, counts.kept)
        assertEquals(5L, counts.total)
        val encoded = json.encodeToString(UnreadCounts.serializer(), counts)
        assertEquals(counts, json.decodeFromString(UnreadCounts.serializer(), encoded))
        assertTrue(encoded.contains("\"by_subscription\":{\"3\":0"))
    }

    @Test
    fun unreadCountsRequestUsesTheAggregateRoute() = runBlocking {
        val (api, requests) = client { response(it, body = """{"by_subscription":{"3":4},"later":1,"kept":2,"total":7}""") }
        val counts = api.unreadCounts()
        assertEquals(listOf("GET"), requests.map { it.method })
        assertEquals("/v1/entries:counts", requests.single().url.encodedPath)
        assertEquals(4L, counts.bySubscription.getValue(3L))
        assertEquals(7L, counts.total)
    }

    @Test
    fun largeRevRoundTripsThroughJson() {
        val settings = ReaderSettings(rev = Long.MAX_VALUE)
        val encoded = Json { encodeDefaults = true }.encodeToString(ReaderSettings.serializer(), settings)
        assertEquals(settings, json.decodeFromString(ReaderSettings.serializer(), encoded))
    }
}

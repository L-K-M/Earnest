package ch.lkmc.earnest.core

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
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
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CopyOnWriteArrayList

class SearchApiTest {
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

    @Test
    fun searchEncodesQueryModeFiltersAndCursorSafely() = runBlocking {
        val (api, requests) = client { response(it, body = """{"matches":[]}""") }
        val page = api.search(
            "café ☕ & q=1 #frag/plus+slash%20",
            SearchFilter(SearchMode.HYBRID, 9007199254740993, 2, 3),
            "abc-_123",
        )
        assertEquals(emptyList<SearchMatch>(), page.matches)

        val request = requests.single()
        assertEquals("GET", request.method)
        assertEquals("/v1/search", request.url.encodedPath)
        assertEquals(listOf("q", "mode", "subscription_id", "folder_id", "limit", "cursor"), request.url.queryParameterNames.toList())
        assertEquals("café ☕ & q=1 #frag/plus+slash%20", request.url.queryParameter("q"))
        assertEquals("hybrid", request.url.queryParameter("mode"))
        assertEquals("9007199254740993", request.url.queryParameter("subscription_id"))
        assertEquals("2", request.url.queryParameter("folder_id"))
        assertEquals("3", request.url.queryParameter("limit"))
        assertEquals("abc-_123", request.url.queryParameter("cursor"))
        val encodedQueryValue = request.url.encodedQuery!!.substringBefore("&")
        assertFalse("query value leaked a raw space", encodedQueryValue.contains(" "))
        assertFalse("query value leaked a raw fragment", encodedQueryValue.contains("#"))
        assertFalse("query value leaked a raw ampersand", encodedQueryValue.contains("&"))
        assertEquals("Bearer secret", request.header("Authorization"))
        assertEquals("application/json", request.header("Accept"))
    }

    @Test
    fun searchDefaultsToFulltextAndOmitsAbsentFilters() = runBlocking {
        val (api, requests) = client { response(it, body = """{"matches":[]}""") }
        api.search("plain")
        val url = requests.single().url
        assertEquals(listOf("q", "mode"), url.queryParameterNames.toList())
        assertEquals("plain", url.queryParameter("q"))
        assertEquals("fulltext", url.queryParameter("mode"))
        assertNull(url.queryParameter("subscription_id"))
        assertNull(url.queryParameter("folder_id"))
        assertNull(url.queryParameter("limit"))
        assertNull(url.queryParameter("cursor"))
    }

    @Test
    fun searchPassesTheReturnedCursorToTheNextPage() = runBlocking {
        val (api, requests) = client {
            response(it, body = """{"matches":[{"kind":"entry","entry_id":1,"subscription_id":2,"feed_id":3,"seq":4,"title":"T","url":"https://e.test/a","snippet":"S","rank":1.5,"untrusted":false}],"next_cursor":"abc-_123"}""")
        }
        val first = api.search("beacon")
        assertEquals("abc-_123", first.nextCursor)
        assertNull(requests.single().url.queryParameter("cursor"))

        val second = api.search("beacon", cursor = first.nextCursor)
        assertEquals("abc-_123", second.nextCursor)
        assertEquals("abc-_123", requests[1].url.queryParameter("cursor"))
    }

    @Test
    fun searchPreservesLongIdsAndAgentNoteMetadata() = runBlocking {
        val (api, _) = client {
            response(
                it,
                body = """
                {"matches":[
                  {"kind":"entry","entry_id":9007199254740993,"subscription_id":9007199254740992,"feed_id":9007199254740991,"seq":9007199254740990,"title":"Entry title","url":"https://e.test/a?b=c","snippet":"Entry snippet","rank":9.25,"untrusted":false},
                  {"kind":"agent_note","entry_id":9007199254740993,"subscription_id":9007199254740992,"feed_id":9007199254740991,"seq":9007199254740990,"title":"Agent note","url":"https://e.test/a?b=c","snippet":"Note snippet","rank":0.75,"untrusted":true,"annotation_id":84,"agent_name":"Morning Agent"}
                ],"next_cursor":"next","status":"enabled"}
                """.trimIndent(),
            )
        }
        val page = api.search("observation", SearchFilter(SearchMode.HYBRID))
        val matches = page.matches.orEmpty()
        assertEquals(2, matches.size)

        val entry = matches[0]
        assertEquals("entry", entry.kind)
        assertEquals(9007199254740993L, entry.entryId)
        assertEquals(9007199254740992L, entry.subscriptionId)
        assertEquals(9007199254740991L, entry.feedId)
        assertEquals(9007199254740990L, entry.seq)
        assertEquals("Entry title", entry.title)
        assertEquals("https://e.test/a?b=c", entry.url)
        assertEquals("Entry snippet", entry.snippet)
        assertEquals(9.25, entry.rank, 0.0)
        assertFalse(entry.untrusted)
        assertNull(entry.annotationId)
        assertNull(entry.agentName)
        assertEquals("entry:9007199254740993:0", entry.key)

        val note = matches[1]
        assertEquals("agent_note", note.kind)
        assertEquals(84L, note.annotationId)
        assertEquals("Morning Agent", note.agentName)
        assertTrue(note.untrusted)
        assertEquals("agent_note:9007199254740993:84", note.key)
        assertNotEquals(entry.key, note.key)
        assertEquals(note.key, note.copy(snippet = "changed").key)

        assertEquals("next", page.nextCursor)
        assertEquals("enabled", page.status)
    }

    @Test
    fun searchDecodesInBandDisabledPausedAndNullMatches() = runBlocking {
        for ((status, body) in listOf(
            "disabled" to """{"matches":[],"status":"disabled"}""",
            "paused" to """{"matches":null,"status":"paused"}""",
        )) {
            val (api, _) = client { response(it, body = body) }
            val page = api.search("observation", SearchFilter(SearchMode.HYBRID))
            assertEquals(status, page.status)
            if (status == "disabled") assertTrue(page.matches!!.isEmpty()) else assertNull(page.matches)
        }
        val (api, _) = client { response(it, body = """{"matches":null}""") }
        val page = api.search("observation")
        assertNull(page.matches)
        assertNull(page.nextCursor)
        assertNull(page.status)
    }

    @Test
    fun searchSurfacesProblemErrors() = runBlocking {
        val (api, _) = client {
            response(it, status = 422, body = """{"type":"about:blank","title":"Unprocessable","status":422,"error":"invalid","detail":"bad query"}""")
        }
        val error = runCatching { api.search("bad") }.exceptionOrNull() as ApiException
        assertEquals(422, error.status)
        assertEquals("invalid", error.code)
        assertTrue(error.message!!.contains("invalid"))

        val (unauthorized, _) = client {
            response(it, status = 401, body = """{"error":"unauthorized"}""")
        }
        val denied = runCatching { unauthorized.search("bad") }.exceptionOrNull() as ApiException
        assertEquals(401, denied.status)
        assertEquals("unauthorized", denied.code)
    }

    @Test
    fun savedSearchCrudUsesExactPathsBodiesAndNoOpId() = runBlocking {
        val (api, requests) = client { request ->
            when (request.method to request.url.encodedPath) {
                "GET" to "/v1/saved-searches" -> response(
                    request,
                    body = """[{"id":9007199254740993,"name":"Stored","query":"stored","filters":{"mode":"hybrid","subscription_id":2,"folder_id":3,"limit":4}}]""",
                )
                "POST" to "/v1/saved-searches" -> response(
                    request,
                    body = """{"id":9007199254740993,"name":"New","query":"New query","filters":{"mode":"hybrid"}}""",
                )
                "PATCH" to "/v1/saved-searches/9007199254740993" -> response(request, body = "{}")
                "DELETE" to "/v1/saved-searches/9007199254740993" -> response(request, body = "{}")
                else -> response(request, status = 404, body = """{"error":"not_found"}""")
            }
        }

        val saved = api.savedSearches().single()
        assertEquals(9007199254740993L, saved.id)
        assertEquals("Stored", saved.name)
        assertEquals("stored", saved.query)
        assertEquals(SearchMode.HYBRID, saved.filters.mode)
        assertEquals(2L, saved.filters.subscriptionId)
        assertEquals(3L, saved.filters.folderId)
        assertEquals(4L, saved.filters.limit)

        val created = api.createSavedSearch("New", "New query", SearchFilter(SearchMode.HYBRID))
        assertEquals(9007199254740993L, created.id)
        assertEquals("New", created.name)
        assertEquals("New query", created.query)
        assertEquals(SearchMode.HYBRID, created.filters.mode)

        assertEquals(Empty(), api.renameSavedSearch(9007199254740993L, "Renamed"))
        assertEquals(Empty(), api.deleteSavedSearch(9007199254740993L))

        assertEquals(4, requests.size)
        assertEquals("GET" to "/v1/saved-searches", requests[0].method to requests[0].url.encodedPath)
        assertNull(requests[0].url.query)
        assertEquals("POST" to "/v1/saved-searches", requests[1].method to requests[1].url.encodedPath)
        assertEquals("""{"name":"New","query":"New query","filters":{"mode":"hybrid"}}""", requests[1].bodyString())
        assertEquals("PATCH" to "/v1/saved-searches/9007199254740993", requests[2].method to requests[2].url.encodedPath)
        assertEquals("""{"name":"Renamed"}""", requests[2].bodyString())
        assertEquals("DELETE" to "/v1/saved-searches/9007199254740993", requests[3].method to requests[3].url.encodedPath)
        assertNull(requests[3].url.query)
    }

    @Test
    fun savedSearchCreateEncodesLargeIdsAndOmitsNullFilters() = runBlocking {
        val (api, requests) = client {
            response(it, body = """{"id":1,"name":"N","query":"q","filters":{"mode":"fulltext","subscription_id":9007199254740993,"folder_id":9007199254740992,"limit":200}}""")
        }
        api.createSavedSearch(
            "N",
            "q",
            SearchFilter(SearchMode.FULLTEXT, 9007199254740993, 9007199254740992, 200),
        )
        assertEquals(
            """{"name":"N","query":"q","filters":{"mode":"fulltext","subscription_id":9007199254740993,"folder_id":9007199254740992,"limit":200}}""",
            requests.single().bodyString(),
        )
    }

    @Test
    fun pipelineStatusReturnsServerJson() = runBlocking {
        val (api, requests) = client {
            response(
                it,
                body = """{"embed":"enabled","classify":"disabled","semantic":"paused","watch":"enabled","blurb":"enabled","summary":"enabled","edition":"enabled","ask":"enabled","compile_rule":"enabled"}""",
            )
        }
        val status = api.pipelineStatus().jsonObject
        assertEquals("enabled", status.getValue("embed").jsonPrimitive.content)
        assertEquals("disabled", status.getValue("classify").jsonPrimitive.content)
        assertEquals("paused", status.getValue("semantic").jsonPrimitive.content)
        assertEquals("GET" to "/v1/pipeline", requests.single().method to requests.single().url.encodedPath)
        assertNull(requests.single().url.query)
    }

    @Test
    fun searchModelsUseSnakeCaseWireNamesAndEnumValues() {
        assertEquals("\"fulltext\"", Json.encodeToString(SearchMode.FULLTEXT))
        assertEquals("\"hybrid\"", Json.encodeToString(SearchMode.HYBRID))
        assertEquals(SearchMode.FULLTEXT, Json.decodeFromString<SearchMode>("\"fulltext\""))
        assertEquals(SearchMode.HYBRID, Json.decodeFromString<SearchMode>("\"hybrid\""))
        assertEquals("fulltext", SearchMode.FULLTEXT.wireName)
        assertEquals("hybrid", SearchMode.HYBRID.wireName)

        val filter = Json.parseToJsonElement(Json.encodeToString(SearchFilter.serializer(), SearchFilter(SearchMode.HYBRID, 1, 2, 3))).jsonObject
        assertEquals(setOf("mode", "subscription_id", "folder_id", "limit"), filter.keys)
        assertEquals("hybrid", filter.getValue("mode").jsonPrimitive.content)

        val match = Json.parseToJsonElement(
            Json.encodeToString(
                SearchMatch.serializer(),
                SearchMatch("agent_note", 1, 2, 3, 4, "T", "https://e.test", "S", 1.5, true, 84, "Morning Agent"),
            ),
        ).jsonObject
        assertEquals(
            setOf("kind", "entry_id", "subscription_id", "feed_id", "seq", "title", "url", "snippet", "rank", "untrusted", "annotation_id", "agent_name"),
            match.keys,
        )
    }
}

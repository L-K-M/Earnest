package ch.lkmc.earnest

import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.SavedSearch
import ch.lkmc.earnest.core.SearchFilter
import ch.lkmc.earnest.core.SearchMode
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference

/**
 * SearchModel owns online search state. These tests drive a real EarnestApi
 * through a scripted OkHttp interceptor (no network) and assert the
 * observable state machine: metadata failures keep full-text usable,
 * unavailable hybrid falls back, identities/dedup, immutable submitted
 * paging, cursor termination, mutation durability, request/session fencing,
 * and generation invalidation.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SearchModelTest {
    private val http = ScriptedHttp()
    private val scope = CoroutineScope(UnconfinedTestDispatcher())
    private val currentSession = AtomicReference<SearchSession?>(null)
    private val activeSessionId = AtomicLong(0)
    private val nextSessionId = AtomicLong(0)
    private val unauthorizedCalls = CopyOnWriteArrayList<String>()
    private val unauthorizedEvents = MutableSharedFlow<String>(replay = 1)

    /** A session whose liveness is decided by the shared active id. */
    private inner class TestSession(val id: Long, val token: String) {
        val delegate: SearchSession = SearchSession(
            api = EarnestApi("https://earnest.test", token, http.client),
            isCurrent = { activeSessionId.get() == id },
            unauthorized = { unauthorizedCalls += token; unauthorizedEvents.tryEmit(token) },
        )

        fun activate() {
            currentSession.set(delegate)
            activeSessionId.set(id)
        }
    }

    private fun newSession(token: String): TestSession = TestSession(nextSessionId.incrementAndGet(), token)

    private fun model(): SearchModel = SearchModel(scope) { currentSession.get() }

    @After
    fun tearDown() {
        http.releaseAll()
        scope.cancel()
        http.client.dispatcher.executorService.shutdown()
    }

    // ---------------------------------------------------------------------
    // Metadata failures and hybrid availability
    // ---------------------------------------------------------------------

    @Test
    fun fullTextSearchStillWorksAfterMetadataFailure() = runBlocking {
        http.handler = { request ->
            when (request.url.encodedPath) {
                "/v1/saved-searches" -> http.response(request, 500, """{"error":"saved_boom"}""")
                "/v1/pipeline" -> http.response(request, 500, """{"error":"pipe_boom"}""")
                "/v1/search" -> http.json(request, pageJson(listOf(entryJson(1))))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.loadMetadata()
        val metadata = awaitState(model) { it.metadataError != null && !it.loadingMetadata }
        assertTrue("saved-search failure was hidden: $metadata", metadata.metadataError!!.startsWith("Could not load saved searches"))
        assertEquals("disabled", metadata.embeddingStatus)
        assertTrue(metadata.savedSearches.isEmpty())

        model.setQuery("still searchable")
        model.submit()
        val search = http.awaitRequest { it.url.encodedPath == "/v1/search" }
        assertEquals("still searchable", search.url.queryParameter("q"))
        assertEquals("fulltext", search.url.queryParameter("mode"))
        val settled = awaitState(model) { it.matches.size == 1 && !it.loading }
        assertNull(settled.error)
    }

    @Test
    fun pipelineFailureKeepsFullTextSearchAvailable() = runBlocking {
        http.handler = { request ->
            when (request.url.encodedPath) {
                "/v1/saved-searches" -> http.json(request, "[]")
                "/v1/pipeline" -> http.response(request, 500, """{"error":"pipeline_down"}""")
                "/v1/search" -> http.json(request, pageJson(listOf(entryJson(2))))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.loadMetadata()
        val metadata = awaitState(model) { !it.loadingMetadata && it.metadataError != null }
        assertEquals(
            "Could not check AI search availability. Full-text search is still available.",
            metadata.metadataError,
        )
        assertEquals("disabled", metadata.embeddingStatus)

        model.setFilter(SearchFilter(SearchMode.HYBRID, subscriptionId = 5))
        model.setQuery("hybrid fallback")
        model.submit()
        val search = http.awaitRequest { it.url.encodedPath == "/v1/search" }
        assertEquals("fulltext", search.url.queryParameter("mode"))
        assertEquals("5", search.url.queryParameter("subscription_id"))
    }

    @Test
    fun hybridFallsBackToFullTextOnlyWhenEmbeddingIsUnavailable() = runBlocking {
        http.handler = { request ->
            when (request.url.encodedPath) {
                "/v1/saved-searches" -> http.json(request, "[]")
                "/v1/pipeline" -> http.json(request, """{"embed":"paused"}""")
                "/v1/search" -> http.json(request, pageJson(listOf(entryJson(3))))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.loadMetadata()
        awaitState(model) { it.embeddingStatus == "paused" && !it.loadingMetadata }
        model.setFilter(SearchFilter(SearchMode.HYBRID, subscriptionId = 5, folderId = 6))
        model.setQuery("paused mode")
        model.submit()
        val paused = http.awaitRequest { it.url.encodedPath == "/v1/search" }
        assertEquals("fulltext", paused.url.queryParameter("mode"))
        assertEquals("5", paused.url.queryParameter("subscription_id"))
        assertEquals("6", paused.url.queryParameter("folder_id"))
        assertEquals("Hybrid search is unavailable. This search uses full text.", model.state.value.notice)

        // The same hybrid filter is preserved once the pipeline reports enabled.
        http.handler = { request ->
            when (request.url.encodedPath) {
                "/v1/saved-searches" -> http.json(request, "[]")
                "/v1/pipeline" -> http.json(request, """{"embed":"enabled"}""")
                "/v1/search" -> http.json(request, pageJson(listOf(entryJson(4))))
                else -> http.json(request, "{}")
            }
        }
        model.loadMetadata()
        awaitState(model) { it.embeddingStatus == "enabled" && !it.loadingMetadata }
        model.setFilter(SearchFilter(SearchMode.HYBRID, subscriptionId = 5, folderId = 6))
        model.setQuery("enabled mode")
        model.submit()
        val enabled = http.awaitRequest {
            it.url.encodedPath == "/v1/search" && it.url.queryParameter("q") == "enabled mode"
        }
        assertEquals("hybrid", enabled.url.queryParameter("mode"))
        assertEquals("5", enabled.url.queryParameter("subscription_id"))
        assertEquals("6", enabled.url.queryParameter("folder_id"))
        assertNull(model.state.value.notice)
    }

    // ---------------------------------------------------------------------
    // Identities, dedup, and paging
    // ---------------------------------------------------------------------

    @Test
    fun enabledHybridPagesKeepTheirMatchesAndContinuation() = runBlocking {
        http.handler = { request ->
            when (request.url.encodedPath) {
                "/v1/saved-searches" -> http.json(request, "[]")
                "/v1/pipeline" -> http.json(request, """{"embed":"enabled"}""")
                "/v1/search" -> if (request.url.queryParameter("cursor") == null) {
                    http.json(request, pageJson(listOf(entryJson(5)), nextCursor = "enabled-next", status = "enabled"))
                } else http.json(request, pageJson(emptyList(), status = "enabled"))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()
        model.loadMetadata()
        awaitState(model) { it.embeddingStatus == "enabled" && !it.loadingMetadata }
        model.setFilter(SearchFilter(mode = SearchMode.HYBRID))
        model.setQuery("enabled result")
        model.submit()
        val first = awaitState(model) { it.matches.size == 1 && !it.loading }
        assertNull(first.status)
        assertEquals("enabled-next", first.nextCursor)
        model.loadMore()
        val last = awaitState(model) { !it.loading && it.nextCursor == null }
        assertNull(last.status)
        assertEquals(listOf(5L), last.matches.map { it.entryId })
    }

    @Test
    fun pagesDeduplicateOverlappingMatchesByIdentity() = runBlocking {
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/search" && request.url.queryParameter("cursor") == null ->
                    http.json(
                        request,
                        pageJson(listOf(entryJson(10), noteJson(10, 84), entryJson(11)), nextCursor = "c1"),
                    )
                request.url.encodedPath == "/v1/search" && request.url.queryParameter("cursor") == "c1" ->
                    http.json(
                        request,
                        pageJson(
                            listOf(entryJson(10), noteJson(10, 84, title = "Updated note"), noteJson(10, 85)),
                        ),
                    )
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.setQuery("overlap")
        model.submit()
        val first = awaitState(model) { it.matches.size == 3 && it.nextCursor == "c1" && !it.loading }
        assertEquals(
            listOf("entry:10:0", "agent_note:10:84", "entry:11:0"),
            first.matches.map { it.key },
        )

        model.loadMore()
        val second = awaitState(model) { it.matches.size == 4 && !it.loading }
        assertEquals(
            listOf("entry:10:0", "agent_note:10:84", "entry:11:0", "agent_note:10:85"),
            second.matches.map { it.key },
        )
        assertNull(second.nextCursor)
        assertNull(second.error)
        // The first occurrence wins: page two's changed title must not replace page one's.
        assertEquals("Note 10", second.matches.first { it.key == "agent_note:10:84" }.title)
        assertFalse(second.matches.first { it.key == "entry:10:0" }.untrusted)
        assertTrue(second.matches.first { it.key == "agent_note:10:85" }.untrusted)
        assertEquals("Morning Agent", second.matches.first { it.key == "agent_note:10:85" }.agentName)
    }

    @Test
    fun nullAndZeroAnnotationIdsShareNoteIdentity() = runBlocking {
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/search" ->
                    http.json(request, pageJson(listOf(noteJson(7, null), noteJson(7, 0), entryJson(7))))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.setQuery("identity")
        model.submit()
        val settled = awaitState(model) { it.matches.size == 2 && !it.loading }
        assertEquals(listOf("agent_note:7:0", "entry:7:0"), settled.matches.map { it.key })
    }

    @Test
    fun pagingKeepsTheSubmittedQueryAndFiltersWhileTheDraftIsEdited() = runBlocking {
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/saved-searches" -> http.json(request, "[]")
                request.url.encodedPath == "/v1/pipeline" -> http.json(request, """{"embed":"enabled"}""")
                request.url.encodedPath == "/v1/search" && request.url.queryParameter("cursor") == null ->
                    http.json(request, pageJson(listOf(entryJson(20)), nextCursor = "c1"))
                request.url.encodedPath == "/v1/search" && request.url.queryParameter("cursor") == "c1" ->
                    http.json(request, pageJson(listOf(entryJson(21))))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.loadMetadata()
        awaitState(model) { it.embeddingStatus == "enabled" && !it.loadingMetadata }
        model.setFilter(SearchFilter(SearchMode.HYBRID, subscriptionId = 5, folderId = 6))
        model.setQuery("first query")
        model.submit()
        val first = http.awaitRequest {
            it.url.encodedPath == "/v1/search" && it.url.queryParameter("cursor") == null
        }
        assertEquals("first query", first.url.queryParameter("q"))
        assertEquals("hybrid", first.url.queryParameter("mode"))
        assertEquals("5", first.url.queryParameter("subscription_id"))
        assertEquals("6", first.url.queryParameter("folder_id"))
        awaitState(model) { it.nextCursor == "c1" && !it.loading }

        // The reader edits the next draft after the first page settled.
        model.setQuery("second query")
        model.setFilter(SearchFilter(SearchMode.FULLTEXT, subscriptionId = 99))

        model.loadMore()
        val second = http.awaitRequest {
            it.url.encodedPath == "/v1/search" && it.url.queryParameter("cursor") == "c1"
        }
        assertEquals("first query", second.url.queryParameter("q"))
        assertEquals("hybrid", second.url.queryParameter("mode"))
        assertEquals("5", second.url.queryParameter("subscription_id"))
        assertEquals("6", second.url.queryParameter("folder_id"))
        assertNull(second.url.queryParameter("limit"))

        val settled = awaitState(model) { it.matches.size == 2 && !it.loading }
        assertEquals(listOf("entry:20:0", "entry:21:0"), settled.matches.map { it.key })
        assertEquals("second query", settled.query)
        assertEquals("first query", settled.submittedQuery)
    }

    @Test
    fun emptyPageTerminatesPaging() = runBlocking {
        http.handler = { request ->
            when {
                request.url.encodedPath != "/v1/search" -> http.json(request, "{}")
                request.url.queryParameter("q") == "empty first" ->
                    http.json(request, pageJson(emptyList(), nextCursor = "c1"))
                request.url.queryParameter("cursor") == null ->
                    http.json(request, pageJson(listOf(entryJson(30)), nextCursor = "c1"))
                else ->
                    http.json(request, pageJson(emptyList(), nextCursor = "c2"))
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.setQuery("empty page")
        model.submit()
        awaitState(model) { it.nextCursor == "c1" && !it.loading }
        model.loadMore()
        val settled = awaitState(model) { !it.loading && it.nextCursor == null }
        assertEquals(listOf("entry:30:0"), settled.matches.map { it.key })
        assertNull(settled.error)
        val terminated = http.requests.count { it.url.encodedPath == "/v1/search" }
        model.loadMore()
        assertEquals("terminated search issued another page", terminated, http.requests.count { it.url.encodedPath == "/v1/search" })

        // An empty first page terminates immediately.
        val emptyFirst = model()
        emptyFirst.setQuery("empty first")
        emptyFirst.submit()
        val first = awaitState(emptyFirst) { !it.loading && it.nextCursor == null && it.matches.isEmpty() }
        assertNull(first.error)
        val firstCount = http.requests.count { it.url.encodedPath == "/v1/search" }
        emptyFirst.loadMore()
        assertEquals("empty first page issued another page", firstCount, http.requests.count { it.url.encodedPath == "/v1/search" })
    }

    @Test
    fun nullMatchesPageTerminatesPaging() = runBlocking {
        http.handler = { request ->
            when {
                request.url.encodedPath != "/v1/search" -> http.json(request, "{}")
                request.url.queryParameter("cursor") == null ->
                    http.json(request, pageJson(listOf(entryJson(32)), nextCursor = "c1"))
                else ->
                    http.json(request, """{"matches":null,"next_cursor":"c2"}""")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.setQuery("null page")
        model.submit()
        awaitState(model) { it.nextCursor == "c1" && !it.loading }
        model.loadMore()
        val settled = awaitState(model) { !it.loading && it.nextCursor == null }
        assertEquals(listOf("entry:32:0"), settled.matches.map { it.key })
        assertNull(settled.error)
    }

    @Test
    fun repeatedCursorReportsFailureAndStopsPaging() = runBlocking {
        http.handler = { request ->
            when {
                request.url.encodedPath != "/v1/search" -> http.json(request, "{}")
                request.url.queryParameter("cursor") == null ->
                    http.json(request, pageJson(listOf(entryJson(40)), nextCursor = "c1"))
                else ->
                    http.json(request, pageJson(listOf(entryJson(41)), nextCursor = "c1"))
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.setQuery("repeat")
        model.submit()
        awaitState(model) { it.nextCursor == "c1" && !it.loading }
        model.loadMore()
        val settled = awaitState(model) { !it.loading && it.nextCursor == null && it.error != null }
        assertEquals(
            "The server returned a repeated search cursor. Run the search again.",
            settled.error,
        )
        // The repeated page's matches were not merged.
        assertEquals(listOf("entry:40:0"), settled.matches.map { it.key })
        model.loadMore()
        assertEquals("repeated cursor issued another page", 2, http.requests.count { it.url.encodedPath == "/v1/search" })
    }

    @Test
    fun loadMoreDoesNotFireWhileLoadingOrWithoutCursor() = runBlocking {
        val gate = http.newGate()
        http.handler = { request ->
            when {
                request.url.encodedPath != "/v1/search" -> http.json(request, "{}")
                request.url.queryParameter("cursor") == null ->
                    http.held(request, gate, pageJson(listOf(entryJson(41)), nextCursor = "c1"))
                else ->
                    http.json(request, pageJson(listOf(entryJson(42))))
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.setQuery("paging")
        model.submit()
        http.awaitRequest { it.url.encodedPath == "/v1/search" && it.url.queryParameter("cursor") == null }
        val inFlight = http.requests.count { it.url.encodedPath == "/v1/search" }
        model.loadMore()
        assertEquals(
            "loadMore fired while the first page was still loading",
            inFlight,
            http.requests.count { it.url.encodedPath == "/v1/search" },
        )

        gate.complete(Unit)
        awaitState(model) { it.nextCursor == "c1" && !it.loading }
        model.loadMore()
        http.awaitRequest { it.url.encodedPath == "/v1/search" && it.url.queryParameter("cursor") == "c1" }
        val settled = awaitState(model) { it.matches.size == 2 && !it.loading }
        assertNull(settled.nextCursor)
        model.loadMore()
        assertEquals(2, http.requests.count { it.url.encodedPath == "/v1/search" })
    }

    // ---------------------------------------------------------------------
    // Saved searches: durability, validation, submitted snapshot
    // ---------------------------------------------------------------------

    @Test
    fun saveNewUsesSubmittedSnapshotAndReportsSuccessOnlyAfterServerSuccess() = runBlocking {
        val createGate = http.newGate()
        val createCount = AtomicInteger()
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/saved-searches" && request.method == "GET" ->
                    http.json(request, "[" + savedJson(7, "Stored", "stored") + "]")
                request.url.encodedPath == "/v1/pipeline" ->
                    http.json(request, """{"embed":"enabled"}""")
                request.url.encodedPath == "/v1/search" ->
                    http.json(request, pageJson(listOf(entryJson(50))))
                request.method == "POST" && request.url.encodedPath == "/v1/saved-searches" -> {
                    createCount.incrementAndGet()
                    http.held(
                        request,
                        createGate,
                        savedJson(42, "My saved search", "draft query", """{"mode":"hybrid","subscription_id":5,"folder_id":6}"""),
                    )
                }
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.loadMetadata()
        awaitState(model) { it.savedSearches.size == 1 && it.embeddingStatus == "enabled" && !it.loadingMetadata }
        model.setFilter(SearchFilter(SearchMode.HYBRID, subscriptionId = 5, folderId = 6))
        model.setQuery("draft query")
        model.submit()
        awaitState(model) { it.matches.size == 1 && !it.loading }

        // The reader edits the draft; the submitted snapshot must own the save.
        model.setQuery("edited draft")
        model.setFilter(SearchFilter(SearchMode.FULLTEXT, subscriptionId = 99))
        model.setSavedName("My saved search")
        model.saveNew()

        val create = http.awaitRequest { it.method == "POST" && it.url.encodedPath == "/v1/saved-searches" }
        val body = bodyOf(create)
        assertNotNull("create request had no body", body)
        val bodyObject = body!!.jsonObject
        assertEquals("My saved search", bodyObject["name"]!!.jsonPrimitive.content)
        assertEquals("draft query", bodyObject["query"]!!.jsonPrimitive.content)
        val filters = bodyObject["filters"]!!.jsonObject
        assertEquals("hybrid", filters["mode"]!!.jsonPrimitive.content)
        assertEquals(5L, filters["subscription_id"]!!.jsonPrimitive.content.toLong())
        assertEquals(6L, filters["folder_id"]!!.jsonPrimitive.content.toLong())
        assertFalse("an unsubmitted limit appeared in the create body", filters.containsKey("limit"))

        // The response is held: success must not be published yet.
        assertTrue("saving was not published", model.state.value.saving)
        assertNull("success published before the server responded", model.state.value.notice)
        assertEquals(1, model.state.value.savedSearches.size)

        createGate.complete(Unit)
        val saved = awaitState(model) { it.notice == "Search saved." && !it.saving }
        assertEquals(2, saved.savedSearches.size)
        assertTrue(saved.savedSearches.any { it.id == 42L && it.name == "My saved search" })
        assertEquals(1, createCount.get())

        // A failed create publishes an error, no notice, and no list change.
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/saved-searches" && request.method == "GET" ->
                    http.json(request, "[" + savedJson(7, "Stored", "stored") + "]")
                request.method == "POST" && request.url.encodedPath == "/v1/saved-searches" ->
                    http.response(request, 500, """{"error":"duplicate"}""")
                else -> http.json(request, "{}")
            }
        }
        model.setSavedName("Another search")
        model.saveNew()
        http.awaitRequest {
            it.method == "POST" && it.url.encodedPath == "/v1/saved-searches" &&
                bodyOf(it)?.jsonObject?.get("name")?.jsonPrimitive?.content == "Another search"
        }
        val failure = awaitState(model) { it.error != null && !it.saving }
        assertEquals(
            "Could not save the search change: Earnest request failed (500): duplicate",
            failure.error,
        )
        assertNull(failure.notice)
        assertEquals(2, failure.savedSearches.size)
    }

    @Test
    fun saveNewRejectsBlankNameOrMissingSubmittedQuery() = runBlocking {
        http.handler = { http.json(it, "{}") }
        newSession("token-a").activate()
        val model = model()

        // No submitted query yet.
        model.setSavedName("Named")
        model.saveNew()
        assertEquals("Enter a name and run a search before saving it.", model.state.value.error)
        assertTrue(http.requests.none { it.method == "POST" })

        // A submitted query plus a blank name is still rejected.
        http.handler = { request ->
            if (request.url.encodedPath == "/v1/search") http.json(request, pageJson(listOf(entryJson(60))))
            else http.json(request, "{}")
        }
        model.setQuery("query")
        model.submit()
        awaitState(model) { it.matches.isNotEmpty() && !it.loading }
        model.setSavedName("   ")
        model.saveNew()
        assertEquals("Enter a name and run a search before saving it.", model.state.value.error)
        assertTrue(http.requests.none { it.method == "POST" })
    }

    @Test
    fun renameUsesSelectedSavedSearchAndRequiresName() = runBlocking {
        val savedList = """[{"id":7,"name":"Old name","query":"stored query","filters":{"mode":"fulltext"}}]"""
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/saved-searches" && request.method == "GET" ->
                    http.json(request, savedList)
                request.url.encodedPath == "/v1/search" ->
                    http.json(request, pageJson(listOf(entryJson(70))))
                request.method == "PATCH" -> http.json(request, "{}")
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.loadMetadata()
        awaitState(model) { it.savedSearches.size == 1 && !it.loadingMetadata }
        model.selectSaved(SavedSearch(7L, "Old name", "stored query", SearchFilter(SearchMode.FULLTEXT)))
        awaitState(model) { it.selectedSavedId == 7L && it.query == "stored query" && !it.loading }

        model.setSavedName("   ")
        model.rename()
        assertEquals("Enter a saved search name.", model.state.value.error)
        assertTrue(http.requests.none { it.method == "PATCH" })

        model.setSavedName("Renamed")
        model.rename()
        val patch = http.awaitRequest { it.method == "PATCH" && it.url.encodedPath == "/v1/saved-searches/7" }
        assertEquals("""{"name":"Renamed"}""", bodyOf(patch).toString())
        val renamed = awaitState(model) { it.notice == "Saved search renamed." && !it.saving }
        assertEquals("Renamed", renamed.savedSearches.single { it.id == 7L }.name)
        assertEquals("Renamed", renamed.savedName)
        assertEquals(7L, renamed.selectedSavedId)

        // A failed rename publishes no notice and leaves the old name.
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/saved-searches" && request.method == "GET" ->
                    http.json(request, savedList)
                request.method == "PATCH" -> http.response(request, 500, """{"error":"rename_failed"}""")
                else -> http.json(request, "{}")
            }
        }
        model.setSavedName("Another rename")
        model.rename()
        http.awaitRequest {
            it.method == "PATCH" && bodyOf(it)?.jsonObject?.get("name")?.jsonPrimitive?.content == "Another rename"
        }
        val failure = awaitState(model) { it.error != null && !it.saving }
        assertTrue("unexpected error: ${failure.error}", failure.error!!.startsWith("Could not save the search change"))
        assertNull(failure.notice)
        assertEquals("Renamed", failure.savedSearches.single { it.id == 7L }.name)

        // A rename with no selection is rejected without a request.
        val unselected = model()
        unselected.setSavedName("Orphan")
        unselected.rename()
        assertEquals("Enter a saved search name.", unselected.state.value.error)
        assertEquals(2, http.requests.count { it.method == "PATCH" })
    }

    @Test
    fun deleteRequiresSelectionAndPublishesOnlyAfterServerSuccess() = runBlocking {
        val twoSaved = """
            [{"id":7,"name":"Keep","query":"k","filters":{"mode":"fulltext"}},
             {"id":8,"name":"Delete me","query":"d","filters":{"mode":"fulltext"}}]
        """.trimIndent()
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/saved-searches" && request.method == "GET" ->
                    http.json(request, twoSaved)
                request.url.encodedPath == "/v1/search" ->
                    http.json(request, pageJson(listOf(entryJson(80))))
                request.method == "DELETE" && request.url.encodedPath == "/v1/saved-searches/8" ->
                    http.json(request, "{}")
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.loadMetadata()
        awaitState(model) { it.savedSearches.size == 2 && !it.loadingMetadata }

        model.delete()
        assertTrue("delete without selection issued a request", http.requests.none { it.method == "DELETE" })

        model.selectSaved(model.state.value.savedSearches.single { it.id == 8L })
        awaitState(model) { it.selectedSavedId == 8L && !it.loading }
        model.delete()
        http.awaitRequest { it.method == "DELETE" && it.url.encodedPath == "/v1/saved-searches/8" }
        val deleted = awaitState(model) { it.notice == "Saved search deleted." && !it.saving }
        assertEquals(listOf(7L), deleted.savedSearches.map { it.id })
        assertNull(deleted.selectedSavedId)
        assertEquals("", deleted.savedName)

        // A failed delete keeps the saved search and its selection.
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/saved-searches" && request.method == "GET" ->
                    http.json(request, "[" + savedJson(7, "Keep", "k") + "]")
                request.url.encodedPath == "/v1/search" ->
                    http.json(request, pageJson(listOf(entryJson(81))))
                request.method == "DELETE" -> http.response(request, 500, """{"error":"delete_failed"}""")
                else -> http.json(request, "{}")
            }
        }
        model.loadMetadata()
        awaitState(model) { it.savedSearches.size == 1 && !it.loadingMetadata }
        model.selectSaved(model.state.value.savedSearches.single())
        awaitState(model) { it.selectedSavedId == 7L && !it.loading }
        model.delete()
        http.awaitRequest { it.method == "DELETE" && it.url.encodedPath == "/v1/saved-searches/7" }
        val failure = awaitState(model) { it.error != null && !it.saving }
        assertTrue("unexpected error: ${failure.error}", failure.error!!.startsWith("Could not save the search change"))
        assertNull(failure.notice)
        assertEquals(listOf(7L), failure.savedSearches.map { it.id })
        assertEquals(7L, failure.selectedSavedId)
    }

    // ---------------------------------------------------------------------
    // Fencing: cancellation, late responses, sessions, generations
    // ---------------------------------------------------------------------

    @Test
    fun staleSavedSelectionCannotSendPrivateQueryToTheNextAccount() = runBlocking {
        val dispatched = CompletableDeferred<Request>()
        http.handler = { request ->
            when (request.url.encodedPath) {
                "/v1/saved-searches" -> http.json(request, """[{"id":7,"name":"Private view","query":"private account-a query","filters":{"mode":"fulltext"}}]""")
                "/v1/pipeline" -> http.json(request, """{"embed":"enabled"}""")
                else -> {
                    dispatched.complete(request)
                    http.json(request, pageJson(emptyList()))
                }
            }
        }
        newSession("token-a").activate()
        val model = model()
        model.loadMetadata()
        val stale = awaitState(model) { it.savedSearches.size == 1 && !it.loadingMetadata }.savedSearches.single()
        model.reset()
        newSession("token-b").activate()
        model.selectSaved(stale)
        assertNull("a stale saved definition was dispatched to the next account", withTimeoutOrNull(500) { dispatched.await() })
        assertTrue(model.state.value.query.isEmpty())
        assertTrue(model.state.value.submittedQuery.isEmpty())
        assertTrue(model.state.value.savedName.isEmpty())
    }

    @Test
    fun resetBetweenSubmissionAndDispatchCannotReuseTheNewEpoch() = runBlocking {
        val dispatched = CompletableDeferred<Request>()
        val reset = CompletableDeferred<Unit>()
        http.handler = { request ->
            dispatched.complete(request)
            http.json(request, pageJson(emptyList()))
        }
        newSession("token-a").activate()
        val model = model()
        scope.launch {
            model.state.collect { state ->
                if (!reset.isCompleted && state.submittedQuery == "validated") {
                    model.reset()
                    reset.complete(Unit)
                }
            }
        }
        model.setQuery("validated")
        model.submit()
        withTimeout(5_000) { reset.await() }
        assertNull("a reset intent was dispatched under the replacement epoch", withTimeoutOrNull(500) { dispatched.await() })
        assertTrue(model.state.value.matches.isEmpty())
        assertTrue(model.state.value.submittedQuery.isEmpty())
        assertFalse(model.state.value.loading)
    }

    @Test
    fun lateResponseAfterNewQueryCannotPopulateState() = runBlocking {
        val firstGate = http.newGate()
        val secondGate = http.newGate()
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/search" && request.url.queryParameter("q") == "first" ->
                    http.held(request, firstGate, pageJson(listOf(entryJson(99, title = "late first"))))
                request.url.encodedPath == "/v1/search" && request.url.queryParameter("q") == "second" ->
                    http.held(request, secondGate, pageJson(listOf(entryJson(20))))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.setQuery("first")
        model.submit()
        http.awaitRequest { it.url.encodedPath == "/v1/search" && it.url.queryParameter("q") == "first" }

        model.setQuery("second")
        model.submit()
        http.awaitRequest { it.url.encodedPath == "/v1/search" && it.url.queryParameter("q") == "second" }

        secondGate.complete(Unit)
        val settled = awaitState(model) { it.matches.size == 1 && !it.loading }
        assertEquals("entry:20:0", settled.matches.single().key)

        // The first request's response arrives after the new query settled.
        firstGate.complete(Unit)
        assertStateStaysFenced(model, 500) { state -> state.matches.any { it.entryId == 99L } }
        assertEquals("entry:20:0", model.state.value.matches.single().key)
    }

    @Test
    fun lateResponseAfterResetCannotPopulateState() = runBlocking {
        val gate = http.newGate()
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/search" ->
                    http.held(request, gate, pageJson(listOf(entryJson(98, title = "late"))))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.setQuery("reset me")
        model.submit()
        http.awaitRequest { it.url.encodedPath == "/v1/search" }
        model.reset()
        assertEquals("", model.state.value.query)
        assertEquals("", model.state.value.submittedQuery)
        assertTrue(model.state.value.matches.isEmpty())
        assertFalse(model.state.value.loading)

        gate.complete(Unit)
        assertStateStaysFenced(model, 500) { state -> state.matches.isNotEmpty() || state.query.isNotEmpty() }
        assertEquals("", model.state.value.query)
        assertTrue(model.state.value.matches.isEmpty())
    }

    @Test
    fun lateResponseAfterSessionChangeCannotPopulateNewSessionState() = runBlocking {
        val staleGate = http.newGate()
        val freshGate = http.newGate()
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/search" && request.url.queryParameter("q") == "stale" ->
                    http.held(request, staleGate, pageJson(listOf(entryJson(97, title = "stale"))))
                request.url.encodedPath == "/v1/search" && request.url.queryParameter("q") == "fresh" ->
                    http.held(request, freshGate, pageJson(listOf(entryJson(21))))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val second = newSession("token-b")
        val model = model()

        model.setQuery("stale")
        model.submit()
        http.awaitRequest { it.url.encodedPath == "/v1/search" && it.url.queryParameter("q") == "stale" }

        second.activate()
        model.setQuery("fresh")
        model.submit()
        http.awaitRequest { it.url.encodedPath == "/v1/search" && it.url.queryParameter("q") == "fresh" }

        freshGate.complete(Unit)
        val settled = awaitState(model) { it.matches.size == 1 && !it.loading }
        assertEquals("entry:21:0", settled.matches.single().key)

        staleGate.complete(Unit)
        assertStateStaysFenced(model, 500) { state -> state.matches.any { it.entryId == 97L } }
        assertEquals("entry:21:0", model.state.value.matches.single().key)
    }

    @Test
    fun lateMetadataResponseAfterResetCannotPopulateState() = runBlocking {
        val gate = http.newGate()
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/saved-searches" ->
                    http.held(request, gate, "[" + savedJson(1, "Late", "q") + "]")
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.loadMetadata()
        http.awaitRequest { it.url.encodedPath == "/v1/saved-searches" }
        model.reset()
        assertFalse(model.state.value.loadingMetadata)
        assertTrue(model.state.value.savedSearches.isEmpty())

        gate.complete(Unit)
        assertStateStaysFenced(model, 500) { state -> state.savedSearches.isNotEmpty() || state.loadingMetadata || state.metadataError != null }
        assertTrue(model.state.value.savedSearches.isEmpty())
        assertFalse(model.state.value.loadingMetadata)
    }

    @Test
    fun unauthorizedInvokesOnlyTheCurrentSessionsCallback() = runBlocking {
        http.handler = { request ->
            if (request.url.encodedPath == "/v1/search") {
                http.response(request, 401, """{"error":"expired"}""")
            } else {
                http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        model.setQuery("denied")
        model.submit()
        awaitState(model) { !it.loading }
        assertEquals(listOf("token-a"), unauthorizedCalls.toList())
        assertNull("unauthorized response surfaced as a normal error", model.state.value.error)

        newSession("token-b").activate()
        model.setQuery("denied again")
        model.submit()
        awaitState(model) { unauthorizedCalls.size == 2 }
        assertEquals(listOf("token-a", "token-b"), unauthorizedCalls.toList())
        assertNull(model.state.value.error)
    }

    @Test
    fun staleUnauthorizedResponseDoesNotInvokeAnyCallback() = runBlocking {
        val gate = http.newGate()
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/search" && request.url.queryParameter("q") == "stale" ->
                    http.heldResponse(request, gate, http.response(request, 401, """{"error":"expired"}"""))
                request.url.encodedPath == "/v1/search" ->
                    http.json(request, pageJson(listOf(entryJson(22))))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val second = newSession("token-b")
        val model = model()

        model.setQuery("stale")
        model.submit()
        http.awaitRequest { it.url.encodedPath == "/v1/search" && it.url.queryParameter("q") == "stale" }

        // Switch sessions before the stale 401 arrives; the new session searches fine.
        second.activate()
        model.setQuery("fresh")
        model.submit()
        val settled = awaitState(model) { it.matches.isNotEmpty() && !it.loading }
        assertEquals("entry:22:0", settled.matches.single().key)

        gate.complete(Unit)
        assertNull("a stale 401 invoked an unauthorized callback", withTimeoutOrNull(500) { unauthorizedEvents.first() })
        assertEquals("a stale 401 invoked an unauthorized callback", 0, unauthorizedCalls.size)
        assertEquals("entry:22:0", model.state.value.matches.single().key)
    }

    @Test
    fun generationChangeClearsStaleSearchState() = runBlocking {
        http.handler = { request ->
            when {
                request.url.encodedPath == "/v1/search" ->
                    http.json(request, pageJson(listOf(entryJson(31))))
                else -> http.json(request, "{}")
            }
        }
        newSession("token-a").activate()
        val model = model()

        // First sighting of a generation does not reset a fresh model.
        model.invalidateForGeneration("g1")
        model.setQuery("one")
        model.submit()
        awaitState(model) { it.matches.size == 1 && !it.loading }

        // A different generation resets stale state.
        model.invalidateForGeneration("g2")
        assertEquals("", model.state.value.query)
        assertEquals("", model.state.value.submittedQuery)
        assertTrue(model.state.value.matches.isEmpty())
        assertNull(model.state.value.nextCursor)
        assertFalse(model.state.value.loading)

        // Repeat searches under the new generation survive a repeat notification.
        model.setQuery("one")
        model.submit()
        awaitState(model) { it.matches.size == 1 && !it.loading }
        model.invalidateForGeneration("g2")
        assertTrue(model.state.value.matches.isNotEmpty())
        assertEquals("one", model.state.value.submittedQuery)

        // A further generation change resets again.
        model.invalidateForGeneration("g3")
        assertTrue(model.state.value.matches.isEmpty())
        assertEquals("", model.state.value.submittedQuery)
    }

    // ---------------------------------------------------------------------
    // Guardrails
    // ---------------------------------------------------------------------

    @Test
    fun submitWithoutSessionReportsSignInError() = runBlocking {
        val model = model()
        model.setQuery("q")
        model.submit()
        assertEquals("Still signing in; search did not run.", model.state.value.error)
        assertTrue(http.requests.isEmpty())
    }

    @Test
    fun submitRejectsBlankAndOversizedQueriesWithoutRequest() = runBlocking {
        newSession("token-a").activate()
        val model = model()

        model.setQuery("   ")
        model.submit()
        assertEquals("Enter a search query of at most 4,096 bytes.", model.state.value.error)

        model.setQuery("a".repeat(4097))
        model.submit()
        assertEquals("Enter a search query of at most 4,096 bytes.", model.state.value.error)
        assertTrue(http.requests.isEmpty())
    }

    // ---------------------------------------------------------------------
    // Helpers
    // ---------------------------------------------------------------------

    private suspend fun awaitState(
        model: SearchModel,
        // The shared CI runner schedules sluggishly; a state that normally
        // arrives in milliseconds has taken over five seconds under load.
        timeoutMs: Long = 15_000,
        predicate: (SearchUiState) -> Boolean,
    ): SearchUiState = try {
        withTimeout(timeoutMs) { model.state.first(predicate) }
    } catch (error: TimeoutCancellationException) {
        throw AssertionError(
            "Timed out after ${timeoutMs}ms waiting for search state; last state was ${model.state.value}",
            error,
        )
    }

    /** Fails if [predicate] ever observes a state, giving a released request time to leak. */
    private suspend fun assertStateStaysFenced(
        model: SearchModel,
        timeoutMs: Long = 500,
        predicate: (SearchUiState) -> Boolean,
    ) {
        val leaked = withTimeoutOrNull(timeoutMs) { model.state.first(predicate) }
        assertNull("a request that should have been fenced published state: $leaked", leaked)
    }

    private fun bodyOf(request: Request): JsonElement? {
        val buffer = Buffer()
        request.body?.writeTo(buffer)
        val text = buffer.readUtf8()
        return if (text.isBlank()) null else Json.parseToJsonElement(text)
    }

    private fun entryJson(entryId: Long, title: String = "Article $entryId"): String =
        """{"kind":"entry","entry_id":$entryId,"subscription_id":11,"feed_id":22,"seq":$entryId,"title":"$title","url":"https://e.test/$entryId","snippet":"snippet $entryId","rank":1.0,"untrusted":false}"""

    private fun noteJson(entryId: Long, annotationId: Long?, title: String = "Note $entryId"): String {
        val annotation =
            if (annotationId != null) ",\"annotation_id\":$annotationId,\"agent_name\":\"Morning Agent\"" else ""
        return """{"kind":"agent_note","entry_id":$entryId,"subscription_id":11,"feed_id":22,"seq":$entryId,"title":"$title","url":"https://e.test/$entryId","snippet":"note snippet","rank":0.5,"untrusted":true$annotation}"""
    }

    private fun pageJson(matches: List<String>, nextCursor: String? = null, status: String? = null): String {
        val builder = StringBuilder("{\"matches\":[").append(matches.joinToString(",")).append("]")
        if (nextCursor != null) builder.append(",\"next_cursor\":\"$nextCursor\"")
        if (status != null) builder.append(",\"status\":\"$status\"")
        return builder.append("}").toString()
    }

    private fun savedJson(
        id: Long,
        name: String,
        query: String,
        filters: String = "{\"mode\":\"fulltext\"}",
    ): String = """{"id":$id,"name":"$name","query":"$query","filters":$filters}"""

    /** Scripted OkHttp transport: records every request and serves [handler] without real network. */
    private class ScriptedHttp {
        val requests = CopyOnWriteArrayList<Request>()
        private val incoming = LinkedBlockingQueue<Request>()
        private val gates = CopyOnWriteArrayList<CompletableDeferred<Unit>>()

        @Volatile
        var handler: (Request) -> Response = { json(it, "{}") }

        val client: OkHttpClient = OkHttpClient.Builder()
            .addInterceptor { chain ->
                val request = chain.request()
                requests += request
                incoming += request
                handler(request)
            }
            .build()

        fun newGate(): CompletableDeferred<Unit> {
            val gate = CompletableDeferred<Unit>()
            gates += gate
            return gate
        }

        fun releaseAll() {
            gates.forEach { it.complete(Unit) }
        }

        fun awaitRequest(timeoutMs: Long = 5_000, predicate: (Request) -> Boolean = { true }): Request {
            val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs)
            while (true) {
                requests.firstOrNull(predicate)?.let { return it }
                val remainingNanos = deadline - System.nanoTime()
                if (remainingNanos <= 0) break
                incoming.poll(remainingNanos, TimeUnit.NANOSECONDS)
            }
            throw AssertionError(
                "Timed out after ${timeoutMs}ms waiting for an HTTP request; captured requests: " +
                    requests.joinToString { "${it.method} ${it.url.encodedPath}?${it.url.encodedQuery}" },
            )
        }

        fun response(request: Request, status: Int = 200, body: String): Response =
            Response.Builder()
                .request(request)
                .protocol(Protocol.HTTP_1_1)
                .code(status)
                .message("test")
                .body(body.toResponseBody(JSON_MEDIA_TYPE))
                .build()

        fun json(request: Request, body: String): Response = response(request, 200, body)

        fun held(request: Request, gate: CompletableDeferred<Unit>, body: String): Response {
            runBlocking { gate.await() }
            return json(request, body)
        }

        fun heldResponse(request: Request, gate: CompletableDeferred<Unit>, response: Response): Response {
            runBlocking { gate.await() }
            return response
        }

        companion object {
            private val JSON_MEDIA_TYPE = "application/json".toMediaType()
        }
    }
}

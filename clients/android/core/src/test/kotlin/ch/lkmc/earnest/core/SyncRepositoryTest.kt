package ch.lkmc.earnest.core

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import org.junit.Assert.*
import org.junit.Test

class SyncRepositoryTest {
    private val json = Json { encodeDefaults = true }
    private class Store(var data: SyncSnapshot) : SyncStore {
        override fun load() = data
        override fun save(snapshot: SyncSnapshot) { data = snapshot }
    }

    private fun api(handler: (okhttp3.Request) -> Pair<Int, String>): EarnestApi = EarnestApi(
        "https://earnest.test", "token",
        OkHttpClient.Builder().addInterceptor { chain ->
            val (status, body) = handler(chain.request())
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
                .code(status).message("test").body(body.toResponseBody()).build()
        }.build(),
    )

    private fun write(id: String, entry: Long = 2, register: String = "shelf", value: JsonElement = JsonPrimitive("later")) =
        StateWrite(id, 1, entry, register, value, Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit")

    private fun engine(pending: List<Pending> = emptyList(), entries: List<CachedEntry> = emptyList()) =
        SyncEngine(Store(SyncSnapshot(Revision("g1", 0), entries = entries, outbox = pending)))

    @Test
    fun batchesCollectEachItemOnceAndStopBeforeRepeatedTarget() = runBlocking {
        val writes = listOf(write("one"), write("two", 3), write("three", value = JsonPrimitive("done")), write("four", register = "kept", value = JsonPrimitive(true)))
        val engine = engine(writes.map { Pending(write = it, inFlight = true) })
        val batches = mutableListOf<List<String>>()
        val api = api { request ->
            assertEquals("/v1/state:batch", request.url.encodedPath)
            val buffer = Buffer()
            request.body!!.writeTo(buffer)
            val batch = json.decodeFromString(kotlinx.serialization.builtins.ListSerializer(StateWrite.serializer()), buffer.readUtf8())
            assertEquals("duplicate IDs in batch", batch.size, batch.map { it.opId }.toSet().size)
            assertEquals("duplicate targets in batch", batch.size, batch.map { Triple(it.principal, it.entryId, it.register) }.toSet().size)
            batches += batch.map { it.opId }
            200 to json.encodeToString(kotlinx.serialization.builtins.ListSerializer(Result.serializer()), batch.map {
                Result(it.opId, "applied", SyncEngine.initialState(it.principal, it.entryId), false)
            })
        }
        SyncRepository(api, engine).flush()
        assertEquals(listOf(listOf("one", "two"), listOf("three", "four")), batches)
        assertTrue(engine.snapshot().outbox.isEmpty())
    }

    @Test
    fun batchesRespectLimitAndRangeOrder() = runBlocking {
        val writes = (1L..101L).map { Pending(write = write("op-$it", it)) }
        val range = RangeWrite(write("range", register = "seen", value = JsonPrimitive("seen")), Scope("all"), 101)
        val engine = engine(writes + Pending(range = range) + Pending(write = write("last", 102)))
        val order = mutableListOf<String>()
        val repository = SyncRepository(api { request ->
            val buffer = Buffer()
            request.body!!.writeTo(buffer)
            if (request.url.encodedPath == "/v1/state:through") {
                order += "range"
                200 to json.encodeToString(RangeResult.serializer(), RangeResult("applied", 101, emptyList(), emptyList(), emptyList(), false))
            } else {
                val batch = json.decodeFromString(kotlinx.serialization.builtins.ListSerializer(StateWrite.serializer()), buffer.readUtf8())
                order += "batch:${batch.size}:${batch.first().opId}"
                200 to json.encodeToString(kotlinx.serialization.builtins.ListSerializer(Result.serializer()), batch.map {
                    Result(it.opId, "applied", SyncEngine.initialState(it.principal, it.entryId), false)
                })
            }
        }, engine)
        repository.flush()
        assertEquals(listOf("batch:100:op-1", "batch:1:op-101", "range", "batch:1:last"), order)
        assertTrue(engine.snapshot().outbox.isEmpty())
    }

    @Test
    fun failedBatchRetriesExactPayloadBeforeNewIntent() = runBlocking {
        val engine = engine(listOf(Pending(write = write("original"))))
        val submitted = mutableListOf<String>()
        var failRequest = true
        val repository = SyncRepository(api { request ->
            val buffer = Buffer()
            request.body!!.writeTo(buffer)
            val body = buffer.readUtf8()
            submitted += body
            if (failRequest) throw java.io.IOException("offline")
            val batch = json.decodeFromString(kotlinx.serialization.builtins.ListSerializer(StateWrite.serializer()), body)
            200 to json.encodeToString(kotlinx.serialization.builtins.ListSerializer(Result.serializer()), batch.map {
                Result(it.opId, "applied", SyncEngine.initialState(it.principal, it.entryId), false)
            })
        }, engine)
        try {
            repository.flush()
            fail("request should fail")
        } catch (_: java.io.IOException) {
            assertTrue(engine.snapshot().outbox.single().inFlight)
        }
        engine.queue(write("new", value = JsonPrimitive("done")))
        failRequest = false
        repository.flush()
        assertEquals(3, submitted.size)
        assertEquals(submitted[0], submitted[1])
        assertTrue(submitted[2].contains("new"))
        assertTrue(engine.snapshot().outbox.isEmpty())
    }

    @Test
    fun nativeReaderAlwaysRequestsClickToLoadImages() = runBlocking {
        val api = api { request ->
            assertEquals("click", request.url.queryParameter("mode"))
            200 to """{"url":"https://content.test/signed","expires_at":"2026-11-03T01:00:00Z"}"""
        }
        api.contentUrl(2)
        Unit
    }

    private val event = """{"id":2,"subscription_id":3,"seq":4,"first_seen_at":"2026-11-03T00:00:00Z","rev":1,"deleted":false}"""
    private val detail = """{"id":2,"subscription_id":3,"feed_id":5,"seq":4,"first_seen_at":"2026-11-03T00:00:00Z","title":"Headline","url":"https://news.test/article","author":"Author","rev":2,"deleted":false}"""

    private fun changes(request: okhttp3.Request, payload: String): Pair<Int, String> {
        return 200 to if (request.url.queryParameter("rev") == "0") {
            """{"changes":[{"rev":1,"type":"entry.updated","payload":$payload}],"next":{"generation":"g1","rev":1},"next_cursor":"g1:1"}"""
        } else {
            """{"changes":[],"next":{"generation":"g1","rev":1},"next_cursor":"g1:1"}"""
        }
    }

    @Test
    fun incrementalEntriesHydrateMetadataBeforeMerging() = runBlocking {
        for (cached in listOf(emptyList(), listOf(CachedEntry(2, title = "Old headline")))) {
            val engine = engine(entries = cached)
            var hydrated = false
            val api = api { request ->
                if (request.url.encodedPath == "/v1/entries/2") {
                    hydrated = true
                    200 to detail
                } else changes(request, """{"entry":$event}""")
            }
            SyncRepository(api, engine).pull()
            assertEquals("Headline", engine.snapshot().entries.single().title)
            assertEquals(5L, engine.snapshot().entries.single().feedId)
            assertEquals(2L, engine.snapshot().entries.single().rev)
            assertTrue(hydrated)
        }
    }

    @Test
    fun missingHydrationBecomesTombstone() = runBlocking {
        val engine = engine(entries = listOf(CachedEntry(2, title = "Old headline")))
        val api = api { request ->
            if (request.url.encodedPath == "/v1/entries/2") 404 to "{}"
            else changes(request, """{"entry":$event}""")
        }
        SyncRepository(api, engine).pull()
        assertTrue(engine.snapshot().entries.single().deleted)
        assertEquals(1L, engine.snapshot().cursor.rev)
    }

    private fun reload(engine: SyncEngine): SyncEngine {
        val encoded = json.encodeToString(SyncSnapshot.serializer(), engine.snapshot())
        return SyncEngine(Store(json.decodeFromString(SyncSnapshot.serializer(), encoded)))
    }

    @Test
    fun missingNotificationMetadataRecoversAfterNewerLiveBackfill() = runBlocking {
        var engine = engine()
        assertNull(SyncRepository(api { 404 to "{}" }, engine).loadEntry(2))
        engine = reload(engine)
        assertTrue(engine.snapshot().entries.single().deleted)
        val state = SyncEngine.initialState(1, 2).let {
            it.copy(seen = it.seen.copy(value = JsonPrimitive("seen"), rev = 2), stateRev = 2)
        }
        val result = json.encodeToString(Result.serializer(), Result("backfill", "applied", state, false))
        val api = api { request ->
            when {
                request.url.encodedPath == "/v1/entries/2" -> 200 to detail
                request.url.queryParameter("rev") == "0" -> 200 to """{"changes":[{"rev":1,"type":"entry.created","payload":{"entry":$event}},{"rev":2,"type":"state.updated","payload":{"results":[$result]}}],"next":{"generation":"g1","rev":2},"next_cursor":"g1:2"}"""
                else -> 200 to """{"changes":[],"next":{"generation":"g1","rev":2},"next_cursor":"g1:2"}"""
            }
        }
        SyncRepository(api, engine).pull()
        assertFalse("metadata 404 suppressed newer live backfill", engine.snapshot().entries.single().deleted)
        assertEquals("Headline", engine.snapshot().entries.single().title)
        assertEquals(JsonPrimitive("seen"), engine.state(1, 2).seen.value)
        assertEquals("Headline", SyncRepository(api, engine).loadEntry(2)?.title)
    }

    @Test
    fun missingHydrationRecoversAfterNewerLiveBackfill() = runBlocking {
        var engine = engine(entries = listOf(CachedEntry(2, title = "Old headline")))
        SyncRepository(api { request ->
            if (request.url.encodedPath == "/v1/entries/2") 404 to "{}"
            else changes(request, """{"entry":$event}""")
        }, engine).pull()
        engine = reload(engine)
        val api = api { request ->
            when {
                request.url.encodedPath == "/v1/entries/2" -> 200 to detail
                request.url.queryParameter("rev") == "1" -> 200 to """{"changes":[{"rev":2,"type":"entry.created","payload":{"entry":${event.replace("\"rev\":1", "\"rev\":2")}}}],"next":{"generation":"g1","rev":2},"next_cursor":"g1:2"}"""
                else -> 200 to """{"changes":[],"next":{"generation":"g1","rev":2},"next_cursor":"g1:2"}"""
            }
        }
        SyncRepository(api, engine).pull()
        assertFalse("hydration 404 suppressed newer live backfill", engine.snapshot().entries.single().deleted)
        assertEquals("Headline", engine.snapshot().entries.single().title)
        assertEquals(2L, engine.snapshot().cursor.rev)
    }

    @Test
    fun metadata404OnlyAcceptsNewerProjections() = runBlocking {
        for (rev in listOf(0L, 1L, 2L)) {
            val engine = engine()
            SyncRepository(api { request ->
                if (request.url.encodedPath == "/v1/entries/2") 404 to "{}"
                else changes(request, """{"entry":$event}""")
            }, engine).pull()
            val restored = reload(engine)
            restored.mergeBootstrap(SyncChange(0, entry = CachedEntry(2, title = "Live", rev = rev)))
            assertEquals("projection rev=$rev", rev <= 1L, restored.entry(2)!!.deleted)
        }
    }

    @Test
    fun authoritativeDeletionCannotBeResurrected() = runBlocking {
        for (incremental in listOf(false, true)) {
            val engine = engine()
            assertNull(SyncRepository(api { 404 to "{}" }, engine).loadEntry(2))
            SyncRepository(api { request ->
                assertEquals("/v1/changes", request.url.encodedPath)
                changes(request, """{"entry":${event.replace("false", "true")}}""")
            }, engine).pull()
            val restored = reload(engine)
            val live = SyncChange(2, entry = CachedEntry(2, title = "Live", rev = 2))
            if (incremental) restored.applyPage(listOf(live), Revision("g1", 2))
            else restored.mergeBootstrap(live)
            assertTrue("authoritative tombstone was resurrected", restored.entry(2)!!.deleted)
        }
    }

    @Test
    fun hydrationFailureDoesNotAdvancePage() = runBlocking {
        val engine = engine(entries = listOf(CachedEntry(2, title = "Old headline")))
        val api = api { request ->
            if (request.url.encodedPath == "/v1/entries/2") 503 to "{}"
            else changes(request, """{"entry":$event}""")
        }
        try {
            SyncRepository(api, engine).pull()
            fail("failed hydration committed a page")
        } catch (_: ApiException) {
            assertEquals(0L, engine.snapshot().cursor.rev)
            assertEquals("Old headline", engine.snapshot().entries.single().title)
        }
    }

    @Test
    fun subscriptionChangeKeepsSiblingNativeProjection() = runBlocking {
        val engine = engine()
        val payload = """{"subscription":{"id":3,"title":"Weekly","cadence":"week","rev":1},"object":{"kind":"subscription","id":3,"name":"Weekly","rev":1}}"""
        SyncRepository(api { changes(it, payload) }, engine).pull()
        val projection = json.parseToJsonElement(engine.snapshot().objects.single().raw).jsonObject
        assertEquals(JsonPrimitive("week"), projection["cadence"])
    }
}

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

    private fun api(
        riverResponses: Map<String, () -> Pair<Int, String>> = emptyMap(),
        handler: (okhttp3.Request) -> Pair<Int, String>,
    ): EarnestApi = EarnestApi(
        "https://earnest.test", "token",
        OkHttpClient.Builder().addInterceptor { chain ->
            val (status, body) = riverResponses[chain.request().url.encodedPath]?.invoke() ?: when (chain.request().url.encodedPath) {
                "/v1/mutes" -> 200 to "[]"
                "/v1/settings" -> 200 to """{"today_decay_days":2,"rev":0}"""
                else -> handler(chain.request())
            }
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
                .code(status).message("test").body(body.toResponseBody()).build()
        }.build(),
    )

    private fun write(id: String, entry: Long = 2, register: String = "shelf", value: JsonElement = JsonPrimitive("later")) =
        StateWrite(id, 1, entry, register, value, Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit")

    private fun engine(pending: List<Pending> = emptyList(), entries: List<CachedEntry> = emptyList()) =
        SyncEngine(Store(SyncSnapshot(Revision("g1", 0), entries = entries, outbox = pending)))

    @Test
    fun failedReplacementPreservesOldCacheAndOnlyRebasesIntentAfterCompletion() = runBlocking {
        val pending = Pending(write = write("pending"), inFlight = true)
        val original = SyncSnapshot(Revision("g1", 8), entries = listOf(CachedEntry(99, title = "Offline")), outbox = listOf(pending))
        val store = Store(original)
        val engine = SyncEngine(store)
        var failPage = true
        val repository = SyncRepository(api { request ->
            val path = request.url.encodedPath
            when {
                path == "/v1/bootstrap" -> 200 to """{"generation":"g2","rev":5}"""
                path == "/v1/bootstrap/state" && failPage -> throw java.io.IOException("offline")
                path.startsWith("/v1/bootstrap/") -> 200 to """{"items":[],"next_id":0,"next_principal":0,"next_view":"","has_more":false}"""
                else -> error("unexpected request $path")
            }
        }, engine)
        try {
            repository.bootstrap()
            fail("bootstrap should fail")
        } catch (_: java.io.IOException) {
        }
        assertEquals(original, engine.snapshot())
        assertEquals(original, SyncEngine(store).snapshot())
        failPage = false
        repository.bootstrap()
        assertEquals(Revision("g2", 5), SyncEngine(store).snapshot().cursor)
        assertTrue(engine.snapshot().entries.isEmpty())
        assertEquals(pending.copy(write = pending.operation.copy(base = Revision("g2", 0))), engine.snapshot().outbox.single())
    }

    @Test
    fun interruptedBootstrapRetriesAfterReloadWithoutLosingIntent() = runBlocking {
        for (failedKind in listOf("entries", "subscriptions", "state")) {
            val pending = Pending(write = write("pending"))
            val store = Store(SyncSnapshot(Revision("", 0), outbox = listOf(pending)))
            var engine = SyncEngine(store)
            var failPage = true
            var starts = 0
            val api = api { request ->
                val path = request.url.encodedPath
                when {
                    path == "/v1/bootstrap" -> { starts++; 200 to """{"generation":"g1","rev":0}""" }
                    path == "/v1/bootstrap/$failedKind" && failPage -> throw java.io.IOException("interrupted $failedKind")
                    path.startsWith("/v1/bootstrap/") -> {
                        val items = if (path.endsWith("/entries")) "[$detail]" else "[]"
                        200 to """{"items":$items,"next_id":0,"next_principal":0,"next_view":"","has_more":false}"""
                    }
                    path == "/v1/changes" -> 200 to """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
                    else -> error("unexpected request $path")
                }
            }
            try {
                SyncRepository(api, engine).pull()
                fail("bootstrap should be interrupted")
            } catch (_: java.io.IOException) {
            }
            engine = SyncEngine(store)
            failPage = false
            SyncRepository(api, engine).pull()
            assertEquals("bootstrap was not retried after $failedKind", 2, starts)
            assertEquals("Headline", engine.entry(2)?.title)
            assertEquals(listOf("pending"), engine.snapshot().outbox.map { it.operation.opId })
        }
    }

    @Test
    fun retentionRecoveryReturnsRejectedWritesAndRanges() = runBlocking {
        for (range in listOf(false, true)) {
            val operation = write("rejected", register = "seen", value = JsonPrimitive("seen"))
            val pending = if (range) Pending(range = RangeWrite(operation, Scope("all"), 4)) else Pending(write = operation)
            val engine = engine(listOf(pending))
            var recovered = false
            val api = api { request ->
                when (val path = request.url.encodedPath) {
                    "/v1/changes" -> if (!recovered) 410 to "{}" else 200 to """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
                    "/v1/bootstrap" -> { recovered = true; 200 to """{"generation":"g1","rev":0}""" }
                    "/v1/state:through" -> 200 to json.encodeToString(RangeResult.serializer(), RangeResult("expired", 4, emptyList(), emptyList(), emptyList(), false))
                    "/v1/state:batch" -> 200 to json.encodeToString(kotlinx.serialization.builtins.ListSerializer(Result.serializer()), listOf(Result("rejected", "gone", SyncEngine.initialState(1, 2), false)))
                    else -> {
                        check(path.startsWith("/v1/bootstrap/"))
                        200 to """{"items":[],"next_id":0,"next_principal":0,"next_view":"","has_more":false}"""
                    }
                }
            }
            assertEquals(listOf(RejectedOperation("rejected", if (range) "expired" else "gone")), SyncRepository(api, engine).pull())
            assertTrue(engine.snapshot().outbox.isEmpty())
        }
    }

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
    fun nativeReaderDefaultsToProxiedImages() = runBlocking {
        val modes = java.util.concurrent.CopyOnWriteArrayList<String?>()
        val api = api { request ->
            modes += request.url.queryParameter("mode")
            200 to """{"url":"https://content.test/signed","expires_at":"2026-11-03T01:00:00Z"}"""
        }
        api.contentUrl(2)
        api.contentUrl(2, mode = "click")
        assertEquals(listOf(null, "click"), modes)
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
        val projection = json.parseToJsonElement(engine.snapshot().objects.single { it.kind == SyncEngine.OBJECT_SUBSCRIPTION }.raw).jsonObject
        assertEquals(JsonPrimitive("week"), projection["cadence"])
    }
    @Test
    fun riverResourcesReplayAndPersistAcrossReload() = runBlocking {
        val store = Store(SyncSnapshot(Revision("g1", 0)))
        val engine = SyncEngine(store)
        val initialMute = SyncObject(River.MUTE, 2, rev = 0, raw = """{"kind":"author","value":"Alice"}""")
        engine.replaceRiverResources(listOf(initialMute), SyncObject(River.SETTINGS, 1, rev = 0, raw = """{"today_decay_days":4}"""))
        var delivered = false
        val api = api { request ->
            assertEquals("/v1/changes", request.url.encodedPath)
            val records = if (delivered) "[]" else """[
                {"rev":1,"type":"mute.updated","payload":{"mute":{"id":2,"kind":"keyword","value":"Spoiler","rev":1,"deleted":false}}},
                {"rev":2,"type":"mute.deleted","payload":{"mute":{"id":2,"kind":"keyword","value":"Spoiler","rev":2,"deleted":true}}},
                {"rev":3,"type":"settings.updated","payload":{"settings":{"today_decay_days":7,"rev":3}}}
            ]"""
            delivered = true
            200 to """{"changes":$records,"next":{"generation":"g1","rev":3},"next_cursor":"g1:3"}"""
        }
        SyncRepository(api, engine).pull()
        val restored = SyncEngine(store)
        assertTrue(restored.snapshot().objects.single { it.kind == River.MUTE }.deleted)
        val settings = Json.parseToJsonElement(restored.snapshot().objects.single { it.kind == River.SETTINGS }.raw).jsonObject
        assertEquals(JsonPrimitive(7), settings["today_decay_days"])
        assertEquals(Revision("g1", 3), restored.snapshot().cursor)
        restored.beginBootstrap(Revision("new", 0))
        assertTrue(restored.snapshot().objects.isEmpty())
    }

    @Test
    fun incrementalPullSurvivesRiverServerFailure() = runBlocking {
        assertPullSurvivesRiverFailure { 503 to "{}" }
    }

    @Test
    fun incrementalPullSurvivesRiverNetworkFailure() = runBlocking {
        assertPullSurvivesRiverFailure { throw java.io.IOException("offline") }
    }

    private suspend fun assertPullSurvivesRiverFailure(failure: () -> Pair<Int, String>) {
        for (path in listOf("/v1/mutes", "/v1/settings")) {
            val original = SyncSnapshot(Revision("g1", 0), objects = listOf(
                SyncObject(River.MUTE, 2, rev = 0, raw = """{"kind":"author","value":"Alice"}"""),
                SyncObject(River.SETTINGS, 1, rev = 0, raw = """{"today_decay_days":4}"""),
            ))
            val store = Store(original)
            val engine = SyncEngine(store)
            var delivered = false
            val api = api(mapOf(path to failure)) { request ->
                assertEquals("/v1/changes", request.url.encodedPath)
                if (!delivered) assertEquals("$path refresh must not partially replace resources", original, engine.snapshot())
                val records = if (delivered) "[]" else """[
                    {"rev":1,"type":"mute.deleted","payload":{"mute":{"id":2,"kind":"author","value":"Alice","rev":1,"deleted":true}}},
                    {"rev":2,"type":"settings.updated","payload":{"settings":{"today_decay_days":7,"rev":2}}}
                ]"""
                delivered = true
                200 to """{"changes":$records,"next":{"generation":"g1","rev":2},"next_cursor":"g1:2"}"""
            }
            assertTrue(SyncRepository(api, engine).pull().isEmpty())
            val restored = SyncEngine(store).snapshot()
            assertEquals(Revision("g1", 2), restored.cursor)
            assertTrue(restored.objects.single { it.kind == River.MUTE }.deleted)
            val settings = Json.parseToJsonElement(restored.objects.single { it.kind == River.SETTINGS }.raw).jsonObject
            assertEquals(JsonPrimitive(7), settings["today_decay_days"])
        }
    }

    @Test
    fun incrementalPullPropagatesRiverAuthorizationFailures() = runBlocking {
        for (path in listOf("/v1/mutes", "/v1/settings")) {
            for (status in listOf(401, 403)) {
                val engine = engine()
                val original = engine.snapshot()
                val api = api(mapOf(path to { status to "{}" })) { error("must not replay after authorization failure") }
                try {
                    SyncRepository(api, engine).pull()
                    fail("$path HTTP $status must propagate")
                } catch (error: ApiException) {
                    assertEquals(status, error.status)
                }
                assertEquals(original, engine.snapshot())
            }
        }
    }

    @Test
    fun riverResourcePersistenceFailureStillPropagates() = runBlocking {
        val original = SyncSnapshot(Revision("g1", 0))
        val failure = java.io.IOException("cache write failed")
        val engine = SyncEngine(object : SyncStore {
            override fun load() = original
            override fun save(snapshot: SyncSnapshot) { throw failure }
        })
        val api = api { error("must not replay after persistence failure") }
        try {
            SyncRepository(api, engine).pull()
            fail("cache write failure must propagate")
        } catch (error: java.io.IOException) {
            assertSame(failure, error)
        }
    }

    @Test
    fun bootstrapResourceFailurePreservesCommittedSnapshot() = runBlocking {
        val original = SyncSnapshot(Revision("g1", 8), entries = listOf(CachedEntry(99, title = "Cached")))
        val store = Store(original)
        val engine = SyncEngine(store)
        val api = EarnestApi("https://earnest.test", "token", OkHttpClient.Builder().addInterceptor { chain ->
            val path = chain.request().url.encodedPath
            val (status, body) = when {
                path == "/v1/bootstrap" -> 200 to """{"generation":"g2","rev":0}"""
                path.startsWith("/v1/bootstrap/") -> 200 to """{"items":[],"next_id":0,"next_principal":0,"next_view":"","has_more":false}"""
                path == "/v1/mutes" -> 200 to "[]"
                path == "/v1/settings" -> 503 to "{}"
                else -> error("unexpected request $path")
            }
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(status).message("test")
                .body(body.toResponseBody()).build()
        }.build())
        try {
            SyncRepository(api, engine).bootstrap()
            fail("bootstrap completed without its river inputs")
        } catch (_: ApiException) {
        }
        assertEquals(original, engine.snapshot())
        assertEquals(original, SyncEngine(store).snapshot())
    }

    @Test
    fun expiredOperationsPersistAfterReload() = runBlocking {
        val stale = Pending(write = write("ancient").copy(occurredAt = "2020-01-01T00:00:00Z"))
        val store = Store(SyncSnapshot(Revision("g1", 0), outbox = listOf(stale)))
        val repository = SyncRepository(api { 200 to "[]" }, SyncEngine(store))
        val dropped = repository.expire(java.time.Instant.parse("2026-01-01T00:00:00Z"))
        assertEquals(listOf("ancient"), dropped.map { it.operation.opId })
        // A fresh engine over the same store must not resurrect the dropped operation.
        assertTrue(SyncEngine(store).snapshot().outbox.isEmpty())
    }

    @Test
    fun httpHappensOutsideEngineSections() = runBlocking {
        // Sections are short load-mutate-save critical sections; a user action
        // queued mid-sync must not wait behind HTTP.
        val store = Store(SyncSnapshot(Revision("g1", 0), outbox = listOf(Pending(write = write("queued")))))
        var inSection = false
        val sections = object : EngineSession {
            override suspend fun <T> invoke(section: suspend (SyncEngine) -> T): T {
                check(!inSection) { "engine sections must not nest" }
                inSection = true
                try {
                    return section(SyncEngine(store))
                } finally {
                    inSection = false
                }
            }
        }
        val repository = SyncRepository(api { request ->
            check(!inSection) { "HTTP ran inside an engine section: ${request.url.encodedPath}" }
            when (request.url.encodedPath) {
                "/v1/changes" -> 200 to """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
                "/v1/state:batch" -> 200 to json.encodeToString(
                    kotlinx.serialization.builtins.ListSerializer(Result.serializer()),
                    listOf(Result("queued", "applied", SyncEngine.initialState(1, 2), false)),
                )
                else -> error("unexpected request ${request.url.encodedPath}")
            }
        }, sections)
        repository.pull()
        repository.flush()
        assertTrue(store.data.outbox.isEmpty())
    }
}

package ch.lkmc.earnest

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.*
import ch.lkmc.earnest.db.ContentEntity
import kotlinx.coroutines.*
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.*
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.SQLiteMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@SQLiteMode(SQLiteMode.Mode.NATIVE)
class SessionCacheTest {
    private val context get() = ApplicationProvider.getApplicationContext<Context>()
    private val identity = AccountIdentity("https://nas.test", 1, 1)
    private val write = StateWrite("later", 1, 2, "shelf", JsonPrimitive("later"), Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit")

    @Test fun workerAndForegroundSerializeReloadThroughFlush() = runBlocking(Dispatchers.IO.limitedParallelism(1)) {
        val worker = SessionCache(context, identity)
        val foreground = SessionCache(context, identity)
        worker.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val loaded = CompletableDeferred<Unit>()
        val response = CompletableDeferred<Unit>()
        val pull = async {
            worker.withEngine { engine ->
                loaded.complete(Unit)
                response.await()
                engine.applyPage(emptyList(), Revision("g1", 0))
            }
        }
        loaded.await()
        val action = async { foreground.withEngine { it.queue(write) } }
        yield()
        response.complete(Unit)
        pull.await()
        action.await()
        assertEquals(listOf("later"), worker.withEngine { it.snapshot().outbox.map { p -> p.operation.opId } })
    }

    @Test fun consecutiveWritesReuseTheEngineUntilAnotherWriterCommits() = runBlocking(Dispatchers.IO.limitedParallelism(1)) {
        val cache = SessionCache(context, identity)
        cache.withEngine { it.beginBootstrap(Revision("g1", 0)); it.queue(write) }
        val first = cache.engine
        assertNotNull(first)
        cache.withEngine { it.queue(write.copy(opId = "done")) }
        assertSame("write path rebuilt the engine", first, cache.engine)
        // Another SessionCache (e.g. the sync worker) committing bumps the write
        // counter, so the next call must reload instead of reusing stale state.
        SessionCache(context, identity).withEngine { it.beginBootstrap(Revision("g2", 9)) }
        cache.withEngine { }
        assertNotSame("external write not detected", first, cache.engine)
        assertEquals(Revision("g2", 9), cache.withEngine { it.snapshot().cursor })
    }

    @Test fun accountsIsolateSnapshotOutboxContentAndPreferences() = runBlocking(Dispatchers.IO.limitedParallelism(1)) {
        val first = SessionCache(context, identity)
        first.withEngine { it.beginBootstrap(Revision("g1", 0)); it.mergeEntry(CachedEntry(2, title = "Private")); it.queue(write) }
        first.db.content().save(ContentEntity(2, "private HTML", "private text", 1, "stored", 0))
        first.preferences.edit().putString("principal_names", "private agent").commit()
        for (other in listOf(identity.copy(server = "https://other.test"), identity.copy(userId = 2, principalId = 2))) {
            val second = SessionCache(context, other)
            assertTrue("another account saw cached entries", second.withEngine { it.snapshot().entries.isEmpty() })
            assertTrue("another account saw pending operations", second.withEngine { it.snapshot().outbox.isEmpty() })
            assertNull(second.db.content().get(2))
            assertNull(second.preferences.getString("principal_names", null))
        }
        val restored = SessionCache(context, identity)
        assertEquals("private HTML", restored.db.content().get(2)?.html)
        assertEquals(listOf("later"), restored.withEngine { it.snapshot().outbox.map { p -> p.operation.opId } })
    }
    @Test fun equivalentOriginsReuseTheAccountNamespace() {
        assertEquals(identity.cacheKey, AccountIdentity("https://NAS.test:443/", 1, 1).cacheKey)
        assertNotEquals(identity.cacheKey, identity.copy(server = "https://nas.test:8443").cacheKey)
    }

    @Test fun legacyDatabaseIsRetainedAndNeverAttached() = runBlocking(Dispatchers.IO) {
        val legacy = context.getDatabasePath("earnest.db")
        legacy.parentFile!!.mkdirs()
        legacy.writeText("legacy outbox retained")
        val cache = SessionCache(context, identity)
        assertTrue(cache.legacyCacheRetained)
        assertTrue(cache.withEngine { it.snapshot().outbox.isEmpty() })
        assertEquals("legacy outbox retained", legacy.readText())
    }

    @Test fun backgroundAgentNoticesPageAndSurviveFetchFailureOffline() = runBlocking(Dispatchers.IO) {
        val cache = SessionCache(context, identity)
        val rows = (1L..1000L).map { id ->
            Activity(id, 9, "Agent", "state.updated", null, null, null, "g1", "2026-11-03T00:00:00Z", "revert")
        }
        val json = kotlinx.serialization.json.Json
        val cursors = mutableListOf<String?>()
        var status = 200
        val api = EarnestApi(identity.server, "token", okhttp3.OkHttpClient.Builder().addInterceptor { chain ->
            cursors += chain.request().url.queryParameter("after_id")
            val page = if (cursors.last() == "0") rows else emptyList()
            okhttp3.Response.Builder().request(chain.request()).protocol(okhttp3.Protocol.HTTP_1_1)
                .code(status).message("test")
                .body(json.encodeToString(kotlinx.serialization.builtins.ListSerializer(Activity.serializer()), page).toResponseBody()).build()
        }.build())
        cache.withEngine {
            cache.markAgentChangesStale()
            cache.refreshAgentChanges(api)
        }
        assertEquals(listOf("0", "1000"), cursors)
        assertEquals(1000, SessionCache(context, identity).agentChanges().size)
        assertFalse(cache.agentChangesStale)
        status = 503
        cache.markAgentChangesStale()
        try {
            cache.withEngine { cache.refreshAgentChanges(api) }
            fail("failed notice fetch succeeded")
        } catch (_: ApiException) {
            assertEquals(1000, cache.agentChanges().size)
            assertTrue(cache.agentChangesStale)
        }
    }

}

package ch.lkmc.earnest

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.summarizeKeptImport
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeout
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.SQLiteMode
import java.util.concurrent.CopyOnWriteArrayList

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@SQLiteMode(SQLiteMode.Mode.NATIVE)
class KeptImportAppTest {
    private val app get() = ApplicationProvider.getApplicationContext<Application>()
    private val identity = AccountIdentity("https://nas.test", 1, 1)
    private data class ImportCall(val query: String?, val body: ByteArray)
    private val importCalls = CopyOnWriteArrayList<ImportCall>()
    private val paths = CopyOnWriteArrayList<String>()
    private var importStatus = 200
    private var importBody = """{"results":null,"subscribed":null}"""

    private class Sessions(initial: AuthenticatedSession?) : SessionStore {
        private val stored = java.util.concurrent.atomic.AtomicReference(initial)
        override fun current() = stored.get()
        override fun clearToken() { stored.set(null) }
        override fun clearToken(expected: AuthenticatedSession): Boolean {
            while (true) {
                val current = stored.get()
                if (current != expected) return false
                if (stored.compareAndSet(current, null)) return true
            }
        }
    }

    @Before fun setup() { Dispatchers.setMain(UnconfinedTestDispatcher()) }
    @After fun teardown() { Dispatchers.resetMain() }

    private fun api() = EarnestApi(identity.server, "token", OkHttpClient.Builder().addInterceptor { chain ->
        val request = chain.request()
        val path = request.url.encodedPath
        paths += path
        if (path == "/v1/kept:import") {
            val buffer = Buffer()
            request.body?.writeTo(buffer)
            importCalls += ImportCall(request.url.query, buffer.readByteArray())
        }
        val body = when (path) {
            "/v1/settings" -> """{"today_decay_days":2,"rev":0}"""
            "/v1/mutes" -> "[]"
            "/v1/changes" -> """{"changes":[],"next":{"generation":"g1","rev":0},"next_cursor":"g1:0"}"""
            "/v1/activity/agent-changes", "/v1/tokens", "/v1/briefs/today" -> "[]"
            "/v1/kept:import" -> importBody
            else -> "[]"
        }
        val status = if (path == "/v1/kept:import") importStatus else 200
        Response.Builder().request(request).protocol(Protocol.HTTP_1_1).code(status).message("test")
            .body(body.toResponseBody()).build()
    }.build())

    private suspend fun model(): AppViewModel {
        SessionCache(app, identity).withEngine { it.beginBootstrap(Revision("g1", 0)) }
        val model = AppViewModel(app, Sessions(AuthenticatedSession(identity, "token"))) { api() }
        withTimeout(15000) { model.state.first { !it.loading && paths.contains("/v1/tokens") } }
        return model
    }

    @Test fun importKeptStoresTheReportAndRepeatsWithSubscribe() = runBlocking(Dispatchers.IO) {
        val model = model()
        importBody = """{"results":[{"index":0,"status":"created","entry_id":12,"subscription_id":3,"feed_url":"https://a.test/feed","title":"An article"},{"index":1,"status":"not_subscribed","feed_url":"https://b.test/rss"}],"subscribed":[]}"""
        val document = """{"items":[]}""".toByteArray()

        model.importKept(document, subscribeMissing = false)
        withTimeout(15000) { model.state.first { it.keptImport.report != null && !it.keptImport.busy } }
        assertEquals("missing_feeds=skip", importCalls.single().query)
        assertArrayEquals(document, importCalls.single().body)
        val report = model.state.value.keptImport.report
        assertNotNull(report)
        assertEquals("created", report!!.results!!.first().status)
        assertEquals(1, summarizeKeptImport(report).added)

        model.importKeptAgain()
        withTimeout(15000) { model.state.first { it.keptImport.report != null && !it.keptImport.busy && importCalls.size == 2 } }
        assertEquals("missing_feeds=subscribe", importCalls[1].query)
        assertArrayEquals(document, importCalls[1].body)
    }

    @Test fun importKeptSurfacesAProblemErrorWithoutAReport() = runBlocking(Dispatchers.IO) {
        val model = model()
        importStatus = 400
        importBody = """{"type":"/problems/invalid-input","title":"Bad Request","status":400,"detail":"the export is not valid JSON: invalid input","error":"invalid-input"}"""

        model.importKept("""{"items":[]}""".toByteArray(), subscribeMissing = false)
        withTimeout(15000) { model.state.first { it.keptImport.error != null && !it.keptImport.busy } }
        val error = model.state.value.keptImport.error
        assertNotNull(error)
        assertTrue("the error names the failed action: $error", error!!.startsWith("Could not import that file"))
        assertNull(model.state.value.keptImport.report)
        assertFalse(model.state.value.keptImport.busy)
    }

    @Test fun readFailureSurfacesWithoutAnImportCall() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.keptImportReadFailed("Could not read that file.")
        withTimeout(15000) { model.state.first { it.keptImport.error != null } }
        assertEquals("Could not read that file.", model.state.value.keptImport.error)
        assertTrue(importCalls.isEmpty())
    }

    @Test fun importKeptReleasesTheFileWhenThereIsNothingToRetry() = runBlocking(Dispatchers.IO) {
        val model = model()
        importBody = """{"results":[{"index":0,"status":"created","entry_id":12}],"subscribed":[]}"""
        model.importKept("""{"items":[]}""".toByteArray(), subscribeMissing = false)
        withTimeout(15000) { model.state.first { it.keptImport.report != null && !it.keptImport.busy } }
        // Nothing was skipped, so the 64 MiB-capable buffer is gone and a
        // retry has nothing to send.
        model.importKeptAgain()
        // importKept marks itself busy before launching, so a retry that ran
        // would show here at once.
        assertFalse(model.state.value.keptImport.busy)
        assertEquals(1, importCalls.size)
    }

    @Test fun importKeptRefusesAnOversizedExportWithoutARequest() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.importKept(ByteArray(ch.lkmc.earnest.core.MAX_KEPT_IMPORT_BYTES + 1), subscribeMissing = false)
        withTimeout(15000) { model.state.first { it.keptImport.error != null } }
        assertEquals("The export is larger than 64 MiB.", model.state.value.keptImport.error)
        assertTrue(importCalls.isEmpty())
    }

    @Test fun aReadFailureClearsAnEarlierReport() = runBlocking(Dispatchers.IO) {
        val model = model()
        importBody = """{"results":[{"index":0,"status":"created","entry_id":12}],"subscribed":[]}"""
        model.importKept("""{"items":[]}""".toByteArray(), subscribeMissing = false)
        withTimeout(15000) { model.state.first { it.keptImport.report != null && !it.keptImport.busy } }
        model.keptImportReadFailed("Could not read that file.")
        withTimeout(15000) { model.state.first { it.keptImport.error != null } }
        assertNull(model.state.value.keptImport.report)
    }
}

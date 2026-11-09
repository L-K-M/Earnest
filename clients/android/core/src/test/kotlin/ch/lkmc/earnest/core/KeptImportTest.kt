package ch.lkmc.earnest.core

import kotlinx.coroutines.runBlocking
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CopyOnWriteArrayList

class KeptImportTest {
    private fun client(respond: (Request) -> Response): Pair<EarnestApi, List<Request>> {
        val requests = CopyOnWriteArrayList<Request>()
        val http = OkHttpClient.Builder().addInterceptor { chain ->
            requests += chain.request()
            respond(chain.request())
        }.build()
        return EarnestApi("https://earnest.test", "secret", http) to requests
    }

    private fun response(request: Request, status: Int = 200, body: String): Response =
        Response.Builder()
            .request(request)
            .protocol(Protocol.HTTP_1_1)
            .code(status)
            .message("test")
            .body(body.toResponseBody("application/json".toMediaType()))
            .build()

    private fun Request.bodyBytes(): ByteArray {
        val buffer = Buffer()
        body?.writeTo(buffer)
        return buffer.readByteArray()
    }

    @Test
    fun importKeptPostsRawBytesWithTheRightPathQueryAndContentType() = runBlocking {
        val document = """{"items":[{"id":"1","title":"An article"}]}""".toByteArray()
        for ((subscribeMissing, expected) in listOf(false to "skip", true to "subscribe")) {
            val (api, requests) = client { response(it, body = """{"results":[],"subscribed":[]}""") }
            val report = api.importKept(document, subscribeMissing)
            assertEquals(emptyList<KeptImportResult>(), report.results)

            val request = requests.single()
            assertEquals("POST", request.method)
            assertEquals("/v1/kept:import", request.url.encodedPath)
            assertEquals(expected, request.url.queryParameter("missing_feeds"))
            assertEquals("application/json", request.body?.contentType()?.toString())
            assertEquals("Bearer secret", request.header("Authorization"))
            assertEquals("application/json", request.header("Accept"))
            assertArrayEquals(document, request.bodyBytes())
        }
    }

    @Test
    fun importKeptDecodesNullListsAndLabels() = runBlocking {
        val (emptyApi, _) = client { response(it, body = """{"results":null,"subscribed":null}""") }
        val empty = emptyApi.importKept("""{}""".toByteArray(), subscribeMissing = false)
        assertNull(empty.results)
        assertNull(empty.subscribed)

        val (api, _) = client {
            response(
                it,
                body = """
                {"results":[
                  {"index":0,"status":"created","entry_id":12,"subscription_id":3,"feed_url":"https://a.test/feed","title":"An article","labels":["starred","read later"],"unknown_field":true},
                  {"index":1,"status":"failed","title":"Broken","error":"no ID"}
                ],"subscribed":[]}
                """.trimIndent(),
            )
        }
        val report = api.importKept("""{}""".toByteArray(), subscribeMissing = true)
        assertEquals(2, report.results!!.size)
        val created = report.results!![0]
        assertEquals(12L, created.entryId)
        assertEquals(3L, created.subscriptionId)
        assertEquals("https://a.test/feed", created.feedUrl)
        assertEquals(listOf("starred", "read later"), created.labels)
        assertEquals("failed", report.results!![1].status)
        assertEquals("no ID", report.results!![1].error)
        assertEquals(emptyList<Subscription>(), report.subscribed)
    }

    @Test
    fun importKeptRejectsExportsOver64MiBBeforeSending() = runBlocking {
        val (api, requests) = client { response(it, body = """{"results":[],"subscribed":[]}""") }
        val tooLarge = ByteArray(64 * 1024 * 1024 + 1)
        val error = runCatching { api.importKept(tooLarge, subscribeMissing = false) }.exceptionOrNull()
        assertTrue("expected a size failure, got $error", error is IllegalArgumentException)
        assertNotNull(error!!.message)
        assertTrue("the message must name the limit: ${error.message}", error.message!!.contains("64 MiB"))
        assertTrue("the oversized export must not be sent", requests.isEmpty())
    }

    @Test
    fun summarizeKeptImportCountsStatusesOrdersSkippedFeedsAndBuildsFailureTexts() {
        val report = KeptImportReport(
            results = listOf(
                KeptImportResult(index = 0, status = "created", title = "Added", labels = listOf("Tech")),
                KeptImportResult(index = 1, status = "matched", title = "Existing", labels = listOf("Tech", "Later")),
                KeptImportResult(index = 2, status = "unchanged", title = "Also existing"),
                KeptImportResult(index = 3, status = "unkept", title = "Removed"),
                KeptImportResult(index = 4, status = "gone", title = "Retained"),
                KeptImportResult(index = 5, status = "not_subscribed", feedUrl = "https://b.test/rss"),
                KeptImportResult(index = 6, status = "not_subscribed", feedUrl = "https://a.test/feed"),
                KeptImportResult(index = 7, status = "not_subscribed", feedUrl = "https://b.test/rss"),
                KeptImportResult(index = 8, status = "not_subscribed"),
                KeptImportResult(index = 9, status = "failed", title = "Broken", error = "no ID, link or title"),
                KeptImportResult(index = 10, status = "failed", error = "bad payload"),
                KeptImportResult(index = 11, status = "something_new", title = "Ignored"),
            ),
            subscribed = listOf(subscription()),
        )
        val summary = summarizeKeptImport(report)
        assertEquals(1, summary.added)
        assertEquals(2, summary.existing)
        assertEquals(1, summary.unkept)
        assertEquals(1, summary.gone)
        assertEquals(1, summary.subscribed)
        assertEquals(2, summary.labelled)
        assertEquals(
            listOf(
                KeptImportSkip("https://b.test/rss", 2),
                KeptImportSkip("", 1),
                KeptImportSkip("https://a.test/feed", 1),
            ),
            summary.skipped,
        )
        assertEquals(listOf("Broken: no ID, link or title", "Article 11: bad payload"), summary.failed)
    }

    @Test
    fun summarizeKeptImportTreatsNullListsAsEmpty() {
        val summary = summarizeKeptImport(KeptImportReport())
        assertEquals(0, summary.added)
        assertEquals(0, summary.existing)
        assertEquals(0, summary.unkept)
        assertEquals(0, summary.gone)
        assertEquals(0, summary.subscribed)
        assertEquals(0, summary.labelled)
        assertTrue(summary.skipped.isEmpty())
        assertTrue(summary.failed.isEmpty())
    }

    private fun subscription() = Subscription(
        initialSince = null,
        initialThroughSeq = 0,
        initialFetchPending = false,
        id = 1,
        feedId = 2,
        title = "Feed",
        url = "https://feed.test/rss",
        siteUrl = "https://feed.test",
        folderId = 0,
        cadence = "today",
        extract = "always",
        rev = 1,
        deleted = false,
        private = false,
        health = FeedHealth(
            lastSuccessAt = null,
            lastAttemptAt = null,
            nextFetchAt = "2026-11-03T00:00:00Z",
            errorClass = null,
            errorMessage = null,
            consecutiveFailures = 0,
            httpStatus = 200,
            postsPerDay = 1.0,
            firehose = false,
        ),
    )

    @Test
    fun readAtMostStopsOneBytePastTheLimit() {
        val small = ByteArray(10) { it.toByte() }
        assertEquals(small.toList(), readAtMost(small.inputStream(), 10).toList())
        val large = ByteArray(200_000)
        assertEquals(101, readAtMost(large.inputStream(), 100).size)
        assertEquals(0, readAtMost(ByteArray(0).inputStream(), 100).size)
    }
}

package ch.lkmc.earnest.core

import kotlinx.coroutines.*
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Call
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

class EarnestApiTest {
    @Test
    fun transportPreservesHeadersAndResponseSemantics() = runBlocking {
        for (token in listOf(null, "secret")) {
            for ((status, body) in listOf(200 to "", 200 to "{\"value\":1}", 401 to "{\"error\":\"unauthorized\"}", 200 to "not json")) {
                val request = AtomicReference<okhttp3.Request>()
                val api = EarnestApi("https://earnest.test", token, OkHttpClient.Builder().addInterceptor { chain ->
                    request.set(chain.request())
                    Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
                        .code(status).message("test").body(body.toResponseBody()).build()
                }.build())
                val result = runCatching { api.get("https://earnest.test/probe".toHttpUrl()) }
                when {
                    status == 401 -> {
                        val error = result.exceptionOrNull() as ApiException
                        assertEquals(401, error.status)
                        assertEquals("unauthorized", error.code)
                    }
                    body == "not json" -> assertTrue(result.exceptionOrNull() is kotlinx.serialization.SerializationException)
                    body.isEmpty() -> assertEquals(kotlinx.serialization.json.JsonNull, result.getOrThrow())
                    else -> assertEquals(kotlinx.serialization.json.Json.parseToJsonElement(body), result.getOrThrow())
                }
                assertEquals("application/json", request.get().header("Accept"))
                assertEquals(token?.let { "Bearer $it" }, request.get().header("Authorization"))
            }
        }
    }

    @Test
    fun cancellingRequestCancelsTheHttpCallPromptly() = runBlocking {
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val call = AtomicReference<Call>()
        val api = EarnestApi("https://earnest.test", client = OkHttpClient.Builder().addInterceptor { chain ->
            call.set(chain.call())
            entered.countDown()
            check(release.await(5, TimeUnit.SECONDS))
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
                .code(200).message("test").body("{}".toResponseBody()).build()
        }.build())
        val job = launch(Dispatchers.Default) { api.me() }
        try {
            assertTrue("HTTP call never started", entered.await(5, TimeUnit.SECONDS))
            job.cancel()
            withContext(Dispatchers.Default) {
                withTimeoutOrNull(1000) {
                    while (!call.get().isCanceled()) delay(10)
                }
            }
            assertTrue("cancelled coroutine left the HTTP call running", call.get().isCanceled())
        } finally {
            release.countDown()
            job.cancelAndJoin()
        }
    }

    @Test
    fun mediaRejectsAnythingButStoredImagePathsBeforeAnyRequest() = runBlocking {
        for (path in listOf(
            "",
            "/v1/entries/1",
            "/v1/entries/0/thumbnail",
            "/v1/entries/01/thumbnail",
            "/v1/entries/1/thumbnail/extra",
            "/v1/entries//thumbnail",
            "/v1/comments/1/thumbnail",
            "/v1/entries/1/thumbnail?v=",
            "/v1/entries/1/thumbnail?x=1",
            "/v1/entries/1/thumbnail?v=a,b",
            "/v1/entries/1/thumbnail?v=" + "a".repeat(65),
            "/v1/entries/1/thumbnail/../icon",
            "https://evil.test/v1/entries/1/thumbnail",
            "//v1/entries/1/thumbnail",
        )) {
            val api = EarnestApi("https://earnest.test", "secret", OkHttpClient.Builder().addInterceptor { chain ->
                error("request escaped path validation: ${chain.request().url}")
            }.build())
            try {
                api.media(path)
                fail("media accepted $path")
            } catch (_: IllegalArgumentException) {
            }
        }
        var saw = false
        val api = EarnestApi("https://earnest.test", "secret", OkHttpClient.Builder().addInterceptor { chain ->
            saw = true
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
                .code(404).message("test").body("{}".toResponseBody()).build()
        }.build())
        assertNull(api.media("/v1/entries/1/thumbnail?v=" + "a".repeat(64)))
        assertTrue("a boundary-length version must reach the network", saw)
    }

    @Test
    fun mediaCarriesBearerAndImageAcceptAndHonoursResponseSemantics() = runBlocking {
        for (token in listOf(null, "secret")) {
            val requests = java.util.concurrent.CopyOnWriteArrayList<okhttp3.Request>()
            val api = EarnestApi("https://earnest.test", token, OkHttpClient.Builder().addInterceptor { chain ->
                requests += chain.request()
                Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
                    .code(200).message("test").body(byteArrayOf(1, 2, 3).toResponseBody("image/png".toMediaType())).build()
            }.build())
            for (path in listOf("/v1/entries/12/thumbnail", "/v1/entries/12/thumbnail?v=0a1b_2c", "/v1/subscriptions/5/icon?v=ff-ee")) {
                val bytes = api.media(path)
                assertEquals(byteArrayOf(1, 2, 3).toList(), bytes!!.toList())
            }
            assertEquals(3, requests.size)
            assertEquals("/v1/entries/12/thumbnail", requests[0].url.encodedPath)
            assertNull(requests[0].url.query)
            assertEquals("0a1b_2c", requests[1].url.queryParameter("v"))
            assertEquals("/v1/subscriptions/5/icon", requests[2].url.encodedPath)
            requests.forEach {
                assertEquals("image/png, image/jpeg", it.header("Accept"))
                assertEquals(token?.let { value -> "Bearer $value" }, it.header("Authorization"))
            }
        }
    }

    @Test
    fun mediaResponseRules() = runBlocking {
        val twoMiB = 2 * 1024 * 1024
        val cases = listOf<Triple<Int, String?, ByteArray>>(
            Triple(404, null, byteArrayOf()),
            Triple(200, "image/jpeg", byteArrayOf(9)),
            Triple(200, "image/png; charset=binary", byteArrayOf(9)),
            Triple(200, "text/plain", byteArrayOf(9)),
            Triple(200, "image/svg+xml", byteArrayOf(9)),
            Triple(200, "image/png", byteArrayOf()),
            Triple(200, "image/png", ByteArray(twoMiB)),
            Triple(200, "image/png", ByteArray(twoMiB + 1)),
            Triple(302, null, byteArrayOf()),
            Triple(500, "application/json", """{"error":"boom"}""".encodeToByteArray()),
        )
        for ((status, contentType, body) in cases) {
            val api = EarnestApi("https://earnest.test", "secret", OkHttpClient.Builder().addInterceptor { chain ->
                Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
                    .code(status).message("test").body(body.toResponseBody(contentType?.toMediaType())).build()
            }.build())
            val result = runCatching { api.media("/v1/entries/1/thumbnail?v=abc") }
            when {
                status == 404 -> assertNull(result.getOrThrow())
                status == 200 && contentType != null &&
                    (contentType.startsWith("image/png") || contentType.startsWith("image/jpeg")) &&
                    body.isNotEmpty() && body.size <= twoMiB ->
                    assertEquals(body.size, result.getOrThrow()!!.size)
                status == 500 -> assertEquals("boom", (result.exceptionOrNull() as ApiException).code)
                else -> assertTrue("$status $contentType ${body.size}", result.exceptionOrNull() is ApiException)
            }
        }
    }

    @Test
    fun positionViewsAreSingleEncodedPathSegments() = runBlocking {
        for (view in listOf("folder/2", "label?x=y", "my #view", "100% read")) {
            val requests = java.util.concurrent.CopyOnWriteArrayList<okhttp3.Request>()
            val api = EarnestApi("https://earnest.test", client = OkHttpClient.Builder().addInterceptor { chain ->
                requests += chain.request()
                Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
                    .code(200).message("test").body("""{"principal":1,"view":"test","seq":0,"rev":0}""".toResponseBody()).build()
            }.build())
            api.position(view)
            api.putPosition(view, PutPosition(0, "op"))
            assertEquals(2, requests.size)
            requests.forEach {
                assertEquals(listOf("v1", "positions", view), it.url.pathSegments)
                assertNull(it.url.query)
            }
        }
    }
}

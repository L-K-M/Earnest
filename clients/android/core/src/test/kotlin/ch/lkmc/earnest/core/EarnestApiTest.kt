package ch.lkmc.earnest.core

import kotlinx.coroutines.*
import okhttp3.HttpUrl.Companion.toHttpUrl
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

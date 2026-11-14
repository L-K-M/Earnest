package ch.lkmc.earnest

import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import ch.lkmc.earnest.core.EarnestApi
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.IOException
import java.util.concurrent.CopyOnWriteArrayList

/**
 * A denied or expired device code, or a transport failure, must leave the
 * server field and Start usable again with the failure still shown. Pending
 * and slow-down responses keep polling instead.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class LoginScreenTest {
    @get:Rule
    val compose = createComposeRule()

    private class FakeApi {
        val starts = CopyOnWriteArrayList<String>()
        val polls = CopyOnWriteArrayList<String>()
        val apiCalls = CopyOnWriteArrayList<Pair<String, String?>>()
        var startCount = 0
        var startGate: CountDownLatch? = null
        var transportFailure: IOException? = null
        var poll: (Int) -> Pair<Int, String> = { 200 to """{"access_token":"tok","token_type":"Bearer"}""" }

        val factory: (String, String?) -> EarnestApi = { server, token ->
            apiCalls += server to token
            val client = OkHttpClient.Builder().addInterceptor { chain ->
                val path = chain.request().url.encodedPath
                when (path) {
                    "/v1/devices/start" -> {
                        starts += path
                        startCount += 1
                        startGate?.await(5, TimeUnit.SECONDS)
                        val code = "CODE-$startCount"
                        respond(chain, 200, """{"device_code":"dev-$code","user_code":"$code","verification_uri":"https://nas.test/device","interval":1,"expires_in":600}""")
                    }
                    "/v1/devices/poll" -> {
                        polls += path
                        transportFailure?.let { throw it }
                        val (status, body) = poll(polls.size - 1)
                        respond(chain, status, body)
                    }
                    else -> respond(chain, 500, "{}")
                }
            }.build()
            EarnestApi(server, token, client)
        }
    }

    private fun start(fake: FakeApi) {
        compose.onNode(hasSetTextAction()).performTextInput(SERVER)
        compose.onNodeWithText("Start device sign in").performClick()
        compose.waitUntil(TIMEOUT) { fake.starts.size == 1 }
    }

    private fun advance(seconds: Long) {
        // The polling delay runs on the composition's test scheduler, not the
        // Robolectric looper, so the Compose clock is what releases it.
        compose.mainClock.advanceTimeBy(seconds * 1000)
        compose.waitForIdle()
    }

    private fun textExists(text: String): Boolean =
        compose.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty()

    private fun waitForText(text: String) {
        compose.waitUntil(TIMEOUT) { textExists(text) }
    }

    /** Advances the Compose clock one second at a time until [count] polls were sent. */
    private fun awaitPolls(fake: FakeApi, count: Int, maxSteps: Int = 20): Int {
        var steps = 0
        while (fake.polls.size < count && steps < maxSteps) {
            advance(1)
            compose.waitForIdle()
            // OkHttp completion is not driven by the Compose clock.
            Thread.sleep(20)
            steps += 1
        }
        check(fake.polls.size >= count) { "polling stopped at ${fake.polls.size} of $count" }
        return steps
    }

    @Test fun aPendingStartDisablesDuplicateRequestsAndServerEdits() {
        val gate = CountDownLatch(1)
        val fake = FakeApi().apply { startGate = gate }
        compose.setContent { LoginScreen(onLoggedIn = {}, apiFactory = fake.factory) }
        try {
            start(fake)
            compose.onNodeWithText("Start device sign in").assertIsNotEnabled()
            compose.onNodeWithText(SERVER).assertIsNotEnabled()
        } finally {
            gate.countDown()
        }
    }

    @Test fun expiredTokenKeepsTheErrorAndRestoresTheForm() {
        val fake = FakeApi()
        fake.poll = { 400 to """{"error":"expired_token"}""" }
        compose.setContent { LoginScreen(onLoggedIn = {}, apiFactory = fake.factory) }
        start(fake)

        advance(1)
        waitForText("Earnest request failed (400): expired_token")

        compose.onNodeWithText(SERVER).assertIsEnabled()
        compose.onNodeWithText("Start device sign in").assertIsEnabled()
    }

    @Test fun accessDeniedRestoresTheFormForANewCode() {
        val fake = FakeApi()
        fake.poll = { 400 to """{"error":"access_denied"}""" }
        compose.setContent { LoginScreen(onLoggedIn = {}, apiFactory = fake.factory) }
        start(fake)

        advance(1)
        waitForText("Earnest request failed (400): access_denied")
        compose.onNodeWithText("Start device sign in").assertIsEnabled()

        compose.onNodeWithText("Start device sign in").performClick()
        compose.waitUntil(TIMEOUT) { fake.starts.size == 2 }
        waitForText("CODE-2")
        compose.onNodeWithText("Start device sign in").assertIsNotEnabled()
    }

    @Test fun transientTransportFailureRestoresTheForm() {
        val fake = FakeApi()
        fake.transportFailure = IOException("offline")
        compose.setContent { LoginScreen(onLoggedIn = {}, apiFactory = fake.factory) }
        start(fake)

        advance(1)
        waitForText("offline")

        compose.onNodeWithText(SERVER).assertIsEnabled()
        compose.onNodeWithText("Start device sign in").assertIsEnabled()
    }

    @Test fun messageLessTransportFailureStillExplainsTheReset() {
        val fake = FakeApi()
        fake.transportFailure = IOException()
        compose.setContent { LoginScreen(onLoggedIn = {}, apiFactory = fake.factory) }
        start(fake)

        advance(1)
        waitForText("Sign-in failed. Try again.")
        compose.onNodeWithText(SERVER).assertIsEnabled()
        compose.onNodeWithText("Start device sign in").assertIsEnabled()
    }

    @Test fun pendingAndSlowDownKeepPollingUntilATerminalFailure() {
        val fake = FakeApi()
        fake.poll = { attempt ->
            when (attempt) {
                0 -> 400 to """{"error":"slow_down"}"""
                1 -> 400 to """{"error":"authorization_pending"}"""
                else -> 400 to """{"error":"expired_token"}"""
            }
        }
        compose.setContent { LoginScreen(onLoggedIn = {}, apiFactory = fake.factory) }
        start(fake)

        advance(1)
        awaitPolls(fake, 1)
        compose.onNodeWithText("CODE-1").assertExists()
        compose.onNodeWithText("Start device sign in").assertIsNotEnabled()

        // slow_down raised the interval from 1s to 6s, so the next poll is slow.
        val steps = awaitPolls(fake, 2)
        check(steps >= 5) { "slow_down did not raise the interval (next poll after ${steps}s)" }
        compose.onNodeWithText("CODE-1").assertExists()
        compose.onNodeWithText("Start device sign in").assertIsNotEnabled()

        awaitPolls(fake, 3)
        waitForText("Earnest request failed (400): expired_token")
        check(fake.apiCalls.count { it.second == null } == 2) {
            "Start and all polls should use one anonymous client each: ${fake.apiCalls}"
        }
        compose.onNodeWithText(SERVER).assertIsEnabled()
        compose.onNodeWithText("Start device sign in").assertIsEnabled()
    }

    private companion object {
        const val SERVER = "https://nas.test"
        const val TIMEOUT = 5_000L
    }
}

private fun respond(chain: okhttp3.Interceptor.Chain, status: Int, body: String): Response =
    Response.Builder()
        .request(chain.request())
        .protocol(Protocol.HTTP_1_1)
        .code(status)
        .message("test")
        .body(body.toResponseBody())
        .build()

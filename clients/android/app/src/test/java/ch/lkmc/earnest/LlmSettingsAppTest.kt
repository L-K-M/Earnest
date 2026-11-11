package ch.lkmc.earnest

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmTaskRoute
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.River
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull
import kotlin.coroutines.CoroutineContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.After
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
class LlmSettingsAppTest {
    private val app get() = ApplicationProvider.getApplicationContext<Application>()
    private val identity = AccountIdentity("https://nas.test", 1, 1)
    private data class Call(val method: String, val path: String, val body: String, val authorization: String? = null)
    private val calls = CopyOnWriteArrayList<Call>()
    private val putSeen = CompletableDeferred<Call>()

    private class Sessions(initial: AuthenticatedSession?) : SessionStore {
        private val stored = java.util.concurrent.atomic.AtomicReference(initial)
        var value: AuthenticatedSession?
            get() = stored.get()
            set(value) { stored.set(value) }
        var beforeClear: (() -> Unit)? = null
        val clearAttempted = CompletableDeferred<Unit>()
        override fun current() = stored.get()
        override fun clearToken() {
            beforeClear?.invoke()
            stored.set(null)
            clearAttempted.complete(Unit)
        }
        override fun clearToken(expected: AuthenticatedSession): Boolean {
            beforeClear?.invoke()
            try {
                while (true) {
                    val current = stored.get()
                    if (current != expected) return false
                    if (stored.compareAndSet(current, null)) return true
                }
            } finally { clearAttempted.complete(Unit) }
        }
    }

    private var getBody = llmJson(revision = 3)
    private var putStatus = 200
    private var putRevision = 4L

    @Before fun setup() { Dispatchers.setMain(UnconfinedTestDispatcher()) }
    @After fun teardown() { Dispatchers.resetMain() }

    private fun llmJson(
        revision: Long = 3,
        providers: String = """[{"name":"openai","kind":"openai","base_url":"https://api.openai.com/v1","has_api_key":true,"monthly_limit_usd":"20.000000","timeout_seconds":60,"token_field":"max_completion_tokens"}]""",
        tasks: String = """[{"task":"embed","provider":"openai","model":"text-embedding-3-small"},{"task":"classify","provider":"","model":""},{"task":"blurb","provider":"","model":""},{"task":"summary","provider":"","model":""},{"task":"edition","provider":"","model":""},{"task":"compile_rule","provider":"","model":""},{"task":"ask","provider":"","model":""}]""",
        models: String = """[{"name":"text-embedding-3-small","input_per_1m":"0.000000","output_per_1m":"0.000000","embedding_per_1m":"0.020000","dimensions":1536,"truncatable":true}]""",
    ): String = """{"revision":$revision,"providers":$providers,"tasks":$tasks,"models":$models}"""

    private fun defaultRespond(request: Request): Pair<Int, String> {
        val path = request.url.encodedPath
        return when (path) {
            "/v1/settings" -> 200 to """{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,""" +
                """"theme":"system","measure":66,"font":"serif","feed_overrides":{},"rev":1}"""
            "/v1/settings/llm" -> if (request.method == "PUT") {
                val raw = request.body?.let { body ->
                    okio.Buffer().also { body.writeTo(it) }.readUtf8()
                }.orEmpty()
                val echoed = JsonObject(Json.parseToJsonElement(raw).jsonObject.toMutableMap().apply {
                    put("revision", JsonPrimitive(putRevision))
                })
                putStatus to echoed.toString()
            } else 200 to getBody
            "/v1/changes" -> {
                val rev = request.url.queryParameter("rev")?.toLongOrNull() ?: 0
                200 to """{"changes":[],"next":{"generation":"g1","rev":$rev},"next_cursor":"g1:$rev"}"""
            }
            "/v1/entries:counts" -> 200 to """{"by_subscription":{"3":4},"later":1,"kept":2,"total":7}"""
            "/v1/mutes" -> 200 to """[{"id":7,"kind":"author","value":"spam","rev":3}]"""
            "/v1/tokens", "/v1/briefs/today", "/v1/activity/agent-changes" -> 200 to "[]"
            else -> 200 to "[]"
        }
    }

    private var respond: (Request) -> Pair<Int, String> = ::defaultRespond

    private fun api(token: String = "token") = EarnestApi(identity.server, token, OkHttpClient.Builder().addInterceptor { chain ->
        val request = chain.request()
        val call = Call(request.method, request.url.encodedPath, request.body?.let { body ->
            okio.Buffer().also { body.writeTo(it) }.readUtf8()
        }.orEmpty(), request.header("Authorization"))
        calls += call
        if (call.method == "PUT" && call.path == "/v1/settings/llm") putSeen.complete(call)
        val (code, body) = respond(request)
        Response.Builder().request(request).protocol(Protocol.HTTP_1_1).code(code).message("test")
            .body(body.toResponseBody()).build()
    }.build())

    private suspend fun model(
        sessions: Sessions = Sessions(AuthenticatedSession(identity, "token")),
        signals: MutableSharedFlow<String> = MutableSharedFlow(extraBufferCapacity = 1),
        dispatcher: CoroutineDispatcher = Dispatchers.IO,
        setup: suspend (SessionCache) -> Unit = {},
    ): AppViewModel {
        val cache = SessionCache(app, identity)
        cache.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        setup(cache)
        val model = AppViewModel(app, sessions, signals, readerDispatcher = dispatcher) { api(it.token) }
        withTimeout(15000) { model.state.first { !it.loading && calls.any { it.path == "/v1/tokens" } } }
        return model
    }

    private suspend fun AppViewModel.loadedLlm(revision: Long = 3) {
        loadLlmSettings()
        withTimeout(15_000) { state.first { it.llmSettingsUi.loadedRevision == revision && !it.llmSettingsUi.loading } }
    }

    private fun llmPuts(): List<Call> = calls.filter { it.method == "PUT" && it.path == "/v1/settings/llm" }

    private class PausedRequestDispatcher : CoroutineDispatcher() {
        @Volatile var pauseNext = false
        val held = CompletableDeferred<Runnable>()
        override fun dispatch(context: CoroutineContext, block: Runnable) {
            if (pauseNext) {
                pauseNext = false
                check(held.complete(block))
            } else Dispatchers.IO.dispatch(context, block)
        }
    }

    @Test fun reloadCannotDiscardAnInflightSaveOrItsFrozenKey() = runBlocking(Dispatchers.IO) {
        val dispatcher = PausedRequestDispatcher()
        val model = model(dispatcher = dispatcher)
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(apiKeyInput = "sk-in-flight") })
        }
        dispatcher.pauseNext = true
        model.saveLlmSettings()
        val pending = withTimeout(15_000) { dispatcher.held.await() }
        val gets = calls.count { it.method == "GET" && it.path == "/v1/settings/llm" }
        try {
            model.reloadLlmSettings()
            model.loadLlmSettings()
            val ui = model.state.value.llmSettingsUi
            assertFalse("loading cannot race a save", ui.loading)
            assertTrue(ui.saving)
            assertEquals("sk-in-flight", ui.draft?.providers?.single()?.apiKeyInput)
            assertNotNull("Reload must not erase the frozen request", ui.frozen)
            assertEquals(gets, calls.count { it.method == "GET" && it.path == "/v1/settings/llm" })
        } finally {
            pending.run()
            withTimeout(15_000) { model.state.first { !it.llmSettingsUi.saving && !it.llmSettingsUi.loading } }
        }
    }

    @Test fun savePublishesRevisionAndOpIdAndDropsTheDraftAndKeyMaterial() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(
                providers = draft.providers.map { it.copy(apiKeyInput = "sk-typed") },
                tasks = draft.tasks.map {
                    if (it.task == "summary") LlmTaskRoute("summary", "openai", "text-embedding-3-small") else it
                },
            )
        }
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.success } }

        val put = llmPuts().single()
        val body = Json.parseToJsonElement(put.body).jsonObject
        assertEquals("3", body.getValue("revision").jsonPrimitive.content)
        assertTrue(body.getValue("op_id").jsonPrimitive.content.isNotEmpty())
        val sent = body.getValue("providers").jsonArray.single().jsonObject
        assertEquals("sk-typed", sent.getValue("api_key").jsonPrimitive.content)
        assertFalse("a PUT provider never carries has_api_key", sent.containsKey("has_api_key"))
        val summary = body.getValue("tasks").jsonArray.map { it.jsonObject }.first { it.getValue("task").jsonPrimitive.content == "summary" }
        assertEquals("openai", summary.getValue("provider").jsonPrimitive.content)
        assertEquals("text-embedding-3-small", summary.getValue("model").jsonPrimitive.content)

        assertNull("success drops the draft and its typed key", model.state.value.llmSettingsUi.draft)
        assertNull(model.state.value.llmSettingsUi.frozen)
        assertEquals(4L, model.state.value.llmSettingsUi.loadedRevision)
        assertFalse(model.state.value.llmSettingsUi.conflict)
    }

    @Test fun blankKeyKeepsTheSavedKeyAndClearRemovesIt() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(clearApiKey = true, apiKeyInput = "") })
        }
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.success } }
        val sent = Json.parseToJsonElement(llmPuts().single().body).jsonObject.getValue("providers").jsonArray.single().jsonObject
        assertFalse("an omitted key keeps the stored one", sent.containsKey("api_key"))
        assertTrue(sent.getValue("clear_api_key").jsonPrimitive.content.toBoolean())
    }

    @Test fun conflictPreservesTheDraftAndItsKeyAndRequiresReload() = runBlocking(Dispatchers.IO) {
        putStatus = 409
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(apiKeyInput = "sk-typed") })
        }
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.conflict } }
        assertFalse(model.state.value.llmSettingsUi.saving)
        assertEquals("sk-typed", model.state.value.llmSettingsUi.draft?.providers?.single()?.apiKeyInput)
        assertNull(model.state.value.llmSettingsUi.frozen)

        val attempts = llmPuts().size
        model.saveLlmSettings()
        delay(300)
        assertEquals("a conflict blocks saving until an explicit reload", attempts, llmPuts().size)

        putStatus = 200
        getBody = llmJson(revision = 9)
        model.reloadLlmSettings()
        withTimeout(15_000) { model.state.first { !it.llmSettingsUi.loading && it.llmSettingsUi.loadedRevision == 9L } }
        assertFalse(model.state.value.llmSettingsUi.conflict)
        assertNull("reload discards the draft and its typed key", model.state.value.llmSettingsUi.draft)
        assertNull(model.state.value.llmSettingsUi.frozen)
    }

    @Test fun reloadClearsTypedKeyMaterial() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(apiKeyInput = "sk-typed") })
        }
        model.reloadLlmSettings()
        withTimeout(15_000) { model.state.first { !it.llmSettingsUi.loading } }
        assertNull(model.state.value.llmSettingsUi.draft)
        assertNull(model.state.value.llmSettingsUi.frozen)
        assertTrue(llmPuts().isEmpty())
    }

    @Test fun signOutAndIdentityChangeClearTypedKeyMaterial() = runBlocking(Dispatchers.IO) {
        val sessions = Sessions(AuthenticatedSession(identity, "token-a"))
        val model = model(sessions = sessions)
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(apiKeyInput = "sk-typed") })
        }
        model.logout()
        assertNull("sign-out drops the draft and its typed key", model.state.value.llmSettingsUi.draft)

        sessions.value = AuthenticatedSession(identity, "token-a")
        model.onLoggedIn()
        withTimeout(15_000) { model.state.first { it.accountKey == identity.cacheKey } }
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(apiKeyInput = "sk-typed") })
        }
        val next = AccountIdentity(identity.server, 2, 2)
        sessions.value = AuthenticatedSession(next, "token-b")
        model.onLoggedIn()
        withTimeout(15_000) { model.state.first { it.accountKey == next.cacheKey } }
        assertNull("an identity change drops the draft and its typed key", model.state.value.llmSettingsUi.draft)
        assertTrue(llmPuts().isEmpty())
    }

    @Test fun saveWithoutALoadedBaselineSendsNothing() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.saveLlmSettings()
        delay(300)
        assertTrue("no baseline means no request", llmPuts().isEmpty())
        assertEquals("Load the LLM services settings before saving.", model.state.value.llmSettingsUi.error)
    }

    @Test fun ambiguousFailureFreezesTheExactBodyAndRetryResendsIt() = runBlocking(Dispatchers.IO) {
        var failures = 1
        respond = { request ->
            if (request.url.encodedPath == "/v1/settings/llm" && request.method == "PUT" && failures-- > 0) {
                503 to "{}"
            } else defaultRespond(request)
        }
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(apiKeyInput = "sk-typed") })
        }
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.error != null && !it.llmSettingsUi.saving } }
        assertNotNull(model.state.value.llmSettingsUi.frozen)

        model.retryLlmSettingsSave()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.success } }
        val attempts = llmPuts()
        assertEquals(2, attempts.size)
        assertEquals("the retry resends the identical body and op ID", attempts[0].body, attempts[1].body)
        assertTrue(model.state.value.llmSettingsUi.draft == null)
    }

    @Test fun editingTheDraftInvalidatesTheFrozenRetry() = runBlocking(Dispatchers.IO) {
        var failures = 1
        respond = { request ->
            if (request.url.encodedPath == "/v1/settings/llm" && request.method == "PUT" && failures-- > 0) {
                503 to "{}"
            } else defaultRespond(request)
        }
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { it.copy(providers = it.providers.map { p -> p.copy(timeoutSeconds = "90") }) }
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.error != null && !it.llmSettingsUi.saving } }
        val frozenOpId = Json.parseToJsonElement(llmPuts().single().body).jsonObject.getValue("op_id").jsonPrimitive.content

        model.updateLlmSettingsDraft { it.copy(providers = it.providers.map { p -> p.copy(timeoutSeconds = "120") }) }
        assertNull("editing invalidates the frozen retry", model.state.value.llmSettingsUi.frozen)
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.success } }
        val retriedOpId = Json.parseToJsonElement(llmPuts().last().body).jsonObject.getValue("op_id").jsonPrimitive.content
        assertFalse("an edited draft must not reuse the frozen op ID", frozenOpId == retriedOpId)
        assertEquals("120", Json.parseToJsonElement(llmPuts().last().body).jsonObject.getValue("providers").jsonArray.single().jsonObject.getValue("timeout_seconds").jsonPrimitive.content)
    }

    @Test fun deterministicFailureIsNotFrozen() = runBlocking(Dispatchers.IO) {
        respond = { request ->
            if (request.url.encodedPath == "/v1/settings/llm" && request.method == "PUT") 400 to """{"error":"invalid_provider"}"""
            else defaultRespond(request)
        }
        val model = model()
        model.loadedLlm()
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.error != null && !it.llmSettingsUi.saving } }
        assertNull("a 4xx is deterministic and must not offer a retry", model.state.value.llmSettingsUi.frozen)
        assertTrue(model.state.value.llmSettingsUi.error!!.contains("invalid_provider"))
    }

    @Test fun unauthorizedSignsOutAndIsNeverRetried() = runBlocking(Dispatchers.IO) {
        respond = { request ->
            if (request.url.encodedPath == "/v1/settings/llm" && request.method == "PUT") 401 to """{"error":"unauthorized"}"""
            else defaultRespond(request)
        }
        val sessions = Sessions(AuthenticatedSession(identity, "token"))
        val model = model(sessions = sessions)
        model.loadedLlm()
        model.saveLlmSettings()
        withTimeout(15_000) { model.loggedIn.first { !it } }
        assertNull(sessions.current())
        val attempts = llmPuts().size
        model.retryLlmSettingsSave()
        delay(300)
        assertEquals("a 401 must never be replayed", attempts, llmPuts().size)
    }

    @Test fun aStaleSaveCannotPublishUnderTheNextAccount() = runBlocking(Dispatchers.IO) {
        val dispatcher = PausedRequestDispatcher()
        val sessions = Sessions(AuthenticatedSession(identity, "token-a"))
        val model = model(sessions = sessions, dispatcher = dispatcher)
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(apiKeyInput = "sk-typed") })
        }
        dispatcher.pauseNext = true
        model.saveLlmSettings()
        val queued = withTimeout(5_000) { dispatcher.held.await() }
        try {
            model.logout()
            val next = AccountIdentity(identity.server, 2, 2)
            sessions.value = AuthenticatedSession(next, "token-b")
            model.onLoggedIn()
            withTimeout(15_000) { model.state.first { it.accountKey == next.cacheKey } }
        } finally {
            queued.run()
        }
        assertNull("the old account's draft was sent under the next account", withTimeoutOrNull(500) { putSeen.await() })
    }

    @Test fun theTypedKeyNeverReachesTheSnapshotOrPreferences() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(apiKeyInput = "sk-typed") })
        }
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.success } }

        val cache = SessionCache(app, identity)
        val stored = cache.withEngine { engine -> engine.snapshot().objects.joinToString { it.raw } }
        assertFalse("the key must not reach the snapshot", stored.contains("sk-typed"))
        val preferences = cache.preferences.all.values.filterIsInstance<String>().joinToString()
        assertFalse("the key must not reach preferences", preferences.contains("sk-typed"))
        assertFalse(cache.preferences.all.keys.any { it.contains("llm", ignoreCase = true) })
        // Nothing was written outside the snapshot either: only the reader settings object exists.
        assertTrue(cache.withEngine { engine -> engine.snapshot().objects.none { it.kind == River.SETTINGS && it.raw.contains("llm") } })
    }

    @Test fun loadCompletionPreservesEditsMadeWhileItWasPending() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadedLlm()
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        getBody = llmJson(revision = 9)
        respond = { request ->
            if (request.method == "GET" && request.url.encodedPath == "/v1/settings/llm") {
                entered.complete(Unit)
                runBlocking { release.await() }
            }
            defaultRespond(request)
        }
        model.loadLlmSettings()
        try {
            withTimeout(5_000) { entered.await() }
            model.updateLlmSettingsDraft { it.copy(providers = it.providers.map { p -> p.copy(timeoutSeconds = "90") }) }
        } finally {
            release.complete(Unit)
        }
        withTimeout(15_000) { model.state.first { !it.llmSettingsUi.loading } }
        assertEquals("90", model.state.value.llmSettingsUi.draft?.providers?.single()?.timeoutSeconds)
        assertTrue("loading must not PUT", llmPuts().isEmpty())
    }

    @Test fun movingAKeyedProvidersUrlWithoutAKeyFailsBeforeAnyRequest() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(baseUrl = "https://api.openai.com/v2") })
        }
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.error != null && !it.llmSettingsUi.saving } }
        assertTrue(llmPuts().isEmpty())
        assertNull(model.state.value.llmSettingsUi.frozen)
        assertTrue(model.state.value.llmSettingsUi.error!!.contains("new API key"))
    }

    @Test fun malformedTimeoutFailsBeforeAnyRequest() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(providers = draft.providers.map { it.copy(timeoutSeconds = "soon") })
        }
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.error != null && !it.llmSettingsUi.saving } }
        assertTrue(llmPuts().isEmpty())
        assertNull(model.state.value.llmSettingsUi.frozen)
        assertTrue(model.state.value.llmSettingsUi.error!!.contains("timeout_seconds"))
    }

    @Test fun anAnthropicEmbedRouteFailsBeforeAnyRequest() = runBlocking(Dispatchers.IO) {
        getBody = llmJson(
            providers = """[{"name":"claude","kind":"anthropic","base_url":"https://api.anthropic.com","has_api_key":true,"monthly_limit_usd":"0","timeout_seconds":60,"token_field":"max_completion_tokens"}]""",
        )
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(tasks = draft.tasks.map { if (it.task == "embed") LlmTaskRoute("embed", "claude", "text-embedding-3-small") else it })
        }
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.error != null && !it.llmSettingsUi.saving } }
        assertTrue(llmPuts().isEmpty())
        assertTrue(model.state.value.llmSettingsUi.error!!.contains("Anthropic"))
    }

    @Test fun aMissingTaskRouteFailsBeforeAnyRequest() = runBlocking(Dispatchers.IO) {
        val model = model()
        model.loadedLlm()
        model.updateLlmSettingsDraft { draft ->
            draft.copy(tasks = draft.tasks.filterNot { it.task == "ask" })
        }
        model.saveLlmSettings()
        withTimeout(15_000) { model.state.first { it.llmSettingsUi.error != null && !it.llmSettingsUi.saving } }
        assertTrue(llmPuts().isEmpty())
        assertTrue(model.state.value.llmSettingsUi.error!!.contains("every known task"))
    }
}

package ch.lkmc.earnest.core

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.test.assertFailsWith
import java.util.concurrent.CopyOnWriteArrayList

class LlmSettingsTest {
    private val json = Json { ignoreUnknownKeys = true }

    private fun client(respond: (Request) -> Response): Pair<EarnestApi, List<Request>> {
        val requests = CopyOnWriteArrayList<Request>()
        val http = OkHttpClient.Builder().addInterceptor { chain ->
            val request = chain.request()
            requests += request
            respond(request)
        }.build()
        return EarnestApi("https://earnest.test", "secret", http) to requests
    }

    private fun response(request: Request, status: Int = 200, body: String = ""): Response =
        Response.Builder()
            .request(request)
            .protocol(Protocol.HTTP_1_1)
            .code(status)
            .message("test")
            .body(body.toResponseBody("application/json".toMediaType()))
            .build()

    private fun Request.bodyString(): String {
        val buffer = Buffer()
        body?.writeTo(buffer)
        return buffer.readUtf8()
    }

    private fun Request.bodyObject(): JsonObject = Json.parseToJsonElement(bodyString()).jsonObject

    private fun tasks(vararg routes: Triple<String, String, String>): List<LlmTaskRoute> =
        LlmTask.entries.map { task ->
            val route = routes.firstOrNull { it.first == task.wireName }
            LlmTaskRoute(task.wireName, route?.second.orEmpty(), route?.third.orEmpty())
        }

    private fun route(task: String, provider: String, model: String = provider) = Triple(task, provider, model)

    private fun provider(
        name: String = "openai",
        kind: LlmProviderKind = LlmProviderKind.OPENAI,
        baseUrl: String = "https://api.openai.com/v1",
        hasApiKey: Boolean = false,
        limit: String = "20.000000",
        timeout: Long = 60,
        tokenField: LlmTokenField = LlmTokenField.MAX_COMPLETION_TOKENS,
    ) = LlmProvider(name, kind, baseUrl, hasApiKey, limit, timeout, tokenField)

    private fun model(
        name: String = "text-embedding-3-small",
        input: String = "0.000000",
        output: String = "0.000000",
        embedding: String = "0.020000",
        dimensions: Long = 1536,
        truncatable: Boolean = true,
    ) = LlmModel(name, input, output, embedding, dimensions, truncatable)

    private fun settings(
        revision: Long = 0,
        providers: List<LlmProvider> = emptyList(),
        tasks: List<LlmTaskRoute> = emptyList(),
        models: List<LlmModel> = emptyList(),
    ) = LlmSettings(revision, providers, tasks, models)

    private fun input(
        revision: Long = 0,
        opId: String = "op-1",
        providers: List<LlmProviderInput> = emptyList(),
        tasks: List<LlmTaskRoute> = emptyList(),
        models: List<LlmModel> = emptyList(),
    ) = LlmSettingsInput(revision, opId, providers, tasks, models)

    private val defaultsJSON =
        """{"revision":0,"providers":[],"tasks":[],"models":[]}"""

    @Test
    fun getDecodesTheContractShapeAndLargeRevisions() = runBlocking {
        val (api, requests) = client {
            response(it, body = """
                {"revision":9223372036854775807,
                 "providers":[{"name":"anthropic","kind":"anthropic","base_url":"https://api.anthropic.com",
                   "has_api_key":true,"monthly_limit_usd":"0","timeout_seconds":120,"token_field":"max_tokens"}],
                 "tasks":[{"task":"embed","provider":"openai","model":"text-embedding-3-small"}],
                 "models":[{"name":"text-embedding-3-small","input_per_1m":"0.000000","output_per_1m":"0.000000",
                   "embedding_per_1m":"0.020000","dimensions":1536,"truncatable":true}]}
            """.trimIndent())
        }
        val loaded = api.llmSettings()
        assertEquals(listOf("GET"), requests.map { it.method })
        assertEquals("/v1/settings/llm", requests.single().url.encodedPath)
        assertEquals("Bearer secret", requests.single().header("Authorization"))
        assertEquals(Long.MAX_VALUE, loaded.revision)
        assertEquals(listOf(provider("anthropic", LlmProviderKind.ANTHROPIC, "https://api.anthropic.com", true, "0", 120, LlmTokenField.MAX_TOKENS)), loaded.providers)
        assertEquals(listOf(LlmTaskRoute("embed", "openai", "text-embedding-3-small")), loaded.tasks)
        assertEquals(listOf(model()), loaded.models)
    }

    @Test
    fun getToleratesAnEmptyDocument() = runBlocking {
        val (api, _) = client { response(it, body = defaultsJSON) }
        assertEquals(settings(), api.llmSettings())
    }

    @Test
    fun putSendsRevisionAndOpIdAndNeverAStoredKeyFlag() = runBlocking {
        val (api, requests) = client {
            response(it, body = """{"revision":9,"providers":[],"tasks":[],"models":[]}""")
        }
        api.updateLlmSettings(
            input(
                revision = 8,
                opId = "op-1",
                providers = listOf(
                    LlmProviderInput(
                        name = "openai",
                        kind = LlmProviderKind.OPENAI,
                        baseUrl = "https://api.openai.com/v1",
                        apiKey = "sk-typed-replacement",
                        monthlyLimitUsd = "20.000000",
                        timeoutSeconds = 60,
                    ),
                ),
                tasks = tasks(route("embed", "openai", "text-embedding-3-small")),
                models = listOf(model()),
            ),
            settings(),
        )
        val request = requests.single()
        assertEquals("PUT", request.method)
        assertEquals("/v1/settings/llm", request.url.encodedPath)
        val body = request.bodyObject()
        assertEquals(setOf("revision", "op_id", "providers", "tasks", "models"), body.keys)
        assertEquals("8", body.getValue("revision").jsonPrimitive.content)
        assertEquals("op-1", body.getValue("op_id").jsonPrimitive.content)
        val sent = body.getValue("providers").let { it as kotlinx.serialization.json.JsonArray }.single().jsonObject
        assertEquals(setOf("name", "kind", "base_url", "api_key", "clear_api_key", "monthly_limit_usd", "timeout_seconds", "token_field"), sent.keys)
        assertEquals("sk-typed-replacement", sent.getValue("api_key").jsonPrimitive.content)
        assertFalse("a PUT provider never carries has_api_key", sent.containsKey("has_api_key"))
    }

    @Test
    fun blankKeyIsOmittedAndClearRemovesTheStoredKey() = runBlocking {
        val baseline = settings(
            revision = 4,
            providers = listOf(provider("openai", hasApiKey = true), provider("local", hasApiKey = true, baseUrl = "http://192.168.1.5:11434/v1")),
            tasks = tasks(),
            models = listOf(model()),
        )
        val (api, requests) = client {
            response(it, body = """{"revision":5,"providers":[],"tasks":[],"models":[]}""")
        }
        api.updateLlmSettings(
            input(
                revision = 4,
                providers = listOf(
                    // Blank key: omitted from the body, the stored key is kept.
                    LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", null, false, "20.000000", 60),
                    // Explicit clear of a private endpoint's key.
                    LlmProviderInput("local", LlmProviderKind.OPENAI, "http://192.168.1.5:11434/v1", null, true, "0", 60),
                ),
                tasks = tasks(),
                models = listOf(model()),
            ),
            baseline,
        )
        val sent = requests.single().bodyObject().getValue("providers").let { it as kotlinx.serialization.json.JsonArray }
        val openai = sent[0].jsonObject
        assertFalse("a blank key must be omitted so the server keeps it", openai.containsKey("api_key"))
        assertFalse(openai.getValue("clear_api_key").jsonPrimitive.content.toBoolean())
        val local = sent[1].jsonObject
        assertFalse(local.containsKey("api_key"))
        assertTrue(local.getValue("clear_api_key").jsonPrimitive.content.toBoolean())
    }

    @Test
    fun roundTripPreservesUneditedEntriesAndPrices() = runBlocking {
        val loaded = settings(
            revision = 3,
            providers = listOf(provider("openai", hasApiKey = true), provider("local", baseUrl = "http://192.168.1.5:11434/v1", limit = "0", timeout = 30)),
            tasks = tasks(route("embed", "openai", "text-embedding-3-small"), route("summary", "local", "llama3")),
            models = listOf(model("text-embedding-3-small"), model("llama3", embedding = "0", dimensions = 1024, truncatable = false)),
        )
        val draft = input(
            revision = loaded.revision,
            providers = loaded.providers.map {
                LlmProviderInput(it.name, it.kind, it.baseUrl, apiKey = null, clearApiKey = false, it.monthlyLimitUsd, it.timeoutSeconds, it.tokenField)
            },
            tasks = loaded.tasks,
            models = loaded.models,
        )
        val (api, requests) = client {
            response(it, body = """{"revision":4,"providers":[],"tasks":[],"models":[]}""")
        }
        api.updateLlmSettings(draft, loaded)
        val body = requests.single().bodyObject()
        assertEquals(
            loaded.providers.map {
                LlmProviderInput(it.name, it.kind, it.baseUrl, apiKey = null, clearApiKey = false, it.monthlyLimitUsd, it.timeoutSeconds, it.tokenField)
            },
            json.decodeFromString(
                kotlinx.serialization.builtins.ListSerializer(LlmProviderInput.serializer()),
                body.getValue("providers").toString(),
            ),
        )
        assertEquals(
            loaded.tasks,
            json.decodeFromString(
                kotlinx.serialization.builtins.ListSerializer(LlmTaskRoute.serializer()),
                body.getValue("tasks").toString(),
            ),
        )
        assertEquals(
            loaded.models,
            json.decodeFromString(
                kotlinx.serialization.builtins.ListSerializer(LlmModel.serializer()),
                body.getValue("models").toString(),
            ),
        )
    }

    @Test
    fun suppliedOpIdIsSentVerbatimForIdenticalRetries() = runBlocking {
        val (api, requests) = client {
            response(it, body = """{"revision":1,"providers":[],"tasks":[],"models":[]}""")
        }
        val body = input(revision = 1, opId = "uncertain-op", providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1")), tasks = tasks(), models = listOf(model()))
        api.updateLlmSettings(body, settings())
        api.updateLlmSettings(body, settings())
        assertEquals(2, requests.size)
        for (request in requests) {
            assertEquals("uncertain-op", request.bodyObject().getValue("op_id").jsonPrimitive.content)
        }
    }

    @Test
    fun conflictIsAnApiExceptionAndNeverCarriesTheKey() = runBlocking {
        val (api, requests) = client {
            response(it, status = 409, body = """{"type":"about:blank","title":"Conflict","status":409,"error":"revision_conflict"}""")
        }
        val error = runCatching {
            api.updateLlmSettings(
                input(revision = 2, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", apiKey = "sk-private")), tasks = tasks(), models = listOf(model())),
                settings(),
            )
        }.exceptionOrNull() as ApiException
        assertEquals(409, error.status)
        assertTrue(error.isConflict)
        assertEquals("revision_conflict", error.code)
        assertFalse("the problem detail must not leak the typed key", error.message!!.contains("sk-private"))
        assertEquals(1, requests.size)
    }

    @Test
    fun invalidDocumentsFailBeforeAnyRequest() = runBlocking {
        val baseline = settings(providers = listOf(provider("openai", hasApiKey = true)), models = listOf(model()), tasks = tasks())
        val validProvider = LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1")
        val validModel = model()
        val invalid = listOf(
            input(revision = -1, tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, opId = "", tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("OpenAI", LlmProviderKind.OPENAI, "https://api.openai.com/v1")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("a--b", LlmProviderKind.OPENAI, "https://api.openai.com/v1")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("-a", LlmProviderKind.OPENAI, "https://api.openai.com/v1")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(validProvider, validProvider), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "ftp://api.openai.com/v1")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://user:pass@api.openai.com/v1")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1?x=1")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1#f")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", monthlyLimitUsd = "-1")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", monthlyLimitUsd = "1.1234567")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", monthlyLimitUsd = "100000000")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", monthlyLimitUsd = "1e3")), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", timeoutSeconds = 0)), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", timeoutSeconds = 601)), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", apiKey = "sk-new", clearApiKey = true)), tasks = tasks(), models = listOf(validModel)),
            input(revision = 0, providers = listOf(validProvider), tasks = tasks().dropLast(1), models = listOf(validModel)),
            input(revision = 0, providers = listOf(validProvider), tasks = tasks() + LlmTaskRoute("extra"), models = listOf(validModel)),
            input(revision = 0, providers = listOf(validProvider), tasks = tasks(route("embed", "missing", "text-embedding-3-small")), models = listOf(validModel)),
            input(revision = 0, providers = listOf(validProvider), tasks = tasks(route("embed", "openai", "text-embedding-3-small")), models = listOf(validModel, validModel)),
            input(revision = 0, providers = listOf(validProvider), tasks = tasks(route("embed", "openai", "text-embedding-3-small")), models = listOf(validModel.copy(name = ""))),
            input(revision = 0, providers = listOf(validProvider), tasks = tasks(route("embed", "openai", "text-embedding-3-small")), models = listOf(validModel.copy(inputPer1m = "-0.1"))),
            input(revision = 0, providers = listOf(validProvider), tasks = tasks(route("embed", "openai", "text-embedding-3-small")), models = listOf(validModel.copy(dimensions = 1025, truncatable = false))),
            input(revision = 0, providers = listOf(validProvider), tasks = tasks(route("embed", "openai", "text-embedding-3-small")), models = listOf(validModel.copy(dimensions = 65537, truncatable = true))),
            input(revision = 0, providers = listOf(validProvider), tasks = listOf(LlmTaskRoute("embed", "openai", "")) + tasks().filter { it.task != "embed" }, models = listOf(validModel)),
        )
        for ((index, document) in invalid.withIndex()) {
            val (api, requests) = client { error("unexpected request") }
            val failure = runCatching { api.updateLlmSettings(document, baseline) }.exceptionOrNull()
            assertTrue("case $index accepted: $document", failure is IllegalArgumentException)
            assertTrue("case $index sent a request", requests.isEmpty())
        }
    }

    @Test
    fun anthropicCannotEmbed() = runBlocking {
        val anthropic = LlmProviderInput("claude", LlmProviderKind.ANTHROPIC, "https://api.anthropic.com")
        val (api, requests) = client { error("unexpected request") }
        val failure = runCatching {
            api.updateLlmSettings(input(providers = listOf(anthropic), tasks = tasks(route("embed", "claude", "text-embedding-3-small")), models = listOf(model())), settings())
        }.exceptionOrNull()
        assertTrue(failure is IllegalArgumentException)
        assertTrue(requests.isEmpty())
        // The same provider routes the other tasks fine.
        val (okApi, okRequests) = client { response(it, body = """{"revision":1,"providers":[],"tasks":[],"models":[]}""") }
        okApi.updateLlmSettings(input(providers = listOf(anthropic), tasks = tasks(route("summary", "claude", "text-embedding-3-small")), models = listOf(model())), settings())
        assertEquals(1, okRequests.size)
    }

    @Test
    fun discoverPostsTheRevisionAndTheDraftProvider() = runBlocking {
        val (api, requests) = client {
            response(it, body = """{"models":[{"id":"gpt-5","name":"GPT 5","chat":true,"input_per_1m":"1.25","output_per_1m":"10"}]}""")
        }
        val found = api.discoverLlmModels(
            LlmModelsInput(
                revision = 7,
                provider = LlmProviderInput(
                    name = "openai",
                    kind = LlmProviderKind.OPENAI,
                    baseUrl = "https://api.openai.com/v1",
                    apiKey = "sk-typed",
                ),
            ),
            settings(),
        )
        val request = requests.single()
        assertEquals("POST", request.method)
        assertEquals("/v1/settings/llm/models", request.url.encodedPath)
        val body = request.bodyObject()
        assertEquals(setOf("revision", "provider"), body.keys)
        assertEquals("7", body.getValue("revision").jsonPrimitive.content)
        val provider = body.getValue("provider").jsonObject
        assertEquals("sk-typed", provider.getValue("api_key").jsonPrimitive.content)
        assertFalse(provider.containsKey("has_api_key"))
        assertEquals(
            listOf(LlmDiscoveredModel("gpt-5", "GPT 5", chat = true, inputPer1m = "1.25", outputPer1m = "10")),
            found.models,
        )
    }

    @Test
    fun discoverDecodesSparseEntriesAndANullList() = runBlocking {
        val (api, _) = client {
            response(it, body = """
                {"models":[{"id":"sparse","name":"Sparse"},
                  {"id":"full","name":"Full","chat":true,"embedding":false,"input_per_1m":"1",
                    "output_per_1m":"2","embedding_per_1m":"3","dimensions":1536,"truncatable":true}]}
            """.trimIndent())
        }
        val found = api.discoverLlmModels(
            LlmModelsInput(0, LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1")),
            settings(),
        )
        // Missing fields mean unknown: they decode as absent, never as false or zero.
        assertEquals(LlmDiscoveredModel("sparse", "Sparse"), found.models!![0])
        assertNull(found.models!![0].chat)
        assertNull(found.models!![0].inputPer1m)
        assertNull(found.models!![0].dimensions)
        assertEquals(LlmDiscoveredModel("full", "Full", true, false, "1", "2", "3", 1536, true), found.models!![1])

        val (emptyApi, _) = client { response(it, body = """{"models":null}""") }
        assertNull(
            emptyApi.discoverLlmModels(
                LlmModelsInput(0, LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1")),
                settings(),
            ).models,
        )
    }

    @Test
    fun discoverValidationFailsBeforeAnyRequest() = runBlocking {
        val baseline = settings(revision = 2, providers = listOf(provider("openai", hasApiKey = true)))
        val openai = LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1")
        val invalid = listOf(
            LlmModelsInput(-1, openai),
            LlmModelsInput(0, LlmProviderInput("OpenAI", LlmProviderKind.OPENAI, "https://api.openai.com/v1")),
            LlmModelsInput(0, LlmProviderInput("openai", LlmProviderKind.OPENAI, "ftp://api.openai.com/v1")),
            LlmModelsInput(0, LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", monthlyLimitUsd = "-1")),
            LlmModelsInput(0, LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", timeoutSeconds = 0)),
            LlmModelsInput(0, openai.copy(apiKey = "sk", clearApiKey = true)),
            // A stored key is never forwarded to a moved destination.
            LlmModelsInput(2, LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v2")),
            LlmModelsInput(2, LlmProviderInput("openai", LlmProviderKind.ANTHROPIC, "https://api.openai.com/v1")),
        )
        for ((index, document) in invalid.withIndex()) {
            val (api, requests) = client { error("unexpected request") }
            val failure = runCatching { api.discoverLlmModels(document, baseline) }.exceptionOrNull()
            assertTrue("case $index accepted: $document", failure is IllegalArgumentException)
            assertTrue("case $index sent a request", requests.isEmpty())
        }
        // The same moved destination is fine with a replacement key or a clear.
        val (api, requests) = client { response(it, body = """{"models":[]}""") }
        api.discoverLlmModels(LlmModelsInput(2, LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v2", apiKey = "sk-new")), baseline)
        api.discoverLlmModels(LlmModelsInput(2, LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v2", clearApiKey = true)), baseline)
        assertEquals(2, requests.size)
    }

    @Test
    fun discoverConflictIsAnApiException() = runBlocking {
        val (api, _) = client {
            response(it, status = 409, body = """{"type":"about:blank","title":"Conflict","status":409,"error":"revision_conflict"}""")
        }
        val error = runCatching {
            api.discoverLlmModels(
                LlmModelsInput(3, LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1", apiKey = "sk-private")),
                settings(),
            )
        }.exceptionOrNull() as ApiException
        assertTrue(error.isConflict)
        assertFalse("the problem detail must not leak the typed key", error.message!!.contains("sk-private"))
    }

    @Test
    fun movingAKeyedProviderRequiresAReplacementKeyOrAnExplicitClear() = runBlocking {
        val baseline = settings(
            revision = 2,
            providers = listOf(provider("openai", hasApiKey = true), provider("local", baseUrl = "http://192.168.1.5:11434/v1", hasApiKey = true)),
            models = listOf(model()),
            tasks = tasks(),
        )
        for (moved in listOf(
            LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v2"),
            LlmProviderInput("openai", LlmProviderKind.ANTHROPIC, "https://api.openai.com/v1"),
            LlmProviderInput("local", LlmProviderKind.OPENAI, "http://192.168.1.6:11434/v1"),
        )) {
            val (api, requests) = client { error("unexpected request") }
            val failure = runCatching {
                api.updateLlmSettings(input(providers = listOf(moved), tasks = tasks(), models = listOf(model())), baseline)
            }.exceptionOrNull()
            assertTrue("accepted a moved key: $moved", failure is IllegalArgumentException)
            assertTrue(requests.isEmpty())
        }
        // A replacement key or an explicit clear allows the move.
        for (allowed in listOf(
            LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v2", apiKey = "sk-new"),
            LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v2", clearApiKey = true),
        )) {
            val (api, requests) = client { response(it, body = """{"revision":3,"providers":[],"tasks":[],"models":[]}""") }
            api.updateLlmSettings(
                input(providers = listOf(allowed), tasks = tasks(), models = listOf(model())),
                baseline,
            )
            assertEquals(1, requests.size)
        }
        // An unkeyed provider may move freely.
        val unkeyed = LlmProviderInput("local", LlmProviderKind.OPENAI, "http://192.168.1.6:11434/v1")
        val (api, requests) = client { response(it, body = """{"revision":3,"providers":[],"tasks":[],"models":[]}""") }
        api.updateLlmSettings(
            input(providers = listOf(unkeyed), tasks = tasks(), models = listOf(model())),
            baseline.copy(providers = emptyList()),
        )
        assertEquals(1, requests.size)
    }

    @Test
    fun serverAlignedMoneyAndUrlNormalizationAreAccepted() = runBlocking {
        // The server's parser accepts a missing whole part and a leading +, and
        // compares destinations after trimming trailing slashes.
        val (api, requests) = client {
            response(it, body = """{"revision":1,"providers":[],"tasks":[],"models":[]}""")
        }
        api.updateLlmSettings(
            input(
                providers = listOf(
                    LlmProviderInput("openai", LlmProviderKind.OPENAI, "https://api.openai.com/v1/", monthlyLimitUsd = ".5"),
                ),
                tasks = tasks(),
                models = listOf(model(input = "+0.25", output = "1", embedding = "0.000001", dimensions = 0, truncatable = false)),
            ),
            settings(providers = listOf(provider("openai", hasApiKey = true))),
        )
        assertEquals(1, requests.size)
    }

    @Test
    fun boundaryLimitsAreAccepted() = runBlocking {
        val providers = (1..LlmSettingsLimits.MAX_PROVIDERS).map {
            LlmProviderInput("p$it", LlmProviderKind.OPENAI, "https://p$it.example/v1")
        }
        val models = (1..LlmSettingsLimits.MAX_MODELS).map {
            model(name = "m$it", dimensions = 65536, truncatable = true)
        }
        val (api, requests) = client { response(it, body = """{"revision":1,"providers":[],"tasks":[],"models":[]}""") }
        api.updateLlmSettings(input(providers = providers, tasks = tasks(), models = models), settings())
        assertEquals(1, requests.size)
    }

    @Test
    fun enumWireNamesMatchTheServer() {
        for ((value, wire) in listOf(
            LlmProviderKind.OPENAI to "openai", LlmProviderKind.ANTHROPIC to "anthropic", LlmProviderKind.GEMINI to "gemini",
        )) {
            assertEquals("\"$wire\"", json.encodeToString(value))
            assertEquals(value, json.decodeFromString(LlmProviderKind.serializer(), "\"$wire\""))
        }
        for ((value, wire) in listOf(
            LlmTokenField.MAX_COMPLETION_TOKENS to "max_completion_tokens", LlmTokenField.MAX_TOKENS to "max_tokens",
        )) {
            assertEquals("\"$wire\"", json.encodeToString(value))
            assertEquals(value, json.decodeFromString(LlmTokenField.serializer(), "\"$wire\""))
        }
        for (task in LlmTask.entries) assertEquals(task, LlmTask.fromWire(task.wireName))
        assertNull(LlmTask.fromWire("translate"))
    }
}

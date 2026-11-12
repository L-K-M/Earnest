package ch.lkmc.earnest.core

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import java.math.BigDecimal

/**
 * Wire models for the server-managed LLM services settings at
 * `/v1/settings/llm`. The configuration is shared for the whole server, so
 * clients never cache, sync or persist it: every screen loads it explicitly
 * and a save replaces the whole provider/task/model configuration.
 *
 * API keys are write-only. GET never returns one ([LlmProvider.hasApiKey] is
 * the only trace); a PUT omits the key or sends an empty string to keep the
 * stored one, and `clear_api_key` removes it.
 */

@Serializable
enum class LlmProviderKind {
    @SerialName("openai") OPENAI,
    @SerialName("anthropic") ANTHROPIC,
    @SerialName("gemini") GEMINI,
}

@Serializable
enum class LlmTokenField {
    @SerialName("max_completion_tokens") MAX_COMPLETION_TOKENS,
    @SerialName("max_tokens") MAX_TOKENS,
}

/** The seven server task routes. A route with both halves empty is disabled. */
enum class LlmTask(val wireName: String) {
    EMBED("embed"),
    CLASSIFY("classify"),
    BLURB("blurb"),
    SUMMARY("summary"),
    EDITION("edition"),
    COMPILE_RULE("compile_rule"),
    ASK("ask");

    companion object {
        fun fromWire(name: String): LlmTask? = entries.firstOrNull { it.wireName == name }
    }
}

@Serializable
data class LlmProvider(
    val name: String,
    val kind: LlmProviderKind,
    @SerialName("base_url") val baseUrl: String,
    @SerialName("has_api_key") val hasApiKey: Boolean = false,
    @SerialName("monthly_limit_usd") val monthlyLimitUsd: String = "0",
    @SerialName("timeout_seconds") val timeoutSeconds: Long = 60,
    @SerialName("token_field") val tokenField: LlmTokenField = LlmTokenField.MAX_COMPLETION_TOKENS,
)

/**
 * A provider replacement. A blank or omitted [apiKey] keeps the stored key;
 * [clearApiKey] removes it and cannot be combined with a new key.
 */
@Serializable
data class LlmProviderInput(
    val name: String,
    val kind: LlmProviderKind,
    @SerialName("base_url") val baseUrl: String,
    @SerialName("api_key") val apiKey: String? = null,
    @SerialName("clear_api_key") val clearApiKey: Boolean = false,
    @SerialName("monthly_limit_usd") val monthlyLimitUsd: String = "0",
    @SerialName("timeout_seconds") val timeoutSeconds: Long = 60,
    @SerialName("token_field") val tokenField: LlmTokenField = LlmTokenField.MAX_COMPLETION_TOKENS,
)

@Serializable
data class LlmModel(
    val name: String,
    @SerialName("input_per_1m") val inputPer1m: String = "0",
    @SerialName("output_per_1m") val outputPer1m: String = "0",
    @SerialName("embedding_per_1m") val embeddingPer1m: String = "0",
    val dimensions: Long = 0,
    val truncatable: Boolean = false,
)

@Serializable
data class LlmTaskRoute(
    val task: String,
    val provider: String = "",
    val model: String = "",
)

/** The redacted settings document returned by GET and by PUT. */
@Serializable
data class LlmSettings(
    val revision: Long = 0,
    val providers: List<LlmProvider> = emptyList(),
    val tasks: List<LlmTaskRoute> = emptyList(),
    val models: List<LlmModel> = emptyList(),
)

/** The whole-configuration replacement body; [revision] comes from the GET. */
@Serializable
data class LlmSettingsInput(
    val revision: Long,
    @SerialName("op_id") val opId: String,
    val providers: List<LlmProviderInput>,
    val tasks: List<LlmTaskRoute>,
    val models: List<LlmModel>,
)

/**
 * A catalogue entry previewed by `POST /v1/settings/llm/models`: a
 * suggestion, not a configured model. Every field beyond [id] and [name] is
 * optional, and a missing one means the provider did not say; never that a
 * capability is absent or that a price is zero.
 */
@Serializable
data class LlmDiscoveredModel(
    val id: String,
    val name: String = "",
    val chat: Boolean? = null,
    val embedding: Boolean? = null,
    @SerialName("input_per_1m") val inputPer1m: String? = null,
    @SerialName("output_per_1m") val outputPer1m: String? = null,
    @SerialName("embedding_per_1m") val embeddingPer1m: String? = null,
    val dimensions: Long? = null,
    val truncatable: Boolean? = null,
)

/**
 * The no-store preview body: the loaded settings [revision] plus a single
 * draft provider. The provider may carry an unsaved replacement key; a blank
 * key inherits the stored one under the same rules as a save.
 */
@Serializable
data class LlmModelsInput(
    val revision: Long,
    val provider: LlmProviderInput,
)

/** The catalogue preview response. A null list means the provider listed none. */
@Serializable
data class LlmDiscoveredModels(
    val models: List<LlmDiscoveredModel>? = emptyList(),
)

/**
 * The server's configured-model table size. Catalogue browsing is never
 * limited by it; only the entries imported into the draft count.
 */
const val LLM_MAX_CONFIGURED_MODELS = 128

/** Server-side ranges mirrored from the Go schema; validated before any request. */
internal object LlmSettingsLimits {
    const val MAX_PROVIDERS = 32
    const val MAX_MODELS = LLM_MAX_CONFIGURED_MODELS
    const val MAX_PROVIDER_NAME_CHARS = 64
    const val MAX_MODEL_NAME_CHARS = 256
    const val MIN_TIMEOUT_SECONDS = 1L
    const val MAX_TIMEOUT_SECONDS = 600L
    const val MAX_DIMENSIONS = 1024L
    const val MAX_TRUNCATABLE_DIMENSIONS = 65536L
    const val MAX_OP_ID_BYTES = 1024
    val MAX_MONEY: BigDecimal = BigDecimal("99999999.999999")
}

private val PROVIDER_NAME = Regex("^[a-z0-9]+(-[a-z0-9]+)*$")

// Mirrors the server's decimal parser: optional leading +, digits with at
// most six fractional places, and a missing whole part allowed (".5").
private val MONEY = Regex("^\\+?(?:\\d+(?:\\.\\d{1,6})?|\\.\\d{1,6})$")

/**
 * Exact currency without floating point: the plain-decimal shape check keeps
 * scientific notation and signs out, then a BigDecimal range check enforces
 * the server money ceiling. Zero is valid and means unlimited for a limit,
 * explicit zero pricing for a model.
 */
private fun validMoney(value: String): Boolean {
    val text = value.trim()
    if (!MONEY.matches(text)) return false
    val parsed = text.removePrefix("+").toBigDecimalOrNull() ?: return false
    return parsed >= BigDecimal.ZERO && parsed <= LlmSettingsLimits.MAX_MONEY
}

/**
 * The server's money shape, exposed for price-review UI: a plain
 * non-negative decimal with at most six fractional places under the money
 * ceiling. Blank is not a price; the review decides which blanks may
 * default to zero.
 */
fun validLlmMoney(value: String): Boolean = validMoney(value)

/** The server compares destinations after trimming whitespace and trailing slashes. */
private fun normalizedBaseUrl(value: String): String = value.trim().trimEnd('/')

/** http(s) with a host, no credentials, query or fragment; the path may be non-root. */
private fun validBaseUrl(value: String): Boolean {
    val url = value.toHttpUrlOrNull() ?: return false
    return (url.scheme == "http" || url.scheme == "https") && url.host.isNotEmpty() &&
        url.username.isEmpty() && url.password.isEmpty() && url.query == null && url.fragment == null
}

/**
 * Validates a whole-configuration replacement before any request, mirroring
 * the server's limits. [baseline] is the loaded redacted settings, used only
 * for the rule that a keyed provider's URL or kind cannot change without a
 * replacement key or an explicit clear, so a stored secret is never
 * forwarded to a different destination. Validation never sees a key.
 */
internal fun validateLlmSettingsInput(input: LlmSettingsInput, baseline: LlmSettings?) {
    require(input.revision >= 0) { "revision out of range" }
    require(input.opId.isNotEmpty() && input.opId.toByteArray(Charsets.UTF_8).size <= LlmSettingsLimits.MAX_OP_ID_BYTES) {
        "op_id out of range"
    }
    require(input.providers.size <= LlmSettingsLimits.MAX_PROVIDERS) { "at most ${LlmSettingsLimits.MAX_PROVIDERS} providers are allowed" }
    require(input.models.size <= LlmSettingsLimits.MAX_MODELS) { "at most ${LlmSettingsLimits.MAX_MODELS} models are allowed" }

    val storedByName = baseline?.providers?.associateBy { it.name }.orEmpty()
    val providerNames = mutableSetOf<String>()
    for (provider in input.providers) {
        require(PROVIDER_NAME.matches(provider.name) && provider.name.length <= LlmSettingsLimits.MAX_PROVIDER_NAME_CHARS) {
            "provider names are lowercase letters, digits and single dashes, at most ${LlmSettingsLimits.MAX_PROVIDER_NAME_CHARS} characters"
        }
        require(providerNames.add(provider.name)) { "provider names must be unique" }
        validateProviderInput(provider, storedByName[provider.name])
    }

    val modelNames = mutableSetOf<String>()
    for (model in input.models) {
        require(model.name.isNotEmpty() && model.name.length <= LlmSettingsLimits.MAX_MODEL_NAME_CHARS) {
            "model names are 1 to ${LlmSettingsLimits.MAX_MODEL_NAME_CHARS} characters"
        }
        require(modelNames.add(model.name)) { "model names must be unique" }
        require(validMoney(model.inputPer1m)) { "input_per_1m must be a decimal with at most six fractional digits" }
        require(validMoney(model.outputPer1m)) { "output_per_1m must be a decimal with at most six fractional digits" }
        require(validMoney(model.embeddingPer1m)) { "embedding_per_1m must be a decimal with at most six fractional digits" }
        val maxDimensions = if (model.truncatable) LlmSettingsLimits.MAX_TRUNCATABLE_DIMENSIONS else LlmSettingsLimits.MAX_DIMENSIONS
        require(model.dimensions in 0..maxDimensions) { "dimensions must be between 0 and $maxDimensions" }
    }

    require(input.tasks.size == LlmTask.entries.size) { "every known task must be configured exactly once" }
    require(input.tasks.all { LlmTask.fromWire(it.task) != null }) { "unknown task name" }
    require(input.tasks.map { it.task }.let { it.size == it.toSet().size }) { "task names must be unique" }
    val kinds = input.providers.associate { it.name to it.kind }
    for (task in input.tasks) {
        require((task.provider.isEmpty()) == (task.model.isEmpty())) { "task provider and model must be configured together" }
        if (task.provider.isEmpty()) continue
        require(task.provider in kinds) { "task provider is not configured" }
        require(task.model in modelNames) { "task model is not configured" }
        if (task.task == LlmTask.EMBED.wireName) {
            require(kinds.getValue(task.provider) != LlmProviderKind.ANTHROPIC) { "Anthropic cannot embed" }
        }
    }
}

/**
 * The field checks a provider input must pass, shared by the
 * whole-configuration replacement and the single-provider catalogue
 * preview. [stored] is the baseline provider of the same name, used only for
 * the rule that a stored key is never forwarded to a changed destination.
 */
private fun validateProviderInput(provider: LlmProviderInput, stored: LlmProvider?) {
    require(PROVIDER_NAME.matches(provider.name) && provider.name.length <= LlmSettingsLimits.MAX_PROVIDER_NAME_CHARS) {
        "provider names are lowercase letters, digits and single dashes, at most ${LlmSettingsLimits.MAX_PROVIDER_NAME_CHARS} characters"
    }
    require(validBaseUrl(provider.baseUrl)) { "base_url must be an http(s) URL with a host and no credentials, query or fragment" }
    require(validMoney(provider.monthlyLimitUsd)) { "monthly_limit_usd must be a decimal with at most six fractional digits" }
    require(provider.timeoutSeconds in LlmSettingsLimits.MIN_TIMEOUT_SECONDS..LlmSettingsLimits.MAX_TIMEOUT_SECONDS) {
        "timeout_seconds must be between ${LlmSettingsLimits.MIN_TIMEOUT_SECONDS} and ${LlmSettingsLimits.MAX_TIMEOUT_SECONDS}"
    }
    require(!(provider.clearApiKey && !provider.apiKey.isNullOrEmpty())) { "clear_api_key cannot be combined with a new API key" }
    val movedKey = stored != null && stored.hasApiKey && provider.apiKey.isNullOrEmpty() && !provider.clearApiKey &&
        (normalizedBaseUrl(stored.baseUrl) != normalizedBaseUrl(provider.baseUrl) || stored.kind != provider.kind)
    require(!movedKey) { "changing the URL or kind of a keyed provider requires a new API key or clearing the saved one" }
}

/**
 * Validates a catalogue preview before any request: the same provider rules
 * a save would enforce, so a draft the server could not accept never leaves
 * the client, and a stored key is never forwarded to a moved destination.
 */
internal fun validateLlmModelsInput(input: LlmModelsInput, baseline: LlmSettings?) {
    require(input.revision >= 0) { "revision out of range" }
    validateProviderInput(input.provider, baseline?.providers?.firstOrNull { it.name == input.provider.name })
}

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

/**
 * How a task picks its route. [OFF] disables the task, [DEFAULT] resolves
 * through the matching default route (text for every task except embed, which
 * uses the embedding default), and [CUSTOM] uses the explicit provider and
 * model. A missing mode is the legacy explicit route: [CUSTOM] when the route
 * is non-empty, [OFF] otherwise.
 */
@Serializable
enum class LlmTaskMode(val wireName: String) {
    @SerialName("off") OFF("off"),
    @SerialName("default") DEFAULT("default"),
    @SerialName("custom") CUSTOM("custom");

    companion object {
        fun fromWire(name: String): LlmTaskMode? = entries.firstOrNull { it.wireName == name }
    }
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

/** One provider/model pair. Both halves empty means "no default route". */
@Serializable
data class LlmDefaultRoute(
    val provider: String = "",
    val model: String = "",
)

/**
 * The optional server-wide default routes. Text work (every task except
 * embed) resolves through [text]; embed resolves through [embedding]. An
 * empty route leaves the task disabled: selecting a default never enables a
 * task that is switched off.
 */
@Serializable
data class LlmSettingsDefaults(
    val text: LlmDefaultRoute = LlmDefaultRoute(),
    val embedding: LlmDefaultRoute = LlmDefaultRoute(),
) {
    fun routeFor(task: LlmTask): LlmDefaultRoute = if (task == LlmTask.EMBED) embedding else text

    val isEmpty: Boolean
        get() = text.provider.isEmpty() && text.model.isEmpty() &&
            embedding.provider.isEmpty() && embedding.model.isEmpty()
}

/** The capability a model check exercises. */
@Serializable
enum class LlmModelCapability(val wireName: String) {
    @SerialName("chat") CHAT("chat"),
    @SerialName("embedding") EMBEDDING("embedding");
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
    /** The provider this price row belongs to; empty is the legacy global fallback. */
    val provider: String = "",
    /** Whether the provider accepts a custom embedding width; distinct from [truncatable]. */
    @SerialName("supports_custom_dimensions") val supportsCustomDimensions: Boolean = false,
) {
    /** The identity a route resolves: a scoped row wins over the global one. */
    fun identity(): Pair<String, String> = provider to name

    companion object {
        /** Resolves a route's model: the scoped row first, then the global fallback. */
        fun resolve(models: List<LlmModel>, provider: String, name: String): LlmModel? =
            models.firstOrNull { it.provider == provider && it.name == name }
                ?: models.firstOrNull { it.provider.isEmpty() && it.name == name }
    }
}

@Serializable
data class LlmTaskRoute(
    val task: String,
    val provider: String = "",
    val model: String = "",
    val mode: LlmTaskMode? = null,
) {
    /** The mode a legacy missing value means: custom when routed, off otherwise. */
    fun resolvedMode(): LlmTaskMode = mode ?: if (provider.isNotEmpty()) LlmTaskMode.CUSTOM else LlmTaskMode.OFF
}

/** The redacted settings document returned by GET and by PUT. */
@Serializable
data class LlmSettings(
    val revision: Long = 0,
    val providers: List<LlmProvider> = emptyList(),
    val tasks: List<LlmTaskRoute> = emptyList(),
    val models: List<LlmModel> = emptyList(),
    val defaults: LlmSettingsDefaults? = null,
)

/** The whole-configuration replacement body; [revision] comes from the GET. */
@Serializable
data class LlmSettingsInput(
    val revision: Long,
    @SerialName("op_id") val opId: String,
    val providers: List<LlmProviderInput>,
    val tasks: List<LlmTaskRoute>,
    val models: List<LlmModel>,
    val defaults: LlmSettingsDefaults? = null,
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
    @SerialName("supports_custom_dimensions") val supportsCustomDimensions: Boolean? = null,
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

/**
 * The explicit model check body: the loaded [revision], the draft provider
 * (which may carry an unsaved replacement key), the model being considered
 * and the capability to exercise. The provider may bill a fixed sample.
 */
@Serializable
data class LlmModelCheckInput(
    val revision: Long,
    val provider: LlmProviderInput,
    val model: LlmModel,
    val capability: LlmModelCapability,
)

/** The check outcome. Observed geometry is a suggestion for the draft only. */
@Serializable
data class LlmModelCheck(
    val compatible: Boolean = false,
    val dimensions: Long? = null,
    @SerialName("supports_custom_dimensions") val supportsCustomDimensions: Boolean? = null,
    val message: String = "",
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

    val modelIdentities = mutableSetOf<Pair<String, String>>()
    for (model in input.models) {
        require(model.name.isNotEmpty() && model.name.length <= LlmSettingsLimits.MAX_MODEL_NAME_CHARS) {
            "model names are 1 to ${LlmSettingsLimits.MAX_MODEL_NAME_CHARS} characters"
        }
        require(model.provider.isEmpty() || model.provider in providerNames) { "model provider is not configured" }
        require(modelIdentities.add(model.identity())) { "model provider and name must be unique" }
        require(validMoney(model.inputPer1m)) { "input_per_1m must be a decimal with at most six fractional digits" }
        require(validMoney(model.outputPer1m)) { "output_per_1m must be a decimal with at most six fractional digits" }
        require(validMoney(model.embeddingPer1m)) { "embedding_per_1m must be a decimal with at most six fractional digits" }
        // A wide native vector needs either truncation or a provider that can
        // accept a requested width; the two flags are not interchangeable.
        val maxDimensions = if (model.truncatable || model.supportsCustomDimensions) {
            LlmSettingsLimits.MAX_TRUNCATABLE_DIMENSIONS
        } else {
            LlmSettingsLimits.MAX_DIMENSIONS
        }
        require(model.dimensions in 0..maxDimensions) { "dimensions must be between 0 and $maxDimensions" }
    }

    val kinds = input.providers.associate { it.name to it.kind }
    input.defaults?.let { defaults ->
        validateDefaultRoute(defaults.text, false, kinds, input.models)
        validateDefaultRoute(defaults.embedding, true, kinds, input.models)
    }

    require(input.tasks.size == LlmTask.entries.size) { "every known task must be configured exactly once" }
    require(input.tasks.all { LlmTask.fromWire(it.task) != null }) { "unknown task name" }
    require(input.tasks.map { it.task }.let { it.size == it.toSet().size }) { "task names must be unique" }
    for (task in input.tasks) {
        val known = requireNotNull(LlmTask.fromWire(task.task))
        val embedding = known == LlmTask.EMBED
        val route = LlmDefaultRoute(task.provider, task.model)
        when (task.mode) {
            // Off ignores any route it carries; the effective route is empty.
            LlmTaskMode.OFF -> Unit
            LlmTaskMode.CUSTOM -> {
                require(route.provider.isNotEmpty() && route.model.isNotEmpty()) {
                    "a custom task needs both a provider and a model"
                }
                validateDefaultRoute(route, embedding, kinds, input.models)
            }
            LlmTaskMode.DEFAULT -> {
                require(input.defaults != null) { "choose a default model before enabling this feature" }
                val effective = input.defaults.routeFor(known)
                require(effective.provider.isNotEmpty() && effective.model.isNotEmpty()) {
                    "choose a model before enabling this feature"
                }
                // The effective route is resolved from the defaults and sent
                // too, so an older server without modes still routes it.
                require(task.provider == effective.provider && task.model == effective.model) {
                    "a default task must send the effective default route"
                }
                validateDefaultRoute(effective, embedding, kinds, input.models)
            }
            null -> {
                // A missing mode is the legacy explicit route.
                if (route.provider.isEmpty() && route.model.isEmpty()) continue
                require(route.provider.isNotEmpty() && route.model.isNotEmpty()) {
                    "task provider and model must be configured together"
                }
                validateDefaultRoute(route, embedding, kinds, input.models)
            }
        }
    }
}

/** One route half must imply the other, the provider must exist, the model
 *  must resolve (scoped first, then the global fallback), and embed cannot
 *  use Anthropic. */
private fun validateDefaultRoute(
    route: LlmDefaultRoute,
    embedding: Boolean,
    kinds: Map<String, LlmProviderKind>,
    models: List<LlmModel>,
) {
    require((route.provider.isEmpty()) == (route.model.isEmpty())) { "provider and model must be configured together" }
    if (route.provider.isEmpty()) return
    require(route.provider in kinds) { "provider is not configured" }
    require(LlmModel.resolve(models, route.provider, route.model) != null) { "model is not configured" }
    if (embedding) require(kinds.getValue(route.provider) != LlmProviderKind.ANTHROPIC) { "Anthropic cannot embed" }
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

/**
 * Validates an explicit model check before any request. The model is the
 * draft record, so the same price and geometry rules a save would enforce
 * apply; the provider rules match the catalogue preview.
 */
internal fun validateLlmModelCheckInput(input: LlmModelCheckInput, baseline: LlmSettings?) {
    require(input.revision >= 0) { "revision out of range" }
    validateProviderInput(input.provider, baseline?.providers?.firstOrNull { it.name == input.provider.name })
    require(input.model.name.isNotEmpty() && input.model.name.length <= LlmSettingsLimits.MAX_MODEL_NAME_CHARS) {
        "model names are 1 to ${LlmSettingsLimits.MAX_MODEL_NAME_CHARS} characters"
    }
    require(validMoney(input.model.inputPer1m)) { "input_per_1m must be a decimal with at most six fractional digits" }
    require(validMoney(input.model.outputPer1m)) { "output_per_1m must be a decimal with at most six fractional digits" }
    require(validMoney(input.model.embeddingPer1m)) { "embedding_per_1m must be a decimal with at most six fractional digits" }
    val maxDimensions = if (input.model.truncatable || input.model.supportsCustomDimensions) {
        LlmSettingsLimits.MAX_TRUNCATABLE_DIMENSIONS
    } else {
        LlmSettingsLimits.MAX_DIMENSIONS
    }
    require(input.model.dimensions in 0..maxDimensions) { "dimensions must be between 0 and $maxDimensions" }
}

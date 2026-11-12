package ch.lkmc.earnest

import ch.lkmc.earnest.core.LLM_MAX_CONFIGURED_MODELS
import ch.lkmc.earnest.core.LlmDiscoveredModel
import ch.lkmc.earnest.core.LlmModel
import ch.lkmc.earnest.core.LlmTask
import ch.lkmc.earnest.core.validLlmMoney

/**
 * Pure helpers for the provider model catalogue. A catalogue entry is a
 * suggestion, never a configured model: a missing capability or price field
 * means the provider did not say, never "cannot" or "free". Results only
 * ever reach the in-memory draft, and only an explicit selection changes a
 * task route; Save persists.
 */

/** Embed is the only task with an embedding contract; everything else is chat work. */
internal fun LlmTask.requiredCapability(): LlmCapability =
    if (this == LlmTask.EMBED) LlmCapability.EMBEDDING else LlmCapability.CHAT

internal enum class LlmCapability { CHAT, EMBEDDING, BOTH }

/**
 * The catalogue flag for one capability. Null means the provider did not
 * say; labelled as unknown, never inferred from the model's name.
 */
internal fun LlmDiscoveredModel.capability(capability: LlmCapability): Boolean? =
    if (capability == LlmCapability.CHAT) chat else embedding

/**
 * A task context hides only what the provider says cannot do the work;
 * unknown capability stays visible and gets a "capabilities unknown" label.
 */
internal fun LlmDiscoveredModel.compatibleWith(task: LlmTask?): Boolean =
    task == null || capability(task.requiredCapability()) != false &&
        (task != LlmTask.EMBED || (dimensions ?: 0) <= 1024 ||
            supportsCustomDimensions != false || truncatable == true)

/** The editable selection fields; numeric values stay strings while typed. */
internal data class LlmModelReview(
    val name: String = "",
    val input: String = "",
    val output: String = "",
    val embedding: String = "",
    val dimensions: String = "",
    val truncatable: Boolean = false,
    val supportsCustomDimensions: Boolean = false,
    val provider: String = "",
)

/**
 * Seeds the selection from a catalogue entry. Published fields prefill;
 * only a model known to be chat-only may default embedding price and geometry
 * to zero, and only a known embedding-only model may default chat prices to
 * zero. Everything else stays blank so a missing applicable price or
 * embedding geometry demands explicit input: unknown is never free.
 */
internal fun LlmDiscoveredModel.reviewPrefill(
    provider: String = "",
    capability: LlmCapability = LlmCapability.BOTH,
): LlmModelReview {
    val chatOnly = chat == true && embedding == false
    val embeddingOnly = embedding == true && chat == false
    return LlmModelReview(
        name = id,
        input = inputPer1m ?: (if (embeddingOnly) "0" else ""),
        output = outputPer1m ?: (if (embeddingOnly) "0" else ""),
        embedding = embeddingPer1m ?: (if (chatOnly) "0" else ""),
        dimensions = dimensions?.toString() ?: (if (chatOnly) "0" else ""),
        truncatable = truncatable ?: false,
        supportsCustomDimensions = supportsCustomDimensions ?: false,
        provider = provider,
    )
}

/**
 * The configured model a passing selection produces. A chat selection
 * zeroes the embedding geometry and rate, which do not apply to chat work;
 * an embedding selection zeroes the chat rates. Applicable text is kept
 * verbatim: validation already ran, and an unknown price is never free.
 */
internal fun LlmModelReview.toConfiguredModel(capability: LlmCapability): LlmModel = LlmModel(
    name = name.trim(),
    inputPer1m = if (capability == LlmCapability.EMBEDDING) "0" else input.trim(),
    outputPer1m = if (capability == LlmCapability.EMBEDDING) "0" else output.trim(),
    embeddingPer1m = if (capability == LlmCapability.CHAT) "0" else embedding.trim(),
    dimensions = if (capability == LlmCapability.CHAT) 0 else dimensions.trim().toLong(),
    truncatable = if (capability == LlmCapability.CHAT) false else truncatable,
    provider = provider.trim(),
    supportsCustomDimensions = if (capability == LlmCapability.CHAT) false else supportsCustomDimensions,
)

private const val MAX_MODEL_NAME_CHARS = 256
private const val MAX_UNTRUNCATABLE_DIMENSIONS = 1024L
private const val MAX_DIMENSIONS = 65536L

/**
 * The configured-model rules applied to one selection, plus the geometry a
 * known embedding model needs. [taken] holds the configured identities and
 * the sibling selection names, so a row that is already in the table cannot
 * be added twice: prices are shared by provider and name, and the existing
 * entry keeps its prices. Blank embedding dimensions are a missing value
 * with their own message, never a malformed integer.
 */
internal fun LlmDiscoveredModel.reviewProblems(
    review: LlmModelReview,
    taken: Set<Pair<String, String>>,
    capability: LlmCapability = LlmCapability.CHAT,
): List<String> = modelSettingsProblems(
    name = review.name,
    input = review.input,
    output = review.output,
    embedding = review.embedding,
    dimensions = review.dimensions,
    truncatable = review.truncatable,
    supportsCustomDimensions = review.supportsCustomDimensions,
    provider = review.provider,
    knownEmbedding = embedding,
    taken = taken,
    capability = capability,
)

/**
 * The field rules shared by catalogue selection and manual model editing.
 * [knownEmbedding] is the provider's own capability claim, when it made one.
 */
internal fun modelSettingsProblems(
    name: String,
    input: String,
    output: String,
    embedding: String,
    dimensions: String,
    truncatable: Boolean,
    supportsCustomDimensions: Boolean,
    provider: String,
    knownEmbedding: Boolean?,
    taken: Set<Pair<String, String>>,
    capability: LlmCapability = LlmCapability.CHAT,
    requireAllPrices: Boolean = false,
): List<String> {
    val trimmed = name.trim()
    val label = if (trimmed.isEmpty()) "A model" else "Model $trimmed"
    val problems = mutableListOf<String>()
    if (trimmed.isEmpty()) {
        problems += "Every model needs a name."
    } else {
        if (trimmed.length > MAX_MODEL_NAME_CHARS) problems += "$label name is at most $MAX_MODEL_NAME_CHARS characters."
        if (trimmed.any { it.isWhitespace() || it.isISOControl() }) {
            problems += "$label name must not contain whitespace or control characters."
        }
        if (provider.trim() to trimmed in taken) problems += "$label is already configured; its saved prices are kept."
    }
    val applicable = when {
        capability == LlmCapability.CHAT && !requireAllPrices -> listOf("input" to input, "output" to output)
        capability == LlmCapability.EMBEDDING && !requireAllPrices -> listOf("embedding" to embedding)
        else -> listOf("input" to input, "output" to output, "embedding" to embedding)
    }
    for ((field, value) in applicable) {
        if (!validLlmMoney(value)) {
            problems += "$label: $field price must be a non-negative amount with at most six decimals."
        }
    }
    if (capability != LlmCapability.CHAT) {
        val text = dimensions.trim()
        val parsed = text.toLongOrNull()
        if (parsed == null) {
            problems += if (text.isEmpty()) {
                "$label: embedding dimensions are missing."
            } else {
                "$label: dimensions must be a whole number."
            }
        } else {
            if (parsed > MAX_DIMENSIONS) problems += "$label: dimensions are at most $MAX_DIMENSIONS."
            if (parsed > MAX_UNTRUNCATABLE_DIMENSIONS && !truncatable && !supportsCustomDimensions) {
                problems += "$label: dimensions above $MAX_UNTRUNCATABLE_DIMENSIONS need truncatable vectors or a provider that accepts a custom width."
            }
            // The provider saying a model can embed means its geometry must
            // be real, not a placeholder.
            if (parsed < 1) problems += "$label: embedding dimensions must be greater than zero."
        }
    }
    return problems
}

/**
 * Appends selected models to the configured table. An identity that is
 * already configured keeps its existing entry; prices are shared by provider
 * and name; and the result never grows past the server's table limit.
 */
internal fun importReviewedModels(models: List<LlmModel>, imports: List<LlmModel>): List<LlmModel> {
    val known = models.mapTo(HashSet()) { it.identity() }
    val fresh = imports.filter { known.add(it.identity()) }
    val room = (LLM_MAX_CONFIGURED_MODELS - models.size).coerceAtLeast(0)
    return if (fresh.size <= room) models + fresh else models
}

/** Substring catalogue search over the model ID and display name. */
internal fun LlmDiscoveredModel.matchesSearch(needle: String): Boolean =
    needle.isEmpty() || id.contains(needle, ignoreCase = true) || name.contains(needle, ignoreCase = true)

/** A stable identity label for the selection pane and model table. */
internal fun LlmModel.identityLabel(): String = if (provider.isEmpty()) name else "$provider/$name"

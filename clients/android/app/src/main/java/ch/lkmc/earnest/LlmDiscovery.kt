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

internal enum class LlmCapability { CHAT, EMBEDDING }

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
    task == null || capability(task.requiredCapability()) != false

/** The editable review fields; numeric values stay strings while typed. */
internal data class LlmModelReview(
    val name: String,
    val input: String,
    val output: String,
    val embedding: String,
    val dimensions: String,
    val truncatable: Boolean,
)

/**
 * Seeds the price review. Catalogue fields prefill when the provider
 * published them; only a model known to be chat-only may default embedding
 * price and geometry to zero, and only a known embedding-only model may
 * default chat prices to zero. Everything else stays blank so a missing
 * applicable price or embedding geometry demands explicit input: unknown is
 * never free.
 */
internal fun LlmDiscoveredModel.reviewPrefill(): LlmModelReview {
    val chatOnly = chat == true && embedding == false
    val embeddingOnly = embedding == true && chat == false
    return LlmModelReview(
        name = id,
        input = inputPer1m ?: (if (embeddingOnly) "0" else ""),
        output = outputPer1m ?: (if (embeddingOnly) "0" else ""),
        embedding = embeddingPer1m ?: (if (chatOnly) "0" else ""),
        dimensions = dimensions?.toString() ?: (if (chatOnly) "0" else ""),
        truncatable = truncatable ?: false,
    )
}

/**
 * The configured model a passing review produces. The name is the catalogue
 * ID unless the review renamed it; prices and geometry are the reviewed
 * text verbatim (validation already ran, including the defaults above).
 */
internal fun LlmModelReview.toConfiguredModel(): LlmModel = LlmModel(
    name = name.trim(),
    inputPer1m = input.trim(),
    outputPer1m = output.trim(),
    embeddingPer1m = embedding.trim(),
    dimensions = dimensions.trim().toLong(),
    truncatable = truncatable,
)

private const val MAX_MODEL_NAME_CHARS = 256
private const val MAX_UNTRUNCATABLE_DIMENSIONS = 1024L
private const val MAX_DIMENSIONS = 65536L

/**
 * The configured-model rules applied to an import, plus the geometry a
 * known embedding model needs. [taken] holds the configured names and the
 * sibling review names, so an ID that is already in the table cannot be
 * imported twice: prices are shared by model ID, and the existing entry
 * keeps its prices.
 */
internal fun LlmDiscoveredModel.reviewProblems(review: LlmModelReview, taken: Set<String>): List<String> {
    val name = review.name.trim()
    val label = if (name.isEmpty()) "A model" else "Model $name"
    val problems = mutableListOf<String>()
    if (name.isEmpty()) {
        problems += "Every model needs a name."
    } else {
        if (name.length > MAX_MODEL_NAME_CHARS) problems += "$label name is at most $MAX_MODEL_NAME_CHARS characters."
        if (name.any { it.isWhitespace() || it.isISOControl() }) {
            problems += "$label name must not contain whitespace or control characters."
        }
        if (name in taken) problems += "$label name is already used; model names must be unique."
    }
    for ((field, value) in listOf("input" to review.input, "output" to review.output, "embedding" to review.embedding)) {
        if (!validLlmMoney(value)) {
            problems += "$label: $field price must be a non-negative amount with at most six decimals."
        }
    }
    val parsed = review.dimensions.trim().toLongOrNull()
    if (parsed == null || parsed < 0) {
        problems += "$label: dimensions must be a whole number."
    } else {
        if (parsed > MAX_DIMENSIONS) problems += "$label: dimensions are at most $MAX_DIMENSIONS."
        if (parsed > MAX_UNTRUNCATABLE_DIMENSIONS && !review.truncatable) {
            problems += "$label: dimensions above $MAX_UNTRUNCATABLE_DIMENSIONS need truncatable vectors."
        }
        // The provider saying a model can embed means its geometry must be
        // real, not a placeholder.
        if (embedding == true && parsed < 1) problems += "$label: dimensions are required for a model the provider says can embed."
    }
    return problems
}

/**
 * Appends reviewed imports to the configured table. An ID that is already
 * configured keeps its existing entry; prices are shared by model ID; and
 * the result never grows past the server's table limit.
 */
internal fun importReviewedModels(models: List<LlmModel>, imports: List<LlmModel>): List<LlmModel> {
    val known = models.mapTo(HashSet()) { it.name }
    val fresh = imports.filter { known.add(it.name) }
    val room = (LLM_MAX_CONFIGURED_MODELS - models.size).coerceAtLeast(0)
    return if (fresh.size <= room) models + fresh else models
}

/** Substring catalogue search over the model ID and display name. */
internal fun LlmDiscoveredModel.matchesSearch(needle: String): Boolean =
    needle.isEmpty() || id.contains(needle, ignoreCase = true) || name.contains(needle, ignoreCase = true)

/** Importing prices alone must never assign a task. */
internal enum class LlmImportIntent {
    IMPORT_ONLY, USE_FOR_TASK;

    fun routedModel(imports: List<LlmModel>): String? =
        if (this == USE_FOR_TASK) imports.singleOrNull()?.name else null
}

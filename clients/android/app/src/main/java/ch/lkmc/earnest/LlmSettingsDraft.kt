package ch.lkmc.earnest

import ch.lkmc.earnest.core.LlmModel
import ch.lkmc.earnest.core.LlmProvider
import ch.lkmc.earnest.core.LlmProviderInput
import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmSettings
import ch.lkmc.earnest.core.LlmSettingsInput
import ch.lkmc.earnest.core.LlmTask
import ch.lkmc.earnest.core.LlmTaskRoute
import ch.lkmc.earnest.core.LlmTokenField

/**
 * The editable working copy of the server's LLM services settings.
 *
 * [LlmProviderDraft.apiKeyInput] is the only place a typed key exists. It is
 * deliberately absent from every wire model, cache and persistence path:
 * blank means "keep the stored key", and the draft is dropped on a successful
 * save, on Reload, on sign-out and on identity change. It must never be
 * stored with `rememberSaveable` or written to preferences or Room.
 */
data class LlmProviderDraft(
    val name: String,
    val kind: LlmProviderKind,
    val baseUrl: String,
    val apiKeyInput: String = "",
    val clearApiKey: Boolean = false,
    val monthlyLimitUsd: String = "0",
    val timeoutSeconds: String = "60",
    val tokenField: LlmTokenField = LlmTokenField.MAX_COMPLETION_TOKENS,
    val hasApiKey: Boolean = false,
)

data class LlmSettingsDraft(
    val revision: Long,
    val providers: List<LlmProviderDraft> = emptyList(),
    val tasks: List<LlmTaskRoute> = emptyList(),
    val models: List<LlmModel> = emptyList(),
)

/** The draft's revision is the loaded revision it was edited on. */
fun LlmSettings.toDraft(): LlmSettingsDraft = LlmSettingsDraft(
    revision = revision,
    providers = providers.map { it.toDraft() },
    // The server always enumerates every task; filling missing ones as
    // disabled keeps a save valid and keeps unknown future tasks visible.
    tasks = LlmTask.entries.map { task ->
        tasks.firstOrNull { it.task == task.wireName } ?: LlmTaskRoute(task.wireName)
    } + tasks.filter { LlmTask.fromWire(it.task) == null },
    models = models,
)

fun LlmProvider.toDraft(): LlmProviderDraft = LlmProviderDraft(
    name = name,
    kind = kind,
    baseUrl = baseUrl,
    hasApiKey = hasApiKey,
    monthlyLimitUsd = monthlyLimitUsd,
    timeoutSeconds = timeoutSeconds.toString(),
    tokenField = tokenField,
)

fun LlmSettingsDraft.toInput(opId: String): LlmSettingsInput = LlmSettingsInput(
    revision = revision,
    opId = opId,
    providers = providers.map { it.toInput() },
    tasks = tasks,
    models = models,
)

/**
 * A blank [apiKeyInput] omits `api_key` so the server keeps the stored key;
 * [clearApiKey] sends the explicit removal. The typed key is sent verbatim,
 * never trimmed, and a malformed timeout fails before any request.
 */
fun LlmProviderDraft.toInput(): LlmProviderInput = LlmProviderInput(
    name = name.trim(),
    kind = kind,
    baseUrl = baseUrl.trim(),
    apiKey = if (clearApiKey || apiKeyInput.isEmpty()) null else apiKeyInput,
    clearApiKey = clearApiKey,
    monthlyLimitUsd = monthlyLimitUsd.trim(),
    timeoutSeconds = timeoutSeconds.trim().toLongOrNull()
        ?: throw IllegalArgumentException("timeout_seconds must be a whole number"),
    tokenField = tokenField,
)

/** The contract's default base URLs for a newly added provider. */
fun defaultProviderBaseUrl(kind: LlmProviderKind): String = when (kind) {
    LlmProviderKind.OPENAI -> "https://api.openai.com/v1"
    LlmProviderKind.ANTHROPIC -> "https://api.anthropic.com"
    LlmProviderKind.GEMINI -> "https://generativelanguage.googleapis.com/v1beta"
}

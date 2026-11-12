package ch.lkmc.earnest

import java.security.MessageDigest

import ch.lkmc.earnest.core.LlmDefaultRoute
import ch.lkmc.earnest.core.LlmModel
import ch.lkmc.earnest.core.LlmProvider
import ch.lkmc.earnest.core.LlmProviderInput
import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmSettings
import ch.lkmc.earnest.core.LlmSettingsDefaults
import ch.lkmc.earnest.core.LlmSettingsInput
import ch.lkmc.earnest.core.LlmTask
import ch.lkmc.earnest.core.LlmTaskMode
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
    // Retain the task reference while the editable Name field is empty.
    internal val routedName: String = name,
)

data class LlmSettingsDraft(
    val revision: Long,
    val providers: List<LlmProviderDraft> = emptyList(),
    val tasks: List<LlmTaskRoute> = emptyList(),
    val models: List<LlmModel> = emptyList(),
    val defaults: LlmSettingsDefaults = LlmSettingsDefaults(),
)

/** The draft's revision is the loaded revision it was edited on. */
fun LlmSettings.toDraft(): LlmSettingsDraft {
    // A configuration saved before defaults existed keeps every route as
    // custom or off; an explicit mode is only honoured when defaults exist.
    val hasDefaults = defaults != null
    return LlmSettingsDraft(
        revision = revision,
        providers = providers.map { it.toDraft() },
        // The server always enumerates every task; filling missing ones as
        // disabled keeps a save valid and keeps unknown future tasks visible.
        tasks = LlmTask.entries.map { task ->
            val loaded = tasks.firstOrNull { it.task == task.wireName }
            LlmTaskRoute(
                task = task.wireName,
                provider = loaded?.provider.orEmpty(),
                model = loaded?.model.orEmpty(),
                mode = if (hasDefaults && loaded?.mode != null) loaded.mode else loaded?.resolvedMode() ?: LlmTaskMode.OFF,
            )
        } + tasks.filter { LlmTask.fromWire(it.task) == null },
        models = models,
        defaults = defaults ?: LlmSettingsDefaults(),
    )
}

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
    tasks = tasks.map { it.forSave(defaults) },
    models = models,
    // Empty defaults are omitted so a configuration saved before defaults
    // existed keeps the exact same document shape.
    defaults = if (defaults.isEmpty) null else defaults,
)

/**
 * The wire form of one route. Off sends no route. Default resolves through
 * the matching default and also sends the effective provider and model so an
 * older server without modes still routes it. Custom keeps the exact route.
 * A missing mode keeps its legacy meaning.
 */
internal fun LlmTaskRoute.forSave(defaults: LlmSettingsDefaults): LlmTaskRoute {
    val task = LlmTask.fromWire(task) ?: return copy(mode = resolvedMode())
    return when (resolvedMode()) {
        LlmTaskMode.OFF -> LlmTaskRoute(task.wireName, "", "", LlmTaskMode.OFF)
        LlmTaskMode.DEFAULT -> {
            // The effective route is resolved from the defaults and sent too,
            // so an older server without modes still routes it. An empty
            // default stays empty: it must not silently become a custom route.
            val effective = defaults.routeFor(task)
            LlmTaskRoute(task.wireName, effective.provider, effective.model, LlmTaskMode.DEFAULT)
        }
        LlmTaskMode.CUSTOM -> copy(mode = LlmTaskMode.CUSTOM)
    }
}

/** The effective route a task resolves to, or null when it is disabled. */
internal fun LlmTaskRoute.effectiveRoute(defaults: LlmSettingsDefaults): LlmDefaultRoute? {
    val task = LlmTask.fromWire(task) ?: return null
    return when (resolvedMode()) {
        LlmTaskMode.OFF -> null
        LlmTaskMode.DEFAULT -> defaults.routeFor(task).takeIf { it.provider.isNotEmpty() }
        LlmTaskMode.CUSTOM -> LlmDefaultRoute(provider, model).takeIf { it.provider.isNotEmpty() }
    }
}

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

/**
 * The four states the provider card's API-key badge distinguishes. The badge
 * summarizes the draft, not the server: a typed key is only "entered" until
 * Save, and the clear flag schedules a removal.
 */
enum class LlmApiKeyStatus {
    SAVED,
    ENTERED_UNSAVED,
    REMOVAL_PENDING,
    NONE,
}

/** The badge reads the saved flag, the trimmed draft key and the clear flag. */
fun LlmProviderDraft.apiKeyStatus(): LlmApiKeyStatus = when {
    apiKeyInput.trim().isNotEmpty() -> LlmApiKeyStatus.ENTERED_UNSAVED
    clearApiKey -> LlmApiKeyStatus.REMOVAL_PENDING
    hasApiKey -> LlmApiKeyStatus.SAVED
    else -> LlmApiKeyStatus.NONE
}

fun LlmApiKeyStatus.label(): String = when (this) {
    LlmApiKeyStatus.SAVED -> "API key saved"
    LlmApiKeyStatus.ENTERED_UNSAVED -> "API key entered · unsaved"
    LlmApiKeyStatus.REMOVAL_PENDING -> "Key removal pending"
    LlmApiKeyStatus.NONE -> "No API key"
}

/**
 * The identity of the provider draft a catalogue reply belongs to, so stale
 * results can be dropped: any edit, including an unsaved key change,
 * produces a different fingerprint. The typed key participates only through
 * a SHA-256 digest, avoiding the easy collisions of String.hashCode without
 * retaining the key itself. The value never leaves memory.
 */
internal fun LlmProviderDraft.discoveryFingerprint(): String = listOf(
    name,
    kind.name,
    baseUrl,
    monthlyLimitUsd,
    timeoutSeconds,
    tokenField.name,
    hasApiKey.toString(),
    clearApiKey.toString(),
    MessageDigest.getInstance("SHA-256").digest(apiKeyInput.toByteArray(Charsets.UTF_8))
        .joinToString("") { "%02x".format(it) },
).joinToString("\u0001")

/** Applies a provider edit and keeps the routes bound to its name. */
internal fun LlmSettingsDraft.editProvider(index: Int, replacement: LlmProviderDraft): LlmSettingsDraft {
    val previous = providers.getOrNull(index) ?: return this
    val edited = replacement.copy(routedName = replacement.name.ifEmpty { previous.routedName })
    // An empty name is a transient edit: the retained routed name keeps the
    // scoped models, defaults and routes bound until a real name is typed.
    return copy(
        providers = providers.mapIndexed { i, provider -> if (i == index) edited else provider },
        tasks = retargetProvider(tasks, previous.routedName, edited.routedName),
        models = models.map { if (it.provider == previous.routedName) it.copy(provider = edited.routedName) else it },
        defaults = retargetDefaults(defaults, previous.routedName, edited.routedName),
    )
}

internal fun LlmSettingsDraft.withDefaultModel(key: String, model: LlmModel): LlmSettingsDraft {
    val route = LlmDefaultRoute(model.provider, model.name)
    return copy(defaults = if (key == "embedding") defaults.copy(embedding = route) else defaults.copy(text = route))
}

/** Remove dependent routes together, so the remaining draft can still be saved. */
internal fun LlmSettingsDraft.withoutProvider(index: Int): LlmSettingsDraft {
    val name = providers.getOrNull(index)?.routedName ?: return this
    fun keep(route: LlmDefaultRoute) = if (route.provider == name) LlmDefaultRoute() else route
    return copy(
        providers = providers.filterIndexed { i, _ -> i != index },
        models = models.filterNot { it.provider == name },
        defaults = LlmSettingsDefaults(keep(defaults.text), keep(defaults.embedding)),
        tasks = tasks.map {
            if (it.provider == name || it.effectiveRoute(defaults)?.provider == name) {
                LlmTaskRoute(it.task, "", "", LlmTaskMode.OFF)
            } else it
        },
    )
}

/** Resolve scoped metadata before falling back to a legacy shared entry. */
internal fun LlmSettingsDraft.withoutModel(target: LlmModel): LlmSettingsDraft {
    fun uses(route: LlmDefaultRoute?): Boolean {
        if (route == null || route.model != target.name) return false
        val found = models.firstOrNull { it.provider == route.provider && it.name == route.model }
            ?: models.firstOrNull { it.provider.isEmpty() && it.name == route.model }
        return found?.identity() == target.identity()
    }
    return copy(
        models = models.filterNot { it.identity() == target.identity() },
        defaults = LlmSettingsDefaults(
            if (uses(defaults.text)) LlmDefaultRoute() else defaults.text,
            if (uses(defaults.embedding)) LlmDefaultRoute() else defaults.embedding,
        ),
        tasks = tasks.map {
            if (uses(it.effectiveRoute(defaults))) LlmTaskRoute(it.task, "", "", LlmTaskMode.OFF) else it
        },
    )
}

/** Points the default routes at a renamed provider. */
private fun retargetDefaults(defaults: LlmSettingsDefaults, old: String, replacement: String): LlmSettingsDefaults {
    if (old.isEmpty() || replacement.isEmpty()) return defaults
    fun fix(route: LlmDefaultRoute): LlmDefaultRoute =
        if (route.provider == old) route.copy(provider = replacement) else route
    return LlmSettingsDefaults(fix(defaults.text), fix(defaults.embedding))
}

/** Points assigned tasks at a renamed provider without claiming disabled routes. */
internal fun retargetProvider(tasks: List<LlmTaskRoute>, old: String, replacement: String): List<LlmTaskRoute> {
    // An empty name is a transient edit, not a provider matching disabled tasks.
    if (old.isEmpty() || replacement.isEmpty()) return tasks
    return tasks.map { if (it.provider == old) it.copy(provider = replacement) else it }
}

/** The contract's default base URLs for a newly added provider. */
fun defaultProviderBaseUrl(kind: LlmProviderKind): String = when (kind) {
    LlmProviderKind.OPENAI -> "https://api.openai.com/v1"
    LlmProviderKind.ANTHROPIC -> "https://api.anthropic.com"
    LlmProviderKind.GEMINI -> "https://generativelanguage.googleapis.com/v1beta"
}

/** A ready-made provider starting point. Custom keeps an editable URL. */
data class LlmProviderPreset(
    val label: String,
    val kind: LlmProviderKind,
    val baseUrl: String,
)

val LLM_PROVIDER_PRESETS = listOf(
    LlmProviderPreset("Venice", LlmProviderKind.OPENAI, "https://api.venice.ai/api/v1"),
    LlmProviderPreset("OpenAI", LlmProviderKind.OPENAI, "https://api.openai.com/v1"),
    LlmProviderPreset("Anthropic", LlmProviderKind.ANTHROPIC, "https://api.anthropic.com"),
    LlmProviderPreset("Gemini", LlmProviderKind.GEMINI, "https://generativelanguage.googleapis.com/v1beta"),
    LlmProviderPreset("Custom", LlmProviderKind.OPENAI, ""),
)

/** The preset's editable name, made unique against the draft's providers. */
fun uniqueProviderName(base: String, existing: Collection<String>): String {
    val slug = base.lowercase().replace(Regex("[^a-z0-9-]+"), "-").trim('-')
    val root = slug.ifEmpty { "provider" }
    var candidate = root
    var suffix = 2
    while (candidate in existing) {
        candidate = "$root-$suffix"
        suffix++
    }
    return candidate
}

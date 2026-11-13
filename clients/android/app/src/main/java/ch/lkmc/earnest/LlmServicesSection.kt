package ch.lkmc.earnest

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import ch.lkmc.earnest.core.LLM_MAX_CONFIGURED_MODELS
import ch.lkmc.earnest.core.LlmDiscoveredModel
import ch.lkmc.earnest.core.LlmModel
import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmTask
import ch.lkmc.earnest.core.LlmTaskRoute
import ch.lkmc.earnest.core.LlmTokenField

/**
 * The server-managed LLM services section. Every field edits an in-memory
 * draft owned by the view model; only the explicit Save button publishes the
 * whole configuration. Typed API keys live only in that draft, are masked,
 * and are never written to preferences, Room, saved instance state or logs.
 */
@Composable
internal fun LlmServicesSection(
    model: AppViewModel,
    state: UiState,
    modifier: Modifier = Modifier,
) {
    val ui = state.llmSettingsUi
    val draft = ui.draft ?: ui.loaded?.toDraft()
    var addProvider by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }
    var routingTask by remember(state.accountKey, ui.epoch) { mutableStateOf<String?>(null) }
    var routingProvider by remember(state.accountKey, ui.epoch) { mutableStateOf("") }
    var routingModel by remember(state.accountKey, ui.epoch) { mutableStateOf("") }
    var editingModel by remember(state.accountKey, ui.epoch) { mutableStateOf<String?>(null) }
    var addingModel by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }
    var modelsExpanded by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }
    // Which catalogue browser is open: beside provider[index], and whether it
    // serves a task's routing. Results live in the view model keyed to the
    // provider draft's fingerprint.
    var discoveryTarget by remember(state.accountKey, ui.epoch) { mutableStateOf<Pair<Int, LlmTask?>?>(null) }

    val current = draft
    Column(modifier.fillMaxWidth()) {
        Text("LLM services", style = MaterialTheme.typography.titleMedium)
        Text(
            "These settings are shared for the whole server: changing them affects all accounts. " +
                "API keys are stored encrypted on the server and are never returned to this app. " +
                "Local model servers need EARNEST_OUTBOUND_ALLOW_PRIVATE allowlisting on the server.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Row(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TextButton(onClick = model::loadLlmSettings, enabled = !ui.loading && !ui.saving) { Text("Load") }
            TextButton(onClick = model::reloadLlmSettings, enabled = !ui.loading && !ui.saving) { Text("Reload") }
            TextButton(
                onClick = model::saveLlmSettings,
                enabled = draft != null && !ui.loading && !ui.saving && !ui.conflict,
            ) { Text("Save") }
            TextButton(
                onClick = model::retryLlmSettingsSave,
                enabled = ui.frozen != null && !ui.loading && !ui.saving && !ui.conflict,
            ) { Text("Retry") }
            if (ui.loading || ui.saving) CircularProgressIndicator(Modifier.width(20.dp))
        }
        ui.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        if (ui.success) Text("Saved", color = MaterialTheme.colorScheme.primary)
        if (ui.conflict) {
            Text(
                "The saved settings changed on the server. Reload them to review; your draft is kept.",
                color = MaterialTheme.colorScheme.error,
            )
        }
        if (current == null) {
            Text("Load the LLM services settings to review them.")
            return@Column
        }

        Text("Providers", style = MaterialTheme.typography.titleSmall)
        current.providers.forEachIndexed { index, provider ->
            ProviderCard(
                provider = provider,
                referencedBy = current.tasks.filter { it.provider == provider.routedName && it.provider.isNotEmpty() }
                    .map { it.task },
                onTransform = { transformed ->
                    model.updateLlmSettingsDraft { it.editProvider(index, transformed) }
                },
                onRemove = {
                    model.updateLlmSettingsDraft { draftCopy ->
                        draftCopy.copy(providers = draftCopy.providers.filterIndexed { i, _ -> i != index })
                    }
                },
                onFetchModels = {
                    val target: Pair<Int, LlmTask?> = index to null
                    if (discoveryTarget == target) {
                        discoveryTarget = null
                        model.dismissLlmDiscovery()
                    } else {
                        discoveryTarget = target
                        model.fetchLlmModels(index)
                    }
                },
            )
        }
        TextButton(
            onClick = { addProvider = true },
            enabled = !ui.saving,
        ) { Text("Add provider") }

        Text("Task routing", style = MaterialTheme.typography.titleSmall)
        Text(
            "Both provider and model must be set to enable a task; clearing the provider disables it.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        current.tasks.forEach { route ->
            val label = LlmTask.fromWire(route.task)?.label() ?: route.task
            Row(
                Modifier.fillMaxWidth().padding(vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f)) {
                    Text(label, style = MaterialTheme.typography.bodyMedium)
                    Text(
                        if (route.provider.isEmpty()) "Disabled" else "${route.provider} / ${route.model}",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                TextButton(
                    onClick = {
                        routingProvider = route.provider
                        routingModel = route.model
                        routingTask = route.task
                    },
                ) { Text("Route") }
            }
        }

        Spacer(Modifier.height(8.dp))
        Row(
            Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("Model pricing", style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
            TextButton(onClick = { modelsExpanded = !modelsExpanded }) { Text(if (modelsExpanded) "Hide" else "Edit") }
        }
        if (modelsExpanded) {
            Text(
                "Prices are USD per 1M tokens; set explicit zero prices for local models. " +
                    "Embedding dimensions are 0-1024, or up to 65536 when truncatable.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            current.models.forEach { target ->
                Row(
                    Modifier.fillMaxWidth().padding(vertical = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(target.name, style = MaterialTheme.typography.bodyMedium)
                        Text(
                            "in ${target.inputPer1m} / out ${target.outputPer1m} / embed ${target.embeddingPer1m} per 1M, " +
                                "${target.dimensions} dimensions" + if (target.truncatable) " (truncatable)" else "",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    TextButton(onClick = { editingModel = target.name }) { Text("Edit") }
                    TextButton(
                        onClick = {
                            model.updateLlmSettingsDraft { draftCopy ->
                                draftCopy.copy(models = draftCopy.models.filterNot { it.name == target.name })
                            }
                        },
                        enabled = current.tasks.none { it.model == target.name && it.model.isNotEmpty() },
                    ) { Text("Remove") }
                }
            }
            TextButton(onClick = { addingModel = true }, enabled = !ui.saving) { Text("Add model") }
        }
    }

    if (current != null) {
        if (addProvider) {
            AddProviderDialog(
                onDismiss = { addProvider = false },
                onConfirm = { name, kind ->
                    addProvider = false
                    model.updateLlmSettingsDraft { draftCopy ->
                        draftCopy.copy(
                            providers = draftCopy.providers + LlmProviderDraft(name, kind, defaultProviderBaseUrl(kind)),
                        )
                    }
                },
            )
        }

        routingTask?.let { taskName ->
            val task = LlmTask.fromWire(taskName)
            RouteTaskDialog(
                taskName = taskName,
                draft = current,
                provider = routingProvider,
                model = routingModel,
                onProviderChange = { routingProvider = it },
                onModelChange = { routingModel = it },
                onFetchModels = task?.let { activeTask ->
                    current.providers.indexOfFirst { it.name == routingProvider }
                        .takeIf { it >= 0 }
                        ?.let { index ->
                            {
                                discoveryTarget = index to activeTask
                                model.fetchLlmModels(index)
                            }
                        }
                },
                onDismiss = { routingTask = null },
                onApply = {
                    routingTask = null
                    model.updateLlmSettingsDraft { draftCopy ->
                        draftCopy.copy(
                            tasks = draftCopy.tasks.map { route ->
                                if (route.task == taskName) LlmTaskRoute(taskName, routingProvider, routingModel) else route
                            },
                        )
                    }
                },
            )
        }

        val editingTarget = editingModel?.let { name -> current.models.firstOrNull { it.name == name } }
        editingTarget?.let { target ->
            ModelDialog(
                title = "Edit model",
                model = target,
                onDismiss = { editingModel = null },
                onApply = { saved ->
                    editingModel = null
                    model.updateLlmSettingsDraft { draftCopy ->
                        draftCopy.copy(
                            models = draftCopy.models.map { if (it.name == target.name) saved else it },
                            tasks = if (saved.name != target.name) retargetModel(draftCopy.tasks, target.name, saved.name) else draftCopy.tasks,
                        )
                    }
                },
            )
        }

        if (addingModel) {
            ModelDialog(
                title = "Add model",
                model = LlmModel(name = ""),
                onDismiss = { addingModel = false },
                onApply = { saved ->
                    addingModel = false
                    model.updateLlmSettingsDraft { draftCopy ->
                        draftCopy.copy(models = draftCopy.models + saved)
                    }
                },
            )
        }

        discoveryTarget?.let { target ->
            val (providerIndex, task) = target
            val provider = current.providers.getOrNull(providerIndex)
            if (provider == null) {
                // The provider was removed while its catalogue was open.
                SideEffect { discoveryTarget = null }
            } else {
                // Results stay visible only while they belong to this
                // provider's current draft; an edit drops them.
                val discovery = ui.discovery?.takeIf { entry ->
                    entry.providerIndex == providerIndex &&
                        provider.discoveryFingerprint() == entry.providerKey
                }
                DiscoveryDialog(
                    providerName = provider.name.ifBlank { "the provider" },
                    task = task,
                    discovery = discovery,
                    configured = current.models.mapTo(HashSet()) { it.name },
                    room = (LLM_MAX_CONFIGURED_MODELS - current.models.size).coerceAtLeast(0),
                    onFetch = { model.fetchLlmModels(providerIndex) },
                    onPick = task?.let {
                        {
                            routingProvider = provider.name
                            routingModel = it
                            discoveryTarget = null
                            model.dismissLlmDiscovery()
                        }
                    },
                    onImport = { imported ->
                        model.updateLlmSettingsDraft { draftCopy ->
                            draftCopy.copy(models = importReviewedModels(draftCopy.models, imported))
                        }
                    },
                    onDismiss = {
                        discoveryTarget = null
                        model.dismissLlmDiscovery()
                    },
                )
            }
        }
    }
}

/** Points tasks that reference a renamed model at its new name. */
private fun retargetModel(tasks: List<LlmTaskRoute>, old: String, replacement: String): List<LlmTaskRoute> =
    tasks.map { if (it.model == old) it.copy(model = replacement) else it }

@Composable
private fun ProviderCard(
    provider: LlmProviderDraft,
    referencedBy: List<String>,
    onTransform: (LlmProviderDraft) -> Unit,
    onRemove: () -> Unit,
    onFetchModels: () -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(provider.name.ifBlank { "(unnamed provider)" }, style = MaterialTheme.typography.titleSmall)
            Spacer(Modifier.width(8.dp))
            KeyStatusChip(provider.apiKeyStatus())
            Spacer(Modifier.weight(1f))
            TextButton(onClick = onFetchModels) { Text("Fetch models") }
            TextButton(onClick = onRemove, enabled = referencedBy.isEmpty()) { Text("Remove") }
        }
        if (referencedBy.isNotEmpty()) {
            Text(
                "Used by ${referencedBy.joinToString(", ")}; clear those routes first.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        OutlinedTextField(
            value = provider.name,
            onValueChange = { value -> onTransform(provider.copy(name = value)) },
            label = { Text("Name") },
            singleLine = true,
            // A saved key is bound to the provider name on the server, so a
            // keyed provider cannot be renamed silently; remove and re-add it.
            enabled = !provider.hasApiKey,
            supportingText = if (provider.hasApiKey) {
                { Text("A saved key is bound to this name; remove the provider to rename it.") }
            } else {
                null
            },
            modifier = Modifier.fillMaxWidth(),
        )
        Text("Kind", style = MaterialTheme.typography.bodySmall)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            LlmProviderKind.entries.forEach { kind ->
                FilterChip(
                    selected = provider.kind == kind,
                    onClick = { onTransform(provider.copy(kind = kind)) },
                    label = { Text(kind.label()) },
                )
            }
        }
        OutlinedTextField(
            value = provider.baseUrl,
            onValueChange = { value -> onTransform(provider.copy(baseUrl = value)) },
            label = { Text("Base URL") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = provider.monthlyLimitUsd,
            onValueChange = { value -> onTransform(provider.copy(monthlyLimitUsd = value)) },
            label = { Text("Monthly limit (USD, 0 = unlimited)") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = provider.timeoutSeconds,
            onValueChange = { value -> onTransform(provider.copy(timeoutSeconds = value)) },
            label = { Text("Timeout (seconds, 1-600)") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            modifier = Modifier.fillMaxWidth(),
        )
        Text("Token field", style = MaterialTheme.typography.bodySmall)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            LlmTokenField.entries.forEach { field ->
                FilterChip(
                    selected = provider.tokenField == field,
                    onClick = { onTransform(provider.copy(tokenField = field)) },
                    label = { Text(field.label()) },
                )
            }
        }
        OutlinedTextField(
            value = provider.apiKeyInput,
            onValueChange = { value -> onTransform(provider.copy(apiKeyInput = value)) },
            label = { Text(if (provider.hasApiKey) "API key (blank keeps the saved key)" else "API key") },
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            singleLine = true,
            enabled = !provider.clearApiKey,
            supportingText = if (provider.hasApiKey) {
                {
                    Text(
                        when {
                            provider.clearApiKey -> "The saved key will be removed on save."
                            provider.apiKeyInput.isEmpty() -> "A key is saved on the server."
                            else -> "The saved key will be replaced on save."
                        },
                    )
                }
            } else {
                null
            },
            modifier = Modifier.fillMaxWidth(),
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(
                checked = provider.clearApiKey,
                onCheckedChange = { checked ->
                    // Clearing removes any typed replacement too, so a removal
                    // can never be combined with a new key.
                    onTransform(provider.copy(clearApiKey = checked, apiKeyInput = if (checked) "" else provider.apiKeyInput))
                },
                enabled = provider.hasApiKey,
            )
            Text("Remove the saved key")
        }
    }
}

@Composable
private fun AddProviderDialog(onDismiss: () -> Unit, onConfirm: (String, LlmProviderKind) -> Unit) {
    var name by remember { mutableStateOf("") }
    var kind by remember { mutableStateOf(LlmProviderKind.OPENAI) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Add provider") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("Name") },
                    singleLine = true,
                )
                Text("Kind")
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    LlmProviderKind.entries.forEach { option ->
                        FilterChip(
                            selected = kind == option,
                            onClick = { kind = option },
                            label = { Text(option.label()) },
                        )
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = { onConfirm(name.trim(), kind) }, enabled = name.trim().isNotEmpty()) { Text("Add") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun RouteTaskDialog(
    taskName: String,
    draft: LlmSettingsDraft,
    provider: String,
    model: String,
    onProviderChange: (String) -> Unit,
    onModelChange: (String) -> Unit,
    onFetchModels: (() -> Unit)?,
    onDismiss: () -> Unit,
    onApply: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Route ${LlmTask.fromWire(taskName)?.label() ?: taskName}") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Provider")
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    FilterChip(
                        selected = provider.isEmpty(),
                        onClick = { onProviderChange(""); onModelChange("") },
                        label = { Text("Disabled") },
                    )
                    draft.providers.forEach { option ->
                        FilterChip(
                            selected = provider == option.name,
                            onClick = { onProviderChange(option.name) },
                            label = { Text(option.name) },
                        )
                    }
                }
                if (provider.isNotEmpty()) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("Configured / manual models", modifier = Modifier.weight(1f))
                        TextButton(onClick = { onFetchModels?.invoke() }, enabled = onFetchModels != null) {
                            Text("Fetch models")
                        }
                    }
                    Column {
                        draft.models.forEach { option ->
                            FilterChip(
                                selected = model == option.name,
                                onClick = { onModelChange(option.name) },
                                label = { Text(option.name) },
                                modifier = Modifier.padding(vertical = 2.dp),
                            )
                        }
                        if (draft.models.isEmpty()) {
                            Text(
                                "No models are configured yet.",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    Text(
                        "Fetching previews the provider in this draft and never saves; " +
                            "only picking a model here changes the route. Apply writes it to the draft; Save persists.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = onApply,
                enabled = (provider.isEmpty() && model.isEmpty()) || (provider.isNotEmpty() && model.isNotEmpty()),
            ) { Text("Apply") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@Composable
private fun ModelDialog(
    title: String,
    model: LlmModel,
    onDismiss: () -> Unit,
    onApply: (LlmModel) -> Unit,
) {
    var name by remember { mutableStateOf(model.name) }
    var input by remember { mutableStateOf(model.inputPer1m) }
    var output by remember { mutableStateOf(model.outputPer1m) }
    var embedding by remember { mutableStateOf(model.embeddingPer1m) }
    var dimensions by remember { mutableStateOf(model.dimensions.toString()) }
    var truncatable by remember { mutableStateOf(model.truncatable) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("Name") },
                    singleLine = true,
                )
                OutlinedTextField(
                    value = input,
                    onValueChange = { input = it },
                    label = { Text("Input per 1M (USD)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                )
                OutlinedTextField(
                    value = output,
                    onValueChange = { output = it },
                    label = { Text("Output per 1M (USD)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                )
                OutlinedTextField(
                    value = embedding,
                    onValueChange = { embedding = it },
                    label = { Text("Embedding per 1M (USD)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                )
                OutlinedTextField(
                    value = dimensions,
                    onValueChange = { dimensions = it },
                    label = { Text("Embedding dimensions (0-1024, or 65536 truncatable)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                )
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(checked = truncatable, onCheckedChange = { truncatable = it })
                    Text("Truncatable")
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    onApply(
                        LlmModel(
                            name = name.trim(),
                            inputPer1m = input.trim(),
                            outputPer1m = output.trim(),
                            embeddingPer1m = embedding.trim(),
                            dimensions = dimensions.trim().toLongOrNull() ?: -1,
                            truncatable = truncatable,
                        ),
                    )
                },
                enabled = name.isNotBlank() && (dimensions.trim().toLongOrNull() ?: -1) >= 0,
            ) { Text("Apply") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

private fun LlmProviderKind.label(): String = when (this) {
    LlmProviderKind.OPENAI -> "OpenAI"
    LlmProviderKind.ANTHROPIC -> "Anthropic"
    LlmProviderKind.GEMINI -> "Gemini"
}

private fun LlmTokenField.label(): String = when (this) {
    LlmTokenField.MAX_COMPLETION_TOKENS -> "max_completion_tokens"
    LlmTokenField.MAX_TOKENS -> "max_tokens (legacy)"
}

private fun LlmTask.label(): String = when (this) {
    LlmTask.EMBED -> "Embed"
    LlmTask.CLASSIFY -> "Classify"
    LlmTask.BLURB -> "Blurb"
    LlmTask.SUMMARY -> "Summary"
    LlmTask.EDITION -> "Edition"
    LlmTask.COMPILE_RULE -> "Compile rule"
    LlmTask.ASK -> "Ask"
}

private fun LlmTask.capabilityLabel(): String = when (this) {
    LlmTask.EMBED -> "embedding"
    else -> "chat"
}

/** A small status chip; the API-key badge and catalogue labels share it. */
@Composable
private fun StatusChip(text: String, container: Color) {
    Surface(color = container, shape = MaterialTheme.shapes.small) {
        Text(
            text,
            Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
            style = MaterialTheme.typography.labelSmall,
        )
    }
}

@Composable
private fun KeyStatusChip(status: LlmApiKeyStatus) {
    val container = when (status) {
        LlmApiKeyStatus.SAVED -> MaterialTheme.colorScheme.primaryContainer
        LlmApiKeyStatus.ENTERED_UNSAVED -> MaterialTheme.colorScheme.tertiaryContainer
        LlmApiKeyStatus.REMOVAL_PENDING -> MaterialTheme.colorScheme.errorContainer
        LlmApiKeyStatus.NONE -> MaterialTheme.colorScheme.surfaceVariant
    }
    StatusChip(status.label(), container)
}

/**
 * The catalogue browser for one provider draft. Results come from the view
 * model already keyed to the provider's fingerprint; they stay in memory and
 * only an explicit selection imports or routes anything. A task context
 * hides only models the provider says cannot do the work, while unknown
 * capabilities stay visible and labelled.
 */
@Composable
private fun DiscoveryDialog(
    providerName: String,
    task: LlmTask?,
    discovery: LlmDiscoveryUi?,
    configured: Set<String>,
    room: Int,
    onFetch: () -> Unit,
    onPick: ((String) -> Unit)?,
    onImport: (List<LlmModel>) -> Unit,
    onDismiss: () -> Unit,
) {
    var query by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf<String?>(null) }
    var roomError by remember { mutableStateOf<String?>(null) }
    // Selection and review are bound to one fetch: a refetch resets them.
    var selected by remember(discovery?.request) { mutableStateOf(setOf<String>()) }
    var reviewIntent by remember(discovery?.request) { mutableStateOf(LlmImportIntent.IMPORT_ONLY) }
    var review by remember(discovery?.request) { mutableStateOf<List<LlmDiscoveredModel>?>(null) }
    val models = discovery?.models.orEmpty()
    val needle = query.trim()
    val searched = models.filter { it.matchesSearch(needle) }
    val hidden = if (task != null) searched.filterNot { it.compatibleWith(task) } else emptyList()
    val shown = if (task != null) searched.filter { it.compatibleWith(task) } else searched

    Dialog(onDismissRequest = onDismiss) {
        Surface(
            shape = MaterialTheme.shapes.extraLarge,
            color = MaterialTheme.colorScheme.surface,
            tonalElevation = 6.dp,
        ) {
            Column(Modifier.padding(24.dp).heightIn(max = 560.dp).fillMaxWidth()) {
                Text(
                    if (task != null) "Model catalogue; ${providerName} for ${task.label()}" else "Model catalogue; $providerName",
                    style = MaterialTheme.typography.titleMedium,
                )
                Text(
                    "Fetching previews the provider details currently in the editor, including an unsaved " +
                        "replacement key. It saves nothing and sends no inference request; results stay in " +
                        "memory and are discarded when the provider changes. Apply writes edits to the draft; " +
                        "Save persists the whole configuration.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Spacer(Modifier.height(8.dp))
                when {
                    discovery == null -> {
                        Text(
                            "Fetch the provider's catalogue to browse it. Configured and manual models stay " +
                                "available when discovery fails or is unsupported.",
                            style = MaterialTheme.typography.bodySmall,
                        )
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            TextButton(onClick = onFetch) { Text("Fetch models") }
                            TextButton(onClick = onDismiss) { Text("Close") }
                        }
                    }
                    discovery.loading -> {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            CircularProgressIndicator(Modifier.width(20.dp))
                            Spacer(Modifier.width(8.dp))
                            Text("Fetching models…", style = MaterialTheme.typography.bodySmall)
                        }
                    }
                    discovery.error != null -> {
                        Text(discovery.error, color = MaterialTheme.colorScheme.error)
                        Text(
                            "The configured and manual models are unchanged and stay available.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            TextButton(onClick = onFetch) { Text("Fetch again") }
                            TextButton(onClick = onDismiss) { Text("Close") }
                        }
                    }
                    else -> {
                        OutlinedTextField(
                            value = query,
                            onValueChange = { query = it },
                            label = { Text("Search catalogue") },
                            placeholder = { Text("Filter by ID or name") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        notice?.let {
                            Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary)
                        }
                        roomError?.let {
                            Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                        }
                        if (shown.isEmpty()) {
                            Text(
                                if (searched.isNotEmpty()) {
                                    "Every matching entry is known to lack ${task?.capabilityLabel() ?: "that"} capability."
                                } else {
                                    "No catalogue entries match the search."
                                },
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        LazyColumn(Modifier.weight(1f, fill = false).fillMaxWidth()) {
                            items(shown, key = { it.id }) { entry ->
                                CatalogueRow(
                                    entry = entry,
                                    task = task,
                                    configured = entry.id in configured,
                                    selected = entry.id in selected,
                                    onSelectedChange = { checked ->
                                        selected = if (checked) selected + entry.id else selected - entry.id
                                        roomError = null
                                        notice = null
                                    },
                                    onPick = onPick?.let { pick ->
                                        {
                                            if (entry.id in configured) {
                                                pick(entry.id)
                                            } else if (room < 1) {
                                                roomError = "The configured-model table is full at $LLM_MAX_CONFIGURED_MODELS entries; remove one before importing."
                                            } else {
                                                reviewIntent = LlmImportIntent.USE_FOR_TASK
                                                review = listOf(entry)
                                            }
                                        }
                                    },
                                )
                            }
                        }
                        if (hidden.isNotEmpty()) {
                            Text(
                                "${hidden.size} catalogue ${if (hidden.size == 1) "entry is" else "entries are"} hidden " +
                                    "because the provider says ${if (hidden.size == 1) "it" else "they"} cannot do ${task?.capabilityLabel()} work.",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        Text(
                            "Only selected entries enter the draft; configured and manual models stay " +
                                "available even when the catalogue fails or omits them.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            TextButton(
                                onClick = {
                                    val picked = models.filter { it.id in selected }
                                    val fresh = picked.filter { it.id !in configured }
                                    when {
                                        fresh.isEmpty() -> {
                                            notice = if (picked.isEmpty()) {
                                                "Select catalogue entries to import."
                                            } else {
                                                "Every selected model is already configured; its existing prices are kept because prices are shared by model ID."
                                            }
                                        }
                                        fresh.size > room -> {
                                            roomError = "The configured-model table holds $LLM_MAX_CONFIGURED_MODELS entries and " +
                                                "$room ${if (room == 1) "slot is" else "slots are"} free. " +
                                                "Deselect ${fresh.size - room} or remove configured models first."
                                        }
                                        else -> {
                                            reviewIntent = LlmImportIntent.IMPORT_ONLY
                                            review = fresh
                                        }
                                    }
                                },
                                enabled = selected.isNotEmpty(),
                            ) { Text("Import selected" + if (selected.isEmpty()) "" else " (${selected.size})") }
                            TextButton(onClick = onFetch) { Text("Fetch again") }
                            TextButton(onClick = onDismiss) { Text("Close") }
                        }
                    }
                }
            }
        }
    }

    review?.let { targets ->
        PriceReviewDialog(
            models = targets,
            taken = configured,
            room = room,
            onApply = { imported ->
                val picked = reviewIntent.routedModel(imported)
                onImport(imported)
                review = null
                selected = emptySet()
                if (picked != null) {
                    onPick?.invoke(picked)
                } else {
                    notice = "${imported.size} ${if (imported.size == 1) "model" else "models"} added to the draft. " +
                        "Review the prices in Model pricing, then Save to persist."
                }
            },
            onDismiss = { review = null },
        )
    }
}

@Composable
private fun CatalogueRow(
    entry: LlmDiscoveredModel,
    task: LlmTask?,
    configured: Boolean,
    selected: Boolean,
    onSelectedChange: (Boolean) -> Unit,
    onPick: (() -> Unit)?,
) {
    Column(Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                if (entry.name.isEmpty() || entry.name == entry.id) entry.id else "${entry.name} (${entry.id})",
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.weight(1f),
            )
            if (configured) StatusChip("configured", MaterialTheme.colorScheme.primaryContainer)
            if (task != null && entry.capability(task.requiredCapability()) == null) {
                Spacer(Modifier.width(4.dp))
                StatusChip("capabilities unknown", MaterialTheme.colorScheme.tertiaryContainer)
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
            if (entry.chat == true) StatusChip("chat", MaterialTheme.colorScheme.surfaceVariant)
            if (entry.embedding == true) StatusChip("embedding", MaterialTheme.colorScheme.surfaceVariant)
            if (task == null && entry.chat == null && entry.embedding == null) {
                StatusChip("capabilities unknown", MaterialTheme.colorScheme.surfaceVariant)
            }
        }
        val details = buildList {
            if (entry.inputPer1m != null || entry.outputPer1m != null) {
                add("in $${entry.inputPer1m ?: "?"} · out $${entry.outputPer1m ?: "?"}")
            }
            entry.embeddingPer1m?.let { add("embed $$it") }
            entry.dimensions?.let { add("$it dimensions") }
            if (entry.truncatable == true) add("truncatable")
        }
        if (details.isNotEmpty()) {
            Text(
                details.joinToString(" · ") + " per 1M tokens",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (configured) {
            Text(
                "Already in the model table; its existing prices are kept because prices are shared by model ID.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(checked = selected, onCheckedChange = onSelectedChange)
            Text("Select", style = MaterialTheme.typography.bodySmall)
            Spacer(Modifier.weight(1f))
            if (onPick != null && task != null) {
                TextButton(onClick = onPick) { Text("Use for ${task.label()}") }
            }
        }
    }
}

/**
 * The gate between the catalogue and the draft. Published fields arrive
 * prefilled; missing applicable prices and embedding geometry must be
 * entered explicitly before the model may be imported.
 */
@Composable
private fun PriceReviewDialog(
    models: List<LlmDiscoveredModel>,
    taken: Set<String>,
    room: Int,
    onApply: (List<LlmModel>) -> Unit,
    onDismiss: () -> Unit,
) {
    var drafts by remember(models) { mutableStateOf(models.map { it.reviewPrefill() }) }
    var problems by remember { mutableStateOf(listOf<String>()) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Review model prices") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    "Published rates are suggestions in USD per million tokens. Missing applicable prices and " +
                        "embedding geometry need explicit input; an unknown price is never treated as free. " +
                        "Prices are shared by model ID, so an ID that is already configured keeps its existing " +
                        "prices instead of these.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                problems.forEach { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
                models.forEachIndexed { index, discovered ->
                    val review = drafts[index]
                    Text(
                        if (discovered.name.isEmpty() || discovered.name == discovered.id) discovered.id else "${discovered.name} (${discovered.id})",
                        style = MaterialTheme.typography.titleSmall,
                    )
                    Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                        if (discovered.chat == true) StatusChip("chat", MaterialTheme.colorScheme.surfaceVariant)
                        if (discovered.embedding == true) StatusChip("embedding", MaterialTheme.colorScheme.surfaceVariant)
                        if (discovered.chat == null && discovered.embedding == null) {
                            StatusChip("capabilities unknown", MaterialTheme.colorScheme.tertiaryContainer)
                        }
                    }
                    OutlinedTextField(
                        value = review.name,
                        onValueChange = { drafts = drafts.mapIndexed { i, d -> if (i == index) d.copy(name = it) else d } },
                        label = { Text("Configured name") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = review.input,
                        onValueChange = { drafts = drafts.mapIndexed { i, d -> if (i == index) d.copy(input = it) else d } },
                        label = { Text("Input per 1M (USD)") },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = review.output,
                        onValueChange = { drafts = drafts.mapIndexed { i, d -> if (i == index) d.copy(output = it) else d } },
                        label = { Text("Output per 1M (USD)") },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = review.embedding,
                        onValueChange = { drafts = drafts.mapIndexed { i, d -> if (i == index) d.copy(embedding = it) else d } },
                        label = { Text("Embedding per 1M (USD)") },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = review.dimensions,
                        onValueChange = { drafts = drafts.mapIndexed { i, d -> if (i == index) d.copy(dimensions = it) else d } },
                        label = { Text("Embedding dimensions (0-1024, or 65536 truncatable)") },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(
                            checked = review.truncatable,
                            onCheckedChange = { checked ->
                                drafts = drafts.mapIndexed { i, d -> if (i == index) d.copy(truncatable = checked) else d }
                            },
                        )
                        Text("Truncatable")
                    }
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    val found = mutableListOf<String>()
                    if (drafts.size > room) found += "The configured-model limit leaves room for $room entries. Deselect models before importing."
                    drafts.forEachIndexed { index, review ->
                        val others = taken + drafts.mapIndexedNotNull { i, d -> if (i != index) d.name.trim() else null }
                        found += models[index].reviewProblems(review.copy(name = review.name.trim()), others)
                    }
                    if (found.isEmpty()) {
                        onApply(drafts.map { it.toConfiguredModel() })
                    } else {
                        problems = found
                    }
                },
            ) { Text("Import ${if (models.size == 1) "model" else "${models.size} models"} to draft") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Back to catalogue") } },
    )
}

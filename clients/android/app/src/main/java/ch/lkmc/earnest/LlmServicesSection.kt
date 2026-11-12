package ch.lkmc.earnest

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
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
    var editingModel by remember(state.accountKey, ui.epoch) { mutableStateOf<String?>(null) }
    var addingModel by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }
    var modelsExpanded by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }

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
                referencedBy = current.tasks.filter { it.provider == provider.name && it.provider.isNotEmpty() }
                    .map { it.task },
                onTransform = { transformed ->
                    model.updateLlmSettingsDraft { draftCopy ->
                        draftCopy.copy(
                            providers = draftCopy.providers.mapIndexed { i, item -> if (i == index) transformed else item },
                            // Retarget complete names only. Transient empty names
                            // must never claim the disabled-task marker.
                            tasks = if (transformed.name != provider.name) {
                                retargetProvider(draftCopy.tasks, provider.name, transformed.name)
                            } else {
                                draftCopy.tasks
                            },
                        )
                    }
                },
                onRemove = {
                    model.updateLlmSettingsDraft { draftCopy ->
                        draftCopy.copy(providers = draftCopy.providers.filterIndexed { i, _ -> i != index })
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
                TextButton(onClick = { routingTask = route.task }) { Text("Route") }
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
            RouteTaskDialog(
                taskName = taskName,
                draft = current,
                onDismiss = { routingTask = null },
                onApply = { selectedProvider, selectedModel ->
                    routingTask = null
                    model.updateLlmSettingsDraft { draftCopy ->
                        draftCopy.copy(
                            tasks = draftCopy.tasks.map { route ->
                                if (route.task == taskName) LlmTaskRoute(taskName, selectedProvider, selectedModel) else route
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
) {
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(provider.name.ifBlank { "(unnamed provider)" }, style = MaterialTheme.typography.titleSmall)
            Spacer(Modifier.weight(1f))
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
    onDismiss: () -> Unit,
    onApply: (provider: String, model: String) -> Unit,
) {
    val current = draft.tasks.firstOrNull { it.task == taskName } ?: LlmTaskRoute(taskName)
    var provider by remember { mutableStateOf(current.provider) }
    var model by remember { mutableStateOf(current.model) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Route ${LlmTask.fromWire(taskName)?.label() ?: taskName}") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Provider")
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    FilterChip(
                        selected = provider.isEmpty(),
                        onClick = { provider = ""; model = "" },
                        label = { Text("Disabled") },
                    )
                    draft.providers.forEach { option ->
                        FilterChip(
                            selected = provider == option.name,
                            onClick = { provider = option.name },
                            label = { Text(option.name) },
                        )
                    }
                }
                if (provider.isNotEmpty()) {
                    Text("Model")
                    Column {
                        draft.models.forEach { option ->
                            FilterChip(
                                selected = model == option.name,
                                onClick = { model = option.name },
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
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = { onApply(provider, model) },
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

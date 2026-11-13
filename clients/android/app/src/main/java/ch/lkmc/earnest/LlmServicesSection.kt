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
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import ch.lkmc.earnest.core.LLM_MAX_CONFIGURED_MODELS
import ch.lkmc.earnest.core.LlmDefaultRoute
import ch.lkmc.earnest.core.LlmDiscoveredModel
import ch.lkmc.earnest.core.LlmModel
import ch.lkmc.earnest.core.LlmModelCapability
import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmSettingsDefaults
import ch.lkmc.earnest.core.LlmTask
import ch.lkmc.earnest.core.LlmTaskMode
import ch.lkmc.earnest.core.LlmTaskRoute
import ch.lkmc.earnest.core.LlmTokenField

/**
 * The server-managed AI services section. Providers and routes edit the
 * in-memory draft directly; only the explicit Save button publishes the
 * whole configuration. Typed API keys live only in that draft, are masked,
 * and are never written to preferences, Room, saved instance state or logs.
 * The catalogue selector fetches automatically when it opens and keeps its
 * results in memory, bound to the provider draft, revision and account.
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
    var defaultsPicker by remember(state.accountKey, ui.epoch) { mutableStateOf<String?>(null) }
    var editingModel by remember(state.accountKey, ui.epoch) { mutableStateOf<Pair<String, String>?>(null) }
    var discoveryDefault by remember(state.accountKey, ui.epoch) { mutableStateOf<String?>(null) }
    var confirmReload by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }
    var modelsExpanded by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }
    var discoveryTarget by remember(state.accountKey, ui.epoch) { mutableStateOf<Pair<Int, LlmTask?>?>(null) }
    var routingTask by remember(state.accountKey, ui.epoch) { mutableStateOf<String?>(null) }

    val current = draft
    Column(modifier.fillMaxWidth()) {
        Text("AI", style = MaterialTheme.typography.titleMedium)
        Text(
            "Server-wide settings. Add a provider, choose models, then enable the features you want.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            val dirty = ui.draft != null
            TextButton(
                onClick = model::saveLlmSettings,
                enabled = current != null && dirty && !ui.loading && !ui.saving && !ui.conflict,
            ) { Text("Save") }
            TextButton(
                onClick = { if (ui.conflict) confirmReload = true else model.discardLlmSettingsDraft() },
                enabled = dirty && !ui.saving,
            ) { Text("Discard") }
            if (ui.frozen != null && !ui.conflict) TextButton(
                onClick = model::retryLlmSettingsSave,
                enabled = !ui.loading && !ui.saving,
            ) { Text("Retry save") }
            if (ui.loading || ui.saving) CircularProgressIndicator(Modifier.width(20.dp))
        }
        ui.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        if (ui.success) Text("Saved", color = MaterialTheme.colorScheme.primary)
        if (ui.conflict) {
            Text(
                "The saved settings changed on the server. Reload them to review; your draft is kept.",
                color = MaterialTheme.colorScheme.error,
            )
            TextButton(onClick = { confirmReload = true }, enabled = !ui.loading && !ui.saving) { Text("Reload") }
        }
        if (current == null) {
            if (!ui.loading) TextButton(onClick = model::loadLlmSettings) { Text("Retry loading") }
            return@Column
        }

        ProvidersSection(
            draft = current,
            saving = ui.saving,
            onTransform = { index, replacement ->
                model.updateLlmSettingsDraft { it.editProvider(index, replacement) }
            },
            onRemove = { index ->
                model.updateLlmSettingsDraft { it.withoutProvider(index) }
            },
            onModels = { index ->
                discoveryDefault = null
                discoveryTarget = index to null
            },
            onAdd = { addProvider = true },
        )

        DefaultsSection(current) { defaultsPicker = it }

        FeaturesSection(
            draft = current,
            onToggle = { route, enabled ->
                model.updateLlmSettingsDraft { draftCopy ->
                    draftCopy.copy(
                        tasks = draftCopy.tasks.map {
                            if (it.task == route.task) toggleTaskRoute(it, enabled, draftCopy.defaults) else it
                        },
                    )
                }
            },
            onRoute = { route -> routingTask = route.task },
        )

        ModelPricingSection(
            draft = current,
            saving = ui.saving,
            expanded = modelsExpanded,
            onToggleExpanded = { modelsExpanded = !modelsExpanded },
            onEdit = { editingModel = it.identity() },
            onRemove = { target ->
                model.updateLlmSettingsDraft { draftCopy ->
                    draftCopy.withoutModel(target)
                }
            },
        )
    }

    if (confirmReload) {
        AlertDialog(
            onDismissRequest = { confirmReload = false },
            title = { Text("Reload AI settings?") },
            text = { Text("Your edits will be replaced after the latest settings load successfully.") },
            confirmButton = { TextButton(onClick = {
                confirmReload = false
                model.reloadLlmSettings()
            }) { Text("Reload") } },
            dismissButton = { TextButton(onClick = { confirmReload = false }) { Text("Keep editing") } },
        )
    }

    if (addProvider) {
        AddProviderDialog(
            existing = current?.providers?.map { it.name }?.toSet().orEmpty(),
            onDismiss = { addProvider = false },
            onConfirm = { name, kind, baseUrl ->
                addProvider = false
                model.updateLlmSettingsDraft { draftCopy ->
                    draftCopy.copy(providers = draftCopy.providers + LlmProviderDraft(name, kind, baseUrl))
                }
            },
        )
    }

    defaultsPicker?.let { key ->
        val title = if (key == "embedding") "Default embedding model" else "Default text model"
        DefaultModelPickerDialog(
            draft = current ?: return,
            title = title,
            capability = if (key == "embedding") LlmCapability.EMBEDDING else LlmCapability.CHAT,
            onBrowse = { index ->
                defaultsPicker = null
                discoveryDefault = key
                discoveryTarget = index to if (key == "embedding") LlmTask.EMBED else LlmTask.BLURB
            },
            onChoose = { route ->
                defaultsPicker = null
                model.updateLlmSettingsDraft { draftCopy ->
                    draftCopy.copy(
                        defaults = if (key == "embedding") {
                            draftCopy.defaults.copy(embedding = route)
                        } else {
                            draftCopy.defaults.copy(text = route)
                        },
                    )
                }
            },
            onDismiss = { defaultsPicker = null },
        )
    }

    routingTask?.let { taskName ->
        val route = current?.tasks?.firstOrNull { it.task == taskName }
        if (route == null) {
            routingTask = null
        } else {
            RouteTaskDialog(
                taskName = taskName,
                draft = current,
                onApply = { replacement ->
                    routingTask = null
                    model.updateLlmSettingsDraft { draftCopy ->
                        draftCopy.copy(tasks = draftCopy.tasks.map { if (it.task == taskName) replacement else it })
                    }
                },
                onDismiss = { routingTask = null },
            )
        }
    }

    editingModel?.let { identity ->
        val target = current?.models?.firstOrNull { it.identity() == identity }
        if (target == null) {
            editingModel = null
        } else {
            ModelDialog(
                title = "Edit model",
                model = target,
                taken = current.models.map { it.identity() }.toSet() - identity,
                onDismiss = { editingModel = null },
                onApply = { saved ->
                    editingModel = null
                    model.updateLlmSettingsDraft { draftCopy ->
                        draftCopy.copy(
                            models = draftCopy.models.map { if (it.identity() == identity) saved else it },

                        )
                    }
                },
            )
        }
    }

    discoveryTarget?.let { target ->
        val (providerIndex, task) = target
        val provider = current?.providers?.getOrNull(providerIndex)
        if (provider == null) {
            discoveryTarget = null
        } else {
            CatalogueDialog(
                model = model,
                state = state,
                providerIndex = providerIndex,
                task = task,
                onSelect = { addition ->
                    model.selectLlmModel(providerIndex, addition, if (discoveryDefault == null) task else null)
                    discoveryDefault?.let { key ->
                        model.updateLlmSettingsDraft { it.withDefaultModel(key, addition) }
                    }
                },
                onDismiss = {
                    discoveryTarget = null
                    discoveryDefault = null
                    model.dismissLlmDiscovery()
                },
            )
        }
    }
}

/**
 * Toggles one feature. Off keeps the route so switching back on restores it;
 * switching on with an explicit provider or model uses Custom, otherwise it
 * inherits the default. Selecting a default never turns an Off feature on by
 * itself.
 */
internal fun toggleTaskRoute(route: LlmTaskRoute, enabled: Boolean, defaults: LlmSettingsDefaults): LlmTaskRoute {
    if (!enabled) return if (route.resolvedMode() == LlmTaskMode.DEFAULT) {
        route.copy(provider = "", model = "", mode = LlmTaskMode.OFF)
    } else route.copy(mode = LlmTaskMode.OFF)
    val mode = if (route.provider.isNotEmpty() || route.model.isNotEmpty()) LlmTaskMode.CUSTOM else LlmTaskMode.DEFAULT
    return route.copy(mode = mode)
}

@Composable
private fun ProvidersSection(
    draft: LlmSettingsDraft,
    saving: Boolean,
    onTransform: (Int, LlmProviderDraft) -> Unit,
    onRemove: (Int) -> Unit,
    onModels: (Int) -> Unit,
    onAdd: () -> Unit,
) {
    Column(Modifier.fillMaxWidth()) {
        Text("Providers", style = MaterialTheme.typography.titleSmall)
        draft.providers.forEachIndexed { index, provider ->
            ProviderCard(
                provider = provider,
                referencedBy = draft.tasks
                    .filter { it.provider == provider.routedName && it.provider.isNotEmpty() }
                    .map { it.task },
                onTransform = { onTransform(index, it) },
                onRemove = { onRemove(index) },
                onModels = { onModels(index) },
            )
        }
        TextButton(onClick = onAdd, enabled = !saving) { Text("Add provider") }
    }
}

@Composable
private fun ProviderCard(
    provider: LlmProviderDraft,
    referencedBy: List<String>,
    onTransform: (LlmProviderDraft) -> Unit,
    onRemove: () -> Unit,
    onModels: () -> Unit,
) {
    var advanced by remember(provider.name, provider.hasApiKey) { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(provider.name.ifBlank { "(unnamed provider)" }, style = MaterialTheme.typography.titleSmall)
            Spacer(Modifier.width(8.dp))
            KeyStatusChip(provider.apiKeyStatus())
            Spacer(Modifier.weight(1f))
            TextButton(onClick = onModels) { Text("Models") }
            TextButton(onClick = onRemove) { Text("Remove") }
        }
        if (referencedBy.isNotEmpty()) {
            Text(
                "Used by ${referencedBy.joinToString(", ")}. Removing this provider turns these features off.",
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
            label = { Text("Monthly budget (USD, 0 = unlimited)") },
            isError = !ch.lkmc.earnest.core.validLlmMoney(provider.monthlyLimitUsd),
            supportingText = if (!ch.lkmc.earnest.core.validLlmMoney(provider.monthlyLimitUsd)) {
                { Text("Enter a non-negative amount with at most six decimal places.") }
            } else null,
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            modifier = Modifier.fillMaxWidth(),
        )
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
        TextButton(onClick = { advanced = !advanced }) { Text(if (advanced) "Hide advanced" else "Advanced") }
        if (advanced) {
        Text("Protocol", style = MaterialTheme.typography.bodySmall)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            LlmProviderKind.entries.forEach { kind ->
                FilterChip(
                    selected = provider.kind == kind,
                    onClick = { onTransform(provider.copy(kind = kind)) },
                    label = { Text(kind.label()) },
                )
            }
        }
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
        }
    }
}

@Composable
private fun AddProviderDialog(
    existing: Set<String>,
    onDismiss: () -> Unit,
    onConfirm: (String, LlmProviderKind, String) -> Unit,
) {
    var preset by remember { mutableStateOf(LLM_PROVIDER_PRESETS[0]) }
    var name by remember(preset) { mutableStateOf(uniqueProviderName(preset.label, existing)) }
    var baseUrl by remember(preset) { mutableStateOf(preset.baseUrl) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Add provider") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Preset", style = MaterialTheme.typography.bodySmall)
                FlowRow(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    LLM_PROVIDER_PRESETS.forEach { option ->
                        FilterChip(
                            selected = preset == option,
                            onClick = { preset = option },
                            label = { Text(option.label) },
                        )
                    }
                }
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("Name") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = baseUrl,
                    onValueChange = { baseUrl = it },
                    label = { Text("Base URL") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            TextButton(
                onClick = { onConfirm(name.trim(), preset.kind, baseUrl.trim()) },
                enabled = name.trim().isNotEmpty() && baseUrl.trim().isNotEmpty(),
            ) { Text("Add") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@Composable
private fun DefaultsSection(
    draft: LlmSettingsDraft,
    onPick: (String) -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Text("Default models", style = MaterialTheme.typography.titleSmall)
        Text(
            "Switched-on tasks use these routes. Embed uses the embedding default; every other task uses the text default.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        DefaultRouteRow("Default text model", draft.defaults.text) { onPick("text") }
        DefaultRouteRow("Default embedding model", draft.defaults.embedding) { onPick("embedding") }
    }
}

@Composable
private fun DefaultRouteRow(label: String, route: LlmDefaultRoute?, onPick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(label, style = MaterialTheme.typography.bodyMedium)
            Text(
                route?.takeIf { it.provider.isNotEmpty() }?.let { "${it.provider} / ${it.model}" } ?: "Not set",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        TextButton(onClick = onPick) { Text("Choose") }
    }
}

@Composable
private fun DefaultModelPickerDialog(
    draft: LlmSettingsDraft,
    title: String,
    capability: LlmCapability,
    onBrowse: (Int) -> Unit,
    onChoose: (LlmDefaultRoute) -> Unit,
    onDismiss: () -> Unit,
) {
    val providerNames = draft.providers.map { it.routedName.ifEmpty { it.name } }.filter { it.isNotEmpty() }.toSet()
    val options = draft.models.filter { it.provider.isNotEmpty() && it.provider in providerNames &&
        (capability != LlmCapability.EMBEDDING || it.dimensions > 0) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(
                    selected = false,
                    onClick = { onChoose(LlmDefaultRoute()) },
                    label = { Text("None") },
                )
                options.forEach { option ->
                    val route = LlmDefaultRoute(option.provider, option.name)
                    FilterChip(
                        selected = false,
                        onClick = { onChoose(route) },
                        label = { Text(option.identityLabel()) },
                    )
                }
                draft.providers.forEachIndexed { index, provider ->
                    TextButton(onClick = { onBrowse(index) }) { Text("Browse ${provider.name} models") }
                }
                if (draft.providers.isEmpty()) Text("Add a provider to choose a model.")
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Close") } },
    )
}

@Composable
private fun FeaturesSection(
    draft: LlmSettingsDraft,
    onToggle: (LlmTaskRoute, Boolean) -> Unit,
    onRoute: (LlmTaskRoute) -> Unit,
) {
    var advanced by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Text("Features", style = MaterialTheme.typography.titleSmall)
        Text(
            "The switch turns a feature on; it uses the matching default above unless Advanced names a " +
                "provider and model. Off disables it.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        draft.tasks.forEach { route ->
            val task = LlmTask.fromWire(route.task) ?: return@forEach
            val enabled = route.resolvedMode() != LlmTaskMode.OFF
            val resolved = route.effectiveRoute(draft.defaults)
            val on = enabled && !resolved?.provider.isNullOrEmpty() && !resolved?.model.isNullOrEmpty()
            Row(
                Modifier.fillMaxWidth().padding(vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f)) {
                    Text(task.label(), style = MaterialTheme.typography.bodyMedium)
                    Text(
                        when {
                            !enabled -> "Off"
                            on -> "Uses ${resolved?.provider} · ${resolved?.model}."
                            else -> "No default is configured, so this feature stays off."
                        },
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(
                    checked = enabled,
                    onCheckedChange = { onToggle(route, it) },
                    modifier = Modifier.semantics { contentDescription = "${task.label()} feature" },
                )
            }
        }
        TextButton(onClick = { advanced = !advanced }) {
            Text(if (advanced) "Hide advanced routes" else "Advanced custom feature routes")
        }
        if (advanced) {
            draft.tasks.forEach { route ->
                val task = LlmTask.fromWire(route.task) ?: return@forEach
                Row(
                    Modifier.fillMaxWidth().padding(vertical = 2.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(task.label(), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
                    TextButton(onClick = { onRoute(route) }) {
                        Text(if (route.resolvedMode() == LlmTaskMode.CUSTOM) "Edit custom route" else "Use a custom route")
                    }
                }
            }
        }
    }
}

private fun taskStatus(route: LlmTaskRoute, draft: LlmSettingsDraft): String {
    val task = LlmTask.fromWire(route.task) ?: return route.task
    val kind = if (task == LlmTask.EMBED) "embedding" else "text"
    return when (route.resolvedMode()) {
        LlmTaskMode.OFF -> "Disabled"
        LlmTaskMode.DEFAULT -> {
            val route = draft.defaults.routeFor(task)
            if (route.provider.isEmpty()) "Default $kind model not set"
            else "Default $kind model: ${route.provider} / ${route.model}"
        }
        LlmTaskMode.CUSTOM -> if (route.provider.isEmpty()) "Custom route not set" else "Custom route: ${route.provider} / ${route.model}"
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun RouteTaskDialog(
    taskName: String,
    draft: LlmSettingsDraft,
    onApply: (LlmTaskRoute) -> Unit,
    onDismiss: () -> Unit,
) {
    val task = requireNotNull(LlmTask.fromWire(taskName))
    val original = draft.tasks.first { it.task == taskName }
    var useDefault by remember { mutableStateOf(original.resolvedMode() != LlmTaskMode.CUSTOM) }
    var provider by remember { mutableStateOf(original.provider) }
    var model by remember { mutableStateOf(original.model) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Route ${task.label()}") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Mode", style = MaterialTheme.typography.bodySmall)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    FilterChip(selected = useDefault, onClick = { useDefault = true }, label = { Text("Default") })
                    FilterChip(selected = !useDefault, onClick = { useDefault = false }, label = { Text("Custom") })
                }
                if (!useDefault) {
                    Text("Provider", style = MaterialTheme.typography.bodySmall)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        draft.providers.forEach { option ->
                            FilterChip(
                                selected = provider == option.name,
                                onClick = {
                                    provider = option.name
                                    model = ""
                                },
                                label = { Text(option.name) },
                            )
                        }
                    }
                    if (provider.isNotEmpty()) {
                        Text("Model", style = MaterialTheme.typography.bodySmall)
                        Column {
                            draft.models.filter { it.provider == provider || it.provider.isEmpty() }.forEach { option ->
                                FilterChip(
                                    selected = model == option.name,
                                    onClick = { model = option.name },
                                    label = {
                                        Text(if (option.provider.isEmpty()) "${option.name} (global)" else option.name)
                                    },
                                    modifier = Modifier.padding(vertical = 2.dp),
                                )
                            }
                            if (draft.models.none { it.provider == provider || it.provider.isEmpty() }) {
                                Text(
                                    "No models are configured for this provider yet.",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                    }
                }
                Text(
                    "Default uses the server's default ${if (task == LlmTask.EMBED) "embedding" else "text"} route. " +
                        "Custom writes an explicit provider and model to the draft; Save persists.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    val mode = if (useDefault) LlmTaskMode.DEFAULT else LlmTaskMode.CUSTOM
                    onApply(LlmTaskRoute(taskName, provider, model, mode))
                },
                enabled = useDefault || (provider.isNotEmpty() && model.isNotEmpty()),
            ) { Text("Apply") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@Composable
private fun ModelPricingSection(
    draft: LlmSettingsDraft,
    saving: Boolean,
    expanded: Boolean,
    onToggleExpanded: () -> Unit,
    onEdit: (LlmModel) -> Unit,
    onRemove: (LlmModel) -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("Model pricing", style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
            TextButton(onClick = onToggleExpanded) { Text(if (expanded) "Hide" else "Edit") }
        }
        if (expanded) {
            Text(
                "Prices are USD per 1M tokens; set explicit zero prices for local models. " +
                    "Embedding dimensions are 0-1024, or wider with truncatable vectors or a provider " +
                    "that accepts a custom width.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            draft.models.forEach { target ->
                Row(
                    Modifier.fillMaxWidth().padding(vertical = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(target.identityLabel(), style = MaterialTheme.typography.bodyMedium)
                        Text(
                            "in ${target.inputPer1m} / out ${target.outputPer1m} / embed ${target.embeddingPer1m} per 1M, " +
                                "${target.dimensions} dimensions" +
                                if (target.truncatable) " (truncatable)" else "" +
                                if (target.supportsCustomDimensions) " (custom width)" else "",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    TextButton(onClick = { onEdit(target) }) { Text("Edit") }
                    TextButton(
                        onClick = { onRemove(target) },
                        enabled = !saving,
                    ) { Text("Remove") }
                }
            }
        }
    }
}

@Composable
private fun ModelDialog(
    title: String,
    model: LlmModel,
    taken: Set<Pair<String, String>>,
    onDismiss: () -> Unit,
    onApply: (LlmModel) -> Unit,
) {
    val name = model.name
    val provider = model.provider
    var input by remember { mutableStateOf(model.inputPer1m) }
    var output by remember { mutableStateOf(model.outputPer1m) }
    var embedding by remember { mutableStateOf(model.embeddingPer1m) }
    var dimensions by remember { mutableStateOf(model.dimensions.toString()) }
    var truncatable by remember { mutableStateOf(model.truncatable) }
    var supportsCustom by remember { mutableStateOf(model.supportsCustomDimensions) }
    val problems = modelSettingsProblems(
        name = name,
        input = input,
        output = output,
        embedding = embedding,
        dimensions = dimensions,
        truncatable = truncatable,
        supportsCustomDimensions = supportsCustom,
        provider = provider,
        knownEmbedding = null,
        taken = taken,
        capability = if (model.dimensions > 0) LlmCapability.EMBEDDING else LlmCapability.CHAT,
        requireAllPrices = true,
    )
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(model.identityLabel(), style = MaterialTheme.typography.bodyMedium)
                OutlinedTextField(
                    value = input,
                    onValueChange = { input = it },
                    label = { Text("Input per 1M (USD)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = output,
                    onValueChange = { output = it },
                    label = { Text("Output per 1M (USD)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = embedding,
                    onValueChange = { embedding = it },
                    label = { Text("Embedding per 1M (USD)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = dimensions,
                    onValueChange = { dimensions = it },
                    label = { Text("Embedding dimensions (0-1024, wider with truncation or custom width)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    isError = dimensions.isBlank() || dimensions.trim().toLongOrNull() == null,
                    modifier = Modifier.fillMaxWidth(),
                )
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(checked = truncatable, onCheckedChange = { truncatable = it })
                    Text("Truncatable")
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(checked = supportsCustom, onCheckedChange = { supportsCustom = it })
                    Text("Provider accepts a custom width")
                }
                problems.forEach { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
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
                            dimensions = dimensions.trim().toLong(),
                            truncatable = truncatable,
                            provider = provider.trim(),
                            supportsCustomDimensions = supportsCustom,
                        ),
                    )
                },
                enabled = problems.isEmpty(),
            ) { Text("Apply") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/** One catalogue row plus its inline selection pane, when it is open. */
private data class ModelSelection(
    val entry: LlmDiscoveredModel?,
    val review: LlmModelReview,
)

/**
 * The provider-scoped catalogue selector. It fetches automatically when it
 * opens, keeps results in memory bound to the provider draft and revision,
 * and applies a selection to the draft immediately. A model with every
 * applicable field published is ready in one tap; anything missing opens an
 * inline pane where an explicit free choice or typed metadata completes it.
 */
@Composable
private fun CatalogueDialog(
    model: AppViewModel,
    state: UiState,
    providerIndex: Int,
    task: LlmTask?,
    onSelect: (LlmModel) -> Unit,
    onDismiss: () -> Unit,
) {
    val ui = state.llmSettingsUi
    val draft = ui.draft ?: ui.loaded?.toDraft() ?: return
    val provider = draft.providers.getOrNull(providerIndex) ?: return
    val providerName = provider.routedName.ifEmpty { provider.name }
    val discovery = ui.discovery?.takeIf { entry ->
        entry.providerIndex == providerIndex &&
            entry.revision == ui.loadedRevision &&
            provider.discoveryFingerprint() == entry.providerKey
    }
    var query by remember { mutableStateOf("") }
    var manualId by remember { mutableStateOf("") }
    var selection by remember { mutableStateOf<ModelSelection?>(null) }
    var free by remember(selection?.review?.name) { mutableStateOf(false) }
    var refresh by remember { mutableStateOf(0) }
    // Provider browsing starts with text; a default or task fixes the capability.
    var capability by remember { mutableStateOf(task?.requiredCapability() ?: LlmCapability.CHAT) }
    val configured = remember(draft.models) { draft.models.mapTo(HashSet()) { it.identity() } }
    val room = (LLM_MAX_CONFIGURED_MODELS - draft.models.size).coerceAtLeast(0)
    val rawDiscovery = ui.discovery
    val stale = rawDiscovery != null && discovery == null
    val loading = rawDiscovery?.loading == true && !stale
    val models = discovery?.models.orEmpty()
    val needle = query.trim()
    val selectedTask = if (capability == LlmCapability.EMBEDDING) LlmTask.EMBED else LlmTask.BLURB
    val shown = models.filter { it.matchesSearch(needle) && it.compatibleWith(selectedTask) }
        .sortedBy { it.capability(capability) != true }
    val hidden = if (task != null) models.count { it.matchesSearch(needle) && !it.compatibleWith(task) } else 0

    // Opening the selector fetches the catalogue once for the provider
    // currently in the draft. Editing the provider discards the results; an
    // explicit refresh fetches the new destination.
    LaunchedEffect(providerIndex, provider.discoveryFingerprint(), ui.loadedRevision, refresh) {
        model.fetchLlmModels(providerIndex, refresh = refresh > 0)
    }

    Dialog(onDismissRequest = onDismiss) {
        Surface(
            shape = MaterialTheme.shapes.extraLarge,
            color = MaterialTheme.colorScheme.surface,
            tonalElevation = 6.dp,
        ) {
            Column(Modifier.padding(24.dp).heightIn(max = 560.dp).fillMaxWidth().verticalScroll(rememberScrollState())) {
                Text(
                    if (task != null) "Models for ${task.label()}; $providerName" else "Models; $providerName",
                    style = MaterialTheme.typography.titleMedium,
                )
                Text(
                    "Choose a model for this provider. Changes are saved with your AI settings.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = query,
                    onValueChange = { query = it },
                    label = { Text("Search models") },
                    placeholder = { Text("Filter by ID or name") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                if (task == null) Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(selected = capability == LlmCapability.CHAT, onClick = {
                        capability = LlmCapability.CHAT; selection = null
                    }, label = { Text("Text") })
                    FilterChip(selected = capability == LlmCapability.EMBEDDING, onClick = {
                        capability = LlmCapability.EMBEDDING; selection = null
                    }, label = { Text("Embedding") })
                }
                Spacer(Modifier.height(8.dp))
                when {
                    loading -> {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            CircularProgressIndicator(Modifier.width(20.dp))
                            Spacer(Modifier.width(8.dp))
                            Text("Fetching models…", style = MaterialTheme.typography.bodySmall)
                        }
                    }
                    stale -> {
                        Text(
                            "The provider details or settings revision changed; the earlier results were discarded.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        TextButton(onClick = { refresh++ }) { Text("Fetch again") }
                    }
                    discovery == null -> {
                        Text(
                            "Fetch the provider's catalogue to browse it.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            TextButton(onClick = { refresh++ }) { Text("Fetch models") }
                        }
                    }
                    discovery.error != null -> {
                        Text(discovery.error, color = MaterialTheme.colorScheme.error)
                        Text(
                            "The configured and manual models are unchanged and stay available.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    else -> {
                        if (shown.isEmpty()) {
                            Text(
                                if (models.isEmpty()) "The provider listed no models." else "No catalogue entries match the search.",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        LazyColumn(Modifier.heightIn(max = 220.dp).fillMaxWidth()) {
                            items(shown, key = { it.id }) { entry ->
                                val prefill = entry.reviewPrefill(providerName, capability)
                                val identity = prefill.provider to prefill.name
                                val alreadyConfigured = identity in configured
                                val problems = entry.reviewProblems(
                                    prefill,
                                    configured - identity,
                                    capability,
                                )
                                val ready = alreadyConfigured || problems.isEmpty()
                                CatalogueRow(
                                    entry = entry,
                                    configured = alreadyConfigured,
                                    ready = ready,
                                    selected = selection?.review?.name == entry.id,
                                    onConfigure = {
                                        selection = ModelSelection(entry, prefill)
                                        free = false
                                    },
                                    onUse = if (ready) {
                                        {
                                            onSelect(draft.models.firstOrNull { it.identity() == identity } ?: prefill.toConfiguredModel(capability))
                                            onDismiss()
                                        }
                                    } else {
                                        null
                                    },
                                )
                            }
                        }
                        if (hidden > 0) {
                            Text(
                                "$hidden catalogue ${if (hidden == 1) "entry is" else "entries are"} hidden because " +
                                    "the provider says ${if (hidden == 1) "it" else "they"} cannot do ${task?.capabilityLabel() ?: "that"} work.",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }

                        Row(verticalAlignment = Alignment.CenterVertically) {
                            OutlinedTextField(
                                value = manualId,
                                onValueChange = { manualId = it },
                                label = { Text("Add model ID manually") },
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                            TextButton(
                                onClick = {
                                    val id = manualId.trim()
                                    if (id.isNotEmpty()) {
                                        selection = ModelSelection(null, LlmModelReview(name = id, provider = providerName))
                                        manualId = ""
                                        free = false
                                    }
                                },
                                enabled = manualId.isNotBlank(),
                            ) { Text("Add") }
                        }

                selection?.let { current ->
                    SelectionPane(
                        model = model,
                        state = state,
                        providerIndex = providerIndex,
                        providerName = providerName,
                        providerKey = provider.discoveryFingerprint(),
                        capability = capability,
                        task = task,
                        selection = current,
                        free = free,
                        configured = configured,
                        room = room,
                        onFreeChange = { value ->
                            free = value
                            if (value) {
                                selection = current.copy(
                                    review = current.review.copy(
                                        input = if (capability != LlmCapability.EMBEDDING) "0" else current.review.input,
                                        output = if (capability != LlmCapability.EMBEDDING) "0" else current.review.output,
                                        embedding = if (capability != LlmCapability.CHAT) "0" else current.review.embedding,
                                    ),
                                )
                            }
                        },
                        onReviewChange = { review ->
                            selection = current.copy(review = review)
                        },
                        onApply = {
                            onSelect(draft.models.firstOrNull { it.identity() == current.review.provider to current.review.name }
                                ?: current.review.toConfiguredModel(capability))
                            onDismiss()
                        },
                        onDismiss = {
                            selection = null
                            free = false
                        },
                    )
                }

                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (!loading) TextButton(onClick = { refresh++ }) { Text(if (discovery?.error != null) "Retry" else "Refresh") }
                    TextButton(onClick = onDismiss) { Text("Close") }
                }
            }
        }
    }
}

@Composable
private fun CatalogueRow(
    entry: LlmDiscoveredModel,
    configured: Boolean,
    ready: Boolean,
    selected: Boolean,
    onConfigure: () -> Unit,
    onUse: (() -> Unit)?,
) {
    Column(Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                if (entry.name.isEmpty() || entry.name == entry.id) entry.id else "${entry.name} (${entry.id})",
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.weight(1f),
            )
            if (configured) StatusChip("configured", MaterialTheme.colorScheme.primaryContainer)
            Spacer(Modifier.width(4.dp))
            if (onUse != null) {
                TextButton(onClick = onUse) { Text("Use") }
            } else {
                TextButton(onClick = onConfigure) { Text("Configure") }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
            if (entry.chat == true) StatusChip("chat", MaterialTheme.colorScheme.surfaceVariant)
            if (entry.embedding == true) StatusChip("embedding", MaterialTheme.colorScheme.surfaceVariant)
            if (entry.chat == null && entry.embedding == null) {
                StatusChip("capabilities unknown", MaterialTheme.colorScheme.tertiaryContainer)
            }
        }
        val details = buildList {
            if (entry.inputPer1m != null || entry.outputPer1m != null) {
                add("in $${entry.inputPer1m ?: "?"} · out $${entry.outputPer1m ?: "?"}")
            }
            entry.embeddingPer1m?.let { add("embed $$it") }
            entry.dimensions?.let { add("$it dimensions") }
            if (entry.truncatable == true) add("truncatable")
            if (entry.supportsCustomDimensions == true) add("custom width")
        }
        if (details.isNotEmpty()) {
            Text(
                details.joinToString(" · ") + " per 1M tokens",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (selected) {
            Text(
                "Editing below.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.primary,
            )
        }
    }
}

@Composable
private fun SelectionPane(
    model: AppViewModel,
    state: UiState,
    providerIndex: Int,
    providerName: String,
    providerKey: String,
    capability: LlmCapability,
    task: LlmTask?,
    selection: ModelSelection,
    free: Boolean,
    configured: Set<Pair<String, String>>,
    room: Int,
    onFreeChange: (Boolean) -> Unit,
    onReviewChange: (LlmModelReview) -> Unit,
    onApply: () -> Unit,
    onDismiss: () -> Unit,
) {
    val review = selection.review
    val identity = review.provider.trim() to review.name.trim()
    val alreadyConfigured = identity in configured
    val baseProblems = selection.entry?.reviewProblems(review, configured - identity, capability)
        ?: modelSettingsProblems(
            name = review.name,
            input = review.input,
            output = review.output,
            embedding = review.embedding,
            dimensions = review.dimensions,
            truncatable = review.truncatable,
            supportsCustomDimensions = review.supportsCustomDimensions,
            provider = review.provider,
            knownEmbedding = null,
            taken = configured - identity,
            capability = capability,
        )
    val problems = baseProblems + if (!alreadyConfigured && room < 1 && !review.name.trim().isEmpty()) {
        listOf("The configured-model table is full at $LLM_MAX_CONFIGURED_MODELS entries; remove one before adding.")
    } else {
        emptyList()
    }
    // A check belongs to one provider draft, revision, account and model;
    // anything else is stale and must not be shown or applied.
    val check = state.llmSettingsUi.check?.takeIf {
        it.providerIndex == providerIndex &&
            it.providerKey == providerKey &&
            it.revision == state.llmSettingsUi.loadedRevision &&
            it.modelId == review.name.trim()
    }
    Surface(
        color = MaterialTheme.colorScheme.surfaceVariant,
        shape = MaterialTheme.shapes.medium,
        modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
    ) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                if (selection.entry == null) review.name else {
                    val entry = selection.entry
                    if (entry.name.isEmpty() || entry.name == entry.id) entry.id else "${entry.name} (${entry.id})"
                },
                style = MaterialTheme.typography.titleSmall,
            )
            Text(
                "Provider $providerName · " + when (capability) {
                    LlmCapability.CHAT -> "chat"
                    LlmCapability.EMBEDDING -> "embedding"
                    LlmCapability.BOTH -> "chat and embedding"
                },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            OutlinedTextField(
                value = review.name,
                onValueChange = { onReviewChange(review.copy(name = it)) },
                label = { Text("Configured name") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            val chat = capability != LlmCapability.EMBEDDING
            val embedding = capability != LlmCapability.CHAT
            if (chat) {
                OutlinedTextField(
                    value = review.input,
                    onValueChange = { onReviewChange(review.copy(input = it)) },
                    label = { Text("Input per 1M (USD)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = review.output,
                    onValueChange = { onReviewChange(review.copy(output = it)) },
                    label = { Text("Output per 1M (USD)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            if (embedding) {
                OutlinedTextField(
                    value = review.embedding,
                    onValueChange = { onReviewChange(review.copy(embedding = it)) },
                    label = { Text("Embedding per 1M (USD)") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = review.dimensions,
                    onValueChange = { onReviewChange(review.copy(dimensions = it)) },
                    label = { Text("Embedding dimensions") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    isError = review.dimensions.isBlank() || review.dimensions.trim().toLongOrNull() == null,
                    modifier = Modifier.fillMaxWidth(),
                )
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(
                        checked = review.truncatable,
                        onCheckedChange = { onReviewChange(review.copy(truncatable = it)) },
                    )
                    Text("Truncatable")
                    Spacer(Modifier.width(8.dp))
                    Checkbox(
                        checked = review.supportsCustomDimensions,
                        onCheckedChange = { onReviewChange(review.copy(supportsCustomDimensions = it)) },
                    )
                    Text("Custom width")
                }
            }
            if (capability == LlmCapability.CHAT) {
                Text(
                    "Embedding price and geometry do not apply to chat and are stored as zero.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = free, onCheckedChange = onFreeChange)
                Text("This model is free")
            }
            problems.forEach {
                Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            }
            Text(
                "Checking sends a fixed sample to the provider; the provider may bill for it.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            when {
                check?.loading == true -> Row(verticalAlignment = Alignment.CenterVertically) {
                    CircularProgressIndicator(Modifier.width(16.dp))
                    Spacer(Modifier.width(8.dp))
                    Text("Checking…", style = MaterialTheme.typography.bodySmall)
                }
                check?.error != null -> Text(check.error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                check?.result != null -> {
                    val result = check.result
                    Column {
                        Text(
                            result.message.ifEmpty { if (result.compatible) "Compatible." else "Not compatible." },
                            style = MaterialTheme.typography.bodySmall,
                        )
                        if (result.compatible && (result.dimensions != null || result.supportsCustomDimensions != null)) {
                            TextButton(onClick = {
                                val next = review.copy(
                                    dimensions = result.dimensions?.toString() ?: review.dimensions,
                                    supportsCustomDimensions = result.supportsCustomDimensions ?: review.supportsCustomDimensions,
                                )
                                onReviewChange(next)
                                model.updateLlmSettingsDraft { draft ->
                                    draft.copy(
                                        models = draft.models.map {
                                            if (it.identity() == identity) {
                                                it.copy(
                                                    dimensions = result.dimensions ?: it.dimensions,
                                                    supportsCustomDimensions = result.supportsCustomDimensions
                                                        ?: it.supportsCustomDimensions,
                                                )
                                            } else {
                                                it
                                            }
                                        },
                                    )
                                }
                            }) { Text("Apply observed geometry to draft") }
                        }
                    }
                }
                else -> Unit
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                TextButton(
                    onClick = {
                        // A no-context selection checks chat; a task context
                        // always has a concrete capability.
                        val wire = if (capability == LlmCapability.EMBEDDING) {
                            LlmModelCapability.EMBEDDING
                        } else {
                            LlmModelCapability.CHAT
                        }
                        val checked = review.copy(dimensions = (review.dimensions.toLongOrNull() ?: 0).toString())
                        model.checkLlmModel(providerIndex, checked.toConfiguredModel(capability), wire)
                    },
                    enabled = check?.loading != true && review.name.isNotBlank() &&
                        (if (embedding) ch.lkmc.earnest.core.validLlmMoney(review.embedding) else true) &&
                        (if (chat) ch.lkmc.earnest.core.validLlmMoney(review.input) && ch.lkmc.earnest.core.validLlmMoney(review.output) else true),
                ) { Text("Check model") }
                TextButton(onClick = onApply, enabled = problems.isEmpty()) {
                    Text(if (task != null) "Use for ${task.label()}" else "Add to draft")
                }
                TextButton(onClick = onDismiss) { Text("Cancel") }
            }
        }
    }
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

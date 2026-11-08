package ch.lkmc.earnest

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.ReaderFont
import kotlin.math.roundToInt
import ch.lkmc.earnest.core.ReaderOverride
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.ReaderTheme
import ch.lkmc.earnest.core.ReadingMode
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull

/**
 * The account reader preferences. Every field edits a local draft; only the
 * explicit Save button publishes it. The chrome theme is deliberately absent:
 * it stays a device-local preference.
 */
@Composable
internal fun SettingsScreen(
    model: AppViewModel,
    state: UiState,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val ui = state.readerSettingsUi
    val settings = ui.draft ?: ui.loaded ?: state.readerSettings
    val feeds = state.snapshot.objects
        .filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }
        .mapNotNull { obj -> obj.feedId()?.let { feedId -> Triple(obj.id, feedId, obj.name) } }
    var addOverride by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }
    var overrideFeedId by remember(state.accountKey, ui.epoch) { mutableStateOf<Long?>(null) }
    var overrideTheme by remember(state.accountKey, ui.epoch) { mutableStateOf<ReaderTheme?>(null) }
    var overrideMeasure by remember(state.accountKey, ui.epoch) { mutableStateOf<Long?>(null) }
    var overrideFont by remember(state.accountKey, ui.epoch) { mutableStateOf<ReaderFont?>(null) }

    Column(modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onBack) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
            }
            Text("Settings", style = MaterialTheme.typography.titleLarge)
            Spacer(Modifier.width(8.dp))
            if (ui.loading) CircularProgressIndicator(Modifier.width(20.dp))
            if (ui.saving) CircularProgressIndicator(Modifier.width(20.dp))
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(onClick = model::loadReaderSettings, enabled = !ui.loading) { Text("Load") }
            val dirty = ui.draft != null
            TextButton(onClick = model::saveReaderSettings, enabled = ui.loaded != null && state.snapshot.cursor.generation.isNotEmpty() && dirty && !ui.saving) { Text("Save") }
            TextButton(onClick = model::retryReaderSettingsSave, enabled = ui.frozen != null && !ui.saving) { Text("Retry") }
            TextButton(onClick = model::reloadReaderSettings, enabled = !ui.loading) { Text("Reload") }
        }
        ui.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        if (ui.success) Text("Saved", color = MaterialTheme.colorScheme.primary)
        if (ui.loaded == null || state.snapshot.cursor.generation.isEmpty()) {
            Text("Sync or load your account settings before editing.")
            return@Column
        }

        Section("Reading mode") {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                ReadingMode.entries.forEach { mode ->
                    FilterChip(
                        selected = settings.readingMode == mode,
                        onClick = { model.updateReaderSettingsDraft { it.copy(readingMode = mode) } },
                        label = { Text(if (mode == ReadingMode.RIVER) "River" else "Unread counts") },
                    )
                }
            }
        }

        Section("Today decay") {
            Text("${settings.todayDecayDays} days")
            Slider(
                value = settings.todayDecayDays.toFloat(),
                onValueChange = { value ->
                    model.updateReaderSettingsDraft { it.copy(todayDecayDays = value.roundToInt().toLong().coerceIn(1, 365)) }
                },
                valueRange = 1f..365f,
                steps = 363,
                modifier = Modifier.semantics { contentDescription = "Today decay days" },
            )
        }

        Section("Shortcuts") {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Switch(
                    checked = settings.singleKeyShortcuts,
                    onCheckedChange = { checked -> model.updateReaderSettingsDraft { it.copy(singleKeyShortcuts = checked) } },
                    modifier = Modifier.semantics { contentDescription = "Single-key shortcuts" },
                )
                Text("Single-key shortcuts (shared with web and Mac)")
            }
        }

        Section("Article theme") {
            ReaderTheme.entries.forEach { theme ->
                Row(
                    Modifier.fillMaxWidth().padding(vertical = 2.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    RadioButton(
                        selected = settings.theme == theme,
                        onClick = { model.updateReaderSettingsDraft { it.copy(theme = theme) } },
                    )
                    Text(theme.label())
                }
            }
        }

        Section("Measure") {
            Text("${settings.measure}")
            Slider(
                value = settings.measure.toFloat(),
                onValueChange = { value ->
                    model.updateReaderSettingsDraft { it.copy(measure = value.roundToInt().toLong().coerceIn(62, 72)) }
                },
                valueRange = 62f..72f,
                steps = 9,
                modifier = Modifier.semantics { contentDescription = "Measure" },
            )
        }

        Section("Font") {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                ReaderFont.entries.forEach { font ->
                    FilterChip(
                        selected = settings.font == font,
                        onClick = { model.updateReaderSettingsDraft { it.copy(font = font) } },
                        label = { Text(font.label()) },
                    )
                }
            }
        }

        Section("Per-feed typography") {
            Text(
                "Overrides are keyed by feed ID and inherit the account setting for members left as Inherit.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            feeds.forEach { (subscriptionId, feedId, name) ->
                val override = settings.feedOverrides[feedId.toString()]
                Row(
                    Modifier.fillMaxWidth().padding(vertical = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Column(Modifier.weight(1f)) {
                        Text("$name (feed $feedId)", style = MaterialTheme.typography.bodyMedium)
                        Text(
                            overrideSummary(subscriptionId, override),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    TextButton(onClick = {
                        overrideFeedId = feedId
                        overrideTheme = override?.theme
                        overrideMeasure = override?.measure
                        overrideFont = override?.font
                    }) { Text("Override") }
                    TextButton(
                        onClick = { model.updateReaderSettingsDraft { it.copy(feedOverrides = it.feedOverrides - feedId.toString()) } },
                        enabled = override != null,
                    ) { Text("Reset") }
                }
            }
            TextButton(onClick = { addOverride = true }) { Text("Add override by feed ID") }
        }
    }

    if (addOverride) {
        AddFeedOverrideDialog(
            onDismiss = { addOverride = false },
            onConfirm = { feedId ->
                addOverride = false
                overrideFeedId = feedId
                overrideTheme = null
                overrideMeasure = null
                overrideFont = null
            },
        )
    }

    val editingFeed = overrideFeedId
    if (editingFeed != null) {
        AlertDialog(
            onDismissRequest = { overrideFeedId = null },
            title = { Text("Feed $editingFeed override") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("Theme")
                    ChipRow(
                        options = ReaderTheme.entries.map { it to it.label() },
                        selected = overrideTheme,
                        onSelect = { overrideTheme = it },
                    )
                    OutlinedTextField(
                        value = overrideMeasure?.toString().orEmpty(),
                        onValueChange = { value ->
                            overrideMeasure = value.trim().toLongOrNull()?.takeIf { it in 62..72 }
                        },
                        label = { Text("Measure 62-72 (blank inherits)") },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    )
                    Text("Font")
                    ChipRow(
                        options = ReaderFont.entries.map { it to it.label() },
                        selected = overrideFont,
                        onSelect = { overrideFont = it },
                    )
                }
            },
            confirmButton = {
                TextButton(onClick = {
                    model.updateReaderSettingsDraft { current ->
                        current.copy(
                            feedOverrides = current.feedOverrides + (
                                editingFeed.toString() to ReaderOverride(
                                    theme = overrideTheme,
                                    measure = overrideMeasure,
                                    font = overrideFont,
                                )
                                ),
                        )
                    }
                    overrideFeedId = null
                }) { Text("Apply") }
            },
            dismissButton = { TextButton(onClick = { overrideFeedId = null }) { Text("Cancel") } },
        )
    }

    ui.conflict?.let { newer ->
        AlertDialog(
            onDismissRequest = { },
            title = { Text("Newer settings are available") },
            text = {
                Text(
                    "This account already has settings from revision ${newer.rev}. " +
                        "Reload them instead of overwriting with revision ${ui.loadedRev}.",
                )
            },
            confirmButton = { TextButton(onClick = model::reloadReaderSettings) { Text("Reload") } },
            dismissButton = {
                TextButton(onClick = model::dismissReaderSettingsConflict) { Text("Keep editing") }
            },
        )
    }
}

@Composable
private fun Section(title: String, content: @Composable () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Text(title, style = MaterialTheme.typography.titleMedium)
        content()
    }
}

@Composable
private fun <T> ChipRow(options: List<Pair<T, String>>, selected: T?, onSelect: (T?) -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(
            selected = selected == null,
            onClick = { onSelect(null) },
            label = { Text("Inherit") },
        )
        options.forEach { (value, label) ->
            FilterChip(
                selected = selected == value,
                onClick = { onSelect(value) },
                label = { Text(label) },
            )
        }
    }
}

@Composable
private fun AddFeedOverrideDialog(onDismiss: () -> Unit, onConfirm: (Long) -> Unit) {
    var value by remember { mutableStateOf("") }
    val feedId = value.trim().toLongOrNull()?.takeIf { it > 0 && it.toString() == value.trim() }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Add override by feed ID") },
        text = {
            OutlinedTextField(
                value = value,
                onValueChange = { value = it },
                label = { Text("Feed ID") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            )
        },
        confirmButton = { TextButton(onClick = { feedId?.let(onConfirm) }, enabled = feedId != null) { Text("Add") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

private fun SyncObject.feedId(): Long? = runCatching {
    Json.parseToJsonElement(raw).jsonObject["feed_id"]?.jsonPrimitive?.longOrNull
}.getOrNull()

private fun ReaderTheme.label(): String = when (this) {
    ReaderTheme.SYSTEM -> "System"
    ReaderTheme.LIGHT -> "Light"
    ReaderTheme.DARK -> "Dark"
    ReaderTheme.BLACK -> "Black"
    ReaderTheme.EINK -> "E-ink"
}

private fun ReaderFont.label(): String = when (this) {
    ReaderFont.SERIF -> "Serif"
    ReaderFont.SANS -> "Sans"
    ReaderFont.DYSLEXIC -> "Dyslexic"
}

private fun overrideSummary(subscriptionId: Long, override: ReaderOverride?): String {
    if (override == null) return "Inherits subscription $subscriptionId"
    val parts = mutableListOf<String>()
    override.theme?.let { parts += "theme ${it.name.lowercase()}" }
    override.measure?.let { parts += "measure $it" }
    override.font?.let { parts += "font ${it.name.lowercase()}" }
    return if (parts.isEmpty()) "Inherits everything" else parts.joinToString(", ")
}

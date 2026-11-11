package ch.lkmc.earnest

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
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
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
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
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.KeptImportReport
import ch.lkmc.earnest.core.ReaderFont
import ch.lkmc.earnest.core.MAX_KEPT_IMPORT_BYTES
import ch.lkmc.earnest.core.readAtMost
import ch.lkmc.earnest.core.summarizeKeptImport
import kotlin.math.roundToInt
import ch.lkmc.earnest.core.ReaderOverride
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.ReaderTheme
import ch.lkmc.earnest.core.ReadingMode
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
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
    var overrideMeasureInput by remember(state.accountKey, ui.epoch) { mutableStateOf(OverrideMeasureInput.fromValue(null)) }
    var overrideFont by remember(state.accountKey, ui.epoch) { mutableStateOf<ReaderFont?>(null) }

    val keptImport = state.keptImport
    var subscribeMissing by remember(state.accountKey) { mutableStateOf(false) }
    var readingImport by remember(state.accountKey) { mutableStateOf(false) }
    val importScope = rememberCoroutineScope()
    val context = LocalContext.current
    val importPicker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        readingImport = true
        importScope.launch {
            // Reading counts as working, so a second tap cannot start a second
            // read while the first file is still on its way.
            // Read at most one byte past the limit, so a huge file picked by
            // mistake is refused without loading it all into memory.
            val document = withContext(Dispatchers.IO) {
                runCatching {
                    context.contentResolver.openInputStream(uri)?.use { readAtMost(it, MAX_KEPT_IMPORT_BYTES) }
                }.getOrNull()
            }
            readingImport = false
            when {
                document == null -> model.keptImportReadFailed("Could not read that file.")
                document.size > MAX_KEPT_IMPORT_BYTES -> model.keptImportReadFailed("The export is larger than 64 MiB.")
                else -> model.importKept(document, subscribeMissing)
            }
        }
    }

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

        Section("Starred articles") {
            Text(
                "Bring starred articles from another reader into Kept. Export them as Google Reader JSON, for example from FreshRSS or Inoreader.",
                style = MaterialTheme.typography.bodyMedium,
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(
                    checked = subscribeMissing,
                    onCheckedChange = { subscribeMissing = it },
                    enabled = !keptImport.busy && !readingImport,
                )
                Text("Also subscribe to feeds you don't follow")
            }
            Button(
                onClick = { importPicker.launch(arrayOf("application/json", "*/*")) },
                enabled = !keptImport.busy && !readingImport,
            ) { Text("Import starred articles") }
            if (keptImport.busy || readingImport) Text("Importing starred articles…")
            keptImport.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            keptImport.report?.let { report ->
                KeptImportReportView(report = report, busy = keptImport.busy, onRetry = model::importKeptAgain)
            }
        }

        LlmServicesSection(model = model, state = state, modifier = Modifier.padding(vertical = 8.dp))

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
            feeds.forEach { (_, feedId, name) ->
                val override = settings.feedOverrides[feedId.toString()]
                Row(
                    Modifier.fillMaxWidth().padding(vertical = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Column(Modifier.weight(1f)) {
                        Text("$name (feed $feedId)", style = MaterialTheme.typography.bodyMedium)
                        Text(
                            overrideSummary(override),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    TextButton(onClick = {
                        overrideFeedId = feedId
                        overrideTheme = override?.theme
                        overrideMeasureInput = OverrideMeasureInput.fromValue(override?.measure)
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
                overrideMeasureInput = OverrideMeasureInput.fromValue(null)
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
                        value = overrideMeasureInput.text,
                        onValueChange = { overrideMeasureInput = overrideMeasureInput.edit(it) },
                        label = { Text("Measure 62-72 (blank inherits)") },
                        isError = !overrideMeasureInput.canApply,
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
                                    measure = overrideMeasureInput.measure,
                                    font = overrideFont,
                                )
                                ),
                        )
                    }
                    overrideFeedId = null
                }, enabled = overrideMeasureInput.canApply) { Text("Apply") }
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

/** The import report, worded the same way as the web client's. */
@Composable
private fun KeptImportReportView(report: KeptImportReport, busy: Boolean, onRetry: () -> Unit) {
    val summary = summarizeKeptImport(report)
    val total = summary.added + summary.existing
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(
            "$total ${if (total == 1) "article" else "articles"} in Kept: " +
                "${summary.added} added from the export, ${summary.existing} already in Earnest.",
        )
        if (summary.subscribed > 0) {
            Text(
                "Subscribed to ${summary.subscribed} ${if (summary.subscribed == 1) "feed" else "feeds"}. " +
                    "Their first fetches run in the background.",
            )
        }
        if (summary.labelled > 0) {
            Text("Added the export's labels to ${summary.labelled} ${if (summary.labelled == 1) "article" else "articles"}.")
        }
        if (summary.unkept > 0) {
            Text("${summary.unkept} left out because you removed ${if (summary.unkept == 1) "it" else "them"} from Kept.")
        }
        if (summary.gone > 0) Text("${summary.gone} no longer available in Earnest.")
        if (summary.skipped.isNotEmpty()) {
            val skippedTotal = summary.skipped.sumOf { it.count }
            Text("$skippedTotal skipped because you don't follow their feed:")
            summary.skipped.forEach { skip ->
                Text(
                    "${skip.feedUrl.ifBlank { "Unknown feed" }} (${skip.count})",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            TextButton(onClick = onRetry, enabled = !busy) { Text("Subscribe to these feeds and import again") }
        }
        if (summary.failed.isNotEmpty()) {
            Text(
                "${summary.failed.size} could not be imported: ${summary.failed.joinToString("; ")}",
                color = MaterialTheme.colorScheme.error,
            )
        }
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

private fun overrideSummary(override: ReaderOverride?): String {
    if (override == null) return "Inherits account settings"
    val parts = mutableListOf<String>()
    override.theme?.let { parts += "theme ${it.name.lowercase()}" }
    override.measure?.let { parts += "measure $it" }
    override.font?.let { parts += "font ${it.name.lowercase()}" }
    return if (parts.isEmpty()) "Inherits everything" else parts.joinToString(", ")
}

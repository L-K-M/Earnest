package ch.lkmc.earnest

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import ch.lkmc.earnest.ReaderFilterChip as FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Slider
import androidx.compose.material3.Surface
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
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

/** Approximate width of one reader measure character, in dp, for the preview. */
private const val PREVIEW_CHAR_WIDTH_DP = 8

/**
 * The account reader preferences. Every field edits a local draft; only the
 * explicit Save button publishes it. The chrome theme is deliberately absent:
 * it stays a device-local preference.
 *
 * Reading controls come first, then the starred import and the server-managed
 * LLM services. The starred import and LLM sections stay available even before
 * the account's reader settings have loaded.
 */
@OptIn(ExperimentalLayoutApi::class)
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
    var confirmReload by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }
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

    val blocked = ui.loaded == null || state.snapshot.cursor.generation.isEmpty()
    val largeText = LocalDensity.current.fontScale >= 1.5f

    // The outer app bar owns the back affordance and the system back gesture,
    // so this screen renders no title or back button of its own; onBack stays
    // in the signature for its callers.
    Column(modifier.fillMaxSize(), horizontalAlignment = Alignment.CenterHorizontally) {
        Column(
            Modifier.weight(1f).fillMaxWidth().verticalScroll(rememberScrollState())
                .padding(horizontal = ReaderSpace.md, vertical = ReaderSpace.sm),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
        Column(
            // Cap the readable width on tablets; fill the narrower screen.
            Modifier.widthIn(max = ReaderSpace.contentWidth).fillMaxWidth(),
            verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
        ) {
            Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
                FlowRow(
                    horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
                    verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
                ) {
                    // Load preserves the draft and frozen retry; Reload discards them.
                    TextButton(onClick = model::loadReaderSettings, enabled = !ui.loading && !ui.saving) { Text("Load") }
                    TextButton(onClick = {
                        if (ui.draft != null || ui.frozen != null) confirmReload = true else model.reloadReaderSettings()
                    }, enabled = !ui.loading && !ui.saving) { Text("Reload") }
                    if (ui.frozen != null) {
                        TextButton(onClick = model::retryReaderSettingsSave, enabled = !ui.saving) { Text("Retry") }
                    }
                    if (ui.loading || ui.saving) {
                        CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                    }
                }
                ui.error?.let { NoticeBanner(it, tone = NoticeTone.ERROR) }
                if (ui.success) NoticeBanner("Saved", tone = NoticeTone.SUCCESS)
                if (blocked) {
                    NoticeBanner("Sync or load your account settings before editing.", tone = NoticeTone.INFO)
                }
            }

            if (!blocked) {
                SettingsGroup("Reading preferences") {
                    Text(
                        "Reading preferences sync across your devices. The app theme stays on this device.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Text("Reading mode", style = MaterialTheme.typography.labelLarge)
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
                        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
                    ) {
                        ReadingMode.entries.forEach { mode ->
                            FilterChip(
                                selected = settings.readingMode == mode,
                                onClick = { model.updateReaderSettingsDraft { it.copy(readingMode = mode) } },
                                label = { Text(if (mode == ReadingMode.RIVER) "River" else "Unread counts") },
                            )
                        }
                    }
                    Text("Article visibility", style = MaterialTheme.typography.labelLarge)
                    SwitchRow(
                        text = "Show only unread",
                        contentDescription = "Show only unread",
                        description = "Read articles stay until you reopen or refresh the list. Front page, saved shelves and search include read articles.",
                        checked = settings.showOnlyUnread,
                        onCheckedChange = { value -> model.updateReaderSettingsDraft { it.copy(showOnlyUnread = value) } },
                    )
                    Text("River retention", style = MaterialTheme.typography.labelLarge)
                    Text("${settings.todayDecayDays} days", style = MaterialTheme.typography.bodyMedium)
                    Slider(
                        value = settings.todayDecayDays.toFloat(),
                        onValueChange = { value ->
                            model.updateReaderSettingsDraft { it.copy(todayDecayDays = value.roundToInt().toLong().coerceIn(1, 365)) }
                        },
                        valueRange = 1f..365f,
                        steps = 363,
                        modifier = Modifier.semantics { contentDescription = "River retention in days" },
                    )
                    Text(
                        "Cadence and retention apply to river lists. Front page always covers the past 24 hours or 7 days across your unmuted feeds.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Text("Shortcuts", style = MaterialTheme.typography.labelLarge)
                    SwitchRow(
                        text = "Single-key shortcuts (shared with web and Mac)",
                        contentDescription = "Single-key shortcuts",
                        checked = settings.singleKeyShortcuts,
                        onCheckedChange = { checked -> model.updateReaderSettingsDraft { it.copy(singleKeyShortcuts = checked) } },
                    )
                }

                SettingsGroup("Typography") {
                    Text("Article theme", style = MaterialTheme.typography.labelLarge)
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
                        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
                    ) {
                        ReaderTheme.entries.forEach { theme ->
                            FilterChip(
                                selected = settings.theme == theme,
                                onClick = { model.updateReaderSettingsDraft { it.copy(theme = theme) } },
                                label = { Text(theme.label()) },
                            )
                        }
                    }
                    Text("Measure", style = MaterialTheme.typography.labelLarge)
                    Text("${settings.measure}", style = MaterialTheme.typography.bodyMedium)
                    Slider(
                        value = settings.measure.toFloat(),
                        onValueChange = { value ->
                            model.updateReaderSettingsDraft { it.copy(measure = value.roundToInt().toLong().coerceIn(62, 72)) }
                        },
                        valueRange = 62f..72f,
                        steps = 9,
                        modifier = Modifier.semantics { contentDescription = "Measure" },
                    )
                    Text("Font", style = MaterialTheme.typography.labelLarge)
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
                        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
                    ) {
                        ReaderFont.entries.forEach { font ->
                            FilterChip(
                                selected = settings.font == font,
                                onClick = { model.updateReaderSettingsDraft { it.copy(font = font) } },
                                label = { Text(font.label()) },
                            )
                        }
                    }
                    Text("Preview", style = MaterialTheme.typography.labelLarge)
                    TypographyPreview(settings)
                    Text("Per-feed typography", style = MaterialTheme.typography.labelLarge)
                    Text(
                        "Overrides are keyed by feed ID and inherit the account setting for members left as Inherit.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    feeds.forEach { (_, feedId, name) ->
                        val override = settings.feedOverrides[feedId.toString()]
                        Column(
                            Modifier.fillMaxWidth().padding(vertical = ReaderSpace.xxs),
                            verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
                        ) {
                            Text("$name (feed $feedId)", style = MaterialTheme.typography.bodyMedium)
                            Text(
                                overrideSummary(override),
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            FlowRow(
                                horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
                                verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
                            ) {
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
                    }
                    TextButton(onClick = { addOverride = true }) { Text("Add override by feed ID") }
                }
            }

            SettingsGroup("Starred articles") {
                Text(
                    "Bring starred articles from another reader into Kept. Export them as Google Reader JSON, for example from FreshRSS or Inoreader.",
                    style = MaterialTheme.typography.bodyMedium,
                )
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 48.dp).toggleable(
                        value = subscribeMissing,
                        enabled = !keptImport.busy && !readingImport,
                        role = Role.Checkbox,
                        onValueChange = { subscribeMissing = it },
                    ),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Checkbox(
                        checked = subscribeMissing,
                        onCheckedChange = null,
                        enabled = !keptImport.busy && !readingImport,
                    )
                    Spacer(Modifier.width(ReaderSpace.xs))
                    Text(
                        "Also subscribe to feeds you don't follow",
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.weight(1f),
                    )
                }
                Button(
                    onClick = { importPicker.launch(arrayOf("application/json", "*/*")) },
                    enabled = !keptImport.busy && !readingImport,
                ) { Text("Import starred articles") }
                if (keptImport.busy || readingImport) {
                    Text(
                        "Importing starred articles…",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                keptImport.error?.let { NoticeBanner(it, tone = NoticeTone.ERROR) }
                keptImport.report?.let { report ->
                    KeptImportReportView(report = report, busy = keptImport.busy, onRetry = model::importKeptAgain)
                }
            }

            LlmServicesSection(model = model, state = state, modifier = Modifier.fillMaxWidth())
        }
        }
        Surface(color = MaterialTheme.colorScheme.surfaceContainerLow) {
            Row(
                Modifier.widthIn(max = ReaderSpace.contentWidth).fillMaxWidth().padding(ReaderSpace.md),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(ReaderSpace.md),
            ) {
                Column(Modifier.weight(1f)) {
                    Text("Reading preferences", style = MaterialTheme.typography.labelLarge)
                    if (!largeText || ui.saving || blocked || ui.draft != null) {
                        Text(
                            when {
                                ui.saving -> "Saving your preferences…"
                                blocked -> "Load settings to make changes"
                                ui.draft != null -> "Unsaved changes"
                                else -> "No unsaved changes"
                            },
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                Button(
                    onClick = model::saveReaderSettings,
                    enabled = !blocked && ui.draft != null && !ui.saving,
                    modifier = Modifier.testTag("reader-settings-save"),
                ) { Text("Save") }
            }
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
                Column(
                    Modifier.verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
                ) {
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

    if (confirmReload) {
        AlertDialog(
            onDismissRequest = { confirmReload = false },
            title = { Text("Reload settings?") },
            text = {
                Text(
                    "Reloading discards your local edits and pending save retry. Your server's settings will be loaded instead.",
                    modifier = Modifier.verticalScroll(rememberScrollState()),
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    confirmReload = false
                    model.reloadReaderSettings()
                }, enabled = !ui.loading && !ui.saving) { Text("Discard draft") }
            },
            dismissButton = { TextButton(onClick = { confirmReload = false }) { Text("Keep editing") } },
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

/** A full-width switch row: the label wraps and the switch stays reachable. */
@Composable
internal fun SwitchRow(
    text: String,
    contentDescription: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    description: String? = null,
) {
    val accessibilityLabel = contentDescription
    Row(
        Modifier.fillMaxWidth().heightIn(min = 48.dp)
            .toggleable(value = checked, role = Role.Switch, onValueChange = onCheckedChange)
            .semantics { this.contentDescription = accessibilityLabel }
            .padding(vertical = ReaderSpace.xxs),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs)) {
            Text(text, style = MaterialTheme.typography.bodyLarge)
            if (description != null) {
                Text(description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        Spacer(Modifier.width(ReaderSpace.sm))
        Switch(checked = checked, onCheckedChange = null)
    }
}

/** A quiet live sample of the draft's reader typography; no request is made. */
@Composable
private fun TypographyPreview(settings: ReaderSettings, modifier: Modifier = Modifier) {
    val systemDark = isSystemInDarkTheme()
    val (background, foreground) = previewColors(settings.theme, systemDark)
    val family = when (settings.font) {
        ReaderFont.SERIF -> FontFamily.Serif
        ReaderFont.SANS -> FontFamily.SansSerif
        ReaderFont.DYSLEXIC -> FontFamily.SansSerif
    }
    val letterSpacing = if (settings.font == ReaderFont.DYSLEXIC) 0.9.sp else 0.sp
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        color = background,
        contentColor = foreground,
    ) {
        Column(
            // The reader's measure is a character width, approximated here;
            // the parent's constraints cap it on a narrow screen.
            Modifier.width((settings.measure.toInt() * PREVIEW_CHAR_WIDTH_DP).dp)
                .padding(ReaderSpace.md),
            verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
        ) {
            Text(
                "The quick brown fox jumps over the lazy dog",
                style = MaterialTheme.typography.titleMedium.copy(
                    fontFamily = family,
                    fontSize = 18.sp,
                    lineHeight = 26.sp,
                    letterSpacing = letterSpacing,
                ),
            )
            Text(
                "A little space makes room for a good story. Settle into the articles you love, at your own pace.",
                style = MaterialTheme.typography.bodyMedium.copy(fontFamily = family, letterSpacing = letterSpacing),
            )
            Text(
                "${settings.measure} characters per line · ${settings.font.label()} · ${settings.theme.label()}",
                style = MaterialTheme.typography.labelSmall,
            )
        }
    }
}

/** The reader content palette, mirroring the offline reader CSS. */
private fun previewColors(theme: ReaderTheme, systemDark: Boolean): Pair<Color, Color> = when (theme) {
    ReaderTheme.SYSTEM -> if (systemDark) Color(0xFF242424) to Color(0xFFEEEEEE) else Color(0xFFFFFFFF) to Color(0xFF202020)
    ReaderTheme.LIGHT -> Color(0xFFFFFFFF) to Color(0xFF202020)
    ReaderTheme.DARK -> Color(0xFF242424) to Color(0xFFEEEEEE)
    ReaderTheme.BLACK -> Color(0xFF000000) to Color(0xFFFFFFFF)
    ReaderTheme.EINK -> Color(0xFFFFFFFF) to Color(0xFF000000)
}

/** The import report, worded the same way as the web client's. */
@Composable
private fun KeptImportReportView(report: KeptImportReport, busy: Boolean, onRetry: () -> Unit) {
    val summary = summarizeKeptImport(report)
    val total = summary.added + summary.existing
    Surface(
        Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(16.dp),
        color = MaterialTheme.colorScheme.surfaceContainerLow,
    ) {
        Column(Modifier.fillMaxWidth().padding(ReaderSpace.sm), verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs)) {
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
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun <T> ChipRow(options: List<Pair<T, String>>, selected: T?, onSelect: (T?) -> Unit) {
    FlowRow(
        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
    ) {
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
            Column(
                Modifier.verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
            ) {
                OutlinedTextField(
                    value = value,
                    onValueChange = { value = it },
                    label = { Text("Feed ID") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                )
            }
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

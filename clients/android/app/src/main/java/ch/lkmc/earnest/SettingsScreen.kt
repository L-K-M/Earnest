package ch.lkmc.earnest

import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ch.lkmc.earnest.core.KeptImportReport
import ch.lkmc.earnest.core.MAX_KEPT_IMPORT_BYTES
import ch.lkmc.earnest.core.ReaderFont
import ch.lkmc.earnest.core.ReaderOverride
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.ReaderTheme
import ch.lkmc.earnest.core.ReadingMode
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncObject
import ch.lkmc.earnest.core.iconUrl
import ch.lkmc.earnest.core.readAtMost
import ch.lkmc.earnest.core.summarizeKeptImport
import kotlin.math.roundToInt
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull

private const val PREVIEW_CHAR_WIDTH_DP = 8
private const val MIN_DECAY_DAYS = 1L
private const val MAX_DECAY_DAYS = 365L
private const val MIN_MEASURE = 62L
private const val MAX_MEASURE = 72L
private const val MEASURE_STEPS = 9
private const val LOAD_DISPATCH_GRACE_MS = 300L
private const val CATEGORY_PIN_MAX_FONT_SCALE = 1.5f
// Phones in landscape; the smallest portrait phones still pin the switch.
private val ShortSettingsHeight = 440.dp

private val OverrideAvatarSize = 32.dp
private val FeedPickerListHeight = 280.dp

/** The settings categories. Each owns one existing settings surface. */
internal enum class SettingsCategory(val label: String) {
    READING("Reading & appearance"),
    AI("AI"),
    IMPORT_EXPORT("Import & export"),
}

/**
 * The account settings, split into dedicated categories instead of one long
 * page. Opening a category loads its settings automatically; each category
 * owns its contextual Save, Discard and Retry actions; leaving a category or
 * Settings with an unsaved draft asks first.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
internal fun SettingsScreen(
    model: AppViewModel,
    state: UiState,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    appTheme: ThemeSetting? = null,
    onAppTheme: (ThemeSetting) -> Unit = {},
) {
    var category by rememberSaveable { mutableStateOf(SettingsCategory.READING) }
    val categoryScroll = key(category) { rememberScrollState() }
    var pendingCategory by remember { mutableStateOf<SettingsCategory?>(null) }
    var leaving by remember { mutableStateOf(false) }
    val readerUi = state.readerSettingsUi
    val llmUi = state.llmSettingsUi
    val dirty = readerUi.draft != null || llmUi.draft != null
    val readingImport = remember(state.accountKey) { mutableStateOf(false) }
    val importScope = rememberCoroutineScope()

    // Opening a category loads its settings once, unless a draft protects
    // unsaved edits. A failed load leaves the baseline absent, so reopening
    // tries again.
    LaunchedEffect(category, state.accountKey, readerUi.epoch, llmUi.epoch) {
        when (category) {
            SettingsCategory.READING -> if (readerUi.draft == null && readerUi.loaded == null) model.loadReaderSettings()
            SettingsCategory.AI -> if (llmUi.draft == null && llmUi.loaded == null) model.loadLlmSettings()
            SettingsCategory.IMPORT_EXPORT -> Unit
        }
    }

    fun discardCurrent() {
        when (category) {
            SettingsCategory.READING -> model.discardReaderSettingsDraft()
            SettingsCategory.AI -> model.discardLlmSettingsDraft()
            SettingsCategory.IMPORT_EXPORT -> Unit
        }
    }

    val guardOpen = pendingCategory != null || leaving
    if (!guardOpen) {
        BackHandler {
            if (dirty || readingImport.value) leaving = true else onBack()
        }
    }

    // A short window or very large text needs the height for the form itself.
    val density = LocalDensity.current
    val windowHeight = with(density) { LocalWindowInfo.current.containerSize.height.toDp() }
    val categoriesScroll = windowHeight < ShortSettingsHeight || density.fontScale > CATEGORY_PIN_MAX_FONT_SCALE
    val categorySwitch = @Composable {
        Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.TopCenter) {
            FlowRow(
                Modifier.widthIn(max = ReaderSpace.formWidth).fillMaxWidth()
                    .selectableGroup()
                    .padding(horizontal = ReaderSpace.md, vertical = ReaderSpace.xs),
                horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
                verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
            ) {
                SettingsCategory.entries.forEach { option ->
                    ReaderFilterChip(
                        selected = category == option,
                        onClick = {
                            if (option != category) {
                                if (dirty) pendingCategory = option else category = option
                            }
                        },
                        label = { Text(option.label) },
                    )
                }
            }
        }
    }

    Column(modifier.fillMaxSize()) {
        TopAppBar(
            title = {
                Text(
                    "Settings",
                    style = MaterialTheme.typography.headlineSmall,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.semantics { heading() },
                )
            },
            navigationIcon = {
                ActionIconButton(
                    EarnestIcons.Back,
                    "Back",
                    onClick = { if (dirty || readingImport.value) leaving = true else onBack() },
                )
            },
            colors = TopAppBarDefaults.topAppBarColors(containerColor = MaterialTheme.colorScheme.surface),
            // The parent scaffold already applies the window insets.
            windowInsets = WindowInsets(0, 0, 0, 0),
        )
        if (!categoriesScroll) categorySwitch()
        // The form scrolls edge to edge; the side padding lives inside the
        // scroll, so group surfaces keep their corners at the viewport edge.
        Column(Modifier.fillMaxWidth().weight(1f).verticalScroll(categoryScroll)) {
            if (categoriesScroll) categorySwitch()
            Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.TopCenter) {
                Column(
                    Modifier.widthIn(max = ReaderSpace.formWidth).fillMaxWidth()
                        .padding(start = ReaderSpace.md, top = ReaderSpace.xs, end = ReaderSpace.md, bottom = ReaderSpace.lg),
                ) {
                    when (category) {
                        SettingsCategory.READING -> ReadingSettingsCategory(model, state, appTheme, onAppTheme)
                        SettingsCategory.AI -> LlmServicesSection(
                            model = model,
                            state = state,
                            modifier = Modifier.padding(vertical = ReaderSpace.xs),
                        )
                        SettingsCategory.IMPORT_EXPORT -> ImportExportCategory(model, state, importScope, readingImport)
                    }
                }
            }
        }
        Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.TopCenter) {
            Box(Modifier.widthIn(max = ReaderSpace.formWidth).fillMaxWidth()) {
                when (category) {
                    SettingsCategory.READING -> ReadingSettingsActions(model, state)
                    SettingsCategory.AI -> LlmSettingsActions(model, state)
                    SettingsCategory.IMPORT_EXPORT -> Unit
                }
            }
        }
    }

    val pending = pendingCategory
    if (pending != null) {
        AlertDialog(
            onDismissRequest = { pendingCategory = null },
            title = { Text("Discard unsaved changes?") },
            text = {
                Text(
                    "The ${category.label} settings have edits that are not saved. " +
                        "Discard them to switch to ${pending.label}?",
                )
            },
            confirmButton = {
                QuietButton("Discard", onClick = {
                    discardCurrent()
                    pendingCategory = null
                    category = pending
                })
            },
            dismissButton = { QuietButton("Keep editing", onClick = { pendingCategory = null }) },
        )
    }

    if (leaving) {
        AlertDialog(
            onDismissRequest = { leaving = false },
            title = { Text("Discard unsaved changes?") },
            text = { Text(if (readingImport.value) "The file is still being read. Leaving cancels the import." else "The ${category.label} settings have edits that are not saved. Discard them and leave Settings?") },
            confirmButton = {
                QuietButton("Discard", onClick = {
                    discardCurrent()
                    leaving = false
                    onBack()
                })
            },
            dismissButton = { QuietButton("Keep editing", onClick = { leaving = false }) },
        )
    }
}

@Composable
private fun ReadingSettingsActions(model: AppViewModel, state: UiState) {
    val ui = state.readerSettingsUi
    val dirty = ui.draft != null
    SettingsActionBar(
        DraftActions(
            canSave = ui.loaded != null && state.snapshot.cursor.generation.isNotEmpty() && dirty && !ui.saving,
            canDiscard = dirty && !ui.saving,
            working = ui.loading || ui.saving,
            onSave = model::saveReaderSettings,
            onDiscard = model::discardReaderSettingsDraft,
            onRetry = if (ui.frozen != null) model::retryReaderSettingsSave else null,
            canRetry = !ui.saving,
        ),
        saveModifier = Modifier.testTag("reader-settings-save"),
    )
}

/** The reader preferences, with contextual Save, Discard and Retry. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ReadingSettingsCategory(model: AppViewModel, state: UiState, appTheme: ThemeSetting?, onAppTheme: (ThemeSetting) -> Unit) {
    val ui = state.readerSettingsUi
    val settings = ui.draft ?: ui.loaded ?: state.readerSettings
    val feeds = remember(state.snapshot) {
        state.snapshot.objects
            .filter { it.kind == SyncEngine.OBJECT_SUBSCRIPTION && !it.deleted }
            .mapNotNull { obj -> obj.feedId()?.let { FeedChoice(it, obj.name, obj.iconUrl()) } }
            .sortedBy { it.name.lowercase() }
    }
    var chooseFeed by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }
    var overrideFeedId by remember(state.accountKey, ui.epoch) { mutableStateOf<Long?>(null) }
    var overrideTheme by remember(state.accountKey, ui.epoch) { mutableStateOf<ReaderTheme?>(null) }
    var overrideMeasureInput by remember(state.accountKey, ui.epoch) { mutableStateOf(OverrideMeasureInput.fromValue(null)) }
    var overrideFont by remember(state.accountKey, ui.epoch) { mutableStateOf<ReaderFont?>(null) }

    fun openOverride(feedId: Long) {
        val existing = settings.feedOverrides[feedId.toString()]
        overrideFeedId = feedId
        overrideTheme = existing?.theme
        overrideMeasureInput = OverrideMeasureInput.fromValue(existing?.measure)
        overrideFont = existing?.font
    }

    Column(Modifier.fillMaxWidth()) {
        val synced = state.snapshot.cursor.generation.isNotEmpty()
        if (ui.loaded == null || !synced) {
            val loadError = ui.error
            when {
                ui.loading -> LoadingState("Loading reading settings…")
                loadError != null -> QuietState(
                    "Settings did not load",
                    loadError,
                    actionLabel = "Retry loading",
                    onAction = model::loadReaderSettings,
                )
                !synced -> QuietState(
                    "Sync your account first",
                    "Reading preferences can be edited here after the first sync.",
                )
                else -> LoadNotStarted("reading settings", model::loadReaderSettings)
            }
            return@Column
        }

        ui.error?.let { NoticeBanner(it, tone = NoticeTone.ERROR) }
        if (ui.success) NoticeBanner("Saved", tone = NoticeTone.SUCCESS)

        SettingsGroup("Reading preferences") {
            Text(
                "Reading preferences sync across your devices.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text("Reading mode", style = MaterialTheme.typography.labelLarge)
            Column(Modifier.fillMaxWidth().selectableGroup()) {
                OptionRow(
                    label = "River",
                    selected = settings.readingMode == ReadingMode.RIVER,
                    onSelect = { model.updateReaderSettingsDraft { it.copy(readingMode = ReadingMode.RIVER) } },
                    description = "A river that ends. Resume where you left off; what you skip stays searchable.",
                )
                OptionRow(
                    label = "Unread counts",
                    selected = settings.readingMode == ReadingMode.UNREAD_COUNTS,
                    onSelect = { model.updateReaderSettingsDraft { it.copy(readingMode = ReadingMode.UNREAD_COUNTS) } },
                    description = "Every feed keeps a count until you clear it.",
                )
            }
            Text("Article visibility", style = MaterialTheme.typography.labelLarge)
            SwitchRow(
                text = "Show only unread",
                contentDescription = "Show only unread",
                description = "Read articles stay until you reopen or refresh the list. Front page, saved shelves and search include read articles.",
                checked = settings.showOnlyUnread,
                onCheckedChange = { value -> model.updateReaderSettingsDraft { it.copy(showOnlyUnread = value) } },
            )
            RiverRetention(
                days = settings.todayDecayDays,
                onDays = { days -> model.updateReaderSettingsDraft { it.copy(todayDecayDays = days) } },
            )
            Text(
                "Cadence and retention apply to river lists. Front page always covers the past 24 hours or 7 days across your unmuted feeds.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text("Shortcuts", style = MaterialTheme.typography.labelLarge)
            SwitchRow(
                text = "Single-key shortcuts",
                contentDescription = "Single-key shortcuts",
                description = "Shared with web and Mac.",
                checked = settings.singleKeyShortcuts,
                onCheckedChange = { checked -> model.updateReaderSettingsDraft { it.copy(singleKeyShortcuts = checked) } },
            )
        }

        if (appTheme != null) {
            SettingsGroup("App appearance") {
                Text(
                    "The app theme stays on this device.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                ThemeOptions(appTheme, onAppTheme)
            }
        }

        SettingsGroup("Typography") {
            Text("Article theme", style = MaterialTheme.typography.labelLarge)
            FlowRow(
                Modifier.selectableGroup(),
                horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
                verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
            ) {
                ReaderTheme.entries.forEach { theme ->
                    ReaderFilterChip(
                        selected = settings.theme == theme,
                        onClick = { model.updateReaderSettingsDraft { it.copy(theme = theme) } },
                        label = { Text(theme.label()) },
                    )
                }
            }
            Text(
                "\"System\" follows your device, not the app theme.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            LineLengthSetting(
                measure = settings.measure,
                onMeasure = { measure -> model.updateReaderSettingsDraft { it.copy(measure = measure) } },
            )
            Text("Font", style = MaterialTheme.typography.labelLarge)
            ChoiceGroup(
                options = ReaderFont.entries,
                selected = settings.font,
                onSelect = { font -> model.updateReaderSettingsDraft { it.copy(font = font) } },
                label = { it.label() },
            )
            Text("Preview", style = MaterialTheme.typography.labelLarge)
            TypographyPreview(settings)
        }

        SettingsGroup("Per-feed typography") {
            Text(
                "A feed can keep its own theme, line length and font. Anything left as Same as account follows your account settings.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            val overrides = settings.feedOverrides.entries
                .mapNotNull { (key, override) -> key.toLongOrNull()?.let { it to override } }
                .sortedBy { feedTitle(feeds, it.first).lowercase() }
            if (overrides.isEmpty()) {
                Text(
                    "No feeds have their own typography yet.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            overrides.forEach { (feedId, override) ->
                OverrideRow(
                    title = feedTitle(feeds, feedId),
                    iconUrl = feeds.firstOrNull { it.feedId == feedId }?.iconUrl,
                    summary = overrideSummary(override),
                    onEdit = { openOverride(feedId) },
                    onReset = {
                        model.updateReaderSettingsDraft { it.copy(feedOverrides = it.feedOverrides - feedId.toString()) }
                    },
                )
            }
            SecondaryButton("Choose a feed", onClick = { chooseFeed = true })
        }
    }

    if (chooseFeed) {
        FeedOverridePickerDialog(
            feeds = feeds,
            onDismiss = { chooseFeed = false },
            onChoose = { feedId ->
                chooseFeed = false
                openOverride(feedId)
            },
        )
    }

    val editingFeed = overrideFeedId
    if (editingFeed != null) {
        FeedOverrideDialog(
            title = feedTitle(feeds, editingFeed),
            theme = overrideTheme,
            onTheme = { overrideTheme = it },
            measureInput = overrideMeasureInput,
            onMeasureInput = { overrideMeasureInput = it },
            font = overrideFont,
            onFont = { overrideFont = it },
            onDismiss = { overrideFeedId = null },
            onApply = {
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
            },
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
            confirmButton = { QuietButton("Reload", onClick = model::reloadReaderSettings) },
            dismissButton = {
                QuietButton("Keep editing", onClick = model::dismissReaderSettingsConflict)
            },
        )
    }
}

/**
 * A load is dispatched from an effect, so "not loading" is normal for a moment.
 * A load that never starts (no verified session yet) reports outside this
 * screen's state, so after that moment this offers the load again.
 */
@Composable
internal fun LoadNotStarted(what: String, onLoad: () -> Unit) {
    var waited by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        delay(LOAD_DISPATCH_GRACE_MS)
        waited = true
    }
    if (!waited) {
        LoadingState("Loading $what…")
        return
    }
    QuietState(
        "Settings are not loaded",
        "Load your $what to edit them.",
        actionLabel = "Load settings",
        onAction = onLoad,
    )
}

/** The days a river keeps articles, with steppers beside a free slider. */
@Composable
private fun RiverRetention(days: Long, onDays: (Long) -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        Text("River retention", style = MaterialTheme.typography.labelLarge)
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            ActionIconButton(
                EarnestIcons.Remove,
                "Keep one day fewer",
                onClick = { onDays(days - 1) },
                enabled = days > MIN_DECAY_DAYS,
            )
            Text(
                "$days ${if (days == 1L) "day" else "days"}",
                style = MaterialTheme.typography.titleMedium,
                textAlign = TextAlign.Center,
                modifier = Modifier.weight(1f),
            )
            ActionIconButton(
                EarnestIcons.Add,
                "Keep one day more",
                onClick = { onDays(days + 1) },
                enabled = days < MAX_DECAY_DAYS,
            )
        }
        Slider(
            value = days.toFloat().coerceIn(MIN_DECAY_DAYS.toFloat(), MAX_DECAY_DAYS.toFloat()),
            onValueChange = { value ->
                onDays(value.roundToInt().toLong().coerceIn(MIN_DECAY_DAYS, MAX_DECAY_DAYS))
            },
            valueRange = MIN_DECAY_DAYS.toFloat()..MAX_DECAY_DAYS.toFloat(),
            steps = 0,
            colors = readerSliderColors(),
            modifier = Modifier.semantics { contentDescription = "River retention in days" },
        )
    }
}

/** The article column width in characters, the measure in the synced settings. */
@Composable
private fun LineLengthSetting(measure: Long, onMeasure: (Long) -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("Line length", style = MaterialTheme.typography.labelLarge, modifier = Modifier.weight(1f))
            Text(
                "$measure characters",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Slider(
            value = measure.toFloat().coerceIn(MIN_MEASURE.toFloat(), MAX_MEASURE.toFloat()),
            onValueChange = { value ->
                onMeasure(value.roundToInt().toLong().coerceIn(MIN_MEASURE, MAX_MEASURE))
            },
            valueRange = MIN_MEASURE.toFloat()..MAX_MEASURE.toFloat(),
            steps = MEASURE_STEPS,
            colors = readerSliderColors(),
            modifier = Modifier.semantics { contentDescription = "Line length" },
        )
    }
}

@Composable
private fun readerSliderColors() = SliderDefaults.colors(
    inactiveTrackColor = MaterialTheme.colorScheme.surfaceContainerHighest,
)

/** A subscription a typography override can be keyed on. */
private data class FeedChoice(val feedId: Long, val name: String, val iconUrl: String?)

private fun feedTitle(feeds: List<FeedChoice>, feedId: Long): String =
    feeds.firstOrNull { it.feedId == feedId }?.name?.ifBlank { "Untitled feed" } ?: "Feed $feedId"

@Composable
private fun OverrideRow(title: String, iconUrl: String?, summary: String, onEdit: () -> Unit, onReset: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = TouchTarget)
            .clickable(onClickLabel = "Edit", role = Role.Button, onClick = onEdit),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
    ) {
        FeedAvatar(title, iconUrl, size = OverrideAvatarSize)
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(summary, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        QuietButton("Reset", onClick = onReset)
    }
}

/** Picks the feed a new override belongs to, by name or by bare ID. */
@Composable
private fun FeedOverridePickerDialog(feeds: List<FeedChoice>, onDismiss: () -> Unit, onChoose: (Long) -> Unit) {
    var query by remember { mutableStateOf("") }
    var byId by remember { mutableStateOf(false) }
    var idText by remember { mutableStateOf("") }
    val feedId = idText.trim().toLongOrNull()?.takeIf { it > 0 && it.toString() == idText.trim() }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Choose a feed") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
                if (byId) {
                    OutlinedTextField(
                        value = idText,
                        onValueChange = { idText = it },
                        label = { Text("Feed ID") },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    QuietButton("Back to the feed list", onClick = { byId = false })
                } else {
                    OutlinedTextField(
                        value = query,
                        onValueChange = { query = it },
                        label = { Text("Search feeds") },
                        leadingIcon = { Icon(EarnestIcons.Search, contentDescription = null) },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    val shown = feeds.filter { it.name.contains(query.trim(), ignoreCase = true) }
                    if (feeds.isEmpty()) {
                        Text(
                            "No feeds yet. You can still enter a feed ID.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    } else if (shown.isEmpty()) {
                        Text(
                            "No feed matches that name.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    } else {
                        LazyColumn(
                            Modifier.fillMaxWidth().weight(1f, fill = false).heightIn(max = FeedPickerListHeight),
                        ) {
                            items(shown) { feed ->
                                Row(
                                    Modifier.fillMaxWidth().heightIn(min = TouchTarget)
                                        .clickable(onClickLabel = "Choose ${feed.name}") { onChoose(feed.feedId) }
                                        .padding(vertical = ReaderSpace.xxs),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
                                ) {
                                    FeedAvatar(feed.name, feed.iconUrl, size = OverrideAvatarSize)
                                    Text(feed.name.ifBlank { "Untitled feed" }, style = MaterialTheme.typography.titleMedium)
                                }
                            }
                        }
                    }
                    QuietButton("Enter a feed ID", onClick = { byId = true })
                }
            }
        },
        confirmButton = {
            if (byId) {
                QuietButton("Add", onClick = { feedId?.let(onChoose) }, enabled = feedId != null)
            }
        },
        dismissButton = { QuietButton("Cancel", onClick = onDismiss) },
    )
}

/** One feed's typography; any member left out inherits the account setting. */
@Composable
private fun FeedOverrideDialog(
    title: String,
    theme: ReaderTheme?,
    onTheme: (ReaderTheme?) -> Unit,
    measureInput: OverrideMeasureInput,
    onMeasureInput: (OverrideMeasureInput) -> Unit,
    font: ReaderFont?,
    onFont: (ReaderFont?) -> Unit,
    onDismiss: () -> Unit,
    onApply: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title, maxLines = 2, overflow = TextOverflow.Ellipsis) },
        text = {
            Column(
                Modifier.fillMaxWidth().verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
            ) {
                Text("Theme", style = MaterialTheme.typography.labelLarge)
                ChipRow(
                    options = ReaderTheme.entries.map { it to it.label() },
                    selected = theme,
                    onSelect = onTheme,
                )
                OutlinedTextField(
                    value = measureInput.text,
                    onValueChange = { onMeasureInput(measureInput.edit(it)) },
                    label = { Text("Line length 62-72 (blank for account)") },
                    isError = !measureInput.canApply,
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    modifier = Modifier.fillMaxWidth(),
                )
                Text("Font", style = MaterialTheme.typography.labelLarge)
                ChipRow(
                    options = ReaderFont.entries.map { it to it.label() },
                    selected = font,
                    onSelect = onFont,
                )
            }
        },
        confirmButton = { QuietButton("Apply", onClick = onApply, enabled = measureInput.canApply) },
        dismissButton = { QuietButton("Cancel", onClick = onDismiss) },
    )
}

/** The starred-article import, owned by the Import & export category. */
@Composable
private fun ImportExportCategory(model: AppViewModel, state: UiState, importScope: CoroutineScope, reading: MutableState<Boolean>) {
    val keptImport = state.keptImport
    var subscribeMissing by remember(state.accountKey) { mutableStateOf(false) }
    var readingImport by reading
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
    val busy = keptImport.busy || readingImport

    Column(Modifier.fillMaxWidth()) {
        SettingsGroup("Starred articles") {
            Text(
                "Bring starred articles from another reader into Kept. Export them as Google Reader JSON, for example from FreshRSS or Inoreader.",
                style = MaterialTheme.typography.bodyMedium,
            )
            Row(
                Modifier.fillMaxWidth().heightIn(min = TouchTarget).toggleable(
                    value = subscribeMissing,
                    enabled = !busy,
                    role = Role.Checkbox,
                    onValueChange = { subscribeMissing = it },
                ),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
            ) {
                Checkbox(
                    checked = subscribeMissing,
                    onCheckedChange = null,
                    enabled = !busy,
                )
                Text("Also subscribe to feeds you don't follow")
            }
            PrimaryButton(
                "Import starred articles",
                onClick = { importPicker.launch(arrayOf("application/json", "*/*")) },
                enabled = !busy,
                busy = busy,
                icon = EarnestIcons.Import,
            )
            keptImport.error?.let { NoticeBanner(it, tone = NoticeTone.ERROR) }
            keptImport.report?.let { report ->
                KeptImportReportView(report = report, busy = keptImport.busy, onRetry = model::importKeptAgain)
            }
        }
        SettingsGroup("Subscriptions") {
            Text(
                "OPML import and export stay on the Feeds screen, next to the subscription list.",
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
}

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
        Modifier.fillMaxWidth().heightIn(min = TouchTarget)
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
    val scheme = MaterialTheme.colorScheme
    val family = when (settings.font) {
        ReaderFont.SERIF -> FontFamily.Serif
        ReaderFont.SANS -> FontFamily.SansSerif
        ReaderFont.DYSLEXIC -> FontFamily.SansSerif
    }
    val letterSpacing = if (settings.font == ReaderFont.DYSLEXIC) 0.9.sp else 0.sp
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.large,
        color = background,
        contentColor = foreground,
        // A canvas that matches the group's surface needs an edge to read.
        border = if (background == scheme.groupContainer || scheme.isMonochrome) {
            BorderStroke(1.dp, scheme.outlineVariant)
        } else {
            null
        },
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
                style = MaterialTheme.typography.titleLarge.copy(
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

/** The import report: a success headline, then the detail in quiet lines. */
@Composable
private fun KeptImportReportView(report: KeptImportReport, busy: Boolean, onRetry: () -> Unit) {
    val summary = summarizeKeptImport(report)
    val total = summary.added + summary.existing
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs)) {
        NoticeBanner(
            "$total ${if (total == 1) "article" else "articles"} in Kept: " +
                "${summary.added} added from the export, ${summary.existing} already in Earnest.",
            tone = NoticeTone.SUCCESS,
        )
        if (summary.subscribed > 0) {
            ReportLine(
                "Subscribed to ${summary.subscribed} ${if (summary.subscribed == 1) "feed" else "feeds"}. " +
                    "Their first fetches run in the background.",
            )
        }
        if (summary.labelled > 0) {
            ReportLine("Added the export's labels to ${summary.labelled} ${if (summary.labelled == 1) "article" else "articles"}.")
        }
        if (summary.unkept > 0) {
            ReportLine("${summary.unkept} left out because you removed ${if (summary.unkept == 1) "it" else "them"} from Kept.")
        }
        if (summary.gone > 0) ReportLine("${summary.gone} no longer available in Earnest.")
        if (summary.skipped.isNotEmpty()) {
            val skippedTotal = summary.skipped.sumOf { it.count }
            ReportLine("$skippedTotal skipped because you don't follow their feed:")
            summary.skipped.forEach { skip ->
                ReportLine("${skip.feedUrl.ifBlank { "Unknown feed" }} (${skip.count})")
            }
            QuietButton("Subscribe to these feeds and import again", onClick = onRetry, enabled = !busy)
        }
        if (summary.failed.isNotEmpty()) {
            NoticeBanner(
                "${summary.failed.size} could not be imported: ${summary.failed.joinToString("; ")}",
                tone = NoticeTone.ERROR,
            )
        }
    }
}

@Composable
private fun ReportLine(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun <T> ChipRow(options: List<Pair<T, String>>, selected: T?, onSelect: (T?) -> Unit) {
    FlowRow(
        Modifier.selectableGroup(),
        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
    ) {
        ReaderFilterChip(
            selected = selected == null,
            onClick = { onSelect(null) },
            label = { Text("Same as account") },
        )
        options.forEach { (value, label) ->
            ReaderFilterChip(
                selected = selected == value,
                onClick = { onSelect(value) },
                label = { Text(label) },
            )
        }
    }
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

private fun overrideSummary(override: ReaderOverride): String {
    val parts = mutableListOf<String>()
    override.theme?.let { parts += "theme ${it.label()}" }
    override.measure?.let { parts += "line length $it" }
    override.font?.let { parts += "font ${it.label()}" }
    if (parts.isEmpty()) return "Same as account"
    return parts.joinToString(", ").replaceFirstChar { it.uppercase() }
}

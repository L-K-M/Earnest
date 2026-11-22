package ch.lkmc.earnest

import androidx.compose.ui.platform.LocalDensity
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/** What the pinned bar offers for a category's draft. */
internal data class DraftActions(
    val canSave: Boolean,
    val canDiscard: Boolean,
    val working: Boolean,
    val onSave: () -> Unit,
    val onDiscard: () -> Unit,
    /** Present only while a failed save is frozen for an identical retry. */
    val onRetry: (() -> Unit)? = null,
    val canRetry: Boolean = !working,
)

/** Every category with a draft saves from this one place, below its scrolling form. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun SettingsActionBar(actions: DraftActions, modifier: Modifier = Modifier, saveModifier: Modifier = Modifier) {
    val scheme = MaterialTheme.colorScheme
    // At large text its line would double the bar's height over the form.
    val showStatus = LocalDensity.current.fontScale <= DRAFT_STATUS_MAX_FONT_SCALE
    Surface(
        modifier.fillMaxWidth().padding(horizontal = ReaderSpace.sm, vertical = ReaderSpace.xs),
        shape = MaterialTheme.shapes.extraLarge,
        color = scheme.surfaceContainerHigh,
        border = if (scheme.isMonochrome) BorderStroke(1.dp, scheme.outline) else null,
    ) {
        FlowRow(
            Modifier.fillMaxWidth().padding(ReaderSpace.xs),
            horizontalArrangement = if (showStatus) Arrangement.SpaceBetween else Arrangement.End,
            itemVerticalAlignment = Alignment.CenterVertically,
        ) {
            if (showStatus) {
                Text(
                    draftStatus(actions),
                    style = MaterialTheme.typography.bodyMedium,
                    color = scheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = ReaderSpace.sm),
                )
            }
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs, Alignment.End),
                itemVerticalAlignment = Alignment.CenterVertically,
            ) {
                QuietButton("Discard", actions.onDiscard, enabled = actions.canDiscard)
                actions.onRetry?.let { QuietButton("Retry save", it, enabled = actions.canRetry) }
                PrimaryButton("Save", actions.onSave, saveModifier, enabled = actions.canSave, busy = actions.working)
            }
        }
    }
}

private const val DRAFT_STATUS_MAX_FONT_SCALE = 1.3f

private fun draftStatus(actions: DraftActions): String = when {
    actions.working -> "Working…"
    actions.canDiscard -> "Unsaved changes"
    else -> "No changes"
}

/** The AI category's bar: discarding over a server conflict reloads, after asking. */
@Composable
internal fun LlmSettingsActions(model: AppViewModel, state: UiState, modifier: Modifier = Modifier) {
    val ui = state.llmSettingsUi
    val dirty = ui.draft != null
    var confirmReload by remember(state.accountKey, ui.epoch) { mutableStateOf(false) }
    SettingsActionBar(
        DraftActions(
            canSave = dirty && !ui.loading && !ui.saving && !ui.conflict,
            canDiscard = dirty && !ui.saving,
            working = ui.loading || ui.saving,
            onSave = model::saveLlmSettings,
            onDiscard = { if (ui.conflict) confirmReload = true else model.discardLlmSettingsDraft() },
            onRetry = if (ui.frozen != null && !ui.conflict) model::retryLlmSettingsSave else null,
        ),
        modifier,
    )
    if (confirmReload) {
        ReloadLlmSettingsDialog(
            onDismiss = { confirmReload = false },
            onReload = {
                confirmReload = false
                model.reloadLlmSettings()
            },
        )
    }
}

@Composable
internal fun ReloadLlmSettingsDialog(onDismiss: () -> Unit, onReload: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Reload AI settings?") },
        text = { Text("Your edits will be replaced after the latest settings load successfully.") },
        confirmButton = { QuietButton("Reload", onReload) },
        dismissButton = { QuietButton("Keep editing", onDismiss) },
    )
}

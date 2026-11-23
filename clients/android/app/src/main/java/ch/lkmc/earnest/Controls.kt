package ch.lkmc.earnest

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ButtonGroupDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MenuDefaults
import androidx.compose.material3.PlainTooltip
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.ToggleButton
import androidx.compose.material3.ToggleButtonColors
import androidx.compose.material3.ToggleButtonDefaults
import androidx.compose.material3.TooltipAnchorPosition
import androidx.compose.material3.TooltipBox
import androidx.compose.material3.TooltipDefaults
import androidx.compose.material3.rememberTooltipState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp

/*
 * One vocabulary for actions. A screen has at most one filled button, tonal
 * buttons are its alternatives, and text buttons are for what sits quietly
 * beside content. Icon-only buttons always have a label and a tooltip.
 */

/** The smallest thing a finger is asked to hit. */
internal val TouchTarget = 48.dp

private val ProminentHeight = 56.dp
private val ButtonIconSize = 20.dp
private val MenuMinWidth = 208.dp

/** Beyond this text size, more than two choices no longer share one row. */
private const val CONNECTED_CHOICE_MAX_FONT_SCALE = 1.3f
private const val MAX_CONNECTED_CHOICES_AT_LARGE_TEXT = 2

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun PrimaryButton(
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    busy: Boolean = false,
    icon: ImageVector? = null,
) {
    Button(
        onClick = onClick,
        shapes = ButtonDefaults.shapes(),
        enabled = enabled,
        modifier = modifier.heightIn(min = ProminentHeight),
        contentPadding = PaddingValues(horizontal = ReaderSpace.lg, vertical = ReaderSpace.sm),
    ) {
        ButtonLabel(label, icon, busy)
    }
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun SecondaryButton(
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    busy: Boolean = false,
    icon: ImageVector? = null,
) {
    FilledTonalButton(
        onClick = onClick,
        shapes = ButtonDefaults.shapes(),
        enabled = enabled,
        modifier = modifier.heightIn(min = TouchTarget),
        contentPadding = PaddingValues(horizontal = ReaderSpace.gutter, vertical = ReaderSpace.xs),
    ) {
        ButtonLabel(label, icon, busy)
    }
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun QuietButton(
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    icon: ImageVector? = null,
    color: Color = MaterialTheme.colorScheme.primary,
) {
    TextButton(
        onClick = onClick,
        shapes = ButtonDefaults.shapes(),
        enabled = enabled,
        modifier = modifier.heightIn(min = TouchTarget),
        colors = ButtonDefaults.textButtonColors(contentColor = color),
        contentPadding = PaddingValues(horizontal = ReaderSpace.sm, vertical = ReaderSpace.xs),
    ) {
        ButtonLabel(label, icon, busy = false)
    }
}

@Composable
private fun ButtonLabel(label: String, icon: ImageVector?, busy: Boolean) {
    if (busy) {
        CircularProgressIndicator(Modifier.size(ButtonIconSize), color = LocalContentColor.current, strokeWidth = 2.dp)
        Spacer(Modifier.width(ReaderSpace.xs))
    } else if (icon != null) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(ButtonIconSize))
        Spacer(Modifier.width(ReaderSpace.xs))
    }
    Text(label, textAlign = TextAlign.Center)
}

/** An icon-only action: it names itself to TalkBack and on a long press. */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun ActionIconButton(
    icon: ImageVector,
    description: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    tint: Color = LocalContentColor.current,
) {
    TooltipBox(
        positionProvider = TooltipDefaults.rememberTooltipPositionProvider(TooltipAnchorPosition.Below),
        tooltip = { PlainTooltip { Text(description) } },
        state = rememberTooltipState(),
    ) {
        IconButton(
            onClick = onClick,
            shapes = IconButtonDefaults.shapes(),
            enabled = enabled,
            modifier = modifier,
            colors = IconButtonDefaults.iconButtonColors(contentColor = tint),
        ) {
            Icon(icon, contentDescription = description)
        }
    }
}

/**
 * One choice from a few, drawn as a connected group that morphs on selection.
 * Large text turns a longer group into chips that wrap, so no label is cut.
 */
@OptIn(ExperimentalMaterial3ExpressiveApi::class, ExperimentalLayoutApi::class)
@Composable
internal fun <T> ChoiceGroup(
    options: List<T>,
    selected: T,
    onSelect: (T) -> Unit,
    label: (T) -> String,
    modifier: Modifier = Modifier,
    supporting: ((T) -> String)? = null,
    icon: ((T) -> ImageVector)? = null,
) {
    val wrap = options.size > MAX_CONNECTED_CHOICES_AT_LARGE_TEXT &&
        LocalDensity.current.fontScale > CONNECTED_CHOICE_MAX_FONT_SCALE
    if (wrap) {
        FlowRow(
            modifier.fillMaxWidth().selectableGroup(),
            horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
        ) {
            options.forEach { option ->
                ReaderFilterChip(
                    selected = option == selected,
                    onClick = { if (option != selected) onSelect(option) },
                    label = { Text(label(option)) },
                )
            }
        }
        return
    }
    Row(
        modifier.fillMaxWidth().height(IntrinsicSize.Min).selectableGroup(),
        horizontalArrangement = Arrangement.spacedBy(ButtonGroupDefaults.ConnectedSpaceBetween),
    ) {
        options.forEachIndexed { index, option ->
            val checked = option == selected
            ToggleButton(
                checked = checked,
                // A group always has one choice, so tapping the chosen one changes nothing.
                onCheckedChange = { if (!checked) onSelect(option) },
                modifier = Modifier.weight(1f).fillMaxHeight().heightIn(min = TouchTarget).semantics {
                    role = Role.RadioButton
                    this.selected = checked
                },
                shapes = when (index) {
                    0 -> ButtonGroupDefaults.connectedLeadingButtonShapes()
                    options.lastIndex -> ButtonGroupDefaults.connectedTrailingButtonShapes()
                    else -> ButtonGroupDefaults.connectedMiddleButtonShapes()
                },
                colors = choiceColors(),
                contentPadding = PaddingValues(horizontal = ReaderSpace.xs, vertical = ReaderSpace.xs),
            ) {
                icon?.let {
                    Icon(it(option), contentDescription = null, modifier = Modifier.size(ButtonIconSize))
                    Spacer(Modifier.width(ReaderSpace.xs))
                }
                Column(Modifier.weight(1f, fill = false), horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(label(option), style = MaterialTheme.typography.labelLarge, textAlign = TextAlign.Center)
                    supporting?.let {
                        Text(it(option), style = MaterialTheme.typography.labelSmall, textAlign = TextAlign.Center)
                    }
                }
            }
        }
    }
}

/** A selection is peach and an action teal. E-paper gets solid black instead. */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun choiceColors(): ToggleButtonColors {
    val scheme = MaterialTheme.colorScheme
    if (scheme.isMonochrome) return ToggleButtonDefaults.colors()
    return ToggleButtonDefaults.colors(
        checkedContainerColor = scheme.secondaryContainer,
        checkedContentColor = scheme.onSecondaryContainer,
    )
}

@Composable
internal fun AppMenu(
    expanded: Boolean,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    DropdownMenu(
        expanded = expanded,
        onDismissRequest = onDismiss,
        modifier = modifier.widthIn(min = MenuMinWidth),
        shape = MaterialTheme.shapes.medium,
        containerColor = MaterialTheme.colorScheme.surfaceContainerHigh,
        content = content,
    )
}

internal enum class MenuRole { NORMAL, DESTRUCTIVE }

@Composable
internal fun AppMenuItem(
    label: String,
    onClick: () -> Unit,
    icon: ImageVector? = null,
    role: MenuRole = MenuRole.NORMAL,
    enabled: Boolean = true,
    supporting: String? = null,
    selected: Boolean? = null,
) {
    val scheme = MaterialTheme.colorScheme
    val colors = if (role == MenuRole.DESTRUCTIVE) {
        MenuDefaults.itemColors(textColor = scheme.error, leadingIconColor = scheme.error)
    } else {
        MenuDefaults.itemColors()
    }
    DropdownMenuItem(
        text = {
            Column(Modifier.padding(vertical = ReaderSpace.xxs)) {
                Text(label)
                supporting?.let {
                    Text(it, style = MaterialTheme.typography.bodySmall, color = scheme.onSurfaceVariant)
                }
            }
        },
        onClick = onClick,
        enabled = enabled,
        colors = colors,
        modifier = if (selected == null) Modifier else Modifier.semantics { this.selected = selected },
        leadingIcon = icon?.let { { Icon(it, contentDescription = null) } },
        trailingIcon = if (selected == true) {
            { Icon(EarnestIcons.Check, contentDescription = null, tint = scheme.primary) }
        } else {
            null
        },
    )
}

/** A radio choice whose whole row is the target. */
@Composable
internal fun OptionRow(
    label: String,
    selected: Boolean,
    onSelect: () -> Unit,
    modifier: Modifier = Modifier,
    description: String? = null,
    enabled: Boolean = true,
    leading: (@Composable () -> Unit)? = null,
) {
    Row(
        modifier.fillMaxWidth().heightIn(min = ProminentHeight)
            .selectable(selected = selected, enabled = enabled, role = Role.RadioButton, onClick = onSelect)
            .padding(vertical = ReaderSpace.xs),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(ReaderSpace.md),
    ) {
        leading?.invoke()
        Column(Modifier.weight(1f)) {
            Text(label, style = MaterialTheme.typography.titleMedium)
            description?.let {
                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        RadioButton(selected = selected, onClick = null, enabled = enabled)
    }
}

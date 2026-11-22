package ch.lkmc.earnest

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ContainedLoadingIndicator
import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.FilterChip as MaterialFilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialShapes
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.toShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp

/** Shared rhythm for chrome, lists, forms and transient states. */
internal object ReaderSpace {
    val xxs = 4.dp
    val xs = 8.dp
    val sm = 12.dp
    val md = 16.dp
    val gutter = 20.dp
    val lg = 24.dp
    val xl = 32.dp
    val xxl = 48.dp
    val contentWidth = 720.dp
    val formWidth = 560.dp
    val paperWidth = 1100.dp
}

private val GroupOuterCorner = 20.dp
private val GroupInnerCorner = 4.dp

/** Rows of one group share its outer corners and keep tight inner ones. */
internal fun groupedShape(index: Int, count: Int): RoundedCornerShape {
    val top = if (index == 0) GroupOuterCorner else GroupInnerCorner
    val bottom = if (index == count - 1) GroupOuterCorner else GroupInnerCorner
    return RoundedCornerShape(topStart = top, topEnd = top, bottomStart = bottom, bottomEnd = bottom)
}

/** Keep a scaled label inside its chip's surface, not just its touch target. */
@Composable
internal fun ReaderFilterChip(
    selected: Boolean,
    onClick: () -> Unit,
    label: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val labelHeight = with(LocalDensity.current) { MaterialTheme.typography.labelLarge.lineHeight.toDp() }
    MaterialFilterChip(
        selected = selected,
        onClick = onClick,
        label = label,
        enabled = enabled,
        modifier = modifier.heightIn(min = maxOf(32.dp, labelHeight + ReaderSpace.sm)),
    )
}

@Composable
internal fun ScreenIntro(title: String, description: String, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth().padding(vertical = ReaderSpace.md), verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
        Text(title, style = MaterialTheme.typography.headlineMedium, modifier = Modifier.semantics { heading() })
        Text(description, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

internal enum class NoticeTone { INFO, ERROR, SUCCESS }

/** Plain-language feedback, with text and semantics rather than color alone. */
@Composable
internal fun NoticeBanner(
    message: String,
    modifier: Modifier = Modifier,
    tone: NoticeTone = NoticeTone.INFO,
    actionLabel: String? = null,
    onAction: () -> Unit = {},
    icon: ImageVector? = null,
    actionModifier: Modifier = Modifier,
) {
    val scheme = MaterialTheme.colorScheme
    val success = toneColors(scheme, LocalTones.current.green)
    val container = when (tone) {
        NoticeTone.ERROR -> scheme.errorContainer
        NoticeTone.SUCCESS -> success.container
        NoticeTone.INFO -> scheme.surfaceContainerHigh
    }
    val ink = when (tone) {
        NoticeTone.ERROR -> scheme.onErrorContainer
        NoticeTone.SUCCESS, NoticeTone.INFO -> scheme.onSurface
    }
    val mark = icon ?: when (tone) {
        NoticeTone.ERROR -> EarnestIcons.Alert
        NoticeTone.SUCCESS -> EarnestIcons.DoneSelected
        NoticeTone.INFO -> EarnestIcons.Info
    }
    Surface(
        modifier.fillMaxWidth().semantics { liveRegion = LiveRegionMode.Polite },
        shape = MaterialTheme.shapes.medium,
        color = container,
        contentColor = ink,
    ) {
        Column(Modifier.padding(start = ReaderSpace.md, end = ReaderSpace.xs, top = ReaderSpace.sm)) {
            Row(
                Modifier.padding(end = ReaderSpace.xs, bottom = if (actionLabel == null) ReaderSpace.sm else 0.dp),
                verticalAlignment = Alignment.Top,
                horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
            ) {
                Icon(mark, contentDescription = null, modifier = Modifier.size(20.dp))
                Text(message, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            }
            if (actionLabel != null) QuietButton(actionLabel, onAction, actionModifier.align(Alignment.End), color = ink)
        }
    }
}

/**
 * Empty, finished and failed places: a shaped mark in the place's tone, what
 * happened, and at most one way onward. Without an [icon] it wears the brand.
 */
@Composable
internal fun QuietState(
    title: String,
    message: String,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
    tone: Color = LocalTones.current.blue,
    actionLabel: String? = null,
    onAction: () -> Unit = {},
    actionModifier: Modifier = Modifier,
) {
    Column(
        modifier.fillMaxWidth().padding(ReaderSpace.lg),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
    ) {
        StateMark(icon, tone)
        Spacer(Modifier.height(ReaderSpace.sm))
        Text(
            title,
            style = MaterialTheme.typography.headlineSmall,
            textAlign = TextAlign.Center,
            modifier = Modifier.semantics { heading() },
        )
        Text(
            message,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
            modifier = Modifier.widthIn(max = StateTextWidth),
        )
        if (actionLabel != null) {
            SecondaryButton(actionLabel, onAction, actionModifier.padding(top = ReaderSpace.sm))
        }
    }
}

private val StateMarkSize = 88.dp
private val StateIconSize = 36.dp
private val StateGlyphSize = 52.dp
private val StateTextWidth = 360.dp

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun StateMark(icon: ImageVector?, tone: Color) {
    val colors = toneColors(MaterialTheme.colorScheme, tone)
    Box(
        Modifier.size(StateMarkSize).background(colors.container, MaterialShapes.Cookie9Sided.toShape()),
        contentAlignment = Alignment.Center,
    ) {
        if (icon == null) {
            Icon(
                painterResource(R.drawable.ic_notification),
                contentDescription = null,
                tint = colors.ink,
                modifier = Modifier.size(StateGlyphSize),
            )
        } else {
            Icon(icon, contentDescription = null, tint = colors.ink, modifier = Modifier.size(StateIconSize))
        }
    }
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun LoadingState(label: String, modifier: Modifier = Modifier) {
    Column(
        modifier.fillMaxWidth().padding(ReaderSpace.xl).semantics { liveRegion = LiveRegionMode.Polite },
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(ReaderSpace.md),
    ) {
        ContainedLoadingIndicator(
            containerColor = MaterialTheme.colorScheme.primaryContainer,
            indicatorColor = MaterialTheme.colorScheme.onPrimaryContainer,
        )
        Text(
            label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
    }
}

/** Names the group beneath it, with at most one quiet action beside it. */
@Composable
internal fun SectionHeader(
    title: String,
    modifier: Modifier = Modifier,
    color: Color = MaterialTheme.colorScheme.primary,
    action: (@Composable () -> Unit)? = null,
) {
    Row(
        modifier.fillMaxWidth().heightIn(min = TouchTarget),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            title,
            style = MaterialTheme.typography.titleSmall,
            color = color,
            modifier = Modifier.weight(1f).padding(vertical = ReaderSpace.xs).semantics { heading() },
        )
        action?.invoke()
    }
}

/** A short fact set apart from prose: coverage, a status, a place. */
@Composable
internal fun TagPill(text: String, modifier: Modifier = Modifier, icon: ImageVector? = null, colors: ToneColors? = null) {
    val scheme = MaterialTheme.colorScheme
    Surface(
        modifier,
        shape = CircleShape,
        color = colors?.container ?: scheme.surfaceContainer,
        contentColor = colors?.ink ?: scheme.onSurfaceVariant,
    ) {
        Row(
            Modifier.padding(horizontal = ReaderSpace.sm, vertical = ReaderSpace.xxs),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xxs),
        ) {
            icon?.let { Icon(it, contentDescription = null, modifier = Modifier.size(16.dp)) }
            Text(text, style = MaterialTheme.typography.labelMedium)
        }
    }
}

/** Group related controls, not individual rows, on one quiet surface. */
@Composable
internal fun SettingsGroup(title: String, modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    val scheme = MaterialTheme.colorScheme
    Column(modifier.fillMaxWidth().padding(bottom = ReaderSpace.md)) {
        SectionHeader(title, Modifier.padding(horizontal = ReaderSpace.xxs))
        Surface(
            shape = MaterialTheme.shapes.large,
            color = scheme.groupContainer,
            // White on white has no edge on e-paper.
            border = if (scheme.isMonochrome) BorderStroke(1.dp, scheme.outlineVariant) else null,
        ) {
            Column(
                Modifier.fillMaxWidth().padding(ReaderSpace.md),
                verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
                content = content,
            )
        }
    }
}

package ch.lkmc.earnest

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.add
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LargeFlexibleTopAppBar
import androidx.compose.material3.LoadingIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ShortNavigationBarItemDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.TopAppBarScrollBehavior
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteItem
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteType
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp

/** A large title that collapses, or one pinned row where height is scarce. */
internal enum class BarSize { LARGE, SMALL }

internal data class ScreenTitle(val title: String, val subtitle: String? = null)

internal enum class BarNavigation { DRAWER, BACK }

/** What the bar offers at its end. Search and Settings are on every main screen. */
internal data class BarActions(
    val onSearch: () -> Unit,
    val onSettings: () -> Unit,
    val onSignOut: () -> Unit,
    val theme: ThemeSetting,
    val onThemeChange: (ThemeSetting) -> Unit,
    /** Null where the screen carries its own refresh, as the front page does. */
    val onRefresh: (() -> Unit)?,
    val refreshing: Boolean,
    /** Present only while Android withholds the notification permission. */
    val onEnableNotifications: (() -> Unit)?,
)

@OptIn(ExperimentalMaterial3Api::class, ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun EarnestTopBar(
    title: ScreenTitle,
    size: BarSize,
    navigation: BarNavigation,
    onNavigation: () -> Unit,
    /** Null on a focused screen, such as Search. */
    actions: BarActions?,
    scrollBehavior: TopAppBarScrollBehavior?,
) {
    val scheme = MaterialTheme.colorScheme
    val colors = TopAppBarDefaults.topAppBarColors(containerColor = scheme.surface, scrolledContainerColor = scheme.surface)
    val navigationButton = @Composable {
        when (navigation) {
            BarNavigation.DRAWER -> ActionIconButton(EarnestIcons.Menu, "Folders and feeds", onNavigation)
            BarNavigation.BACK -> ActionIconButton(EarnestIcons.Back, "Back", onNavigation)
        }
    }
    val barInsets = TopAppBarDefaults.windowInsets.add(BarInset)
    when (size) {
        BarSize.LARGE -> LargeFlexibleTopAppBar(
            title = { BarTitle(title.title, maxLines = 2) },
            subtitle = title.subtitle?.let { subtitle ->
                { Text(subtitle, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            },
            navigationIcon = navigationButton,
            actions = { actions?.let { BarActionButtons(it) } },
            colors = colors,
            windowInsets = barInsets,
            scrollBehavior = scrollBehavior,
        )
        BarSize.SMALL -> TopAppBar(
            title = { SmallBarTitle(title.title) },
            navigationIcon = navigationButton,
            actions = { actions?.let { BarActionButtons(it) } },
            colors = colors,
            windowInsets = barInsets,
        )
    }
}

// A bar lays out 16dp from its edge; this brings it to the 20dp content gutter.
private val BarInset = WindowInsets(left = ReaderSpace.gutter - ReaderSpace.md, right = ReaderSpace.gutter - ReaderSpace.md)

@Composable
private fun BarTitle(text: String, maxLines: Int) {
    Text(text, maxLines = maxLines, overflow = TextOverflow.Ellipsis, modifier = Modifier.semantics { heading() })
}

/** A one-line title shares its row with the actions, so it stops growing before it truncates. */
@Composable
private fun SmallBarTitle(text: String) {
    val density = LocalDensity.current
    val titleDensity = Density(density.density, density.fontScale.coerceAtMost(BAR_TITLE_MAX_FONT_SCALE))
    CompositionLocalProvider(LocalDensity provides titleDensity) { BarTitle(text, maxLines = 1) }
}

private const val BAR_TITLE_MAX_FONT_SCALE = 1.15f

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun BarActionButtons(actions: BarActions) {
    ActionIconButton(EarnestIcons.Search, "Search", actions.onSearch)
    actions.onRefresh?.let { refresh ->
        if (actions.refreshing) {
            Box(Modifier.size(TouchTarget).semantics { contentDescription = "Refreshing" }, contentAlignment = Alignment.Center) {
                LoadingIndicator(Modifier.size(28.dp))
            }
        } else {
            ActionIconButton(EarnestIcons.Refresh, "Refresh", refresh)
        }
    }
    OverflowMenu(actions)
}

@Composable
private fun OverflowMenu(actions: BarActions) {
    var open by remember { mutableStateOf(false) }
    var choosingTheme by remember { mutableStateOf(false) }
    // The menu anchors to this Box, beside the button rather than inside the
    // button's content slot.
    Box {
        ActionIconButton(EarnestIcons.More, "More options", { open = true })
        AppMenu(open, { open = false }) {
            AppMenuItem(
                "Theme",
                { open = false; choosingTheme = true },
                EarnestIcons.Theme,
                supporting = actions.theme.label,
            )
            actions.onEnableNotifications?.let { enable ->
                AppMenuItem("Enable notifications", { open = false; enable() }, EarnestIcons.Bell)
            }
            AppMenuItem("Settings", { open = false; actions.onSettings() }, EarnestIcons.Settings)
            HorizontalDivider(Modifier.padding(vertical = ReaderSpace.xxs))
            AppMenuItem("Sign out", { open = false; actions.onSignOut() }, EarnestIcons.SignOut, role = MenuRole.DESTRUCTIVE)
        }
    }
    if (choosingTheme) {
        ThemeDialog(
            current = actions.theme,
            onDismiss = { choosingTheme = false },
            onSelect = { setting ->
                choosingTheme = false
                actions.onThemeChange(setting)
            },
        )
    }
}

/** One destination in the bottom bar or the rail. */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun DestinationItem(destination: View, selected: Boolean, type: NavigationSuiteType, onClick: () -> Unit) {
    val scheme = MaterialTheme.colorScheme
    val tone = destination.tone?.invoke(LocalTones.current) ?: scheme.primary
    val colors = toneColors(scheme, tone)
    val density = LocalDensity.current
    val labelDensity = Density(density.density, density.fontScale.coerceAtMost(BAR_LABEL_MAX_FONT_SCALE))
    NavigationSuiteItem(
        selected = selected,
        onClick = onClick,
        navigationSuiteType = type,
        icon = { Icon(destination.navigationIcon(selected), contentDescription = null) },
        label = {
            CompositionLocalProvider(LocalDensity provides labelDensity) {
                Text(destination.navigationLabel, textAlign = TextAlign.Center, maxLines = 2)
            }
        },
        colors = ShortNavigationBarItemDefaults.colors(
            selectedIconColor = colors.ink,
            selectedTextColorTopIconPosition = scheme.onSurface,
            selectedTextColorStartIconPosition = colors.ink,
            selectedIndicatorColor = colors.container,
            unselectedIconColor = scheme.onSurfaceVariant,
            unselectedTextColor = scheme.onSurfaceVariant,
        ),
    )
}

/** Kept, Later and Done share the Saved destination; this moves between them. */
@Composable
internal fun SavedShelfSwitcher(selected: View, onSelect: (View) -> Unit) {
    ChoiceGroup(
        options = savedShelves,
        selected = selected,
        onSelect = onSelect,
        label = { it.label },
        icon = { if (it == selected) it.selectedIcon else it.icon },
        modifier = Modifier.padding(horizontal = ReaderSpace.md, vertical = ReaderSpace.xxs),
    )
}

@Composable
internal fun ThemeDialog(current: ThemeSetting, onDismiss: () -> Unit, onSelect: (ThemeSetting) -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Theme") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState())) { ThemeOptions(current, onSelect) }
        },
        confirmButton = { QuietButton("Close", onDismiss) },
    )
}

/** The app's five themes, as the app bar's dialog and Settings both offer them. */
@Composable
internal fun ThemeOptions(current: ThemeSetting, onSelect: (ThemeSetting) -> Unit) {
    Column(Modifier.fillMaxWidth().selectableGroup()) {
        ThemeSetting.entries.forEach { setting ->
            OptionRow(
                label = setting.label,
                selected = setting == current,
                onSelect = { onSelect(setting) },
                leading = { ThemeSwatch(setting) },
            )
        }
    }
}

private val SwatchSize = 36.dp
private val SwatchInk = 12.dp

/** The paper and ink of a theme. System shows both of the ones it moves between. */
@Composable
private fun ThemeSwatch(setting: ThemeSetting) {
    Row(
        Modifier.size(SwatchSize).clip(CircleShape)
            .border(1.dp, MaterialTheme.colorScheme.outline, CircleShape),
    ) {
        val papers = if (setting == ThemeSetting.SYSTEM) listOf(ThemeSetting.LIGHT, ThemeSetting.DARK) else listOf(setting)
        papers.forEach { paper ->
            val preview = colorScheme(paper, systemDark = false)
            Box(
                Modifier.weight(1f).fillMaxHeight().background(preview.background),
                contentAlignment = Alignment.Center,
            ) {
                Box(Modifier.size(SwatchInk).background(preview.primary, CircleShape))
            }
        }
    }
}

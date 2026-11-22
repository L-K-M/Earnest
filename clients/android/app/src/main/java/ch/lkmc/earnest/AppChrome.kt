package ch.lkmc.earnest

import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.ui.semantics.heading
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun EarnestTopBar(
    title: String,
    loading: Boolean,
    model: AppViewModel,
    theme: ThemeSetting,
    onThemeChange: (ThemeSetting) -> Unit,
    notificationsDenied: Boolean,
    onRequestNotifications: () -> Unit,
    onSearch: () -> Unit,
    onSettings: () -> Unit,
    onRefresh: () -> Unit,
    focused: Boolean,
    showRefresh: Boolean,
    navigationIcon: ImageVector,
    navigationDescription: String,
    onNavigation: () -> Unit,
) {
    var themeOpen by remember { mutableStateOf(false) }
    var menuOpen by remember { mutableStateOf(false) }
    TopAppBar(
        title = { Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.titleLarge, modifier = Modifier.semantics { heading() }) },
        colors = TopAppBarDefaults.topAppBarColors(containerColor = MaterialTheme.colorScheme.surface),
        navigationIcon = {
            IconButton(onClick = onNavigation) { Icon(navigationIcon, contentDescription = navigationDescription) }
        },
        actions = {
            if (!focused) IconButton(onClick = onSearch) { Icon(Icons.Filled.Search, contentDescription = "Search") }
            if (loading && showRefresh) {
                Box(Modifier.size(48.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(
                        Modifier.size(20.dp),
                        strokeWidth = 2.dp,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
            } else if (showRefresh) {
                IconButton(onClick = onRefresh) {
                    Icon(Icons.Filled.Refresh, contentDescription = "Refresh")
                }
            }
            // The menu anchors to this Box, beside the button rather than
            // inside the button's content slot.
            Box {
                IconButton(onClick = { menuOpen = true }) {
                    Icon(Icons.Filled.MoreVert, contentDescription = "More options")
                }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text("Theme") },
                        onClick = { menuOpen = false; themeOpen = true },
                    )
                    if (notificationsDenied) {
                        DropdownMenuItem(
                            text = { Text("Enable notifications") },
                            onClick = { menuOpen = false; onRequestNotifications() },
                        )
                    }
                    DropdownMenuItem(
                        text = { Text("Settings") },
                        onClick = {
                            menuOpen = false
                            onSettings()
                        },
                    )
                    DropdownMenuItem(
                        text = { Text("Sign out") },
                        onClick = {
                            menuOpen = false
                            model.logout()
                        },
                    )
                }
            }
        },
    )
    if (themeOpen) {
        ThemeDialog(
            current = theme,
            onDismiss = { themeOpen = false },
            onSelect = { setting ->
                themeOpen = false
                onThemeChange(setting)
            },
        )
    }
}

@Composable
internal fun ThemeDialog(
    current: ThemeSetting,
    onDismiss: () -> Unit,
    onSelect: (ThemeSetting) -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Theme") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                ThemeSetting.entries.forEach { setting ->
                    Row(
                        Modifier.fillMaxWidth().clickable { onSelect(setting) },
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        RadioButton(selected = setting == current, onClick = { onSelect(setting) })
                        Text(setting.label)
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Close") } },
    )
}

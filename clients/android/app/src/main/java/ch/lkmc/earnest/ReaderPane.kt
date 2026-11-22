package ch.lkmc.earnest

import android.webkit.WebView
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.material3.FilledTonalButton
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.CachedEntry

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ReaderHeader(
    entry: CachedEntry,
    source: String,
    isRead: Boolean,
    offlineCopy: Boolean,
    onOpenOriginal: () -> Unit,
    onCopyLink: () -> Unit,
    onShare: () -> Unit,
    onMarkUnread: () -> Unit,
    onFind: () -> Unit,
    onSmallerText: () -> Unit,
    onLargerText: () -> Unit,
    modifier: Modifier = Modifier,
    articleActions: List<CustomAccessibilityAction> = emptyList(),
) {
    var menuOpen by remember { mutableStateOf(false) }
    Column(modifier.fillMaxWidth().padding(start = ReaderSpace.gutter, end = ReaderSpace.gutter, top = ReaderSpace.sm)) {
        Text(
            entry.title.ifBlank { "Untitled" },
            style = MaterialTheme.typography.titleLarge,
            modifier = Modifier.semantics {
                heading()
                customActions = articleActions
            },
            maxLines = 4,
            overflow = TextOverflow.Ellipsis,
        )
        val date = entryDateLabel(entry)
        val meta = listOfNotNull(
            source.takeIf { it.isNotBlank() },
            date.takeIf { it.isNotBlank() },
            "Cached copy".takeIf { offlineCopy },
        )
        if (meta.isNotEmpty()) {
            Text(
                meta.joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        FlowRow(Modifier.fillMaxWidth().padding(top = ReaderSpace.xs), horizontalArrangement = Arrangement.SpaceBetween) {
            if (originalUrl(entry) != null) {
                FilledTonalButton(onClick = onOpenOriginal) { Text("Open original") }
            }
            Row {
            IconButton(onClick = onFind) { Icon(Icons.Filled.Search, contentDescription = "Find in article") }
            Box {
                IconButton(onClick = { menuOpen = true }) { Icon(Icons.Filled.MoreVert, contentDescription = "Reader options") }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    if (originalUrl(entry) != null) {
                        DropdownMenuItem(text = { Text("Share") }, onClick = { menuOpen = false; onShare() })
                        DropdownMenuItem(text = { Text("Copy link") }, onClick = { menuOpen = false; onCopyLink() })
                    }
                    DropdownMenuItem(text = { Text("Smaller text") }, onClick = { menuOpen = false; onSmallerText() })
                    DropdownMenuItem(text = { Text("Larger text") }, onClick = { menuOpen = false; onLargerText() })
                    if (isRead) {
                        DropdownMenuItem(text = { Text("Mark unread") }, onClick = { menuOpen = false; onMarkUnread() })
                    }
                }
            }
            }
        }
    }
}

@Composable
internal fun FindBar(
    query: String,
    label: String,
    onQuery: (String) -> Unit,
    onPrevious: () -> Unit,
    onNext: () -> Unit,
    onClose: () -> Unit,
) {
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
        OutlinedTextField(
            value = query,
            onValueChange = onQuery,
            label = { Text("Find in article") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth().focusRequester(focus),
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
            keyboardActions = KeyboardActions(onSearch = { onNext() }),
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(label, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
            IconButton(onClick = onPrevious) { Icon(Icons.Filled.KeyboardArrowUp, contentDescription = "Previous match") }
            IconButton(onClick = onNext) { Icon(Icons.Filled.KeyboardArrowDown, contentDescription = "Next match") }
            IconButton(onClick = onClose) { Icon(Icons.Filled.Close, contentDescription = "Close find") }
        }
    }
}

/**
 * Bridges the reader WebView's find results into [onLabel] for the article
 * [entryId]. It re-registers whenever either changes, so a result always
 * names the current article's matches, even for a reader that reuses one
 * WebView across articles.
 */
@Composable
internal fun FindLabelBridge(webView: WebView?, entryId: Long, onLabel: (String) -> Unit) {
    // The listener outlives recompositions; it must always call the newest
    // sink even when the keys do not change.
    val currentOnLabel by rememberUpdatedState(onLabel)
    DisposableEffect(webView, entryId) {
        val listener = WebView.FindListener { active, matches, done ->
            if (done) currentOnLabel(findCountLabel(active, matches))
        }
        webView?.setFindListener(listener)
        onDispose { webView?.setFindListener(null) }
    }
}

package ch.lkmc.earnest

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi
import androidx.compose.material3.adaptive.layout.AnimatedPane
import androidx.compose.material3.adaptive.layout.ListDetailPaneScaffoldRole
import androidx.compose.material3.adaptive.navigation.NavigableListDetailPaneScaffold
import androidx.compose.material3.adaptive.navigation.rememberListDetailPaneScaffoldNavigator
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteScaffold
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import org.json.JSONObject

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val manager = getSystemService(NotificationManager::class.java)
        listOf("new_content" to NotificationManager.IMPORTANCE_LOW, "digest" to NotificationManager.IMPORTANCE_LOW,
            "brief" to NotificationManager.IMPORTANCE_LOW, "alert_now" to NotificationManager.IMPORTANCE_DEFAULT).forEach { (id, importance) ->
            manager.createNotificationChannel(NotificationChannel(id, id.replace('_', ' '), importance))
        }
        if (runCatching { Secrets(this).token() }.getOrNull() != null) SyncWorker.schedule(this)
        val selected = entryId(intent?.data)
        setContent { EarnestApp(selected) }
    }
}

private enum class View(val label: String, val query: String) {
    TODAY("Today", "view=river&cadence=today"), EDITION("Edition", "view=river"),
    LATER("Later", "view=later"), KEPT("Kept", "view=kept"), DONE("Done", "view=done"), FEEDS("Feeds", "")
}

@OptIn(ExperimentalMaterial3AdaptiveApi::class)
@Composable
private fun EarnestApp(initialEntry: Long?) {
    val context = LocalContext.current
    val secrets = remember { Secrets(context) }
    var server by remember { mutableStateOf(secrets.server) }
    var token by remember { mutableStateOf(runCatching { secrets.token() }.getOrNull()) }
    var auth by remember { mutableStateOf<JSONObject?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var view by remember { mutableStateOf(View.TODAY) }
    val navigator = rememberListDetailPaneScaffoldNavigator<Long>()
    val selection = navigator.currentDestination?.contentKey
    var html by remember { mutableStateOf<String?>(null) }
    var titles by remember { mutableStateOf<List<Pair<Long, String>>>(emptyList()) }
    var theme by remember { mutableStateOf(0) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(initialEntry) {
        initialEntry?.let { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, it) }
    }
    val scheme = when (theme) {
        1 -> darkColorScheme()
        2 -> darkColorScheme(background = androidx.compose.ui.graphics.Color.Black, surface = androidx.compose.ui.graphics.Color.Black)
        3 -> lightColorScheme(primary = androidx.compose.ui.graphics.Color.Black, background = androidx.compose.ui.graphics.Color.White, surface = androidx.compose.ui.graphics.Color.White)
        else -> lightColorScheme()
    }
    MaterialTheme(colorScheme = scheme) {
        Surface(Modifier.fillMaxSize()) {
            if (token == null) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text("Connect to Earnest", style = MaterialTheme.typography.headlineSmall)
                    OutlinedTextField(server, { server = it }, label = { Text("HTTPS server origin") }, singleLine = true)
                    Button(onClick = { scope.launch {
                        try {
                            auth = Server(server).request("/v1/devices/start", JSONObject().put("client_name", "Earnest Android"))
                            error = null
                        } catch (e: Exception) { error = e.message }
                    } }) { Text("Start device sign in") }
                    auth?.let { data ->
                        Text("Enter code ${data.optString("user_code")} at ${data.optString("verification_uri")}")
                        Button(onClick = {
                            val uri = android.net.Uri.parse(data.optString("verification_uri"))
                            if (uri.scheme == "https") runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, uri)) }
                        }) { Text("Open approval page") }
                        Button(onClick = { scope.launch {
                            try {
                                val result = Server(server).request("/v1/devices/poll", JSONObject().put("device_code", data.getString("device_code")))
                                val value = result.getString("access_token")
                                secrets.server = server.trim().trimEnd('/')
                                secrets.saveToken(value)
                                token = value
                                SyncWorker.schedule(context)
                                error = null
                            } catch (e: Exception) { error = e.message }
                        } }) { Text("Check approval") }
                    }
                    error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                }
            } else {
                LaunchedEffect(view, token) {
                    try {
                        if (view == View.FEEDS) {
                            val feeds = Server(server, token).array("/v1/subscriptions")
                            titles = (0 until feeds.length()).map { index ->
                                feeds.getJSONObject(index).let { it.getLong("id") to it.optString("title", "Feed") }
                            }
                            error = null
                        } else {
                            val result = Server(server, token).request("/v1/entries?${view.query}")
                            val entries = result.optJSONArray("entries")
                            titles = (0 until (entries?.length() ?: 0)).map { index ->
                                entries!!.getJSONObject(index).let { it.getLong("id") to it.optString("title") }
                            }
                            error = null
                        }
                    } catch (e: Exception) { error = e.message }
                }
                LaunchedEffect(selection, token) {
                    html = null
                    selection?.let { id ->
                        try {
                            html = Server(server, token).request("/v1/entries/$id/content").getString("html")
                            error = null
                        } catch (e: Exception) { error = e.message }
                    }
                }
                NavigationSuiteScaffold(navigationSuiteItems = {
                    View.entries.forEach { destination ->
                        item(
                            selected = view == destination,
                            onClick = {
                                view = destination
                                scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.List) }
                            },
                            icon = { Icon(Icons.AutoMirrored.Filled.List, contentDescription = null) },
                            label = { Text(destination.label) },
                        )
                    }
                }) {
                    Column(Modifier.fillMaxSize()) {
                        TextButton(onClick = { theme = (theme + 1) % 4 }) { Text("Theme: ${listOf("Light", "Dark", "Black", "E-ink")[theme]}") }
                        error?.let { Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(8.dp)) }
                        NavigableListDetailPaneScaffold(
                            navigator = navigator,
                            listPane = {
                                AnimatedPane {
                                    LazyColumn { items(titles, key = { it.first }) { (id, title) ->
                                        Text(title, Modifier.fillMaxWidth().clickable {
                                            if (view != View.FEEDS) scope.launch { navigator.navigateTo(ListDetailPaneScaffoldRole.Detail, id) }
                                        }.padding(16.dp))
                                    } }
                                }
                            },
                            detailPane = {
                                AnimatedPane {
                                    if (html == null) Text("Select an entry") else Reader(html!!, Modifier.fillMaxSize())
                                }
                            },
                        )
                    }
                }
            }
        }
    }
}

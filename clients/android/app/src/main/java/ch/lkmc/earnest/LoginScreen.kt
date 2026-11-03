package ch.lkmc.earnest

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.net.toUri
import ch.lkmc.earnest.core.ApiException
import ch.lkmc.earnest.core.DeviceResult
import ch.lkmc.earnest.core.EarnestApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.withLock

/** Device authorization: start, show the code, then poll automatically. */
@Composable
internal fun LoginScreen(onLoggedIn: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var server by remember { mutableStateOf("") }
    var authorization by remember { mutableStateOf<DeviceResult?>(null) }
    var intervalSeconds by remember { mutableLongStateOf(5L) }
    var error by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(authorization) {
        val current = authorization ?: return@LaunchedEffect
        intervalSeconds = current.interval
        while (true) {
            delay(intervalSeconds * 1000)
            try {
                val api = EarnestApi(server)
                val token = api.devicePoll(current.deviceCode)
                val me = EarnestApi(server, token.accessToken).me()
                SyncAccess.mutex.withLock { Secrets(context).saveSession(server, token.accessToken, me) }
                onLoggedIn()
                SyncWorker.schedule(context)
                EarnestPushService.register(context)
                return@LaunchedEffect
            } catch (failure: ApiException) {
                when (failure.code) {
                    "slow_down" -> intervalSeconds += SLOW_DOWN_STEP_SECONDS
                    "authorization_pending" -> Unit
                    else -> {
                        error = failure.message
                        return@LaunchedEffect
                    }
                }
            } catch (failure: Exception) {
                error = failure.message
                return@LaunchedEffect
            }
        }
    }

    Column(
        modifier = Modifier.padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Connect to Earnest", style = MaterialTheme.typography.headlineSmall)
        OutlinedTextField(
            value = server,
            onValueChange = { server = it },
            label = { Text("HTTPS server origin") },
            singleLine = true,
            enabled = authorization == null,
            modifier = Modifier.fillMaxWidth(),
        )
        Button(
            onClick = {
                scope.launch {
                    error = null
                    try {
                        val api = EarnestApi(server)
                        authorization = api.deviceStart("Earnest Android")
                    } catch (failure: Exception) {
                        error = failure.message
                    }
                }
            },
            enabled = server.isNotBlank() && authorization == null,
        ) {
            Text("Start device sign in")
        }
        authorization?.let { current ->
            Text("Enter code ${current.userCode} at ${current.verificationUri}")
            TextButton(
                onClick = {
                    val uri = current.verificationUri.toUri()
                    if (uri.scheme == "https") runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, uri)) }
                },
            ) {
                Text("Open approval page")
            }
        }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
    }
}

private const val SLOW_DOWN_STEP_SECONDS = 5L

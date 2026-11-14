package ch.lkmc.earnest

import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Surface
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.sp
import androidx.compose.ui.unit.dp
import androidx.core.net.toUri
import ch.lkmc.earnest.core.ApiException
import ch.lkmc.earnest.core.DeviceResult
import ch.lkmc.earnest.core.EarnestApi
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.withLock

/** Device authorization: start, show the code, then poll automatically. */
@Composable
internal fun LoginScreen(
    onLoggedIn: () -> Unit,
    notice: String? = null,
    apiFactory: (server: String, token: String?) -> EarnestApi = { server, token -> EarnestApi(server, token) },
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var server by remember { mutableStateOf("") }
    var starting by remember { mutableStateOf(false) }
    val keyboard = LocalSoftwareKeyboardController.current
    val focus = LocalFocusManager.current
    var authorization by remember { mutableStateOf<DeviceResult?>(null) }
    var intervalSeconds by remember { mutableLongStateOf(5L) }
    var error by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(authorization) {
        val current = authorization ?: return@LaunchedEffect
        intervalSeconds = current.interval
        // Construct within the handled poll path, then reuse its connections.
        val api by lazy { apiFactory(server, null) }
        while (true) {
            delay(intervalSeconds * 1000)
            try {
                val token = api.devicePoll(current.deviceCode)
                val me = apiFactory(server, token.accessToken).me()
                SyncAccess.mutex.withLock { Secrets(context).saveSession(server, token.accessToken, me) }
                onLoggedIn()
                // Permission and push registration start only now that sign-in succeeded.
                (context as? MainActivity)?.startBackgroundWork() ?: run {
                    SyncWorker.schedule(context)
                    EarnestPushService.register(context)
                }
                return@LaunchedEffect
            } catch (failure: ApiException) {
                when (failure.code) {
                    "slow_down" -> intervalSeconds += SLOW_DOWN_STEP_SECONDS
                    "authorization_pending" -> Unit
                    else -> {
                        // A terminal code ends this authorization: keep the
                        // failure and the entered server, but let Start run
                        // again with a fresh code.
                        error = failure.message
                        authorization = null
                        return@LaunchedEffect
                    }
                }
            } catch (failure: CancellationException) {
                // Composition or effect cancellation is not a login failure.
                throw failure
            } catch (failure: Exception) {
                error = failure.message ?: SIGN_IN_FAILED_MESSAGE
                authorization = null
                return@LaunchedEffect
            }
        }
    }

    val startSignIn: () -> Unit = {
        if (!starting && authorization == null && server.isNotBlank()) {
            starting = true
            error = null
            server = server.trim()
            keyboard?.hide()
            focus.clearFocus()
            scope.launch {
                try {
                    authorization = apiFactory(server, null).deviceStart("Earnest Android")
                } catch (failure: CancellationException) {
                    throw failure
                } catch (failure: Exception) {
                    error = failure.message ?: SIGN_IN_FAILED_MESSAGE
                } finally {
                    starting = false
                }
            }
        }
    }

    Box(Modifier.fillMaxSize().safeDrawingPadding().imePadding(), contentAlignment = Alignment.TopCenter) {
        Column(
            Modifier.widthIn(max = ReaderSpace.formWidth).fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = ReaderSpace.lg, vertical = ReaderSpace.xl),
            verticalArrangement = Arrangement.spacedBy(ReaderSpace.md),
        ) {
            Spacer(Modifier.height(ReaderSpace.md))
            BrandLockup()
            Spacer(Modifier.height(ReaderSpace.sm))
            ScreenIntro("Connect to Earnest", "Your feeds, your pace. Sign in to your private server to pick up where you left off.")
            notice?.let { NoticeBanner(it, tone = NoticeTone.ERROR) }
            OutlinedTextField(
                value = server,
                onValueChange = { server = it; error = null },
                label = { Text("HTTPS server origin") },
                placeholder = { Text("https://reader.example.com") },
                supportingText = { Text("Use the address of your Earnest server.") },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
                keyboardActions = KeyboardActions(onGo = { startSignIn() }),
                singleLine = true,
                enabled = authorization == null && !starting,
                modifier = Modifier.fillMaxWidth(),
            )
            Button(
                onClick = startSignIn,
                enabled = server.isNotBlank() && authorization == null && !starting,
                modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp),
            ) {
                if (starting) {
                    CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                    Spacer(Modifier.size(ReaderSpace.xs))
                }
                Text("Start device sign in")
            }
            if (starting) Text("Connecting to your server…", style = MaterialTheme.typography.bodySmall, modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite })
            authorization?.let { current ->
                Surface(shape = RoundedCornerShape(24.dp), color = MaterialTheme.colorScheme.surfaceContainerLowest) {
                    Column(Modifier.fillMaxWidth().padding(ReaderSpace.gutter), verticalArrangement = Arrangement.spacedBy(ReaderSpace.md)) {
                        Text("Approve this device", style = MaterialTheme.typography.titleLarge)
                        Text("Enter this code in your browser to finish signing in.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        SelectionContainer {
                            Text(current.userCode, style = MaterialTheme.typography.headlineLarge.copy(fontFamily = FontFamily.Monospace, letterSpacing = 2.sp))
                        }
                        Text(current.verificationUri, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
                        TextButton(onClick = {
                            val uri = current.verificationUri.toUri()
                            error = when {
                                uri.scheme != "https" -> "The server returned an unsupported approval address. Open your server's device page in a browser."
                                !openOriginal(context, uri.toString()) -> "No browser could open the approval page. Open the address above on another device."
                                else -> null
                            }
                        }) { Text("Open approval page") }
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
                            CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                            Text("Waiting for your approval…", style = MaterialTheme.typography.bodySmall, modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite })
                        }
                        TextButton(onClick = { authorization = null; error = null }) { Text("Cancel sign in") }
                    }
                }
            }
            error?.let { NoticeBanner(it, tone = NoticeTone.ERROR) }
            if (authorization == null) Text("Sign-in approval happens in your browser. Your password never passes through this app.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

private const val SLOW_DOWN_STEP_SECONDS = 5L
private const val SIGN_IN_FAILED_MESSAGE = "Sign-in failed. Try again."

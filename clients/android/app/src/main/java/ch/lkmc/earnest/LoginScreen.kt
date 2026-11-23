package ch.lkmc.earnest

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
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

    val formScroll = rememberScrollState()
    // A code appears above the form that asked for it.
    LaunchedEffect(authorization != null) { if (authorization != null) formScroll.animateScrollTo(0) }
    Box(Modifier.fillMaxSize().safeDrawingPadding().imePadding(), contentAlignment = Alignment.Center) {
        Column(
            Modifier.widthIn(max = ReaderSpace.formWidth).fillMaxWidth()
                .verticalScroll(formScroll)
                .padding(horizontal = ReaderSpace.lg, vertical = ReaderSpace.xl),
            verticalArrangement = Arrangement.spacedBy(ReaderSpace.md),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            BrandLockup()
            Ribbon()
            Column(
                Modifier.fillMaxWidth().padding(top = ReaderSpace.md, bottom = ReaderSpace.xs),
                verticalArrangement = Arrangement.spacedBy(ReaderSpace.xs),
            ) {
                Text(
                    if (authorization == null) "Connect to Earnest" else "Approve this device",
                    style = MaterialTheme.typography.displaySmall,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.fillMaxWidth().semantics { heading() },
                )
                Text(
                    if (authorization == null) {
                        "Your feeds, your pace. Sign in to your private server to pick up where you left off."
                    } else {
                        "Enter this code in your browser to finish signing in."
                    },
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            notice?.let { NoticeBanner(it, tone = NoticeTone.ERROR) }
            // The code comes first while it waits; the server it came from stays in view below.
            authorization?.let { current ->
                ApprovalCard(
                    authorization = current,
                    onOpen = {
                        val uri = current.verificationUri.toUri()
                        error = when {
                            uri.scheme != "https" -> "The server returned an unsupported approval address. Open your server's device page in a browser."
                            !openOriginal(context, uri.toString()) -> "No browser could open the approval page. Open the address above on another device."
                            else -> null
                        }
                    },
                    onCancel = { authorization = null; error = null },
                )
            }
            if (authorization != null) error?.let { NoticeBanner(it, tone = NoticeTone.ERROR) }
            OutlinedTextField(
                value = server,
                onValueChange = { server = it; error = null },
                label = { Text("HTTPS server origin") },
                placeholder = { Text("https://reader.example.com") },
                supportingText = { Text("Use the address of your Earnest server.") },
                shape = MaterialTheme.shapes.large,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
                keyboardActions = KeyboardActions(onGo = { startSignIn() }),
                singleLine = true,
                enabled = authorization == null && !starting,
                modifier = Modifier.fillMaxWidth(),
            )
            PrimaryButton(
                "Start device sign in",
                onClick = startSignIn,
                modifier = Modifier.fillMaxWidth(),
                enabled = server.isNotBlank() && authorization == null && !starting,
                busy = starting,
            )
            if (starting) {
                Text(
                    "Connecting to your server…",
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                )
            }
            if (authorization == null) {
                // Below the button, so a failure never moves the retry from under a finger.
                error?.let { NoticeBanner(it, tone = NoticeTone.ERROR) }
                PasswordReassurance()
            }
        }
    }
}

@Composable
private fun ApprovalCard(authorization: DeviceResult, onOpen: () -> Unit, onCancel: () -> Unit) {
    val scheme = MaterialTheme.colorScheme
    val context = LocalContext.current
    var copied by remember(authorization) { mutableStateOf(false) }
    Surface(
        Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.extraLarge,
        color = scheme.groupContainer,
        border = if (scheme.isMonochrome) BorderStroke(1.dp, scheme.outline) else null,
    ) {
        Column(
            Modifier.fillMaxWidth().padding(ReaderSpace.gutter),
            verticalArrangement = Arrangement.spacedBy(ReaderSpace.sm),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            SelectionContainer {
                Text(
                    authorization.userCode,
                    style = MaterialTheme.typography.headlineLarge.copy(fontFamily = FontFamily.Monospace, letterSpacing = CodeTracking),
                    textAlign = TextAlign.Center,
                )
            }
            QuietButton(
                if (copied) "Copied" else "Copy code",
                onClick = { copied = copyText(context, "Sign-in code", authorization.userCode) },
                icon = if (copied) EarnestIcons.Check else EarnestIcons.Copy,
            )
            PrimaryButton("Open approval page", onOpen, Modifier.fillMaxWidth(), icon = EarnestIcons.External)
            SelectionContainer {
                Text(
                    authorization.verificationUri,
                    style = MaterialTheme.typography.bodyMedium,
                    color = scheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(ReaderSpace.sm)) {
                CircularProgressIndicator(Modifier.size(WaitingSpinner), strokeWidth = 2.dp)
                Text(
                    "Waiting for your approval…",
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                )
            }
            QuietButton("Cancel sign in", onCancel)
        }
    }
}

@Composable
private fun PasswordReassurance() {
    val ink = MaterialTheme.colorScheme.onSurfaceVariant
    Row(horizontalArrangement = Arrangement.spacedBy(ReaderSpace.xs)) {
        Icon(EarnestIcons.Lock, contentDescription = null, tint = ink, modifier = Modifier.size(WaitingSpinner))
        Text(
            "Sign-in approval happens in your browser. Your password never passes through this app.",
            style = MaterialTheme.typography.bodySmall,
            color = ink,
            modifier = Modifier.weight(1f),
        )
    }
}

/** The seven colours of the brand's ribbon, in equal bands. E-paper has no use for them. */
@Composable
private fun Ribbon() {
    if (MaterialTheme.colorScheme.isMonochrome) return
    Row(Modifier.size(RibbonWidth, RibbonHeight).clip(CircleShape)) {
        RibbonBands.forEach { Box(Modifier.weight(1f).fillMaxHeight().background(it)) }
    }
}

private val RibbonWidth = 168.dp
private val RibbonHeight = 6.dp
private val RibbonBands = listOf(
    Color(0xFFFFA01C), Color(0xFFEA5F12), Color(0xFFC9481E), Color(0xFF8E3B68),
    Color(0xFF916E9D), Color(0xFF408CAD), Color(0xFF5DA8A1),
)
private val WaitingSpinner = 18.dp
private val CodeTracking = 2.sp
private const val SLOW_DOWN_STEP_SECONDS = 5L
private const val SIGN_IN_FAILED_MESSAGE = "Sign-in failed. Try again."

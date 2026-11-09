package ch.lkmc.earnest

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.clearAndSetSemantics
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
internal fun LoginScreen(onLoggedIn: () -> Unit, notice: String? = null) {
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

    val scheme = MaterialTheme.colorScheme
    // E-ink and true black stay monochrome, so neither gets the glow; the
    // ribbon is hidden in e-ink alone.
    val eInk = scheme.primary == Color.Black
    val glow = when {
        eInk || scheme.background == Color.Black -> null
        barIcons(scheme) == BarIcons.LIGHT -> GlowOrange.copy(alpha = 0.12f)
        else -> GlowCream.copy(alpha = 0.60f)
    }

    Box(Modifier.fillMaxSize()) {
        // The glow only draws, so it never intercepts a touch meant for the form.
        // It spreads from the top edge and reaches transparent at the box's
        // bottom, so the box never shows an edge.
        glow?.let { color ->
            Box(
                Modifier
                    .align(Alignment.TopCenter)
                    .fillMaxWidth()
                    .height(320.dp)
                    .drawBehind {
                        drawRect(
                            Brush.radialGradient(
                                colors = listOf(color, Color.Transparent),
                                center = Offset(size.width / 2f, 0f),
                                radius = size.height,
                            ),
                        )
                    },
            )
        }
        // The activity draws edge to edge, so keep the form clear of the status bar and keyboard.
        Column(
            modifier = Modifier.safeDrawingPadding().padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            // The ribbon takes the lockup's width, which only the lockup's
            // intrinsic size knows.
            Column(Modifier.width(IntrinsicSize.Min)) {
                BrandLockup()
                if (!eInk) {
                    BrandRibbon(Modifier.fillMaxWidth())
                }
            }
            Text("Connect to Earnest", style = MaterialTheme.typography.headlineSmall)
            notice?.let { Text(it, color = MaterialTheme.colorScheme.error) }
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
}

/** The icon's cream, the light-theme glow behind the sign-in form. */
private val GlowCream = Color(0xFFFEDFC1)

/** The icon's orange, the dark-theme glow behind the sign-in form. */
private val GlowOrange = Color(0xFFEA5F12)

/** The spec's ribbon order: the icon's own colours as equal hard segments. */
private val BrandRibbonColors = listOf(
    Color(0xFFFFA01C),
    Color(0xFFEA5F12),
    Color(0xFFC9481E),
    Color(0xFF8E3B68),
    Color(0xFF916E9D),
    Color(0xFF408CAD),
    Color(0xFF5DA8A1),
)

/** A thin decorative band of the icon's colours under the lockup. */
@Composable
private fun BrandRibbon(modifier: Modifier = Modifier) {
    Row(
        modifier
            .height(4.dp)
            .clip(RoundedCornerShape(percent = 50))
            .clearAndSetSemantics {},
    ) {
        BrandRibbonColors.forEach { color ->
            Box(
                Modifier
                    .weight(1f)
                    .fillMaxHeight()
                    .background(color),
            )
        }
    }
}

private const val SLOW_DOWN_STEP_SECONDS = 5L

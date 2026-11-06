package ch.lkmc.earnest

import android.Manifest
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.mutableStateOf

/**
 * The single activity. Predictive back is left to the system: no back callback
 * is registered at the launcher activity.
 */
class MainActivity : ComponentActivity() {
    private val launchEntry = mutableStateOf<Long?>(null)
    private val notificationId = mutableStateOf<Long?>(null)
    private var notificationPermissionResult: ((Boolean) -> Unit)? = null
    private val notificationPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
            notificationPermissionResult?.invoke(granted)
            notificationPermissionResult = null
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Notifications.ensureChannels(this)
        startBackgroundWork()
        readIntent(intent)
        setContent {
            EarnestApp(initialEntry = launchEntry.value, notificationId = notificationId.value)
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        readIntent(intent)
    }

    /**
     * Starts push registration and periodic sync for a signed-in device. The
     * notification prompt follows the first sign-in, never the connect screen.
     */
    internal fun startBackgroundWork() {
        if (!Secrets(this).hasToken()) return
        requestNotificationPermission()
        SyncWorker.schedule(this)
        EarnestPushService.register(this)
    }

    /** Notifications do nothing on Android 13+ until the user allows them. */
    internal fun requestNotificationPermission(onResult: (Boolean) -> Unit = {}) {
        if (!Notifications.permissionNeeded(this)) {
            onResult(true)
            return
        }
        notificationPermissionResult = onResult
        notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
    }

    private fun readIntent(intent: Intent?) {
        launchEntry.value = entryId(intent?.data)
        notificationId.value = intent?.getLongExtra(Notifications.EXTRA_NOTIFICATION_ID, -1L)?.takeIf { it > 0 }
    }
}

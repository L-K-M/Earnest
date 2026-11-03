package ch.lkmc.earnest

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.mutableStateOf
import androidx.core.content.ContextCompat

/**
 * The single activity. Predictive back is left to the system: no back callback
 * is registered at the launcher activity.
 */
class MainActivity : ComponentActivity() {
    private val launchEntry = mutableStateOf<Long?>(null)
    private val notificationId = mutableStateOf<Long?>(null)
    private val notificationPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Notifications.ensureChannels(this)
        requestNotificationPermission()
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

    /** Notifications do nothing on Android 13+ until the user allows them. */
    private fun requestNotificationPermission() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val granted = ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED
        if (!granted) notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
    }

    private fun startBackgroundWork() {
        if (!Secrets(this).hasToken()) return
        SyncWorker.schedule(this)
        EarnestPushService.register(this)
    }

    private fun readIntent(intent: Intent?) {
        launchEntry.value = entryId(intent?.data)
        notificationId.value = intent?.getLongExtra(Notifications.EXTRA_NOTIFICATION_ID, -1L)?.takeIf { it > 0 }
    }
}

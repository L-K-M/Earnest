package ch.lkmc.earnest

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.runtime.mutableStateOf

/**
 * The single activity. Predictive back is left to the system: no back callback
 * is registered at the launcher activity.
 */
class MainActivity : ComponentActivity() {
    private val launchEntry = mutableStateOf<Long?>(null)
    private val notificationId = mutableStateOf<Long?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Notifications.ensureChannels(this)
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

    private fun readIntent(intent: Intent?) {
        launchEntry.value = entryId(intent?.data)
        notificationId.value = intent?.getLongExtra(Notifications.EXTRA_NOTIFICATION_ID, -1L)?.takeIf { it > 0 }
    }
}

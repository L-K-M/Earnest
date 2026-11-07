package ch.lkmc.earnest

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat

/**
 * Notification channels, one per user-visible category. New content is
 * IMPORTANCE_LOW; only Now alerts rise above that, per Google's guidance.
 */
object Notifications {
    const val CHANNEL_NEW_CONTENT = "new_content"
    const val CHANNEL_ALERT = "alert_now"
    const val CHANNEL_DIGEST = "digest"
    const val CHANNEL_BRIEF = "brief"

    const val EXTRA_NOTIFICATION_ID = "earnest.notification_id"

    /** Whether the app must ask before it can post notifications. */
    fun permissionNeeded(context: Context): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED

    /** New content is quiet; only Now alerts rise above IMPORTANCE_LOW. */
    fun channelFor(category: String): String = when (category) {
        "alert" -> CHANNEL_ALERT
        "digest" -> CHANNEL_DIGEST
        "brief" -> CHANNEL_BRIEF
        else -> CHANNEL_NEW_CONTENT
    }

    fun ensureChannels(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java)
        listOf(
            CHANNEL_NEW_CONTENT to NotificationManager.IMPORTANCE_LOW,
            CHANNEL_DIGEST to NotificationManager.IMPORTANCE_LOW,
            CHANNEL_BRIEF to NotificationManager.IMPORTANCE_LOW,
            CHANNEL_ALERT to NotificationManager.IMPORTANCE_HIGH,
        ).forEach { (id, importance) ->
            manager.createNotificationChannel(
                NotificationChannel(id, id.replace('_', ' ').replaceFirstChar { it.uppercase() }, importance),
            )
        }
    }

    /**
     * Shows only the server-generated title and body. The agent's own words are
     * never displayed; the tap target opens the stored notification's entry.
     */
    fun show(context: Context, notificationId: Long, category: String, title: String, body: String) {
        val manager = context.getSystemService(NotificationManager::class.java)
        if (!manager.areNotificationsEnabled()) return
        val channel = channelFor(category)
        val intent = Intent(context, MainActivity::class.java).apply {
            putExtra(EXTRA_NOTIFICATION_ID, notificationId)
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
        }
        val pending = PendingIntent.getActivity(
            context,
            notificationId.toInt(),
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val notification = NotificationCompat.Builder(context, channel)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setContentIntent(pending)
            .setAutoCancel(true)
            .build()
        manager.notify(notificationId.toInt(), notification)
    }
}

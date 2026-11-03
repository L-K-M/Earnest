package ch.lkmc.earnest

import android.app.Notification
import android.app.NotificationManager
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class NotificationsTest {
    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Test fun channelsAreCreatedPerCategoryWithOnlyAlertsHigh() {
        Notifications.ensureChannels(context)
        val manager = context.getSystemService(NotificationManager::class.java)
        val importances = manager.notificationChannels.associate { it.id to it.importance }
        assertEquals(NotificationManager.IMPORTANCE_LOW, importances[Notifications.CHANNEL_NEW_CONTENT])
        assertEquals(NotificationManager.IMPORTANCE_LOW, importances[Notifications.CHANNEL_DIGEST])
        assertEquals(NotificationManager.IMPORTANCE_LOW, importances[Notifications.CHANNEL_BRIEF])
        assertEquals(NotificationManager.IMPORTANCE_HIGH, importances[Notifications.CHANNEL_ALERT])
    }

    @Test fun categoriesMapToTheirChannels() {
        assertEquals(Notifications.CHANNEL_ALERT, Notifications.channelFor("alert"))
        assertEquals(Notifications.CHANNEL_DIGEST, Notifications.channelFor("digest"))
        assertEquals(Notifications.CHANNEL_BRIEF, Notifications.channelFor("brief"))
        assertEquals(Notifications.CHANNEL_NEW_CONTENT, Notifications.channelFor("rule"))
    }

    @Test fun showsOnlyServerTextAndOpensTheStoredEntry() {
        Notifications.ensureChannels(context)
        Notifications.show(context, 42L, "alert", "Server title", "Server body")
        val manager = context.getSystemService(NotificationManager::class.java)
        val posted = manager.activeNotifications
        assertEquals(1, posted.size)
        val notification = posted[0].notification
        assertEquals(Notifications.CHANNEL_ALERT, notification.channelId)
        assertEquals("Server title", notification.extras.getCharSequence(Notification.EXTRA_TITLE))
        assertEquals("Server body", notification.extras.getCharSequence(Notification.EXTRA_TEXT))
        val saved = shadowOf(notification.contentIntent).savedIntent
        assertEquals(MainActivity::class.java.name, saved.component?.className)
        assertEquals(42L, saved.getLongExtra(Notifications.EXTRA_NOTIFICATION_ID, -1L))
    }
}

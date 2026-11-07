package ch.lkmc.earnest

import android.Manifest
import android.app.Application
import android.app.Notification
import android.app.NotificationManager
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
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

    @Test fun permissionIsNeededOnlyWhenAndroid13PlusAndNotGranted() {
        val application = ApplicationProvider.getApplicationContext<Application>()
        shadowOf(application).denyPermissions(Manifest.permission.POST_NOTIFICATIONS)
        assertTrue(Notifications.permissionNeeded(context))
        shadowOf(application).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        assertFalse(Notifications.permissionNeeded(context))
    }

    @Test @Config(sdk = [32]) fun noPromptIsNeededBeforeAndroid13() {
        assertFalse(Notifications.permissionNeeded(context))
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

    @Test fun usesTheAppGlyphAsTheStatusBarIcon() {
        Notifications.ensureChannels(context)
        Notifications.show(context, 7L, "alert", "Title", "Body")
        val notification = context.getSystemService(NotificationManager::class.java).activeNotifications[0].notification
        assertEquals(R.drawable.ic_notification, notification.smallIcon.resId)
    }
}

package ch.lkmc.earnest

import android.Manifest
import android.app.Application
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.test.core.app.ApplicationProvider
import androidx.work.Configuration
import androidx.work.WorkManager
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class MainActivityTest {
    private val application get() = ApplicationProvider.getApplicationContext<Application>()
    private val connection get() = application.getSharedPreferences("connection", Context.MODE_PRIVATE)

    @Before fun setup() {
        if (!WorkManager.isInitialized()) {
            WorkManager.initialize(application, Configuration.Builder().build())
        }
    }

    @Test fun connectScreenDoesNotAskForNotificationPermission() {
        connection.edit().clear().commit()
        val activity = Robolectric.buildActivity(MainActivity::class.java).setup().get()
        assertNull(shadowOf(activity).lastRequestedPermission)
    }

    @Test fun signedInLaunchAsksOnceForNotificationPermission() {
        connection.edit().putString("token", "stored").commit()
        shadowOf(application).denyPermissions(Manifest.permission.POST_NOTIFICATIONS)
        val activity = Robolectric.buildActivity(MainActivity::class.java).setup().get()
        val requested = shadowOf(activity).lastRequestedPermission
        assertEquals(Manifest.permission.POST_NOTIFICATIONS, requested?.requestedPermissions?.single())
    }

    /** The screen opens a link once per delivery, so only a new intent may count as one. */
    @Test fun recreationKeepsTheIntentDeliveryAndANewIntentRaisesIt() {
        connection.edit().clear().commit()
        val link = Intent(Intent.ACTION_VIEW, Uri.parse("earnest://entry/5"))
        val controller = Robolectric.buildActivity(MainActivity::class.java, link).setup()
        assertEquals(1, controller.get().intentDeliveries)
        controller.recreate()
        assertEquals("a rotation hands back the same intent", 1, controller.get().intentDeliveries)
        controller.newIntent(Intent(link))
        assertEquals("tapping the same link again is a new delivery", 2, controller.get().intentDeliveries)
        controller.recreate()
        assertEquals(2, controller.get().intentDeliveries)
    }
}

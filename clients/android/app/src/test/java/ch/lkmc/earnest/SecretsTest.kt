package ch.lkmc.earnest

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.MeResult
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class SecretsTest {
    private val context get() = ApplicationProvider.getApplicationContext<Context>()

    @Test fun legacyCredentialsCannotAttachAnUnverifiedAccount() {
        context.getSharedPreferences("connection", Context.MODE_PRIVATE).edit()
            .putString("server", "https://nas.test").putString("token", "legacy").commit()
        val secrets = Secrets(context)
        assertTrue(secrets.hasToken())
        assertNull(secrets.current())
    }

    @Test fun logoutRemovesBothTokenAndItsIdentity() {
        val prefs = context.getSharedPreferences("connection", Context.MODE_PRIVATE)
        prefs.edit().putString("token", "encrypted").putLong("user_id", 1).putLong("principal_id", 2).commit()
        val secrets = Secrets(context)
        secrets.clearToken()
        assertFalse(secrets.hasToken())
        assertFalse(prefs.contains("user_id"))
        assertFalse(prefs.contains("principal_id"))
    }

    @Test fun agentIdentityCannotBeSavedAsTheHumanSession() {
        try {
            Secrets(context).saveSession("https://nas.test", "token", MeResult(1, 2, "agent", emptyList()))
            fail("agent identity accepted")
        } catch (_: IllegalArgumentException) {
            assertFalse(Secrets(context).hasToken())
        }
    }
}

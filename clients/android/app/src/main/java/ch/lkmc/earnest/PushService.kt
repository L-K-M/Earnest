package ch.lkmc.earnest

import android.content.Context
import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import ch.lkmc.earnest.core.DeviceBody
import ch.lkmc.earnest.core.EarnestApi
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.unifiedpush.android.connector.FailedReason
import org.unifiedpush.android.connector.data.PushEndpoint
import org.unifiedpush.android.connector.data.PushMessage
import org.unifiedpush.android.connector.PushService
import org.unifiedpush.android.connector.UnifiedPush
import org.unifiedpush.android.connector.data.PublicKeySet
import java.security.MessageDigest

/** The encrypted push payload; [text] is the agent's own words and is never displayed. */
@Serializable
data class PushPayload(
    val title: String = "",
    val body: String = "",
    @SerialName("notification_id") val notificationId: String = "",
    val category: String = "",
    val text: String? = null,
)

/** Receives UnifiedPush events from the distributor and forwards them to Earnest. */
class EarnestPushService : PushService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val json = Json { ignoreUnknownKeys = true }

    override fun onNewEndpoint(endpoint: PushEndpoint, instance: String) {
        val keys = endpoint.pubKeySet ?: return
        scope.launch { registerEndpoint(endpoint.url, keys) }
    }

    override fun onMessage(message: PushMessage, instance: String) {
        val payload = runCatching { json.decodeFromString(PushPayload.serializer(), message.content.decodeToString()) }
            .getOrNull() ?: return
        Notifications.show(
            this,
            payload.notificationId.toLongOrNull() ?: 0L,
            payload.category,
            payload.title,
            payload.body,
        )
    }

    override fun onRegistrationFailed(reason: FailedReason, instance: String) {
        Log.i(TAG, "UnifiedPush registration failed: $reason")
    }

    override fun onUnregistered(instance: String) {
        Log.i(TAG, "UnifiedPush unregistered")
    }

    private suspend fun registerEndpoint(url: String, keys: PublicKeySet) {
        val secrets = Secrets(this)
        val token = secrets.token() ?: return
        val api = EarnestApi(secrets.server, token)
        val body = DeviceBody(
            kind = "unifiedpush",
            endpoint = url,
            p256dh = keys.pubKey,
            authSecret = keys.auth,
            showText = showText(),
            opId = "device-" + MessageDigest.getInstance("SHA-256")
                .digest(url.toByteArray(Charsets.UTF_8))
                .joinToString("") { "%02x".format(it) },
        )
        runCatching { api.registerDevice(body) }
            .onFailure { Log.i(TAG, "Device registration failed: ${it.message}") }
    }

    private fun showText(): Boolean =
        getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(KEY_SHOW_TEXT, false)

    companion object {
        private const val TAG = "EarnestPush"
        private const val PREFS = "push"
        private const val KEY_SHOW_TEXT = "show_text"

        /** Registers with the current or default distributor, if one is installed. */
        fun register(context: Context) {
            UnifiedPush.tryUseCurrentOrDefaultDistributor(context) { success ->
                if (success) {
                    UnifiedPush.register(context, messageForDistributor = "Earnest")
                }
            }
        }
    }
}

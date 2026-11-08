package ch.lkmc.earnest

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import androidx.core.content.edit
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * The server origin and bearer token. The token is encrypted with an
 * Android Keystore key, so it cannot be read from a backup or another process.
 */
internal class Secrets(context: Context) : SessionStore {
    private val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private val keyStore by lazy { KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) } }
    private val key: SecretKey
        get() {
            (keyStore.getKey(KEY_ALIAS, null) as? SecretKey)?.let { return it }
            val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
            generator.init(
                KeyGenParameterSpec.Builder(KEY_ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                    .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                    .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                    .build(),
            )
            return generator.generateKey()
        }

    val server: String get() = prefs.getString(KEY_SERVER, "").orEmpty()

    /** Save identity and encrypted credentials together, after verifying /v1/me. */
    fun saveSession(server: String, token: String, me: ch.lkmc.earnest.core.MeResult) = synchronized(sessionLock) {
        require(me.kind == "human") { "Sign in with a human account" }
        val origin = server.trim().trimEnd('/')
        AccountIdentity(origin, me.userId, me.principalId)
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key)
        val payload = cipher.iv + cipher.doFinal(token.toByteArray(Charsets.UTF_8))
        check(prefs.edit().putString(KEY_SERVER, origin)
            .putString(KEY_TOKEN, Base64.encodeToString(payload, Base64.NO_WRAP))
            .putLong(KEY_USER, me.userId).putLong(KEY_PRINCIPAL, me.principalId).commit()) {
            "Could not save the authenticated session"
        }
    }

    /** Cheap check for whether a token exists, without touching the Keystore. */
    fun hasToken(): Boolean = prefs.contains(KEY_TOKEN)

    private fun decrypt(encoded: String): String? = runCatching {
        val payload = Base64.decode(encoded, Base64.NO_WRAP)
        if (payload.size <= IV_BYTES) return null
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(128, payload.copyOfRange(0, IV_BYTES)))
        cipher.doFinal(payload.copyOfRange(IV_BYTES, payload.size)).toString(Charsets.UTF_8)
    }.getOrNull()

    override fun current(): AuthenticatedSession? {
        // One preference snapshot binds the token to exactly the identity saved with it.
        val values = prefs.all
        val user = values[KEY_USER] as? Long ?: return null
        val principal = values[KEY_PRINCIPAL] as? Long ?: return null
        val server = values[KEY_SERVER] as? String ?: return null
        val encoded = values[KEY_TOKEN] as? String ?: return null
        val identity = runCatching { AccountIdentity(server, user, principal) }.getOrNull() ?: return null
        return decrypt(encoded)?.let { AuthenticatedSession(identity, it) }
    }

    override fun clearToken() = synchronized(sessionLock) {
        prefs.edit(commit = true) { remove(KEY_TOKEN); remove(KEY_USER); remove(KEY_PRINCIPAL) }
    }

    override fun clearToken(expected: AuthenticatedSession): Boolean = synchronized(sessionLock) {
        if (current() != expected) return@synchronized false
        prefs.edit().remove(KEY_TOKEN).remove(KEY_USER).remove(KEY_PRINCIPAL).commit()
    }

    companion object {
        // All Secrets instances share these preferences, including the worker.
        private val sessionLock = Any()
        private const val PREFS = "connection"
        private const val KEY_SERVER = "server"
        private const val KEY_TOKEN = "token"
        private const val KEY_USER = "user_id"
        private const val KEY_PRINCIPAL = "principal_id"
        private const val KEY_ALIAS = "earnest-token"
        private const val ANDROID_KEYSTORE = "AndroidKeyStore"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private const val IV_BYTES = 12
    }
}

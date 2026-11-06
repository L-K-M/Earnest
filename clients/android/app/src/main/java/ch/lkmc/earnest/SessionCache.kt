package ch.lkmc.earnest

import android.content.Context
import androidx.core.content.edit
import ch.lkmc.earnest.core.Activity
import ch.lkmc.earnest.core.EarnestApi
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.db.AppDatabase
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.net.URI
import java.security.MessageDigest

/** Identity comes from /v1/me before any account cache is attached. */
internal data class AccountIdentity(val server: String, val userId: Long, val principalId: Long) {
    init {
        require(userId > 0 && principalId > 0) { "invalid authenticated identity" }
    }

    private val origin = URI(server.trim().trimEnd('/')).let {
        require(it.scheme.equals("https", ignoreCase = true) && it.host != null && it.userInfo == null &&
            it.path.isNullOrEmpty() && it.query == null && it.fragment == null) { "server must be an HTTPS origin" }
        val port = if (it.port == -1 || it.port == 443) "" else ":${it.port}"
        "https://${it.host.lowercase()}$port"
    }
    val cacheKey: String = MessageDigest.getInstance("SHA-256")
        .digest("$origin\n$userId\n$principalId".toByteArray(Charsets.UTF_8))
        .joinToString("") { "%02x".format(it) }
}

internal data class AuthenticatedSession(val identity: AccountIdentity, val token: String)

internal interface SessionStore {
    fun current(): AuthenticatedSession?
    fun clearToken()
}

internal object SyncAccess {
    val mutex = Mutex()

    /**
     * Cache key of an account whose Room store a background writer just
     * committed, so a running app reloads instead of waiting for a manual
     * refresh. One buffered slot is enough: only the latest commit matters.
     */
    val committed = MutableSharedFlow<String>(extraBufferCapacity = 1, onBufferOverflow = BufferOverflow.DROP_OLDEST)
}

/** Reload, mutate and flush form one critical section for every snapshot writer. */
internal class SessionCache(context: Context, val identity: AccountIdentity) {
    val db = AppDatabase.get(context, identity.cacheKey)
    val preferences = context.getSharedPreferences("session-${identity.cacheKey}", Context.MODE_PRIVATE)
    val legacyCacheRetained = context.getDatabasePath("earnest.db").exists()

    private val json = Json { ignoreUnknownKeys = true }

    val agentChangesStale: Boolean get() = preferences.getBoolean(KEY_AGENT_CHANGES_STALE, false)

    fun markAgentChangesStale() {
        preferences.edit { putBoolean(KEY_AGENT_CHANGES_STALE, true) }
    }

    fun agentChanges(): List<Activity> = runCatching {
        json.decodeFromString(ListSerializer(Activity.serializer()), preferences.getString(KEY_AGENT_CHANGES, "[]")!!)
    }.getOrDefault(emptyList())

    /** Called inside withEngine so offline notices follow the same sync owner. */
    suspend fun refreshAgentChanges(
        api: EarnestApi,
        since: String = preferences.getString(KEY_CHANGES_SINCE, null) ?: java.time.Instant.EPOCH.toString(),
    ): List<Activity> {
        val changes = mutableListOf<Activity>()
        var afterId = 0L
        while (true) {
            val page = api.agentChanges(since, afterId, AGENT_PAGE_LIMIT)
            changes += page
            if (page.size < AGENT_PAGE_LIMIT) break
            val next = page.last().id
            require(next > afterId) { "agent change cursor did not advance" }
            afterId = next
        }
        preferences.edit {
            putString(KEY_AGENT_CHANGES, json.encodeToString(ListSerializer(Activity.serializer()), changes))
            putBoolean(KEY_AGENT_CHANGES_STALE, false)
        }
        return changes
    }

    suspend fun <T> withEngine(block: suspend (SyncEngine) -> T): T = SyncAccess.mutex.withLock {
        val engine = SyncEngine(RoomSyncStore(db))
        val result = block(engine)
        engine.flush()
        result
    }
    companion object {
        const val KEY_CHANGES_SINCE = "agent_changes_since"
        private const val KEY_AGENT_CHANGES = "agent_changes"
        private const val KEY_AGENT_CHANGES_STALE = "agent_changes_stale"
        private const val AGENT_PAGE_LIMIT = 1000
    }

}

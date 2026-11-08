package ch.lkmc.earnest

import android.content.Context
import androidx.core.content.edit
import ch.lkmc.earnest.core.Activity
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.EngineSession
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.ReadingMode
import ch.lkmc.earnest.core.River
import ch.lkmc.earnest.core.UnreadCounts
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncRepository
import ch.lkmc.earnest.db.AppDatabase
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.IOException
import java.net.URI
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

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

/** Orders foreground and worker count requests within this process. */
internal class UnreadCountsRequest internal constructor(
    internal val cacheKey: String,
    internal val generation: String,
    internal val serial: Long,
)

internal interface SessionStore {
    fun current(): AuthenticatedSession?
    fun clearToken()
    /** Atomically removes only these credentials, never a replacement login. */
    fun clearToken(expected: AuthenticatedSession): Boolean
}

internal object SyncAccess {
    /** Serializes read-through-commit sections on the persisted snapshot and session save. */
    val mutex = Mutex()

    /**
     * Cache key of an account whose Room store a background writer just
     * committed, so a running app reloads instead of waiting for a manual
     * refresh. One buffered slot is enough: only the latest commit matters.
     */
    val committed = MutableSharedFlow<String>(extraBufferCapacity = 1, onBufferOverflow = BufferOverflow.DROP_OLDEST)

    /** Serializes a pull/flush cycle; only its snapshot sections take [mutex]. */
    val sync = Mutex()
}

/** Reload, mutate and flush form one critical section for every snapshot writer. */
internal class SessionCache(context: Context, val identity: AccountIdentity) {
    val db = AppDatabase.get(context, identity.cacheKey)
    val preferences = context.getSharedPreferences("session-${identity.cacheKey}", Context.MODE_PRIVATE)
    val legacyCacheRetained = context.getDatabasePath("earnest.db").exists()

    private val json = Json { ignoreUnknownKeys = true }
    // The snapshot's settings object keeps the server's field names and only
    // present override members, so a partial override stays partial.
    private val settingsJson = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        explicitNulls = false
    }

    val agentChangesStale: Boolean get() = preferences.getBoolean(KEY_AGENT_CHANGES_STALE, false)

    fun markAgentChangesStale() {
        preferences.edit { putBoolean(KEY_AGENT_CHANGES_STALE, true) }
    }

    fun agentChanges(): List<Activity> = runCatching {
        json.decodeFromString(ListSerializer(Activity.serializer()), preferences.getString(KEY_AGENT_CHANGES, "[]")!!)
    }.getOrDefault(emptyList())

    /** Called from a sync cycle so offline notices follow the same sync owner. */
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

    /**
     * The account's reader settings from the snapshot. The snapshot is the
     * offline source of truth: no network is needed to read preferences.
     */
    fun readerSettings(engine: SyncEngine): ReaderSettings {
        val raw = engine.snapshot().objects.firstOrNull { it.kind == River.SETTINGS && !it.deleted }?.raw
            ?: return ReaderSettings()
        return runCatching { settingsJson.decodeFromString(ReaderSettings.serializer(), raw) }.getOrDefault(ReaderSettings())
    }

    /** The snapshot revision of the settings object, or 0 when absent. */
    fun readerSettingsRev(engine: SyncEngine): Long =
        engine.snapshot().objects.firstOrNull { it.kind == River.SETTINGS && !it.deleted }?.rev ?: 0L

    /**
     * Merges acknowledged reader settings only while the snapshot still runs
     * the generation the caller captured before dispatch; a restore makes
     * revisions incomparable, so a stale acknowledgement is dropped instead of
     * regressing the new baseline. Returns the canonical stored settings, or
     * null when the generation moved on. An acknowledgement older than the
     * cached revision never overwrites it; the caller gets the stored winner.
     */
    suspend fun mergeReaderSettings(
        expectedGeneration: String,
        settings: ReaderSettings,
        owns: () -> Boolean,
    ): ReaderSettings? =
        withEngine { engine ->
            // The owner predicate runs after the lock is acquired: a caller
            // could lose its scope while suspended waiting for it, so checking
            // only before waiting (or after committing) leaves a gap.
            if (expectedGeneration.isEmpty() || engine.snapshot().cursor.generation != expectedGeneration || !owns()) return@withEngine null
            val current = engine.snapshot().objects.firstOrNull { it.kind == River.SETTINGS && !it.deleted }
            if (current == null || settings.rev > current.rev) {
                engine.mergeObject(
                    ch.lkmc.earnest.core.SyncObject(
                        kind = River.SETTINGS,
                        id = 1,
                        rev = settings.rev,
                        raw = settingsJson.encodeToString(ReaderSettings.serializer(), settings),
                    ),
                )
                settings
            } else {
                readerSettings(engine)
            }
        }

    /** The last exact unread counts, or null when none has synced. */
    fun cachedUnreadCounts(generation: String): UnreadCounts? {
        if (generation.isEmpty() || preferences.getString(KEY_UNREAD_COUNTS_GENERATION, null) != generation) return null
        return preferences.getString(KEY_UNREAD_COUNTS, null)?.let {
            runCatching { json.decodeFromString(UnreadCounts.serializer(), it) }.getOrNull()
        }
    }

    /** Wall-clock millis of a valid last-synced count record, or null. */
    fun cachedUnreadCountsAt(generation: String): Long? =
        if (cachedUnreadCounts(generation) == null) null else preferences.getLong(KEY_UNREAD_COUNTS_AT, 0L).takeIf { it > 0L }

    /** Captures a count intent only after validating its durable account scope. */
    suspend fun beginUnreadCountsRequest(generation: String, owns: () -> Boolean): UnreadCountsRequest? = withEngine { engine ->
        if (generation.isEmpty() || engine.snapshot().cursor.generation != generation ||
            readerSettings(engine).readingMode != ReadingMode.UNREAD_COUNTS || !owns()
        ) return@withEngine null
        UnreadCountsRequest(identity.cacheKey, generation, countsSequence.incrementAndGet())
    }

    /** A later-started successful writer must also fence delayed UI publications. */
    fun ownsUnreadCounts(request: UnreadCountsRequest): Boolean = request.cacheKey == identity.cacheKey &&
        (completedCountRequests[identity.cacheKey] ?: 0L) <= request.serial

    /** Counts commit with their generation only for an owning unread-count request. */
    suspend fun mergeUnreadCounts(request: UnreadCountsRequest, counts: UnreadCounts, owns: () -> Boolean): Boolean = withEngine { engine ->
        if (!ownsUnreadCounts(request) || engine.snapshot().cursor.generation != request.generation ||
            readerSettings(engine).readingMode != ReadingMode.UNREAD_COUNTS || !owns()
        ) return@withEngine false
        val stored = preferences.edit()
            .putString(KEY_UNREAD_COUNTS, json.encodeToString(UnreadCounts.serializer(), counts))
            .putString(KEY_UNREAD_COUNTS_GENERATION, request.generation)
            .putLong(KEY_UNREAD_COUNTS_AT, System.currentTimeMillis())
            .commit()
        if (!stored) throw IOException("Could not store unread counts on this device")
        completedCountRequests[identity.cacheKey] = request.serial
        true
    }

    // Internal so tests can observe engine reuse.
    internal var engine: SyncEngine? = null
    internal var snapshotVersion = -1L

    suspend fun <T> withEngine(block: suspend (SyncEngine) -> T): T = SyncAccess.mutex.withLock {
        // Rebuilding the engine decodes the whole snapshot JSON on every call.
        // The snapshot row carries a write counter, so an unchanged version means
        // no writer (e.g. the sync worker) committed since the last load.
        val store = RoomSyncStore(db)
        val version = store.version()
        val cached = engine
        val active = if (cached != null && version != -1L && version == snapshotVersion) {
            cached
        } else {
            SyncEngine(store).also { engine = it; snapshotVersion = version }
        }
        try {
            val result = block(active)
            active.flush()
            // flush() bumped the counter; record the post-write version so the
            // next call reuses this engine instead of rebuilding it.
            snapshotVersion = store.version()
            result
        } catch (t: Throwable) {
            // A failed block may have staged a write via save(); drop the engine
            // so a later, unrelated call cannot flush it.
            engine = null
            snapshotVersion = -1L
            throw t
        }
    }

    /** A repository whose engine sections run under [SyncAccess.mutex], keeping HTTP off the lock. */
    fun repository(api: EarnestApi): SyncRepository = SyncRepository(api, object : EngineSession {
        override suspend fun <T> invoke(section: suspend (SyncEngine) -> T): T = withEngine(section)
    })

    /** Runs [block] as the only pull owner so its change pages cannot interleave. */
    suspend fun <T> withSync(api: EarnestApi, block: suspend (SyncRepository) -> T): T =
        SyncAccess.sync.withLock { block(repository(api)) }

    companion object {
        const val KEY_CHANGES_SINCE = "agent_changes_since"
        private const val KEY_AGENT_CHANGES = "agent_changes"
        private const val KEY_AGENT_CHANGES_STALE = "agent_changes_stale"
        // Shared across cache instances. Writes hold SyncAccess.mutex; UI
        // publication reads are concurrent. In-flight requests cannot survive
        // process death, so their ordering need not be persisted.
        private val countsSequence = AtomicLong()
        private val completedCountRequests = ConcurrentHashMap<String, Long>()
        private const val KEY_UNREAD_COUNTS = "unread_counts"
        private const val KEY_UNREAD_COUNTS_AT = "unread_counts_at"
        private const val KEY_UNREAD_COUNTS_GENERATION = "unread_counts_generation"
        private const val AGENT_PAGE_LIMIT = 1000
    }

}

package ch.lkmc.earnest

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.ReaderSettings
import ch.lkmc.earnest.core.ReadingMode
import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.UnreadCounts
import java.io.IOException
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.SQLiteMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@SQLiteMode(SQLiteMode.Mode.NATIVE)
class UnreadCountsCacheTest {
    private val context get() = ApplicationProvider.getApplicationContext<Context>()
    private val identity = AccountIdentity("https://counts.test", 9, 9)
    private val counts = UnreadCounts(mapOf(12L to 9_007_199_254_740_993L), 2, 3, 9_007_199_254_740_993L)

    private suspend fun cacheForUnread(context: Context = this.context): SessionCache {
        val cache = SessionCache(context, identity)
        cache.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        cache.mergeReaderSettings("g1", ReaderSettings(readingMode = ReadingMode.UNREAD_COUNTS, rev = 1)) { true }
        return cache
    }

    private suspend fun SessionCache.countRequest(generation: String = "g1"): UnreadCountsRequest =
        checkNotNull(beginUnreadCountsRequest(generation) { true })

    @Test fun restoreDoesNotReuseCountsFromThePreviousGeneration() = runBlocking(Dispatchers.IO) {
        val cache = cacheForUnread()
        assertTrue(cache.mergeUnreadCounts(cache.countRequest(), counts) { true })
        assertEquals(counts, cache.cachedUnreadCounts("g1"))
        val stale = cache.countRequest()
        cache.withEngine { it.beginBootstrap(Revision("g2", 0)) }
        assertNull("old subscription IDs must not acquire pre-restore counts", cache.cachedUnreadCounts("g2"))
        assertNull(cache.cachedUnreadCountsAt("g2"))
        cache.mergeReaderSettings("g2", ReaderSettings(readingMode = ReadingMode.UNREAD_COUNTS, rev = 1)) { true }
        assertFalse("an old HTTP result cannot adopt the new generation", cache.mergeUnreadCounts(stale, counts) { true })
        val current = counts.copy(bySubscription = mapOf(12L to 8L), total = 8)
        assertTrue(cache.mergeUnreadCounts(cache.countRequest("g2"), current) { true })
        assertFalse(cache.mergeUnreadCounts(stale, counts) { true })
        assertEquals(current, cache.cachedUnreadCounts("g2"))
    }

    @Test fun countsWithoutGenerationMetadataAreUnavailable() = runBlocking(Dispatchers.IO) {
        val cache = cacheForUnread()
        cache.preferences.edit()
            .putString("unread_counts", Json.encodeToString(UnreadCounts.serializer(), counts))
            .putLong("unread_counts_at", 123)
            .commit()
        assertNull("legacy unowned counts are not a valid snapshot", cache.cachedUnreadCounts("g1"))
        assertNull(cache.cachedUnreadCountsAt("g1"))
    }

    @Test fun sameGenerationRetainsExactCountsAndAccountsRemainSeparate() = runBlocking(Dispatchers.IO) {
        val cache = cacheForUnread()
        assertTrue(cache.mergeUnreadCounts(cache.countRequest(), counts) { true })
        cache.withEngine { it.beginBootstrap(Revision("g1", 4)) }
        assertEquals(counts, SessionCache(context, identity).cachedUnreadCounts("g1"))
        assertNotNull(cache.cachedUnreadCountsAt("g1"))
        val other = SessionCache(context, identity.copy(userId = 10, principalId = 10))
        other.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        assertNull(other.cachedUnreadCounts("g1"))
    }

    @Test fun riverModeDoesNotAcceptAnInFlightCountResult() = runBlocking(Dispatchers.IO) {
        val cache = cacheForUnread()
        cache.mergeUnreadCounts(cache.countRequest(), counts) { true }
        val syncedAt = cache.cachedUnreadCountsAt("g1")
        val pending = cache.countRequest()
        cache.mergeReaderSettings("g1", ReaderSettings(readingMode = ReadingMode.RIVER, rev = 2)) { true }
        assertFalse(cache.mergeUnreadCounts(pending, counts.copy(total = 0)) { true })
        assertNull(cache.beginUnreadCountsRequest("g1") { true })
        assertEquals(counts, cache.cachedUnreadCounts("g1"))
        assertEquals(syncedAt, cache.cachedUnreadCountsAt("g1"))
    }

    @Test fun losingOwnershipWhileWaitingForTheLockCannotPersistSettingsOrCounts() = runBlocking(Dispatchers.IO) {
        val cache = cacheForUnread()
        cache.mergeUnreadCounts(cache.countRequest(), counts) { true }
        val pending = cache.countRequest()
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        val owner = AtomicBoolean(true)
        val holder = launch { cache.withEngine { entered.complete(Unit); release.await() } }
        entered.await()
        val settingsWrite = async(start = CoroutineStart.UNDISPATCHED) {
            cache.mergeReaderSettings("g1", ReaderSettings(measure = 70, rev = 2), owner::get)
        }
        val countsWrite = async(start = CoroutineStart.UNDISPATCHED) {
            cache.mergeUnreadCounts(pending, counts.copy(total = 0), owner::get)
        }
        owner.set(false)
        release.complete(Unit)
        holder.join()
        assertNull(settingsWrite.await())
        assertFalse(countsWrite.await())
        assertEquals(1L, cache.withEngine { cache.readerSettingsRev(it) })
        assertEquals(counts, cache.cachedUnreadCounts("g1"))
    }

    @Test fun newerSuccessfulWriterFencesOtherCacheInstancesAndTheirPublications() = runBlocking(Dispatchers.IO) {
        val foreground = cacheForUnread()
        val worker = SessionCache(context, identity)
        val older = worker.countRequest()
        val newer = foreground.countRequest()
        val current = counts.copy(bySubscription = emptyMap(), total = 0)
        assertTrue(foreground.mergeUnreadCounts(newer, current) { true })
        val syncedAt = foreground.cachedUnreadCountsAt("g1")
        assertFalse(worker.ownsUnreadCounts(older))
        assertFalse(worker.mergeUnreadCounts(older, counts) { true })
        assertEquals(current, worker.cachedUnreadCounts("g1"))
        assertEquals(syncedAt, worker.cachedUnreadCountsAt("g1"))
    }

    @Test fun countIntentCannotBeMovedToAnotherAccount() = runBlocking(Dispatchers.IO) {
        val original = cacheForUnread()
        val request = original.countRequest()
        val other = SessionCache(context, identity.copy(userId = 10, principalId = 10))
        other.withEngine { it.beginBootstrap(Revision("g1", 0)) }
        other.mergeReaderSettings("g1", ReaderSettings(readingMode = ReadingMode.UNREAD_COUNTS, rev = 1)) { true }
        assertFalse(other.mergeUnreadCounts(request, counts) { true })
        assertNull(other.cachedUnreadCounts("g1"))
        assertTrue(original.mergeUnreadCounts(request, counts) { true })
    }

    @Test fun failedCountPersistenceIsNotReportedAsASuccess() = runBlocking(Dispatchers.IO) {
        val delegate = context.getSharedPreferences("failed-counts", Context.MODE_PRIVATE)
        val failingContext = object : ContextWrapper(context) {
            override fun getSharedPreferences(name: String, mode: Int): SharedPreferences =
                object : SharedPreferences by delegate {
                    override fun edit(): SharedPreferences.Editor {
                        val editor = delegate.edit()
                        return object : SharedPreferences.Editor by editor {
                            override fun putString(key: String, value: String?): SharedPreferences.Editor {
                                editor.putString(key, value)
                                return this
                            }
                            override fun putLong(key: String, value: Long): SharedPreferences.Editor {
                                editor.putLong(key, value)
                                return this
                            }
                            override fun commit() = false
                        }
                    }
                }
        }
        val cache = cacheForUnread(failingContext)
        val failure = runCatching { cache.mergeUnreadCounts(cache.countRequest(), counts) { true } }.exceptionOrNull()
        assertTrue("disk failure must reach the caller", failure is IOException)
        assertNull(cache.cachedUnreadCounts("g1"))
    }
}

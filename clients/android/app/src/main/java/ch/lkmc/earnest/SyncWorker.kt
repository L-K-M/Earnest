package ch.lkmc.earnest

import android.content.Context
import android.util.Log
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import ch.lkmc.earnest.core.ApiException
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.ReadingMode
import kotlinx.coroutines.CancellationException
import java.time.Instant
import java.util.concurrent.TimeUnit

/** Periodic offline sync: pull change pages, flush the outbox, expire old intent. */
internal class SyncWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val secrets = Secrets(applicationContext)
        val session = secrets.current() ?: return Result.failure()
        val cache = SessionCache(applicationContext, session.identity)
        return try {
            val api = EarnestApi(session.identity.server, session.token)
            cache.withSync(api) { repository ->
                if (secrets.current() != session) return@withSync
                cache.markAgentChangesStale()
                val rejected = repository.pull() + repository.flush()
                rejected.forEach { Log.i(TAG, "server rejected ${it.opId}: ${it.status}") }
                repository.expire(Instant.now()).forEach { Log.i(TAG, "expired ${it.operation.opId}") }
                cache.refreshAgentChanges(api)
            }
            // Capture the generation before the request; a restore must not
            // attach its result to replacement subscription IDs.
            val countsGeneration = cache.withEngine { engine ->
                engine.snapshot().cursor.generation.takeIf {
                    it.isNotEmpty() && secrets.current() == session &&
                        cache.readerSettings(engine).readingMode == ReadingMode.UNREAD_COUNTS
                }
            }
            val countsRequest = countsGeneration?.let { generation ->
                cache.beginUnreadCountsRequest(generation) { secrets.current() == session }
            }
            if (countsRequest != null) {
                try {
                    val counts = api.unreadCounts()
                    cache.mergeUnreadCounts(countsRequest, counts) { secrets.current() == session }
                } catch (error: ApiException) {
                    if (error.isUnauthorized && cache.ownsUnreadCounts(countsRequest)) throw error
                    // Counts are optional. Keep the last successful snapshot.
                } catch (error: CancellationException) {
                    throw error
                } catch (_: Exception) {
                    // An unavailable count endpoint is not a zero count.
                }
            }
            SyncAccess.committed.tryEmit(session.identity.cacheKey)
            refreshWidgets(applicationContext)
            Result.success()
        } catch (error: ApiException) {
            if (error.isUnauthorized) secrets.clearToken(session)
            Result.retry()
        } catch (error: CancellationException) {
            throw error
        } catch (error: Exception) {
            Result.retry()
        }
    }

    companion object {
        private const val TAG = "EarnestSync"

        fun schedule(context: Context) {
            val request = PeriodicWorkRequestBuilder<SyncWorker>(15, TimeUnit.MINUTES)
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .build()
            WorkManager.getInstance(context).enqueueUniquePeriodicWork("sync", ExistingPeriodicWorkPolicy.KEEP, request)
        }
    }
}

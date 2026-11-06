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
import ch.lkmc.earnest.core.SyncRepository
import java.time.Instant
import java.util.concurrent.TimeUnit

/** Periodic offline sync: pull change pages, flush the outbox, expire old intent. */
internal class SyncWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val secrets = Secrets(applicationContext)
        val session = secrets.current() ?: return Result.failure()
        val cache = SessionCache(applicationContext, session.identity)
        return try {
            cache.withEngine { engine ->
                if (secrets.current() != session) return@withEngine
                val api = EarnestApi(session.identity.server, session.token)
                val repository = SyncRepository(api, engine)
                cache.markAgentChangesStale()
                val rejected = repository.pull() + repository.flush()
                rejected.forEach { Log.i(TAG, "server rejected ${it.opId}: ${it.status}") }
                engine.expire(Instant.now()).forEach { Log.i(TAG, "expired ${it.operation.opId}") }
                engine.flush()
                cache.refreshAgentChanges(api)
            }
            SyncAccess.committed.tryEmit(session.identity.cacheKey)
            Result.success()
        } catch (error: ApiException) {
            if (error.isUnauthorized && secrets.current() == session) secrets.clearToken()
            Result.retry()
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

package ch.lkmc.earnest

import android.content.Context
import androidx.room.Room
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.SyncEngine
import ch.lkmc.earnest.core.SyncRepository
import java.util.concurrent.TimeUnit
import java.security.MessageDigest

internal class SyncWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val secrets = Secrets(applicationContext)
        val token = try { secrets.token() } catch (_: Exception) { return Result.failure() } ?: return Result.failure()
        val databaseId = MessageDigest.getInstance("SHA-256").digest((secrets.server + "\u0000" + token).toByteArray(Charsets.UTF_8))
            .joinToString("") { "%02x".format(it) }
        val database = Room.databaseBuilder(applicationContext, SyncDatabase::class.java, "sync-$databaseId.db").build()
        return try {
            val engine = SyncEngine(RoomSyncStore(database.sync()))
            val repository = SyncRepository(EarnestApi(secrets.server, token), engine)
            repository.pull()
            // Pending writes require a user-visible rejection path before they can be flushed.
            Result.success()
        } catch (_: Exception) {
            Result.retry()
        } finally {
            database.close()
        }
    }

    companion object {
        fun schedule(context: Context) {
            val request = PeriodicWorkRequestBuilder<SyncWorker>(15, TimeUnit.MINUTES)
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .build()
            WorkManager.getInstance(context).enqueueUniquePeriodicWork("sync", ExistingPeriodicWorkPolicy.KEEP, request)
        }
    }
}

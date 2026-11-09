package ch.lkmc.earnest

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.util.LruCache
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Box
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap

/**
 * The account's stored feed icons and article thumbnails. Versioned paths
 * (`?v=` marks immutable content) are written under `media/<account>` in the
 * cache directory, scoped by account key like the snapshot database and the
 * session preferences, so Android drops them with the rest of the app's
 * cached data. Unversioned paths may change, so they are fetched every time
 * and only the memory cache serves them.
 */
internal class MediaCache(context: Context) {
    private val root = File(context.cacheDir, "media")

    // LruCache cannot store null; Miss remembers an absent or undecodable
    // image for the process so it is not fetched on every composition. A
    // failed fetch is not remembered, so the image loads once the network is
    // back.
    private val memory = object : LruCache<String, Any>(MEMORY_BYTES) {
        override fun sizeOf(key: String, value: Any): Int = (value as? Bitmap)?.byteCount ?: 1
    }
    private val inFlight = ConcurrentHashMap<String, Deferred<Bitmap?>>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    fun close() = scope.cancel()

    /**
     * The bitmap for [path] under [accountKey], or null when none is stored or
     * the fetch or decode failed. Loads run on the cache's scope so concurrent
     * callers coalesce on one fetch and a disposed row cannot cancel the
     * disk write other rows still wait on.
     */
    suspend fun load(accountKey: String, path: String, fetch: suspend (String) -> ByteArray?): ImageBitmap? {
        require(accountKey.matches(Regex("[0-9a-f]{64}"))) { "invalid account cache key" }
        val key = "$accountKey\n$path"
        memory.get(key)?.let { return (it as? Bitmap)?.asImageBitmap() }
        val job = inFlight.computeIfAbsent(key) {
            scope.async {
                try {
                    val bitmap = try {
                        fetchBitmap(accountKey, path, fetch)
                    } catch (error: CancellationException) {
                        throw error
                    } catch (_: Exception) {
                        return@async null
                    }
                    memory.put(key, bitmap ?: Miss)
                    bitmap
                } finally {
                    inFlight.remove(key)
                }
            }
        }
        return job.await()?.asImageBitmap()
    }

    private suspend fun fetchBitmap(accountKey: String, path: String, fetch: suspend (String) -> ByteArray?): Bitmap? {
        val target = versionedFile(accountKey, path)
        if (target != null && target.isFile) {
            val cached = runCatching {
                target.setLastModified(System.currentTimeMillis())
                target.readBytes().decode()
            }.getOrNull()
            if (cached != null) return cached
        }
        val bytes = fetch(path) ?: return null
        val bitmap = bytes.decode() ?: return null
        if (target != null) {
            runCatching { writeDisk(target, bytes) }
        }
        return bitmap
    }

    /** Only versioned paths are immutable enough to earn a disk copy. */
    private fun versionedFile(accountKey: String, path: String): File? =
        if (!path.contains("?v=")) null else fileFor(accountKey, path)

    internal fun fileFor(accountKey: String, path: String): File =
        File(root, "$accountKey/${sha256(path)}")

    /** Files beyond the per-account cap go out least recently used first. */
    private fun writeDisk(file: File, bytes: ByteArray) {
        val dir = file.parentFile ?: return
        dir.mkdirs()
        file.writeBytes(bytes)
        file.setLastModified(System.currentTimeMillis())
        dir.listFiles().orEmpty()
            .filter { it.isFile }
            .sortedBy { it.lastModified() }
            .dropLast(MAX_DISK_FILES)
            .forEach { it.delete() }
    }

    private fun ByteArray.decode(): Bitmap? = BitmapFactory.decodeByteArray(this, 0, size)

    private fun sha256(value: String): String =
        MessageDigest.getInstance("SHA-256").digest(value.toByteArray(Charsets.UTF_8))
            .joinToString("") { "%02x".format(it) }

    private object Miss

    companion object {
        private const val MEMORY_BYTES = 8 * 1024 * 1024
        private const val MAX_DISK_FILES = 2000
    }
}

/**
 * A stored feed icon or article thumbnail. Only a modifier-sized box reserves
 * space until the bitmap is ready; the images are decorative and carry no
 * content description.
 */
@Composable
internal fun MediaImage(path: String?, modifier: Modifier = Modifier, shape: Shape = RectangleShape) {
    if (path.isNullOrEmpty()) return
    val model: AppViewModel = viewModel()
    val uiState by model.state.collectAsState()
    val bitmap by produceState<ImageBitmap?>(null, path, uiState.accountKey) {
        value = model.mediaBitmap(path)
    }
    val image = bitmap
    if (image == null) {
        Box(modifier.clip(shape))
    } else {
        Image(
            bitmap = image,
            contentDescription = null,
            modifier = modifier.clip(shape),
            contentScale = ContentScale.Crop,
        )
    }
}

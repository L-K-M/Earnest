package ch.lkmc.earnest

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.async
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File
import java.util.Base64

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class MediaCacheTest {
    private val context get() = ApplicationProvider.getApplicationContext<Context>()
    private val account = "a".repeat(64)
    private val other = "b".repeat(64)
    private val png = Base64.getDecoder()
        .decode("iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAEUlEQVR4nGP4z8DwH4QZYAwAR8oH+WdZbrcAAAAASUVORK5CYII=")

    @Before
    fun clean() {
        File(context.cacheDir, "media").deleteRecursively()
    }

    private fun cache() = MediaCache(context)

    @Test
    fun versionedLoadRoundTripsThroughDisk() = runBlocking {
        val path = "/v1/entries/2/thumbnail?v=one"
        var fetches = 0
        val cache = cache()
        assertNotNull(cache.load(account, path) { fetches++; png })
        assertEquals(1, fetches)
        // The second load comes from memory; a cold instance must hit disk.
        assertNotNull(cache.load(account, path) { fetches++; null })
        assertEquals(1, fetches)
        assertNotNull(cache().load(account, path) { fetches++; null })
        assertEquals(1, fetches)
        assertTrue(cache.fileFor(account, path).isFile)
    }

    @Test
    fun accountsDoNotShareCopies() = runBlocking {
        val path = "/v1/entries/2/thumbnail?v=one"
        val cache = cache()
        assertNotNull(cache.load(account, path) { png })
        var otherFetches = 0
        assertNotNull(cache.load(other, path) { otherFetches++; png })
        assertEquals("the second account must pay its own fetch", 1, otherFetches)
        assertTrue(cache.fileFor(other, path).isFile)
        assertNotNull(cache.load(other, path) { otherFetches++; null })
        assertEquals(1, otherFetches)
    }

    @Test
    fun diskKeepsAtMostTwoThousandFilesPerAccount() = runBlocking {
        val cache = cache()
        for (i in 1..2005) {
            cache.load(account, "/v1/entries/$i/thumbnail?v=$i") { png }
        }
        val dir = cache.fileFor(account, "/v1/entries/1/thumbnail?v=1").parentFile!!
        val files = dir.listFiles().orEmpty().filter { it.isFile }
        assertEquals(2000, files.size)
    }

    @Test
    fun evictionDropsLeastRecentlyUsed() = runBlocking {
        val cache = cache()
        // The fixture files use the cache's own layout; the trim runs
        // synchronously inside the write, so the assertions cannot race it.
        val oldest = cache.fileFor(account, "/v1/entries/1/thumbnail?v=old").apply {
            parentFile!!.mkdirs()
            writeBytes(png)
            setLastModified(1)
        }
        for (i in 2..2000) {
            cache.fileFor(account, "/v1/entries/$i/thumbnail?v=$i").apply {
                writeBytes(png)
                setLastModified(1000 + i.toLong())
            }
        }
        cache.load(account, "/v1/entries/2001/thumbnail?v=new") { png }
        assertFalse("the oldest copy survived eviction", oldest.isFile)
        assertTrue(cache.fileFor(account, "/v1/entries/2001/thumbnail?v=new").isFile)
        assertEquals(2000, oldest.parentFile!!.listFiles().orEmpty().filter { it.isFile }.size)
    }

    @Test
    fun anOversizedBodyIsRefusedBeforeItIsCached() = runBlocking {
        val cache = cache()
        val path = "/v1/entries/9/thumbnail?v=1"
        assertNull(cache.load(account, path) { ByteArray(600 * 1024) })
        assertFalse("an oversized body reached the disk", cache.fileFor(account, path).isFile)
    }

    @Test
    fun unversionedPathsNeverReachDisk() = runBlocking {
        val path = "/v1/entries/2/thumbnail"
        var fetches = 0
        val cache = cache()
        assertNotNull(cache.load(account, path) { fetches++; png })
        assertFalse(File(context.cacheDir, "media").exists())
        // Memory still serves an unversioned path inside the process.
        assertNotNull(cache.load(account, path) { fetches++; png })
        assertEquals(1, fetches)
    }

    @Test
    fun failedLoadsStayInMemoryOnly() = runBlocking {
        val path = "/v1/entries/2/thumbnail?v=missing"
        var fetches = 0
        val cache = cache()
        assertNull(cache.load(account, path) { fetches++; null })
        assertNull(cache.load(account, path) { fetches++; null })
        assertEquals("the miss must be remembered, not refetched", 1, fetches)
        assertFalse(cache.fileFor(account, path).exists())
    }

    // Regression: a fetch that fails (offline, server error) is not
    // remembered for good, so the image loads once the network is back, but
    // it waits a minute rather than refetching on every row appearance.
    @Test
    fun failedFetchesAreRetriedAfterADelay() = runBlocking {
        val path = "/v1/entries/3/thumbnail?v=retry"
        var fetches = 0
        var now = 1_000L
        val cache = MediaCache(context) { now }
        assertNull(cache.load(account, path) { fetches++; throw java.io.IOException("offline") })
        assertNull(cache.load(account, path) { fetches++; png })
        assertEquals("a retry within the delay must not fetch", 1, fetches)
        now += 60_001L
        assertNotNull(cache.load(account, path) { fetches++; png })
        assertEquals(2, fetches)
    }

    // An image larger than any the server stores is rejected without being
    // decoded at full size.
    @Test
    fun oversizedImagesAreNotDecoded() = runBlocking {
        val path = "/v1/entries/4/thumbnail?v=huge"
        val bitmap = android.graphics.Bitmap.createBitmap(2000, 10, android.graphics.Bitmap.Config.ARGB_8888)
        val bytes = java.io.ByteArrayOutputStream().also { bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }.toByteArray()
        assertNull(cache().load(account, path) { bytes })
    }

    @Test
    fun frontPageImagesAllowTheStored1200PixelDerivative() = runBlocking {
        val path = "/v1/entries/5/image?v=feature"
        val bitmap = android.graphics.Bitmap.createBitmap(1200, 800, android.graphics.Bitmap.Config.ARGB_8888)
        val bytes = java.io.ByteArrayOutputStream().also { bitmap.compress(android.graphics.Bitmap.CompressFormat.JPEG, 82, it) }.toByteArray()
        assertNotNull(cache().load(account, path) { bytes })
    }

    @Test
    fun frontPageImagesStillRejectDimensionsBeyondTheDerivative() = runBlocking {
        val path = "/v1/entries/5/image?v=huge"
        val bitmap = android.graphics.Bitmap.createBitmap(1201, 800, android.graphics.Bitmap.Config.ARGB_8888)
        val bytes = java.io.ByteArrayOutputStream().also { bitmap.compress(android.graphics.Bitmap.CompressFormat.JPEG, 82, it) }.toByteArray()
        assertNull(cache().load(account, path) { bytes })
    }

    @Test
    fun thumbnailsStillRejectTheLargerFrontPageDerivative() = runBlocking {
        val path = "/v1/entries/5/thumbnail?v=feature"
        val bitmap = android.graphics.Bitmap.createBitmap(1200, 800, android.graphics.Bitmap.Config.ARGB_8888)
        val bytes = java.io.ByteArrayOutputStream().also { bitmap.compress(android.graphics.Bitmap.CompressFormat.JPEG, 82, it) }.toByteArray()
        assertNull(cache().load(account, path) { bytes })
    }

    @Test
    fun frontPageImagesStillRejectOversizedBodies() = runBlocking {
        val path = "/v1/entries/5/image?v=huge"
        assertNull(cache().load(account, path) { ByteArray(8 * 1024 * 1024 + 1) })
    }

    @Test
    fun signOutDropsImagesAndFencesALateDownload() = runBlocking {
        val cache = cache()
        val path = "/v1/entries/5/image?v=one"
        assertNotNull(cache.load(account, path) { png })
        assertNotNull(cache.load(other, path) { png })
        cache.clearAccount(account)
        assertFalse(cache.fileFor(account, path).exists())
        assertTrue(cache.fileFor(other, path).exists())
        assertNull(cache.load(account, path) { null })

        val started = kotlinx.coroutines.CompletableDeferred<Unit>()
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        val latePath = "/v1/entries/6/image?v=late"
        val job = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.Dispatchers.IO).async {
            runCatching { cache.load(account, latePath) { started.complete(Unit); release.await(); png } }
        }
        started.await()
        cache.clearAccount(account)
        release.complete(Unit)
        job.await()
        assertFalse(cache.fileFor(account, latePath).exists())
        cache.close()
    }
}

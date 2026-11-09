package ch.lkmc.earnest

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
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
        val dir = File(context.cacheDir, "media/$account")
        dir.mkdirs()
        val oldest = File(dir, "0".repeat(64)).apply {
            writeBytes(png)
            setLastModified(1)
        }
        for (i in 1..1999) {
            File(dir, "%064x".format(i)).apply {
                writeBytes(png)
                setLastModified(1000 + i.toLong())
            }
        }
        cache.load(account, "/v1/entries/2/thumbnail?v=new") { png }
        assertFalse("the oldest copy survived eviction", oldest.isFile)
        assertTrue(cache.fileFor(account, "/v1/entries/2/thumbnail?v=new").isFile)
        assertEquals(2000, dir.listFiles().orEmpty().filter { it.isFile }.size)
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
    // remembered, so the image loads once the network is back.
    @Test
    fun failedFetchesAreRetried() = runBlocking {
        val path = "/v1/entries/3/thumbnail?v=retry"
        var fetches = 0
        val cache = cache()
        assertNull(cache.load(account, path) { fetches++; throw java.io.IOException("offline") })
        assertNotNull(cache.load(account, path) { fetches++; png })
        assertEquals(2, fetches)
    }
}

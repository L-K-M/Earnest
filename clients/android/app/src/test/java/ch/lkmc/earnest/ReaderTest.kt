package ch.lkmc.earnest

import android.content.Context
import android.net.Uri
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class ReaderTest {
    private val url = "https://content.test/signed"
    private fun request(url: String = this.url, main: Boolean = true) = object : WebResourceRequest {
        override fun getUrl() = Uri.parse(url)
        override fun isForMainFrame() = main
        override fun isRedirect() = false
        override fun hasGesture() = false
        override fun getMethod() = "GET"
        override fun getRequestHeaders() = emptyMap<String, String>()
    }
    private val error: WebResourceError = android.webkit.TestWebResourceError()

    @Test fun readerStateClearsLoadingOnFailureCancellationAndDeselection() = kotlinx.coroutines.runBlocking {
        val state = ReaderLoadState()
        state.load(2, "account") { throw java.io.IOException("offline") }
        assertFalse(state.loading)
        assertEquals("offline", state.error)
        try {
            state.load(2, "account") { throw kotlinx.coroutines.CancellationException("selection changed") }
            fail("reader swallowed cancellation")
        } catch (_: kotlinx.coroutines.CancellationException) {
        }
        assertFalse(state.loading)
        state.load(null, "account") { error("deselection fetched content") }
        assertNull(state.error)
        assertNull(state.content)
        state.load(2, "account") { ContentUrlResult("https://content.test/signed", null, null) }
        assertEquals("https://content.test/signed", state.content?.url)
        state.load(2, null) { error("signed-out reader fetched content") }
        assertNull(state.content)
        assertFalse(state.loading)
    }

    @Test fun missingAccountDoesNotLeaveReaderLoading() = kotlinx.coroutines.runBlocking {
        val state = ReaderLoadState()
        state.load(2, null) { error("unauthenticated reader fetched content") }
        assertFalse("reader spinner remained active without an account", state.loading)
        assertNull(state.content)
    }

    @Test fun mainFrameNetworkFailureLoadsCachedHtmlOnce() {
        val view = createReaderWebView(ApplicationProvider.getApplicationContext<Context>(), url, "<p>cached</p>")
        view.loadUrl(url)
        val client = view.webViewClient
        client.onReceivedError(view, request(main = false), error)
        assertEquals(url, shadowOf(view).lastLoadedUrl)
        client.onReceivedError(view, request(), error)
        val fallback = shadowOf(view).lastLoadedUrl
        assertEquals("https://appassets.androidplatform.net/article/index.html", fallback)
        val response = client.shouldInterceptRequest(view, request(fallback!!))!!
        assertEquals("<p>cached</p>", response.data.bufferedReader().readText())
        assertNotNull("fallback must not make live proxy requests", client.shouldInterceptRequest(view, request("https://content.test/image/tracker", main = false)))
        view.loadUrl("https://appassets.androidplatform.net/article/probe")
        client.onReceivedError(view, request(), error)
        assertEquals("https://appassets.androidplatform.net/article/probe", shadowOf(view).lastLoadedUrl)
        view.destroy()
    }

    @Test fun mainFrameHttpFailureLoadsCacheButSubresourcesDoNot() {
        val view = createReaderWebView(ApplicationProvider.getApplicationContext<Context>(), url, "cached")
        view.loadUrl(url)
        val response = WebResourceResponse("text/html", "UTF-8", 503, "Unavailable", emptyMap(), "error".byteInputStream())
        view.webViewClient.onReceivedHttpError(view, request(main = false), response)
        assertEquals(url, shadowOf(view).lastLoadedUrl)
        view.webViewClient.onReceivedHttpError(view, request(), response)
        assertEquals("https://appassets.androidplatform.net/article/index.html", shadowOf(view).lastLoadedUrl)
        view.destroy()
    }

    @Test fun noCacheDoesNotLoadEmptyFallback() {
        val view = createReaderWebView(ApplicationProvider.getApplicationContext<Context>(), url, null)
        view.loadUrl(url)
        view.webViewClient.onReceivedError(view, request(), error)
        assertEquals(url, shadowOf(view).lastLoadedUrl)
        assertFalse(view.settings.javaScriptEnabled)
        assertFalse(view.settings.allowFileAccess)
        assertFalse(view.settings.allowContentAccess)
        view.destroy()
    }
}

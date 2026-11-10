package ch.lkmc.earnest

import android.app.Activity
import android.app.Application
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.net.Uri
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import androidx.core.net.toUri
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class ReaderTest {
    private val url = "https://content.test/signed"
    private fun request(url: String = this.url, main: Boolean = true, gesture: Boolean = false) = object : WebResourceRequest {
        override fun getUrl() = Uri.parse(url)
        override fun isForMainFrame() = main
        override fun isRedirect() = false
        override fun hasGesture() = gesture
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

    @Test fun theOfflineCopyNotifiesOnceAndOnlyWithCache() {
        var notified = 0
        val view = createReaderWebView(ApplicationProvider.getApplicationContext<Context>(), url, "<p>cached</p>") { notified++ }
        view.loadUrl(url)
        val client = view.webViewClient
        client.onReceivedError(view, request(), error)
        client.onReceivedError(view, request(), error)
        assertEquals(1, notified)
        view.destroy()

        val uncached = createReaderWebView(ApplicationProvider.getApplicationContext<Context>(), url, null) { notified++ }
        uncached.loadUrl(url)
        uncached.webViewClient.onReceivedError(uncached, request(), error)
        assertEquals("no offline copy exists to announce", 1, notified)
        uncached.destroy()
    }

    @Test fun theFindLabelNamesTheMatchPosition() {
        assertEquals("No matches", findCountLabel(0, 0))
        assertEquals("1/1", findCountLabel(0, 1))
        assertEquals("3/12", findCountLabel(2, 12))
    }

    @Test fun textZoomStepsStayWithinBounds() {
        assertEquals(110, zoomStep(100, true))
        assertEquals(90, zoomStep(100, false))
        assertEquals(MAX_READER_ZOOM, zoomStep(MAX_READER_ZOOM, true))
        assertEquals(MIN_READER_ZOOM, zoomStep(MIN_READER_ZOOM, false))
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

    @Test fun readerUrlKeepsSignatureAndAddsTypography() {
        val typography = ch.lkmc.earnest.core.ReaderTypography(
            ch.lkmc.earnest.core.ReaderTheme.DARK,
            68,
            ch.lkmc.earnest.core.ReaderFont.DYSLEXIC,
        )
        val themed = readerUrl("https://content.test/content/7?sig=a.b%2Bc", typography, systemDark = false).toUri()
        assertEquals("a.b+c", themed.getQueryParameter("sig"))
        assertEquals("dark", themed.getQueryParameter("theme"))
        assertEquals("68", themed.getQueryParameter("measure"))
        assertEquals("dyslexic", themed.getQueryParameter("font"))
    }

    @Test fun articleThemeMapsEverySetting() {
        assertEquals("light", ch.lkmc.earnest.core.ReaderTheme.LIGHT.contentTheme(systemDark = true))
        assertEquals("dark", ch.lkmc.earnest.core.ReaderTheme.DARK.contentTheme(systemDark = false))
        assertEquals("black", ch.lkmc.earnest.core.ReaderTheme.BLACK.contentTheme(systemDark = false))
        assertEquals("eink", ch.lkmc.earnest.core.ReaderTheme.EINK.contentTheme(systemDark = false))
        assertEquals("dark", ch.lkmc.earnest.core.ReaderTheme.SYSTEM.contentTheme(systemDark = true))
        assertEquals("light", ch.lkmc.earnest.core.ReaderTheme.SYSTEM.contentTheme(systemDark = false))
    }

    @Test fun offlineFallbackAppliesSafeTypography() {
        val typography = ch.lkmc.earnest.core.ReaderTypography(
            ch.lkmc.earnest.core.ReaderTheme.BLACK,
            64,
            ch.lkmc.earnest.core.ReaderFont.SANS,
        )
        val html = offlineReaderHtml("<p>cached</p>", typography, systemDark = false)
        assertTrue(html.contains("max-width:64ch"))
        assertTrue(html.contains("system-ui,sans-serif"))
        assertTrue(html.contains("background:#000"))
        assertTrue(html.contains("<p>cached</p>"))
        assertFalse("the fallback must not enable scripts", html.contains("<script"))
    }

    @Test fun offlineFallbackKeepsPoliciesByNotInjectingRemoteResources() {
        val typography = ch.lkmc.earnest.core.ReaderTypography(
            ch.lkmc.earnest.core.ReaderTheme.EINK,
            72,
            ch.lkmc.earnest.core.ReaderFont.DYSLEXIC,
        )
        val html = offlineReaderHtml("<img src=\"https://tracker.test/pixel\">", typography, systemDark = true)
        assertTrue(html.contains("max-width:72ch"))
        assertTrue(html.contains("letter-spacing:.05em"))
        assertTrue(html.contains("background:#fff"))
    }

    @Test fun mainFrameNavigationStaysOnTheArticlePath() {
        val view = createReaderWebView(ApplicationProvider.getApplicationContext<Context>(), url, null)
        val client = view.webViewClient
        assertFalse("the signed article URL must keep loading in place", client.shouldOverrideUrlLoading(view, request("$url?sig=token", gesture = true)))
        assertTrue("a click-to-load /img/ link must not replace the article", client.shouldOverrideUrlLoading(view, request("https://content.test/img/sig/encoded", gesture = true)))
        assertTrue("another content-origin page must not replace the article", client.shouldOverrideUrlLoading(view, request("https://content.test/other", gesture = true)))
        assertTrue("an external link must not load in the reader", client.shouldOverrideUrlLoading(view, request("https://example.com/article", gesture = true)))
        view.destroy()
    }

    @Test fun openOriginalReturnsFalseWhenNoAppHandlesTheLink() {
        val application = ApplicationProvider.getApplicationContext<Application>()
        shadowOf(application).checkActivities(true)
        assertFalse(openOriginal(application, "https://example.com/article"))
    }

    @Test fun openOriginalReturnsFalseWhenLaunchIsDenied() {
        val context = object : ContextWrapper(ApplicationProvider.getApplicationContext<Context>()) {
            override fun startActivity(intent: Intent) {
                throw SecurityException("Browser launch denied")
            }
        }
        assertFalse(openOriginal(context, "https://example.com/article"))
    }

    @Test fun openOriginalLaunchesThePublisherUrl() {
        val application = ApplicationProvider.getApplicationContext<Application>()
        assertTrue(openOriginal(application, "https://example.com/article"))
        val intent = shadowOf(application).nextStartedActivity
        assertEquals(Intent.ACTION_VIEW, intent.action)
        assertEquals("https://example.com/article", intent.dataString)
        assertTrue(intent.flags and Intent.FLAG_ACTIVITY_NEW_TASK != 0)
    }

    @Test fun openOriginalPreservesTheActivityBackStack() {
        val controller = Robolectric.buildActivity(Activity::class.java).setup()
        try {
            val activity = controller.get()
            assertTrue(openOriginal(activity, "https://example.com/article"))
            val intent = shadowOf(activity).nextStartedActivity
            assertEquals("https://example.com/article", intent.dataString)
            assertEquals(0, intent.flags and Intent.FLAG_ACTIVITY_NEW_TASK)
        } finally {
            controller.pause().stop().destroy()
        }
    }

    @Test fun copyLinkPutsTheUrlOnTheClipboard() {
        val application = ApplicationProvider.getApplicationContext<Application>()
        assertTrue(copyLink(application, "https://example.com/article"))
        val clipboard = application.getSystemService(android.content.ClipboardManager::class.java)
        assertEquals("https://example.com/article", clipboard.primaryClip?.getItemAt(0)?.text?.toString())
    }

    @Test fun shareLinkOpensTheSystemShareSheet() {
        val application = ApplicationProvider.getApplicationContext<Application>()
        assertTrue(shareLink(application, "An article", "https://example.com/article"))
        val intent = shadowOf(application).nextStartedActivity
        assertEquals(Intent.ACTION_CHOOSER, intent.action)
        val send = intent.getParcelableExtra(Intent.EXTRA_INTENT, Intent::class.java)
        assertEquals(Intent.ACTION_SEND, send?.action)
        assertEquals("text/plain", send?.type)
        assertEquals("An article", send?.getStringExtra(Intent.EXTRA_SUBJECT))
        assertEquals("https://example.com/article", send?.getStringExtra(Intent.EXTRA_TEXT))
        assertTrue(intent.flags and Intent.FLAG_ACTIVITY_NEW_TASK != 0)
    }

    // The system chooser reports its own empty state when no app can share,
    // so this covers only the share sheet failing to start.
    @Test fun shareLinkReturnsFalseWhenTheShareSheetCannotBeStarted() {
        val application = ApplicationProvider.getApplicationContext<Application>()
        shadowOf(application).checkActivities(true)
        assertFalse(shareLink(application, "An article", "https://example.com/article"))
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

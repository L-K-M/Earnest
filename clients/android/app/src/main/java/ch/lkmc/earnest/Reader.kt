package ch.lkmc.earnest

import android.content.Context
import android.content.Intent
import android.os.Build
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.net.toUri
import androidx.webkit.WebViewAssetLoader
import ch.lkmc.earnest.core.CachedEntry
import java.io.ByteArrayInputStream
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

private const val ASSET_HOST = "appassets.androidplatform.net"
private const val ASSET_URL = "https://$ASSET_HOST/article/index.html"

/** Published date, falling back to when Earnest first saw the entry. */
internal fun entryDateLabel(entry: CachedEntry, zone: ZoneId = ZoneId.systemDefault()): String {
    val instant = entry.publishedAt?.let(::parseInstant)
        ?: entry.firstSeenAt.takeIf { it.isNotBlank() }?.let(::parseInstant)
    return instant?.atZone(zone)?.format(DateTimeFormatter.ofPattern("MMM d, yyyy")) ?: ""
}

/** The publisher's page, when the entry carries a browsable URL. */
internal fun originalUrl(entry: CachedEntry): String? =
    entry.url.takeIf { it.isNotBlank() }?.let { url ->
        runCatching { java.net.URI(url) }.getOrNull()
            // URI keeps the scheme's case; schemes are case-insensitive.
            ?.takeIf { it.scheme.equals("https", ignoreCase = true) || it.scheme.equals("http", ignoreCase = true) }
            ?.toString()
    }

private fun parseInstant(value: String): Instant? = runCatching { Instant.parse(value) }.getOrNull()

/**
 * The reading view. JavaScript, file access and content access are off. The
 * main frame may load only the signed content origin (or the local asset
 * origin when offline); every other link opens in the browser.
 */
@Composable
internal fun Reader(url: String?, cachedHtml: String?, modifier: Modifier = Modifier) {
    key(url, cachedHtml) {
        AndroidView(
            modifier = modifier,
            factory = { context -> createReaderWebView(context, url, cachedHtml) },
            update = { webView ->
                if (webView.tag != (url ?: cachedHtml)) {
                    webView.tag = url ?: cachedHtml
                    if (url != null) webView.loadUrl(url) else webView.loadUrl(ASSET_URL)
                }
            },
            onReset = null,
            onRelease = { it.destroy() },
        )
    }
}

/** State for one selection; cancellation clears the previous load before a new one starts. */
internal class ReaderLoadState {
    var content by mutableStateOf<ContentUrlResult?>(null)
        private set
    var error by mutableStateOf<String?>(null)
        private set
    var loading by mutableStateOf(false)
        private set

    suspend fun load(selected: Long?, accountKey: String?, fetch: suspend (Long) -> ContentUrlResult?) {
        content = null
        error = null
        loading = false
        val id = selected ?: return
        if (accountKey == null) return
        loading = true
        try {
            content = fetch(id)
        } catch (error: kotlinx.coroutines.CancellationException) {
            throw error
        } catch (failure: Exception) {
            error = failure.message ?: "Could not load this entry."
        } finally {
            loading = false
        }
    }
}

internal fun createReaderWebView(context: Context, url: String?, cachedHtml: String?): WebView {
    val allowedOrigin = url?.toUri()
    fun allowed(uri: android.net.Uri): Boolean = allowedOrigin != null &&
        uri.scheme == "https" && uri.host.equals(allowedOrigin.host, ignoreCase = true) &&
        (if (uri.port == -1) 443 else uri.port) == (if (allowedOrigin.port == -1) 443 else allowedOrigin.port)
    return WebView(context).apply {
        settings.javaScriptEnabled = false
        settings.allowFileAccess = false
        settings.allowContentAccess = false
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
            // API 29 still honours these; API 30+ removed them and defaults to false.
            @Suppress("DEPRECATION")
            settings.allowFileAccessFromFileURLs = false
            @Suppress("DEPRECATION")
            settings.allowUniversalAccessFromFileURLs = false
        }
        settings.blockNetworkLoads = false

        val loader = WebViewAssetLoader.Builder().addPathHandler("/article/") { path ->
            if (path == "index.html" && cachedHtml != null) {
                WebResourceResponse("text/html", "UTF-8", ByteArrayInputStream(cachedHtml.toByteArray(Charsets.UTF_8)))
            } else {
                null
            }
        }.build()

        webViewClient = object : WebViewClient() {
            private var usingFallback = url == null

            private fun fallback(view: WebView, request: WebResourceRequest) {
                if (usingFallback || cachedHtml == null || !request.isForMainFrame || !allowed(request.url)) return
                usingFallback = true
                view.loadUrl(ASSET_URL)
            }

            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                fallback(view, request)
            }

            override fun onReceivedHttpError(view: WebView, request: WebResourceRequest, response: WebResourceResponse) {
                fallback(view, request)
            }

            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                // The render process died; keep the app alive and let Compose recreate the view.
                return true
            }

            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                val uri = request.url
                if (request.isForMainFrame && !usingFallback && allowed(uri)) return false
                if (request.isForMainFrame && uri.toString() == ASSET_URL) return false
                if (request.hasGesture() && (uri.scheme == "https" || uri.scheme == "http")) {
                    runCatching { view.context.startActivity(Intent(Intent.ACTION_VIEW, uri)) }
                }
                return true
            }

            override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                val uri = request.url
                loader.shouldInterceptRequest(uri)?.let { return it }
                if (uri.host == ASSET_HOST) return empty()
                if (!usingFallback && allowed(uri)) return null
                return empty()
            }
        }
    }
}

private fun empty(): WebResourceResponse = WebResourceResponse("text/plain", "UTF-8", ByteArrayInputStream(ByteArray(0)))

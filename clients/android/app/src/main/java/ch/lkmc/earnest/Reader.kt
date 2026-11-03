package ch.lkmc.earnest

import android.content.Intent
import android.os.Build
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.net.toUri
import androidx.webkit.WebViewAssetLoader
import java.io.ByteArrayInputStream

private const val ASSET_HOST = "appassets.androidplatform.net"
private const val ASSET_URL = "https://$ASSET_HOST/article/index.html"

/**
 * The reading view. JavaScript, file access and content access are off. The
 * main frame may load only the signed content origin (or the local asset
 * origin when offline); every other link opens in the browser.
 */
@Composable
internal fun Reader(url: String?, cachedHtml: String?, modifier: Modifier = Modifier) {
    val allowedHost = url?.let { it.toUri().host }
    AndroidView(
        modifier = modifier,
        factory = { context ->
            WebView(context).apply {
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
                    override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                        // The render process died; keep the app alive and let Compose recreate the view.
                        return true
                    }

                    override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                        val uri = request.url
                        if (request.isForMainFrame && uri.host == allowedHost) return false
                        if (request.isForMainFrame && uri.host == ASSET_HOST) return false
                        if (request.hasGesture() && (uri.scheme == "https" || uri.scheme == "http")) {
                            runCatching { view.context.startActivity(Intent(Intent.ACTION_VIEW, uri)) }
                        }
                        return true
                    }

                    override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                        val uri = request.url
                        loader.shouldInterceptRequest(uri)?.let { return it }
                        if (uri.host == ASSET_HOST) return empty()
                        if (uri.host == allowedHost) return null
                        return empty()
                    }
                }
            }
        },
        update = { webView ->
            if (webView.tag != (url ?: cachedHtml)) {
                webView.tag = url ?: cachedHtml
                if (url != null) webView.loadUrl(url) else webView.loadUrl(ASSET_URL)
            }
        },
    )
}

private fun empty(): WebResourceResponse = WebResourceResponse("text/plain", "UTF-8", ByteArrayInputStream(ByteArray(0)))

package ch.lkmc.earnest

import android.content.Intent
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView
import androidx.webkit.WebViewAssetLoader
import java.io.ByteArrayInputStream

private const val READER_URL = "https://appassets.androidplatform.net/article/index.html"

@Composable
internal fun Reader(html: String, modifier: Modifier = Modifier) {
    AndroidView(modifier = modifier, factory = { context ->
        WebView(context).apply {
            settings.javaScriptEnabled = false
            settings.allowFileAccess = false
            settings.allowContentAccess = false
        }
    }, update = { webView ->
        if (webView.tag != html) {
            webView.tag = html
            val loader = WebViewAssetLoader.Builder().addPathHandler("/article/") { path ->
                if (path == "index.html") WebResourceResponse("text/html", "UTF-8", ByteArrayInputStream(html.toByteArray(Charsets.UTF_8))) else null
            }.build()
            webView.webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                    val uri = request.url
                    if (uri.scheme == "https" && uri.host == "appassets.androidplatform.net" && uri.port == -1 && uri.userInfo == null) return false
                    if (request.hasGesture() && (uri.scheme == "https" || uri.scheme == "http")) {
                        runCatching { view.context.startActivity(Intent(Intent.ACTION_VIEW, uri)) }
                    }
                    return true
                }
                override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse =
                    loader.shouldInterceptRequest(request.url)
                        ?: WebResourceResponse("text/plain", "UTF-8", ByteArrayInputStream(ByteArray(0)))
            }
            webView.loadUrl(READER_URL)
        }
    })
}

package ch.lkmc.earnest

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.Build
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.net.toUri
import androidx.webkit.WebViewAssetLoader
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.ReaderFont
import ch.lkmc.earnest.core.ReaderTheme
import ch.lkmc.earnest.core.ReaderTypography
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

/** Opens the publisher's page, reporting false when the browser cannot launch. */
internal fun openOriginal(context: Context, url: String): Boolean = try {
    val intent = Intent(Intent.ACTION_VIEW, url.toUri())
    // Non-activity callers need a task; preserve the reader's back stack otherwise.
    if (context !is Activity) intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    context.startActivity(intent)
    true
} catch (_: ActivityNotFoundException) {
    false
} catch (_: SecurityException) {
    false
}

/** Copies the article link to the clipboard, reporting false without one. */
internal fun copyLink(context: Context, url: String): Boolean {
    val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager ?: return false
    clipboard.setPrimaryClip(ClipData.newPlainText("Link", url))
    return true
}

/**
 * Shares the article link through the system share sheet, reporting false
 * when the sheet cannot start. The chooser reports its own empty state when
 * no app can share, so availability is not checked here: such a query would
 * need a package-visibility declaration and can fail open on Android 11+.
 */
internal fun shareLink(context: Context, title: String, url: String): Boolean = try {
    val send = Intent(Intent.ACTION_SEND).apply {
        type = "text/plain"
        putExtra(Intent.EXTRA_SUBJECT, title)
        putExtra(Intent.EXTRA_TEXT, url)
    }
    val chooser = Intent.createChooser(send, null)
    if (context !is Activity) chooser.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    context.startActivity(chooser)
    true
} catch (_: ActivityNotFoundException) {
    false
} catch (_: SecurityException) {
    false
}

/**
 * The reading view. JavaScript, file access and content access are off. The
 * main frame may load only the signed article URL (or the local asset
 * origin when offline); every other link opens in the browser. [typography]
 * reaches the article page through the content URL and the offline fallback.
 */
@Composable
internal fun Reader(
    url: String?,
    cachedHtml: String?,
    typography: ReaderTypography,
    textZoom: Int = 100,
    modifier: Modifier = Modifier,
    onFallback: () -> Unit = {},
    onWebView: (WebView?) -> Unit = {},
) {
    val systemDark = isSystemInDarkTheme()
    val displayUrl = url?.let { readerUrl(it, typography, systemDark) }
    val fallbackHtml = cachedHtml?.let { offlineReaderHtml(it, typography, systemDark) }
    val fallbackCallback = rememberUpdatedState(onFallback)
    val webViewCallback = rememberUpdatedState(onWebView)
    // The last WebView handed out; a release may run after a replacement has
    // published itself and must not null the newer instance.
    val published = remember { arrayOfNulls<WebView>(1) }
    key(url, fallbackHtml) {
        AndroidView(
            modifier = modifier,
            factory = { context ->
                createReaderWebView(context, displayUrl, fallbackHtml) { fallbackCallback.value() }
            },
            update = { webView ->
                published[0] = webView
                webViewCallback.value(webView)
                if (webView.settings.textZoom != textZoom) webView.settings.textZoom = textZoom
                if (webView.tag != (displayUrl ?: fallbackHtml)) {
                    webView.tag = displayUrl ?: fallbackHtml
                    if (displayUrl != null) webView.loadUrl(displayUrl) else webView.loadUrl(ASSET_URL)
                }
            },
            onReset = null,
            onRelease = {
                it.destroy()
                if (published[0] === it) {
                    published[0] = null
                    webViewCallback.value(null)
                }
            },
        )
    }
}

/** The reader's text zoom bounds; [zoomStep] never leaves them. */
internal const val MIN_READER_ZOOM = 80
internal const val MAX_READER_ZOOM = 200

/** The next text zoom step, clamped to the supported range. */
internal fun zoomStep(current: Int, up: Boolean): Int =
    (current + if (up) 10 else -10).coerceIn(MIN_READER_ZOOM, MAX_READER_ZOOM)

/** The find bar's match label: "3/12", or "No matches" for an empty search. */
internal fun findCountLabel(active: Int, total: Int): String =
    if (total <= 0) "No matches" else "${active + 1}/$total"

/** Maps the account's article theme to the content origin's `theme` value (internal/web/content.go). */
internal fun ReaderTheme.contentTheme(systemDark: Boolean): String = when (this) {
    ReaderTheme.SYSTEM -> if (systemDark) "dark" else "light"
    ReaderTheme.LIGHT -> "light"
    ReaderTheme.DARK -> "dark"
    ReaderTheme.BLACK -> "black"
    ReaderTheme.EINK -> "eink"
}

private fun ReaderFont.wireName(): String = when (this) {
    ReaderFont.SERIF -> "serif"
    ReaderFont.SANS -> "sans"
    ReaderFont.DYSLEXIC -> "dyslexic"
}

/**
 * Adds the display params the content origin reads (internal/web/content.go).
 * Only safe enum values travel; the server re-validates them.
 */
internal fun readerUrl(url: String, typography: ReaderTypography, systemDark: Boolean): String =
    url.toUri().buildUpon()
        .appendQueryParameter("theme", typography.theme.contentTheme(systemDark))
        .appendQueryParameter("measure", typography.measure.toString())
        .appendQueryParameter("font", typography.font.wireName())
        .toString()

/**
 * Mirrors the content origin's typography CSS for the offline fallback, so a
 * cached article keeps the account's safe enum-based presentation.
 */
internal fun readerCss(typography: ReaderTypography, systemDark: Boolean): String {
    val (background, foreground) = when (typography.theme.contentTheme(systemDark)) {
        "dark" -> "#242424" to "#eee"
        "black" -> "#000" to "#fff"
        "eink" -> "#fff" to "#000"
        else -> "#fff" to "#202020"
    }
    val family = when (typography.font) {
        ReaderFont.SERIF -> "Georgia,serif"
        ReaderFont.SANS -> "system-ui,sans-serif"
        ReaderFont.DYSLEXIC -> "Verdana,Arial,sans-serif"
    }
    val spacing = if (typography.font == ReaderFont.DYSLEXIC) "letter-spacing:.05em;word-spacing:.12em;" else ""
    return "body{max-width:${typography.measure}ch;margin:2rem auto;padding:0 1rem;font:1.1rem/1.65 $family;" +
        "background:$background;color:$foreground;${spacing}overflow-wrap:anywhere}" +
        "img{max-width:100%;height:auto}a{color:inherit}pre{overflow:auto}"
}

/** Wraps cached article HTML in the same safe document the content origin serves. */
internal fun offlineReaderHtml(cachedHtml: String, typography: ReaderTypography, systemDark: Boolean): String =
    "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">" +
        "<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" +
        "<style>${readerCss(typography, systemDark)}</style></head><body>$cachedHtml</body></html>"

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

internal fun createReaderWebView(context: Context, url: String?, cachedHtml: String?, onFallback: () -> Unit = {}): WebView {
    val allowedOrigin = url?.toUri()
    fun allowed(uri: android.net.Uri): Boolean = allowedOrigin != null &&
        uri.scheme == "https" && uri.host.equals(allowedOrigin.host, ignoreCase = true) &&
        (if (uri.port == -1) 443 else uri.port) == (if (allowedOrigin.port == -1) 443 else allowedOrigin.port)
    // Only the article path itself may drive the main frame: click-to-load
    // /img/ links share the origin and would otherwise replace the article.
    fun article(uri: android.net.Uri): Boolean = allowed(uri) && uri.encodedPath == allowedOrigin?.encodedPath
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
                onFallback()
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
                if (request.isForMainFrame && !usingFallback && article(uri)) return false
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

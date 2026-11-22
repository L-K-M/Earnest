package ch.lkmc.earnest

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.os.Build
import android.view.ViewGroup
import android.view.ViewOutlineProvider
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
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
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
internal fun copyLink(context: Context, url: String): Boolean = copyText(context, "Link", url)

/** Copies plain text to the clipboard, reporting false without one. */
internal fun copyText(context: Context, label: String, text: String): Boolean {
    val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager ?: return false
    clipboard.setPrimaryClip(ClipData.newPlainText(label, text))
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
 *
 * With [onContentHeight] the page lays out at its whole content height, for a
 * scrolling parent to move, and reports that height; [modifier] must then
 * leave the height unbounded. With [swipe] the page takes part in article
 * swipes.
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
    onLiveContent: () -> Unit = {},
    onContentHeight: ((Int) -> Unit)? = null,
    onPageLoaded: () -> Unit = {},
    onRendererLost: () -> Unit = {},
    swipe: ArticleSwipeState? = null,
) {
    val systemDark = isSystemInDarkTheme()
    val displayUrl = remember(url, typography, systemDark) { url?.let { readerUrl(it, typography, systemDark) } }
    val fallbackHtml = remember(cachedHtml, typography, systemDark) {
        cachedHtml?.let { offlineReaderHtml(it, typography, systemDark) }
    }
    val backgroundColor = readerBackground(typography, systemDark)
    val fallbackCallback = rememberUpdatedState(onFallback)
    val liveContentCallback = rememberUpdatedState(onLiveContent)
    val webViewCallback = rememberUpdatedState(onWebView)
    val heightCallback = rememberUpdatedState(onContentHeight)
    val loadedCallback = rememberUpdatedState(onPageLoaded)
    val lostCallback = rememberUpdatedState(onRendererLost)
    val swipeState = rememberUpdatedState(swipe)
    // The last WebView handed out; a release may run after a replacement has
    // published itself and must not null the newer instance.
    val published = remember { arrayOfNulls<WebView>(1) }
    key(url, fallbackHtml) {
        var rendererLost by remember { mutableStateOf(false) }
        if (rendererLost) {
            QuietState(
                title = "The article renderer stopped.",
                message = "Your place in the list is safe.",
                modifier = modifier,
                icon = EarnestIcons.Alert,
                tone = LocalTones.current.amber,
                actionLabel = "Reload article",
                onAction = { rendererLost = false },
            )
        } else AndroidView(
            modifier = (if (swipe == null) Modifier else Modifier.articlePage(swipe) { published[0] }).then(modifier),
            factory = { context ->
                createReaderWebView(
                    context, displayUrl, fallbackHtml,
                    onContentHeight = onContentHeight?.let { { height -> heightCallback.value?.invoke(height) } },
                    onRendererLost = {
                        if (published[0] === it) {
                            rendererLost = true
                            lostCallback.value()
                        }
                    },
                    onPageLoaded = { loadedCallback.value() },
                    onScrollRefused = { swipeState.value?.pageRefusedScroll() },
                    backgroundColor = backgroundColor,
                    onLiveContent = { liveContentCallback.value() },
                    onFallback = { fallbackCallback.value() },
                )
            },
            update = { webView ->
                published[0] = webView
                webViewCallback.value(webView)
                webView.setBackgroundColor(backgroundColor)
                if (webView.settings.textZoom != textZoom) webView.settings.textZoom = textZoom
                if (webView.tag != (displayUrl ?: fallbackHtml)) {
                    webView.tag = displayUrl ?: fallbackHtml
                    if (displayUrl != null) webView.loadUrl(displayUrl) else webView.loadUrl(ASSET_URL)
                }
            },
            onReset = null,
            onRelease = {
                if (published[0] === it) {
                    published[0] = null
                    webViewCallback.value(null)
                }
                it.destroy()
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

internal const val NO_MATCHES = "No matches"

/** The find bar's match label: "3/12", or "No matches" for an empty search. */
internal fun findCountLabel(active: Int, total: Int): String =
    if (total <= 0) NO_MATCHES else "${active + 1}/$total"

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

internal fun readerBackground(typography: ReaderTypography, systemDark: Boolean): Int =
    Color.parseColor(readerColors(typography, systemDark).first)

/** The text colour of the same canvas, for chrome drawn on it. */
internal fun readerForeground(typography: ReaderTypography, systemDark: Boolean): Int =
    when (typography.theme.contentTheme(systemDark)) {
        // The CSS uses three-digit colours, which parseColor does not accept.
        "dark" -> Color.parseColor("#eeeeee")
        "black" -> Color.WHITE
        "eink" -> Color.BLACK
        else -> Color.parseColor("#202020")
    }

private fun readerColors(typography: ReaderTypography, systemDark: Boolean): Pair<String, String> =
    when (typography.theme.contentTheme(systemDark)) {
        "dark" -> "#242424" to "#eee"
        "black" -> "#000000" to "#fff"
        "eink" -> "#ffffff" to "#000"
        else -> "#ffffff" to "#202020"
    }

/**
 * Mirrors the content origin's typography CSS for the offline fallback, so a
 * cached article keeps the account's safe enum-based presentation.
 */
internal fun readerCss(typography: ReaderTypography, systemDark: Boolean): String {
    val (background, foreground) = readerColors(typography, systemDark)
    val scheme = if (typography.theme.contentTheme(systemDark) in setOf("dark", "black")) "dark" else "light"
    val family = when (typography.font) {
        ReaderFont.SERIF -> "Georgia,serif"
        ReaderFont.SANS -> "system-ui,sans-serif"
        ReaderFont.DYSLEXIC -> "Verdana,Arial,sans-serif"
    }
    val spacing = if (typography.font == ReaderFont.DYSLEXIC) "letter-spacing:.05em;word-spacing:.12em;" else ""
    return "html{color-scheme:$scheme;background:$background;color:$foreground}" +
        "body{max-width:${typography.measure}ch;margin:2rem auto;padding:0 1rem;font:1.1rem/1.65 $family;" +
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
    /** The article the other fields describe; a new selection precedes its load by a frame. */
    var entryId by mutableStateOf<Long?>(null)
        private set
    var content by mutableStateOf<ContentUrlResult?>(null)
        private set
    var error by mutableStateOf<String?>(null)
        private set
    var loading by mutableStateOf(false)
        private set

    suspend fun load(selected: Long?, accountKey: String?, fetch: suspend (Long) -> ContentUrlResult?) {
        entryId = selected
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

internal fun createReaderWebView(
    context: Context,
    url: String?,
    cachedHtml: String?,
    onContentHeight: ((Int) -> Unit)? = null,
    backgroundColor: Int = Color.WHITE,
    onRendererLost: (WebView) -> Unit = {},
    onPageLoaded: () -> Unit = {},
    onScrollRefused: () -> Unit = {},
    onLiveContent: () -> Unit = {},
    onFallback: () -> Unit = {},
): WebView {
    val allowedOrigin = url?.toUri()
    fun allowed(uri: android.net.Uri): Boolean = allowedOrigin != null &&
        uri.scheme == "https" && uri.host.equals(allowedOrigin.host, ignoreCase = true) &&
        (if (uri.port == -1) 443 else uri.port) == (if (allowedOrigin.port == -1) 443 else allowedOrigin.port)
    // Only the article path itself may drive the main frame: click-to-load
    // /img/ links share the origin and would otherwise replace the article.
    fun article(uri: android.net.Uri): Boolean = allowed(uri) && uri.encodedPath == allowedOrigin?.encodedPath
    return object : WebView(context) {
        override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) {
            super.onSizeChanged(w, h, oldw, oldh)
            // Under WRAP_CONTENT the view's own height is the document's. The
            // scroll range is not: it can count the viewport on top of it.
            if (onContentHeight != null && h > 0) post { onContentHeight(h) }
        }

        override fun onOverScrolled(scrollX: Int, scrollY: Int, clampedX: Boolean, clampedY: Boolean) {
            super.onOverScrolled(scrollX, scrollY, clampedX, clampedY)
            // Chromium gets here only with movement that no element on the page took.
            if (clampedX) onScrollRefused()
        }
    }.apply {
        // Cover the first Chromium frame and clip its separate native draw
        // layer while Compose resizes the preview. CSS alone cannot do either.
        setBackgroundColor(backgroundColor)
        outlineProvider = ViewOutlineProvider.BOUNDS
        clipToOutline = true
        if (onContentHeight != null) {
            // Asks Chromium to size the view from the document. An explicit
            // height would also overflow Compose's constraints on long articles.
            layoutParams = ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            // The list draws the overscroll stretch. The page's own would need
            // a layer as tall as the article, which can exceed the GPU's limit.
            overScrollMode = android.view.View.OVER_SCROLL_NEVER
        }
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

            private fun currentDocument(address: String) =
                if (usingFallback) address == ASSET_URL else article(address.toUri())

            override fun onPageCommitVisible(view: WebView, url: String) {
                if (!usingFallback && currentDocument(url)) onLiveContent()
            }

            override fun onPageFinished(view: WebView, url: String) {
                if (currentDocument(url)) onPageLoaded()
            }

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
                onRendererLost(view)
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
        // A reader starting directly on the cached copy shows it from the
        // first frame; the error paths above report the same moment later.
        if (url == null && cachedHtml != null) onFallback()
    }
}

private fun empty(): WebResourceResponse = WebResourceResponse("text/plain", "UTF-8", ByteArrayInputStream(ByteArray(0)))

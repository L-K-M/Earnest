import Foundation
import AppKit
import SwiftUI
import WebKit
import EarnestKit

// Article HTML is untrusted. Only an explicitly authorized content origin may
// supply images; navigation policy alone cannot guard network subresources.
final class ArticleScheme: NSObject, WKURLSchemeHandler {
    var html = ""
    var imageOrigin: URL?
    var style = ReaderStyle.default

    func webView(_ webView: WKWebView, start task: WKURLSchemeTask) {
        guard let url = task.request.url, url.scheme == "earnest-content", url.host == "article" else {
            task.didFailWithError(URLError(.badURL))
            return
        }
        let data = Data(page.utf8)
        task.didReceive(URLResponse(url: url, mimeType: "text/html", expectedContentLength: data.count, textEncodingName: "utf-8"))
        task.didReceive(data)
        task.didFinish()
    }

    var page: String {
        let imageSource = imageOrigin?.absoluteString ?? "'none'"
        return """
        <!doctype html><html><head><meta charset="utf-8">
        <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src \(imageSource); style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
        <meta name="color-scheme" content="\(style.colorScheme)">
        <style>\(style.css)</style>
        </head><body>\(html)</body></html>
        """
    }

    func webView(_ webView: WKWebView, stop task: WKURLSchemeTask) {}
}

struct Reader: NSViewRepresentable {
    let html: String
    var imageOrigin: URL? = nil
    var style: ReaderStyle = .default

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.defaultWebpagePreferences.allowsContentJavaScript = false
        config.websiteDataStore = .nonPersistent()
        config.setURLSchemeHandler(context.coordinator.scheme, forURLScheme: "earnest-content")
        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        return web
    }

    func updateNSView(_ web: WKWebView, context: Context) {
        let scheme = context.coordinator.scheme
        guard scheme.html != html || scheme.imageOrigin != imageOrigin || scheme.style != style || web.url == nil else { return }
        scheme.html = html
        scheme.imageOrigin = imageOrigin
        scheme.style = style
        web.load(URLRequest(url: URL(string: "earnest-content://article/")!))
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        let scheme = ArticleScheme()

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            guard let url = action.request.url else { decisionHandler(.cancel); return }
            if url.scheme == "earnest-content", url.host == "article" {
                decisionHandler(.allow)
                return
            }
            if action.navigationType == .linkActivated, ["http", "https"].contains(url.scheme?.lowercased() ?? "") {
                NSWorkspace.shared.open(url)
            }
            decisionHandler(.cancel)
        }
    }
}

struct ReaderHeader: View {
    let entry: EarnestKit.APIEntry
    let feedTitle: String
    let openOriginal: () -> Void

    var date: Date? {
        entry.publishedAt.flatMap(DateFormatting.parse) ?? DateFormatting.parse(entry.firstSeenAt)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(entry.title.isEmpty ? "Untitled" : entry.title)
                .font(.title)
                .accessibilityAddTraits(.isHeader)
                .textSelection(.enabled)
            Text(metadata)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
            Button("Open Original", action: openOriginal)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
    }

    private var metadata: String {
        var fields = [feedTitle.isEmpty ? "Feed" : feedTitle]
        if !entry.author.isEmpty { fields.append(entry.author) }
        if let date { fields.append(date.formatted(date: .abbreviated, time: .shortened)) }
        return fields.joined(separator: " · ")
    }
}

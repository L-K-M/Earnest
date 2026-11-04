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
        <meta name="color-scheme" content="light dark">
        <style>
        :root{color-scheme:light dark;background:Canvas;color:CanvasText}
        /* Signed pages also contain publisher-independent server styles. Keep the native reader palette. */
        body{max-width:68ch!important;margin:2rem auto;padding:0 1rem;font:1.1rem/1.65 Georgia,serif;overflow-wrap:anywhere;background:Canvas!important;color:CanvasText!important}
        img{max-width:100%;height:auto}pre{overflow:auto}a{color:inherit}
        </style>
        </head><body>\(html)</body></html>
        """
    }

    func webView(_ webView: WKWebView, stop task: WKURLSchemeTask) {}
}

struct Reader: NSViewRepresentable {
    let html: String
    var imageOrigin: URL? = nil

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
        guard context.coordinator.scheme.html != html || context.coordinator.scheme.imageOrigin != imageOrigin || web.url == nil else { return }
        context.coordinator.scheme.html = html
        context.coordinator.scheme.imageOrigin = imageOrigin
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

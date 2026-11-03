import Foundation
import AppKit
import SwiftUI
import WebKit

// The content is the server's sanitized HTML. The restrictive CSP blocks all
// network subresources, including those that do not trigger navigation policy.
final class ArticleScheme: NSObject, WKURLSchemeHandler {
    var html = ""

    func webView(_ webView: WKWebView, start task: WKURLSchemeTask) {
        guard let url = task.request.url, url.scheme == "earnest-content", url.host == "article" else {
            task.didFailWithError(URLError(.badURL))
            return
        }
        let page = """
        <!doctype html><html><head><meta charset="utf-8">
        <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
        <style>body{max-width:68ch;margin:2rem auto;padding:0 1rem;font:1.1rem/1.65 Georgia,serif;overflow-wrap:anywhere}img{max-width:100%}</style>
        </head><body>\(html)</body></html>
        """
        let data = Data(page.utf8)
        task.didReceive(URLResponse(url: url, mimeType: "text/html", expectedContentLength: data.count, textEncodingName: "utf-8"))
        task.didReceive(data)
        task.didFinish()
    }

    func webView(_ webView: WKWebView, stop task: WKURLSchemeTask) {}
}

struct Reader: NSViewRepresentable {
    let html: String

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
        guard context.coordinator.scheme.html != html || web.url == nil else { return }
        context.coordinator.scheme.html = html
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

import Foundation
import AppKit
import SwiftUI
import WebKit
import EarnestKit

/// The article's title block, rendered by Earnest inside the reader page so
/// it scrolls with the text. Every field is publisher data and is escaped;
/// only an HTTP(S) article link becomes a link.
struct ArticleHeader: Equatable {
    let title: String
    let feedTitle: String
    let author: String
    let date: Date?
    let link: URL?

    init(entry: EarnestKit.APIEntry, feedTitle: String) {
        title = entry.title.isEmpty ? "Untitled" : entry.title
        self.feedTitle = feedTitle.isEmpty ? "Feed" : feedTitle
        author = entry.author
        date = entry.publishedAt.flatMap(DateFormatting.parse) ?? DateFormatting.parse(entry.firstSeenAt)
        link = WebLink.url(entry.url)
    }

    var html: String {
        var meta = [Self.escape(feedTitle)]
        if !author.isEmpty { meta.append(Self.escape(author)) }
        if let date {
            let text = date.formatted(date: .abbreviated, time: .shortened)
            meta.append("<time datetime=\"\(Self.escape(ISO8601DateFormatter().string(from: date)))\">\(Self.escape(text))</time>")
        }
        let heading = link.map { "<a href=\"\(Self.escape($0.absoluteString))\">\(Self.escape(title))</a>" } ?? Self.escape(title)
        return "<header class=\"earnest-header\"><h1 dir=\"auto\">\(heading)</h1>"
            + "<p class=\"earnest-meta\" dir=\"auto\">\(meta.joined(separator: " · "))</p></header>"
    }

    static func escape(_ text: String) -> String {
        var escaped = ""
        escaped.reserveCapacity(text.count)
        for character in text {
            switch character {
            case "&": escaped += "&amp;"
            case "<": escaped += "&lt;"
            case ">": escaped += "&gt;"
            case "\"": escaped += "&quot;"
            case "'": escaped += "&#39;"
            default: escaped.append(character)
            }
        }
        return escaped
    }
}

// Article HTML is untrusted. Only an explicitly authorized content origin may
// supply images; navigation policy alone cannot guard network subresources.
final class ArticleScheme: NSObject, WKURLSchemeHandler {
    var html = ""
    var header: ArticleHeader?
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
        </head><body>\(header?.html ?? "")\(html)</body></html>
        """
    }

    func webView(_ webView: WKWebView, stop task: WKURLSchemeTask) {}
}

/// The live web view behind one window's reader, for the View menu's zoom
/// commands and Edit > Find. The view registers itself when it is made.
@MainActor final class ReaderController: ObservableObject {
    weak var webView: WKWebView?
    @Published var findVisible = false
    @Published var findQuery = ""
    /// Nil before a search; false when the last search found nothing.
    @Published private(set) var findMatched: Bool?
    /// Incremented to put keyboard focus in the find field.
    @Published var findFocusRequest = 0

    static let zoomKey = "readerZoom"
    static let zoomSteps: [Double] = [0.75, 0.85, 1, 1.15, 1.3, 1.5, 1.75, 2]

    static var zoom: Double {
        let stored = UserDefaults.standard.double(forKey: zoomKey)
        return stored == 0 ? 1 : stored
    }

    static func setZoom(_ step: Int) {
        let index = zoomSteps.firstIndex { $0 >= zoom - 0.001 } ?? zoomSteps.count - 1
        let next = zoomSteps[min(zoomSteps.count - 1, max(0, index + step))]
        UserDefaults.standard.set(next, forKey: zoomKey)
    }

    static func resetZoom() { UserDefaults.standard.set(1.0, forKey: zoomKey) }
    static var canZoomIn: Bool { zoom < (zoomSteps.last ?? 2) - 0.001 }
    static var canZoomOut: Bool { zoom > (zoomSteps.first ?? 0.75) + 0.001 }

    /// A changed query has not been searched yet.
    func queryEdited() { findMatched = nil }

    func showFind() {
        findVisible = true
        findFocusRequest &+= 1
    }

    func hideFind() {
        findVisible = false
        findMatched = nil
        clearFindHighlight()
        // Return focus to the article so the keyboard keeps scrolling it.
        webView?.window?.makeFirstResponder(webView)
    }

    /// Two private-use characters, which article text does not contain.
    static let unmatchable = "\u{E000}\u{E001}"

    /// WebKit keeps its yellow match indicator up after the find bar closes,
    /// and has no public call to hide it; a search that finds nothing does.
    /// (An empty search is ignored and leaves the indicator up.)
    private func clearFindHighlight() {
        webView?.find(Self.unmatchable, configuration: WKFindConfiguration()) { _ in }
    }

    func find(backwards: Bool = false) {
        guard let webView, !findQuery.isEmpty else {
            findMatched = nil
            return
        }
        let configuration = WKFindConfiguration()
        configuration.backwards = backwards
        configuration.caseSensitive = false
        configuration.wraps = true
        webView.find(findQuery, configuration: configuration) { [weak self] result in
            self?.findMatched = result.matchFound
            if !result.matchFound { NSSound.beep() }
        }
    }

    func findNext() {
        guard findVisible, !findQuery.isEmpty else { showFind(); return }
        find()
    }

    func findPrevious() {
        guard findVisible, !findQuery.isEmpty else { showFind(); return }
        find(backwards: true)
    }
}

struct Reader: NSViewRepresentable {
    let html: String
    var header: ArticleHeader? = nil
    var imageOrigin: URL? = nil
    var style: ReaderStyle = .default
    var zoom: Double = 1
    var controller: ReaderController? = nil

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.defaultWebpagePreferences.allowsContentJavaScript = false
        config.websiteDataStore = .nonPersistent()
        config.setURLSchemeHandler(context.coordinator.scheme, forURLScheme: "earnest-content")
        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.allowsMagnification = true
        web.setAccessibilityLabel("Article")
        controller?.webView = web
        return web
    }

    func updateNSView(_ web: WKWebView, context: Context) {
        controller?.webView = web
        if abs(web.pageZoom - zoom) > 0.001 { web.pageZoom = zoom }
        let scheme = context.coordinator.scheme
        guard scheme.html != html || scheme.header != header || scheme.imageOrigin != imageOrigin
                || scheme.style != style || web.url == nil else { return }
        scheme.html = html
        scheme.header = header
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
            if action.navigationType == .linkActivated, let web = WebLink.validated(url) {
                NSWorkspace.shared.open(web)
            }
            decisionHandler(.cancel)
        }
    }
}

/// The detail column: the open article with its notes, find bar and empty
/// states. Publisher HTML stays inside the restricted web view.
@MainActor struct ReaderPane: View {
    @EnvironmentObject private var window: WindowModel
    @ObservedObject var reader: ReaderController
    @AppStorage(ReaderController.zoomKey) private var zoom = 1.0
    @FocusState private var findFocused: Bool
    @SwiftUI.State private var showSpinner = false

    var body: some View {
        Group {
            if window.selected == nil {
                ContentUnavailableView("No Article Selected", systemImage: "doc.text",
                                       description: Text("Choose an article from the list."))
            } else if window.articleUnavailable {
                ContentUnavailableView("Not Available Offline", systemImage: "wifi.slash",
                                       description: Text("Earnest keeps articles you have opened. This one loads when you are back online."))
            } else if let failure = window.readerFailure {
                ContentUnavailableView {
                    Label("Couldn’t Load Article", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(failure)
                } actions: {
                    Button("Try Again") { window.retryOpen() }
                }
            } else {
                article
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(nsColor: .textBackgroundColor))
    }

    private var header: ArticleHeader? {
        window.readingEntry.map { ArticleHeader(entry: $0, feedTitle: window.readingFeedTitle) }
    }

    private var loading: Bool { window.html.isEmpty && !window.articleUnavailable }

    private var article: some View {
        VStack(spacing: 0) {
            if reader.findVisible { findBar; Divider() }
            Reader(html: window.html, header: header, imageOrigin: window.imageOrigin,
                   style: window.readerStyle, zoom: zoom, controller: reader)
                .opacity(loading ? 0 : 1)
                .overlay {
                    if loading && showSpinner {
                        ProgressView().controlSize(.small).accessibilityLabel("Loading article")
                    }
                }
                .overlay(alignment: .bottom) { notes }
        }
        // Only show a spinner when loading is slow enough to notice.
        .task(id: window.selected) {
            showSpinner = false
            try? await Task.sleep(for: .milliseconds(250))
            showSpinner = true
        }
    }

    @ViewBuilder private var notes: some View {
        if window.imageMode == .click || window.offlineCopy {
            HStack(spacing: 10) {
                Image(systemName: window.offlineCopy ? "icloud.slash" : "photo.badge.exclamationmark")
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                Text(window.offlineCopy ? "Offline copy. Images are blocked." : "Images are blocked for this article.")
                    .font(.callout)
                if window.imageMode == .click && !window.offlineCopy {
                    Button("Load Images") { window.loadImages() }
                        .buttonStyle(.borderless)
                        .help("Load this article's images through your Earnest server")
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .glassEffect(.regular, in: .capsule)
            .padding(.bottom, 14)
            .accessibilityElement(children: .contain)
        }
    }

    private var findBar: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").foregroundStyle(.secondary).accessibilityHidden(true)
            TextField("Find in Article", text: $reader.findQuery)
                .textFieldStyle(.roundedBorder)
                .focused($findFocused)
                .frame(maxWidth: 260)
                .onSubmit { reader.find() }
                .onChange(of: reader.findQuery) { reader.queryEdited() }
                .onKeyPress(.escape) {
                    reader.hideFind()
                    return .handled
                }
            ControlGroup {
                Button("Previous", systemImage: "chevron.left") { reader.find(backwards: true) }
                    .help("Find Previous (⇧⌘G)")
                Button("Next", systemImage: "chevron.right") { reader.find() }
                    .help("Find Next (⌘G)")
            }
            .controlGroupStyle(.navigation)
            .disabled(reader.findQuery.isEmpty)
            if reader.findMatched == false {
                Text("Not found").font(.callout).foregroundStyle(.secondary)
            }
            Spacer()
            Button("Done") { reader.hideFind() }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(.bar)
        .onChange(of: reader.findFocusRequest, initial: true) { findFocused = reader.findVisible }
    }
}

import AppKit
import ApplicationServices
import Network
import SwiftUI
import WebKit
import XCTest
import EarnestKit
@testable import Earnest

@MainActor final class ReaderTests: XCTestCase {
    func testReaderBackgroundFollowsDarkAppearance() async throws {
        let scheme = ArticleScheme()
        scheme.html = "<!doctype html><html><head><style>body{background:#fff;color:#171717}</style></head><body><p>Article</p></body></html>"
        let configuration = WKWebViewConfiguration()
        configuration.defaultWebpagePreferences.allowsContentJavaScript = false
        configuration.websiteDataStore = .nonPersistent()
        configuration.setURLSchemeHandler(scheme, forURLScheme: "earnest-content")
        let web = WKWebView(frame: NSRect(x: 0, y: 0, width: 600, height: 400), configuration: configuration)
        let window = NSWindow(contentRect: web.frame, styleMask: [.titled], backing: .buffered, defer: false)
        window.appearance = NSAppearance(named: .darkAqua)
        window.contentView = web
        window.orderFront(nil)
        defer { window.orderOut(nil) }
        let loaded = expectation(description: "Reader loaded")
        let delegate = PageLoaded { loaded.fulfill() }
        web.navigationDelegate = delegate
        web.load(URLRequest(url: URL(string: "earnest-content://article/")!))
        await fulfillment(of: [loaded], timeout: 15)
        // Allow WebKit's drawing transaction to reach the hosted window.
        try await Task.sleep(for: .milliseconds(200))
        let snapshot = try await web.takeSnapshot(configuration: nil)
        let attachment = XCTAttachment(image: snapshot)
        attachment.lifetime = .keepAlways
        add(attachment)
        let bitmap = try XCTUnwrap(NSBitmapImageRep(data: XCTUnwrap(snapshot.tiffRepresentation)))
        let color = try XCTUnwrap(bitmap.colorAt(x: bitmap.pixelsWide - 10, y: bitmap.pixelsHigh - 10)?.usingColorSpace(.deviceRGB))
        XCTAssertLessThan(max(color.redComponent, color.greenComponent, color.blueComponent), 0.3,
                          "Reader background must be dark, got \(color)")
        window.appearance = NSAppearance(named: .aqua)
        try await Task.sleep(for: .milliseconds(200))
        let light = try await web.takeSnapshot(configuration: nil)
        let lightBitmap = try XCTUnwrap(NSBitmapImageRep(data: XCTUnwrap(light.tiffRepresentation)))
        let lightColor = try XCTUnwrap(lightBitmap.colorAt(x: lightBitmap.pixelsWide - 10, y: lightBitmap.pixelsHigh - 10)?.usingColorSpace(.deviceRGB))
        XCTAssertGreaterThan(min(lightColor.redComponent, lightColor.greenComponent, lightColor.blueComponent), 0.9)
    }

    func testRevertAccessibilityNamesActionAndArticle() async throws {
        let model = AppModel()
        let windowModel = WindowModel(app: model)
        windowModel.entries = [try JSONDecoder().decode(APIEntry.self, from: Data(ContentLoadingTests.entryJSON.utf8))]
        model.activityGroups = [ActivityGroup(principalID: 2, name: "Scout", items: [ActivityItem(.object([
            "id": .integer(8), "principal_id": .integer(2), "action": .string("state.updated"),
            "target": .object(["entry_id": .integer(42), "register": .string("shelf")]),
            "after": .object(["shelf": .object(["value": .string("done")])]),
            "next_action": .string("revert")
        ])), ActivityItem(.object([
            "id": .integer(9), "principal_id": .integer(2), "action": .string("notification.created"),
            "target": .object(["id": .integer(19)]),
            "after": .object(["entry_ids": .array([.integer(42)])]),
            "next_action": .string("cancel")
        ]))])]
        let view = NSHostingView(rootView: ActivityView().environmentObject(model).environmentObject(windowModel))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 320, height: 400),
                              styleMask: [.titled], backing: .buffered, defer: false)
        window.contentView = view
        window.orderFront(nil)
        defer { window.orderOut(nil) }
        view.layoutSubtreeIfNeeded()
        try await Task.sleep(for: .milliseconds(100))
        let pid = ProcessInfo.processInfo.processIdentifier
        // Request the native AX tree once so SwiftUI materializes virtual list children.
        let result = await Task.detached {
            let application = AXUIElementCreateApplication(pid)
            var windows: CFTypeRef?
            return AXUIElementCopyAttributeValue(application, kAXWindowsAttribute as CFString, &windows)
        }.value
        XCTAssertEqual(result, .success)
        let buttons = accessibilityButtons(view)
        XCTAssertTrue(buttons.contains { attribute($0, "accessibilityLabel") as? String == "Revert moved to Done for Article title" },
                      "Button labels: \(buttons.map { attribute($0, "accessibilityLabel") as? String ?? "<nil>" })")
        XCTAssertTrue(buttons.contains { attribute($0, "accessibilityLabel") as? String == "Cancel notification for Article title" })
    }

    private func attribute(_ value: NSObject, _ name: String) -> Any? {
        let selector = NSSelectorFromString(name)
        guard value.responds(to: selector) else { return nil }
        return value.perform(selector)?.takeUnretainedValue()
    }

    private func accessibilityButtons(_ root: Any) -> [NSObject] {
        var visited = Set<ObjectIdentifier>()
        func visit(_ value: Any) -> [NSObject] {
            guard let element = value as? NSObject, visited.insert(ObjectIdentifier(element)).inserted else { return [] }
            let children = ((attribute(element, "accessibilityChildren") as? [Any]) ?? []).flatMap(visit)
            let native = (element as? NSView)?.subviews.flatMap(visit) ?? []
            return (attribute(element, "accessibilityRole") as? String == "AXButton" ? [element] : []) + children + native
        }
        return visit(root)
    }

    func testShortBriefUsesOnlyItsContentHeight() async throws {
        let height = try await briefHeight("Short brief")
        XCTAssertGreaterThan(height, 20)
        XCTAssertLessThan(height, 100)
    }

    func testLongBriefStopsGrowingAtMaximumHeight() async throws {
        let height = try await briefHeight(String(repeating: "An agent's plain text briefing. ", count: 100))
        XCTAssertEqual(height, 180, accuracy: 1)
    }

    private func briefHeight(_ text: String) async throws -> CGFloat {
        let brief = try XCTUnwrap(BriefItem(.object([
            "id": .integer(1), "principal_id": .integer(2), "value": .string(text)
        ])))
        var height: CGFloat = 0
        let view = NSHostingView(rootView: VStack(spacing: 0) {
            BriefsView(briefs: [brief], names: [2: "Scout"])
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
            Color.clear
        })
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 320, height: 300),
                              styleMask: [.titled], backing: .buffered, defer: false)
        window.contentView = view
        window.orderFront(nil)
        defer { window.orderOut(nil) }
        view.layoutSubtreeIfNeeded()
        try await Task.sleep(for: .milliseconds(100))
        return height
    }

}

@MainActor private final class PageLoaded: NSObject, WKNavigationDelegate {
    private let finished: () -> Void
    init(_ finished: @escaping () -> Void) { self.finished = finished }
    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) { finished() }
}

extension ReaderTests {
    func testImagesLoadOnlyFromAuthorizedContentOrigin() async throws {
        let allowed = expectation(description: "Image on content origin loads")
        let blocked = expectation(description: "Other origin stays blocked")
        blocked.isInverted = true
        let content = try ImageServer { allowed.fulfill() }
        let publisher = try ImageServer { blocked.fulfill() }
        try await content.start()
        try await publisher.start()
        defer { content.stop(); publisher.stop() }
        let scheme = ArticleScheme()
        scheme.imageOrigin = content.origin
        scheme.html = "<img src='\(content.origin)/allowed' width='100' height='100'><img src='\(publisher.origin)/blocked'>"
        let (web, window) = makeWebView(scheme)
        defer { window.orderOut(nil) }
        let loaded = expectation(description: "Images page loaded")
        let delegate = PageLoaded { loaded.fulfill() }
        web.navigationDelegate = delegate
        web.load(URLRequest(url: URL(string: "earnest-content://article/")!))
        // WebKit startup can outlast the image deadline, so wait for success first, then poll the inverted expectation alone.
        await fulfillment(of: [loaded, allowed], timeout: 30)
        await fulfillment(of: [blocked], timeout: 1)
        let snapshot = try await web.takeSnapshot(configuration: nil)
        let bitmap = try XCTUnwrap(NSBitmapImageRep(data: XCTUnwrap(snapshot.tiffRepresentation)))
        let color = try XCTUnwrap(bitmap.colorAt(x: 30, y: 60)?.usingColorSpace(.deviceRGB))
        XCTAssertGreaterThan(color.blueComponent, 0.8)
        XCTAssertLessThan(color.redComponent, 0.2)
    }

    func testImagesStayBlockedWithoutAnAuthorizedOrigin() async throws {
        let blocked = expectation(description: "Fallback and click mode cannot fetch images")
        blocked.isInverted = true
        let publisher = try ImageServer { blocked.fulfill() }
        try await publisher.start()
        defer { publisher.stop() }
        let scheme = ArticleScheme()
        scheme.html = "<img src='\(publisher.origin)/blocked'>"
        let (web, window) = makeWebView(scheme)
        defer { window.orderOut(nil) }
        let loaded = expectation(description: "Blocked images page loaded")
        let delegate = PageLoaded { loaded.fulfill() }
        web.navigationDelegate = delegate
        web.load(URLRequest(url: URL(string: "earnest-content://article/")!))
        // WebKit startup can outlast the page-load deadline, so wait for load success first, then poll the inverted expectation alone.
        await fulfillment(of: [loaded], timeout: 30)
        await fulfillment(of: [blocked], timeout: 1)
    }

    private func makeWebView(_ scheme: ArticleScheme) -> (WKWebView, NSWindow) {
        let configuration = WKWebViewConfiguration()
        configuration.defaultWebpagePreferences.allowsContentJavaScript = false
        configuration.websiteDataStore = .nonPersistent()
        configuration.setURLSchemeHandler(scheme, forURLScheme: "earnest-content")
        let web = WKWebView(frame: NSRect(x: 0, y: 0, width: 600, height: 400), configuration: configuration)
        let window = NSWindow(contentRect: web.frame, styleMask: [.titled], backing: .buffered, defer: false)
        window.appearance = NSAppearance(named: .aqua)
        window.contentView = web
        window.orderFront(nil)
        return (web, window)
    }
}

private final class ImageServer: @unchecked Sendable {
    private let listener: NWListener
    private let queue = DispatchQueue(label: "EarnestReaderImageTest")

    init(onRequest: @escaping @Sendable () -> Void) throws {
        listener = try NWListener(using: .tcp, on: .any)
        listener.newConnectionHandler = { connection in
            connection.start(queue: DispatchQueue.global())
            connection.receive(minimumIncompleteLength: 1, maximumLength: 65536) { data, _, _, _ in
                guard let data else { connection.cancel(); return }
                let request = String(decoding: data, as: UTF8.self).lowercased()
                XCTAssertFalse(request.contains("authorization:"))
                XCTAssertFalse(request.contains("cookie:"))
                onRequest()
                let image = ##"<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="#0033ff"/></svg>"##
                let response = "HTTP/1.1 200 OK\r\nContent-Type: image/svg+xml\r\nContent-Length: \(image.utf8.count)\r\nConnection: close\r\n\r\n\(image)"
                connection.send(content: Data(response.utf8), completion: .contentProcessed { _ in connection.cancel() })
            }
        }
    }

    var origin: URL { URL(string: "http://localhost:\(listener.port!.rawValue)")! }

    func start() async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            listener.stateUpdateHandler = { state in
                switch state {
                case .ready: continuation.resume()
                case .failed(let error): continuation.resume(throwing: error)
                default: break
                }
            }
            listener.start(queue: queue)
        }
    }

    func stop() { listener.cancel() }
}

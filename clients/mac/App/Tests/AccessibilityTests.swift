import AppKit
import ApplicationServices
import SwiftUI
import XCTest
import EarnestKit
@testable import Earnest

@MainActor final class AccessibilityTests: XCTestCase {
    /// Assistive apps read the sign-in code spelled out. (The crash this
    /// screen once had came only from another process reading it, which
    /// this in-process check does not reproduce.)
    func testSignInCodeIsReadable() async throws {
        let model = AppModel(api: nil, engine: SyncEngine(store: MemorySyncStore()), principalID: 7)
        model.code = try JSONDecoder().decode(DeviceAuthorization.self, from: Data(#"""
            {"device_code":"d","user_code":"WDJB-MJHT","verification_uri":"https://earnest.test/device","interval":5,"expires_in":600}
            """#.utf8))
        let view = NSHostingView(rootView: LoginView().environmentObject(model))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 520, height: 480),
                              styleMask: [.titled], backing: .buffered, defer: false)
        window.contentView = view
        window.orderFront(nil)
        defer { window.orderOut(nil) }
        view.layoutSubtreeIfNeeded()
        try await Task.sleep(for: .milliseconds(100))

        // Read the window the way assistive apps do: through the AX API, off
        // the main thread, so the main thread can answer.
        let pid = ProcessInfo.processInfo.processIdentifier
        let labels = await Task.detached { Self.descriptions(of: AXUIElementCreateApplication(pid)) }.value
        XCTAssertTrue(labels.contains("Code W D J B - M J H T"), "Labels: \(labels)")
    }

    /// Every text attribute of every element, read in one request per
    /// element as inspectors and screen readers do.
    nonisolated private static func descriptions(of root: AXUIElement) -> [String] {
        var found: [String] = []
        var queue = [root]
        var visited = 0
        while !queue.isEmpty, visited < 2000 {
            let element = queue.removeFirst()
            visited += 1
            var names: CFArray?
            guard AXUIElementCopyAttributeNames(element, &names) == .success, let names else { continue }
            var values: CFArray?
            guard AXUIElementCopyMultipleAttributeValues(element, names, [], &values) == .success,
                  let values = values as? [AnyObject] else { continue }
            for (name, value) in zip(names as? [String] ?? [], values) {
                if let text = value as? String, !text.isEmpty { found.append(text) }
                if name == kAXWindowsAttribute || name == kAXChildrenAttribute,
                   let list = value as? [AXUIElement] {
                    queue.append(contentsOf: list)
                }
            }
        }
        return found
    }
}

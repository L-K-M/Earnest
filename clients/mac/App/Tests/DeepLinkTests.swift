import Foundation
import AppKit
import Carbon
import XCTest
@testable import Earnest

final class DeepLinkTests: XCTestCase {
    func testOnlyInternalIDs() {
        XCTAssertEqual(DeepLink.entryID(URL(string: "earnest://entry/123")!), 123)
        for link in ["earnest://entry/0", "earnest://entry/-1", "earnest://entry/1/2", "earnest://entry/1?url=https://example.com", "earnest://example.com/1", "https://entry/1"] {
            XCTAssertNil(DeepLink.entryID(URL(string: link)!))
        }
    }
}

@MainActor final class DeepLinkWindowTests: XCTestCase {
    func testExternalURLsReuseWindowsAndNewWindowIsNotOffered() async throws {
        let initial = readerWindows.count
        XCTAssertGreaterThan(initial, 0)
        try await sendEntryURL()
        try await sendEntryURL()
        XCTAssertEqual(readerWindows.count, initial, "External URLs must reuse a reader window")

        // One window by design: the AppModel owns the sync engine, cache and
        // reading selection as a singleton, so a second window mirrored the
        // first instead of reading side by side.
        let menus = NSApp.mainMenu?.items.compactMap(\.submenu) ?? []
        let hasNewWindow = menus.contains { $0.indexOfItem(withTitle: "New Window") >= 0 }
        XCTAssertFalse(hasNewWindow, "New Window must not be offered: its window shared the first window's reading state")

        for window in readerWindows { window.close() }
        try await sendEntryURL()
        XCTAssertEqual(readerWindows.count, 1, "An external URL must create a window when none exists")
    }

    private var readerWindows: [NSWindow] {
        NSApp.windows.filter { $0.isVisible && $0.canBecomeMain && !($0 is NSPanel) }
    }

    private func sendEntryURL() async throws {
        let pid = ProcessInfo.processInfo.processIdentifier
        let event = NSAppleEventDescriptor(eventClass: AEEventClass(kInternetEventClass), eventID: AEEventID(kAEGetURL),
                                          targetDescriptor: NSAppleEventDescriptor(processIdentifier: pid),
                                          returnID: AEReturnID(kAutoGenerateReturnID), transactionID: AETransactionID(kAnyTransactionID))
        event.setParam(NSAppleEventDescriptor(string: "earnest://entry/3"), forKeyword: AEKeyword(keyDirectObject))
        _ = try event.sendEvent(options: .noReply, timeout: 5)
        try await Task.sleep(for: .milliseconds(100))
    }
}

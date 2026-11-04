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
    func testExternalURLsReuseWindowsAndNewWindowRemainsAvailable() async throws {
        let initial = readerWindows.count
        XCTAssertGreaterThan(initial, 0)
        try await sendEntryURL()
        try await sendEntryURL()
        XCTAssertEqual(readerWindows.count, initial, "External URLs must reuse a reader window")

        let fileMenu = try XCTUnwrap(NSApp.mainMenu?.items.first(where: { $0.title == "File" })?.submenu)
        let index = fileMenu.indexOfItem(withTitle: "New Window")
        XCTAssertGreaterThanOrEqual(index, 0)
        guard index >= 0 else { return }
        fileMenu.performActionForItem(at: index)
        try await Task.sleep(for: .milliseconds(100))
        XCTAssertEqual(readerWindows.count, initial + 1, "File > New Window must keep multi-window support")
        try await sendEntryURL()
        XCTAssertEqual(readerWindows.count, initial + 1)

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

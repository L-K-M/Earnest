import AppKit
import Foundation
import XCTest
@testable import Earnest

final class BrandTests: XCTestCase {
    func testBrandIconIsInAppBundle() {
        XCTAssertNotNil(NSImage(named: "BrandIcon"))
    }

    func testWordmarkIsInAppBundleAndTemplated() {
        let wordmark = NSImage(named: "Wordmark")
        XCTAssertNotNil(wordmark)
        XCTAssertTrue(wordmark?.isTemplate ?? false)
    }

    func testBrandColorsAreInAppBundle() {
        XCTAssertNotNil(NSColor(named: "BrandInk"))
        XCTAssertNotNil(NSColor(named: "AccentColor"))
    }

    func testBundleIconNameIsAppIcon() {
        let info = Bundle.main.infoDictionary
        let name = info?["CFBundleIconName"] as? String ?? info?["CFBundleIconFile"] as? String
        XCTAssertEqual(name, "AppIcon")
    }
}

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

    func testToneColorSetsMatchSpecValues() {
        let spec: [(tone: Tone, light: (red: Int, green: Int, blue: Int), dark: (red: Int, green: Int, blue: Int))] = [
            (.orange, (0xDD, 0x56, 0x02), (0xFE, 0x71, 0x2E)),
            (.amber, (0xAE, 0x77, 0x05), (0xFF, 0xB4, 0x28)),
            (.plum, (0x8E, 0x3B, 0x68), (0xDA, 0x7F, 0xAD)),
            (.purple, (0x91, 0x6E, 0x9D), (0xB3, 0x8F, 0xC0)),
            (.blue, (0x3E, 0x8A, 0xAB), (0x5A, 0xA5, 0xC7)),
            (.green, (0x42, 0x8E, 0x87), (0x5E, 0xA9, 0xA2))
        ]
        for entry in spec {
            guard let color = NSColor(named: entry.tone.rawValue) else {
                XCTFail("Missing colour set \(entry.tone.rawValue)")
                continue
            }
            assertRGB(color, is: entry.light, in: .aqua)
            assertRGB(color, is: entry.dark, in: .darkAqua)
        }
    }

    /// Compares a colour set's resolved sRGB components against the spec,
    /// rounded to the 0-255 values the asset catalogue stores.
    private func assertRGB(_ color: NSColor, is expected: (red: Int, green: Int, blue: Int), in appearance: NSAppearance.Name) {
        NSAppearance(named: appearance)!.performAsCurrentDrawingAppearance {
            guard let srgb = color.usingColorSpace(.sRGB) else {
                XCTFail("\(color) does not convert to sRGB")
                return
            }
            XCTAssertEqual(Int(round(srgb.redComponent * 255)), expected.red, appearance.rawValue)
            XCTAssertEqual(Int(round(srgb.greenComponent * 255)), expected.green, appearance.rawValue)
            XCTAssertEqual(Int(round(srgb.blueComponent * 255)), expected.blue, appearance.rawValue)
        }
    }

    func testSectionTones() {
        XCTAssertEqual(Section.frontPage.tone, .orange)
        XCTAssertEqual(Section.kept.tone, .plum)
        XCTAssertEqual(Section.done.tone, .green)
        XCTAssertNil(Section.search.tone)
    }
}

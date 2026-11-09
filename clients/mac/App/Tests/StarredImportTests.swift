import Foundation
import XCTest
import EarnestKit
@testable import Earnest

/// The kept-import report wording mirrors the web client, including the
/// retry offer when the server skipped feeds.
@MainActor final class StarredImportTests: XCTestCase {
    func testKeptImportReportStatesOutcomesPlainly() {
        let document = Data(#"{"items":[]}"#.utf8)
        let summary = KeptImportSummary(added: 1, existing: 1, unkept: 2, gone: 1,
                                        skipped: [(feedURL: "https://b.example/rss", count: 2),
                                                  (feedURL: "https://a.example/feed", count: 1)],
                                        failures: ["Broken: no link"], subscribed: 1)
        let report = StarredImportCoordinator.report(for: summary, name: "starred.json", data: document)
        XCTAssertEqual(report.title, "Imported “starred.json” with problems")
        XCTAssertTrue(report.detail.hasPrefix("2 articles in Kept: 1 added from the export, 1 already in Earnest."))
        XCTAssertTrue(report.detail.contains("Subscribed to 1 feed. Their first fetches run in the background."))
        XCTAssertTrue(report.detail.contains("2 left out because you removed them from Kept."))
        XCTAssertTrue(report.detail.contains("1 no longer available in Earnest."))
        XCTAssertTrue(report.detail.contains("3 skipped because you don't follow their feed:"))
        XCTAssertTrue(report.detail.contains("https://b.example/rss (2)"))
        XCTAssertTrue(report.detail.contains("https://a.example/feed (1)"))
        XCTAssertTrue(report.detail.contains("1 could not be imported: Broken: no link"))
        XCTAssertEqual(report.retry?.name, "starred.json")
        XCTAssertEqual(report.retry?.data, document)
    }

    func testKeptImportReportWithoutSkippedFeedsHasNoRetry() {
        let summary = KeptImportSummary(added: 0, existing: 0, unkept: 0, gone: 0)
        let report = StarredImportCoordinator.report(for: summary, name: "empty.json", data: Data())
        XCTAssertEqual(report.title, "Imported “empty.json”")
        XCTAssertEqual(report.detail, "0 articles in Kept: 0 added from the export, 0 already in Earnest.")
        XCTAssertNil(report.retry)
    }

    func testKeptImportLimitMatchesTheServer() {
        XCTAssertEqual(AppModel.keptLimit, 64 << 20)
    }
}

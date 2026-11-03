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

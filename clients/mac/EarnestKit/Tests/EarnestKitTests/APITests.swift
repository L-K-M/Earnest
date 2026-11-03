import Foundation
import Testing
@testable import EarnestKit

@Test func deviceAuthorizationContract() throws {
    let data = Data(#"{"device_code":"secret","user_code":"ABCD","verification_uri":"https://example.test/device","interval":5,"expires_in":600}"#.utf8)
    let result = try JSONDecoder().decode(DeviceAuthorization.self, from: data)
    #expect(result.deviceCode == "secret")
    #expect(result.userCode == "ABCD")
    #expect(result.verificationURI == "https://example.test/device")
    #expect(result.interval == 5)
    #expect(result.expiresIn == 600)
}

@Test func entriesAndContentContracts() throws {
    let page = Data(#"{"entries":[{"id":9223372036854775807,"subscription_id":2,"feed_id":3,"seq":4,"first_seen_at":"2026-01-01T00:00:00Z","title":"Title","url":"https://example.test/","author":"Author","rev":5,"deleted":false,"state":{"principal":1,"entry_id":9223372036854775807,"seen":{"value":"unseen","rev":0,"occurred_at":"2026-01-01T00:00:00Z","actor":1,"source":"human"},"shelf":{"value":"none","rev":0,"occurred_at":"2026-01-01T00:00:00Z","actor":1,"source":"human"},"kept":{"value":false,"rev":0,"occurred_at":"2026-01-01T00:00:00Z","actor":1,"source":"human"},"state_rev":0}}]}"#.utf8)
    let result = try JSONDecoder().decode(EntriesPage.self, from: page)
    #expect(result.entries?.first?.id == Int64.max)
    #expect(result.entries?.first?.state.kept.value == .bool(false))
    let content = Data(#"{"entry_id":1,"html":"<p>hello</p>","text":"hello","sanitizer_version":1,"source":"feed"}"#.utf8)
    #expect(try JSONDecoder().decode(ArticleContent.self, from: content).html == "<p>hello</p>")
}

@Test func changePayloadPreservesInt64() throws {
    let data = Data(#"{"changes":[{"rev":3,"type":"entry","payload":{"id":9223372036854775807}}],"next":{"generation":"g","rev":3},"next_cursor":"g:3"}"#.utf8)
    let page = try JSONDecoder().decode(APIChangesPage.self, from: data)
    #expect(page.changes?.first?.payload == .object(["id": .integer(Int64.max)]))
    #expect(page.nextCursor == "g:3")
}

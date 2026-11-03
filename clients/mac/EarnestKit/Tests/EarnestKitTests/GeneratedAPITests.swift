import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import Testing
@testable import EarnestKit

@Test func generatedWireTypesPreserveIntegerIdentity() throws {
    let source = Data(#"{"rev":1,"type":"entry.created","payload":{"entry":{"id":9223372036854775807}}}"#.utf8)
    let generated = try JSONDecoder().decode(Components.Schemas.ChangeRecord.self, from: source)
    let result: APIChange = try adapt(generated)
    #expect(result.payload == .object(["entry": .object(["id": .integer(Int64.max)])]))
}

@Test func eventHintsParseChunkedFrames() throws {
    var parser = EventHintParser()
    #expect(try parser.append(Data(": heartbeat\r\nevent: changes\r\ndata: {\"generation\":\"g1\",\"rev\":".utf8)).isEmpty)
    let hints = try parser.append(Data("9}\r\n\r\nevent: other\ndata: ignored\n\n".utf8))
    #expect(hints.count == 1)
    #expect(hints.first?.generation == "g1")
    #expect(hints.first?.rev == 9)
    #expect(throws: (any Error).self) { try parser.append(Data(repeating: 65, count: 65537)) }
}

@Test func signedURLUsesNativeExpiryShape() throws {
    let payload = Data(#"{"url":"https://content.example/article/1?signature=x","expires_at":"2099-01-01T00:00:00.123Z"}"#.utf8)
    let signed = try JSONDecoder().decode(SignedContentURL.self, from: payload)
    #expect(signed.url.host == "content.example")
    #expect(parseAPIDate(signed.expiresAt) != nil)
}

@Test func generatedRequestAdapterAcceptsFractionalClocks() throws {
    let wire: APIValue = .object([
        "op_id": .string("offline"), "principal": .integer(1), "entry_id": .integer(1),
        "register": .string("seen"), "value": .string("seen"),
        "base_rev": .object(["generation": .string("g"), "rev": .integer(0)]),
        "occurred_at": .string("2026-11-03T00:00:00.123Z"), "kind": .string("explicit"),
    ])
    let generated: Components.Schemas.StateWrite = try adapt(wire)
    #expect(generated.occurredAt == parseAPIDate("2026-11-03T00:00:00.123Z"))
}

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

@Test func eventHintsDrainLargeChunksBeforeCheckingRemainder() throws {
    let frame = "event: changes\ndata: {\"generation\":\"g1\",\"rev\":9}\n\n"
    let frames = String(repeating: frame, count: 1500)
    #expect(frames.utf8.count > 65536)
    var parser = EventHintParser()
    let hints = try parser.append(Data((frames + "event: chan").utf8))
    #expect(hints.count == 1500)
    #expect(hints.allSatisfy { $0.generation == "g1" && $0.rev == 9 })
    #expect(try parser.append(Data("ges\ndata: {\"generation\":\"g1\",\"rev\":10}\n\n".utf8)).first?.rev == 10)
}

@Test func eventHintsBoundLinesAndAccumulatedPayload() throws {
    var partial = EventHintParser()
    #expect(try partial.append(Data(repeating: 65, count: 65536)).isEmpty)
    #expect(throws: APIError.self) { try partial.append(Data([65])) }

    var complete = EventHintParser()
    #expect(throws: APIError.self) {
        try complete.append(Data((":" + String(repeating: "a", count: 65536) + "\n").utf8))
    }

    var payload = EventHintParser()
    #expect(try payload.append(Data(("data: " + String(repeating: "a", count: 32768) + "\n").utf8)).isEmpty)
    #expect(try payload.append(Data(("data: " + String(repeating: "b", count: 32768) + "\n").utf8)).isEmpty)
    #expect(throws: APIError.self) { try payload.append(Data("data: c\n".utf8)) }
}

@Test func signedURLUsesNativeExpiryShape() throws {
    let payload = Data(#"{"url":"https://content.example/article/1?signature=x","expires_at":"2099-01-01T00:00:00.123Z"}"#.utf8)
    let decoder = JSONDecoder()
    decoder.dateDecodingStrategy = .custom { decoder in
        let value = try decoder.singleValueContainer().decode(String.self)
        return try RFC3339DateTranscoder().decode(value)
    }
    let signed = try decoder.decode(Components.Schemas.ContentURLResult.self, from: payload)
    #expect(URL(string: signed.url)?.host == "content.example")
    #expect(signed.expiresAt > Date())
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

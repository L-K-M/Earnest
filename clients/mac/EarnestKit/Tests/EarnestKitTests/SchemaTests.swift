import Foundation
import Testing
@testable import EarnestKit

private let openAPI = URL(fileURLWithPath: #filePath)
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    .appendingPathComponent("api/openapi.json")

// Generate a populated example directly from the committed schema. Round-tripping
// it catches ignored or renamed fields as well as incompatible Swift types.
private func example(_ schema: [String: Any], schemas: [String: Any], alternative: Int) throws -> Any {
    if let ref = schema["$ref"] as? String {
        return try example(#require(schemas[String(ref.split(separator: "/").last!)] as? [String: Any]), schemas: schemas, alternative: alternative)
    }
    if let choices = schema["oneOf"] as? [[String: Any]] {
        return try example(choices[min(alternative, choices.count - 1)], schemas: schemas, alternative: alternative)
    }
    if let properties = schema["properties"] as? [String: [String: Any]] {
        return try properties.mapValues { try example($0, schemas: schemas, alternative: alternative) }
    }
    let type = (schema["type"] as? String) ?? (schema["type"] as? [String])?.first { $0 != "null" }
    switch type {
    case "string": return (schema["enum"] as? [String])?.first ?? "2026-01-01T00:00:00Z"
    case "integer": return 123
    case "number": return 1.25
    case "boolean": return true
    case "array": return [try example(schema["items"] as? [String: Any] ?? [:], schemas: schemas, alternative: alternative)]
    case nil: return "untyped"
    default: throw SchemaError.unsupported
    }
}
private enum SchemaError: Error { case unsupported }

private func check<T: Codable>(_ type: T.Type, name: String, schemas: [String: Any]) throws {
    let schema = try #require(schemas[name] as? [String: Any])
    for alternative in 0...1 {
        let input = try example(schema, schemas: schemas, alternative: alternative)
        let data = try JSONSerialization.data(withJSONObject: input, options: [.sortedKeys, .fragmentsAllowed])
        let model = try JSONDecoder().decode(type, from: data)
        let output = try JSONSerialization.jsonObject(with: JSONEncoder().encode(model), options: [.fragmentsAllowed])
        #expect((input as? NSDictionary) == (output as? NSDictionary), "\(name) does not preserve schema fields or values")
    }
}

@Test func modelsMatchOpenAPISchemas() throws {
    let document = try #require(JSONSerialization.jsonObject(with: Data(contentsOf: openAPI)) as? [String: Any])
    let components = try #require(document["components"] as? [String: Any])
    let schemas = try #require(components["schemas"] as? [String: Any])
    try check(DeviceAuthorization.self, name: "DeviceResult", schemas: schemas)
    try check(DeviceToken.self, name: "DeviceToken", schemas: schemas)
    try check(APIRegister.self, name: "Register", schemas: schemas)
    try check(APIState.self, name: "State", schemas: schemas)
    try check(APIEntry.self, name: "Entry", schemas: schemas)
    try check(EntriesPage.self, name: "EntriesPage", schemas: schemas)
    try check(ArticleContent.self, name: "Content", schemas: schemas)
    try check(APIRevision.self, name: "Revision", schemas: schemas)
    try check(APIBootstrapPage.self, name: "BootstrapPage", schemas: schemas)
    try check(APIChange.self, name: "ChangeRecord", schemas: schemas)
    try check(APIChangesPage.self, name: "ChangesPage", schemas: schemas)
    try check(LabelDefinition.self, name: "Label", schemas: schemas)
    try check(APIProblem.self, name: "Problem", schemas: schemas)
}

@Test func labelMembershipMatchesOpenAPISchema() throws {
    let document = try #require(JSONSerialization.jsonObject(with: Data(contentsOf: openAPI)) as? [String: Any])
    let components = try #require(document["components"] as? [String: Any])
    let schemas = try #require(components["schemas"] as? [String: Any])
    let schema = try #require(schemas["LabelMembership"] as? [String: Any])
    let input = try example(schema, schemas: schemas, alternative: 0)
    let value = try JSONDecoder().decode(APIValue.self, from: JSONSerialization.data(withJSONObject: input))
    let membership = try #require(SyncObject(labelMembership: value))
    #expect(membership.id == value["id"]?.int64Value)
    #expect(membership.entryID == value["entry_id"]?.int64Value)
    #expect(membership.labelID == value["label_id"]?.int64Value)
    #expect(membership.rev == value["rev"]?.int64Value)
    #expect(membership.deleted == value["deleted"]?.boolValue)
}

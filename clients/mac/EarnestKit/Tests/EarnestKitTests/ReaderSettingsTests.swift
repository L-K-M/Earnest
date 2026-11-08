import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import Testing
@testable import EarnestKit

/// These live in the serialized NetworkTests suite because ScriptedHTTP keeps
/// process-global reply queues; a separate suite would race it.
extension NetworkTests {
    private func makeReaderSettingsAPI(_ replies: [ScriptedHTTP.Reply]) throws -> EarnestAPI {
        let session = ScriptedHTTP.install(replies)
        return try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
    }

    @Test func readerDefaultsMatchServerDefaults() {
        let settings = APIReaderSettings()
        #expect(settings.readingMode == .river)
        #expect(settings.todayDecayDays == 2)
        #expect(settings.singleKeyShortcuts)
        #expect(settings.theme == .system)
        #expect(settings.measure == 66)
        #expect(settings.font == .serif)
        #expect(settings.feedOverrides.isEmpty)
        #expect(settings.rev == 0)
    }

    @Test func readerSettingsDecodesServerDefaultsAndLargeRev() async throws {
        let api = try makeReaderSettingsAPI([.init(path: "/v1/settings", body: #"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{},"rev":9223372036854775807}"#)])
        let settings = try await api.readerSettings()
        #expect(settings.readingMode == .river)
        #expect(settings.todayDecayDays == 2)
        #expect(settings.singleKeyShortcuts)
        #expect(settings.theme == .system)
        #expect(settings.measure == 66)
        #expect(settings.font == .serif)
        #expect(settings.feedOverrides.isEmpty)
        #expect(settings.rev == Int64.max)
        ScriptedHTTP.assertFinished()
    }

    @Test func updateSendsEveryReaderFieldAndNeverRev() async throws {
        let api = try makeReaderSettingsAPI([
            .init(method: "PUT", path: "/v1/settings", body: #"{"reading_mode":"unread_counts","today_decay_days":3,"single_key_shortcuts":false,"theme":"dark","measure":68,"font":"sans","feed_overrides":{},"rev":7}"#, checkBody: { data in
                let value = try JSONDecoder().decode(APIValue.self, from: #require(data))
                guard case .object(let fields) = value else { Issue.record("body is not an object"); return }
                #expect(fields.count == 8)
                #expect(fields["rev"] == nil)
                #expect(fields["op_id"] == .string("op-1"))
                #expect(fields["reading_mode"] == .string("unread_counts"))
                #expect(fields["today_decay_days"] == .integer(3))
                #expect(fields["single_key_shortcuts"] == .bool(false))
                #expect(fields["theme"] == .string("dark"))
                #expect(fields["measure"] == .integer(68))
                #expect(fields["font"] == .string("sans"))
                #expect(fields["feed_overrides"] == .object([:]))
            }),
        ])
        let settings = APIReaderSettings(
            readingMode: .unreadCounts, todayDecayDays: 3, singleKeyShortcuts: false,
            theme: .dark, measure: 68, font: .sans, feedOverrides: [:], rev: 9223372036854775807
        )
        let updated = try await api.updateReaderSettings(settings, opID: "op-1")
        #expect(updated.rev == 7)
        #expect(updated.readingMode == .unreadCounts)
        ScriptedHTTP.assertFinished()
    }

    @Test func suppliedOpIDIsPreserved() async throws {
        let api = try makeReaderSettingsAPI([
            .init(method: "PUT", path: "/v1/settings", body: #"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{},"rev":1}"#),
            .init(method: "PUT", path: "/v1/settings", body: #"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{},"rev":1}"#, checkBody: { data in
                let value = try JSONDecoder().decode(APIValue.self, from: #require(data))
                guard case .object(let fields) = value else { Issue.record("body is not an object"); return }
                #expect(fields["op_id"] == .string("offline-op"))
            }),
        ])
        _ = try await api.updateReaderSettings(APIReaderSettings(), opID: "offline-op")
        _ = try await api.updateReaderSettings(APIReaderSettings(), opID: "offline-op")
        ScriptedHTTP.assertFinished()
    }

    @Test func partialOverridesInheritMissingMembers() throws {
        let data = Data(#"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{"42":{"theme":"eink"}},"rev":4}"#.utf8)
        let settings = try JSONDecoder().decode(APIReaderSettings.self, from: data)
        let override = try #require(settings.feedOverrides["42"])
        #expect(override.theme == .eink)
        #expect(override.measure == nil)
        #expect(override.font == nil)

        let encoded = try JSONEncoder().encode(override)
        let fields = try JSONDecoder().decode([String: APIValue].self, from: encoded)
        #expect(Set(fields.keys) == ["theme"])
    }

    @Test func emptyOverrideMapClearsOverrides() async throws {
        let api = try makeReaderSettingsAPI([
            .init(method: "PUT", path: "/v1/settings", body: #"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{},"rev":4}"#, checkBody: { data in
                let value = try JSONDecoder().decode(APIValue.self, from: #require(data))
                guard case .object(let fields) = value else { Issue.record("body is not an object"); return }
                #expect(fields["feed_overrides"] == .object([:]))
            }),
        ])
        _ = try await api.updateReaderSettings(APIReaderSettings(feedOverrides: [:], rev: 3), opID: "op-1")
        ScriptedHTTP.assertFinished()
    }

    @Test func enumWireNamesMatchTheServer() {
        #expect(APIReadingMode.river.rawValue == "river")
        #expect(APIReadingMode.unreadCounts.rawValue == "unread_counts")
        #expect(APIReaderTheme.system.rawValue == "system")
        #expect(APIReaderTheme.light.rawValue == "light")
        #expect(APIReaderTheme.dark.rawValue == "dark")
        #expect(APIReaderTheme.black.rawValue == "black")
        #expect(APIReaderTheme.eink.rawValue == "eink")
        #expect(APIReaderFont.serif.rawValue == "serif")
        #expect(APIReaderFont.sans.rawValue == "sans")
        #expect(APIReaderFont.dyslexic.rawValue == "dyslexic")
        #expect(APIReadingMode(rawValue: "unread_counts") == .unreadCounts)
        #expect(throws: DecodingError.self) {
            _ = try JSONDecoder().decode(APIReaderSettings.self, from: Data(#"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"sepia","measure":66,"font":"serif","feed_overrides":{},"rev":0}"#.utf8))
        }
    }

    @Test func updatePropagatesUnauthorizedProblem() async throws {
        let api = try makeReaderSettingsAPI([.init(method: "PUT", path: "/v1/settings", status: 401, body: #"{"type":"about:blank","title":"Unauthorized","status":401,"error":"unauthorized"}"#)])
        await #expect(throws: APIProblem.self) { try await api.updateReaderSettings(APIReaderSettings(), opID: "op-1") }
        ScriptedHTTP.assertFinished()
    }

    @Test func outOfRangeSettingsFailBeforeAnyRequest() async throws {
        let api = try makeReaderSettingsAPI([])
        await #expect(throws: APIReaderSettingsError.self) { try await api.updateReaderSettings(APIReaderSettings(measure: 73), opID: "op-1") }
        await #expect(throws: APIReaderSettingsError.self) { try await api.updateReaderSettings(APIReaderSettings(measure: 61), opID: "op-1") }
        await #expect(throws: APIReaderSettingsError.self) { try await api.updateReaderSettings(APIReaderSettings(todayDecayDays: 366), opID: "op-1") }
        await #expect(throws: APIReaderSettingsError.self) { try await api.updateReaderSettings(APIReaderSettings(feedOverrides: ["0": .init()]), opID: "op-1") }
        await #expect(throws: APIReaderSettingsError.self) { try await api.updateReaderSettings(APIReaderSettings(feedOverrides: ["42": .init(measure: 73)]), opID: "op-1") }
        await #expect(throws: APIReaderSettingsError.self) { try await api.updateReaderSettings(APIReaderSettings(), opID: "") }
        ScriptedHTTP.assertFinished()
    }

    @Test func readerValidationErrorsStayActionableThroughErrorExistentials() {
        let failure: any Error = APIReaderSettingsError(field: "measure")
        #expect(failure.localizedDescription == "Invalid reader setting: measure")
    }

    @Test func generatedModelsCoverEveryReaderField() throws {
        let json = Data(#"{"reading_mode":"unread_counts","today_decay_days":3,"single_key_shortcuts":false,"theme":"black","measure":72,"font":"dyslexic","feed_overrides":{"42":{"theme":"eink","measure":64,"font":"sans"}},"rev":5}"#.utf8)
        let generated = try JSONDecoder().decode(Components.Schemas.ReaderSettings.self, from: json)
        #expect(generated.readingMode == "unread_counts")
        #expect(generated.todayDecayDays == 3)
        #expect(generated.singleKeyShortcuts == false)
        #expect(generated.theme == "black")
        #expect(generated.measure == 72)
        #expect(generated.font == "dyslexic")
        #expect(generated.feedOverrides.additionalProperties["42"]?.theme == "eink")
        #expect(generated.feedOverrides.additionalProperties["42"]?.measure == 64)
        #expect(generated.feedOverrides.additionalProperties["42"]?.font == "sans")
        #expect(generated.rev == 5)

        let body = Components.Schemas.SettingsBody(
            feedOverrides: .init(additionalProperties: ["42": .init(font: "sans", measure: 64, theme: "eink")]),
            font: "dyslexic",
            measure: 72,
            opId: "op-1",
            readingMode: "unread_counts",
            singleKeyShortcuts: false,
            theme: "black",
            todayDecayDays: 3
        )
        let encoded = try JSONEncoder().encode(body)
        let fields = try JSONDecoder().decode([String: APIValue].self, from: encoded)
        #expect(Set(fields.keys) == ["reading_mode", "today_decay_days", "single_key_shortcuts", "theme", "measure", "font", "feed_overrides", "op_id"])
        #expect(fields["rev"] == nil)
    }

    @Test func unreadCountsDecodeStringKeysAndInt64Values() async throws {
        let api = try makeReaderSettingsAPI([
            .init(path: "/v1/entries:counts", body: #"{"by_subscription":{"1":2,"42":7},"later":3,"kept":1,"total":10}"#)
        ])
        let counts = try await api.unreadCounts()
        #expect(counts.bySubscription == ["1": 2, "42": 7])
        #expect(counts.later == 3)
        #expect(counts.kept == 1)
        #expect(counts.total == 10)
        ScriptedHTTP.assertFinished()
    }

    @Test func settingsObjectCapturesEveryReaderField() throws {
        let raw: APIValue = .object([
            "reading_mode": .string("unread_counts"),
            "today_decay_days": .integer(9),
            "single_key_shortcuts": .bool(false),
            "theme": .string("eink"),
            "measure": .integer(70),
            "font": .string("dyslexic"),
            "feed_overrides": .object(["42": .object(["theme": .string("black")])]),
            "rev": .integer(12),
        ])
        let object = try #require(SyncObject(settings: raw))
        #expect(object.rev == 12)
        let settings = try #require(object.readerSettings)
        #expect(settings.readingMode == .unreadCounts)
        #expect(settings.todayDecayDays == 9)
        #expect(!settings.singleKeyShortcuts)
        #expect(settings.theme == .eink)
        #expect(settings.measure == 70)
        #expect(settings.font == .dyslexic)
        #expect(settings.feedOverrides["42"]?.theme == .black)
        #expect(settings.rev == 12)
    }

    @Test func mergeSettingsKeepsNewerRevAndPreservesMutesAndCursor() throws {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 5))
        try engine.replaceRiverResources(mutes: [.object([
            "id": .integer(1), "kind": .string("author"), "value": .string("spam"), "rev": .integer(2)
        ])], settings: .object([
            "reading_mode": .string("river"), "today_decay_days": .integer(3),
            "single_key_shortcuts": .bool(true), "theme": .string("light"),
            "measure": .integer(64), "font": .string("sans"), "feed_overrides": .object([:]),
            "rev": .integer(8),
        ]))
        // An older acknowledged write cannot regress the snapshot.
        try engine.mergeSettings(APIReaderSettings(readingMode: .unreadCounts, todayDecayDays: 30, rev: 7))
        #expect(try engine.readerSettings()?.todayDecayDays == 3)
        // A newer one replaces only the settings object.
        try engine.mergeSettings(APIReaderSettings(readingMode: .unreadCounts, todayDecayDays: 30, theme: .black, rev: 9))
        let settings = try #require(try engine.readerSettings())
        #expect(settings.todayDecayDays == 30)
        #expect(settings.theme == .black)
        let snapshot = try engine.snapshot()
        #expect(snapshot.cursor?.rev == 5)
        #expect(snapshot.objects.contains { $0.kind == .mute && $0.id == 1 })
        #expect(snapshot.objects.filter { $0.kind == .settings }.count == 1)
    }

    @Test func unreadCountsPersistAcrossBootstrapForColdLaunch() throws {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        let counts = APIUnreadCounts(bySubscription: ["7": 4], later: 1, kept: 2, total: 5)
        try engine.mergeUnreadCounts(counts)
        // A new generation replaces the cache but keeps the last-synced counts.
        try engine.bootstrap(Cursor(generation: "g2", rev: 0))
        #expect(try engine.snapshot().unreadCounts == counts)
        #expect(try engine.readerSettings() == nil)
    }
}

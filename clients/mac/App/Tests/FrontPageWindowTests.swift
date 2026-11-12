import Foundation
import AppKit
import SwiftUI
import XCTest
import EarnestKit
@testable import Earnest

@MainActor final class FrontPageWindowTests: XCTestCase {
    private func response(_ period: String, _ revision: String) -> String {
        """
        {"period":"\(period)","window_start":"2026-11-11T10:00:00Z","window_end":"2026-11-12T10:00:00Z","selected_at":"2026-11-12T10:00:00Z","revision":"\(revision)","sections":[]}
        """
    }

    func testPeriodSwitchLoadsItsOwnPageWithoutASecondRefresh() async throws {
        let session = ContentHTTP.install([
            .init(path: "/v1/front-page", body: response("daily", "daily-1"), query: "period=daily"),
            .init(path: "/v1/front-page", body: response("weekly", "weekly-1"), query: "period=weekly"),
        ])
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, engine: SyncEngine(store: MemorySyncStore()), principalID: 7)
        model.loggedIn = true
        defer { model.signOut(); session.invalidateAndCancel() }
        let window = WindowModel(app: model)
        await window.refreshFrontPage()
        XCTAssertEqual(window.frontPage?.period, .daily)
        window.frontPagePeriod = .weekly
        await window.refreshFrontPage()
        XCTAssertEqual(window.frontPage?.period, .weekly)
        XCTAssertEqual(window.frontPage?.revision, "weekly-1")
        XCTAssertNil(window.frontPageAvailable)
        XCTAssertEqual(model.pendingOps, 0)
        ContentHTTP.assertFinished()
    }

    func testBackgroundCoverageWaitsForExplicitRefresh() async throws {
        let session = ContentHTTP.install([
            .init(path: "/v1/front-page", body: response("daily", "first")),
            .init(path: "/v1/front-page", body: response("daily", "new")),
        ])
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, engine: SyncEngine(store: MemorySyncStore()), principalID: 7)
        model.loggedIn = true
        defer { model.signOut(); session.invalidateAndCancel() }
        let window = WindowModel(app: model)
        await window.refreshFrontPage()
        await window.refreshFrontPage()
        XCTAssertEqual(window.frontPage?.revision, "first")
        XCTAssertEqual(window.frontPageAvailable?.revision, "new")
        window.applyAvailableFrontPage()
        XCTAssertEqual(window.frontPage?.revision, "new")
        XCTAssertNil(window.frontPageAvailable)
        XCTAssertEqual(model.pendingOps, 0)
        ContentHTTP.assertFinished()
    }

    func testExplicitRefreshUpdatesReaderMetadata() async throws {
        let first = pageJSON(period: "daily", revision: "r1")
        let revised = pageJSON(period: "daily", revision: "r2")
            .replacingOccurrences(of: "Lead story", with: "Updated headline")
            .replacingOccurrences(of: "\"rev\":1", with: "\"rev\":2")
        let (model, window, session) = try makeModel([
            .init(path: "/v1/front-page", body: first),
            .init(path: "/v1/front-page", body: revised),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        await window.refreshFrontPage()
        await window.refreshFrontPage(force: true)
        XCTAssertEqual(window.entries.first(where: { $0.id == 9 })?.title, "Updated headline")
        XCTAssertEqual(model.pendingOps, 0)
        ContentHTTP.assertFinished()
    }

    // MARK: - Fixtures

    func testAccountCacheKeepsBothWindowsAndClearsPrivateMedia() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let identity = try AccountIdentity(server: URL(string: "https://api.test")!, userID: 1, principalID: 7)
        let otherIdentity = try AccountIdentity(server: URL(string: "https://api.test")!, userID: 2, principalID: 8)
        let cache = try Cache(identity: identity, directory: directory)
        let other = try Cache(identity: otherIdentity, directory: directory)
        let daily = try JSONDecoder().decode(FrontPage.self, from: Data(response("daily", "d").utf8))
        let weekly = try JSONDecoder().decode(FrontPage.self, from: Data(response("weekly", "w").utf8))
        try cache.saveFrontPage(daily, period: .daily)
        try cache.saveFrontPage(weekly, period: .weekly)
        XCTAssertNil(try other.frontPage(.daily))
        let reopened = try Cache(identity: identity, directory: directory)
        XCTAssertEqual(try reopened.frontPage(.daily)?.page.revision, "d")
        XCTAssertEqual(try reopened.frontPage(.weekly)?.page.revision, "w")
        let image = "/v1/entries/9/image?v=one"
        try cache.saveMedia(path: image, data: Data([1, 2, 3]))
        try cache.clearFrontPages()
        try cache.saveMedia(path: image, data: Data([4, 5, 6]))
        XCTAssertNil(try reopened.frontPage(.daily))
        XCTAssertNil(try reopened.frontPage(.weekly))
        XCTAssertNil(try reopened.media(path: image), "An old download must not recreate a signed-out account's media")
    }

    private func entryJSON(_ id: Int64, _ title: String, seen: String = "unseen", shelf: String = "none") -> String {
        """
        {"id":\(id),"subscription_id":1,"feed_id":2,"seq":\(id),"title":"\(title)","url":"https://publisher.test/\(id)","author":"Ada","first_seen_at":"2026-11-04T08:00:00Z","rev":1,"deleted":false,"state":{"principal":7,"entry_id":\(id),"state_rev":0,"seen":{"value":"\(seen)","rev":0,"occurred_at":"2026-11-04T08:00:00Z","actor":0,"source":""},"shelf":{"value":"\(shelf)","rev":0,"occurred_at":"2026-11-04T08:00:00Z","actor":0,"source":""},"kept":{"value":false,"rev":0,"occurred_at":"2026-11-04T08:00:00Z","actor":0,"source":""}}}
        """
    }

    private func pageJSON(period: String, revision: String, image: String? = nil, summary: Bool = false) -> String {
        let imageField = image.map { ",\"image_url\":\"\($0)\"" } ?? ",\"image_url\":null"
        let summaryField = summary
            ? #","summary":{"claims":[{"text":"The council approved the plan.","citations":[{"entry_id":9,"content_hash":"a","passages":["The council approved the plan."],"verified":true}],"verified":true}],"model":"test-model","prompt_version":"front-page-v1","generated_at":"2026-11-04T09:05:00Z","excerpt_notice":"Some sources gave excerpts only."}"#
            : ""
        return """
        {"period":"\(period)","window_start":"2026-11-11T10:00:00Z","window_end":"2026-11-12T10:00:00Z","selected_at":"2026-11-12T10:00:00Z","revision":"\(revision)","sections":[
          {"name":"Across your feeds","stories":[
            {"id":"cluster:9","entry":\(entryJSON(9, "Lead story")),"source_title":"First Source","published_at":"2026-11-04T08:30:00Z","feed_count":2,"excerpt":"A publisher excerpt."\(imageField)\(summaryField),"coverage":[{"entry":\(entryJSON(9, "Lead story")),"source_title":"First Source"},{"entry":\(entryJSON(10, "Second original")),"source_title":"Second Source"}]},
            {"id":"entry:11","entry":\(entryJSON(11, "Single source")),"source_title":"Solo Source","published_at":"2026-11-04T07:00:00Z","feed_count":1,"excerpt":"","image_url":null,"coverage":[{"entry":\(entryJSON(11, "Single source")),"source_title":"Solo Source"}]}
          ]},
          {"name":"More from your feeds","stories":[
            {"id":"entry:12","entry":\(entryJSON(12, "Later story")),"source_title":"Another Source","published_at":"2026-11-04T06:00:00Z","feed_count":1,"excerpt":"Another excerpt.","image_url":null,"coverage":[{"entry":\(entryJSON(12, "Later story")),"source_title":"Another Source"}]}
          ]}
        ]}
        """
    }

    private func makeModel(_ replies: [ContentHTTP.Reply]) throws -> (AppModel, WindowModel, URLSession) {
        let session = ContentHTTP.install(replies)
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 0))
        let model = AppModel(api: api, engine: engine, principalID: 7)
        model.loggedIn = true
        return (model, WindowModel(app: model), session)
    }

    // MARK: - Ordering and rendering

    func testStoryOrderFollowsTheServerAndSurvivesRebuild() async throws {
        let (model, window, session) = try makeModel([
            .init(path: "/v1/front-page", body: pageJSON(period: "daily", revision: "r1"), query: "period=daily"),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        await window.refreshFrontPage()
        let stories = window.frontPage?.sections.flatMap(\.stories) ?? []
        XCTAssertEqual(stories.map(\.id), ["cluster:9", "entry:11", "entry:12"])
        XCTAssertEqual(window.visibleEntryIDs(), [9, 11, 12], "The front page lists representatives in server order")
        model.rebuildPresentation()
        XCTAssertEqual(window.frontPage?.sections.flatMap(\.stories).map(\.id), ["cluster:9", "entry:11", "entry:12"],
                       "A rebuild never reorders or re-ranks the paper")
        XCTAssertEqual(window.rows, [], "The front page is not a flat article table")
        ContentHTTP.assertFinished()
    }

    func testRenderingAndCoverageExpansionDoNotMarkSeen() async throws {
        let (model, window, session) = try makeModel([
            .init(path: "/v1/front-page", body: pageJSON(period: "daily", revision: "r1"), query: "period=daily"),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        await window.refreshFrontPage()
        XCTAssertTrue(try model.engine?.snapshot().outbox.isEmpty ?? true, "A front-page fetch never writes a read mark")
        let story = try XCTUnwrap(window.frontPage?.sections.flatMap(\.stories).first)
        XCTAssertEqual(window.visibleCoverage(story).map(\.entry.id), [9, 10])
        XCTAssertEqual(window.representative(story)?.entry.id, 9)
        XCTAssertFalse(window.isStoryRead(story))
        XCTAssertTrue(try model.engine?.snapshot().outbox.isEmpty ?? true,
                      "Rendering and expanding coverage never write a read mark")
        ContentHTTP.assertFinished()
    }

    func testOpeningRepresentativeAndCoverage() async throws {
        let (model, window, session) = try makeModel([
            .init(path: "/v1/front-page", body: pageJSON(period: "daily", revision: "r1"), query: "period=daily"),
            .init(path: "/v1/entries/9/content-url", body: #"{"url":"https://content.test:8081/article/9?signature=abc","expires_at":"2099-01-01T00:00:00Z"}"#, mode: "proxied"),
            .init(path: "/article/9", body: "<p>Nine</p>"),
            .init(path: "/v1/entries/10/content-url", body: #"{"url":"https://content.test:8081/article/10?signature=abc","expires_at":"2099-01-01T00:00:00Z"}"#, mode: "proxied"),
            .init(path: "/article/10", body: "<p>Ten</p>"),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        await window.refreshFrontPage()
        XCTAssertEqual(window.entries.map(\.id), [9, 10, 11, 12], "Coverage entries join the window for the reader and commands")

        window.selectFromList(9)
        try await waitUntil { window.html == "<p>Nine</p>" }
        XCTAssertEqual(window.readingEntry?.id, 9)
        let firstSeen = try model.engine?.snapshot().outbox
        XCTAssertEqual(firstSeen?.map(\.entryID), [9], "Opening the representative marks only it seen")

        window.selectFromList(10)
        try await waitUntil { window.html == "<p>Ten</p>" }
        XCTAssertEqual(window.readingEntry?.id, 10)
        let secondSeen = try model.engine?.snapshot().outbox
        XCTAssertEqual(secondSeen?.map(\.entryID), [9, 10])
        XCTAssertTrue(secondSeen?.allSatisfy { $0.kind == .passive } ?? false)
        ContentHTTP.assertFinished()
    }

    func testReadIndicatorNeedsEveryCoverageArticle() async throws {
        let (model, window, session) = try makeModel([
            .init(path: "/v1/front-page", body: pageJSON(period: "daily", revision: "r1"), query: "period=daily"),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        await window.refreshFrontPage()
        let story = try XCTUnwrap(window.frontPage?.sections.flatMap(\.stories).first)
        XCTAssertFalse(window.isStoryRead(story))
        try markSeen(9, in: model)
        XCTAssertFalse(window.isStoryRead(story), "One read original is not the whole story")
        try markSeen(10, in: model)
        XCTAssertTrue(window.isStoryRead(story), "Seen coverage makes the story read")
        try markDone(10, in: model)
        XCTAssertTrue(window.isStoryRead(story), "A Done original still counts as read")
        ContentHTTP.assertFinished()
    }

    // MARK: - Cache

    func testNavigationReplacesGoneRepresentativesAndDropsEmptyStories() async throws {
        let (model, window, session) = try makeModel([
            .init(path: "/v1/front-page", body: pageJSON(period: "daily", revision: "r1")),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        await window.refreshFrontPage()
        try model.engine?.markGone(entryID: 9)
        XCTAssertEqual(window.visibleEntryIDs(), [10, 11, 12])
        for id: Int64 in [10, 11, 12] { try model.engine?.markGone(entryID: id) }
        XCTAssertTrue(window.visibleEntryIDs().isEmpty)
        XCTAssertTrue(window.visibleFrontPageSections(try XCTUnwrap(window.frontPage)).isEmpty)
        XCTAssertEqual(model.pendingOps, 0)
        ContentHTTP.assertFinished()
    }

    func testRestoredSelectionWaitsForTheFrontPageEntries() async throws {
        let (model, window, session) = try makeModel([
            .init(path: "/v1/front-page", body: pageJSON(period: "daily", revision: "r1")),
            .init(path: "/v1/entries/9/content-url", body: #"{"url":"https://content.test:8081/article/9?signature=abc","expires_at":"2099-01-01T00:00:00Z"}"#, mode: "proxied"),
            .init(path: "/article/9", body: "<p>Nine</p>"),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        window.pendingRestoredSelection = 9
        window.restoreCachedEntries([])
        model.rebuildPresentation()
        XCTAssertEqual(window.pendingRestoredSelection, 9)
        guard window.pendingRestoredSelection != nil else { return }
        await window.refreshFrontPage()
        model.rebuildPresentation()
        try await waitUntil { window.html == "<p>Nine</p>" }
        XCTAssertEqual(window.selected, 9)
        window.restoreCachedEntries([])
        XCTAssertTrue(window.entries.contains { $0.id == 10 }, "A sync restore keeps the displayed coverage available")
        ContentHTTP.assertFinished()
    }

    func testManualNavigationCancelsDelayedRestoration() throws {
        let (model, window, session) = try makeModel([])
        defer { model.signOut(); session.invalidateAndCancel() }
        window.pendingRestoredSelection = 9
        model.rebuildPresentation()
        XCTAssertEqual(window.pendingRestoredSelection, 9)
        window.go(to: .allArticles)
        XCTAssertNil(window.pendingRestoredSelection)
        window.pendingRestoredSelection = 9
        window.frontPagePeriod = .weekly
        XCTAssertNil(window.pendingRestoredSelection)
    }

    func testFrontPageCacheIsolatesAccountsAndSurvivesOffline() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let suite = "Earnest.FrontPageWindowTests.\(UUID())"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }
        let store = IdentityStore(defaults: defaults)
        let identity = try AccountIdentity(server: URL(string: "https://api.test")!, userID: 1, principalID: 7)
        let cache = try Cache(identity: identity, directory: directory)
        let session = ContentHTTP.install([
            .init(path: "/v1/me", body: #"{"user_id":1,"principal_id":7,"kind":"human","scopes":[]}"#),
            .init(path: "/v1/front-page", body: pageJSON(period: "daily", revision: "cached"), query: "period=daily"),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        model.loggedIn = true
        _ = try await model.establishSession()
        let window = WindowModel(app: model)
        await window.refreshFrontPage()
        XCTAssertEqual(window.frontPage?.revision, "cached")
        session.invalidateAndCancel()

        let other = try Cache(identity: AccountIdentity(server: identity.server, userID: 2, principalID: 8), directory: directory)
        XCTAssertNil(try other.frontPage(.daily), "Another account never sees this page")

        // A cold launch shows the cached page before any request.
        let offline = ContentHTTP.install([
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
            .init(path: "/v1/front-page", body: "", failure: .cannotConnectToHost),
        ])
        let offlineAPI = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: offline)
        let relaunched = AppModel(api: offlineAPI, serverURL: identity.server, identities: store, cacheDirectory: directory)
        relaunched.loggedIn = true
        _ = try await relaunched.establishSession()
        let restored = WindowModel(app: relaunched)
        await restored.refreshFrontPage()
        XCTAssertEqual(restored.frontPage?.revision, "cached")
        XCTAssertNotNil(restored.frontPageUpdated, "The stale page carries its updated time")
        offline.invalidateAndCancel()

        relaunched.signOut()
        XCTAssertNil(try cache.frontPage(.daily), "Sign out clears the private front page")
    }

    // MARK: - Legacy destinations

    func testLegacyDestinationsMapToFrontPageWindows() throws {
        XCTAssertEqual(Section.legacy("Today"), .frontPage)
        XCTAssertEqual(Section.legacyPeriod("Today"), .daily)
        XCTAssertEqual(Section.legacy("Edition"), .frontPage)
        XCTAssertEqual(Section.legacyPeriod("Edition"), .weekly)
        XCTAssertEqual(Section.legacy("Later"), .later)
        XCTAssertNil(Section.legacyPeriod("Later"))

        let model = AppModel(engine: SyncEngine(store: MemorySyncStore()), principalID: 7)
        let window = WindowModel(app: model, section: .kept)
        window.openDeepLink(URL(string: "earnest://entry/42")!)
        XCTAssertEqual(window.section, .frontPage)
        XCTAssertEqual(window.frontPagePeriod, .daily, "A deep link opens the daily front page")
        window.deferDeepLink(43)
        XCTAssertEqual(window.section, .frontPage)
        XCTAssertEqual(window.frontPagePeriod, .daily)
    }

    // MARK: - All articles

    func testAllArticlesFetchesNewestFirstWithUnreadFilter() async throws {
        let session = ContentHTTP.install([
            .init(path: "/v1/entries",
                  body: """
                  {"entries":[\(entryJSON(43, "Newest")),\(entryJSON(42, "Older"))]}
                  """,
                  query: "view=all&unread_only=true&direction=backward"),
        ])
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.replaceRiverResources(mutes: [], settings: .object([
            "today_decay_days": .integer(2), "show_only_unread": .bool(true),
        ]))
        let model = AppModel(api: api, engine: engine, principalID: 7)
        try model.restoreReaderPreferences()
        model.loggedIn = true
        defer { model.signOut(); session.invalidateAndCancel() }
        let window = WindowModel(app: model, section: .allArticles)
        await window.refreshEntries()
        model.rebuildPresentation()
        XCTAssertNil(model.error)
        XCTAssertEqual(window.rows.map(\.id), [43, 42], "All articles reads newest first")
        XCTAssertEqual(window.visibleEntryIDs(), [43, 42])
        ContentHTTP.assertFinished()
    }

    // MARK: - Missing pictures

    func testFrontPageViewRendersTextOnlyWithoutAnImage() async throws {
        let (model, window, session) = try makeModel([
            .init(path: "/v1/front-page", body: pageJSON(period: "daily", revision: "r1"), query: "period=daily"),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        await window.refreshFrontPage()
        for width in [420, 960] {
            let paper = FrontPageView().environmentObject(model).environmentObject(window)
                .environment(\.colorScheme, width == 420 ? .dark : .light)
                .environment(\.dynamicTypeSize, width == 420 ? .accessibility1 : .large)
                .frame(width: CGFloat(width), height: 760)
            let view = NSHostingView(rootView: paper)
            let host = NSWindow(contentRect: NSRect(x: 0, y: 0, width: width, height: 760),
                                styleMask: [.titled], backing: .buffered, defer: false)
            host.appearance = NSAppearance(named: width == 420 ? .darkAqua : .aqua)
            host.contentView = view
            host.orderFront(nil)
            view.layoutSubtreeIfNeeded()
            try await Task.sleep(for: .milliseconds(200))
            host.orderOut(nil)
        }
        XCTAssertEqual(model.pendingOps, 0, "Rendering the newspaper leaves reading state unchanged")
        ContentHTTP.assertFinished()
    }

    // MARK: - Helpers

    private func markSeen(_ id: Int64, in model: AppModel) throws {
        try model.engine?.queue(Write(opID: "seen-\(id)", principal: 7, entryID: id, register: .seen,
                                      value: .text("seen"), base: Cursor(generation: "g", rev: 0),
                                      occurredAt: Date(), kind: .passive))
        model.rebuildPresentation()
    }

    private func markDone(_ id: Int64, in model: AppModel) throws {
        try model.engine?.queue(Write(opID: "done-\(id)", principal: 7, entryID: id, register: .shelf,
                                      value: .text("done"), base: Cursor(generation: "g", rev: 0),
                                      occurredAt: Date()))
        model.rebuildPresentation()
    }

    private func waitUntil(_ condition: @escaping () -> Bool) async throws {
        let deadline = Date().addingTimeInterval(5)
        while !condition() && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertTrue(condition(), "Condition not met in time")
    }
}

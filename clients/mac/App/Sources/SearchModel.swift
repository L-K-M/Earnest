import Foundation
import EarnestKit
import SwiftUI

/// Search is online and window-local. Results and credentials never enter the
/// offline cache; session and request checks fence late responses after sign-out
/// or a new query. Saved definitions themselves belong to the server account.
@MainActor final class SearchModel: ObservableObject {
    @Published var query = ""
    @Published var filter = APISearchFilter()
    @Published var savedName = ""
    @Published private(set) var submittedQuery = ""
    @Published private(set) var matches: [APISearchMatch] = []
    @Published private(set) var savedSearches: [APISavedSearch] = []
    @Published private(set) var selectedSavedID: Int64?
    @Published private(set) var nextCursor: String?
    @Published private(set) var status: String?
    @Published private(set) var embeddingStatus = "disabled"
    @Published private(set) var loading = false
    @Published private(set) var loadingMetadata = false
    @Published private(set) var saving = false
    @Published private(set) var error: String?
    @Published private(set) var metadataError: String?
    @Published private(set) var notice: String?

    private let app: AppModel
    private var submittedFilter = APISearchFilter()
    private var generation: String?
    private var requestID = 0
    private var searchTask: Task<Void, Never>?
    private var metadataTask: Task<Void, Never>?
    private var mutationTask: Task<Void, Never>?

    init(app: AppModel) { self.app = app }

    var selectedSaved: APISavedSearch? { savedSearches.first { $0.id == selectedSavedID } }
    var canSearch: Bool { !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var folders: [(id: Int64, name: String)] {
        ((try? app.engine?.snapshot().objects) ?? []).filter { $0.kind == .folder && !$0.deleted }
            .map { (id: $0.id, name: $0.fields["name"]?.stringValue ?? "Folder \($0.id)") }
            .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }

    func reset() {
        searchTask?.cancel()
        metadataTask?.cancel()
        mutationTask?.cancel()
        requestID &+= 1
        query = ""
        filter = APISearchFilter()
        submittedFilter = APISearchFilter()
        savedName = ""
        submittedQuery = ""
        matches = []
        savedSearches = []
        selectedSavedID = nil
        nextCursor = nil
        status = nil
        embeddingStatus = "disabled"
        loading = false
        loadingMetadata = false
        saving = false
        error = nil
        metadataError = nil
        notice = nil
        generation = nil
    }

    func invalidateAfterRestore() {
        let current = try? app.engine?.snapshot().cursor?.generation
        if let generation, current != generation { reset() }
        generation = current
    }

    private func isCurrent(_ session: Int, generation: String?) -> Bool {
        app.loggedIn && app.session == session && !Task.isCancelled
            && (generation == nil || (try? app.engine?.snapshot().cursor?.generation) == generation)
    }

    func loadMetadata() {
        guard let api = app.api, app.loggedIn else { return }
        invalidateAfterRestore()
        metadataTask?.cancel()
        let session = app.session
        let generation = self.generation
        loadingMetadata = true
        metadataError = nil
        metadataTask = Task { [weak self] in
            guard let self else { return }
            defer { if self.isCurrent(session, generation: generation) { self.loadingMetadata = false } }
            do {
                let saved = try await api.savedSearches()
                guard self.isCurrent(session, generation: generation) else { return }
                self.savedSearches = saved
            } catch {
                guard self.isCurrent(session, generation: generation) else { return }
                if ErrorPresentation.isUnauthorized(error) { app.report(error); return }
                self.metadataError = "Could not load saved searches. \(ErrorPresentation.message(error))"
            }
            do {
                let pipeline = try await api.pipelineStatus()
                guard self.isCurrent(session, generation: generation) else { return }
                self.embeddingStatus = pipeline["embed"]?.stringValue ?? "disabled"
            } catch {
                guard self.isCurrent(session, generation: generation) else { return }
                if ErrorPresentation.isUnauthorized(error) { app.report(error); return }
                self.embeddingStatus = "disabled"
                self.metadataError = "Could not check AI search availability. Full-text search is still available."
            }
        }
    }

    func selectSaved(_ saved: APISavedSearch?) {
        guard !saving else { return }
        selectedSavedID = saved?.id
        savedName = saved?.name ?? ""
        guard let saved else { return }
        query = saved.query
        filter = saved.filters
        submit()
    }

    func submit() {
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { error = "Enter a search query."; return }
        guard trimmed.utf8.count <= 4096 else { error = "Search queries must be at most 4,096 bytes."; return }
        invalidateAfterRestore()
        submittedQuery = trimmed
        submittedFilter = filter
        if embeddingStatus != "enabled" { submittedFilter.mode = .fulltext }
        matches = []
        nextCursor = nil
        status = nil
        notice = nil
        fetch(cursor: nil)
    }

    func loadMore() {
        guard !loading, let nextCursor else { return }
        fetch(cursor: nextCursor)
    }

    func useFullText() {
        filter.mode = .fulltext
        submit()
    }

    private func fetch(cursor: String?) {
        guard let api = app.api, app.loggedIn else { error = "Sign in before searching."; return }
        searchTask?.cancel()
        requestID &+= 1
        let requestID = self.requestID
        let session = app.session
        let generation = self.generation
        let query = submittedQuery
        let filter = submittedFilter
        loading = true
        error = nil
        searchTask = Task { [weak self] in
            guard let self else { return }
            defer {
                if self.requestID == requestID, self.isCurrent(session, generation: generation) { self.loading = false }
            }
            do {
                let page = try await api.search(query: query, filter: filter, cursor: cursor)
                guard self.requestID == requestID, self.isCurrent(session, generation: generation) else { return }
                let incoming = page.matches ?? []
                if let cursor, !incoming.isEmpty, page.nextCursor == cursor {
                    self.error = "The server returned a repeated search cursor. Run the search again."
                    self.nextCursor = nil
                    return
                }
                var ids = Set(self.matches.map(\.id))
                self.matches.append(contentsOf: incoming.filter { ids.insert($0.id).inserted })
                let unavailable = page.status == "disabled" || page.status == "paused"
                self.status = unavailable ? page.status : nil
                self.nextCursor = incoming.isEmpty || unavailable ? nil : page.nextCursor?.nonempty
            } catch {
                guard self.requestID == requestID, self.isCurrent(session, generation: generation) else { return }
                if ErrorPresentation.isUnauthorized(error) { app.report(error); return }
                self.error = "Could not search. \(ErrorPresentation.message(error))"
            }
        }
    }

    private enum SavedAction {
        case create(name: String, query: String, filter: APISearchFilter)
        case rename(id: Int64, name: String)
        case delete(id: Int64)
    }

    func saveNew() {
        let name = savedName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty, !submittedQuery.isEmpty else { error = "Enter a name and run a search before saving it."; return }
        mutate(.create(name: name, query: submittedQuery, filter: submittedFilter))
    }

    func rename() {
        let name = savedName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let id = selectedSavedID, !name.isEmpty else { error = "Enter a saved search name."; return }
        mutate(.rename(id: id, name: name))
    }

    func delete() {
        guard let id = selectedSavedID else { return }
        mutate(.delete(id: id))
    }

    private func mutate(_ action: SavedAction) {
        guard let api = app.api, app.loggedIn, !saving else { return }
        let session = app.session
        let generation = self.generation
        saving = true
        error = nil
        notice = nil
        mutationTask = Task { [weak self] in
            guard let self else { return }
            defer { if self.isCurrent(session, generation: generation) { self.saving = false } }
            do {
                switch action {
                case .create(let name, let query, let filter):
                    let saved = try await api.createSavedSearch(name: name, query: query, filter: filter)
                    guard self.isCurrent(session, generation: generation) else { return }
                    self.savedSearches.removeAll { $0.id == saved.id }
                    self.savedSearches.append(saved)
                    self.notice = "Search saved."
                case .rename(let id, let name):
                    try await api.renameSavedSearch(id: id, name: name)
                    guard self.isCurrent(session, generation: generation) else { return }
                    self.notice = "Saved search renamed."
                    self.savedSearches = self.savedSearches.map {
                        var saved = $0
                        if saved.id == id { saved.name = name }
                        return saved
                    }
                case .delete(let id):
                    try await api.deleteSavedSearch(id: id)
                    guard self.isCurrent(session, generation: generation) else { return }
                    self.savedSearches.removeAll { $0.id == id }
                    if self.selectedSavedID == id { self.selectedSavedID = nil; self.savedName = "" }
                    self.notice = "Saved search deleted."
                }
            } catch {
                guard self.isCurrent(session, generation: generation) else { return }
                if ErrorPresentation.isUnauthorized(error) { app.report(error); return }
                self.error = "Could not save the search change. \(ErrorPresentation.message(error))"
            }
        }
    }
}

private extension String {
    var nonempty: String? { isEmpty ? nil : self }
}

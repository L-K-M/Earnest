import SwiftUI
import EarnestKit

extension APISavedSearch: @retroactive Identifiable {}

/// Search results for the toolbar search field, with a scope bar of filters
/// and a Save button, as in Finder. Publisher matches and agent notes stay in
/// separate, labelled sections; snippets are literal text.
@MainActor struct SearchView: View {
    @ObservedObject var search: SearchModel
    @EnvironmentObject private var app: AppModel
    @EnvironmentObject private var window: WindowModel
    @SwiftUI.State private var saving = false

    private static let pageSizes: [Int64] = [20, 50, 100, 200]

    var body: some View {
        VStack(spacing: 0) {
            scopeBar
            Divider()
            results
        }
        .sheet(isPresented: $saving) {
            NameSheet(title: "Save Search", prompt: "Name", action: "Save", initial: search.submittedQuery) { name in
                search.saveNew(named: name)
            }
        }
    }

    private var entries: [APISearchMatch] { search.matches.filter { $0.kind == "entry" } }
    private var notes: [APISearchMatch] { search.matches.filter { $0.kind == "agent_note" } }
    private var hybridAvailable: Bool { search.embeddingStatus == "enabled" }

    private var scopeBar: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Picker("Feed", selection: filterBinding(\.subscriptionID)) {
                    Text("All Feeds").tag(Int64?.none)
                    Divider()
                    ForEach(app.feeds.filter { !$0.deleted }) { feed in
                        Text(verbatim: feed.title.isEmpty ? feed.url : feed.title).tag(Int64?.some(feed.id))
                    }
                }
                .help("Search one feed")
                Picker("Folder", selection: filterBinding(\.folderID)) {
                    Text("All Folders").tag(Int64?.none)
                    if !search.folders.isEmpty { Divider() }
                    ForEach(search.folders, id: \.id) { folder in
                        Text(verbatim: folder.name).tag(Int64?.some(folder.id))
                    }
                }
                .help("Search one folder")
                .disabled(search.folders.isEmpty)
            }
            .labelsHidden()
            .pickerStyle(.menu)
            HStack(spacing: 8) {
                if hybridAvailable {
                    Picker("Mode", selection: filterBinding(\.mode)) {
                        Text("Full Text").tag(APISearchMode.fulltext)
                        Text("Hybrid").tag(APISearchMode.hybrid)
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .fixedSize()
                    .help("Hybrid search can make an embedding call and searches a bounded window. Use Full Text to page further.")
                }
                Menu {
                    Picker("Results per Page", selection: filterBinding(\.limit)) {
                        Text("Server Default").tag(Int64?.none)
                        ForEach(Self.pageSizes, id: \.self) { Text("\($0)").tag(Int64?.some($0)) }
                        if let limit = search.filter.limit, !Self.pageSizes.contains(limit) {
                            Text("\(limit)").tag(Int64?.some(limit))
                        }
                    }
                    .pickerStyle(.inline)
                } label: {
                    Label("Results per Page", systemImage: "line.3.horizontal.decrease.circle")
                }
                .menuStyle(.button)
                .buttonStyle(.borderless)
                .labelStyle(.iconOnly)
                .fixedSize()
                .help("Results per page")
                Spacer(minLength: 0)
                if search.selectedSavedID == nil {
                    Button("Save…") { saving = true }
                        .help("Save this search in the sidebar")
                        .disabled(!search.hasResults || search.saving || search.loading)
                }
                if search.saving { ProgressView().controlSize(.small) }
            }
            if search.embeddingStatus == "paused" {
                Text("Hybrid search is paused. Full-text search is available.")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .controlSize(.small)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    /// Filters apply at once to a submitted search, as Finder's scope bar does.
    private func filterBinding<Value>(_ path: WritableKeyPath<APISearchFilter, Value>) -> Binding<Value> {
        Binding(get: { search.filter[keyPath: path] }, set: { value in
            search.filter[keyPath: path] = value
            if search.hasResults { search.submit() }
        })
    }

    /// Results as article-table rows. Row identifiers are positions, since an
    /// article and an agent note on it share an entry ID.
    private var table: (groups: [EditionGroup], matches: [Int64: APISearchMatch]) {
        var byRow: [Int64: APISearchMatch] = [:]
        var next: Int64 = 1
        func rows(_ matches: [APISearchMatch]) -> [EntryRow] {
            matches.map { match in
                let id = next
                next += 1
                byRow[id] = match
                let source = match.kind == "agent_note"
                    ? "Agent note · \(match.agentName.flatMap { $0.isEmpty ? nil : $0 } ?? "Unknown agent")"
                    : app.feedMap[match.subscriptionID]?.title ?? "Feed"
                let snippet = match.snippet.trimmingCharacters(in: .whitespacesAndNewlines)
                let url = URL(string: match.url).flatMap { ["http", "https"].contains($0.scheme?.lowercased() ?? "") ? $0 : nil }
                return EntryRow(id: id, title: match.title.isEmpty ? "Untitled" : match.title,
                                subtitle: snippet.isEmpty ? source : "\(source) — \(snippet)", unread: false, url: url)
            }
        }
        var groups: [EditionGroup] = []
        if !entries.isEmpty { groups.append(EditionGroup(subscriptionID: -1, title: "Articles", rows: rows(entries))) }
        if !notes.isEmpty { groups.append(EditionGroup(subscriptionID: -2, title: "Agent Notes", rows: rows(notes))) }
        return (groups, byRow)
    }

    @ViewBuilder private var results: some View {
        if !search.hasResults {
            ContentUnavailableView("Search Your Feeds", systemImage: "magnifyingglass",
                                   description: Text("Type in the search field and press Return. Publisher text and agent notes are searched separately."))
        } else if search.matches.isEmpty && search.loading {
            ProgressView("Searching…").controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if search.matches.isEmpty && (search.error != nil || search.status != nil) {
            // The message bar below says why and offers the way forward.
            ContentUnavailableView("Couldn’t Search", systemImage: "exclamationmark.magnifyingglass")
        } else if search.matches.isEmpty {
            ContentUnavailableView.search(text: search.submittedQuery)
        } else {
            let table = self.table
            EntryTable(groups: table.groups, selection: Binding(get: {
                table.matches.first { $0.value.id == window.selectedMatchID && $0.value.entryID == window.selected }?.key
            }, set: { row in
                if let row, let match = table.matches[row] { window.selectSearchMatch(match) }
            }), actions: EntryTableActions(
                perform: { command, row in
                    if let match = table.matches[row] { window.command(command, entryID: match.entryID) }
                },
                openInBrowser: { row in
                    if let url = table.matches[row].flatMap({ URL(string: $0.url) }),
                       ["http", "https"].contains(url.scheme?.lowercased() ?? "") { NSWorkspace.shared.open(url) }
                },
                canAct: window.tableActions.canAct,
                // Reading actions need the article, which loads when it is opened.
                canActOn: { row in
                    guard let id = table.matches[row]?.entryID else { return false }
                    return window.entries.contains { $0.id == id }
                }))
            if search.nextCursor != nil {
                Button("Load More Results") { search.loadMore() }
                    .disabled(search.loading)
                    .buttonStyle(.link)
                    .padding(8)
                    .frame(maxWidth: .infinity)
                    .background(.bar)
            }
        }
        messages
    }

    @ViewBuilder private var messages: some View {
        let lines = [search.error, search.metadataError, search.notice].compactMap { $0 }
        if !lines.isEmpty || search.status != nil {
            VStack(alignment: .leading, spacing: 6) {
                if let status = search.status {
                    HStack {
                        Text("Hybrid search is \(status).")
                        Spacer()
                        Button("Use Full Text") { search.useFullText() }
                    }
                }
                ForEach(lines, id: \.self) { Text(verbatim: $0) }
                if search.metadataError != nil {
                    Button("Retry") { search.loadMetadata() }.disabled(search.loadingMetadata)
                }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(10)
            .background(.bar)
        }
    }
}

import SwiftUI
import EarnestKit

@MainActor struct SearchView: View {
    @ObservedObject var search: SearchModel
    @EnvironmentObject private var app: AppModel
    @EnvironmentObject private var window: WindowModel
    @FocusState private var field: Field?
    @SwiftUI.State private var confirmingDelete = false

    private enum Field { case query, name }

    var body: some View {
        VStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    TextField("Search your feeds", text: $search.query)
                        .textFieldStyle(.roundedBorder)
                        .focused($field, equals: .query)
                        .onSubmit { search.submit() }
                    Button("Search") { search.submit() }.disabled(!search.canSearch)
                }
                HStack {
                    Picker("Mode", selection: Binding(
                        get: { search.embeddingStatus == "enabled" ? search.filter.mode : .fulltext },
                        set: { search.filter.mode = $0 }
                    )) {
                        Text("Full text").tag(APISearchMode.fulltext)
                        if search.embeddingStatus == "enabled" { Text("Hybrid").tag(APISearchMode.hybrid) }
                    }
                    Picker("Page size", selection: $search.filter.limit) {
                        Text("Default").tag(Int64?.none)
                        ForEach([20, 50, 100, 200], id: \.self) { count in
                            Text("\(count)").tag(Int64?.some(Int64(count)))
                        }
                        if let limit = search.filter.limit, ![20, 50, 100, 200].contains(limit) {
                            Text("\(limit)").tag(Int64?.some(limit))
                        }
                    }
                }
                Picker("Feed", selection: $search.filter.subscriptionID) {
                    Text("All feeds").tag(Int64?.none)
                    ForEach(app.feeds.filter { !$0.deleted }) { feed in
                        Text(verbatim: feed.title.isEmpty ? feed.url : feed.title).tag(Int64?.some(feed.id))
                    }
                }
                Picker("Folder", selection: $search.filter.folderID) {
                    Text("All folders").tag(Int64?.none)
                    ForEach(search.folders, id: \.id) { folder in
                        Text(verbatim: folder.name).tag(Int64?.some(folder.id))
                    }
                }
                if search.embeddingStatus == "paused" {
                    Text("Hybrid search is paused. Full-text search is available.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                if search.filter.mode == .hybrid && search.embeddingStatus == "enabled" {
                    Text("Hybrid search can make an embedding call and searches a bounded window. Use Full text to page further.")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            .padding([.horizontal, .top], 10)

            List {
                if !search.savedSearches.isEmpty {
                    SwiftUI.Section("Saved searches") {
                        ForEach(search.savedSearches, id: \.id) { saved in
                            Button { search.selectSaved(saved) } label: {
                                HStack {
                                    Text(verbatim: saved.name)
                                    if search.selectedSavedID == saved.id { Image(systemName: "checkmark") }
                                }
                            }
                            .disabled(search.saving)
                        }
                        Button("New saved search") { search.selectSaved(nil) }.disabled(search.saving)
                    }
                }
                if !search.submittedQuery.isEmpty {
                    SwiftUI.Section("Articles") {
                        ForEach(search.matches.filter { $0.kind == "entry" }) { match in result(match) }
                    }
                    SwiftUI.Section("Agent notes") {
                        ForEach(search.matches.filter { $0.kind == "agent_note" }) { match in result(match) }
                    }
                    if !search.loading && search.matches.isEmpty && search.error == nil && search.status == nil {
                        Text("No matches.").foregroundStyle(.secondary)
                    }
                    if let status = search.status {
                        Text("Hybrid search is \(status).")
                        Button("Use full-text search") { search.useFullText() }
                    }
                    if search.nextCursor != nil {
                        Button("More search results") { search.loadMore() }.disabled(search.loading)
                    }
                } else {
                    Text("Search publisher content and separately labelled agent notes.")
                        .foregroundStyle(.secondary)
                }
            }
            if search.loading { ProgressView("Searching…").controlSize(.small) }
            if let error = search.error { message(error, color: .red) }
            if let error = search.metadataError {
                message(error, color: .red)
                Button("Retry search setup") { search.loadMetadata() }.disabled(search.loadingMetadata)
            }
            if let notice = search.notice { message(notice) }
            if !search.submittedQuery.isEmpty {
                VStack(spacing: 8) {
                    TextField("Saved search name", text: $search.savedName)
                        .textFieldStyle(.roundedBorder)
                        .focused($field, equals: .name)
                        .disabled(search.saving)
                    HStack {
                        Button("Save as new") { search.saveNew() }.disabled(search.saving)
                        if search.selectedSavedID != nil {
                            Button("Rename") { search.rename() }.disabled(search.saving)
                            Button("Delete", role: .destructive) { confirmingDelete = true }.disabled(search.saving)
                        }
                        if search.saving { ProgressView().controlSize(.small) }
                    }
                }
                .padding([.horizontal, .bottom], 10)
            }
        }
        .onChange(of: field) { window.editingSearch = field != nil }
        .onDisappear { window.editingSearch = false }
        .confirmationDialog("Delete this saved search?", isPresented: $confirmingDelete) {
            Button("Delete", role: .destructive) { search.delete() }
        } message: {
            Text("Only the saved view is removed. Your articles stay in Earnest.")
        }
    }

    private func result(_ match: APISearchMatch) -> some View {
        Button { window.selectSearchMatch(match) } label: {
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: match.title.isEmpty ? "Untitled" : match.title).font(.headline)
                if match.kind == "agent_note" {
                    Text("Agent note · \(match.agentName.flatMap { $0.isEmpty ? nil : $0 } ?? "Unknown agent")")
                        .font(.caption).foregroundStyle(.secondary)
                }
                Text(verbatim: match.snippet).font(.callout).lineLimit(4)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.vertical, 4)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(match.kind == "agent_note" ? "Agent note" : "Article"): \(match.title.isEmpty ? "Untitled" : match.title)")
    }

    private func message(_ text: String, color: Color = .secondary) -> some View {
        Text(verbatim: text).font(.caption).foregroundStyle(color)
            .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 10)
    }
}

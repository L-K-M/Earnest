import SwiftUI
import EarnestKit

/// The window's places, in the order a reading session runs: the river and
/// edition, the shelves, saved searches like Mail's smart mailboxes, and the
/// library of feeds and agent activity. Counts appear only in the unread-count
/// reading mode.
@MainActor struct SidebarView: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel
    @SwiftUI.State private var renaming: APISavedSearch?
    @SwiftUI.State private var deleting: APISavedSearch?

    enum Item: Hashable {
        case place(Section)
        case saved(Int64)
    }

    private var selection: Binding<Item?> {
        Binding(get: {
            if window.section == .search { return window.search.selectedSavedID.map(Item.saved) }
            return .place(window.section)
        }, set: { item in
            switch item {
            case .place(let section): window.go(to: section)
            case .saved(let id):
                if let saved = window.search.savedSearches.first(where: { $0.id == id }) { window.openSavedSearch(saved) }
            case nil: break
            }
        })
    }

    var body: some View {
        List(selection: selection) {
            SwiftUI.Section("Reading") {
                place(.today, count: counts?.total)
                place(.edition)
            }
            SwiftUI.Section("Shelves") {
                place(.later, count: counts?.later)
                place(.kept, count: counts?.kept)
                place(.done)
            }
            if !window.search.savedSearches.isEmpty {
                SwiftUI.Section("Saved Searches") {
                    ForEach(window.search.savedSearches, id: \.id) { saved in
                        Label {
                            Text(verbatim: saved.name)
                        } icon: {
                            Image(systemName: "magnifyingglass")
                        }
                        .tag(Item.saved(saved.id))
                        .contextMenu {
                            Group {
                                Button("Rename…", systemImage: "pencil") { renaming = saved }
                                Button("Delete…", systemImage: "trash", role: .destructive) { deleting = saved }
                            }
                            .labelStyle(.titleAndIcon)
                        }
                        .disabled(window.search.saving)
                    }
                }
            }
            SwiftUI.Section("Library") {
                place(.feeds)
                place(.activity)
            }
        }
        .listStyle(.sidebar)
        .safeAreaInset(edge: .bottom, spacing: 0) { SyncStatusView() }
        .onAppear { if window.search.savedSearches.isEmpty { window.search.loadMetadata() } }
        .onChange(of: model.lastSyncedAt) {
            if window.search.savedSearches.isEmpty && window.search.metadataError == nil { window.search.loadMetadata() }
        }
        .sheet(item: $renaming) { saved in
            NameSheet(title: "Rename Saved Search", prompt: "Name", action: "Rename", initial: saved.name) { name in
                window.search.rename(saved, to: name)
            }
        }
        .confirmationDialog(deleting.map { "Delete “\($0.name)”?" } ?? "",
                            isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            presenting: deleting) { saved in
            Button("Delete", role: .destructive) {
                if window.section == .search && window.search.selectedSavedID == saved.id { window.endSearch() }
                window.search.delete(saved)
            }
        } message: { _ in
            Text("Only the saved search is removed. Your articles stay in Earnest.")
        }
    }

    private var counts: APIUnreadCounts? {
        model.readerSettings.readingMode == .unreadCounts ? model.unreadCounts : nil
    }

    private func place(_ section: Section, count: Int64? = nil) -> some View {
        Label(section.title, systemImage: section.icon)
            .badge(count.map { Int($0) } ?? 0)
            .tag(Item.place(section))
    }
}

/// A quiet line at the foot of the sidebar: what sync is doing, what is
/// waiting, and when the account last caught up.
@MainActor struct SyncStatusView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        HStack(spacing: 6) {
            if model.transfer != nil || model.syncing {
                ProgressView().controlSize(.mini)
            } else if model.connectionNote != nil {
                Image(systemName: "exclamationmark.icloud").foregroundStyle(.secondary)
            }
            TimelineView(.periodic(from: .now, by: 30)) { context in
                Text(status(now: context.date))
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .help(model.readerSettings.readingMode == .unreadCounts ? "Counts as last synced." : "")
    }

    private func status(now: Date) -> String {
        if let transfer = model.transfer { return transfer }
        if model.syncing { return "Syncing…" }
        var parts: [String] = []
        if let note = model.connectionNote { parts.append(note) }
        if model.pendingOps > 0 {
            parts.append("\(model.pendingOps) change\(model.pendingOps == 1 ? "" : "s") waiting to sync.")
        }
        if parts.isEmpty, let synced = model.lastSyncedAt {
            parts.append(now.timeIntervalSince(synced) < 60
                         ? "Updated just now"
                         : "Updated \(synced.formatted(.relative(presentation: .named)))")
        }
        if parts.isEmpty, let error = model.unreadCountsError { parts.append(error) }
        return parts.joined(separator: " ")
    }
}

/// A small sheet that asks for a name, used to save and rename searches.
struct NameSheet: View {
    let title: String
    let prompt: String
    let action: String
    let initial: String
    let commit: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @SwiftUI.State private var name = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(title).font(.headline)
            TextField(prompt, text: $name, prompt: Text("Name"))
                .textFieldStyle(.roundedBorder)
                .focused($focused)
                .onSubmit(submit)
            HStack {
                Spacer()
                Button("Cancel", role: .cancel) { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button(action) { submit() }
                    .keyboardShortcut(.defaultAction)
                    .disabled(trimmed.isEmpty)
            }
        }
        .padding(20)
        .frame(width: 340)
        .onAppear {
            name = initial
            focused = true
        }
    }

    private var trimmed: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }

    private func submit() {
        guard !trimmed.isEmpty else { return }
        commit(trimmed)
        dismiss()
    }
}

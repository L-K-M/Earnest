import Foundation
import SwiftUI
import EarnestKit

/// Earnest > Settings. Account and reading preferences apply as you change
/// them, as Mac settings do; the account's synced settings are the source of
/// truth, and a change made elsewhere is flagged instead of overwritten.
@MainActor struct SettingsWindow: View {
    @ObservedObject var settings: ReaderSettingsModel
    @EnvironmentObject private var model: AppModel

    private enum Pane: String { case general, reading }
    @AppStorage("settingsTab") private var tab = Pane.general

    var body: some View {
        TabView(selection: $tab) {
            Tab("General", systemImage: "gearshape", value: Pane.general) {
                GeneralSettings(settings: settings)
            }
            Tab("Reading", systemImage: "textformat.size", value: Pane.reading) {
                ReadingSettings(settings: settings)
            }
        }
        .scenePadding()
        .frame(width: 520)
        .fixedSize(horizontal: false, vertical: true)
    }
}

@MainActor private struct GeneralSettings: View {
    @ObservedObject var settings: ReaderSettingsModel
    @EnvironmentObject private var model: AppModel
    @SwiftUI.State private var confirmingSignOut = false

    var body: some View {
        Form {
            SwiftUI.Section("Account") {
                LabeledContent("Server") {
                    Text(verbatim: model.server.isEmpty ? "Not connected" : model.server)
                        .textSelection(.enabled)
                        .foregroundStyle(model.server.isEmpty ? .secondary : .primary)
                }
                LabeledContent("Status") {
                    HStack {
                        Text(model.loggedIn ? "Signed in" : "Signed out")
                            .foregroundStyle(.secondary)
                        Spacer()
                        if model.loggedIn {
                            Button("Sign Out…") { confirmingSignOut = true }
                        }
                    }
                }
            }
            SwiftUI.Section {
                Picker("Reading mode", selection: $settings.draft.readingMode) {
                    Text("A river that ends").tag(APIReadingMode.river)
                    Text("Unread counts").tag(APIReadingMode.unreadCounts)
                }
                .pickerStyle(.radioGroup)
                Toggle(isOn: $settings.draft.showOnlyUnread) {
                    Text("Show only unread")
                    Text("Read articles stay until you reopen or refresh the list. Saved shelves and search are not filtered.")
                }
                IntegerField("Keep Today articles for", value: $settings.draft.todayDecayDays, in: 1...365,
                             unit: settings.draft.todayDecayDays == 1 ? "day" : "days")
                Toggle(isOn: $settings.draft.singleKeyShortcuts) {
                    Text("Single-key shortcuts")
                    Text("J, K, E, S, F, M and O act on articles when you are not typing.")
                }
            } header: {
                Text("Reading")
            } footer: {
                SettingsStatus(settings: settings)
            }
            .disabled(!settings.ready)
        }
        .formStyle(.grouped)
        .alert("Sign Out of Earnest?", isPresented: $confirmingSignOut) {
            Button("Sign Out", role: .destructive) { model.signOut() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("You will need to approve this Mac again to sign in.")
        }
    }
}

@MainActor private struct ReadingSettings: View {
    @ObservedObject var settings: ReaderSettingsModel
    @EnvironmentObject private var model: AppModel

    private var overriddenFeeds: [FeedInfo] {
        model.feeds.filter { $0.feedID > 0 && settings.draft.feedOverrides[String($0.feedID)] != nil }
    }

    var body: some View {
        Form {
            SwiftUI.Section {
                Picker("Theme", selection: $settings.draft.theme) {
                    ForEach(APIReaderTheme.allCases, id: \.self) { Text($0.title).tag($0) }
                }
                Picker("Font", selection: $settings.draft.font) {
                    ForEach(APIReaderFont.allCases, id: \.self) { Text($0.title).tag($0) }
                }
                IntegerField("Line length", value: $settings.draft.measure, in: 62...72, unit: "characters")
            } header: {
                Text("Article Typography")
            } footer: {
                SettingsStatus(settings: settings)
            }
            SwiftUI.Section {
                if overriddenFeeds.isEmpty {
                    Text("Every feed uses the typography above. To change one feed, select it in Feeds.")
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(overriddenFeeds) { feed in
                        LabeledContent(feed.displayTitle) {
                            Button("Use Account Typography") { settings.resetFeedOverride(feedID: feed.feedID) }
                                .buttonStyle(.link)
                        }
                    }
                }
            } header: {
                Text("Feeds with Their Own Typography")
            }
        }
        .formStyle(.grouped)
        .disabled(!settings.ready)
    }
}

/// Saving, saved, failed or conflicting: the settings draft's state, with
/// the one action that resolves it.
@MainActor struct SettingsStatus: View {
    @ObservedObject var settings: ReaderSettingsModel

    var body: some View {
        HStack(spacing: 6) {
            if !settings.ready {
                Text("Sign in and sync to change reading settings.")
            } else if settings.busy {
                ProgressView().controlSize(.mini)
                Text("Saving…")
            } else if settings.conflict {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.yellow)
                Text(settings.error ?? "Settings changed on another device.")
                Button("Use Latest") { settings.load() }.buttonStyle(.link)
            } else if let error = settings.error {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.yellow)
                Text(error).textSelection(.enabled)
                Button("Try Again") { settings.save() }.buttonStyle(.link)
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .accessibilityElement(children: .contain)
    }
}

/// The text of a number field while it is edited. Text you type is kept,
/// even incomplete, until it is committed; a value changed by the stepper or
/// a sync replaces the text unless you have typed since the last commit, and
/// committing untouched text never writes an old value back.
struct IntegerEditing: Equatable {
    private(set) var text: String
    private(set) var edited = false

    init(value: Int64) { text = String(value) }

    mutating func typed(_ text: String) {
        self.text = text
        edited = true
    }

    mutating func valueChanged(to value: Int64, focused: Bool) {
        guard !(focused && edited) else { return }
        text = String(value)
    }

    mutating func commit(_ value: inout Int64, in range: ClosedRange<Int64>) {
        if edited, let number = IntegerField.parse(text, in: range), number != value { value = number }
        edited = false
        text = String(value)
    }

    mutating func revert(to value: Int64) {
        edited = false
        text = String(value)
    }
}

/// A whole number typed or stepped within a range. Typing can pass through
/// incomplete values like "6" on the way to "68"; the field commits on
/// Return, Tab or when focus leaves, clamped to the range, and Escape puts
/// back the current value. The stepper applies at once.
struct IntegerField: View {
    let title: String
    @Binding var value: Int64
    let range: ClosedRange<Int64>
    let unit: String
    @SwiftUI.State private var editing = IntegerEditing(value: 0)
    @FocusState private var focused: Bool

    init(_ title: String, value: Binding<Int64>, in range: ClosedRange<Int64>, unit: String) {
        self.title = title
        _value = value
        self.range = range
        self.unit = unit
    }

    /// The committed value for typed text, or nil to keep the current one.
    static func parse(_ text: String, in range: ClosedRange<Int64>) -> Int64? {
        guard let number = Int64(text.trimmingCharacters(in: .whitespaces)) else { return nil }
        return clamp(number, to: range)
    }

    static func clamp(_ value: Int64, to range: ClosedRange<Int64>) -> Int64 {
        min(range.upperBound, max(range.lowerBound, value))
    }

    var body: some View {
        LabeledContent(title) {
            HStack(spacing: 6) {
                TextField(title, text: Binding(get: { editing.text }, set: { editing.typed($0) }))
                    .labelsHidden()
                    .multilineTextAlignment(.trailing)
                    .frame(width: 52)
                    .focused($focused)
                    .onSubmit(commit)
                    // Tab ends editing, as in AppKit, even when focus has nowhere to go.
                    .onKeyPress(keys: [.tab, KeyEquivalent("\u{19}")]) { _ in
                        commit()
                        return .ignored
                    }
                    .onKeyPress(.escape) {
                        editing.revert(to: value)
                        return .handled
                    }
                Stepper(title, value: Binding(get: { value }, set: { value = Self.clamp($0, to: range) }), in: range)
                    .labelsHidden()
                Text(unit).foregroundStyle(.secondary)
            }
        }
        .onAppear { editing.revert(to: value) }
        .onChange(of: focused) { if !focused { commit() } }
        .onChange(of: value) { editing.valueChanged(to: value, focused: focused) }
    }

    private func commit() {
        var committed = value
        editing.commit(&committed, in: range)
        if committed != value { value = committed }
    }
}

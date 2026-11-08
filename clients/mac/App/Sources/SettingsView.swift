import Foundation
import SwiftUI
import EarnestKit

/// Account reader preferences for one window. The draft is local; Save writes
/// the whole document, Reload discards local edits, and the status line shows
/// busy, error or confirmed success.
struct SettingsView: View {
    @ObservedObject var settings: ReaderSettingsModel
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Form {
            SwiftUI.Section("Reading") {
                Picker("Mode", selection: $settings.draft.readingMode) {
                    Text("A river that ends").tag(APIReadingMode.river)
                    Text("Unread counts").tag(APIReadingMode.unreadCounts)
                }
                Stepper(value: $settings.draft.todayDecayDays, in: 1...365) {
                    Text("Today items stay \(settings.draft.todayDecayDays) days")
                }
                Toggle("Single-key shortcuts", isOn: $settings.draft.singleKeyShortcuts)
            }
            SwiftUI.Section("Typography") {
                Picker("Theme", selection: $settings.draft.theme) {
                    ForEach(APIReaderTheme.allCases, id: \.self) { Text($0.rawValue.capitalized).tag($0) }
                }
                Stepper(value: $settings.draft.measure, in: 62...72) {
                    Text("Measure \(settings.draft.measure) characters")
                }
                Picker("Font", selection: $settings.draft.font) {
                    ForEach(APIReaderFont.allCases, id: \.self) { Text($0.rawValue.capitalized).tag($0) }
                }
            }
            SwiftUI.Section("Per-feed typography") {
                // Overrides are keyed by feed ID, the same identity the reader
                // resolves; subscriptions without one cannot carry an override.
                let feeds = model.feeds.filter { !$0.deleted && $0.feedID > 0 }
                if feeds.isEmpty {
                    Text("No subscriptions").foregroundStyle(.secondary)
                } else {
                    ForEach(feeds) { feed in
                        FeedTypographyRow(
                            feedID: feed.feedID,
                            title: feed.title.isEmpty ? "Feed \(feed.feedID)" : feed.title,
                            settings: settings
                        )
                    }
                }
            }
            SwiftUI.Section {
                HStack {
                    Button("Save") { settings.save() }
                        .disabled(settings.busy || !model.loggedIn)
                    Button("Reload") { settings.load() }
                        .disabled(settings.busy)
                    if settings.busy {
                        ProgressView().controlSize(.small)
                    }
                }
                if let error = settings.error {
                    Text(error).font(.caption).foregroundStyle(.red)
                        .accessibilityAddTraits(.isStaticText)
                }
                if settings.confirmed {
                    Text("Saved").font(.caption).foregroundStyle(.green)
                }
                if settings.conflict {
                    Text("Settings changed elsewhere. Reload before saving.")
                        .font(.caption).foregroundStyle(.orange)
                }
            }
        }
        .formStyle(.grouped)
        .frame(maxWidth: 560)
        .padding()
    }
}

/// One feed's typography override. Every picker's Inherit value removes only
/// that member; Reset removes the feed's entry, leaving other feeds alone.
struct FeedTypographyRow: View {
    let feedID: Int64
    let title: String
    @ObservedObject var settings: ReaderSettingsModel

    private var key: String { String(feedID) }
    private var hasOverride: Bool { settings.draft.feedOverrides[key] != nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(.headline)
            HStack {
                Picker("Theme", selection: Binding<APIReaderTheme?>(
                    get: { settings.draft.feedOverrides[key]?.theme },
                    set: { settings.setFeedOverride(feedID: feedID, theme: $0) }
                )) {
                    Text("Inherit").tag(APIReaderTheme?.none)
                    ForEach(APIReaderTheme.allCases, id: \.self) { Text($0.rawValue.capitalized).tag(APIReaderTheme?.some($0)) }
                }
                .labelsHidden()
                .accessibilityLabel("Theme for \(title)")
                Picker("Font", selection: Binding<APIReaderFont?>(
                    get: { settings.draft.feedOverrides[key]?.font },
                    set: { settings.setFeedOverride(feedID: feedID, font: $0) }
                )) {
                    Text("Inherit").tag(APIReaderFont?.none)
                    ForEach(APIReaderFont.allCases, id: \.self) { Text($0.rawValue.capitalized).tag(APIReaderFont?.some($0)) }
                }
                .labelsHidden()
                .accessibilityLabel("Font for \(title)")
            }
            HStack {
                Picker("Measure", selection: Binding<Int64?>(
                    get: { settings.draft.feedOverrides[key]?.measure },
                    set: { settings.setFeedOverride(feedID: feedID, measure: $0) }
                )) {
                    Text("Inherit").tag(Int64?.none)
                    ForEach(Array(62...72), id: \.self) { Text("\($0)").tag(Int64?.some(Int64($0))) }
                }
                .labelsHidden()
                .accessibilityLabel("Measure for \(title)")
                Spacer()
                if hasOverride {
                    Button("Reset to inherit") { settings.resetFeedOverride(feedID: feedID) }
                        .controlSize(.small)
                }
            }
        }
    }
}

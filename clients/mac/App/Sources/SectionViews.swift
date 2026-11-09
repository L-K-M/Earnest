import AppKit
import Foundation
import SwiftUI
import EarnestKit

// Agent briefs are plain text: never rendered as HTML or Markdown, always
// attributed to the agent's name. They sit above the river in a tinted
// panel so they never read like the articles themselves.
struct BriefsView: View {
    let briefs: [BriefItem]
    let names: [Int64: String]
    @SwiftUI.State private var contentHeight: CGFloat = 0

    private let maximumHeight: CGFloat = 180

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                ForEach(briefs) { brief in
                    VStack(alignment: .leading, spacing: 3) {
                        Label {
                            Text(names[brief.principalID] ?? "Agent #\(brief.principalID) (name unavailable)")
                        } icon: {
                            Image(systemName: "text.bubble")
                                .foregroundStyle(Tone.purple.color)
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        Text(brief.text)
                            .font(.callout)
                            .textSelection(.enabled)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .accessibilityElement(children: .combine)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { contentHeight = $0 }
        }
        .frame(height: min(contentHeight, maximumHeight))
        .scrollBounceBehavior(.basedOnSize)
        .background(Tone.purple.color.opacity(0.08))
        .accessibilityLabel("Agent briefs")
    }
}

// MARK: - Feeds

extension FeedInfo {
    var displayTitle: String { title.isEmpty ? url : title }
    var website: URL? {
        WebLink.url(siteURL)
    }
}

/// Subscriptions as a sortable table: the name, the cadence as a pop-up, and
/// a warning for feeds that are failing. Delete unsubscribes after asking.
@MainActor struct FeedsView: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel
    @SwiftUI.State private var sortOrder = [KeyPathComparator(\FeedInfo.displayTitle, comparator: .localizedStandard)]

    private var countsMode: Bool { model.readerSettings.readingMode == .unreadCounts }

    private var feeds: [FeedInfo] {
        model.feeds.filter { !$0.deleted }.sorted(using: sortOrder)
    }

    var body: some View {
        Group {
            if model.feeds.isEmpty {
                ContentUnavailableView {
                    Label {
                        Text("No Subscriptions")
                    } icon: {
                        Image(systemName: Section.feeds.icon).foregroundStyle(Tone.blue.color)
                    }
                } description: {
                    Text("Subscribe to a feed by its address, or import an OPML file from another reader.")
                } actions: {
                    Button("New Subscription…") { window.subscribing = true }
                    Button("Import Subscriptions…") {
                        NotificationCenter.default.post(name: .earnestImportSubscriptions, object: nil)
                    }
                }
            } else {
                table
            }
        }
        .confirmationDialog(
            window.unsubscribing.map { "Unsubscribe from “\($0.displayTitle)”?" } ?? "",
            isPresented: Binding(get: { window.unsubscribing != nil }, set: { if !$0 { window.unsubscribing = nil } }),
            titleVisibility: .visible, presenting: window.unsubscribing) { feed in
            Button("Unsubscribe", role: .destructive) {
                if window.selectedFeedID == feed.id { window.selectedFeedID = nil }
                model.unsubscribe(feed.id)
                window.unsubscribing = nil
            }
            Button("Cancel", role: .cancel) { window.unsubscribing = nil }
        } message: { _ in
            Text("Earnest stops following this feed. You can subscribe to it again later.")
        }
    }

    private var table: some View {
        Table(feeds, selection: $window.selectedFeedID, sortOrder: $sortOrder) {
            TableColumn("Name", value: \.displayTitle, comparator: .localizedStandard) { feed in
                HStack(spacing: 6) {
                    if let path = feed.iconURL {
                        FeedIcon(path: path)
                    }
                    if feed.issue != nil {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.yellow)
                            .help(feed.issue ?? "")
                            .accessibilityLabel("Failing")
                    }
                    Text(verbatim: feed.displayTitle)
                        .lineLimit(1)
                        .help(feed.url)
                    Spacer(minLength: 4)
                    if countsMode, let count = model.unreadCounts?.bySubscription[String(feed.id)], count > 0 {
                        Text("\(count)")
                            .font(.callout.monospacedDigit())
                            .foregroundStyle(.secondary)
                            .accessibilityLabel("\(count) unread")
                    }
                }
            }
            TableColumn("Cadence", value: \.cadence) { feed in
                Picker("Cadence for \(feed.displayTitle)", selection: Binding(
                    get: { Cadence(server: feed.cadence) },
                    set: { window.setCadence(feed, $0.rawValue) })) {
                    ForEach(Cadence.allCases) { Text($0.title).tag($0) }
                }
                .labelsHidden()
                .pickerStyle(.menu)
                .controlSize(.small)
                .fixedSize()
            }
            .width(min: 96, ideal: 104, max: 130)
        }
        .contextMenu(forSelectionType: Int64.self) { ids in
            if let id = ids.first, let feed = model.feeds.first(where: { $0.id == id }) {
                FeedMenu(feed: feed)
            }
        } primaryAction: { ids in
            if let id = ids.first, let site = model.feeds.first(where: { $0.id == id })?.website {
                NSWorkspace.shared.open(site)
            }
        }
        .onDeleteCommand {
            if let id = window.selectedFeedID { window.unsubscribing = model.feeds.first { $0.id == id } }
        }
        .alternatingRowBackgrounds(.disabled)
        .accessibilityLabel("Subscriptions")
    }
}

/// A subscription's stored icon in the Feeds table. Nothing draws until the
/// image loads, and nothing at all when the feed has none; decorative only.
@MainActor private struct FeedIcon: View {
    let path: String
    @EnvironmentObject private var model: AppModel
    @SwiftUI.State private var image: NSImage?

    var body: some View {
        // A ZStack exists even while it is empty, so the frame keeps the slot
        // and the task runs; an empty Group would have no view to attach to.
        ZStack {
            if let image {
                Image(nsImage: image)
                    .resizable()
                    .aspectRatio(contentMode: .fit)
                    .clipShape(RoundedRectangle(cornerRadius: 3, style: .continuous))
            }
        }
        .frame(width: 16, height: 16)
        .accessibilityHidden(true)
        .task(id: path) {
            image = await model.media.image(for: path)
        }
    }
}

/// The commands for one subscription, shared by its context menu and detail.
@MainActor struct FeedMenu: View {
    let feed: FeedInfo
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel

    // From macOS 27, menus hide item icons unless the labels ask for them.
    var body: some View {
        Group { items }.labelStyle(.titleAndIcon)
    }

    @ViewBuilder private var items: some View {
        Button("Open Website", systemImage: "safari") { if let site = feed.website { NSWorkspace.shared.open(site) } }
            .disabled(feed.website == nil)
        Button("Copy Feed Address", systemImage: "doc.on.doc") {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(feed.url, forType: .string)
        }
        Divider()
        Picker("Cadence", systemImage: "timer", selection: Binding(get: { Cadence(server: feed.cadence) },
                                             set: { window.setCadence(feed, $0.rawValue) })) {
            ForEach(Cadence.allCases) { Text($0.title).tag($0) }
        }
        Button("Refresh Feed", systemImage: "arrow.clockwise") { model.refreshFeed(feed.id) }
        Divider()
        Button("Unsubscribe…", systemImage: "minus.circle", role: .destructive) { window.unsubscribing = feed }
    }
}

/// The selected subscription, with its address, cadence, health and
/// per-feed typography. Typography edits apply at once to the account.
@MainActor struct FeedDetailView: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel
    @EnvironmentObject private var preferences: ReaderSettingsModel

    private var feed: FeedInfo? { model.feeds.first { $0.id == window.selectedFeedID } }

    var body: some View {
        if let feed {
            Form {
                SwiftUI.Section {
                    LabeledContent("Name") { Text(verbatim: feed.displayTitle).textSelection(.enabled) }
                    LabeledContent("Feed Address") {
                        Text(verbatim: feed.url).textSelection(.enabled).lineLimit(2).truncationMode(.middle)
                    }
                    if let site = feed.website {
                        LabeledContent("Website") { Link(site.host() ?? site.absoluteString, destination: site) }
                    }
                    Picker("Cadence", selection: Binding(get: { Cadence(server: feed.cadence) },
                                                         set: { window.setCadence(feed, $0.rawValue) })) {
                        ForEach(Cadence.allCases) { Text($0.title).tag($0) }
                    }
                    LabeledContent("Status") {
                        if let issue = feed.issue {
                            Label(issue, systemImage: "exclamationmark.triangle.fill")
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                        } else {
                            Text("Updating normally").foregroundStyle(.secondary)
                        }
                    }
                    if model.readerSettings.readingMode == .unreadCounts {
                        LabeledContent("Unread") {
                            Text(model.unreadCounts?.bySubscription[String(feed.id)].map { "\($0)" } ?? "Unavailable")
                                .foregroundStyle(.secondary)
                        }
                    }
                } footer: {
                    cadenceFooter(Cadence(server: feed.cadence))
                }
                if feed.feedID > 0 {
                    FeedTypographySection(feedID: feed.feedID, settings: preferences)
                }
                SwiftUI.Section {
                    HStack {
                        Button("Refresh Feed") { model.refreshFeed(feed.id) }
                            .help("Ask the server to check this feed on its next pass")
                        Spacer()
                        Button("Unsubscribe…", role: .destructive) { window.unsubscribing = feed }
                    }
                }
            }
            .formStyle(.grouped)
        } else {
            ContentUnavailableView {
                Label {
                    Text("No Feed Selected")
                } icon: {
                    Image(systemName: Section.feeds.icon).foregroundStyle(Tone.blue.color)
                }
            } description: {
                Text("Select a subscription to see its details.")
            }
        }
    }

    private func cadenceFooter(_ cadence: Cadence) -> some View {
        Group {
            switch cadence {
            case .now: Text("New articles join the river and may notify under a rule.")
            case .today: Text("New articles join the river and leave it after the Today period.")
            case .week: Text("New articles collect in the weekly Edition instead of the river.")
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
    }
}

/// One feed's typography override. Each pop-up's Account Default removes only
/// that member; Use Account Typography removes the feed's whole override.
@MainActor struct FeedTypographySection: View {
    let feedID: Int64
    @ObservedObject var settings: ReaderSettingsModel

    private var key: String { String(feedID) }
    private var hasOverride: Bool { settings.draft.feedOverrides[key] != nil }

    var body: some View {
        SwiftUI.Section {
            Picker("Theme", selection: Binding<APIReaderTheme?>(
                get: { settings.draft.feedOverrides[key]?.theme },
                set: { settings.setFeedOverride(feedID: feedID, theme: $0) })) {
                Text("Account Default").tag(APIReaderTheme?.none)
                Divider()
                ForEach(APIReaderTheme.allCases, id: \.self) { Text($0.title).tag(APIReaderTheme?.some($0)) }
            }
            Picker("Font", selection: Binding<APIReaderFont?>(
                get: { settings.draft.feedOverrides[key]?.font },
                set: { settings.setFeedOverride(feedID: feedID, font: $0) })) {
                Text("Account Default").tag(APIReaderFont?.none)
                Divider()
                ForEach(APIReaderFont.allCases, id: \.self) { Text($0.title).tag(APIReaderFont?.some($0)) }
            }
            Picker("Line Length", selection: Binding<Int64?>(
                get: { settings.draft.feedOverrides[key]?.measure },
                set: { settings.setFeedOverride(feedID: feedID, measure: $0) })) {
                Text("Account Default").tag(Int64?.none)
                Divider()
                ForEach(Array(Int64(62)...72), id: \.self) { Text("\($0) characters").tag(Int64?.some($0)) }
            }
        } header: {
            HStack {
                Text("Typography")
                Spacer()
                if hasOverride {
                    Button("Use Account Typography") { settings.resetFeedOverride(feedID: feedID) }
                        .buttonStyle(.link)
                        .font(.caption)
                }
            }
        } footer: {
            SettingsStatus(settings: settings)
        }
        .disabled(!settings.ready)
    }
}

// MARK: - Activity

/// Agent activity grouped by agent. Each row says what happened and what can
/// happen next: Revert, Cancel, or nothing. Selecting a row opens its article.
@MainActor struct ActivityView: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel

    var body: some View {
        if model.activityGroups.isEmpty {
            ContentUnavailableView {
                Label {
                    Text("No Agent Activity")
                } icon: {
                    Image(systemName: Section.activity.icon).foregroundStyle(Tone.plum.color)
                }
            } description: {
                Text("Changes your agents make, and the notifications they send, appear here.")
            }
        } else {
            List(selection: Binding(get: { window.selectedActivityID }, set: { id in
                window.selectActivity(model.activityGroups.flatMap(\.items).first { $0.id == id })
            })) {
                ForEach(model.activityGroups) { group in
                    SwiftUI.Section(group.name) {
                        ForEach(group.items) { item in
                            row(item).tag(item.id)
                        }
                    }
                }
            }
        }
    }

    private func row(_ item: ActivityItem) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            let mark = mark(for: item)
            Image(systemName: mark.symbol)
                .foregroundStyle(mark.tone?.color ?? Color.secondary)
                .frame(width: 16)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline) {
                    Text(item.detail.prefix(1).uppercased() + item.detail.dropFirst())
                    Spacer()
                    if let date = item.createdAt {
                        Text(date.formatted(.relative(presentation: .named)))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                if let title = model.title(for: item.entryID), !title.isEmpty {
                    Text(title)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                switch item.nextAction {
                case "revert":
                    Button("Revert") { model.revert(item) }
                        .controlSize(.small)
                        .accessibilityLabel("Revert \(item.detail) for \(articleName(item))")
                        .disabled(model.activityActionPending)
                case "cancel":
                    HStack {
                        Text("Held notification").font(.caption).foregroundStyle(.secondary)
                        Button("Cancel") { model.cancelNotification(item) }
                            .controlSize(.small)
                            .accessibilityLabel("Cancel notification for \(articleName(item))")
                            .disabled(model.activityActionPending)
                    }
                default:
                    if !item.reason.isEmpty {
                        Text(item.reason)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .padding(.vertical, 2)
        .contextMenu {
            Group {
                if item.nextAction == "revert" {
                    Button("Revert", systemImage: "arrow.uturn.backward") { model.revert(item) }.disabled(model.activityActionPending)
                }
                if item.nextAction == "cancel" {
                    Button("Cancel Notification", systemImage: "bell.slash") { model.cancelNotification(item) }.disabled(model.activityActionPending)
                }
            }
            .labelStyle(.titleAndIcon)
        }
    }

    /// The symbol that stands for an action and the tone it is drawn in,
    /// decided together so the two never drift apart. Actions without a tone
    /// keep the secondary label colour.
    private func mark(for item: ActivityItem) -> (symbol: String, tone: Tone?) {
        switch item.action {
        case "state.updated": return ("checkmark.circle", .green)
        case "annotation.updated", "annotation.deleted", "annotation.reverted": return ("text.bubble", .purple)
        case "position.updated": return ("bookmark", .plum)
        case "notification.created", "notification.cancelled": return ("bell", .orange)
        default: return ("circle.dashed", nil)
        }
    }

    private func articleName(_ item: ActivityItem) -> String {
        if let title = model.title(for: item.entryID), !title.isEmpty { return title }
        if let id = item.entryID { return "article \(id)" }
        return "activity \(item.id)"
    }
}

extension APIReaderTheme {
    var title: String {
        switch self {
        case .system: return "Match System"
        case .light: return "Light"
        case .dark: return "Dark"
        case .black: return "Black"
        case .eink: return "E-Ink"
        }
    }
}

extension APIReaderFont {
    var title: String {
        switch self {
        case .serif: return "Serif"
        case .sans: return "Sans Serif"
        case .dyslexic: return "Dyslexia-Friendly"
        }
    }
}

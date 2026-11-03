import Foundation
import SwiftUI

// Agent briefs are plain text: never rendered as HTML or Markdown, always
// attributed to the agent's name.
struct BriefsView: View {
    let briefs: [BriefItem]
    let names: [Int64: String]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                ForEach(briefs) { brief in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(names[brief.principalID] ?? "Agent #\(brief.principalID) (name unavailable)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Text(brief.text)
                            .font(.callout)
                            .textSelection(.enabled)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .frame(maxHeight: 180)
        .background(.regularMaterial)
    }
}

struct EditionListView: View {
    let groups: [EditionGroup]
    @Binding var selection: Int64?

    var body: some View {
        if groups.isEmpty {
            ContentUnavailableView("Nothing in this week's edition", systemImage: "newspaper")
        } else {
            EntryTable(groups: groups, selection: $selection)
        }
    }
}

struct FeedsView: View {
    @EnvironmentObject private var model: AppModel
    @State private var removing: FeedInfo?

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                TextField("Feed URL", text: $model.subscribeURL)
                    .textFieldStyle(.roundedBorder)
                    .onSubmit { model.subscribe() }
                Picker("Cadence", selection: $model.subscribeCadence) {
                    Text("Now").tag("now")
                    Text("Today").tag("today")
                    Text("This week").tag("week")
                }
                .labelsHidden()
                .frame(width: 110)
                Button("Subscribe") { model.subscribe() }
                    .disabled(model.subscribeURL.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            .padding(10)
            Divider()
            if model.feeds.isEmpty {
                ContentUnavailableView("No subscriptions", systemImage: "dot.radiowaves.left.and.right",
                                       description: Text("Paste a feed URL above."))
            } else {
                List(model.feeds) { feed in
                    HStack(alignment: .firstTextBaseline) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(feed.title.isEmpty ? feed.url : feed.title).lineLimit(1)
                            Text(feed.url)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                                .textSelection(.enabled)
                            if let issue = feed.issue {
                                Text(issue).font(.caption).foregroundStyle(.red)
                            }
                        }
                        Spacer()
                        Picker("Cadence", selection: Binding(
                            get: { feed.cadence },
                            set: { model.setCadence(feed.id, $0) })) {
                            Text("Now").tag("now")
                            Text("Today").tag("today")
                            Text("This week").tag("week")
                        }
                        .labelsHidden()
                        .frame(width: 105)
                        Button("Unsubscribe") { removing = feed }
                    }
                }
            }
        }
        .confirmationDialog(
            "Unsubscribe from \(removing?.title.isEmpty == false ? removing!.title : (removing?.url ?? "this feed"))?",
            isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
            titleVisibility: .visible) {
            Button("Unsubscribe", role: .destructive) {
                if let removing { model.unsubscribe(removing.id) }
                removing = nil
            }
            Button("Cancel", role: .cancel) { removing = nil }
        }
    }
}

struct ActivityView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        if model.activityGroups.isEmpty {
            ContentUnavailableView("No agent activity", systemImage: "clock.arrow.circlepath",
                                   description: Text("Automated changes and notifications appear here."))
        } else {
            List {
                ForEach(model.activityGroups) { group in
                    SwiftUI.Section(header: Text(group.name)) {
                        ForEach(group.items) { item in
                            VStack(alignment: .leading, spacing: 3) {
                                HStack(alignment: .firstTextBaseline) {
                                    Text(item.detail)
                                    Spacer()
                                    if let date = item.createdAt {
                                        Text(date, style: .relative)
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
                                case "cancel":
                                    Text("Held notification. Can be cancelled")
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                default:
                                    if !item.reason.isEmpty {
                                        Text(item.reason)
                                            .font(.caption)
                                            .foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

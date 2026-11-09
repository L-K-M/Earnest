import Foundation
import EarnestKit
#if canImport(SwiftUI)
import SwiftUI
#endif

extension AppModel {
    /// The server rejects kept imports over 64 MiB, so the open panel and the
    /// import share that bound.
    static let keptLimit = 64 << 20
}

/// One window's kept-import result, including the file to retry with when the
/// server skipped feeds you do not follow.
struct KeptImportReport: Identifiable, Equatable {
    let id = UUID()
    let title: String
    let detail: String
    let retry: KeptImportRetry?
}

struct KeptImportRetry: Equatable {
    let name: String
    let data: Data
}

/// Runs a starred-articles import for one window and reports the outcome.
/// AppModel's task registry is private, so this owns its one task; the
/// session check keeps a stale result from reaching a new account.
@MainActor final class StarredImportCoordinator {
    static let shared = StarredImportCoordinator()
    private init() {}

    private var task: Task<Void, Never>?

    func importStarred(model: AppModel, document: Data, name: String, subscribeMissing: Bool,
                       reported: @escaping (KeptImportReport) -> Void) {
        guard let api = model.api, model.loggedIn else { return }
        guard task == nil else {
            model.error = "Wait for the current starred import to finish, then import “\(name)” again."
            return
        }
        guard model.transfer == nil else {
            model.error = "Wait for the current subscription transfer to finish, then import “\(name)” again."
            return
        }
        guard document.count <= AppModel.keptLimit else {
            model.error = "“\(name)” is too large to be a starred-articles export."
            return
        }
        let session = model.session
        task = Task { [weak model] in
            defer { self.task = nil }
            do {
                let summary = try await api.importKept(document, subscribeMissing: subscribeMissing)
                guard let model, model.session == session, model.loggedIn, !Task.isCancelled else { return }
                await model.refreshFeeds()
                model.rebuildPresentation()
                model.scheduleSync()
                // Finish before reporting, so the report's retry is never
                // refused as an import still in progress.
                self.task = nil
                reported(Self.report(for: summary, name: name, data: document))
            } catch is CancellationError {
            } catch {
                guard let model, model.session == session else { return }
                model.report(error)
            }
        }
    }

    /// The report wording mirrors the web client: the Kept count first, then
    /// what happened to the rest, then the retry offer when feeds were skipped.
    static func report(for summary: KeptImportSummary, name: String, data: Data) -> KeptImportReport {
        var parts: [String] = []
        let kept = summary.added + summary.existing
        parts.append("\(kept) \(kept == 1 ? "article" : "articles") in Kept: \(summary.added) added from the export, \(summary.existing) already in Earnest.")
        if summary.subscribed > 0 {
            parts.append("Subscribed to \(summary.subscribed) \(summary.subscribed == 1 ? "feed" : "feeds"). Their first fetches run in the background.")
        }
        if summary.labelled > 0 {
            parts.append("Added the export's labels to \(summary.labelled) \(summary.labelled == 1 ? "article" : "articles").")
        }
        if summary.unkept > 0 {
            parts.append("\(summary.unkept) left out because you removed \(summary.unkept == 1 ? "it" : "them") from Kept.")
        }
        if summary.gone > 0 {
            parts.append("\(summary.gone) no longer available in Earnest.")
        }
        if !summary.skipped.isEmpty {
            let total = summary.skipped.reduce(0) { $0 + $1.count }
            var line = "\(total) skipped because you don't follow their feed:"
            for feed in summary.skipped {
                line += "\n\(feed.feedURL.isEmpty ? "Unknown feed" : feed.feedURL) (\(feed.count))"
            }
            parts.append(line)
        }
        if !summary.failures.isEmpty {
            parts.append("\(summary.failures.count) could not be imported: \(summary.failures.joined(separator: "; "))")
        }
        let title = summary.failures.isEmpty ? "Imported “\(name)”" : "Imported “\(name)” with problems"
        let retry = summary.skipped.isEmpty ? nil : KeptImportRetry(name: name, data: data)
        return KeptImportReport(title: title, detail: parts.joined(separator: " "), retry: retry)
    }
}

/// The confirmation before an import, offering to subscribe to missing feeds.
struct StarredImportConfirmationSheet: View {
    let name: String
    @Binding var subscribeMissing: Bool
    let onImport: () -> Void
    let onCancel: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Import Starred Articles?")
                .font(.headline)
            Text("Earnest will keep the starred articles in “\(name)”. Articles already in Earnest stay as they are.")
                .fixedSize(horizontal: false, vertical: true)
            Toggle("Also subscribe to feeds you don't follow", isOn: $subscribeMissing)
            HStack {
                Spacer()
                Button("Cancel", action: onCancel)
                Button("Import", action: onImport)
                    .keyboardShortcut(.defaultAction)
            }
        }
        .padding(20)
        .frame(width: 440)
    }
}

extension Notification.Name {
    static let earnestImportStarredArticles = Notification.Name("EarnestImportStarredArticles")
}

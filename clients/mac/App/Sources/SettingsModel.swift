import Foundation
import EarnestKit

/// One window's draft over the shared account reader settings. Loading copies
/// the account values without mutating them; saving is explicit. A failed save
/// freezes its full request body and operation ID so a retry is idempotent;
/// editing the draft discards the frozen request. A newer account revision
/// rejects a stale draft instead of overwriting settings changed elsewhere.
@MainActor final class ReaderSettingsModel: ObservableObject {
    @Published var draft: APIReaderSettings { didSet { confirmed = false } }
    @Published private(set) var baseline: APIReaderSettings
    @Published private(set) var busy = false
    @Published private(set) var error: String?
    @Published private(set) var confirmed = false
    @Published private(set) var conflict = false

    private let app: AppModel
    private var pending: PendingSave?
    private var task: Task<Void, Never>?

    private struct PendingSave: Equatable {
        let body: APIReaderSettings
        let opID: String
    }

    init(app: AppModel) {
        self.app = app
        draft = app.readerSettings
        baseline = app.readerSettings
    }

    var isDirty: Bool { draft != baseline }

    /// Adopt account settings that arrived from sync or another window. A
    /// dirty draft is never overwritten: newer settings are flagged as a
    /// conflict so the local edits survive until the user reloads.
    func update(from settings: APIReaderSettings) {
        guard settings != baseline else { return }
        if isDirty {
            conflict = settings.rev > baseline.rev
            return
        }
        draft = settings
        baseline = settings
        pending = nil
        error = nil
        confirmed = false
        conflict = false
    }

    /// Explicit reload: copy the account settings into the draft. Local edits
    /// are discarded only because the user asked for it.
    func load() {
        task?.cancel()
        draft = app.readerSettings
        baseline = app.readerSettings
        pending = nil
        busy = false
        error = nil
        confirmed = false
        conflict = false
    }

    func save() {
        guard !busy, let api = app.api, app.loggedIn else { return }
        // The draft's base is older than the account's current revision:
        // saving would silently overwrite newer settings, so reject locally.
        guard app.readerSettings.rev <= baseline.rev else {
            conflict = true
            error = "Settings changed elsewhere. Reload before saving."
            return
        }
        if pending == nil || pending?.body != draft {
            pending = PendingSave(body: draft, opID: "mac.settings.\(UUID().uuidString.lowercased())")
        }
        guard let pending else { return }
        let expectedSession = app.session
        busy = true
        error = nil
        confirmed = false
        task = Task { [weak self] in
            defer { self?.busy = false }
            do {
                let saved = try await api.updateReaderSettings(pending.body, opID: pending.opID)
                try Task.checkCancellation()
                guard let self, self.app.session == expectedSession, !Task.isCancelled else { return }
                self.draft = saved
                self.baseline = saved
                self.pending = nil
                self.conflict = false
                self.confirmed = true
                self.app.adoptReaderSettings(saved)
            } catch is CancellationError {
            } catch {
                guard let self, self.app.session == expectedSession, !Task.isCancelled else { return }
                self.error = ErrorPresentation.message(error)
            }
        }
    }

    // MARK: - Per-feed typography overrides

    func setFeedOverride(feedID: Int64, theme: APIReaderTheme?) {
        var override = draft.feedOverrides[String(feedID)] ?? APIReaderOverride()
        override.theme = theme
        assign(override, feedID: feedID)
    }

    func setFeedOverride(feedID: Int64, measure: Int64?) {
        var override = draft.feedOverrides[String(feedID)] ?? APIReaderOverride()
        override.measure = measure
        assign(override, feedID: feedID)
    }

    func setFeedOverride(feedID: Int64, font: APIReaderFont?) {
        var override = draft.feedOverrides[String(feedID)] ?? APIReaderOverride()
        override.font = font
        assign(override, feedID: feedID)
    }

    /// Reset one feed to inherit the account-wide typography. Overrides for
    /// every other feed are left untouched.
    func resetFeedOverride(feedID: Int64) {
        draft.feedOverrides.removeValue(forKey: String(feedID))
    }

    /// The shared session ended: a frozen request must never replay under a
    /// different account or server.
    func endSession() {
        task?.cancel()
        task = nil
        pending = nil
        busy = false
        error = nil
        confirmed = false
        conflict = false
        draft = app.readerSettings
        baseline = app.readerSettings
    }

    private func assign(_ override: APIReaderOverride, feedID: Int64) {
        let key = String(feedID)
        if override.theme == nil, override.measure == nil, override.font == nil {
            draft.feedOverrides.removeValue(forKey: key)
        } else {
            draft.feedOverrides[key] = override
        }
    }
}

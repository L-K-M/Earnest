import Foundation
import EarnestKit

/// One window's draft over the shared account reader settings. Loading copies
/// the account values without mutating them; saving is explicit. A failed save
/// freezes its full request body and operation ID so a retry is idempotent;
/// editing the draft discards the frozen request. A newer account revision
/// rejects a stale draft instead of overwriting settings changed elsewhere.
@MainActor final class ReaderSettingsModel: ObservableObject {
    @Published var draft: APIReaderSettings {
        didSet {
            confirmed = false
            if draft != oldValue { pending = nil }
        }
    }
    @Published private(set) var baseline: APIReaderSettings
    @Published private(set) var busy = false
    @Published private(set) var error: String?
    @Published private(set) var confirmed = false
    @Published private(set) var conflict = false
    @Published private(set) var ready = false

    private let app: AppModel
    private var pending: PendingSave?
    private var task: Task<Void, Never>?
    private var scope: ReaderPreferencesScope?
    private var requestID = 0

    private struct PendingSave: Equatable {
        let body: APIReaderSettings
        let opID: String
    }

    init(app: AppModel) {
        self.app = app
        draft = app.readerSettings
        baseline = app.readerSettings
        scope = app.readerPreferencesScope()
        ready = scope != nil
    }

    var isDirty: Bool { draft != baseline }

    /// Adopt account settings that arrived from sync or another window. A
    /// dirty draft is never overwritten: newer settings are flagged as a
    /// conflict so the local edits survive until the user reloads.
    func update(from settings: APIReaderSettings) {
        if synchronizeScope() { return }
        guard settings.rev >= baseline.rev, settings != baseline else { return }
        if isDirty {
            conflict = true
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
        resetDraft(scope: app.readerPreferencesScope())
    }

    func save() {
        guard !busy else { return }
        if synchronizeScope() {
            error = "Account data changed. Review the current settings before saving."
            return
        }
        guard let api = app.api, app.loggedIn, let scope else {
            error = "Sync your account before saving settings."
            return
        }
        guard !conflict, app.readerSettings.rev <= baseline.rev,
              app.readerSettings.rev != baseline.rev || app.readerSettings == baseline else {
            conflict = true
            error = "Settings changed elsewhere. Reload before saving."
            return
        }
        if pending == nil || pending?.body != draft {
            pending = PendingSave(body: draft, opID: "mac.settings.\(UUID().uuidString.lowercased())")
        }
        guard let pending else { return }
        requestID &+= 1
        let requestID = self.requestID
        busy = true
        error = nil
        confirmed = false
        task = Task { [weak self] in
            guard let self else { return }
            defer {
                if self.requestID == requestID {
                    self.busy = false
                    self.task = nil
                }
            }
            do {
                guard self.current(requestID, scope: scope) else { return }
                let saved = try await api.updateReaderSettings(pending.body, opID: pending.opID)
                try Task.checkCancellation()
                guard self.current(requestID, scope: scope) else { return }
                let latestDraft = self.draft
                let edited = latestDraft != pending.body
                let effective: APIReaderSettings?
                do {
                    effective = try self.app.adoptReaderSettings(saved, scope: scope)
                } catch {
                    guard self.current(requestID, scope: scope) else { return }
                    self.error = "Saved on the server, but could not confirm offline settings. Retry to confirm. \(ErrorPresentation.message(error))"
                    return
                }
                guard let effective, self.current(requestID, scope: scope) else { return }
                self.pending = nil
                if effective != saved {
                    if edited {
                        self.draft = latestDraft
                        self.conflict = true
                    } else {
                        self.draft = effective
                        self.baseline = effective
                        self.conflict = false
                    }
                    self.error = "Newer settings are active. Review them before saving again."
                    self.confirmed = false
                    return
                }
                self.baseline = effective
                self.draft = edited ? APIReaderSettings(
                    readingMode: latestDraft.readingMode, todayDecayDays: latestDraft.todayDecayDays,
                    singleKeyShortcuts: latestDraft.singleKeyShortcuts, theme: latestDraft.theme,
                    measure: latestDraft.measure, font: latestDraft.font,
                    feedOverrides: latestDraft.feedOverrides, rev: effective.rev
                ) : effective
                self.conflict = false
                self.confirmed = !edited
            } catch is CancellationError {
            } catch {
                guard self.current(requestID, scope: scope) else { return }
                if ErrorPresentation.isUnauthorized(error) {
                    self.app.report(error)
                    self.endSession()
                    return
                }
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
        resetDraft(scope: app.readerPreferencesScope())
    }

    private func current(_ requestID: Int, scope: ReaderPreferencesScope) -> Bool {
        self.requestID == requestID && !Task.isCancelled && app.ownsReaderPreferences(scope)
    }

    @discardableResult
    private func synchronizeScope() -> Bool {
        let current = app.readerPreferencesScope()
        guard scope != current else { return false }
        resetDraft(scope: current)
        return true
    }

    private func resetDraft(scope: ReaderPreferencesScope?) {
        requestID &+= 1
        task?.cancel()
        task = nil
        pending = nil
        busy = false
        error = nil
        confirmed = false
        conflict = false
        self.scope = scope
        ready = scope != nil
        let settings = scope == nil ? APIReaderSettings() : app.readerSettings
        draft = settings
        baseline = settings
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

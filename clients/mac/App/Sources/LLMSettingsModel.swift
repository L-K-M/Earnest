import Foundation
import EarnestKit
import SwiftUI

/// The server-wide LLM configuration is bound to one signed-in identity and
/// server session on this client. A sign-out, a server change or a different
/// account ends the session, which clears the draft and any typed key.
struct LLMSettingsScope: Equatable {
    let session: Int
    let principalID: Int64
}

/// One provider as edited in Settings. `apiKey` is a write-only replacement
/// draft: it starts empty, is never pre-filled from the server, and is cleared
/// after a successful save, a reload, or a session change.
struct LLMProviderDraft: Identifiable, Equatable {
    let id = UUID()
    var name = ""
    var kind: APILLMProviderKind = .openai
    var baseURL = ""
    var monthlyLimitUSD = "0"
    var timeoutSeconds: Int64 = 60
    var tokenField: APILLMTokenField = .maxCompletionTokens
    var hasAPIKey = false
    var apiKey = ""
    var clearAPIKey = false

    /// Identity is for SwiftUI only; two drafts loaded from the same server
    /// state must compare equal, so `isDirty` sees real edits.
    static func == (lhs: LLMProviderDraft, rhs: LLMProviderDraft) -> Bool {
        lhs.name == rhs.name && lhs.kind == rhs.kind && lhs.baseURL == rhs.baseURL
            && lhs.monthlyLimitUSD == rhs.monthlyLimitUSD && lhs.timeoutSeconds == rhs.timeoutSeconds
            && lhs.tokenField == rhs.tokenField && lhs.hasAPIKey == rhs.hasAPIKey
            && lhs.apiKey == rhs.apiKey && lhs.clearAPIKey == rhs.clearAPIKey
    }

    init() {}

    init(settings: APILLMProviderSettings) {
        name = settings.name
        kind = settings.kind
        baseURL = settings.baseURL
        monthlyLimitUSD = settings.monthlyLimitUSD
        timeoutSeconds = settings.timeoutSeconds
        tokenField = settings.tokenField
        hasAPIKey = settings.hasAPIKey
    }
}

/// One task route as edited. Provider and model are configured together;
/// clearing the provider disables the task.
struct LLMTaskDraft: Identifiable, Equatable {
    let id: APILLMTask
    var task: APILLMTask
    var provider = ""
    var model = ""

    init(task: APILLMTask, provider: String = "", model: String = "") {
        self.id = task
        self.task = task
        self.provider = provider
        self.model = model
    }
}

/// A model's prices and embedding geometry as edited. Prices stay decimal
/// text end to end, so an exact configured price never drifts.
struct LLMModelDraft: Identifiable, Equatable {
    let id = UUID()
    var name = ""
    var inputPer1M = "0"
    var outputPer1M = "0"
    var embeddingPer1M = "0"
    var dimensions: Int64 = 0
    var truncatable = false

    static func == (lhs: LLMModelDraft, rhs: LLMModelDraft) -> Bool {
        lhs.name == rhs.name && lhs.inputPer1M == rhs.inputPer1M && lhs.outputPer1M == rhs.outputPer1M
            && lhs.embeddingPer1M == rhs.embeddingPer1M && lhs.dimensions == rhs.dimensions
            && lhs.truncatable == rhs.truncatable
    }

    init() {}

    init(settings: APILLMModelSettings) {
        name = settings.name
        inputPer1M = settings.inputPer1M
        outputPer1M = settings.outputPer1M
        embeddingPer1M = settings.embeddingPer1M
        dimensions = settings.dimensions
        truncatable = settings.truncatable
    }
}

/// The whole edited configuration. A fresh draft enumerates every task as
/// disabled, matching the server's own enumeration.
struct LLMSettingsDraft: Equatable {
    var providers: [LLMProviderDraft] = []
    var tasks: [LLMTaskDraft] = APILLMTask.allCases.map { LLMTaskDraft(task: $0) }
    var models: [LLMModelDraft] = []

    init() {}

    init(settings: APILLMSettings) {
        providers = settings.providers.map(LLMProviderDraft.init(settings:))
        tasks = APILLMTask.allCases.map { task in
            let route = settings.tasks.first { $0.task == task }
            return LLMTaskDraft(task: task, provider: route?.provider ?? "", model: route?.model ?? "")
        }
        models = settings.models.map(LLMModelDraft.init(settings:))
    }

    /// An empty replacement key keeps the stored one; a cleared key sends the
    /// explicit removal instead.
    func input(revision: Int64, opID: String) -> APILLMSettingsInput {
        APILLMSettingsInput(
            revision: revision,
            opID: opID,
            providers: providers.map { provider in
                APILLMProviderInput(
                    name: provider.name,
                    kind: provider.kind,
                    baseURL: provider.baseURL,
                    apiKey: provider.clearAPIKey ? nil : (provider.apiKey.isEmpty ? nil : provider.apiKey),
                    clearAPIKey: provider.clearAPIKey,
                    monthlyLimitUSD: provider.monthlyLimitUSD,
                    timeoutSeconds: provider.timeoutSeconds,
                    tokenField: provider.tokenField
                )
            },
            tasks: tasks.map { APILLMTaskSettings(task: $0.task, provider: $0.provider, model: $0.model) },
            models: models.map { model in
                APILLMModelSettings(name: model.name, inputPer1M: model.inputPer1M,
                                    outputPer1M: model.outputPer1M, embeddingPer1M: model.embeddingPer1M,
                                    dimensions: model.dimensions, truncatable: model.truncatable)
            }
        )
    }

    /// The redacted server shape of this draft, used for the destination-change
    /// check against the configuration the revision was loaded from.
    func serverSettings(revision: Int64) -> APILLMSettings {
        APILLMSettings(
            revision: revision,
            providers: providers.map {
                APILLMProviderSettings(name: $0.name, kind: $0.kind, baseURL: $0.baseURL,
                                       hasAPIKey: $0.hasAPIKey, monthlyLimitUSD: $0.monthlyLimitUSD,
                                       timeoutSeconds: $0.timeoutSeconds, tokenField: $0.tokenField)
            },
            tasks: tasks.map { APILLMTaskSettings(task: $0.task, provider: $0.provider, model: $0.model) },
            models: models.map {
                APILLMModelSettings(name: $0.name, inputPer1M: $0.inputPer1M, outputPer1M: $0.outputPer1M,
                                    embeddingPer1M: $0.embeddingPer1M, dimensions: $0.dimensions,
                                    truncatable: $0.truncatable)
            }
        )
    }
}

/// One window's draft over the server-wide LLM services configuration. There
/// is no sync or cache: loading and saving are explicit, a failed or
/// conflicting save never claims success, and a 409 preserves the draft until
/// the user reloads.
@MainActor final class LLMSettingsModel: ObservableObject {
    @Published var draft: LLMSettingsDraft = LLMSettingsDraft() {
        didSet {
            saved = false
            // Editing ends a frozen request; the next save mints a new op ID.
            pending = nil
        }
    }
    @Published private(set) var baseline: LLMSettingsDraft?
    @Published private(set) var revision: Int64?
    @Published private(set) var loaded = false
    @Published private(set) var busy = false
    @Published private(set) var error: String?
    @Published private(set) var saved = false
    @Published private(set) var conflict = false
    @Published private(set) var ready = false

    private let app: AppModel
    private var pending: PendingSave?
    private var task: Task<Void, Never>?
    private var requestID = 0
    private var scope: LLMSettingsScope?

    private struct PendingSave: Equatable {
        let body: APILLMSettingsInput
        let opID: String
    }

    init(app: AppModel) {
        self.app = app
        scope = app.llmSettingsScope()
        ready = scope != nil
        app.register(llmSettings: self)
    }

    var isDirty: Bool {
        guard let baseline else { return false }
        return draft != baseline
    }

    /// A failed save keeps its exact body and operation ID for Try Again.
    var canRetry: Bool { pending != nil }

    // MARK: - Loading

    /// Explicitly fetch the server configuration. Nothing is requested until
    /// the user asks, and any typed key draft is discarded.
    func load() {
        guard !busy else { return }
        if synchronizeScope() {
            error = "Account data changed. Reload the LLM settings."
            return
        }
        guard let api = app.api, let scope else {
            error = "Sign in to load the LLM settings."
            return
        }
        requestID &+= 1
        let requestID = self.requestID
        busy = true
        error = nil
        saved = false
        conflict = false
        task = Task { [weak self] in
            guard let self else { return }
            defer {
                if self.requestID == requestID {
                    self.busy = false
                    self.task = nil
                }
            }
            do {
                let settings = try await api.llmSettings()
                try Task.checkCancellation()
                guard self.current(requestID, scope: scope) else { return }
                self.adopt(settings)
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

    // MARK: - Saving

    /// Save the whole configuration. The draft must match the loaded revision;
    /// a conflict preserves the draft and requires Reload.
    func save() {
        guard !busy else { return }
        if synchronizeScope() {
            error = "Account data changed. Reload the LLM settings."
            return
        }
        guard let api = app.api, let scope else {
            error = "Sign in to save the LLM settings."
            return
        }
        guard !conflict else {
            error = "These settings changed elsewhere. Reload before saving."
            return
        }
        guard let revision else {
            error = "Load the LLM settings before saving."
            return
        }
        var candidate = draft.input(revision: revision, opID: pending?.opID ?? "")
        if pending == nil || pending?.body != candidate {
            // A changed draft mints a new operation ID; an unchanged one reuses
            // the frozen request so a retry is idempotent.
            let opID = "mac.llm-settings.\(UUID().uuidString.lowercased())"
            candidate = draft.input(revision: revision, opID: opID)
            pending = PendingSave(body: candidate, opID: opID)
        }
        guard let pending else { return }
        let previous = baseline?.serverSettings(revision: revision)
        let sentDraft = draft
        requestID &+= 1
        let requestID = self.requestID
        busy = true
        error = nil
        saved = false
        task = Task { [weak self] in
            guard let self else { return }
            defer {
                if self.requestID == requestID {
                    self.busy = false
                    self.task = nil
                }
            }
            do {
                let settings = try await api.updateLLMSettings(pending.body, previous: previous)
                try Task.checkCancellation()
                guard self.current(requestID, scope: scope) else { return }
                let latest = self.draft
                self.adopt(settings)
                if latest != sentDraft {
                    // The user kept editing while the save was in flight. Keep
                    // those edits and flag the conflict instead of discarding
                    // them under the server's newer state.
                    self.draft = latest
                    self.conflict = true
                    self.error = "You edited these settings while the save was in flight. Review the saved configuration and reload."
                } else {
                    self.saved = true
                }
            } catch is CancellationError {
            } catch {
                guard self.current(requestID, scope: scope) else { return }
                if ErrorPresentation.isUnauthorized(error) {
                    self.app.report(error)
                    self.endSession()
                    return
                }
                if ErrorPresentation.isConflict(error) {
                    self.conflict = true
                    self.pending = nil
                    self.error = "These settings changed elsewhere. Reload to see the current configuration before saving."
                    return
                }
                self.error = ErrorPresentation.message(error)
            }
        }
    }

    // MARK: - Editing

    func addProvider() {
        var provider = LLMProviderDraft()
        provider.baseURL = provider.kind.defaultBaseURL
        draft.providers.append(provider)
    }

    /// Changing a provider's dialect offers that dialect's default base URL,
    /// unless the field already holds a custom destination.
    func setProviderKind(_ kind: APILLMProviderKind, id: UUID) {
        guard let index = draft.providers.firstIndex(where: { $0.id == id }) else { return }
        let defaults = APILLMProviderKind.allCases.map(\.defaultBaseURL)
        if draft.providers[index].baseURL.isEmpty || defaults.contains(draft.providers[index].baseURL) {
            draft.providers[index].baseURL = kind.defaultBaseURL
        }
        draft.providers[index].kind = kind
    }

    func removeProvider(id: UUID) {
        draft.providers.removeAll { $0.id == id }
        // A removed provider takes its routes with it.
        for index in draft.tasks.indices where !draft.providers.contains(where: { $0.name == draft.tasks[index].provider }) {
            draft.tasks[index].provider = ""
            draft.tasks[index].model = ""
        }
    }

    func addModel() {
        draft.models.append(LLMModelDraft())
    }

    func removeModel(id: UUID) {
        let name = draft.models.first { $0.id == id }?.name
        draft.models.removeAll { $0.id == id }
        // A removed model disables the routes that used it.
        for index in draft.tasks.indices where draft.tasks[index].model == name {
            draft.tasks[index].provider = ""
            draft.tasks[index].model = ""
        }
    }

    /// The shared session ended: the draft and any typed key must never
    /// survive into another account or server.
    func endSession() {
        scope = nil
        ready = false
        reset()
    }

    /// Bind to the current account after a session is established. A sign-in
    /// enables the pane; any other scope change discards the draft, including
    /// a typed key.
    func refreshScope() {
        let current = app.llmSettingsScope()
        guard scope != current else { return }
        scope = current
        reset()
        ready = current != nil
    }

    // MARK: - Scope and state

    private func current(_ requestID: Int, scope: LLMSettingsScope) -> Bool {
        self.requestID == requestID && !Task.isCancelled && app.ownsLLMSettings(scope)
    }

    @discardableResult
    private func synchronizeScope() -> Bool {
        let current = app.llmSettingsScope()
        guard scope != current else { return false }
        scope = current
        reset()
        ready = current != nil
        return true
    }

    private func reset() {
        requestID &+= 1
        task?.cancel()
        task = nil
        pending = nil
        busy = false
        error = nil
        saved = false
        conflict = false
        loaded = false
        revision = nil
        baseline = nil
        draft = LLMSettingsDraft()
    }

    private func adopt(_ settings: APILLMSettings) {
        // Rebuilding from the response discards every key draft.
        draft = LLMSettingsDraft(settings: settings)
        baseline = LLMSettingsDraft(settings: settings)
        revision = settings.revision
        loaded = true
        pending = nil
        error = nil
        conflict = false
        saved = false
    }
}

extension APILLMProviderKind {
    /// The server's own default base URL for a new provider.
    var defaultBaseURL: String {
        switch self {
        case .openai: return "https://api.openai.com/v1"
        case .anthropic: return "https://api.anthropic.com"
        case .gemini: return "https://generativelanguage.googleapis.com/v1beta"
        }
    }
}

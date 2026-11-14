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
    var routedName = ""
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
        routedName = settings.name
        kind = settings.kind
        baseURL = settings.baseURL
        monthlyLimitUSD = settings.monthlyLimitUSD
        timeoutSeconds = settings.timeoutSeconds
        tokenField = settings.tokenField
        hasAPIKey = settings.hasAPIKey
    }

    /// The wire form of this draft: a blank replacement key keeps the stored
    /// one and a flagged removal sends `clearAPIKey` instead. Save and model
    /// discovery must send the identical provider, so both go through here.
    var providerInput: APILLMProviderInput {
        APILLMProviderInput(
            name: name,
            kind: kind,
            baseURL: baseURL,
            apiKey: clearAPIKey ? nil
                : (apiKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : apiKey),
            clearAPIKey: clearAPIKey,
            monthlyLimitUSD: monthlyLimitUSD,
            timeoutSeconds: timeoutSeconds,
            tokenField: tokenField
        )
    }
}

/// What a provider's key badge reports. The badge describes what the next
/// Save would do; the saved flag resolved against the unsaved replacement
/// draft and a pending removal; not just what the server already holds.
enum LLMKeyStatus: Equatable {
    case saved
    case entered
    case removal
    case none

    var title: String {
        switch self {
        case .saved: return "API key saved"
        case .entered: return "API key entered · unsaved"
        case .removal: return "Key removal pending"
        case .none: return "No API key"
        }
    }
}

extension LLMProviderDraft {
    /// A pending removal wins over an inert draft key: `providerInput` drops
    /// the typed text while `clearAPIKey` is set, so Save removes the stored
    /// key and discards the draft. Otherwise a non-blank draft is an unsent
    /// replacement, and only then does the saved flag report "saved".
    var keyStatus: LLMKeyStatus {
        if clearAPIKey { return .removal }
        if !apiKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return .entered }
        return hasAPIKey ? .saved : .none
    }
}

/// One task route as edited. A task is off, follows the text or embedding
/// default, or carries its own advanced provider and model. Provider and
/// model are configured together; clearing the provider disables a custom
/// route.
struct LLMTaskDraft: Identifiable, Equatable {
    let id: APILLMTask
    var task: APILLMTask
    var mode: APILLMTaskMode
    var provider = ""
    var model = ""

    /// The legacy form: a nonempty route is custom, an empty one is off.
    init(task: APILLMTask, provider: String = "", model: String = "") {
        self.id = task
        self.task = task
        self.mode = provider.isEmpty && model.isEmpty ? .off : .custom
        self.provider = provider
        self.model = model
    }

    init(task: APILLMTask, mode: APILLMTaskMode, provider: String, model: String) {
        self.id = task
        self.task = task
        self.mode = mode
        self.provider = provider
        self.model = model
    }

    var isEnabled: Bool { mode != .off }
}

/// A model's prices and embedding geometry as edited. Prices stay decimal
/// text end to end, so an exact configured price never drifts. An empty
/// provider is the legacy global entry; a nonempty one scopes the model to a
/// provider, and a scoped entry wins over a global one with the same name.
struct LLMModelDraft: Identifiable, Equatable {
    let id = UUID()
    var name = ""
    var provider = ""
    var supportsCustomDimensions = false
    var inputPer1M = "0"
    var outputPer1M = "0"
    var embeddingPer1M = "0"
    var dimensions: Int64 = 0
    var truncatable = false

    static func == (lhs: LLMModelDraft, rhs: LLMModelDraft) -> Bool {
        lhs.name == rhs.name && lhs.provider == rhs.provider
            && lhs.supportsCustomDimensions == rhs.supportsCustomDimensions
            && lhs.inputPer1M == rhs.inputPer1M && lhs.outputPer1M == rhs.outputPer1M
            && lhs.embeddingPer1M == rhs.embeddingPer1M && lhs.dimensions == rhs.dimensions
            && lhs.truncatable == rhs.truncatable
    }

    init() {}

    init(settings: APILLMModelSettings) {
        name = settings.name
        provider = settings.provider
        supportsCustomDimensions = settings.supportsCustomDimensions
        inputPer1M = settings.inputPer1M
        outputPer1M = settings.outputPer1M
        embeddingPer1M = settings.embeddingPer1M
        dimensions = settings.dimensions
        truncatable = settings.truncatable
    }

    var identity: String { llmModelIdentity(provider, name) }

    /// The wire form used by a model check. Prices must already be explicit.
    var settings: APILLMModelSettings {
        APILLMModelSettings(name: name, provider: provider,
                            supportsCustomDimensions: supportsCustomDimensions,
                            inputPer1M: inputPer1M, outputPer1M: outputPer1M,
                            embeddingPer1M: embeddingPer1M, dimensions: dimensions,
                            truncatable: truncatable)
    }
}

/// The whole edited configuration. A fresh draft enumerates every task as
/// disabled, matching the server's own enumeration.
struct LLMSettingsDraft: Equatable {
    var providers: [LLMProviderDraft] = []
    var tasks: [LLMTaskDraft] = APILLMTask.allCases.map { LLMTaskDraft(task: $0) }
    var models: [LLMModelDraft] = []
    var defaults: APILLMDefaults? = nil

    init() {}

    init(settings: APILLMSettings) {
        providers = settings.providers.map(LLMProviderDraft.init(settings:))
        tasks = APILLMTask.allCases.map { task in
            let route = settings.tasks.first { $0.task == task }
            // A missing mode is the legacy explicit route: custom when a
            // provider and model are set, off otherwise.
            let mode = route?.mode
                ?? ((route?.provider.isEmpty == false && route?.model.isEmpty == false) ? .custom : .off)
            return LLMTaskDraft(task: task, mode: mode,
                                provider: route?.provider ?? "", model: route?.model ?? "")
        }
        models = settings.models.map(LLMModelDraft.init(settings:))
        defaults = settings.defaults
    }

    /// The route a task actually uses. A default task follows the text
    /// default, except embed which follows the embedding default.
    func effectiveRoute(for task: LLMTaskDraft) -> APILLMDefaultRoute {
        switch task.mode {
        case .off:
            return APILLMDefaultRoute()
        case .custom:
            return APILLMDefaultRoute(provider: task.provider, model: task.model)
        case .default:
            guard let defaults else { return APILLMDefaultRoute() }
            return task.task == .embed ? defaults.embedding : defaults.text
        }
    }

    /// A model entry for a provider and name. A provider-scoped entry wins
    /// over a legacy global one.
    func model(provider: String, name: String) -> LLMModelDraft? {
        models.first { $0.provider == provider && $0.name == name }
            ?? models.first { $0.provider.isEmpty && $0.name == name }
    }

    /// An empty replacement key keeps the stored one; a cleared key sends the
    /// explicit removal instead. Default-mode tasks also carry their resolved
    /// provider and model so an old reader sees the effective route.
    func input(revision: Int64, opID: String) -> APILLMSettingsInput {
        APILLMSettingsInput(
            revision: revision,
            opID: opID,
            providers: providers.map(\.providerInput),
            tasks: tasks.map { task in
                let route = effectiveRoute(for: task)
                return APILLMTaskSettings(task: task.task, mode: task.mode,
                                          provider: route.provider, model: route.model)
            },
            models: models.map { model in
                APILLMModelSettings(name: model.name, provider: model.provider,
                                    supportsCustomDimensions: model.supportsCustomDimensions,
                                    inputPer1M: model.inputPer1M,
                                    outputPer1M: model.outputPer1M, embeddingPer1M: model.embeddingPer1M,
                                    dimensions: model.dimensions, truncatable: model.truncatable)
            },
            defaults: defaults.flatMap { $0.isEmpty ? nil : $0 }
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
            tasks: tasks.map { task in
                let route = effectiveRoute(for: task)
                return APILLMTaskSettings(task: task.task, mode: task.mode,
                                          provider: route.provider, model: route.model)
            },
            models: models.map {
                APILLMModelSettings(name: $0.name, provider: $0.provider,
                                    supportsCustomDimensions: $0.supportsCustomDimensions,
                                    inputPer1M: $0.inputPer1M, outputPer1M: $0.outputPer1M,
                                    embeddingPer1M: $0.embeddingPer1M, dimensions: $0.dimensions,
                                    truncatable: $0.truncatable)
            },
            defaults: defaults.flatMap { $0.isEmpty ? nil : $0 }
        )
    }
}

/// The configured-model cap the server enforces, mirrored for imports.
let llmModelLimit = 128

/// A catalogue fetch bound to the exact provider input it was sent with.
/// Results live only in memory: editing the provider, reloading, signing out
/// or changing the server makes them stale, and a reply that no longer
/// matches the draft is discarded rather than shown.
struct LLMDiscoverySession: Equatable {
    enum Phase: Equatable {
        case fetching
        case loaded([APIDiscoveredModel])
        case failed(String)
    }
    /// Distinct per fetch, so a late earlier reply cannot overwrite a newer
    /// request for the same provider.
    let fetchID: Int
    /// The exact provider body sent. Any provider edit changes it and
    /// invalidates the session.
    let identity: APILLMProviderInput
    /// The configuration revision the catalogue was fetched against. A
    /// reload or save changes it and makes the results stale.
    let revision: Int64
    var phase: Phase
}

/// A model check bound to its model, capability, provider and revision.
/// Results are memory-only and never change a route or a price.
struct LLMModelCheckSession: Equatable {
    enum Phase: Equatable {
        case checking
        case result(APILLMModelCheck)
        case failed(String)
    }
    let checkID: Int
    let modelName: String
    let capability: APILLMModelCapability
    let identity: APILLMProviderInput
    let revision: Int64
    var phase: Phase
}

/// Which capability a selection is for. It decides which metadata the
/// catalogue must supply: a chat selection ignores embedding geometry and
/// rate, an embedding selection ignores chat rates.
enum LLMModelUsage: String, CaseIterable, Identifiable {
    case chat
    case embedding

    var id: String { rawValue }
    var title: String { self == .chat ? "Chat" : "Embedding" }
}

/// A catalogue entry pending metadata completion before it becomes a
/// configured model. The catalogue pre-fills what it knows; fields the
/// selection does not use are explicitly zero. An unknown applicable price is
/// never silently free, and blank dimensions are a missing value rather than
/// an invalid integer.
struct LLMModelReview: Identifiable, Equatable {
    let discovered: APIDiscoveredModel
    let provider: String
    let usage: LLMModelUsage
    var inputPer1M: String
    var outputPer1M: String
    var embeddingPer1M: String
    var dimensions: String
    var truncatable: Bool
    var supportsCustomDimensions: Bool
    /// The user explicitly declared the model free; this is the only way a
    /// missing price becomes zero without a capability ruling it out.
    var isFree = false
    /// True for the manual-ID fallback, which the catalogue cannot vouch for.
    var isManual = false
    var id: String { discovered.id }

    init(discovered: APIDiscoveredModel, provider: String, usage: LLMModelUsage,
         isManual: Bool = false) {
        self.discovered = discovered
        self.provider = provider
        self.usage = usage
        self.isManual = isManual
        if usage == .chat {
            inputPer1M = discovered.inputPer1M ?? ""
            outputPer1M = discovered.outputPer1M ?? ""
            embeddingPer1M = "0"
            dimensions = "0"
            truncatable = false
            supportsCustomDimensions = false
        } else {
            inputPer1M = "0"
            outputPer1M = "0"
            embeddingPer1M = discovered.embeddingPer1M ?? ""
            dimensions = discovered.dimensions.map(String.init) ?? ""
            truncatable = discovered.truncatable ?? false
            supportsCustomDimensions = discovered.supportsCustomDimensions ?? false
        }
    }

    var inputValid: Bool { isValidLLMMoney(inputPer1M) }
    var outputValid: Bool { isValidLLMMoney(outputPer1M) }
    var embeddingPriceValid: Bool { isValidLLMMoney(embeddingPer1M) }
    var dimensionsValue: Int64? {
        let text = dimensions.trimmingCharacters(in: .whitespaces)
        guard !text.isEmpty, let value = Int64(text), (0...65536).contains(value) else { return nil }
        return value
    }
    var dimensionsValid: Bool {
        guard let value = dimensionsValue else { return false }
        if usage == .embedding && value == 0 { return false }
        return value <= llmStorageDimensions || truncatable || supportsCustomDimensions
    }

    /// A blank or unusable width is reported as missing metadata, with a
    /// precise next step, never as a bare invalid integer.
    var dimensionsMessage: String? {
        let text = dimensions.trimmingCharacters(in: .whitespaces)
        if text.isEmpty { return usage == .embedding ? "Dimensions missing" : nil }
        guard let value = Int64(text) else { return "Invalid dimensions" }
        if !(0...65536).contains(value) { return "Invalid dimensions" }
        if usage == .embedding && value == 0 { return "Embeddings need a positive width" }
        if value > llmStorageDimensions, !truncatable, !supportsCustomDimensions {
            return "Above \(llmStorageDimensions) needs truncatable or custom dimensions"
        }
        return nil
    }

    /// Ready to select: every applicable field holds an explicit valid value.
    var isReady: Bool {
        usage == .chat
            ? inputValid && outputValid
            : embeddingPriceValid && dimensionsValid
    }

    /// A check needs explicit rates, but an embedding width may be observed by
    /// the check itself. It never invents a price.
    var isCheckable: Bool {
        usage == .chat ? inputValid && outputValid : embeddingPriceValid
    }

    var missingMetadata: [String] {
        var missing: [String] = []
        if usage == .chat {
            if !inputValid { missing.append("input price") }
            if !outputValid { missing.append("output price") }
        } else {
            if !embeddingPriceValid { missing.append("embedding price") }
            if dimensionsMessage != nil { missing.append("dimensions") }
        }
        return missing
    }

    /// Marks every price explicitly free. Geometry is untouched.
    mutating func markFree() {
        isFree = true
        inputPer1M = "0"
        outputPer1M = "0"
        embeddingPer1M = "0"
    }

    /// The configured model this review becomes. Call only when `isReady`.
    func configured() -> LLMModelDraft {
        var draft = LLMModelDraft()
        draft.name = discovered.id
        draft.provider = provider
        draft.supportsCustomDimensions = supportsCustomDimensions
        draft.inputPer1M = inputPer1M
        draft.outputPer1M = outputPer1M
        draft.embeddingPer1M = embeddingPer1M
        draft.dimensions = dimensionsValue ?? 0
        draft.truncatable = truncatable
        return draft
    }
}

/// A catalogue entry stays a task candidate unless the capability the task
/// needs is known absent; unknown capabilities are labelled, not filtered.
func llmModelSuits(_ model: APIDiscoveredModel, task: APILLMTask) -> Bool {
    guard task == .embed else { return model.chat != false }
    if let width = model.dimensions, width > 1024,
       model.supportsCustomDimensions == false, model.truncatable != true { return false }
    return model.embedding != false
}

/// One window's draft over the server-wide AI configuration. There
/// is no sync or cache: loading and saving are explicit, a failed or
/// conflicting save never claims success, and a 409 preserves the draft until
/// the user reloads.
@MainActor final class LLMSettingsModel: ObservableObject {
    private static let providerNameConflict = "That provider name is already in use. Choose a different name."
    @Published var draft: LLMSettingsDraft = LLMSettingsDraft() {
        didSet {
            saved = false
            // Editing ends a frozen request; the next save mints a new op ID.
            pending = nil
            // A provider edit changes the input a fetch was bound to, so its
            // catalogue session is stale. Replies that still arrive after
            // this point are discarded on their identity check.
            discoveries = discoveries.filter { providerID, session in
                session.revision == revision
                    && draft.providers.first { $0.id == providerID }?.providerInput == session.identity
            }
            checks = checks.filter { providerID, session in
                session.revision == revision
                    && draft.providers.first { $0.id == providerID }?.providerInput == session.identity
            }
        }
    }
    /// Catalogue sessions per provider draft row, memory only. Keys are draft
    /// identities, so a reload or save; which rebuilds the draft; drops
    /// every session, and nothing about a typed key outlives the draft.
    @Published private(set) var discoveries: [UUID: LLMDiscoverySession] = [:]
    /// Model checks per provider draft row, memory only and scoped to the
    /// exact provider and revision they were sent with.
    @Published private(set) var checks: [UUID: LLMModelCheckSession] = [:]
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
    private var fetchCounter = 0
    private var checkCounter = 0
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

    /// Discard is meaningful once the draft differs from the loaded baseline.
    var canDiscard: Bool { isDirty || conflict }

    // MARK: - Loading

    /// Load on opening the pane, without replacing an existing draft.
    func loadIfNeeded() {
        guard ready, !loaded, !busy else { return }
        load()
    }

    /// Fetch the server configuration. The caller confirms discarding edits;
    /// a failed load keeps them until a replacement is successfully received.
    func load() {
        guard !busy else { return }
        if synchronizeScope() {
            error = "Account data changed. Reload the AI settings."
            return
        }
        guard let api = app.api, let scope else {
            error = "Sign in to load the AI settings."
            return
        }
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
            error = "Account data changed. Reload the AI settings."
            return
        }
        guard let api = app.api, let scope else {
            error = "Sign in to save the AI settings."
            return
        }
        guard !conflict else {
            error = "These settings changed elsewhere. Reload before saving."
            return
        }
        guard let revision else {
            error = "Load the AI settings before saving."
            return
        }
        for provider in draft.providers {
            guard let original = baseline?.providers.first(where: { $0.id == provider.id }),
                  original.hasAPIKey, original.name != provider.name,
                  provider.apiKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  !provider.clearAPIKey else { continue }
            pending = nil
            error = "Renaming a provider with a saved API key requires a replacement key or explicit removal."
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
                // Definite rejection needs an edit, not an identical retry.
                // Preserve the frozen body when the write's outcome is uncertain.
                if error is APILLMSettingsError ||
                    ((error is APIProblem || error is APIError) && !ErrorPresentation.isTransient(error)) {
                    self.pending = nil
                }
                self.error = ErrorPresentation.message(error)
            }
        }
    }

    // MARK: - Model discovery

    /// Fetch the catalogue for one provider draft exactly as it is currently
    /// edited. The preview persists nothing and sends no inference request;
    /// results stay in memory and are discarded the moment the provider, the
    /// account or the server changes.
    func fetchModels(providerID: UUID) {
        if synchronizeScope() {
            error = "Account data changed. Reload the AI settings."
            return
        }
        guard let api = app.api, let scope else {
            error = "Sign in to fetch models."
            return
        }
        guard let revision else {
            error = "Load the AI settings before fetching models."
            return
        }
        guard !conflict else {
            error = "These settings changed elsewhere. Reload before fetching models."
            return
        }
        guard let provider = draft.providers.first(where: { $0.id == providerID }) else { return }
        if case .fetching = discoveries[providerID]?.phase { return }
        let identity = provider.providerInput
        if let cached = discoveries[providerID], cached.identity == identity,
           cached.revision == revision, case .loaded = cached.phase { return }
        fetchCounter &+= 1
        let fetchID = fetchCounter
        discoveries[providerID] = LLMDiscoverySession(fetchID: fetchID, identity: identity,
                                                      revision: revision, phase: .fetching)
        Task { [weak self] in
            guard let self else { return }
            do {
                let models = try await api.discoverLLMModels(revision: revision, provider: identity)
                try Task.checkCancellation()
                guard self.acceptsDiscovery(fetchID: fetchID, providerID: providerID,
                                            identity: identity, revision: revision, scope: scope) else { return }
                self.discoveries[providerID]?.phase = .loaded(models)
            } catch is CancellationError {
            } catch {
                guard self.acceptsDiscovery(fetchID: fetchID, providerID: providerID,
                                            identity: identity, revision: revision, scope: scope) else { return }
                if ErrorPresentation.isUnauthorized(error) {
                    self.app.report(error)
                    self.endSession()
                    return
                }
                if ErrorPresentation.isConflict(error) {
                    self.discoveries[providerID]?.phase = .failed("These settings changed elsewhere. Reload before fetching models.")
                    return
                }
                self.discoveries[providerID]?.phase = .failed(ErrorPresentation.message(error))
            }
        }
    }

    func refreshModels(providerID: UUID) {
        discoveries.removeValue(forKey: providerID)
        fetchModels(providerID: providerID)
    }

    /// A reply counts only while its fetch is still the current one, for the
    /// exact provider input and revision it was sent with, in the same account
    /// and server session. Anything else; a provider edit, a reload, a newer
    /// fetch or a scope change; means the catalogue is stale and the reply is
    /// dropped.
    private func acceptsDiscovery(fetchID: Int, providerID: UUID,
                                  identity: APILLMProviderInput, revision: Int64,
                                  scope: LLMSettingsScope) -> Bool {
        guard app.ownsLLMSettings(scope), !Task.isCancelled,
              self.revision == revision,
              let session = discoveries[providerID],
              session.fetchID == fetchID,
              session.revision == revision,
              case .fetching = session.phase,
              draft.providers.first(where: { $0.id == providerID })?.providerInput == identity else {
            return false
        }
        return true
    }

    // MARK: - Model check

    func modelCheck(providerID: UUID, modelName: String,
                    capability: APILLMModelCapability) -> LLMModelCheckSession? {
        guard let check = checks[providerID], check.modelName == modelName,
              check.capability == capability else { return nil }
        return check
    }

    /// Check one draft model against its provider with a fixed sample. The
    /// sample may cost; nothing is persisted and no route changes. Observed
    /// geometry is applied to the matching draft model only.
    func checkModel(providerID: UUID, model: LLMModelDraft, capability: APILLMModelCapability) {
        if synchronizeScope() {
            error = "Account data changed. Reload the AI settings."
            return
        }
        guard let api = app.api, let scope else {
            error = "Sign in to check a model."
            return
        }
        guard let revision else {
            error = "Load the AI settings before checking a model."
            return
        }
        guard !conflict else {
            error = "These settings changed elsewhere. Reload before checking a model."
            return
        }
        guard let provider = draft.providers.first(where: { $0.id == providerID }) else { return }
        let identity = provider.providerInput
        guard isValidLLMMoney(model.inputPer1M), isValidLLMMoney(model.outputPer1M),
              isValidLLMMoney(model.embeddingPer1M) else {
            error = "Provide the rates or mark the model free before checking it."
            return
        }
        checkCounter &+= 1
        let checkID = checkCounter
        checks[providerID] = LLMModelCheckSession(checkID: checkID, modelName: model.name,
                                                  capability: capability, identity: identity,
                                                  revision: revision, phase: .checking)
        Task { [weak self] in
            guard let self else { return }
            do {
                let result = try await api.checkLLMModel(revision: revision, provider: identity,
                                                         model: model.settings, capability: capability)
                try Task.checkCancellation()
                guard self.acceptsCheck(checkID: checkID, providerID: providerID,
                                        identity: identity, revision: revision, scope: scope) else { return }
                self.applyObservedGeometry(model.identity, result)
                self.checks[providerID]?.phase = .result(result)
            } catch is CancellationError {
            } catch {
                guard self.acceptsCheck(checkID: checkID, providerID: providerID,
                                        identity: identity, revision: revision, scope: scope) else { return }
                if ErrorPresentation.isUnauthorized(error) {
                    self.app.report(error)
                    self.endSession()
                    return
                }
                if ErrorPresentation.isConflict(error) {
                    self.checks[providerID]?.phase = .failed("These settings changed elsewhere. Reload before checking this model.")
                    return
                }
                self.checks[providerID]?.phase = .failed(ErrorPresentation.message(error))
            }
        }
    }

    /// A check reply counts only while it is still current for the exact
    /// provider input, revision and account it was sent with.
    private func acceptsCheck(checkID: Int, providerID: UUID, identity: APILLMProviderInput,
                              revision: Int64, scope: LLMSettingsScope) -> Bool {
        guard app.ownsLLMSettings(scope), !Task.isCancelled,
              self.revision == revision,
              let session = checks[providerID],
              session.checkID == checkID,
              session.revision == revision,
              case .checking = session.phase,
              draft.providers.first(where: { $0.id == providerID })?.providerInput == identity else {
            return false
        }
        return true
    }

    /// Only observed geometry may reach the draft; prices and routes are
    /// never changed by a check.
    private func applyObservedGeometry(_ identity: String, _ result: APILLMModelCheck) {
        guard result.compatible,
              let index = draft.models.firstIndex(where: { $0.identity == identity }) else { return }
        if let dimensions = result.dimensions { draft.models[index].dimensions = dimensions }
        if let supports = result.supportsCustomDimensions {
            draft.models[index].supportsCustomDimensions = supports
        }
    }

    /// The provider row a task route currently names, for task-scoped
    /// fetching. A route by another name means the provider is gone.
    func providerID(named name: String) -> UUID? {
        draft.providers.first { $0.name == name }?.id
    }

    /// Add configured models. An identity that is already configured keeps
    /// its existing entry and prices. Returns the number added, capped by the
    /// server-wide configured-model limit.
    @discardableResult
    func importModels(_ models: [LLMModelDraft]) -> Int {
        var existing = Set(draft.models.map(\.identity))
        let fresh = models.filter { existing.insert($0.identity).inserted }
        guard draft.models.count + fresh.count <= llmModelLimit else {
            error = "At most \(llmModelLimit) configured models are allowed. Deselect models before importing."
            return 0
        }
        draft.models.append(contentsOf: fresh)
        return fresh.count
    }

    /// What a catalogue selection does. Selecting a default only chooses the
    /// inherited route; it never enables a task that is off.
    enum LLMModelSelection: Equatable {
        case add
        case task(APILLMTask)
        case defaultText
        case defaultEmbedding
    }

    /// Select one reviewed catalogue model. A ready model updates the draft
    /// immediately: it is added when its provider-scoped identity is not
    /// configured yet, and a task or default selection then routes it. An
    /// existing entry keeps its saved prices. Ownership is rechecked here, not
    /// just in the sheet's last render.
    @discardableResult
    func selectCatalogueModel(_ review: LLMModelReview, providerID: UUID, fetchID: Int,
                              selection: LLMModelSelection) -> Bool {
        guard let scope, app.ownsLLMSettings(scope), ready, !busy, !conflict,
              let provider = draft.providers.first(where: { $0.id == providerID }),
              let session = discoveries[providerID], session.fetchID == fetchID,
              session.revision == revision,
              session.identity == provider.providerInput,
              case .loaded(let models) = session.phase,
              models.contains(where: { $0.id == review.id }) else {
            error = "The catalogue is no longer current. Fetch models again before selecting this model."
            return false
        }
        return applySelection(review, provider: provider, selection: selection)
    }

    /// The manual-ID fallback: the model is not in the catalogue, so only
    /// ownership, readiness and the provider draft are checked.
    @discardableResult
    func selectManualModel(_ review: LLMModelReview, providerID: UUID,
                           selection: LLMModelSelection) -> Bool {
        guard let scope, app.ownsLLMSettings(scope), ready, !busy, !conflict,
              let provider = draft.providers.first(where: { $0.id == providerID }) else {
            error = "The provider is no longer current. Fetch models again before selecting this model."
            return false
        }
        return applySelection(review, provider: provider, selection: selection)
    }

    private func applySelection(_ review: LLMModelReview, provider: LLMProviderDraft,
                                selection: LLMModelSelection) -> Bool {
        guard !provider.name.isEmpty, provider.name == provider.routedName else {
            error = "Choose a unique provider name before choosing a model."
            return false
        }
        guard review.isReady else {
            error = "Fill in the missing metadata before selecting this model."
            return false
        }
        let suits: Bool
        switch selection {
        case .add: suits = true
        case .task(let task): suits = llmModelSuits(review.discovered, task: task)
        case .defaultText: suits = review.usage == .chat
        case .defaultEmbedding: suits = review.usage == .embedding
        }
        guard suits else {
            error = "This model cannot be used for that feature."
            return false
        }
        // The provider draft is the source of truth; a sheet left open while
        // the provider was renamed must not scope the model to the old name.
        var configured = review.configured()
        configured.provider = provider.name
        if !draft.models.contains(where: { $0.identity == configured.identity }) {
            guard draft.models.count < llmModelLimit else {
                error = "At most \(llmModelLimit) configured models are allowed. Remove a model before adding this one."
                return false
            }
            draft.models.append(configured)
        }
        return applyRoute(providerName: provider.name, modelName: configured.name, selection: selection)
    }

    /// Use a model that is already configured, without changing its prices.
    @discardableResult
    func selectConfiguredModel(_ model: LLMModelDraft, providerID: UUID,
                                selection: LLMModelSelection) -> Bool {
        guard let scope, app.ownsLLMSettings(scope), ready, !busy, !conflict,
              let provider = draft.providers.first(where: { $0.id == providerID }) else {
            error = "The provider is no longer current. Fetch models again before selecting this model."
            return false
        }
        guard !provider.name.isEmpty, provider.name == provider.routedName else {
            error = "Choose a unique provider name before choosing a model."
            return false
        }
        guard draft.model(provider: provider.name, name: model.name)?.id == model.id else {
            error = "This model is no longer configured for this provider. Choose a current model."
            return false
        }
        return applyRoute(providerName: provider.name, modelName: model.name, selection: selection)
    }

    private func applyRoute(providerName: String, modelName: String,
                            selection: LLMModelSelection) -> Bool {
        switch selection {
        case .add:
            break
        case .task(let task):
            route(task: task, providerName: providerName, modelName: modelName)
        case .defaultText:
            if draft.defaults == nil { draft.defaults = APILLMDefaults() }
            draft.defaults?.text = APILLMDefaultRoute(provider: providerName, model: modelName)
        case .defaultEmbedding:
            if draft.defaults == nil { draft.defaults = APILLMDefaults() }
            draft.defaults?.embedding = APILLMDefaultRoute(provider: providerName, model: modelName)
        }
        error = nil
        return true
    }

    /// Route one task to a provider and model. A selection is an advanced
    /// override, so it makes the task custom; clearing the route disables it.
    func route(task: APILLMTask, providerName: String, modelName: String) {
        guard let index = draft.tasks.firstIndex(where: { $0.task == task }) else { return }
        if providerName.isEmpty || modelName.isEmpty {
            draft.tasks[index].mode = .off
            draft.tasks[index].provider = ""
            draft.tasks[index].model = ""
        } else {
            draft.tasks[index].provider = providerName
            draft.tasks[index].model = modelName
            draft.tasks[index].mode = .custom
        }
    }

    /// The route a task resolves to, including default inheritance. Used when
    /// a model or provider removal must disable exactly the routes that used
    /// it.
    func effectiveTaskRoute(_ task: LLMTaskDraft) -> APILLMDefaultRoute {
        draft.effectiveRoute(for: task)
    }

    /// Enable or disable a feature without choosing a route for it. Enabling
    /// adopts the matching default when one exists; otherwise it leaves a
    /// custom route for the user to fill. Disabling keeps any edited route so
    /// re-enabling restores it.
    func setTaskEnabled(_ task: APILLMTask, _ enabled: Bool) {
        guard let index = draft.tasks.firstIndex(where: { $0.task == task }) else { return }
        if enabled {
            guard draft.tasks[index].mode == .off else { return }
            // Re-enabling restores an edited custom route; otherwise it follows
            // the current default when one exists.
            let custom = APILLMDefaultRoute(provider: draft.tasks[index].provider,
                                            model: draft.tasks[index].model)
            if !custom.provider.isEmpty, !custom.model.isEmpty {
                draft.tasks[index].mode = .custom
                return
            }
            let inherited = defaultRoute(for: task)
            draft.tasks[index].mode = inherited.provider.isEmpty && inherited.model.isEmpty ? .custom : .default
            draft.tasks[index].provider = inherited.provider
            draft.tasks[index].model = inherited.model
            return
        }
        // Disabling a default task drops the inherited fields so re-enabling
        // follows the current default; a custom route is kept for restoration.
        if draft.tasks[index].mode == .default {
            draft.tasks[index].provider = ""
            draft.tasks[index].model = ""
        }
        draft.tasks[index].mode = .off
    }

    /// Choose default inheritance or an advanced custom route. Switching to
    /// default shows the inherited route; switching to custom seeds the fields
    /// from it so the edit starts where the default points.
    func setTaskMode(_ task: APILLMTask, _ mode: APILLMTaskMode) {
        guard let index = draft.tasks.firstIndex(where: { $0.task == task }) else { return }
        draft.tasks[index].mode = mode
        let inherited = defaultRoute(for: task)
        switch mode {
        case .default:
            draft.tasks[index].provider = inherited.provider
            draft.tasks[index].model = inherited.model
        case .custom:
            if draft.tasks[index].provider.isEmpty, !inherited.provider.isEmpty {
                draft.tasks[index].provider = inherited.provider
                draft.tasks[index].model = inherited.model
            }
        case .off:
            break
        }
    }

    func setTaskProvider(_ task: APILLMTask, _ provider: String) {
        guard let index = draft.tasks.firstIndex(where: { $0.task == task }) else { return }
        draft.tasks[index].provider = provider
        if draft.model(provider: provider, name: draft.tasks[index].model) == nil { draft.tasks[index].model = "" }
    }

    func setTaskModel(_ task: APILLMTask, _ model: String) {
        guard let index = draft.tasks.firstIndex(where: { $0.task == task }) else { return }
        draft.tasks[index].model = model
    }

    /// The inherited route for a task: text for every purpose except embed.
    func defaultRoute(for task: APILLMTask) -> APILLMDefaultRoute {
        guard let defaults = draft.defaults else { return APILLMDefaultRoute() }
        return task == .embed ? defaults.embedding : defaults.text
    }

    /// Configured models a provider can use. A provider-scoped entry wins
    /// over a global entry with the same name; the global entry is hidden in
    /// that case.
    func configuredModels(for provider: String) -> [LLMModelDraft] {
        let scoped = draft.models.filter { $0.provider == provider }
        let scopedNames = Set(scoped.map(\.name))
        return scoped + draft.models.filter { $0.provider.isEmpty && !scopedNames.contains($0.name) }
    }

    /// Set or clear a default route directly from a configured provider and
    /// model. It never changes a task's mode.
    func setDefaultRoute(provider: String, model: String, usage: LLMModelUsage) {
        if provider.isEmpty, model.isEmpty {
            if var defaults = draft.defaults {
                switch usage {
                case .chat: defaults.text = APILLMDefaultRoute()
                case .embedding: defaults.embedding = APILLMDefaultRoute()
                }
                draft.defaults = defaults.isEmpty ? nil : defaults
            }
            return
        }
        if draft.defaults == nil { draft.defaults = APILLMDefaults() }
        let route = APILLMDefaultRoute(provider: provider, model: model)
        switch usage {
        case .chat: draft.defaults?.text = route
        case .embedding: draft.defaults?.embedding = route
        }
    }

    // MARK: - Editing

    func addProvider() {
        addProvider(preset: .openai)
    }

    /// Add a provider from a preset with a unique, editable name. Custom
    /// leaves the URL empty so the operator supplies the destination.
    @discardableResult
    func addProvider(preset: LLMProviderPreset) -> UUID {
        var provider = LLMProviderDraft()
        provider.name = uniqueProviderName(base: preset.rawValue)
        provider.routedName = provider.name
        provider.kind = preset.kind
        provider.baseURL = preset.baseURL
        draft.providers.append(provider)
        return provider.id
    }

    /// A preset's base name is used once; the next one gains a numeric
    /// suffix. Names stay valid provider identifiers. A sibling's name and
    /// its routed name are both reserved, so a cleared name keeps its
    /// identity out of the pool.
    private func uniqueProviderName(base: String) -> String {
        var names = Set(draft.providers.flatMap { [$0.name, $0.routedName] })
        if names.insert(base).inserted { return base }
        var suffix = 2
        while !names.insert("\(base)-\(suffix)").inserted { suffix += 1 }
        return "\(base)-\(suffix)"
    }

    // An empty name is a transient edit, never the identity of global models.
    func renameProvider(id: UUID, name: String) {
        guard let index = draft.providers.firstIndex(where: { $0.id == id }) else { return }
        // A name identifies routes, defaults and scoped models. A colliding
        // value remains editable, but cannot take the sibling's
        // identity or move any routes while the user finishes typing.
        let collision = !name.isEmpty && draft.providers.contains(where: {
            $0.id != id && ($0.name == name || $0.routedName == name)
        })
        if collision {
            draft.providers[index].name = name
            error = Self.providerNameConflict
            return
        }
        var next = draft
        let old = next.providers[index].routedName.isEmpty ? next.providers[index].name : next.providers[index].routedName
        next.providers[index].name = name
        if !name.isEmpty {
            next.providers[index].routedName = name
            if !old.isEmpty, old != name {
                for index in next.tasks.indices where next.tasks[index].provider == old {
                    next.tasks[index].provider = name
                }
                for index in next.models.indices where next.models[index].provider == old {
                    next.models[index].provider = name
                }
                if next.defaults?.text.provider == old { next.defaults?.text.provider = name }
                if next.defaults?.embedding.provider == old { next.defaults?.embedding.provider = name }
            }
        }
        draft = next
        if error == Self.providerNameConflict { error = nil }
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
        guard let provider = draft.providers.first(where: { $0.id == id }) else { return }
        let name = provider.routedName.isEmpty ? provider.name : provider.routedName
        let affected = draft.tasks.indices.filter { index in
            let task = draft.tasks[index]
            return !name.isEmpty && (effectiveTaskRoute(task).provider == name
                || (task.mode != .default && task.provider == name))
        }
        draft.providers.removeAll { $0.id == id }
        guard !name.isEmpty else { return }
        // A removed provider takes its scoped models, routes and defaults with
        // it. Global models and other providers' routes stay.
        draft.models.removeAll { $0.provider == name }
        if var defaults = draft.defaults {
            if defaults.text.provider == name { defaults.text = APILLMDefaultRoute() }
            if defaults.embedding.provider == name { defaults.embedding = APILLMDefaultRoute() }
            draft.defaults = defaults.isEmpty ? nil : defaults
        }
        for index in affected {
            draft.tasks[index].mode = .off
            draft.tasks[index].provider = ""
            draft.tasks[index].model = ""
        }
    }

    func removeModel(id: UUID) {
        func usesRemoved(_ route: APILLMDefaultRoute) -> Bool {
            draft.model(provider: route.provider, name: route.model)?.id == id
        }
        let affected = draft.tasks.indices.filter { index in
            let task = draft.tasks[index]
            return usesRemoved(effectiveTaskRoute(task)) ||
                (task.mode == .off && usesRemoved(APILLMDefaultRoute(provider: task.provider, model: task.model)))
        }
        var defaults = draft.defaults
        if let route = defaults?.text, usesRemoved(route) { defaults?.text = APILLMDefaultRoute() }
        if let route = defaults?.embedding, usesRemoved(route) { defaults?.embedding = APILLMDefaultRoute() }
        draft.models.removeAll { $0.id == id }
        draft.defaults = defaults.flatMap { $0.isEmpty ? nil : $0 }
        for index in affected {
            draft.tasks[index].mode = .off
            draft.tasks[index].provider = ""
            draft.tasks[index].model = ""
        }
    }

    /// Discard every unsaved edit and return to the loaded baseline. A
    /// conflict reloads the current saved configuration; a failed load keeps
    /// the existing draft and conflict visible.
    func discard() {
        if synchronizeScope() {
            error = "Account data changed. Reload the AI settings."
            return
        }
        if conflict { load(); return }
        guard let baseline else { return }
        pending = nil
        conflict = false
        error = nil
        saved = false
        checks = [:]
        draft = baseline
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
        discoveries = [:]
        checks = [:]
    }

    private func adopt(_ settings: APILLMSettings) {
        // Rebuilding from the response discards every key draft.
        draft = LLMSettingsDraft(settings: settings)
        baseline = draft
        revision = settings.revision
        loaded = true
        pending = nil
        error = nil
        conflict = false
        saved = false
        // The revision moved: every cached catalogue and check is stale.
        discoveries = [:]
        checks = [:]
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

/// The provider starting points offered when a provider is added. Venice
/// speaks the OpenAI dialect at its own destination; Custom leaves the URL
/// for the operator to enter.
enum LLMProviderPreset: String, CaseIterable, Identifiable {
    case venice
    case openai
    case anthropic
    case gemini
    case custom

    var id: String { rawValue }

    var title: String {
        switch self {
        case .venice: return "Venice"
        case .openai: return "OpenAI"
        case .anthropic: return "Anthropic"
        case .gemini: return "Gemini"
        case .custom: return "Custom"
        }
    }

    var kind: APILLMProviderKind {
        switch self {
        case .venice, .openai, .custom: return .openai
        case .anthropic: return .anthropic
        case .gemini: return .gemini
        }
    }

    var baseURL: String {
        switch self {
        case .venice: return "https://api.venice.ai/api/v1"
        case .openai: return "https://api.openai.com/v1"
        case .anthropic: return "https://api.anthropic.com"
        case .gemini: return "https://generativelanguage.googleapis.com/v1beta"
        case .custom: return ""
        }
    }

    var isCustom: Bool { self == .custom }
}

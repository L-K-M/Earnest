import Foundation

/// The provider dialects the server speaks. Wire values mirror
/// `internal/llm/config.go`.
public enum APILLMProviderKind: String, Codable, Sendable, Equatable, CaseIterable {
    case openai = "openai"
    case anthropic = "anthropic"
    case gemini = "gemini"
}

/// Which request field carries the completion token budget.
public enum APILLMTokenField: String, Codable, Sendable, Equatable, CaseIterable {
    case maxCompletionTokens = "max_completion_tokens"
    case maxTokens = "max_tokens"
}

/// The pipeline purposes that route to a provider and model. Every task is
/// enumerated even when disabled, so a response always carries all seven.
public enum APILLMTask: String, Codable, Sendable, Equatable, CaseIterable {
    case embed
    case classify
    case blurb
    case summary
    case edition
    case compileRule = "compile_rule"
    case ask
}

/// How one task chooses its route. A missing wire value is the legacy form:
/// a nonempty explicit route means custom, an empty one means off.
public enum APILLMTaskMode: String, Codable, Sendable, Equatable, CaseIterable {
    case off
    case `default` = "default"
    case custom
}

/// One default route. Both strings empty means the task resolves to disabled.
public struct APILLMDefaultRoute: Codable, Sendable, Equatable {
    public var provider: String
    public var model: String

    enum CodingKeys: String, CodingKey { case provider, model }

    public init(provider: String = "", model: String = "") {
        self.provider = provider
        self.model = model
    }
}

/// Editor inheritance for tasks: every non-embed task follows `text`, embed
/// follows `embedding`. The server stores effective routes too, so old
/// readers keep working.
public struct APILLMDefaults: Codable, Sendable, Equatable {
    public var text: APILLMDefaultRoute
    public var embedding: APILLMDefaultRoute

    enum CodingKeys: String, CodingKey { case text, embedding }

    public init(text: APILLMDefaultRoute = APILLMDefaultRoute(),
                embedding: APILLMDefaultRoute = APILLMDefaultRoute()) {
        self.text = text
        self.embedding = embedding
    }

    /// A defaults block with neither route set carries no selection.
    public var isEmpty: Bool {
        text.provider.isEmpty && text.model.isEmpty
            && embedding.provider.isEmpty && embedding.model.isEmpty
    }
}

/// One provider as the server reports it. It never carries the key itself,
/// only whether one is stored.
public struct APILLMProviderSettings: Codable, Sendable, Equatable {
    public var name: String
    public var kind: APILLMProviderKind
    public var baseURL: String
    public var hasAPIKey: Bool
    public var monthlyLimitUSD: String
    public var timeoutSeconds: Int64
    public var tokenField: APILLMTokenField

    enum CodingKeys: String, CodingKey {
        case name, kind
        case baseURL = "base_url"
        case hasAPIKey = "has_api_key"
        case monthlyLimitUSD = "monthly_limit_usd"
        case timeoutSeconds = "timeout_seconds"
        case tokenField = "token_field"
    }

    public init(name: String, kind: APILLMProviderKind, baseURL: String, hasAPIKey: Bool,
                monthlyLimitUSD: String, timeoutSeconds: Int64, tokenField: APILLMTokenField) {
        self.name = name
        self.kind = kind
        self.baseURL = baseURL
        self.hasAPIKey = hasAPIKey
        self.monthlyLimitUSD = monthlyLimitUSD
        self.timeoutSeconds = timeoutSeconds
        self.tokenField = tokenField
    }
}

/// One provider as a save sends it. An empty `apiKey` keeps the stored key for
/// the same name, URL and kind; `clearAPIKey` removes it and cannot be combined
/// with a replacement.
public struct APILLMProviderInput: Codable, Sendable, Equatable {
    public var name: String
    public var kind: APILLMProviderKind
    public var baseURL: String
    public var apiKey: String?
    public var clearAPIKey: Bool
    public var monthlyLimitUSD: String
    public var timeoutSeconds: Int64
    public var tokenField: APILLMTokenField

    enum CodingKeys: String, CodingKey {
        case name, kind
        case baseURL = "base_url"
        case apiKey = "api_key"
        case clearAPIKey = "clear_api_key"
        case monthlyLimitUSD = "monthly_limit_usd"
        case timeoutSeconds = "timeout_seconds"
        case tokenField = "token_field"
    }

    public init(name: String, kind: APILLMProviderKind, baseURL: String, apiKey: String? = nil,
                clearAPIKey: Bool = false, monthlyLimitUSD: String, timeoutSeconds: Int64,
                tokenField: APILLMTokenField) {
        self.name = name
        self.kind = kind
        self.baseURL = baseURL
        self.apiKey = apiKey
        self.clearAPIKey = clearAPIKey
        self.monthlyLimitUSD = monthlyLimitUSD
        self.timeoutSeconds = timeoutSeconds
        self.tokenField = tokenField
    }
}

/// One task route. Both strings empty means the task is disabled and makes no
/// outbound calls.
public struct APILLMTaskSettings: Codable, Sendable, Equatable {
    public var task: APILLMTask
    public var mode: APILLMTaskMode?
    public var provider: String
    public var model: String

    enum CodingKeys: String, CodingKey { case task, mode, provider, model }

    public init(task: APILLMTask, mode: APILLMTaskMode? = nil, provider: String, model: String) {
        self.task = task
        self.mode = mode
        self.provider = provider
        self.model = model
    }
}

/// A model's prices and embedding geometry. Prices are decimal strings, never
/// floating point, so an exact configured price can never drift.
public struct APILLMModelSettings: Codable, Sendable, Equatable {
    public var name: String
    /// Empty is the legacy global entry; a nonempty value scopes the model to
    /// one provider. Identity is provider plus name, and a scoped entry wins
    /// over a global one.
    public var provider: String
    /// Provider-side resizing, never local truncation. A width above the
    /// storage limit is valid when either this or `truncatable` is set.
    public var supportsCustomDimensions: Bool
    public var inputPer1M: String
    public var outputPer1M: String
    public var embeddingPer1M: String
    public var dimensions: Int64
    public var truncatable: Bool

    enum CodingKeys: String, CodingKey {
        case name, provider, supportsCustomDimensions = "supports_custom_dimensions"
        case inputPer1M = "input_per_1m"
        case outputPer1M = "output_per_1m"
        case embeddingPer1M = "embedding_per_1m"
        case dimensions, truncatable
    }

    public init(name: String, provider: String = "", supportsCustomDimensions: Bool = false,
                inputPer1M: String, outputPer1M: String, embeddingPer1M: String,
                dimensions: Int64, truncatable: Bool) {
        self.name = name
        self.provider = provider
        self.supportsCustomDimensions = supportsCustomDimensions
        self.inputPer1M = inputPer1M
        self.outputPer1M = outputPer1M
        self.embeddingPer1M = embeddingPer1M
        self.dimensions = dimensions
        self.truncatable = truncatable
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decode(String.self, forKey: .name)
        provider = try container.decodeIfPresent(String.self, forKey: .provider) ?? ""
        supportsCustomDimensions = try container.decodeIfPresent(Bool.self, forKey: .supportsCustomDimensions) ?? false
        inputPer1M = try container.decode(String.self, forKey: .inputPer1M)
        outputPer1M = try container.decode(String.self, forKey: .outputPer1M)
        embeddingPer1M = try container.decode(String.self, forKey: .embeddingPer1M)
        dimensions = try container.decode(Int64.self, forKey: .dimensions)
        truncatable = try container.decode(Bool.self, forKey: .truncatable)
    }
}

/// One catalogue entry a provider reports through discovery. It is a
/// suggestion, not a configured model: a missing capability or price field
/// means unknown, never false or free.
public struct APIDiscoveredModel: Codable, Sendable, Equatable {
    public var id: String
    public var name: String
    public var chat: Bool?
    public var embedding: Bool?
    public var inputPer1M: String?
    public var outputPer1M: String?
    public var embeddingPer1M: String?
    public var dimensions: Int64?
    public var truncatable: Bool?
    public var supportsCustomDimensions: Bool?

    enum CodingKeys: String, CodingKey {
        case id, name, chat, embedding
        case inputPer1M = "input_per_1m"
        case outputPer1M = "output_per_1m"
        case embeddingPer1M = "embedding_per_1m"
        case dimensions, truncatable
        case supportsCustomDimensions = "supports_custom_dimensions"
    }

    public init(id: String, name: String, chat: Bool? = nil, embedding: Bool? = nil,
                inputPer1M: String? = nil, outputPer1M: String? = nil,
                embeddingPer1M: String? = nil, dimensions: Int64? = nil,
                truncatable: Bool? = nil, supportsCustomDimensions: Bool? = nil) {
        self.id = id
        self.name = name
        self.chat = chat
        self.embedding = embedding
        self.inputPer1M = inputPer1M
        self.outputPer1M = outputPer1M
        self.embeddingPer1M = embeddingPer1M
        self.dimensions = dimensions
        self.truncatable = truncatable
        self.supportsCustomDimensions = supportsCustomDimensions
    }
}

/// The catalogue response. `models` is nullable on the wire; clients treat
/// null the same as an empty catalogue.
public struct APIDiscoveredModels: Codable, Sendable, Equatable {
    public var models: [APIDiscoveredModel]?

    enum CodingKeys: String, CodingKey { case models }

    public init(models: [APIDiscoveredModel]? = nil) {
        self.models = models
    }
}

/// The redacted server-wide LLM configuration. `revision` 0 means the
/// deployment's environment defaults; 1 or more is a saved configuration.
public struct APILLMSettings: Codable, Sendable, Equatable {
    public var revision: Int64
    public var providers: [APILLMProviderSettings]
    public var tasks: [APILLMTaskSettings]
    public var models: [APILLMModelSettings]
    public var defaults: APILLMDefaults?

    enum CodingKeys: String, CodingKey { case revision, providers, tasks, models, defaults }

    public init(revision: Int64, providers: [APILLMProviderSettings], tasks: [APILLMTaskSettings],
                models: [APILLMModelSettings], defaults: APILLMDefaults? = nil) {
        self.revision = revision
        self.providers = providers
        self.tasks = tasks
        self.models = models
        self.defaults = defaults
    }
}

/// A complete replacement of the server-wide configuration. `opID` is minted
/// once per intentional save and reused only when retrying the same request.
public struct APILLMSettingsInput: Codable, Sendable, Equatable {
    public var revision: Int64
    public var opID: String
    public var providers: [APILLMProviderInput]
    public var tasks: [APILLMTaskSettings]
    public var models: [APILLMModelSettings]
    public var defaults: APILLMDefaults?

    enum CodingKeys: String, CodingKey {
        case revision
        case opID = "op_id"
        case providers, tasks, models, defaults
    }

    public init(revision: Int64, opID: String, providers: [APILLMProviderInput],
                tasks: [APILLMTaskSettings], models: [APILLMModelSettings],
                defaults: APILLMDefaults? = nil) {
        self.revision = revision
        self.opID = opID
        self.providers = providers
        self.tasks = tasks
        self.models = models
        self.defaults = defaults
    }
}

/// Which capability a model check exercises.
public enum APILLMModelCapability: String, Codable, Sendable, Equatable, CaseIterable {
    case chat
    case embedding
}

/// A model check sends a fixed sample through the same provider path as a
/// real request. It never changes routing or writes article content.
public struct APILLMModelCheckInput: Codable, Sendable, Equatable {
    public var revision: Int64
    public var provider: APILLMProviderInput
    public var model: APILLMModelSettings
    public var capability: APILLMModelCapability

    enum CodingKeys: String, CodingKey { case revision, provider, model, capability }

    public init(revision: Int64, provider: APILLMProviderInput, model: APILLMModelSettings,
                capability: APILLMModelCapability) {
        self.revision = revision
        self.provider = provider
        self.model = model
        self.capability = capability
    }
}

/// The observed result of a model check. `dimensions` and
/// `supportsCustomDimensions` are observations, not saved settings; a caller
/// may apply them to its draft only.
public struct APILLMModelCheck: Codable, Sendable, Equatable {
    public var compatible: Bool
    public var dimensions: Int64?
    public var supportsCustomDimensions: Bool?
    public var message: String

    enum CodingKeys: String, CodingKey {
        case compatible, dimensions, message
        case supportsCustomDimensions = "supports_custom_dimensions"
    }

    public init(compatible: Bool, dimensions: Int64? = nil,
                supportsCustomDimensions: Bool? = nil, message: String) {
        self.compatible = compatible
        self.dimensions = dimensions
        self.supportsCustomDimensions = supportsCustomDimensions
        self.message = message
    }
}

/// Thrown when a save fails client-side validation, before any request is
/// sent. The server applies the same rules; failing locally keeps a bad edit
/// off the wire.
public struct APILLMSettingsError: LocalizedError, Sendable, Equatable {
    public let field: String
    public init(field: String) { self.field = field }
    public var errorDescription: String? { "Invalid LLM setting: \(field)" }
}

/// The server's exact money bound: 99999999.999999 USD in millionths.
public let llmSettingsMaxMoneyMicros: Int64 = 99_999_999_999_999

/// The storage width a model must fit unless truncation is allowed.
public let llmStorageDimensions: Int64 = 1024

/// Client-side mirror of the server's `validateLLMSettings`, including the
/// destination-change rule against the previously loaded configuration. Money
/// stays decimal text: no floating-point conversion happens anywhere.
public func validateLLMSettings(_ input: APILLMSettingsInput, previous: APILLMSettings? = nil) throws {
    guard !input.opID.isEmpty, input.opID.utf8.count <= 1024 else { throw APILLMSettingsError(field: "op_id") }
    guard input.revision >= 0 else { throw APILLMSettingsError(field: "revision") }
    guard input.providers.count <= 32 else { throw APILLMSettingsError(field: "providers") }
    guard input.models.count <= 128 else { throw APILLMSettingsError(field: "models") }
    guard input.tasks.count <= APILLMTask.allCases.count else { throw APILLMSettingsError(field: "tasks") }

    var providerNames = Set<String>()
    for provider in input.providers {
        guard isValidLLMProviderName(provider.name) else { throw APILLMSettingsError(field: "providers.name") }
        guard providerNames.insert(provider.name).inserted else { throw APILLMSettingsError(field: "providers.name") }
        guard isValidLLMBaseURL(provider.baseURL) else { throw APILLMSettingsError(field: "providers.base_url") }
        guard isValidLLMMoney(provider.monthlyLimitUSD) else { throw APILLMSettingsError(field: "providers.monthly_limit_usd") }
        guard (1...600).contains(provider.timeoutSeconds) else { throw APILLMSettingsError(field: "providers.timeout_seconds") }
        let key = (provider.apiKey ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        guard key.utf8.count <= 8192, !key.unicodeScalars.contains(where: isControlScalar) else {
            throw APILLMSettingsError(field: "providers.api_key")
        }
        if provider.clearAPIKey && !key.isEmpty {
            throw APILLMSettingsError(field: "providers.api_key")
        }
        if key.isEmpty, !provider.clearAPIKey,
           let old = previous?.providers.first(where: { $0.name == provider.name }),
           old.hasAPIKey,
           normalizeLLMBaseURL(old.baseURL) != normalizeLLMBaseURL(provider.baseURL) || old.kind != provider.kind {
            // A stored secret must never be silently forwarded to a different
            // destination; the server enforces the same rule.
            throw APILLMSettingsError(field: "providers.api_key")
        }
    }

    var modelIdentities = Set<String>()
    for model in input.models {
        guard isValidLLMModelName(model.name) else { throw APILLMSettingsError(field: "models.name") }
        guard modelIdentities.insert(llmModelIdentity(model.provider, model.name)).inserted else {
            throw APILLMSettingsError(field: "models.name")
        }
        if !model.provider.isEmpty, !providerNames.contains(model.provider) {
            throw APILLMSettingsError(field: "models.provider")
        }
        guard isValidLLMMoney(model.inputPer1M), isValidLLMMoney(model.outputPer1M),
              isValidLLMMoney(model.embeddingPer1M) else {
            throw APILLMSettingsError(field: "models.prices")
        }
        guard (0...65536).contains(model.dimensions),
              model.dimensions <= llmStorageDimensions
              || model.truncatable || model.supportsCustomDimensions else {
            throw APILLMSettingsError(field: "models.dimensions")
        }
    }

    if let defaults = input.defaults {
        for (target, embed) in [(defaults.text, false), (defaults.embedding, true)] {
            if target.provider.isEmpty, target.model.isEmpty { continue }
            try validateLLMRoute(target.provider, target.model,
                                 providers: input.providers, models: input.models,
                                 embed: embed, field: "defaults")
        }
    }

    var seenTasks = Set<APILLMTask>()
    for task in input.tasks {
        guard seenTasks.insert(task.task).inserted else { throw APILLMSettingsError(field: "tasks") }
        let route: APILLMDefaultRoute
        switch task.mode {
        case .none:
            // Legacy explicit route: empty means off, a half-configured
            // route is rejected below.
            route = APILLMDefaultRoute(provider: task.provider, model: task.model)
        case .custom:
            route = APILLMDefaultRoute(provider: task.provider, model: task.model)
            guard !route.provider.isEmpty, !route.model.isEmpty else {
                throw APILLMSettingsError(field: "tasks.\(task.task.rawValue)")
            }
        case .off:
            route = APILLMDefaultRoute()
        case .default:
            guard let defaults = input.defaults else {
                throw APILLMSettingsError(field: "tasks.\(task.task.rawValue)")
            }
            route = task.task == .embed ? defaults.embedding : defaults.text
            guard !route.provider.isEmpty, !route.model.isEmpty else {
                throw APILLMSettingsError(field: "tasks.\(task.task.rawValue)")
            }
        }
        if route.provider.isEmpty, route.model.isEmpty { continue }
        try validateLLMRoute(route.provider, route.model,
                             providers: input.providers, models: input.models,
                             embed: task.task == .embed,
                             field: "tasks.\(task.task.rawValue)")
    }
}

/// Provider plus name. Model names cannot contain control characters, so the
/// separator is unambiguous and mirrors the server's key.
public func llmModelIdentity(_ provider: String, _ name: String) -> String {
    provider.isEmpty ? name : provider + "\u{0}" + name
}

/// A route names a configured provider and a model. A provider-scoped model
/// entry wins over a legacy global one with the same name.
func llmScopedModel(_ models: [APILLMModelSettings], provider: String, name: String) -> APILLMModelSettings? {
    models.first { $0.provider == provider && $0.name == name }
        ?? models.first { $0.provider.isEmpty && $0.name == name }
}

private func validateLLMRoute(_ provider: String, _ model: String,
                              providers: [APILLMProviderInput], models: [APILLMModelSettings],
                              embed: Bool, field: String) throws {
    guard !provider.isEmpty, !model.isEmpty,
          providers.contains(where: { $0.name == provider }),
          llmScopedModel(models, provider: provider, name: model) != nil else {
        throw APILLMSettingsError(field: field)
    }
    if embed, providers.first(where: { $0.name == provider })?.kind == .anthropic {
        throw APILLMSettingsError(field: field)
    }
}

func isControlScalar(_ scalar: Unicode.Scalar) -> Bool {
    CharacterSet.controlCharacters.contains(scalar)
}

/// Lowercase ASCII letters and digits joined by single dashes, 1-64 bytes.
func isValidLLMProviderName(_ name: String) -> Bool {
    guard !name.isEmpty, name.utf8.count <= 64,
          !name.hasPrefix("-"), !name.hasSuffix("-"), !name.contains("--") else { return false }
    return name.unicodeScalars.allSatisfy { scalar in
        (scalar.value >= 97 && scalar.value <= 122) || (scalar.value >= 48 && scalar.value <= 57) || scalar.value == 45
    }
}

/// 1-256 bytes without whitespace or control characters.
func isValidLLMModelName(_ name: String) -> Bool {
    guard !name.isEmpty, name.utf8.count <= 256 else { return false }
    return !name.unicodeScalars.contains { CharacterSet.whitespacesAndNewlines.contains($0) || isControlScalar($0) }
}

/// A nonnegative decimal with at most six places, within the server's money
/// bound. The digits are parsed as an integer count of millionths, so no
/// floating-point rounding can occur.
public func isValidLLMMoney(_ text: String) -> Bool {
    let trimmed = text.trimmingCharacters(in: .whitespaces)
    guard !trimmed.isEmpty, !trimmed.hasPrefix("-") else { return false }
    let body = trimmed.hasPrefix("+") ? String(trimmed.dropFirst()) : trimmed
    let parts = body.split(separator: ".", maxSplits: 1, omittingEmptySubsequences: false)
    let whole = parts.count > 0 ? String(parts[0]) : ""
    let fraction = parts.count > 1 ? String(parts[1]) : ""
    guard !whole.isEmpty || !fraction.isEmpty, fraction.count <= 6 else { return false }
    let digits = whole + fraction
    guard digits.unicodeScalars.allSatisfy({ $0.value >= 48 && $0.value <= 57 }) else { return false }
    let padded = fraction + String(repeating: "0", count: max(0, 6 - fraction.count))
    guard let micros = Int64(whole + padded) else { return false }
    return micros <= llmSettingsMaxMoneyMicros
}

/// HTTP(S) with a host and no credentials, query or fragment. Trailing slashes
/// are trimmed, matching the server, so a re-saved URL compares equal.
func isValidLLMBaseURL(_ raw: String) -> Bool {
    let base = normalizeLLMBaseURL(raw)
    guard base.utf8.count <= 2048, let components = URLComponents(string: base) else { return false }
    guard let scheme = components.scheme?.lowercased(), scheme == "http" || scheme == "https",
          let host = components.host, !host.isEmpty,
          components.user == nil, components.password == nil,
          components.query == nil, components.fragment == nil else { return false }
    if let port = components.port, !(1...65535).contains(port) { return false }
    return true
}

func normalizeLLMBaseURL(_ raw: String) -> String {
    raw.trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "/"))
}

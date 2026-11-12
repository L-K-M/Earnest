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
    public var provider: String
    public var model: String

    enum CodingKeys: String, CodingKey { case task, provider, model }

    public init(task: APILLMTask, provider: String, model: String) {
        self.task = task
        self.provider = provider
        self.model = model
    }
}

/// A model's prices and embedding geometry. Prices are decimal strings, never
/// floating point, so an exact configured price can never drift.
public struct APILLMModelSettings: Codable, Sendable, Equatable {
    public var name: String
    public var inputPer1M: String
    public var outputPer1M: String
    public var embeddingPer1M: String
    public var dimensions: Int64
    public var truncatable: Bool

    enum CodingKeys: String, CodingKey {
        case name
        case inputPer1M = "input_per_1m"
        case outputPer1M = "output_per_1m"
        case embeddingPer1M = "embedding_per_1m"
        case dimensions, truncatable
    }

    public init(name: String, inputPer1M: String, outputPer1M: String, embeddingPer1M: String,
                dimensions: Int64, truncatable: Bool) {
        self.name = name
        self.inputPer1M = inputPer1M
        self.outputPer1M = outputPer1M
        self.embeddingPer1M = embeddingPer1M
        self.dimensions = dimensions
        self.truncatable = truncatable
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

    enum CodingKeys: String, CodingKey {
        case id, name, chat, embedding
        case inputPer1M = "input_per_1m"
        case outputPer1M = "output_per_1m"
        case embeddingPer1M = "embedding_per_1m"
        case dimensions, truncatable
    }

    public init(id: String, name: String, chat: Bool? = nil, embedding: Bool? = nil,
                inputPer1M: String? = nil, outputPer1M: String? = nil,
                embeddingPer1M: String? = nil, dimensions: Int64? = nil,
                truncatable: Bool? = nil) {
        self.id = id
        self.name = name
        self.chat = chat
        self.embedding = embedding
        self.inputPer1M = inputPer1M
        self.outputPer1M = outputPer1M
        self.embeddingPer1M = embeddingPer1M
        self.dimensions = dimensions
        self.truncatable = truncatable
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

    enum CodingKeys: String, CodingKey { case revision, providers, tasks, models }

    public init(revision: Int64, providers: [APILLMProviderSettings], tasks: [APILLMTaskSettings],
                models: [APILLMModelSettings]) {
        self.revision = revision
        self.providers = providers
        self.tasks = tasks
        self.models = models
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

    enum CodingKeys: String, CodingKey {
        case revision
        case opID = "op_id"
        case providers, tasks, models
    }

    public init(revision: Int64, opID: String, providers: [APILLMProviderInput],
                tasks: [APILLMTaskSettings], models: [APILLMModelSettings]) {
        self.revision = revision
        self.opID = opID
        self.providers = providers
        self.tasks = tasks
        self.models = models
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

    var modelNames = Set<String>()
    for model in input.models {
        guard isValidLLMModelName(model.name) else { throw APILLMSettingsError(field: "models.name") }
        guard modelNames.insert(model.name).inserted else { throw APILLMSettingsError(field: "models.name") }
        guard isValidLLMMoney(model.inputPer1M), isValidLLMMoney(model.outputPer1M),
              isValidLLMMoney(model.embeddingPer1M) else {
            throw APILLMSettingsError(field: "models.prices")
        }
        guard (0...65536).contains(model.dimensions),
              model.dimensions <= llmStorageDimensions || model.truncatable else {
            throw APILLMSettingsError(field: "models.dimensions")
        }
    }

    var seenTasks = Set<APILLMTask>()
    for task in input.tasks {
        guard seenTasks.insert(task.task).inserted else { throw APILLMSettingsError(field: "tasks") }
        if task.provider.isEmpty, task.model.isEmpty { continue }
        guard !task.provider.isEmpty, !task.model.isEmpty,
              providerNames.contains(task.provider), modelNames.contains(task.model) else {
            throw APILLMSettingsError(field: "tasks.\(task.task.rawValue)")
        }
        if task.task == .embed,
           let provider = input.providers.first(where: { $0.name == task.provider }),
           provider.kind == .anthropic {
            throw APILLMSettingsError(field: "tasks.embed")
        }
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

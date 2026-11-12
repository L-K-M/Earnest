import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import Testing
@testable import EarnestKit

private func validInput(revision: Int64 = 3, opID: String = "configure") -> APILLMSettingsInput {
    APILLMSettingsInput(
        revision: revision,
        opID: opID,
        providers: [
            APILLMProviderInput(name: "openai", kind: .openai, baseURL: "https://api.openai.com/v1",
                                monthlyLimitUSD: "20.000000", timeoutSeconds: 60,
                                tokenField: .maxCompletionTokens),
            APILLMProviderInput(name: "local", kind: .openai, baseURL: "http://nas.local:11434/v1",
                                apiKey: "sk-local", monthlyLimitUSD: "0", timeoutSeconds: 30,
                                tokenField: .maxTokens),
        ],
        tasks: [
            APILLMTaskSettings(task: .embed, provider: "openai", model: "text-embedding-3-small"),
            APILLMTaskSettings(task: .blurb, provider: "local", model: "local-chat"),
        ]
        + APILLMTask.allCases.filter { $0 != .embed && $0 != .blurb }
            .map { APILLMTaskSettings(task: $0, provider: "", model: "") },
        models: [
            APILLMModelSettings(name: "text-embedding-3-small", inputPer1M: "0.000000",
                                outputPer1M: "0.000000", embeddingPer1M: "0.020000",
                                dimensions: 1536, truncatable: true),
            APILLMModelSettings(name: "local-chat", inputPer1M: "0", outputPer1M: "0",
                                embeddingPer1M: "0", dimensions: 0, truncatable: false),
        ]
    )
}

/// Each suite owns a harness: URLProtocol classes are per-session, but a
/// shared scripted registry would race other suites in this test target.
final class LLMHTTP: URLProtocol {
    struct Reply {
        var method = "GET"
        let path: String
        var status = 200
        let body: String
        var checkBody: ((Data?) throws -> Void)?
    }
    private static let lock = NSLock()
    nonisolated(unsafe) private static var replies: [Reply] = []

    static func install(_ replies: [Reply]) -> URLSession {
        lock.withLock { Self.replies = replies }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [Self.self]
        return URLSession(configuration: configuration)
    }
    static func assertFinished() { #expect(lock.withLock { replies.isEmpty }) }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let reply = Self.lock.withLock { Self.replies.isEmpty ? nil : Self.replies.removeFirst() }
        guard let reply else {
            Issue.record("Unexpected HTTP request: \(request.url?.path ?? "")")
            client?.urlProtocol(self, didFailWithError: APIError.invalidResponse)
            return
        }
        #expect(request.url?.path == reply.path)
        #expect(request.httpMethod == reply.method)
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test-token")
        do { try reply.checkBody?(Self.body(of: request)) }
        catch { Issue.record("Invalid HTTP body: \(error)") }
        let response = HTTPURLResponse(url: request.url!, statusCode: reply.status,
                                       httpVersion: "HTTP/1.1",
                                       headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(reply.body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}

    private static func body(of request: URLRequest) -> Data? {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while true {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            data.append(buffer, count: count)
        }
        return data
    }
}

@Suite(.serialized) struct LLMSettingsTests {
    // MARK: - Wire shape

    @Test func redactedSettingsDecodeWithoutKeys() throws {
        let data = Data(#"{"revision":2,"providers":[{"name":"openai","kind":"openai","base_url":"https://api.openai.com/v1","has_api_key":true,"monthly_limit_usd":"20.000000","timeout_seconds":60,"token_field":"max_completion_tokens"}],"tasks":[{"task":"embed","provider":"openai","model":"text-embedding-3-small"},{"task":"classify","provider":"","model":""},{"task":"blurb","provider":"","model":""},{"task":"summary","provider":"","model":""},{"task":"edition","provider":"","model":""},{"task":"compile_rule","provider":"","model":""},{"task":"ask","provider":"","model":""}],"models":[{"name":"text-embedding-3-small","input_per_1m":"0.000000","output_per_1m":"0.000000","embedding_per_1m":"0.020000","dimensions":1536,"truncatable":true}]}"#.utf8)
        let settings = try JSONDecoder().decode(APILLMSettings.self, from: data)
        #expect(settings.revision == 2)
        #expect(settings.providers.count == 1)
        #expect(settings.providers[0].hasAPIKey)
        #expect(settings.providers[0].kind == .openai)
        #expect(settings.providers[0].tokenField == .maxCompletionTokens)
        #expect(settings.tasks.count == 7)
        #expect(settings.tasks.map(\.task) == APILLMTask.allCases)
        #expect(settings.tasks.first?.provider == "openai")
        #expect(settings.models[0].embeddingPer1M == "0.020000")
        #expect(settings.models[0].dimensions == 1536)
    }

    @Test func providerInputEncodingOmitsAnAbsentKey() throws {
        var input = validInput()
        input.providers[0].apiKey = nil
        input.providers[1].apiKey = ""
        input.providers[1].clearAPIKey = true
        let fields = try JSONDecoder().decode(APIValue.self, from: JSONEncoder().encode(input))
        let providers = try #require(fields["providers"]?.arrayValue)
        #expect(providers.count == 2)
        #expect(providers[0]["api_key"] == nil, "an absent replacement must omit the key")
        #expect(providers[1]["api_key"]?.stringValue == "", "an empty replacement keeps the stored key")
        #expect(providers[1]["clear_api_key"]?.boolValue == true)
        #expect(try #require(fields["op_id"]?.stringValue) == "configure")
        #expect(fields["revision"]?.int64Value == 3)
    }

    @Test func llmSettingsTransportRoundTrips() async throws {
        let session = LLMHTTP.install([
            .init(path: "/v1/settings/llm", body: #"{"revision":0,"providers":[],"tasks":[{"task":"embed","provider":"","model":""},{"task":"classify","provider":"","model":""},{"task":"blurb","provider":"","model":""},{"task":"summary","provider":"","model":""},{"task":"edition","provider":"","model":""},{"task":"compile_rule","provider":"","model":""},{"task":"ask","provider":"","model":""}],"models":[]}"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let settings = try await api.llmSettings()
        #expect(settings.revision == 0)
        #expect(settings.providers.isEmpty)
        #expect(settings.tasks.count == 7)
        LLMHTTP.assertFinished()
    }

    @Test func llmSaveSendsWholeReplacementWithFrozenKeys() async throws {
        let session = LLMHTTP.install([
            .init(method: "PUT", path: "/v1/settings/llm",
                  body: #"{"revision":4,"providers":[{"name":"local","kind":"openai","base_url":"http://nas.local:11434/v1","has_api_key":true,"monthly_limit_usd":"0","timeout_seconds":30,"token_field":"max_tokens"}],"tasks":[{"task":"embed","provider":"","model":""},{"task":"classify","provider":"","model":""},{"task":"blurb","provider":"local","model":"local-chat"},{"task":"summary","provider":"","model":""},{"task":"edition","provider":"","model":""},{"task":"compile_rule","provider":"","model":""},{"task":"ask","provider":"","model":""}],"models":[{"name":"local-chat","input_per_1m":"0","output_per_1m":"0","embedding_per_1m":"0","dimensions":0,"truncatable":false}]}"#,
                  checkBody: { body in
                      let fields = try JSONDecoder().decode(APIValue.self, from: try #require(body))
                      #expect(fields["revision"]?.int64Value == 3)
                      let providers = try #require(fields["providers"]?.arrayValue)
                      #expect(providers.count == 2)
                      #expect(providers[0]["api_key"] == nil, "keeping the saved key must not send it")
                      #expect(providers[1]["api_key"]?.stringValue == "sk-local")
                      #expect(try #require(fields["op_id"]?.stringValue) == "configure")
                      let tasks = try #require(fields["tasks"]?.arrayValue)
                      #expect(tasks.count == 7)
                  }),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let saved = try await api.updateLLMSettings(validInput())
        #expect(saved.revision == 4)
        #expect(saved.providers[0].hasAPIKey)
        LLMHTTP.assertFinished()
    }

    @Test func discoveredModelDecodesUnknownFieldsAsAbsent() throws {
        let sparse = try JSONDecoder().decode(APIDiscoveredModel.self, from: Data(#"{"id":"m1","name":"Model"}"#.utf8))
        #expect(sparse.id == "m1" && sparse.name == "Model")
        #expect(sparse.chat == nil && sparse.embedding == nil, "unknown capability is never false")
        #expect(sparse.inputPer1M == nil && sparse.outputPer1M == nil && sparse.embeddingPer1M == nil,
                "unknown price is never zero")
        #expect(sparse.dimensions == nil && sparse.truncatable == nil)

        let full = try JSONDecoder().decode(APIDiscoveredModel.self, from: Data(
            #"{"id":"e","name":"E","chat":false,"embedding":true,"embedding_per_1m":"0.020000","dimensions":1536,"truncatable":true}"#.utf8))
        #expect(full.chat == false && full.embedding == true)
        #expect(full.embeddingPer1M == "0.020000" && full.dimensions == 1536 && full.truncatable == true)
        #expect(full.inputPer1M == nil, "absent fields stay absent, not zero")
    }

    @Test func discoveredModelDecodesCustomDimensionSupport() throws {
        let model = try JSONDecoder().decode(APIDiscoveredModel.self, from: Data(
            #"{"id":"e","name":"E","supports_custom_dimensions":true}"#.utf8))
        #expect(model.supportsCustomDimensions == true)
        #expect(model.dimensions == nil, "resizing support is not a native width")
    }

    @Test func defaultsModesAndScopedModelsRoundTrip() throws {
        var input = validInput()
        input.defaults = APILLMDefaults(
            text: APILLMDefaultRoute(provider: "local", model: "local-chat"),
            embedding: APILLMDefaultRoute(provider: "openai", model: "text-embedding-3-small"))
        input.tasks[0].mode = .default
        input.tasks[1].mode = .default
        input.tasks[2].mode = .off
        input.models[1].provider = "local"
        input.models[1].supportsCustomDimensions = true
        let fields = try JSONDecoder().decode(APIValue.self, from: JSONEncoder().encode(input))
        #expect(fields["defaults"]?["text"]?["provider"]?.stringValue == "local")
        #expect(fields["defaults"]?["embedding"]?["model"]?.stringValue == "text-embedding-3-small")
        let tasks = try #require(fields["tasks"]?.arrayValue)
        #expect(tasks[0]["mode"]?.stringValue == "default")
        #expect(tasks[2]["mode"]?.stringValue == "off")
        let models = try #require(fields["models"]?.arrayValue)
        #expect(models[1]["provider"]?.stringValue == "local")
        #expect(models[1]["supports_custom_dimensions"]?.boolValue == true)
    }

    @Test func legacySettingsDecodeWithoutNewFields() throws {
        let data = Data(#"{"revision":1,"providers":[],"tasks":[{"task":"embed","provider":"p","model":"m"},{"task":"classify","provider":"","model":""},{"task":"blurb","provider":"","model":""},{"task":"summary","provider":"","model":""},{"task":"edition","provider":"","model":""},{"task":"compile_rule","provider":"","model":""},{"task":"ask","provider":"","model":""}],"models":[{"name":"m","input_per_1m":"0","output_per_1m":"0","embedding_per_1m":"0","dimensions":0,"truncatable":false}]}"#.utf8)
        let settings = try JSONDecoder().decode(APILLMSettings.self, from: data)
        #expect(settings.defaults == nil)
        #expect(settings.tasks[0].mode == nil, "a missing mode is the legacy explicit route")
        #expect(settings.models[0].provider.isEmpty)
        #expect(settings.models[0].supportsCustomDimensions == false)
    }

    @Test func checkModelPostsRevisionProviderModelAndCapability() async throws {
        let session = LLMHTTP.install([
            .init(method: "POST", path: "/v1/settings/llm/models/check",
                  body: #"{"compatible":true,"dimensions":4096,"supports_custom_dimensions":true,"message":"Embedding model checked successfully."}"#,
                  checkBody: { body in
                      let fields = try JSONDecoder().decode(APIValue.self, from: try #require(body))
                      #expect(fields["revision"]?.int64Value == 7)
                      #expect(fields["capability"]?.stringValue == "embedding")
                      let provider = try #require(fields["provider"])
                      #expect(provider["name"]?.stringValue == "openai")
                      #expect(provider["base_url"]?.stringValue == "https://api.openai.com/v1")
                      let model = try #require(fields["model"])
                      #expect(model["name"]?.stringValue == "text-embedding-3-small")
                      #expect(model["provider"]?.stringValue == "openai")
                      #expect(model["supports_custom_dimensions"]?.boolValue == true)
                  }),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let provider = APILLMProviderInput(name: "openai", kind: .openai,
                                           baseURL: "https://api.openai.com/v1",
                                           monthlyLimitUSD: "0", timeoutSeconds: 60,
                                           tokenField: .maxCompletionTokens)
        let model = APILLMModelSettings(name: "text-embedding-3-small", provider: "openai",
                                        supportsCustomDimensions: true,
                                        inputPer1M: "0", outputPer1M: "0", embeddingPer1M: "0.02",
                                        dimensions: 4096, truncatable: false)
        let result = try await api.checkLLMModel(revision: 7, provider: provider,
                                                 model: model, capability: .embedding)
        #expect(result.compatible)
        #expect(result.dimensions == 4096)
        #expect(result.supportsCustomDimensions == true)
        #expect(result.message.contains("checked"))
        LLMHTTP.assertFinished()
    }

    @Test func llmModelDiscoveryPostsRevisionAndOneProvider() async throws {
        let session = LLMHTTP.install([
            .init(method: "POST", path: "/v1/settings/llm/models",
                  body: #"{"models":[{"id":"m","name":"M","chat":true},{"id":"e","name":"E"}]}"#,
                  checkBody: { body in
                      let fields = try JSONDecoder().decode(APIValue.self, from: try #require(body))
                      #expect(fields["revision"]?.int64Value == 2)
                      let provider = try #require(fields["provider"])
                      #expect(provider["name"]?.stringValue == "openai")
                      #expect(provider["kind"]?.stringValue == "openai")
                      #expect(provider["base_url"]?.stringValue == "https://api.openai.com/v1")
                      #expect(provider["api_key"] == nil, "a blank draft key is omitted for inheritance")
                      #expect(provider["clear_api_key"]?.boolValue == false)
                      #expect(fields["providers"] == nil && fields["tasks"] == nil && fields["models"] == nil,
                              "discovery is a provider preview, not a settings save")
                  }),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let provider = APILLMProviderInput(name: "openai", kind: .openai, baseURL: "https://api.openai.com/v1",
                                           monthlyLimitUSD: "0", timeoutSeconds: 60,
                                           tokenField: .maxCompletionTokens)
        let models = try await api.discoverLLMModels(revision: 2, provider: provider)
        #expect(models.count == 2)
        #expect(models[0].id == "m" && models[0].chat == true)
        #expect(models[1].chat == nil)
        LLMHTTP.assertFinished()
    }

    @Test func llmModelDiscoverySendsATypedDraftKey() async throws {
        let session = LLMHTTP.install([
            .init(method: "POST", path: "/v1/settings/llm/models", body: #"{"models":[]}"#,
                  checkBody: { body in
                      let fields = try JSONDecoder().decode(APIValue.self, from: try #require(body))
                      #expect(fields["provider"]?["api_key"]?.stringValue == "sk-draft",
                              "the preview uses the draft exactly as a save would")
                  }),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let provider = APILLMProviderInput(name: "local", kind: .openai, baseURL: "http://nas.local:11434/v1",
                                           apiKey: "sk-draft", monthlyLimitUSD: "0", timeoutSeconds: 30,
                                           tokenField: .maxTokens)
        let models = try await api.discoverLLMModels(revision: 0, provider: provider)
        #expect(models.isEmpty)
        LLMHTTP.assertFinished()
    }

    @Test func llmModelDiscoveryConflictIsAProblem() async throws {
        let session = LLMHTTP.install([
            .init(method: "POST", path: "/v1/settings/llm/models", status: 409,
                  body: #"{"type":"about:blank","title":"Conflict","status":409,"detail":"LLM settings changed; reload before fetching models"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let provider = APILLMProviderInput(name: "openai", kind: .openai, baseURL: "https://api.openai.com/v1",
                                           monthlyLimitUSD: "0", timeoutSeconds: 60,
                                           tokenField: .maxCompletionTokens)
        do {
            _ = try await api.discoverLLMModels(revision: 1, provider: provider)
            Issue.record("Expected a conflict")
        } catch let error as APIProblem {
            #expect(error.status == 409)
        }
        LLMHTTP.assertFinished()
    }

    @Test func llmConflictIsAProblem() async throws {
        let session = LLMHTTP.install([
            .init(method: "PUT", path: "/v1/settings/llm", status: 409,
                  body: #"{"type":"about:blank","title":"Conflict","status":409,"detail":"LLM settings changed; reload before saving"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        do {
            _ = try await api.updateLLMSettings(validInput())
            Issue.record("Expected a conflict")
        } catch let error as APIProblem {
            #expect(error.status == 409)
        }
        LLMHTTP.assertFinished()
    }

    // MARK: - Validation

    @Test func validationAcceptsAConfiguredExample() throws {
        try validateLLMSettings(validInput())
    }

    @Test func validationRejectsBadProviderNames() {
        for name in ["", "-openai", "openai-", "open--ai", "OpenAI", "open_ai", "open ai"] {
            var input = validInput()
            input.providers[0].name = name
            #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(input) }
        }
    }

    @Test func validationRejectsBadBaseURLs() {
        for url in ["", "ftp://example.test/v1", "https://", "https://user@example.test/v1",
                    "https://example.test/v1?token=x", "https://example.test/v1#frag",
                    "https://example.test:0/v1", "not a url"] {
            var input = validInput()
            input.providers[0].baseURL = url
            #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(input) }
        }
    }

    @Test func validationRejectsBadMoneyAndTimeouts() {
        for amount in ["", "-1", "1.1234567", "1e5", "abc", "1,5", "100000000"] {
            var input = validInput()
            input.providers[0].monthlyLimitUSD = amount
            #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(input) }
        }
        for timeout in [0, 601, -1] {
            var input = validInput()
            input.providers[0].timeoutSeconds = Int64(timeout)
            #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(input) }
        }
    }

    @Test func validationAcceptsExactMoneyBoundaries() throws {
        var input = validInput()
        input.providers[0].monthlyLimitUSD = "99999999.999999"
        input.models[0].inputPer1M = "0.000001"
        try validateLLMSettings(input)
    }

    @Test func validationRejectsDuplicateNamesAndUnknownTasks() {
        var duplicate = validInput()
        duplicate.providers[1].name = "openai"
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(duplicate) }

        var duplicateModel = validInput()
        duplicateModel.models[1].name = "text-embedding-3-small"
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(duplicateModel) }
    }

    @Test func validationRejectsHalfConfiguredTasks() {
        var missingModel = validInput()
        missingModel.tasks[0].model = ""
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(missingModel) }

        var missingProvider = validInput()
        missingProvider.tasks[0].provider = ""
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(missingProvider) }

        var unknownProvider = validInput()
        unknownProvider.tasks[0].provider = "gemini-provider"
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(unknownProvider) }

        var unknownModel = validInput()
        unknownModel.tasks[0].model = "not-configured"
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(unknownModel) }
    }

    @Test func validationRejectsAnthropicEmbeddings() {
        var input = validInput()
        input.providers[0].kind = .anthropic
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(input) }
    }

    @Test func validationRejectsClearAndReplaceTogether() {
        var input = validInput()
        input.providers[1].clearAPIKey = true
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(input) }
    }

    @Test func validationRejectsDimensionsWithoutTruncation() {
        var input = validInput()
        input.models[0].truncatable = false
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(input) }

        var over = validInput()
        over.models[0].dimensions = 65537
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(over) }
    }

    @Test func validationRejectsCustomModeWithoutARoute() {
        var input = validInput()
        input.tasks[1].mode = .custom
        input.tasks[1].provider = ""
        input.tasks[1].model = ""
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(input) }
    }

    @Test func validationAcceptsWideModelsThatTruncateOrResize() throws {
        var resized = validInput()
        resized.models[0].dimensions = 4096
        resized.models[0].truncatable = false
        resized.models[0].supportsCustomDimensions = true
        try validateLLMSettings(resized)

        var truncated = validInput()
        truncated.models[0].dimensions = 4096
        truncated.models[0].truncatable = true
        truncated.models[0].supportsCustomDimensions = false
        try validateLLMSettings(truncated)
    }

    @Test func validationResolvesDefaultRoutesAndRejectsMissingOnes() throws {
        var input = validInput()
        input.defaults = APILLMDefaults(
            text: APILLMDefaultRoute(provider: "local", model: "local-chat"),
            embedding: APILLMDefaultRoute(provider: "openai", model: "text-embedding-3-small"))
        input.tasks[0].mode = .default
        input.tasks[1].mode = .default
        try validateLLMSettings(input)

        var noDefaults = input
        noDefaults.defaults = nil
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(noDefaults) }

        var unknownModel = input
        unknownModel.defaults?.text.model = "not-configured"
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(unknownModel) }

        var anthropicEmbed = input
        anthropicEmbed.defaults?.embedding = APILLMDefaultRoute(provider: "local", model: "local-chat")
        anthropicEmbed.providers[1].kind = .anthropic
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(anthropicEmbed) }
    }

    @Test func validationUsesProviderScopedModelIdentity() throws {
        var input = validInput()
        input.models[1].provider = "local"
        try validateLLMSettings(input)

        // The same name under a different provider is a different identity.
        var sameName = validInput()
        sameName.models.append(APILLMModelSettings(name: "text-embedding-3-small", provider: "local",
                                                   inputPer1M: "0", outputPer1M: "0", embeddingPer1M: "0",
                                                   dimensions: 0, truncatable: false))
        try validateLLMSettings(sameName)

        var duplicate = validInput()
        duplicate.models.append(APILLMModelSettings(name: "text-embedding-3-small", provider: "",
                                                   inputPer1M: "0", outputPer1M: "0", embeddingPer1M: "0",
                                                   dimensions: 0, truncatable: false))
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(duplicate) }

        var unknownProvider = validInput()
        unknownProvider.models[1].provider = "missing"
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(unknownProvider) }
    }

    @Test func validationRejectsTooManyEntries() {
        var input = validInput()
        input.providers = (0..<33).map {
            APILLMProviderInput(name: "p\($0)", kind: .openai, baseURL: "https://example.test/v1",
                                monthlyLimitUSD: "0", timeoutSeconds: 60, tokenField: .maxCompletionTokens)
        }
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(input) }

        var models = validInput()
        models.models = (0..<129).map {
            APILLMModelSettings(name: "m\($0)", inputPer1M: "0", outputPer1M: "0",
                                embeddingPer1M: "0", dimensions: 0, truncatable: false)
        }
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(models) }
    }

    @Test func validationRequiresAKeyWhenTheDestinationChanged() throws {
        let previous = APILLMSettings(
            revision: 3,
            providers: [APILLMProviderSettings(name: "openai", kind: .openai,
                                               baseURL: "https://api.openai.com/v1", hasAPIKey: true,
                                               monthlyLimitUSD: "0", timeoutSeconds: 60,
                                               tokenField: .maxCompletionTokens)],
            tasks: [],
            models: []
        )
        var moved = validInput()
        moved.providers[0].baseURL = "https://other.example/v1"
        #expect(throws: APILLMSettingsError.self) { try validateLLMSettings(moved, previous: previous) }

        // An explicit replacement or removal is accepted.
        moved.providers[0].apiKey = "sk-new"
        try validateLLMSettings(moved, previous: previous)
        moved.providers[0].apiKey = nil
        moved.providers[0].clearAPIKey = true
        try validateLLMSettings(moved, previous: previous)

        // Keeping the same destination may keep the stored key.
        var same = validInput()
        same.providers[0].baseURL = "https://api.openai.com/v1/"
        try validateLLMSettings(same, previous: previous)
    }
}

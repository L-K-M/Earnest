import Foundation
import XCTest
import EarnestKit
@testable import Earnest

/// Provider identity: a provider's name and its routed name are both
/// reserved. Renaming one provider onto a sibling's identity must be rejected
/// whole, and clearing a name is a transient edit that keeps the routed name
/// reserved for the provider that still uses it.
@MainActor final class LLMSettingsIdentityTests: XCTestCase {
    private func settings() -> LLMSettingsModel {
        let app = AppModel(engine: SyncEngine(store: MemorySyncStore()), principalID: 7)
        app.loggedIn = true
        return LLMSettingsModel(app: app)
    }

    private func provider(_ name: String, kind: APILLMProviderKind = .openai) -> LLMProviderDraft {
        LLMProviderDraft(settings: APILLMProviderSettings(
            name: name, kind: kind, baseURL: kind.defaultBaseURL, hasAPIKey: false,
            monthlyLimitUSD: "0", timeoutSeconds: 60, tokenField: .maxCompletionTokens))
    }

    private func scopedModel(_ name: String, provider: String, price: String) -> LLMModelDraft {
        var model = LLMModelDraft()
        model.name = name
        model.provider = provider
        model.inputPer1M = price
        model.outputPer1M = price
        return model
    }

    func testRenamingOntoASiblingNameIsRejectedAndRewritesNothing() {
        let settings = settings()
        settings.draft.providers = [provider("alpha"), provider("beta")]
        let alphaID = settings.draft.providers[0].id
        settings.importModels([scopedModel("alpha-model", provider: "alpha", price: "1"),
                               scopedModel("beta-model", provider: "beta", price: "2")])
        settings.route(task: .blurb, providerName: "alpha", modelName: "alpha-model")
        settings.route(task: .summary, providerName: "beta", modelName: "beta-model")
        settings.setDefaultRoute(provider: "alpha", model: "alpha-model", usage: .chat)
        settings.setDefaultRoute(provider: "beta", model: "beta-model", usage: .embedding)
        let before = settings.draft

        settings.renameProvider(id: alphaID, name: "beta")

        XCTAssertNotNil(settings.error, "a colliding rename is reported, not applied")
        XCTAssertEqual(settings.draft, before, "a colliding rename leaves the whole draft unchanged")
        XCTAssertEqual(settings.draft.providers[0].name, "alpha")
        XCTAssertEqual(settings.draft.providers[0].routedName, "alpha")
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .blurb }?.provider, "alpha")
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .summary }?.provider, "beta")
        XCTAssertEqual(settings.draft.defaults?.text.provider, "alpha")
        XCTAssertEqual(settings.draft.defaults?.embedding.provider, "beta")
        XCTAssertEqual(settings.draft.model(provider: "alpha", name: "alpha-model")?.inputPer1M, "1")
        XCTAssertEqual(settings.draft.model(provider: "beta", name: "beta-model")?.inputPer1M, "2",
                       "the sibling's scoped price is untouched")

        settings.renameProvider(id: alphaID, name: "gamma")
        XCTAssertNil(settings.error, "correcting the collision clears its error")
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .blurb }?.provider, "gamma")
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .summary }?.provider, "beta")
    }

    func testRenamingOntoASiblingRoutedNameIsRejected() {
        let settings = settings()
        settings.draft.providers = [provider("alpha"), provider("beta")]
        let alphaID = settings.draft.providers[0].id
        let betaID = settings.draft.providers[1].id
        // Clearing the sibling's editable name keeps its routed identity.
        settings.renameProvider(id: betaID, name: "")
        XCTAssertEqual(settings.draft.providers[1].name, "")
        XCTAssertEqual(settings.draft.providers[1].routedName, "beta")
        settings.importModels([scopedModel("beta-model", provider: "beta", price: "2")])
        settings.route(task: .blurb, providerName: "beta", modelName: "beta-model")
        settings.setDefaultRoute(provider: "beta", model: "beta-model", usage: .chat)
        let before = settings.draft

        settings.renameProvider(id: alphaID, name: "beta")

        XCTAssertNotNil(settings.error)
        XCTAssertEqual(settings.draft, before)
        XCTAssertEqual(settings.draft.providers[0].name, "alpha")
        XCTAssertEqual(settings.draft.model(provider: "beta", name: "beta-model")?.inputPer1M, "2",
                       "the routed sibling's scoped price is untouched")
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .blurb }?.provider, "beta")
        XCTAssertEqual(settings.draft.defaults?.text.provider, "beta")
    }

    func testClearingANameKeepsItReservedForThePreset() {
        let settings = settings()
        let openaiID = settings.addProvider(preset: .openai)
        settings.importModels([scopedModel("openai-model", provider: "openai", price: "1")])
        settings.route(task: .blurb, providerName: "openai", modelName: "openai-model")
        settings.setDefaultRoute(provider: "openai", model: "openai-model", usage: .chat)

        // Clearing the name is a transient edit: the routed name is retained
        // and nothing scoped to it is rewritten.
        settings.renameProvider(id: openaiID, name: "")
        XCTAssertEqual(settings.draft.providers[0].name, "")
        XCTAssertEqual(settings.draft.providers[0].routedName, "openai")
        XCTAssertEqual(settings.draft.model(provider: "openai", name: "openai-model")?.inputPer1M, "1")
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .blurb }?.provider, "openai")
        XCTAssertEqual(settings.draft.defaults?.text.provider, "openai")

        // The preset's base name is still spoken for by the routed identity.
        let addedID = settings.addProvider(preset: .openai)
        XCTAssertEqual(settings.draft.providers[1].name, "openai-2")
        XCTAssertEqual(settings.draft.providers[1].routedName, "openai-2")

        // Removing the new provider must not take the old scoped state with it.
        settings.removeProvider(id: addedID)
        XCTAssertEqual(settings.draft.providers.count, 1)
        XCTAssertEqual(settings.draft.model(provider: "openai", name: "openai-model")?.inputPer1M, "1")
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .blurb }?.provider, "openai")
        XCTAssertEqual(settings.draft.defaults?.text.provider, "openai")
    }

    func testRenamingToAFreshNameStillRewritesRoutesDefaultsAndScopedPrices() {
        let settings = settings()
        let id = settings.addProvider(preset: .venice)
        settings.importModels([scopedModel("venice-model", provider: "venice", price: "0.5")])
        settings.route(task: .blurb, providerName: "venice", modelName: "venice-model")
        settings.setDefaultRoute(provider: "venice", model: "venice-model", usage: .chat)

        settings.renameProvider(id: id, name: "venice-renamed")

        XCTAssertNil(settings.error)
        XCTAssertEqual(settings.draft.providers[0].name, "venice-renamed")
        XCTAssertEqual(settings.draft.providers[0].routedName, "venice-renamed")
        XCTAssertNil(settings.draft.model(provider: "venice", name: "venice-model"))
        XCTAssertEqual(settings.draft.model(provider: "venice-renamed", name: "venice-model")?.inputPer1M, "0.5")
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .blurb }?.provider, "venice-renamed")
        XCTAssertEqual(settings.draft.defaults?.text.provider, "venice-renamed")
    }
}

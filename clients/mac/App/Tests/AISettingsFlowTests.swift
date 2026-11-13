import XCTest
import EarnestKit
@testable import Earnest

@MainActor final class AISettingsFlowTests: XCTestCase {
    private func settings() -> LLMSettingsModel {
        let app = AppModel(engine: SyncEngine(store: MemorySyncStore()), principalID: 7)
        return LLMSettingsModel(app: app)
    }

    func testChoosingACustomProviderKeepsTheModelPickerEnabled() {
        let settings = settings()
        settings.setTaskEnabled(.blurb, true)
        settings.setTaskProvider(.blurb, "venice")
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .blurb }?.mode, .custom)
    }

    func testKnownIncompatibleEmbeddingWidthIsExcluded() {
        let model = APIDiscoveredModel(id: "wide", name: "Wide", embedding: true,
                                      dimensions: 4096, truncatable: false,
                                      supportsCustomDimensions: false)
        XCTAssertFalse(llmModelSuits(model, task: .embed))
    }

    func testDefaultPickerKeepsTheProviderWhileChoosingAModel() {
        let settings = settings()
        settings.setDefaultRoute(provider: "venice", model: "", usage: .embedding)
        XCTAssertEqual(settings.defaultRoute(for: .embed).provider, "venice")
        XCTAssertTrue(settings.defaultRoute(for: .embed).model.isEmpty)
        XCTAssertTrue(settings.draft.tasks.allSatisfy { $0.mode == .off })
    }

    func testRenamingThroughAnEmptyNameKeepsGlobalAndScopedModelsSeparate() {
        let settings = settings()
        let provider = settings.addProvider(preset: .venice)
        var scoped = LLMModelDraft()
        scoped.provider = "venice"
        scoped.name = "shared-id"
        var global = LLMModelDraft()
        global.name = "shared-id"
        settings.importModels([scoped, global])
        settings.setDefaultRoute(provider: "venice", model: "shared-id", usage: .chat)
        settings.renameProvider(id: provider, name: "")
        settings.renameProvider(id: provider, name: "renamed")
        XCTAssertEqual(settings.draft.models.filter { $0.provider.isEmpty }.count, 1)
        XCTAssertEqual(settings.draft.models.filter { $0.provider == "renamed" }.count, 1)
        XCTAssertEqual(settings.defaultRoute(for: .blurb).provider, "renamed")
    }

    func testRemovingProviderDisablesNewDefaultFeatures() {
        let settings = settings()
        let providerID = settings.addProvider(preset: .venice)
        settings.setDefaultRoute(provider: "venice", model: "qwen3", usage: .embedding)
        settings.setTaskEnabled(.embed, true)
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .embed }?.mode, .default)
        settings.removeProvider(id: providerID)
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .embed }?.mode, .off)
        XCTAssertTrue(settings.defaultRoute(for: .embed).provider.isEmpty)
    }

    func testRemovingProviderPreservesUnrelatedIncompleteEdits() throws {
        let settings = settings()
        let providerID = settings.addProvider(preset: .venice)
        settings.setTaskEnabled(.blurb, true)
        let before = try XCTUnwrap(settings.draft.tasks.first { $0.task == .blurb })
        XCTAssertEqual(before.mode, .custom)
        settings.removeProvider(id: providerID)
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .blurb }, before)
    }

    func testCustomRouteSurvivesDisablingAndEnablingAFeature() {
        let settings = settings()
        settings.setDefaultRoute(provider: "default-provider", model: "default-model", usage: .chat)
        let index = settings.draft.tasks.firstIndex { $0.task == .blurb }!
        settings.draft.tasks[index].provider = "custom-provider"
        settings.draft.tasks[index].model = "custom-model"
        settings.draft.tasks[index].mode = .custom
        settings.setTaskEnabled(.blurb, false)
        settings.setTaskEnabled(.blurb, true)
        XCTAssertEqual(settings.draft.tasks[index].mode, .custom)
        XCTAssertEqual(settings.effectiveTaskRoute(settings.draft.tasks[index]).provider, "custom-provider")
    }
}

import Foundation
import Testing
@testable import EarnestKit

@Test func pricingConfirmationSurvivesGeneratedWireRoundTrip() throws {
    let free = APILLMModelSettings(name: "dual", provider: "local",
                                   textPricingConfigured: true, embeddingPricingConfigured: false,
                                   inputPer1M: "0", outputPer1M: "0", embeddingPer1M: "0",
                                   dimensions: 0, truncatable: false)
    let generated: Components.Schemas.LLMModelSettings = try adapt(free)
    let decoded: APILLMModelSettings = try adapt(generated)
    #expect(decoded == free)
    let encoded = try JSONEncoder().encode(decoded)
    let body = try #require(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
    #expect(body["text_pricing_configured"] as? Bool == true)
    #expect(body["embedding_pricing_configured"] as? Bool == false)

    let legacy = Data("""
    {"name":"legacy","input_per_1m":"0","output_per_1m":"0",\
    "embedding_per_1m":"0","dimensions":0,"truncatable":false}
    """.utf8)
    let old = try JSONDecoder().decode(APILLMModelSettings.self, from: legacy)
    #expect(old.textPricingConfigured == nil)
    #expect(old.embeddingPricingConfigured == nil)
}

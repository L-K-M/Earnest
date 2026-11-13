import Foundation
import SwiftUI
import EarnestKit

/// What the catalogue sheet browses: one provider draft as currently edited,
/// and what a selection does. The provider is the destination whose catalogue
/// is fetched; the selection decides whether a pick only adds a model, routes
/// a task, or sets a default.
struct LLMDiscoveryTarget: Identifiable {
    let providerID: UUID
    let providerName: String
    let selection: LLMSettingsModel.LLMModelSelection
    var id: UUID { providerID }

    /// The capability a selection needs. Provider browsing lets the user
    /// choose; every other selection fixes it.
    var fixedUsage: LLMModelUsage? {
        switch selection {
        case .add: return nil
        case .task(let task): return task == .embed ? .embedding : .chat
        case .defaultText: return .chat
        case .defaultEmbedding: return .embedding
        }
    }

    var title: String {
        switch selection {
        case .add: return "Models on \(providerName)"
        case .task(let task): return "Models for \(task.title.lowercased()) on \(providerName)"
        case .defaultText: return "Text default on \(providerName)"
        case .defaultEmbedding: return "Embedding default on \(providerName)"
        }
    }
}

/// A capability badge for a catalogue row: only what the provider reported,
/// with unknowns labelled rather than guessed.
func llmCapabilitySummary(_ model: APIDiscoveredModel) -> String {
    switch (model.chat, model.embedding) {
    case (true, true): return "chat · embedding"
    case (true, false): return "chat"
    case (false, true): return "embedding"
    case (nil, nil): return "capabilities unknown"
    case (nil, true): return "chat unknown · embedding"
    case (true, nil): return "chat · embedding unknown"
    case (nil, false): return "chat unknown · no embedding"
    case (false, nil): return "no chat · embedding unknown"
    case (false, false): return "no chat or embedding"
    }
}

/// The searchable catalogue for one provider. It fetches automatically when
/// opened, keeps results in memory only, and applies a ready selection to the
/// draft immediately. Missing applicable metadata is completed inline; the
/// catalogue never invents a price or a width.
@MainActor struct LLMDiscoveryView: View {
    @ObservedObject var settings: LLMSettingsModel
    let target: LLMDiscoveryTarget
    @Environment(\.dismiss) private var dismiss
    @State private var search = ""
    @State private var review: LLMModelReview?
    @State private var usage: LLMModelUsage = .chat
    @State private var manualID = ""

    private var session: LLMDiscoverySession? { settings.discoveries[target.providerID] }
    private var providerName: String { target.providerName }
    private var configuredIdentities: Set<String> {
        Set(settings.draft.models.map(\.identity))
    }

    /// The loaded catalogue before search, with the capability filter applied
    /// and duplicate catalogue IDs collapsed; the first entry wins.
    private var candidates: [APIDiscoveredModel] {
        guard case .loaded(let models) = session?.phase else { return [] }
        let task: APILLMTask = usage == .embedding ? .embed : .blurb
        var seen = Set<String>()
        let applicable = models.filter { model in
            seen.insert(model.id).inserted && llmModelSuits(model, task: task)
        }
        return applicable.filter { (usage == .embedding ? $0.embedding : $0.chat) == true }
            + applicable.filter { (usage == .embedding ? $0.embedding : $0.chat) != true }
    }

    private var visible: [APIDiscoveredModel] {
        let query = search.trimmingCharacters(in: .whitespaces)
        guard !query.isEmpty else { return candidates }
        return candidates.filter {
            $0.id.localizedCaseInsensitiveContains(query) || $0.name.localizedCaseInsensitiveContains(query)
        }
    }

    private var isFetching: Bool {
        if case .fetching = session?.phase { return true }
        return false
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(target.title).font(.headline)
            Text("Choose a model for this provider. Save in AI settings to keep your selection.")
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack {
                Spacer()
                Button("Close") { dismiss() }.keyboardShortcut(.cancelAction)
            }
            if let error = settings.error { Text(error).foregroundStyle(.red) }
            if let review {
                LLMModelReviewEditor(
                    review: Binding(get: { review }, set: { self.review = $0 }),
                    check: settings.checks[target.providerID],
                    target: target,
                    onBack: { self.review = nil },
                    onCheck: { check(review) },
                    onUse: { commit(review) }
                )
            } else {
                browseContent
                manualFallback
            }
        }
        .padding(16)
        .frame(width: 560, height: 520)
        .onAppear {
            if let fixed = target.fixedUsage { usage = fixed }
            settings.fetchModels(providerID: target.providerID)
        }
        .onChange(of: session?.fetchID) { _, _ in
            review = nil
        }
        .onChange(of: settings.draft.providers.map(\.id)) { _, ids in
            if !ids.contains(target.providerID) { dismiss() }
        }
    }

    // MARK: - Catalogue browsing

    @ViewBuilder private var browseContent: some View {
        HStack(spacing: 8) {
            TextField("Search models", text: $search)
                .textFieldStyle(.roundedBorder)
            Button("Refresh") { settings.refreshModels(providerID: target.providerID) }
                .disabled(isFetching)
        }
        if target.fixedUsage == nil {
            Picker("Add for", selection: $usage) {
                ForEach(LLMModelUsage.allCases) { usage in
                    Text(usage.title).tag(usage)
                }
            }
            .pickerStyle(.segmented)
            .frame(width: 220)
        }
        Group {
            switch session?.phase {
            case .fetching:
                HStack(spacing: 6) {
                    ProgressView().controlSize(.small)
                    Text("Fetching the catalogue…")
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            case .loaded(let models):
                if visible.isEmpty {
                    Text(models.isEmpty
                         ? "The provider's catalogue is empty. Add a model by ID below."
                         : "No models match.")
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    List(visible, id: \.id) { model in row(model) }
                }
            case .failed(let message):
                VStack(spacing: 6) {
                    Text(message).foregroundStyle(.secondary)
                    Text("Configured models stay available in Settings.")
                        .font(.caption).foregroundStyle(.tertiary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            case nil:
                VStack(spacing: 6) {
                    Text("Fetch the provider's catalogue to browse its models.")
                        .foregroundStyle(.secondary)
                    Button("Fetch models") { settings.fetchModels(providerID: target.providerID) }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)

        Text("Results are held only in memory and are discarded when the provider, revision or account changes.")
            .font(.caption)
            .foregroundStyle(.secondary)
    }

    private var manualFallback: some View {
        HStack(spacing: 8) {
            TextField("Model ID", text: $manualID)
                .textFieldStyle(.roundedBorder)
            Button("Add by ID") {
                let id = manualID.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !id.isEmpty else { return }
                review = LLMModelReview(discovered: APIDiscoveredModel(id: id, name: id),
                                        provider: providerName, usage: usage, isManual: true)
            }
        }
    }

    @ViewBuilder
    private func row(_ model: APIDiscoveredModel) -> some View {
        let identity = llmModelIdentity(providerName, model.id)
        let configured = configuredIdentities.contains(identity)
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(model.name).lineLimit(1)
                Text(model.id).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                Text(llmCapabilitySummary(model) + (configured ? " · configured" : ""))
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if configured {
                if target.selection == .add {
                    Text("Configured").font(.caption).foregroundStyle(.secondary)
                } else if let existing = settings.draft.models.first(where: { $0.identity == identity }) {
                    Button(target.selection == .defaultEmbedding ? "Use for embeddings" : "Use") {
                        useConfigured(existing)
                    }
                }
            } else {
                let draftReview = LLMModelReview(discovered: model, provider: providerName, usage: usage)
                Button("Details") { review = draftReview }
                if draftReview.isReady {
                    Button(target.selection == .add ? "Add" : "Use") { use(model) }
                } else {
                    Button("Missing: \(draftReview.missingMetadata.joined(separator: ", "))") {
                        review = draftReview
                    }
                }
            }
        }
    }

    private func use(_ model: APIDiscoveredModel) {
        let draftReview = LLMModelReview(discovered: model, provider: providerName, usage: usage)
        guard draftReview.isReady else {
            review = draftReview
            return
        }
        commit(draftReview)
    }

    private func useConfigured(_ model: LLMModelDraft) {
        guard settings.selectConfiguredModel(model, providerID: target.providerID,
                                             selection: target.selection) else { return }
        if target.selection != .add { dismiss() }
    }

    private func commit(_ review: LLMModelReview) {
        let accepted: Bool
        if review.isManual {
            accepted = settings.selectManualModel(review, providerID: target.providerID,
                                                  selection: target.selection)
        } else {
            accepted = settings.selectCatalogueModel(review, providerID: target.providerID,
                                                     fetchID: session?.fetchID ?? -1,
                                                     selection: target.selection)
        }
        guard accepted else { return }
        self.review = nil
        if target.selection != .add { dismiss() }
    }

    private func check(_ review: LLMModelReview) {
        settings.checkModel(providerID: target.providerID, model: review.configured(),
                            capability: review.usage == .embedding ? .embedding : .chat)
    }
}

/// Inline metadata completion for one catalogue pick. Fields the selection
/// does not use are shown as explicitly zero; a missing applicable value is
/// reported next to its field, never as a bare invalid number.
private struct LLMModelReviewEditor: View {
    @Binding var review: LLMModelReview
    let check: LLMModelCheckSession?
    let target: LLMDiscoveryTarget
    let onBack: () -> Void
    let onCheck: () -> Void
    let onUse: () -> Void

    private var dimensionsMessage: String? { review.dimensionsMessage }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(review.discovered.name).font(.headline).lineLimit(1)
                    Text("\(review.id) · \(target.providerName) · \(llmCapabilitySummary(review.discovered))")
                        .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer()
                Button("Back", action: onBack)
            }

            if review.usage == .chat {
                priceField("Input per 1M (USD)", text: $review.inputPer1M, valid: review.inputValid)
                priceField("Output per 1M (USD)", text: $review.outputPer1M, valid: review.outputValid)
                Text("Embedding geometry and rate are not used for chat and stay zero.")
                    .font(.caption).foregroundStyle(.secondary)
            } else {
                priceField("Embedding per 1M (USD)", text: $review.embeddingPer1M,
                           valid: review.embeddingPriceValid)
                LabeledContent("Dimensions") {
                    VStack(alignment: .trailing, spacing: 2) {
                        TextField("0", text: $review.dimensions)
                            .frame(width: 90)
                            .multilineTextAlignment(.trailing)
                        if let message = dimensionsMessage {
                            Text(message)
                                .font(.caption2)
                                .foregroundStyle(.red)
                        }
                    }
                }
                Toggle("Truncatable", isOn: $review.truncatable)
                Toggle("Supports custom dimensions", isOn: $review.supportsCustomDimensions)
            }

            Toggle("This model is free", isOn: Binding(
                get: { review.isFree },
                set: { enabled in
                    if enabled { review.markFree() } else { review.isFree = false }
                }
            ))

            if !review.isReady {
                Text("Missing: \(review.missingMetadata.joined(separator: ", "))")
                    .font(.caption)
                    .foregroundStyle(.orange)
            }

            checkSection

            HStack {
                Spacer()
                Button(target.selection == .add ? "Add Model" : "Use Model", action: onUse)
                    .disabled(!review.isReady)
            }
        }
        .padding(8)
        .background(.quaternary.opacity(0.3))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .onChange(of: check?.phase) { _, phase in
            guard case .result(let result) = phase, result.compatible else { return }
            if let dimensions = result.dimensions { review.dimensions = String(dimensions) }
            if let supports = result.supportsCustomDimensions {
                review.supportsCustomDimensions = supports
            }
        }
    }

    @ViewBuilder private var checkSection: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button("Check Model") { onCheck() }
                .disabled(!review.isCheckable)
            Text("A fixed sample is sent to the provider and may cost. Checking an embedding can observe its width and resizing support.")
                .font(.caption2)
                .foregroundStyle(.secondary)
            if let check {
                switch check.phase {
                case .checking:
                    HStack(spacing: 4) {
                        ProgressView().controlSize(.mini)
                        Text("Checking…").font(.caption)
                    }
                case .result(let result):
                    Text((result.compatible ? "Compatible. " : "Not compatible. ") + result.message)
                        .font(.caption)
                        .foregroundStyle(result.compatible ? Color.secondary : Color.orange)
                case .failed(let message):
                    Text(message).font(.caption).foregroundStyle(.red)
                }
            }
        }
    }

    @ViewBuilder
    private func priceField(_ title: String, text: Binding<String>, valid: Bool) -> some View {
        LabeledContent(title) {
            VStack(alignment: .trailing, spacing: 2) {
                TextField("0", text: text)
                    .frame(width: 90)
                    .multilineTextAlignment(.trailing)
                if !valid {
                    Text(text.wrappedValue.trimmingCharacters(in: .whitespaces).isEmpty
                         ? "Required" : "Invalid")
                        .font(.caption2)
                        .foregroundStyle(.red)
                }
            }
        }
    }
}

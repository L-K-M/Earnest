import Foundation
import SwiftUI
import EarnestKit

/// What the catalogue sheet browses: one provider draft as currently edited,
/// optionally narrowed to the models a task can use.
struct LLMDiscoveryTarget: Identifiable {
    let providerID: UUID
    let providerName: String
    let task: APILLMTask?
    var id: UUID { providerID }
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

/// The catalogue browser for one provider. Results are memory-only: they are
/// fetched for the provider exactly as edited and vanish the moment the
/// provider, the account or the server changes. Browsing for a task filters
/// out models the task cannot use and routes only on an explicit choice;
/// unconfigured picks always pass a price review before entering the draft.
@MainActor struct LLMDiscoveryView: View {
    @ObservedObject var settings: LLMSettingsModel
    let target: LLMDiscoveryTarget
    @Environment(\.dismiss) private var dismiss
    @State private var search = ""
    @State private var selection: Set<String> = []
    @State private var reviewing = false
    @State private var reviewFetchID: Int?
    @State private var reviews: [LLMModelReview] = []

    private var session: LLMDiscoverySession? { settings.discoveries[target.providerID] }
    private var configuredNames: Set<String> { Set(settings.draft.models.map(\.name)) }
    private var slotsLeft: Int { max(0, llmModelLimit - settings.draft.models.count) }

    /// The loaded catalogue before search, with the task filter applied and
    /// any duplicate catalogue IDs collapsed; the first entry wins, matching
    /// the import's keep-existing rule.
    private var candidates: [APIDiscoveredModel] {
        guard case .loaded(let models) = session?.phase else { return [] }
        var seen = Set<String>()
        return models.filter { model in
            seen.insert(model.id).inserted && (target.task.map { llmModelSuits(model, task: $0) } ?? true)
        }
    }

    /// The catalogue rows the search text leaves visible.
    private var visible: [APIDiscoveredModel] {
        let query = search.trimmingCharacters(in: .whitespaces)
        guard !query.isEmpty else { return candidates }
        return candidates.filter {
            $0.id.localizedCaseInsensitiveContains(query) || $0.name.localizedCaseInsensitiveContains(query)
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(target.task.map { "Models for \($0.title.lowercased()) on \(target.providerName)" }
                 ?? "Models on \(target.providerName)")
                .font(.headline)
            Text("Fetching previews the provider as currently edited; nothing is persisted. Applying a choice updates the draft; Save persists the whole configuration.")
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack {
                Spacer()
                Button("Close") { dismiss() }.keyboardShortcut(.cancelAction)
            }
            if let error = settings.error { Text(error).foregroundStyle(.red) }
            if reviewing {
                reviewContent
            } else {
                browseContent
            }
        }
        .padding(16)
        .frame(width: 520, height: 470)
        .onChange(of: session?.fetchID) { _, _ in
            selection = []
            reviews = []
            reviewing = false
            reviewFetchID = nil
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
                .disabled(candidates.isEmpty)
            Button("Fetch again") { settings.fetchModels(providerID: target.providerID) }
                .disabled(isFetching)
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
                         ? "The provider's catalogue is empty. Add models manually below."
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

        HStack {
            Text("Results are held only in memory and are discarded when the provider changes.")
                .font(.caption)
                .foregroundStyle(.secondary)
            Spacer()
            if target.task == nil {
                Button("Import selected") { importSelection() }
                    .disabled(selection.isEmpty || selection.count > slotsLeft)
            }
        }
        if target.task == nil && selection.count > slotsLeft {
            Text("The \(llmModelLimit)-model limit leaves room for \(slotsLeft); deselect \(selection.count - slotsLeft) before importing.")
                .font(.caption)
                .foregroundStyle(.orange)
        }
    }

    private var isFetching: Bool {
        if case .fetching = session?.phase { return true }
        return false
    }

    @ViewBuilder
    private func row(_ model: APIDiscoveredModel) -> some View {
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(model.name).lineLimit(1)
                Text(model.id).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                Text(llmCapabilitySummary(model)
                     + (configuredNames.contains(model.id) ? " · configured" : ""))
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if let task = target.task {
                Button(task == .embed ? "Use for embeddings" : "Use") { pick(model) }
            } else if configuredNames.contains(model.id) {
                Text("Configured").font(.caption).foregroundStyle(.secondary)
            } else {
                Toggle("Select", isOn: Binding(
                    get: { selection.contains(model.id) },
                    set: { checked in
                        if checked { selection.insert(model.id) } else { selection.remove(model.id) }
                    }
                ))
                .labelsHidden()
            }
        }
    }

    /// A task-mode choice. An unconfigured pick needs a price review before it
    /// becomes a configured model; an already-configured one routes directly,
    /// keeping the prices it shares by ID.
    private func pick(_ model: APIDiscoveredModel) {
        guard let task = target.task, let fetchID = session?.fetchID else { return }
        if configuredNames.contains(model.id) {
            if settings.applyCatalogueSelection([LLMModelReview(discovered: model)], providerID: target.providerID,
                                                fetchID: fetchID, task: task) { dismiss() }
        } else {
            reviewFetchID = fetchID
            reviews = [LLMModelReview(discovered: model)]
            reviewing = true
        }
    }

    /// Provider-mode import: the selected catalogue IDs that are not yet
    /// configured enter a price review; configured IDs are already present.
    private func importSelection() {
        reviewFetchID = session?.fetchID
        reviews = candidates
            .filter { selection.contains($0.id) && !configuredNames.contains($0.id) }
            .map(LLMModelReview.init(discovered:))
        guard !reviews.isEmpty else { return }
        reviewing = true
    }

    // MARK: - Price review

    @ViewBuilder private var reviewContent: some View {
        Text("Review prices and geometry before importing. The catalogue pre-fills what it publishes; enter explicit values; zero included; for whatever it leaves out.")
            .font(.caption)
            .foregroundStyle(.secondary)
        Form {
            ForEach(reviews.indices, id: \.self) { index in
                SwiftUI.Section {
                    priceField("Input per 1M (USD)", text: $reviews[index].inputPer1M,
                               valid: reviews[index].inputValid)
                    priceField("Output per 1M (USD)", text: $reviews[index].outputPer1M,
                               valid: reviews[index].outputValid)
                    priceField("Embedding per 1M (USD)", text: $reviews[index].embeddingPer1M,
                               valid: reviews[index].embeddingPriceValid)
                    LabeledContent("Dimensions") {
                        VStack(alignment: .trailing, spacing: 2) {
                            TextField("0", text: $reviews[index].dimensions)
                                .frame(width: 90)
                                .multilineTextAlignment(.trailing)
                            if !reviews[index].dimensionsValid {
                                Text(reviews[index].dimensions.trimmingCharacters(in: .whitespaces).isEmpty
                                     ? "Required" : "Invalid")
                                    .font(.caption2)
                                    .foregroundStyle(.red)
                            }
                        }
                    }
                    Toggle("Truncatable", isOn: $reviews[index].truncatable)
                } header: {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(reviews[index].discovered.name)
                        Text("\(reviews[index].id) · \(llmCapabilitySummary(reviews[index].discovered))")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .formStyle(.grouped)
        .frame(maxHeight: .infinity)

        HStack {
            Button("Back") { reviewing = false }
            Spacer()
            Button(target.task == nil ? "Import \(reviews.count) model\(reviews.count == 1 ? "" : "s")"
                                      : "Import and use") { importReviewed() }
                .disabled(!reviews.allSatisfy(\.isReady) || reviews.count > slotsLeft || settings.busy || session?.fetchID != reviewFetchID)
        }
        if slotsLeft < reviews.count {
            Text("The \(llmModelLimit)-model limit leaves room for \(slotsLeft); deselect models before importing.")
                .font(.caption)
                .foregroundStyle(.orange)
        }
        Text("Prices are shared by model ID across providers: importing an ID that is already configured keeps its existing prices.")
            .font(.caption)
            .foregroundStyle(.secondary)
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

    /// Import the reviewed models into the draft. Task mode routes only when
    /// the picked model actually became (or already was) configured.
    private func importReviewed() {
        guard let fetchID = reviewFetchID else { return }
        if settings.applyCatalogueSelection(reviews, providerID: target.providerID, fetchID: fetchID, task: target.task) {
            dismiss()
        }
    }
}

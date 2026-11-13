import Foundation
import SwiftUI
import EarnestKit

extension APILLMTask {
    /// The human name shown in Settings.
    var title: String {
        switch self {
        case .embed: return "Embeddings"
        case .classify: return "Classify"
        case .blurb: return "Blurbs"
        case .summary: return "Summaries"
        case .edition: return "Edition"
        case .compileRule: return "Compile rules"
        case .ask: return "Ask"
        }
    }
}

/// Earnest > Settings > LLM Services. The configuration is server-wide: it
/// affects every account and API keys stay on the server. Nothing is fetched
/// or saved until the user asks, and tasks make no calls while they are
/// disabled.
@MainActor struct LLMSettingsView: View {
    @ObservedObject var settings: LLMSettingsModel
    @State private var discoveryTarget: LLMDiscoveryTarget?

    /// Fetch the catalogue for a provider row and open the browser. The
    /// request previews the provider as currently edited; nothing persists.
    private func discover(providerID: UUID, providerName: String, task: APILLMTask? = nil) {
        settings.fetchModels(providerID: providerID)
        // A rejected fetch reports on the pane; the sheet opens only when
        // there is a configuration to preview.
        guard settings.loaded else { return }
        discoveryTarget = LLMDiscoveryTarget(providerID: providerID, providerName: providerName, task: task)
    }

    var body: some View {
        Form {
            SwiftUI.Section {
                Text("These settings apply to the whole server and affect every account. API keys are stored only on the server, and a task makes no calls until it routes to a provider and model.")
                    .foregroundStyle(.secondary)
                HStack(spacing: 8) {
                    Button("Reload") { settings.load() }
                        .disabled(!settings.ready || settings.busy)
                    Button("Save") { settings.save() }
                        .disabled(!settings.ready || settings.busy || settings.conflict || !settings.isDirty)
                    Spacer()
                }
                LLMSettingsStatus(settings: settings)
            }

            SwiftUI.Section {
                if settings.draft.providers.isEmpty {
                    Text("No providers configured. Every task stays disabled.")
                        .foregroundStyle(.secondary)
                }
                ForEach($settings.draft.providers) { $provider in
                    LLMProviderEditor(
                        provider: $provider,
                        onRemove: { settings.removeProvider(id: provider.id) },
                        onKindChange: { kind in settings.setProviderKind(kind, id: provider.id) },
                        onDiscover: {
                            discover(providerID: provider.id,
                                     providerName: provider.name.isEmpty ? "New provider" : provider.name)
                        }
                    )
                }
                Button("Add Provider") { settings.addProvider() }
            } header: {
                Text("Providers")
            } footer: {
                Text("Local servers need the server's outbound allowlist; Earnest never bypasses it.")
            }

            SwiftUI.Section {
                ForEach($settings.draft.tasks) { $task in
                    LLMTaskEditor(task: $task, providers: settings.draft.providers, models: settings.draft.models) {
                        guard let providerID = settings.providerID(named: task.provider) else { return }
                        discover(providerID: providerID, providerName: task.provider, task: task.task)
                    }
                }
            } header: {
                Text("Tasks")
            } footer: {
                Text("A task is disabled until both a provider and a model are chosen. Anthropic providers cannot embed. Fetch models browses a provider's catalogue; a route changes only on an explicit choice.")
            }

            DisclosureGroup("Models and Pricing") {
                Text("Prices are USD per million tokens. Enter the service's rates for paid models; use explicit zero prices for local models.")
                    .foregroundStyle(.secondary)
                if settings.draft.models.isEmpty {
                    Text("No models configured. Every task stays disabled.")
                        .foregroundStyle(.secondary)
                }
                ForEach($settings.draft.models) { $model in
                    LLMModelEditor(model: $model) {
                        settings.removeModel(id: model.id)
                    }
                }
                Button("Add Model") { settings.addModel() }
            }
        }
        .formStyle(.grouped)
        .frame(height: 560)
        .sheet(item: $discoveryTarget) { target in
            LLMDiscoveryView(settings: settings, target: target)
        }
    }
}

/// Saving, saved, failed or conflicting: the draft's state, with the one
/// action that resolves it.
@MainActor struct LLMSettingsStatus: View {
    @ObservedObject var settings: LLMSettingsModel

    var body: some View {
        HStack(spacing: 6) {
            if !settings.ready {
                Text("Sign in to load the LLM settings.")
            } else if settings.busy {
                ProgressView().controlSize(.mini)
                Text("Working…")
            } else if settings.conflict {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.yellow)
                Text(settings.error ?? "Settings changed elsewhere.")
                Button("Reload") { settings.load() }.buttonStyle(.link)
            } else if let error = settings.error {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.yellow)
                Text(error).textSelection(.enabled)
                if settings.canRetry {
                    Button("Try Again") { settings.save() }.buttonStyle(.link)
                }
            } else if settings.saved {
                Text("Saved.")
            } else if settings.loaded {
                Text("Loaded.")
            } else {
                Text("Choose Reload to fetch the server configuration.")
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .accessibilityElement(children: .contain)
    }
}

@MainActor private struct LLMProviderEditor: View {
    @Binding var provider: LLMProviderDraft
    let onRemove: () -> Void
    let onKindChange: (APILLMProviderKind) -> Void
    let onDiscover: () -> Void

    var body: some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 8) {
                LabeledContent("Name") {
                    TextField("openai", text: $provider.name)
                        .frame(width: 160)
                        .disabled(provider.hasAPIKey && provider.apiKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !provider.clearAPIKey)
                }
                if provider.hasAPIKey {
                    Text("To rename this provider, enter a replacement API key or remove the saved key.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                LabeledContent("Kind") {
                    Picker("Kind", selection: Binding(get: { provider.kind }, set: onKindChange)) {
                        ForEach(APILLMProviderKind.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                    }
                    .labelsHidden()
                    .frame(width: 160)
                }
                LabeledContent("Base URL") {
                    TextField("https://", text: $provider.baseURL)
                        .frame(width: 220)
                }
                LabeledContent("Monthly limit (USD)") {
                    TextField("0", text: $provider.monthlyLimitUSD)
                        .frame(width: 90)
                }
                Text("0 means no monthly limit. Leave the API key blank to keep the saved key.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                IntegerField("Request timeout", value: $provider.timeoutSeconds, in: 1...600, unit: "seconds")
                LabeledContent("Token field") {
                    Picker("Token field", selection: $provider.tokenField) {
                        ForEach(APILLMTokenField.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                    }
                    .labelsHidden()
                    .frame(width: 200)
                }
                LabeledContent("API key") {
                    HStack(spacing: 8) {
                        Text(provider.keyStatus.title)
                            .foregroundStyle(provider.keyStatus == .none || provider.keyStatus == .removal
                                             ? Color.orange : Color.secondary)
                        SecureField("Replace key", text: $provider.apiKey)
                            .frame(width: 150)
                            .disabled(provider.clearAPIKey)
                    }
                }
                Toggle("Remove the saved key", isOn: $provider.clearAPIKey)
                    .disabled(!provider.hasAPIKey)
                Button("Fetch models…", action: onDiscover)
                Text("Fetching previews this provider exactly as edited; a typed key is used but never persisted.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Button("Remove Provider", role: .destructive, action: onRemove)
            }
            .padding(.vertical, 4)
        } label: {
            HStack {
                Text(provider.name.isEmpty ? "New provider" : provider.name)
                Text(provider.kind.rawValue)
                    .foregroundStyle(.secondary)
                Spacer()
                Text(provider.keyStatus.title)
                    .foregroundStyle(provider.keyStatus == .none || provider.keyStatus == .removal
                                     ? Color.orange : Color.secondary)
            }
        }
    }
}

@MainActor private struct LLMTaskEditor: View {
    @Binding var task: LLMTaskDraft
    let providers: [LLMProviderDraft]
    let models: [LLMModelDraft]
    let onDiscover: () -> Void

    /// Anthropic has no embeddings dialect, so it is not offered for embed.
    private var providerChoices: [LLMProviderDraft] {
        task.task == .embed ? providers.filter { $0.kind != .anthropic } : providers
    }

    private var providerSelection: Binding<String> {
        Binding(
            get: { task.provider },
            set: { newValue in
                task.provider = newValue
                // Disabling clears the model too; both must be set together.
                if newValue.isEmpty { task.model = "" }
            }
        )
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            LabeledContent(task.task.title) {
                Picker("Provider", selection: providerSelection) {
                    Text("Disabled").tag("")
                    ForEach(providerChoices) { provider in
                        Text(provider.name.isEmpty ? "New provider" : provider.name).tag(provider.name)
                    }
                }
                .labelsHidden()
                .frame(width: 180)
            }
            if !task.provider.isEmpty {
                LabeledContent("Model") {
                    HStack(spacing: 8) {
                        Picker("Model", selection: $task.model) {
                            Text("Choose a model").tag("")
                            ForEach(models) { model in
                                Text(model.name.isEmpty ? "New model" : model.name).tag(model.name)
                            }
                        }
                        .labelsHidden()
                        .frame(width: 180)
                        Button("Fetch models…", action: onDiscover)
                            .disabled(!providers.contains { $0.name == task.provider })
                    }
                }
            }
        }
        .padding(.vertical, 2)
    }
}

@MainActor private struct LLMModelEditor: View {
    @Binding var model: LLMModelDraft
    let onRemove: () -> Void

    var body: some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 8) {
                LabeledContent("Name") {
                    TextField("text-embedding-3-small", text: $model.name)
                        .frame(width: 220)
                }
                LabeledContent("Input per 1M (USD)") {
                    TextField("0", text: $model.inputPer1M)
                        .frame(width: 90)
                }
                LabeledContent("Output per 1M (USD)") {
                    TextField("0", text: $model.outputPer1M)
                        .frame(width: 90)
                }
                LabeledContent("Embedding per 1M (USD)") {
                    TextField("0", text: $model.embeddingPer1M)
                        .frame(width: 90)
                }
                IntegerField("Dimensions", value: $model.dimensions, in: 0...65536, unit: "values")
                Toggle("Truncatable", isOn: $model.truncatable)
                Button("Remove Model", role: .destructive, action: onRemove)
            }
            .padding(.vertical, 4)
        } label: {
            HStack {
                Text(model.name.isEmpty ? "New model" : model.name)
                Spacer()
                Text(model.truncatable ? "truncatable" : "\(model.dimensions) dimensions")
                    .foregroundStyle(.secondary)
            }
        }
    }
}

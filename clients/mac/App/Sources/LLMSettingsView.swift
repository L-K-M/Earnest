import Foundation
import SwiftUI
import EarnestKit

extension APILLMTask {
    /// The human name shown in Settings.
    var title: String {
        switch self {
        case .embed: return "Embeddings"
        case .classify: return "Classification"
        case .blurb: return "Blurbs"
        case .summary: return "Summaries"
        case .edition: return "Editions"
        case .compileRule: return "Rule compilation"
        case .ask: return "Ask my feeds"
        }
    }
}

/// Earnest > Settings > AI. The configuration is server-wide: it affects
/// every account and API keys stay on the server. Providers are edited
/// directly in the draft; defaults and per-task overrides choose routes, and
/// nothing is saved until Save. Discard returns to the loaded configuration.
@MainActor struct LLMSettingsView: View {
    @ObservedObject var settings: LLMSettingsModel
    @State private var discoveryTarget: LLMDiscoveryTarget?

    var body: some View {
        Form {
            SwiftUI.Section {
                Text("AI settings apply to everyone on this server. API keys are encrypted on the server.")
                    .foregroundStyle(.secondary)
                HStack(spacing: 8) {
                    Button("Save") { settings.save() }
                        .disabled(!settings.ready || settings.busy || settings.conflict || !settings.isDirty)
                    Button("Discard") { settings.discard() }
                        .disabled(!settings.ready || settings.busy || !settings.canDiscard)
                    Spacer()
                }
                LLMSettingsStatus(settings: settings)
            }

            Group {
            SwiftUI.Section {
                if settings.draft.providers.isEmpty {
                    Text("No providers configured. Every feature stays off.")
                        .foregroundStyle(.secondary)
                }
                ForEach($settings.draft.providers) { $provider in
                    LLMProviderEditor(
                        provider: $provider,
                        onRemove: { settings.removeProvider(id: provider.id) },
                        onKindChange: { kind in settings.setProviderKind(kind, id: provider.id) },
                        onNameChange: { name in settings.renameProvider(id: provider.id, name: name) },
                        onBrowse: {
                            discover(providerID: provider.id,
                                     providerName: provider.name.isEmpty ? "New provider" : provider.name,
                                     selection: .add)
                        }
                    )
                }
                Menu {
                    ForEach(LLMProviderPreset.allCases) { preset in
                        Button(preset.title) { settings.addProvider(preset: preset) }
                    }
                } label: {
                    Label("Add Provider", systemImage: "plus")
                }
            } header: {
                Text("Providers")
            } footer: {
                Text("Choose a preset, enter an API key, and set a monthly budget. A zero budget has no limit.")
            }

            SwiftUI.Section {
                DefaultRouteRow(
                    title: "Text model",
                    usage: .chat,
                    settings: settings,
                    providers: settings.draft.providers,
                    models: settings.configuredModels(for:),
                    selection: .defaultText,
                    target: $discoveryTarget
                )
                DefaultRouteRow(
                    title: "Embedding model",
                    usage: .embedding,
                    settings: settings,
                    providers: settings.draft.providers,
                    models: settings.configuredModels(for:),
                    selection: .defaultEmbedding,
                    target: $discoveryTarget
                )
            } header: {
                Text("Defaults")
            } footer: {
                Text("Features use these models unless you choose a custom override. Selecting a model does not enable a feature.")
            }

            SwiftUI.Section {
                ForEach($settings.draft.tasks) { $task in
                    LLMTaskEditor(
                        task: $task,
                        settings: settings,
                        onEnable: { enabled in settings.setTaskEnabled(task.task, enabled) },
                        onModeChange: { mode in settings.setTaskMode(task.task, mode) },
                        onProviderChange: { provider in settings.setTaskProvider(task.task, provider) },
                        onModelChange: { model in settings.setTaskModel(task.task, model) },
                        onBrowse: { providerName in
                            guard let provider = settings.draft.providers.first(where: { $0.name == providerName }) else { return }
                            discover(providerID: provider.id, providerName: provider.name,
                                     selection: .task(task.task))
                        }
                    )
                }
            } header: {
                Text("Features")
            } footer: {
                Text("Enable the features you want to use. Per-feature model overrides are in Advanced.")
            }

            DisclosureGroup("Models and Pricing") {
                Text("Prices are USD per million tokens. Enter the service's rates for paid models; use explicit zero prices for local models. A provider-scoped entry wins over a global entry with the same name.")
                    .foregroundStyle(.secondary)
                if settings.draft.models.isEmpty {
                    Text("No models configured. Every feature stays off.")
                        .foregroundStyle(.secondary)
                }
                ForEach($settings.draft.models) { $model in
                    LLMModelEditor(model: $model, providers: settings.draft.providers) {
                        settings.removeModel(id: model.id)
                    }
                }
            }
            }
            .disabled(!settings.loaded || settings.busy)
        }
        .formStyle(.grouped)
        .frame(height: 560)
        .task(id: settings.ready) { settings.loadIfNeeded() }
        .sheet(item: $discoveryTarget) { target in
            LLMDiscoveryView(settings: settings, target: target)
        }
    }

    private func discover(providerID: UUID, providerName: String,
                          selection: LLMSettingsModel.LLMModelSelection) {
        guard settings.loaded else { return }
        discoveryTarget = LLMDiscoveryTarget(providerID: providerID,
                                             providerName: providerName,
                                             selection: selection)
    }
}

/// Saving, saved, failed or conflicting: the draft's state, with the actions
/// that resolve it.
@MainActor struct LLMSettingsStatus: View {
    @ObservedObject var settings: LLMSettingsModel
    @State private var confirmingReload = false

    var body: some View {
        HStack(spacing: 6) {
            if !settings.ready {
                Text("Sign in to load the AI settings.")
            } else if settings.busy {
                ProgressView().controlSize(.mini)
                Text("Working…")
            } else if settings.conflict {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.yellow)
                Text(settings.error ?? "Settings changed elsewhere.")
                Button("Reload") {
                    if settings.isDirty { confirmingReload = true } else { settings.load() }
                }.buttonStyle(.link)
            } else if let error = settings.error {
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.yellow)
                Text(error).textSelection(.enabled)
                if settings.canRetry {
                    Button("Retry save") { settings.save() }.buttonStyle(.link)
                } else if !settings.loaded {
                    Button("Retry") { settings.load() }.buttonStyle(.link)
                }
            } else if settings.saved {
                Text("Saved.")
            } else if settings.loaded {
                Text(settings.isDirty ? "Unsaved changes" : "")
            } else {
                Text("Loading AI settings…")
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .accessibilityElement(children: .contain)
        .alert("Discard edits and reload?", isPresented: $confirmingReload) {
            Button("Reload", role: .destructive) { settings.load() }
            Button("Keep editing", role: .cancel) {}
        } message: {
            Text("The current saved settings will replace your unsaved AI edits.")
        }
    }
}

@MainActor private struct LLMProviderEditor: View {
    @Binding var provider: LLMProviderDraft
    @State private var expanded = true
    let onRemove: () -> Void
    let onKindChange: (APILLMProviderKind) -> Void
    let onNameChange: (String) -> Void
    let onBrowse: () -> Void

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            VStack(alignment: .leading, spacing: 8) {
                LabeledContent("Name") {
                    TextField("Provider name", text: Binding(get: { provider.name }, set: onNameChange))
                        .frame(width: 160)
                        .disabled(provider.hasAPIKey && provider.apiKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !provider.clearAPIKey)
                }
                if provider.hasAPIKey {
                    Text("To rename this provider, enter a replacement API key or remove the saved key.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                LabeledContent("Base URL") {
                    TextField("https://", text: $provider.baseURL)
                        .frame(width: 220)
                }
                LabeledContent("Monthly budget (USD)") {
                    TextField("0", text: $provider.monthlyLimitUSD)
                        .frame(width: 90)
                }
                if !isValidLLMMoney(provider.monthlyLimitUSD) {
                    Text("Enter a non-negative monthly budget with at most six decimal places.")
                        .font(.caption)
                        .foregroundStyle(.red)
                }
                Text("0 means no monthly limit. Leave the API key blank to keep the saved key.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                LabeledContent("API key") {
                    HStack(spacing: 8) {
                        Text(provider.keyStatus.title)
                            .foregroundStyle(provider.keyStatus == .none || provider.keyStatus == .removal
                                             ? Color.orange : Color.secondary)
                        SecureField(provider.hasAPIKey ? "Leave blank to keep the key" : "API key", text: $provider.apiKey)
                            .frame(width: 150)
                            .disabled(provider.clearAPIKey)
                    }
                }
                Toggle("Remove the saved key", isOn: $provider.clearAPIKey)
                    .disabled(!provider.hasAPIKey)
                DisclosureGroup("Advanced") {
                    VStack(alignment: .leading, spacing: 8) {
                LabeledContent("API protocol") {
                    Picker("API protocol", selection: Binding(get: { provider.kind }, set: onKindChange)) {
                        ForEach(APILLMProviderKind.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                    }
                    .labelsHidden()
                    .frame(width: 160)
                }
                        IntegerField("Request timeout", value: $provider.timeoutSeconds, in: 1...600, unit: "seconds")
                        LabeledContent("Token field") {
                            Picker("Token field", selection: $provider.tokenField) {
                                ForEach(APILLMTokenField.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                            }
                            .labelsHidden()
                            .frame(width: 200)
                        }
                    }
                    .padding(.top, 4)
                }
                Button("Browse Models…", action: onBrowse)
                    .disabled(provider.name.isEmpty || provider.name != provider.routedName)
                Text("Browse available models or enter a model ID manually.")
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

/// One default route: a provider and a configured model, plus the searchable
/// catalogue for models not configured yet.
private struct DefaultRouteRow: View {
    let title: String
    let usage: LLMModelUsage
    @ObservedObject var settings: LLMSettingsModel
    let providers: [LLMProviderDraft]
    let models: (String) -> [LLMModelDraft]
    let selection: LLMSettingsModel.LLMModelSelection
    @Binding var target: LLMDiscoveryTarget?

    init(title: String, usage: LLMModelUsage, settings: LLMSettingsModel,
         providers: [LLMProviderDraft],
         models: @escaping (String) -> [LLMModelDraft],
         selection: LLMSettingsModel.LLMModelSelection,
         target: Binding<LLMDiscoveryTarget?>) {
        self.title = title
        self.usage = usage
        self._settings = ObservedObject(wrappedValue: settings)
        self.providers = providers
        self.models = models
        self.selection = selection
        self._target = target
    }

    private var route: APILLMDefaultRoute {
        switch usage {
        case .chat: return settings.draft.defaults?.text ?? APILLMDefaultRoute()
        case .embedding: return settings.draft.defaults?.embedding ?? APILLMDefaultRoute()
        }
    }

    private var providerChoices: [LLMProviderDraft] {
        providers.filter { !$0.name.isEmpty && $0.name == $0.routedName && (usage != .embedding || $0.kind != .anthropic) }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            LabeledContent(title) {
                Picker("Provider", selection: Binding(
                    get: { route.provider },
                    set: { settings.setDefaultRoute(provider: $0, model: "", usage: usage) }
                )) {
                    Text("None").tag("")
                    ForEach(providerChoices) { provider in
                        Text(provider.name.isEmpty ? "New provider" : provider.name).tag(provider.name)
                    }
                }
                .labelsHidden()
                .frame(width: 160)
            }
            LabeledContent("Model") {
                HStack(spacing: 8) {
                    Picker("Model", selection: Binding(
                        get: { route.model },
                        set: { settings.setDefaultRoute(provider: route.provider, model: $0, usage: usage) }
                    )) {
                        Text("Choose a model").tag("")
                        ForEach(models(route.provider)) { model in
                            Text(model.name.isEmpty ? "New model" : model.name).tag(model.name)
                        }
                    }
                    .labelsHidden()
                    .frame(width: 180)
                    Button("Browse…") {
                        guard let provider = providerChoices.first(where: { $0.name == route.provider }) else { return }
                        target = LLMDiscoveryTarget(providerID: provider.id, providerName: provider.name,
                                                    selection: selection)
                    }
                    .disabled(route.provider.isEmpty)
                }
            }
        }
        .padding(.vertical, 2)
    }
}

@MainActor private struct LLMTaskEditor: View {
    @Binding var task: LLMTaskDraft
    @ObservedObject var settings: LLMSettingsModel
    let onEnable: (Bool) -> Void
    let onModeChange: (APILLMTaskMode) -> Void
    let onProviderChange: (String) -> Void
    let onModelChange: (String) -> Void
    let onBrowse: (String) -> Void

    private var providerChoices: [LLMProviderDraft] {
        settings.draft.providers.filter {
            !$0.name.isEmpty && $0.name == $0.routedName && (task.task != .embed || $0.kind != .anthropic)
        }
    }

    private var route: APILLMDefaultRoute {
        settings.effectiveTaskRoute(task)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Toggle(isOn: Binding(get: { task.isEnabled }, set: onEnable)) {
                Text(task.task.title)
            }
            if task.isEnabled {
                Text(route.model.isEmpty ? "Choose a default model or set an override in Advanced." : "\(route.provider) · \(route.model)")
                    .font(.caption).foregroundStyle(.secondary)
                DisclosureGroup("Advanced") {
                LabeledContent("Route") {
                    Picker("Route", selection: Binding(get: { task.mode }, set: onModeChange)) {
                        Text("Default").tag(APILLMTaskMode.default)
                        Text("Custom").tag(APILLMTaskMode.custom)
                    }
                    .labelsHidden()
                    .frame(width: 140)
                }
                if task.mode == .default {
                    Text(route.provider.isEmpty && route.model.isEmpty
                         ? "No default set. Choose one in Defaults or switch to Custom."
                         : "\(route.provider) · \(route.model)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    LabeledContent("Provider") {
                        Picker("Provider", selection: Binding(get: { task.provider }, set: onProviderChange)) {
                            Text("Choose a provider").tag("")
                            ForEach(providerChoices) { provider in
                                Text(provider.name.isEmpty ? "New provider" : provider.name).tag(provider.name)
                            }
                        }
                        .labelsHidden()
                        .frame(width: 170)
                    }
                    if !task.provider.isEmpty {
                        LabeledContent("Model") {
                            HStack(spacing: 8) {
                                Picker("Model", selection: Binding(get: { task.model }, set: onModelChange)) {
                                    Text("Choose a model").tag("")
                                    ForEach(settings.configuredModels(for: task.provider)) { model in
                                        Text(model.name.isEmpty ? "New model" : model.name).tag(model.name)
                                    }
                                }
                                .labelsHidden()
                                .frame(width: 170)
                                Button("Browse…") { onBrowse(task.provider) }
                            }
                        }
                    }
                }
                }
            }
        }
        .padding(.vertical, 2)
    }
}

@MainActor private struct LLMModelEditor: View {
    @Binding var model: LLMModelDraft
    let providers: [LLMProviderDraft]
    let onRemove: () -> Void

    var body: some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: 8) {
                LabeledContent("Name") {
                    TextField("text-embedding-3-small", text: $model.name)
                        .frame(width: 220)
                }
                LabeledContent("Provider") {
                    Picker("Provider", selection: $model.provider) {
                        if model.provider.isEmpty { Text("Legacy shared pricing").tag("") }
                        ForEach(providers.filter { !$0.name.isEmpty }) { provider in
                            Text(provider.name).tag(provider.name)
                        }
                    }
                    .labelsHidden()
                    .frame(width: 160)
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
                Toggle("Supports custom dimensions", isOn: $model.supportsCustomDimensions)
                Button("Remove Model", role: .destructive, action: onRemove)
            }
            .padding(.vertical, 4)
        } label: {
            HStack {
                Text(model.name.isEmpty ? "New model" : model.name)
                if !model.provider.isEmpty {
                    Text(model.provider).foregroundStyle(.secondary)
                }
                Spacer()
                Text(model.truncatable ? "truncatable"
                     : model.supportsCustomDimensions ? "custom dimensions"
                     : "\(model.dimensions) dimensions")
                    .foregroundStyle(.secondary)
            }
        }
    }
}

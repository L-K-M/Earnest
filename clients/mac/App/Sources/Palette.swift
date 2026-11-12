import SwiftUI
import EarnestKit

/// ⌘K: type to filter every command, move with the arrow keys, run with
/// Return, dismiss with Escape. Disabled commands are listed but cannot run.
@MainActor struct PaletteView: View {
    @EnvironmentObject private var window: WindowModel
    @Environment(\.dismiss) private var dismiss
    @SwiftUI.State private var filter = ""
    @SwiftUI.State private var highlighted: String?
    @FocusState private var fieldFocused: Bool

    private var commands: [PaletteCommand] {
        let all = window.paletteCommands()
        let query = filter.trimmingCharacters(in: .whitespaces)
        guard !query.isEmpty else { return all }
        return all.filter { $0.title.localizedCaseInsensitiveContains(query) }
    }

    var body: some View {
        let commands = self.commands
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: "command").foregroundStyle(.secondary).accessibilityHidden(true)
                TextField("Type a command", text: $filter)
                    .textFieldStyle(.plain)
                    .font(.title3)
                    .focused($fieldFocused)
                    .onSubmit { run(commands.first { $0.id == highlighted && $0.enabled }) }
                    .onKeyPress(.downArrow) { step(1, in: commands); return .handled }
                    .onKeyPress(.upArrow) { step(-1, in: commands); return .handled }
            }
            .padding(14)
            Divider()
            ScrollViewReader { proxy in
                List(selection: $highlighted) {
                    ForEach(commands) { command in
                        HStack {
                            Text(command.title)
                            Spacer()
                            Text(command.key)
                                .font(.callout.monospaced())
                                .foregroundStyle(.secondary)
                        }
                        .foregroundStyle(command.enabled ? .primary : .tertiary)
                        .contentShape(Rectangle())
                        .tag(command.id)
                        .onTapGesture(count: 2) { run(command) }
                        .accessibilityAddTraits(.isButton)
                        .accessibilityHint(command.enabled ? "" : "Unavailable")
                    }
                }
                .listStyle(.plain)
                .onChange(of: highlighted) { if let highlighted { proxy.scrollTo(highlighted) } }
            }
            if commands.isEmpty {
                Text("No matching commands").foregroundStyle(.secondary).padding()
            }
        }
        .frame(width: 440, height: 380)
        // Escape and Return still work after a click moves focus to the list.
        .onKeyPress(.escape) { dismiss(); return .handled }
        .onKeyPress(.return) {
            // The field submits on its own; this covers a focused list.
            guard !fieldFocused else { return .ignored }
            run(commands.first { $0.id == highlighted })
            return .handled
        }
        .onAppear {
            fieldFocused = true
            highlighted = commands.first { $0.enabled }?.id
        }
        .onChange(of: filter) { highlighted = self.commands.first { $0.enabled }?.id ?? self.commands.first?.id }
    }

    /// Move to the next command that can run, staying put at either end.
    private func step(_ offset: Int, in commands: [PaletteCommand]) {
        guard !commands.isEmpty else { return }
        var index = commands.firstIndex { $0.id == highlighted } ?? (offset > 0 ? -1 : commands.count)
        repeat {
            index += offset
        } while commands.indices.contains(index) && !commands[index].enabled
        if commands.indices.contains(index) { highlighted = commands[index].id }
    }

    private func run(_ command: PaletteCommand?) {
        guard let command, command.enabled else {
            NSSound.beep()
            return
        }
        dismiss()
        command.action()
    }
}

/// File > New Subscription: a feed address and its cadence. Return
/// subscribes; the sheet stays open with the error if the server refuses.
@MainActor struct NewSubscriptionSheet: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel
    @Environment(\.dismiss) private var dismiss
    @FocusState private var focused: Bool

    var body: some View {
        Form {
            SwiftUI.Section {
                TextField("Feed address", text: $window.subscribeURL, prompt: Text(verbatim: "https://example.com/feed.xml"))
                    .focused($focused)
                    .onSubmit(subscribe)
                    .autocorrectionDisabled()
                Picker("Cadence", selection: Binding(get: { Cadence(server: window.subscribeCadence) },
                                                     set: { window.subscribeCadence = $0.rawValue })) {
                    ForEach(Cadence.allCases) { Text($0.title).tag($0) }
                }
            } header: {
                Text("New Subscription").font(.headline)
            } footer: {
                Text("The front page selects from every cadence. Cadence decides how long articles stay in the river.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .frame(width: 440)
        .fixedSize(horizontal: false, vertical: true)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { dismiss() }
            }
            ToolbarItem(placement: .confirmationAction) {
                Button("Subscribe", action: subscribe)
                    .disabled(window.subscribeURL.trimmingCharacters(in: .whitespaces).isEmpty || model.subscribing)
            }
        }
        .onAppear { focused = true }
    }

    private func subscribe() {
        window.subscribe { dismiss() }
    }
}

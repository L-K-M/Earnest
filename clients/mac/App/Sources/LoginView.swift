import AppKit
import SwiftUI
import EarnestKit

/// Signed out: choose a server, then approve this Mac from a browser where
/// you are signed in to Earnest. The browser opens only when you ask.
@MainActor struct LoginView: View {
    @EnvironmentObject private var model: AppModel
    @FocusState private var serverFocused: Bool

    var body: some View {
        VStack(spacing: 22) {
            BrandLockup()
            if let code = model.code {
                approval(code)
            } else if model.connected {
                signIn
            } else {
                serverForm
            }
            if let note = model.connectionNote {
                Label(note, systemImage: "exclamationmark.icloud")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(48)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .onAppear { serverFocused = !model.connected }
    }

    private var serverForm: some View {
        VStack(spacing: 14) {
            Text("Connect to your Earnest server")
                .font(.title3)
            TextField("Server address", text: $model.server, prompt: Text("earnest.example.com"))
                .textFieldStyle(.roundedBorder)
                .frame(width: 320)
                .focused($serverFocused)
                .onSubmit(connect)
                .autocorrectionDisabled()
            Text("Use the address you open Earnest at in your browser.")
                .font(.caption)
                .foregroundStyle(.secondary)
            Button("Continue", action: connect)
                .keyboardShortcut(.defaultAction)
                .controlSize(.large)
                .disabled(model.server.trimmingCharacters(in: .whitespaces).isEmpty)
        }
    }

    private var signIn: some View {
        VStack(spacing: 14) {
            Text("Sign in to \(serverName)")
                .font(.title3)
            Text("Earnest gives you a code to approve this Mac in your browser.")
                .font(.callout)
                .foregroundStyle(.secondary)
            HStack {
                Button("Use a Different Server") { model.changeServer() }
                Button("Sign In") { model.login() }
                    .keyboardShortcut(.defaultAction)
                    .disabled(model.loggingIn || model.server.isEmpty)
            }
            .controlSize(.large)
            if model.loggingIn {
                ProgressView().controlSize(.small)
            }
        }
    }

    private func approval(_ code: DeviceAuthorization) -> some View {
        VStack(spacing: 14) {
            Text("Approve this Mac")
                .font(.title3)
            Text("Open the sign-in page in a browser where you are signed in to Earnest, and enter this code:")
                .font(.callout)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 360)
            Text(verbatim: code.userCode)
                .font(.system(size: 34, weight: .semibold, design: .monospaced))
                .textSelection(.enabled)
                .padding(.horizontal, 20)
                .padding(.vertical, 8)
                .background(.quaternary, in: .rect(cornerRadius: 10))
                .accessibilityLabel("Code \(code.userCode.map(String.init).joined(separator: " "))")
            Text(verbatim: code.verificationURI)
                .font(.caption)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
            HStack {
                Button("Cancel") { model.cancelLogin() }
                    .keyboardShortcut(.cancelAction)
                Button("Copy Code") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(code.userCode, forType: .string)
                }
                Button("Open Sign-In Page") { model.openSignInPage() }
                    .keyboardShortcut(.defaultAction)
                    .disabled(model.signInPage == nil)
            }
            .controlSize(.large)
            HStack(spacing: 6) {
                ProgressView().controlSize(.small)
                Text("Waiting for approval…").font(.callout).foregroundStyle(.secondary)
            }
        }
    }

    private var serverName: String {
        URL(string: model.server)?.host() ?? model.server
    }

    private func connect() {
        guard !model.server.trimmingCharacters(in: .whitespaces).isEmpty else { return }
        model.connect()
    }
}

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
                // The ribbon sits just under the lockup and, through the
                // bottom padding, is exactly as wide as it.
                .padding(.bottom, 12)
                .overlay(alignment: .bottom) { BrandRibbon() }
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
            // Not selectable: on macOS 27, selectable text with its own
            // accessibility label crashed the app when an accessibility
            // client read it. Copy Code copies the code instead.
            Text(verbatim: code.userCode)
                .font(.system(size: 34, weight: .semibold, design: .monospaced))
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

/// A thin band of the icon's own colours, drawn as seven equal hard-edged
/// segments. Fixed brand colours, never themed. Decorative only, so it is
/// hidden from assistive technology.
private struct BrandRibbon: View {
    /// The spec's ribbon order: #FFA01C, #EA5F12, #C9481E, #8E3B68,
    /// #916E9D, #408CAD, #5DA8A1.
    private static let segments: [Color] = [
        Color(red: 1.0, green: 0.62745098, blue: 0.10980392),
        Color(red: 0.91764706, green: 0.37254902, blue: 0.07058824),
        Color(red: 0.78823529, green: 0.28235294, blue: 0.11764706),
        Color(red: 0.55686275, green: 0.23137255, blue: 0.40784314),
        Color(red: 0.56862745, green: 0.43137255, blue: 0.61568627),
        Color(red: 0.25098039, green: 0.54901961, blue: 0.67843137),
        Color(red: 0.36470588, green: 0.65882353, blue: 0.63137255)
    ]

    var body: some View {
        HStack(spacing: 0) {
            ForEach(Array(Self.segments.enumerated()), id: \.offset) { _, segment in
                Rectangle().fill(segment)
            }
        }
        .frame(height: 4)
        .clipShape(Capsule())
        .accessibilityHidden(true)
    }
}

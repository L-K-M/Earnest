import AppKit
import EarnestKit
import SwiftUI

enum Section: String, CaseIterable, Identifiable {
    case today = "Today", edition = "Edition", later = "Later", kept = "Kept", done = "Done", feeds = "Feeds", activity = "Activity"
    var id: String { rawValue }
    var view: String? {
        switch self {
        case .today: return "river"
        case .later: return "later"
        case .kept: return "kept"
        case .done: return "done"
        default: return nil
        }
    }
}

@MainActor final class AppModel: ObservableObject {
    @Published var server = UserDefaults.standard.string(forKey: "server") ?? ""
    @Published var section: Section = .today
    @Published var entries: [APIEntry] = []
    @Published var selected: Int64?
    @Published var html = ""
    @Published var error: String?
    @Published var code: DeviceAuthorization?
    @Published var loggedIn = false
    @Published var connected = false
    @Published var palette = false
    private var api: EarnestAPI?

    func connect() {
        do {
            guard let url = URL(string: server) else { throw APIError.invalidBaseURL }
            let store = KeychainTokens(server: url)
            api = try EarnestAPI(baseURL: url, tokenStore: store)
            connected = true
            UserDefaults.standard.set(server, forKey: "server")
            loggedIn = try store.load() != nil
            if loggedIn { Task { await refresh() } }
        } catch { self.error = String(describing: error) }
    }

    func signOut() {
        do {
            guard let url = URL(string: server) else { throw APIError.invalidBaseURL }
            try KeychainTokens(server: url).delete()
            loggedIn = false
            selected = nil
            entries = []
            html = ""
        } catch { self.error = String(describing: error) }
    }

    func login() async {
        do {
            guard let api else { return }
            let authorization = try await api.startDevice(clientName: "Earnest for Mac")
            code = authorization
            if let url = URL(string: authorization.verificationURI), ["http", "https"].contains(url.scheme?.lowercased() ?? "") { NSWorkspace.shared.open(url) }
            let deadline = Date().addingTimeInterval(TimeInterval(authorization.expiresIn))
            var interval = max(1, authorization.interval)
            while Date() < deadline {
                try await Task.sleep(for: .seconds(interval))
                do {
                    _ = try await api.pollDevice(deviceCode: authorization.deviceCode)
                    code = nil
                    loggedIn = true
                    await refresh()
                    return
                } catch let problem as APIProblem {
                    if problem.error == "authorization_pending" || problem.detail == "authorization_pending" { continue }
                    if problem.error == "slow_down" || problem.detail == "slow_down" { interval += 5; continue }
                    throw problem
                }
            }
            self.error = "The device code expired. Start again."
            code = nil
        } catch {
            code = nil
            self.error = String(describing: error)
        }
    }

    func refresh() async {
        let requestedSection = section
        guard let api, let view = requestedSection.view else { entries = []; return }
        do {
            var all: [APIEntry] = []
            var cursor: String?
            var seenCursors = Set<String>()
            repeat {
                let page = try await api.entries(view: view, cursor: cursor)
                all.append(contentsOf: page.entries ?? [])
                cursor = page.nextCursor
                if let cursor, !seenCursors.insert(cursor).inserted { throw APIError.invalidResponse }
            } while cursor != nil && !Task.isCancelled
            if section == requestedSection { entries = all }
        } catch { self.error = String(describing: error) }
    }

    func open(_ id: Int64?) async {
        html = ""
        guard let api, let id else { return }
        do {
            let content = try await api.content(entryID: id)
            if selected == id { html = content.html }
        } catch { self.error = String(describing: error) }
    }

    func move(_ offset: Int) {
        guard !entries.isEmpty else { return }
        let current = entries.firstIndex(where: { $0.id == selected }) ?? (offset > 0 ? -1 : entries.count)
        selected = entries[min(entries.count - 1, max(0, current + offset))].id
    }

    func openOriginal() {
        guard let entry = entries.first(where: { $0.id == selected }), let url = URL(string: entry.url),
              ["http", "https"].contains(url.scheme?.lowercased() ?? "") else { return }
        NSWorkspace.shared.open(url)
    }
}

@main struct EarnestApp: App {
    @StateObject private var model = AppModel()
    var body: some Scene {
        WindowGroup {
            ContentView().environmentObject(model)
                .onOpenURL { url in
                    guard let id = DeepLink.entryID(url) else { return }
                    if model.selected == id { Task { await model.open(id) } }
                    else { model.selected = id }
                }
        }
        .commands { ReaderCommands(model: model) }
    }
}

struct ReaderCommands: Commands {
    @ObservedObject var model: AppModel
    var body: some Commands {
        CommandMenu("Article") {
            Button("Next Article") { model.move(1) }.keyboardShortcut("j", modifiers: [])
            Button("Previous Article") { model.move(-1) }.keyboardShortcut("k", modifiers: [])
            Button("Done") {}.keyboardShortcut("e", modifiers: []).disabled(true)
            Button("Later") {}.keyboardShortcut("s", modifiers: []).disabled(true)
            Button("Keep") {}.keyboardShortcut("f", modifiers: []).disabled(true)
            Button("Mark Unread") {}.keyboardShortcut("m", modifiers: []).disabled(true)
            Button("Open Original") { model.openOriginal() }.keyboardShortcut("o", modifiers: [])
            Button("View Original") { model.openOriginal() }.keyboardShortcut("v", modifiers: [])
            Divider()
            Button("Command Palette") { model.palette = true }.keyboardShortcut("k", modifiers: .command)
        }
    }
}

struct ContentView: View {
    @EnvironmentObject private var model: AppModel
    var body: some View {
        Group {
            if model.loggedIn {
                NavigationSplitView {
                    List(Section.allCases) { section in
                        Button(section.rawValue) { model.section = section }.buttonStyle(.plain)
                    }
                        .navigationSplitViewColumnWidth(min: 150, ideal: 180)
                } content: {
                    Group {
                        if model.section.view == nil {
                            ContentUnavailableView("Not available yet", systemImage: "tray", description: Text("This section has not been implemented."))
                        } else {
                            EntryTable(entries: model.entries.map { EntryRow(id: $0.id, title: $0.title) }, selection: $model.selected)
                        }
                    }.navigationSplitViewColumnWidth(min: 200, ideal: 300)
                } detail: {
                    if model.selected != nil { Reader(html: model.html) }
                    else { ContentUnavailableView("Select an article", systemImage: "doc.text") }
                }
                .toolbar {
                    Button("Refresh", systemImage: "arrow.clockwise") { Task { await model.refresh() } }
                    Button("Sign Out", systemImage: "rectangle.portrait.and.arrow.right") { model.signOut() }
                }
                .onChange(of: model.section) { _, _ in Task { await model.refresh() } }
                .onChange(of: model.selected) { _, id in Task { await model.open(id) } }
            } else {
                VStack(alignment: .leading, spacing: 12) {
                    Text("Connect to Earnest").font(.title)
                    TextField("Server URL", text: $model.server)
                    Button("Connect") { model.connect() }
                    if model.code == nil { Button("Sign in") { Task { await model.login() } }.disabled(!model.connected) }
                    if let code = model.code {
                        Text("Enter this code in your browser: \(code.userCode)").textSelection(.enabled)
                        Text(code.verificationURI).textSelection(.enabled)
                    }
                }.padding().frame(width: 420)
            }
        }
        .onAppear { if !model.server.isEmpty { model.connect() } }
        .alert("Earnest", isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
            Button("OK") { model.error = nil }
        } message: { Text(model.error ?? "") }
        .sheet(isPresented: $model.palette) {
            VStack(alignment: .leading, spacing: 12) {
                Text("Commands").font(.headline)
                Button("Next Article") { model.move(1); model.palette = false }
                Button("Previous Article") { model.move(-1); model.palette = false }
                Button("Open Original") { model.openOriginal(); model.palette = false }
            }.padding().frame(width: 300)
        }
    }
}

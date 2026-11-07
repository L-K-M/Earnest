import Foundation
import EarnestKit

struct AccountIdentity: Codable, Equatable {
    let server: URL
    let userID: Int64
    let principalID: Int64

    init(server: URL, userID: Int64, principalID: Int64) throws {
        guard userID > 0, principalID > 0 else { throw APIError.invalidResponse }
        self.server = try Self.origin(server)
        self.userID = userID
        self.principalID = principalID
    }

    static func origin(_ url: URL) throws -> URL {
        guard var parts = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let scheme = parts.scheme?.lowercased(), ["http", "https"].contains(scheme),
              let host = parts.host?.lowercased(), !host.isEmpty,
              parts.user == nil, parts.password == nil, parts.query == nil, parts.fragment == nil,
              parts.path.isEmpty || parts.path == "/" else { throw APIError.invalidBaseURL }
        parts.scheme = scheme
        parts.host = host
        parts.path = ""
        if parts.port == (scheme == "https" ? 443 : 80) { parts.port = nil }
        guard let origin = parts.url else { throw APIError.invalidBaseURL }
        return origin
    }

    var cacheKey: String { "\(server.absoluteString)\n\(userID)\n\(principalID)" }
}

/// Only /v1/me may establish this record. Credentials remain in Keychain.
struct IdentityStore {
    private let defaults: UserDefaults
    init(defaults: UserDefaults = .standard) { self.defaults = defaults }

    func load(server: URL) throws -> AccountIdentity? {
        let origin = try AccountIdentity.origin(server)
        guard let data = defaults.data(forKey: key(origin)) else { return nil }
        let saved = try JSONDecoder().decode(AccountIdentity.self, from: data)
        let identity = try AccountIdentity(server: saved.server, userID: saved.userID, principalID: saved.principalID)
        guard identity.server == origin else { throw APIError.invalidResponse }
        return identity
    }

    func save(_ identity: AccountIdentity) throws {
        defaults.set(try JSONEncoder().encode(identity), forKey: key(identity.server))
    }

    func remove(server: URL) {
        guard let origin = try? AccountIdentity.origin(server) else { return }
        defaults.removeObject(forKey: key(origin))
    }

    private func key(_ server: URL) -> String { "verifiedIdentity.\(server.absoluteString)" }
}

enum SessionResolution: Equatable {
    case verified(AccountIdentity)
    case offline(AccountIdentity)

    var identity: AccountIdentity {
        switch self { case .verified(let identity), .offline(let identity): return identity }
    }

    static func resolve(server: URL, result: Result<APIValue, Error>, remembered: AccountIdentity?) throws -> Self {
        switch result {
        case .success(let me):
            guard let user = me["user_id"]?.int64Value, let principal = parsePrincipalID(me) else {
                throw APIError.invalidResponse
            }
            return .verified(try AccountIdentity(server: server, userID: user, principalID: principal))
        case .failure(let error):
            guard ErrorPresentation.isTransient(error), let remembered,
                  remembered.server == (try AccountIdentity.origin(server)) else { throw error }
            return .offline(remembered)
        }
    }
}

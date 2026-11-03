import Foundation

/// Secure token persistence is supplied by the host application.
public protocol TokenStore: Sendable {
    func load() throws -> String?
    func save(_ token: String) throws
    func delete() throws
}

public struct DeviceAuthorization: Codable, Sendable {
    public let deviceCode: String
    public let userCode: String
    public let verificationURI: String
    public let interval: Int64
    public let expiresIn: Int64

    enum CodingKeys: String, CodingKey {
        case deviceCode = "device_code", userCode = "user_code", verificationURI = "verification_uri"
        case interval, expiresIn = "expires_in"
    }
}

public struct DeviceToken: Codable, Sendable {
    public let accessToken: String
    public let tokenType: String
    enum CodingKeys: String, CodingKey { case accessToken = "access_token", tokenType = "token_type" }
}

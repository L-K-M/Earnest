import Foundation
import EarnestKit

enum ErrorPresentation {
    static func isConnection(_ error: Error) -> Bool {
        let error = error as NSError
        guard error.domain == NSURLErrorDomain else { return false }
        switch URLError.Code(rawValue: error.code) {
        case .notConnectedToInternet, .cannotConnectToHost, .cannotFindHost, .dnsLookupFailed,
             .networkConnectionLost, .timedOut, .internationalRoamingOff, .dataNotAllowed:
            return true
        default: return false
        }
    }

    static func isUnauthorized(_ error: Error) -> Bool {
        if let problem = error as? APIProblem { return problem.status == 401 }
        if case APIError.httpStatus(401) = error { return true }
        return false
    }

    /// A revision conflict: the draft was loaded from an older configuration
    /// and must be reloaded before it can be saved.
    static func isConflict(_ error: Error) -> Bool {
        if let problem = error as? APIProblem { return problem.status == 409 }
        if case APIError.httpStatus(409) = error { return true }
        return false
    }

    /// Failures that can clear without user action: connection dropouts and
    /// timeouts, rate limiting, server errors, and malformed responses (the
    /// generated client surfaces undecodable bodies as DecodingError). Auth
    /// and other client errors stay fatal so a bad credential is not retried
    /// forever.
    static func isTransient(_ error: Error) -> Bool {
        if isConnection(error) { return true }
        let urlError = error as NSError
        if urlError.domain == NSURLErrorDomain {
            return [.badServerResponse, .cannotParseResponse, .cannotDecodeContentData, .cannotDecodeRawData]
                .contains(URLError.Code(rawValue: urlError.code))
        }
        if let problem = error as? APIProblem { return [408, 429].contains(problem.status) || problem.status >= 500 }
        if case APIError.httpStatus(let status) = error { return [408, 429].contains(status) || status >= 500 }
        if case APIError.invalidResponse = error { return true }
        return error is DecodingError
    }

    static func message(_ error: Error) -> String {
        if isUnauthorized(error) { return "Your sign-in expired or was revoked. Sign in again." }
        if isConnection(error) { return "Offline. Showing what was last synced." }
        if let problem = error as? APIProblem { return problem.detail ?? problem.title }
        if let error = error as? APIError {
            switch error {
            case .invalidBaseURL: return "Enter a valid HTTP or HTTPS server address."
            case .invalidDocument: return "This file isn't a valid starred-articles export."
            case .invalidResponse: return "The server sent an unexpected response. Try again."
            case .invalidEntryID: return "This article ID is invalid."
            case .httpStatus(403): return "You don't have permission to do that."
            case .httpStatus(404): return "This item is no longer available."
            case .httpStatus(let status): return "The server couldn't complete the request (HTTP \(status)). Try again."
            }
        }
        return error.localizedDescription
    }
}

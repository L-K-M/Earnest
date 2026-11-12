import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import HTTPTypes
import OpenAPIRuntime

/// A single transport keeps bearer headers and redirect policy identical for generated calls.
struct APITransport: ClientTransport {
    let session: URLSession
    let tokenStore: any TokenStore

    func send(_ request: HTTPRequest, body: HTTPBody?, baseURL: URL, operationID: String) async throws -> (HTTPResponse, HTTPBody?) {
        guard let path = request.path, let url = URL(string: baseURL.absoluteString.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + path) else {
            throw APIError.invalidBaseURL
        }
        var outgoing = URLRequest(url: url)
        outgoing.httpMethod = request.method.rawValue
        for field in request.headerFields { outgoing.setValue(field.value, forHTTPHeaderField: field.name.rawName) }
        if operationID != "device-start", operationID != "device-poll", let token = try tokenStore.load() {
            outgoing.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        // The kept import accepts a 64 MiB export; every other route keeps the
        // default 8 MiB bound, which the server also enforces.
        let bodyLimit = operationID == "import-kept" ? 64 * 1024 * 1024 : 8 * 1024 * 1024
        if let body { outgoing.httpBody = try await Data(collecting: body, upTo: bodyLimit) }
        let (data, response) = try await session.apiData(for: outgoing)
        guard let response = response as? HTTPURLResponse else { throw APIError.invalidResponse }
        try validateHTTP(response, data: data)
        var headers = HTTPFields()
        for (key, value) in response.allHeaderFields {
            if let name = HTTPField.Name(String(describing: key)) { headers[name] = String(describing: value) }
        }
        return (HTTPResponse(status: .init(code: response.statusCode), headerFields: headers), HTTPBody(data))
    }
}

extension URLSession {
    /// Foundation reports a cancelled request as a URL error. Normalize it
    /// so callers can handle it alongside other Swift task cancellations.
    func apiData(for request: URLRequest) async throws -> (Data, URLResponse) {
        do { return try await data(for: request) }
        catch let error as URLError where error.code == .cancelled { throw CancellationError() }
    }
}

func validateHTTP(_ response: HTTPURLResponse, data: Data) throws {
    guard (200..<300).contains(response.statusCode) else {
        if let problem = try? JSONDecoder().decode(APIProblem.self, from: data) { throw problem }
        throw APIError.httpStatus(response.statusCode)
    }
}

/// Go writes RFC 3339 with fractional seconds whenever they are non-zero, for
/// example 2026-11-03T11:48:38.509896+01:00; the runtime's default transcoder
/// accepts only whole seconds.
struct RFC3339DateTranscoder: DateTranscoder {
    func encode(_ date: Date) throws -> String { ISO8601DateFormatter().string(from: date) }

    func decode(_ text: String) throws -> Date {
        guard let date = parseAPIDate(text) else {
            throw DecodingError.dataCorrupted(.init(codingPath: [], debugDescription: "Invalid RFC 3339 date"))
        }
        return date
    }
}

/// Generated wire types stay internal; app and sync types retain stable names and Int64 IDs.
func adapt<T: Decodable>(_ value: some Encodable, as type: T.Type = T.self) throws -> T {
    let encoder = JSONEncoder()
    encoder.dateEncodingStrategy = .iso8601
    let decoder = JSONDecoder()
    decoder.dateDecodingStrategy = .custom { decoder in
        let container = try decoder.singleValueContainer()
        let text = try container.decode(String.self)
        guard let date = parseAPIDate(text) else {
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Invalid RFC 3339 date")
        }
        return date
    }
    return try decoder.decode(type, from: encoder.encode(value))
}

func generated<T: Sendable>(_ operation: () async throws -> T) async throws -> T {
    do { return try await operation() }
    catch let error as ClientError { throw error.underlyingError }
}

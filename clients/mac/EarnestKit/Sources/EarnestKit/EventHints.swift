import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// SSE carries cursor hints only. The coordinator always reads the durable change feed.
struct EventHintParser {
    private var buffer = Data()
    private var event = ""
    private var payload: [String] = []

    mutating func append(_ data: Data) throws -> [APIRevision] {
        buffer.append(data)
        guard buffer.count <= 65536 else { throw APIError.invalidResponse }
        var hints: [APIRevision] = []
        while let newline = buffer.firstIndex(of: 10) {
            var lineData = buffer.prefix(upTo: newline)
            if lineData.last == 13 { lineData = lineData.dropLast() }
            guard let line = String(data: lineData, encoding: .utf8) else { throw APIError.invalidResponse }
            buffer.removeSubrange(...newline)
            if line.isEmpty {
                if event == "changes", !payload.isEmpty {
                    hints.append(try JSONDecoder().decode(APIRevision.self, from: Data(payload.joined(separator: "\n").utf8)))
                }
                event = ""
                payload = []
                continue
            }
            if line.hasPrefix(":") { continue }
            let parts = line.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
            let value = parts.count > 1 ? String(parts[1]).replacingOccurrences(of: "^ ", with: "", options: .regularExpression) : ""
            if parts[0] == "event" { event = value }
            if parts[0] == "data" {
                payload.append(value)
                guard payload.reduce(0, { $0 + $1.utf8.count }) <= 65536 else { throw APIError.invalidResponse }
            }
        }
        return hints
    }
}

// URLSession serializes delegate callbacks. The only cross-thread method cancels its session.
final class EventConnection: NSObject, URLSessionDataDelegate, @unchecked Sendable {
    private let continuation: AsyncThrowingStream<APIRevision, Error>.Continuation
    private var parser = EventHintParser()

    init(_ continuation: AsyncThrowingStream<APIRevision, Error>.Continuation) { self.continuation = continuation }

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive response: URLResponse,
                    completionHandler: @escaping @Sendable (URLSession.ResponseDisposition) -> Void) {
        guard let response = response as? HTTPURLResponse else {
            continuation.finish(throwing: APIError.invalidResponse)
            completionHandler(.cancel)
            return
        }
        guard response.statusCode == 200 else {
            continuation.finish(throwing: APIError.httpStatus(response.statusCode))
            completionHandler(.cancel)
            return
        }
        guard response.mimeType == "text/event-stream" else {
            continuation.finish(throwing: APIError.invalidResponse)
            completionHandler(.cancel)
            return
        }
        completionHandler(.allow)
    }

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        do { for hint in try parser.append(data) { continuation.yield(hint) } }
        catch { continuation.finish(throwing: error); session.invalidateAndCancel() }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: (any Error)?) {
        if let error { continuation.finish(throwing: error) }
        else { continuation.finish() }
        session.finishTasksAndInvalidate()
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

func eventStream(baseURL: URL, token: String?, cursor: String?) -> AsyncThrowingStream<APIRevision, Error> {
    AsyncThrowingStream(bufferingPolicy: .bufferingNewest(1)) { continuation in
        let delegate = EventConnection(continuation)
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 90
        configuration.timeoutIntervalForResource = .greatestFiniteMagnitude
        let session = URLSession(configuration: configuration, delegate: delegate, delegateQueue: nil)
        var request = URLRequest(url: baseURL.appendingPathComponent("v1/events"))
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let cursor { request.setValue(cursor, forHTTPHeaderField: "Last-Event-ID") }
        let task = session.dataTask(with: request)
        continuation.onTermination = { _ in task.cancel(); session.invalidateAndCancel() }
        task.resume()
    }
}

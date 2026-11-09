import AppKit
import Foundation
import EarnestKit

/// Thumbnails and feed icons for one account. Hits and misses are remembered
/// in memory for the life of the store, keyed by identity and path so one
/// account's images never reach another's rows; misses are memory-only so a
/// 404 does not retry every time a row scrolls past. Paths carrying `?v=`
/// also persist in the identity's cache, which is safe because the server
/// never changes the content behind a version.
@MainActor final class MediaStore {
    private let memory = NSCache<NSString, NSImage>()
    private var misses: Set<String> = []
    /// When a load last failed; it is not retried for `retryDelay`, so a
    /// broken image does not cost a request every time its row scrolls past.
    private var failures: [String: Date] = [:]
    private static let retryDelay: TimeInterval = 60
    private var loads: [String: Task<Lookup, Never>] = [:]

    /// A load's outcome. Only `absent`, the server's answer that nothing is
    /// stored, is remembered; a failed load is retried on the next ask.
    private enum Lookup {
        case found(NSImage)
        case absent
        case failed
    }
    private let identity: String
    private let api: EarnestAPI?
    private let cache: Cache?

    init(identity: String = "", api: EarnestAPI? = nil, cache: Cache? = nil) {
        self.identity = identity
        self.api = api
        self.cache = cache
    }

    /// The image at a server media path, or nil when there is none or it
    /// cannot be loaded. Concurrent asks for one path share a single load.
    func image(for path: String) async -> NSImage? {
        let key = "\(identity)\n\(path)"
        if let image = memory.object(forKey: key as NSString) { return image }
        if misses.contains(key) { return nil }
        if let failed = failures[key], Date().timeIntervalSince(failed) < Self.retryDelay { return nil }
        guard api != nil else { return nil }
        let load = loads[key] ?? startLoad(key: key, path: path)
        switch await load.value {
        case .found(let image):
            failures[key] = nil
            memory.setObject(image, forKey: key as NSString)
            return image
        case .absent:
            misses.insert(key)
            return nil
        case .failed:
            failures[key] = Date()
            return nil
        }
    }

    /// The image already in memory, without loading: a recycled row shows
    /// it at once instead of flashing an empty slot.
    func cachedImage(for path: String) -> NSImage? {
        memory.object(forKey: "\(identity)\n\(path)" as NSString)
    }

    private func startLoad(key: String, path: String) -> Task<Lookup, Never> {
        let task: Task<Lookup, Never> = Task { [weak self] in
            defer { self?.loads[key] = nil }
            guard let self, let api = self.api else { return .failed }
            let versioned = path.contains("?v=")
            // SQLite reads and writes run off the main actor so scrolling a
            // list of new images does not wait on the database.
            let cache = self.cache
            if versioned, let data = await Task.detached(priority: .utility, operation: { try? cache?.media(path: path) }).value,
               let image = NSImage(data: data) {
                return .found(image)
            }
            let fetched: Data?
            do { fetched = try await api.media(path: path) } catch { return .failed }
            guard let data = fetched else { return .absent }
            guard let image = NSImage(data: data) else { return .failed }
            if versioned { Task.detached(priority: .utility) { try? cache?.saveMedia(path: path, data: data) } }
            return .found(image)
        }
        loads[key] = task
        return task
    }
}

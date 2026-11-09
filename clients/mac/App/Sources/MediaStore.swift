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
        guard api != nil else { return nil }
        let load = loads[key] ?? startLoad(key: key, path: path)
        switch await load.value {
        case .found(let image):
            memory.setObject(image, forKey: key as NSString)
            return image
        case .absent:
            misses.insert(key)
            return nil
        case .failed:
            return nil
        }
    }

    private func startLoad(key: String, path: String) -> Task<Lookup, Never> {
        let task: Task<Lookup, Never> = Task { [weak self] in
            defer { self?.loads[key] = nil }
            guard let self, let api = self.api else { return .failed }
            let versioned = path.contains("?v=")
            if versioned, let data = try? self.cache?.media(path: path), let image = NSImage(data: data) {
                return .found(image)
            }
            let fetched: Data?
            do { fetched = try await api.media(path: path) } catch { return .failed }
            guard let data = fetched else { return .absent }
            guard let image = NSImage(data: data) else { return .failed }
            if versioned { try? self.cache?.saveMedia(path: path, data: data) }
            return .found(image)
        }
        loads[key] = task
        return task
    }
}

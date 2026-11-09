import Foundation

/// Trusted operation classification for a queued write. The server derives
/// `source` from this and the token kind; never send a caller-supplied source.
public enum OperationKind: String, Codable, Sendable {
    case explicit, passive
}

public enum ScopeKind: String, Codable, Sendable {
    case all, subscriptions, folder
}

/// A resolved `state:through` scope, fixed at queue time.
public struct Scope: Codable, Equatable, Sendable {
    public var kind: ScopeKind
    public var ids: [Int64]
    public init(kind: ScopeKind, ids: [Int64] = []) {
        self.kind = kind
        self.ids = ids
    }
    private enum CodingKeys: String, CodingKey { case kind, ids }
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        kind = try container.decode(ScopeKind.self, forKey: .kind)
        ids = try container.decodeIfPresent([Int64].self, forKey: .ids) ?? []
    }
}

/// A first-class queued range: scope, fixed boundary and the original action
/// clock are retained; it is never expanded to cached entries because the
/// server also holds uncached members.
public struct RangeWrite: Codable, Equatable, Sendable {
    public var write: Write
    public var scope: Scope
    public var throughSeq: Int64
    public init(write: Write, scope: Scope, throughSeq: Int64) {
        self.write = write
        self.scope = scope
        self.throughSeq = throughSeq
    }
    private enum CodingKeys: String, CodingKey { case write, scope, throughSeq = "through_seq" }
}

/// One ordered outbox item: a register write or a queued range.
public enum OutboxItem: Codable, Equatable, Sendable {
    case write(Write)
    case range(RangeWrite)

    /// The operation payload regardless of item kind.
    public var write: Write {
        switch self {
        case .write(let write): return write
        case .range(let range): return range.write
        }
    }
    public var opID: String { write.opID }
    public var principal: Int64 { write.principal }
    public var entryID: Int64 { write.entryID }
    public var register: Register { write.register }
    public var value: RegisterValue { write.value }
    public var base: Cursor { write.base }
    public var occurredAt: Date { write.occurredAt }
    public var kind: OperationKind { write.kind }
    public var inFlight: Bool { write.inFlight }

    var markedInFlight: OutboxItem {
        switch self {
        case .write(var write):
            write.inFlight = true
            return .write(write)
        case .range(var range):
            range.write.inFlight = true
            return .range(range)
        }
    }
    func rebased(generation: String) -> OutboxItem {
        switch self {
        case .write(var write):
            write.base = Cursor(generation: generation, rev: 0)
            write.inFlight = false
            return .write(write)
        case .range(var range):
            range.write.base = Cursor(generation: generation, rev: 0)
            range.write.inFlight = false
            return .range(range)
        }
    }

    private enum Key: String, CodingKey { case write, range }
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: Key.self)
        if let write = try container.decodeIfPresent(Write.self, forKey: .write) {
            self = .write(write)
        } else if let range = try container.decodeIfPresent(RangeWrite.self, forKey: .range) {
            self = .range(range)
        } else {
            throw DecodingError.dataCorruptedError(forKey: .write, in: container, debugDescription: "outbox item holds neither write nor range")
        }
    }
    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: Key.self)
        switch self {
        case .write(let write): try container.encode(write, forKey: .write)
        case .range(let range): try container.encode(range, forKey: .range)
        }
    }
}

/// A cached entry row. Tombstones merge by revision like live rows, but a
/// locally deleted entry never merges again within a generation.
public struct SyncEntry: Codable, Equatable, Sendable {
    public var id: Int64
    public var subscriptionID: Int64
    public var feedID: Int64
    public var seq: Int64
    public var guid: String?
    public var title: String
    public var url: String
    public var author: String
    public var publishedAt: Date?
    public var updatedAt: Date?
    public var firstSeenAt: Date?
    public var language: String?
    public var rev: Int64
    public var deleted: Bool
    public var hasBody: Bool
    /// Whether the row can be listed: it has a body, or kept the title or
    /// link of an earlier one when a bodyless change, such as a new
    /// thumbnail, raised its revision. Such a row stays listed while it
    /// waits for the REST refresh that restores its body.
    public var hasMetadata: Bool { hasBody || !title.isEmpty || !url.isEmpty }
    public init(id: Int64, subscriptionID: Int64 = 0, feedID: Int64 = 0, seq: Int64 = 0,
                guid: String? = nil, title: String = "", url: String = "", author: String = "",
                publishedAt: Date? = nil, updatedAt: Date? = nil, firstSeenAt: Date? = nil,
                language: String? = nil, rev: Int64 = 0, deleted: Bool = false, hasBody: Bool = true) {
        self.id = id
        self.subscriptionID = subscriptionID
        self.feedID = feedID
        self.seq = seq
        self.guid = guid
        self.title = title
        self.url = url
        self.author = author
        self.publishedAt = publishedAt
        self.updatedAt = updatedAt
        self.firstSeenAt = firstSeenAt
        self.language = language
        self.rev = rev
        self.deleted = deleted
        self.hasBody = hasBody
    }
    public init(from decoder: Decoder) throws {
        guard let decoded = SyncEntry(try APIValue(from: decoder)) else {
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "invalid entry wire value"))
        }
        self = decoded
    }
    public func encode(to encoder: Encoder) throws { try apiValue.encode(to: encoder) }
}

public enum ObjectKind: String, Codable, Sendable {
    case subscription, folder, label, annotation, token, mute, settings
}

/// A revisioned resource: subscription, folder, label membership, annotation
/// or token. Removal retains a tombstone; merges keep the higher revision.
public struct SyncObject: Codable, Equatable, Sendable {
    public var kind: ObjectKind
    public var id: Int64
    public var principal: Int64
    public var entryID: Int64
    public var folderID: Int64
    public var labelID: Int64
    public var name: String
    public var rev: Int64
    public var deleted: Bool
    public var fields: [String: APIValue] = [:]
    public init(kind: ObjectKind, id: Int64, principal: Int64 = 0, entryID: Int64 = 0,
                folderID: Int64 = 0, labelID: Int64 = 0, name: String = "", rev: Int64 = 0,
                deleted: Bool = false) {
        self.kind = kind
        self.id = id
        self.principal = principal
        self.entryID = entryID
        self.folderID = folderID
        self.labelID = labelID
        self.name = name
        self.rev = rev
        self.deleted = deleted
    }
    public init(from decoder: Decoder) throws {
        guard let decoded = SyncObject(try APIValue(from: decoder)) else {
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "invalid object wire value"))
        }
        self = decoded
    }
    public func encode(to encoder: Encoder) throws { try apiValue.encode(to: encoder) }
}

/// A user-owned label name. Memberships are `SyncObject`s of kind `.label`;
/// definitions are keyed by label ID, a different ID space than memberships.
public struct LabelDefinition: Codable, Equatable, Sendable {
    public var id: Int64
    public var name: String
    public var rev: Int64
    public var deleted: Bool
    public init(id: Int64, name: String = "", rev: Int64 = 0, deleted: Bool = false) {
        self.id = id
        self.name = name
        self.rev = rev
        self.deleted = deleted
    }
    public init(from decoder: Decoder) throws {
        guard let decoded = LabelDefinition(try APIValue(from: decoder)) else {
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "invalid label wire value"))
        }
        self = decoded
    }
    public func encode(to encoder: Encoder) throws { try apiValue.encode(to: encoder) }
}

/// Plain-text annotation owned by a principal. `untrusted` marks text the
/// server could not attribute to the reader; render it as plain text.
public struct SyncAnnotation: Codable, Equatable, Sendable {
    public var id: Int64
    public var principalID: Int64
    public var kind: String
    public var entryIDs: [Int64]
    public var value: String
    public var rev: Int64
    public var untrusted: Bool
    public var deleted: Bool
    public init(id: Int64, principalID: Int64 = 0, kind: String = "", entryIDs: [Int64] = [],
                value: String = "", rev: Int64 = 0, untrusted: Bool = false, deleted: Bool = false) {
        self.id = id
        self.principalID = principalID
        self.kind = kind
        self.entryIDs = entryIDs
        self.value = value
        self.rev = rev
        self.untrusted = untrusted
        self.deleted = deleted
    }
    public init(from decoder: Decoder) throws {
        guard let decoded = SyncAnnotation(try APIValue(from: decoder)) else {
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "invalid annotation wire value"))
        }
        self = decoded
    }
    public func encode(to encoder: Encoder) throws { try apiValue.encode(to: encoder) }
}

/// Per-principal read position in a view; last committed writer wins.
public struct SyncPosition: Codable, Equatable, Sendable {
    public var principal: Int64
    public var view: String
    public var seq: Int64
    public var rev: Int64
    public init(principal: Int64, view: String, seq: Int64, rev: Int64 = 0) {
        self.principal = principal
        self.view = view
        self.seq = seq
        self.rev = rev
    }
    public init(from decoder: Decoder) throws {
        guard let decoded = SyncPosition(try APIValue(from: decoder)) else {
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "invalid position wire value"))
        }
        self = decoded
    }
    public func encode(to encoder: Encoder) throws { try apiValue.encode(to: encoder) }
}

/// The server's answer to a queued range. `results` carry the resolved
/// membership and each entry's effective state at commit.
public struct RangeResult: Codable, Equatable, Sendable {
    public var status: WriteStatus
    public var throughSeq: Int64
    public var subscriptions: [Int64]
    public var except: [Int64]
    public var results: [WriteAcknowledgement]
    public var replay: Bool
    public init(status: WriteStatus, throughSeq: Int64 = 0, subscriptions: [Int64] = [],
                except: [Int64] = [], results: [WriteAcknowledgement] = [], replay: Bool = false) {
        self.status = status
        self.throughSeq = throughSeq
        self.subscriptions = subscriptions
        self.except = except
        self.results = results
        self.replay = replay
    }
    public init(from decoder: Decoder) throws {
        guard let decoded = RangeResult(try APIValue(from: decoder)) else {
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "invalid range result wire value"))
        }
        self = decoded
    }
    public func encode(to encoder: Encoder) throws { try apiValue.encode(to: encoder) }
}

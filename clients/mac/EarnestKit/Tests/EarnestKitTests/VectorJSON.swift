import Foundation
@testable import EarnestKit

/// Strict JSON tree for the shared sync vectors. `JSONSerialization` rounds
/// integers through `Double` on some platforms, so this parser keeps signed
/// 64-bit IDs exact (vector 34) and rejects duplicate keys, trailing data and
/// non-integer overflow the fixtures never contain.
indirect enum VJSON: Equatable, Sendable {
    case null, bool(Bool), int(Int64), double(Double), string(String)
    case array([VJSON]), object([String: VJSON])

    enum Failure: Error, Equatable {
        case syntax(String), duplicateKey(String), trailingData, depthLimit
        case missingKey(String), wrongType(String), unknownField(String)
        case badValue(String)
    }

    subscript(key: String) -> VJSON? {
        if case .object(let object) = self { return object[key] }
        return nil
    }
    var array: [VJSON]? {
        if case .array(let array) = self { return array }
        return nil
    }
    var object: [String: VJSON]? {
        if case .object(let object) = self { return object }
        return nil
    }
    var int64: Int64? {
        if case .int(let value) = self { return value }
        return nil
    }
    var string: String? {
        if case .string(let string) = self { return string }
        return nil
    }
    var bool: Bool? {
        if case .bool(let flag) = self { return flag }
        return nil
    }
    var isNull: Bool { self == .null }

    /// Numeric equality used by subset comparison: integers must match
    /// exactly; a floating actual (e.g. age_seconds) equals an integral
    /// expectation when they name the same number.
    func numericallyEquals(_ other: VJSON) -> Bool {
        switch (self, other) {
        case (.int(let a), .int(let b)): return a == b
        case (.double(let a), .double(let b)): return a == b
        case (.int(let a), .double(let b)), (.double(let b), .int(let a)):
            return b.isFinite && b.rounded() == b && abs(b) < 9_007_199_254_740_992 && Int64(b) == a
        default: return false
        }
    }
}

extension VJSON {
    /// `expect` objects are recursive subsets: every expected key must exist
    /// and match; arrays are exact ordered matches; scalars and null compare
    /// exactly (numbers by numeric equality).
    static func subset(expected: VJSON, actual: VJSON, path: String = "") -> String? {
        switch expected {
        case .object(let want):
            guard case .object(let got) = actual else {
                return "\(path.isEmpty ? "$" : path): expected object, got \(actual.describe())"
            }
            for (key, value) in want {
                guard let next = got[key] else { return "\(path).\(key): missing key" }
                if let failure = subset(expected: value, actual: next, path: "\(path).\(key)") { return failure }
            }
            return nil
        case .array(let want):
            guard case .array(let got) = actual, got.count == want.count else {
                return "\(path): array mismatch: want \(expected.describe()), got \(actual.describe())"
            }
            for index in want.indices {
                if let failure = subset(expected: want[index], actual: got[index], path: "\(path)[\(index)]") { return failure }
            }
            return nil
        case .int, .double:
            if !actual.numericallyEquals(expected) {
                return "\(path): want \(expected.describe()), got \(actual.describe())"
            }
            return nil
        case .string(let want):
            guard case .string(let got) = actual else {
                return "\(path): want \(expected.describe()), got \(actual.describe())"
            }
            // Timestamps compare by instant: the Go wire omits fractional
            // seconds while the engine's wire formatter emits them
            // ("…T12:00:00Z" vs "…T12:00:00.000Z") — the same value, not drift.
            if want != got {
                if let expectedDate = try? VectorTime.parse(want),
                   let actualDate = try? VectorTime.parse(got), expectedDate == actualDate {
                    return nil
                }
                return "\(path): want \(expected.describe()), got \(actual.describe())"
            }
            return nil
        default:
            if expected != actual {
                return "\(path): want \(expected.describe()), got \(actual.describe())"
            }
            return nil
        }
    }

    func describe() -> String {
        switch self {
        case .null: return "null"
        case .bool(let flag): return flag ? "true" : "false"
        case .int(let value): return String(value)
        case .double(let value): return String(value)
        case .string(let string): return "\"\(string)\""
        case .array(let array): return "[" + array.map { $0.describe() }.joined(separator: ",") + "]"
        case .object(let object):
            return "{" + object.keys.sorted().map { "\"\($0)\":\(object[$0]!.describe())" }.joined(separator: ",") + "}"
        }
    }
}

extension VJSON {
    /// Required-key accessors. Every misuse is a fixture defect, not a soft nil.
    func required(_ key: String) throws -> VJSON {
        guard let value = self[key] else { throw Failure.missingKey(key) }
        return value
    }
    func requiredArray(_ key: String) throws -> [VJSON] {
        guard let array = try required(key).array else { throw Failure.wrongType(key) }
        return array
    }
    func requiredObject(_ key: String) throws -> [String: VJSON] {
        guard let object = try required(key).object else { throw Failure.wrongType(key) }
        return object
    }
    func requiredInt(_ key: String) throws -> Int64 {
        guard let value = try required(key).int64 else { throw Failure.wrongType(key) }
        return value
    }
    func requiredString(_ key: String) throws -> String {
        guard let value = try required(key).string else { throw Failure.wrongType(key) }
        return value
    }
    /// Reject keys outside the declared set (DisallowUnknownFields parity).
    func checkKeys(_ allowed: Set<String>) throws {
        guard let object else { throw Failure.wrongType("object") }
        for key in object.keys where !allowed.contains(key) { throw Failure.unknownField(key) }
    }
    var arrayOrEmpty: [VJSON] { array ?? [] }
}

// MARK: - Strict parser

struct VJSONParser {
    private var bytes: [UInt8]
    private var index = 0

    static func parse(_ data: Data) throws -> VJSON {
        var parser = VJSONParser(bytes: [UInt8](data))
        let value = try parser.value(depth: 0)
        parser.whitespace()
        guard parser.index == parser.bytes.count else { throw VJSON.Failure.trailingData }
        return value
    }
    static func parse(_ text: String) throws -> VJSON {
        try parse(Data(text.utf8))
    }

    private mutating func whitespace() {
        while index < bytes.count, bytes[index] == 0x20 || bytes[index] == 0x09 || bytes[index] == 0x0A || bytes[index] == 0x0D {
            index += 1
        }
    }
    private mutating func expect(_ byte: UInt8) throws {
        guard index < bytes.count, bytes[index] == byte else {
            throw VJSON.Failure.syntax("expected '\(Character(UnicodeScalar(byte)))' at \(index)")
        }
        index += 1
    }
    private mutating func literal(_ word: String, _ value: VJSON) throws -> VJSON {
        guard index + word.utf8.count <= bytes.count,
              String(bytes: bytes[index..<(index + word.utf8.count)], encoding: .utf8) == word else {
            throw VJSON.Failure.syntax("bad literal at \(index)")
        }
        index += word.utf8.count
        return value
    }

    private mutating func value(depth: Int) throws -> VJSON {
        if depth > 200 { throw VJSON.Failure.depthLimit }
        whitespace()
        guard index < bytes.count else { throw VJSON.Failure.syntax("unexpected end") }
        switch bytes[index] {
        case UInt8(ascii: "{"): return try objectValue(depth: depth + 1)
        case UInt8(ascii: "["): return try arrayValue(depth: depth + 1)
        case UInt8(ascii: "\""): return .string(try stringValue())
        case UInt8(ascii: "t"): index += 1; return try literal("rue", .bool(true))
        case UInt8(ascii: "f"): index += 1; return try literal("alse", .bool(false))
        case UInt8(ascii: "n"): index += 1; return try literal("ull", .null)
        default: return try numberValue()
        }
    }
    private mutating func objectValue(depth: Int) throws -> VJSON {
        try expect(UInt8(ascii: "{"))
        var object: [String: VJSON] = [:]
        whitespace()
        if index < bytes.count, bytes[index] == UInt8(ascii: "}") { index += 1; return .object(object) }
        while true {
            whitespace()
            let key = try stringValue()
            guard object[key] == nil else { throw VJSON.Failure.duplicateKey(key) }
            whitespace()
            try expect(UInt8(ascii: ":"))
            object[key] = try value(depth: depth)
            whitespace()
            guard index < bytes.count else { throw VJSON.Failure.syntax("unterminated object") }
            if bytes[index] == UInt8(ascii: ",") { index += 1; continue }
            try expect(UInt8(ascii: "}"))
            return .object(object)
        }
    }
    private mutating func arrayValue(depth: Int) throws -> VJSON {
        try expect(UInt8(ascii: "["))
        var array: [VJSON] = []
        whitespace()
        if index < bytes.count, bytes[index] == UInt8(ascii: "]") { index += 1; return .array(array) }
        while true {
            array.append(try value(depth: depth))
            whitespace()
            guard index < bytes.count else { throw VJSON.Failure.syntax("unterminated array") }
            if bytes[index] == UInt8(ascii: ",") { index += 1; continue }
            try expect(UInt8(ascii: "]"))
            return .array(array)
        }
    }
    private mutating func stringValue() throws -> String {
        try expect(UInt8(ascii: "\""))
        var scalars = String.UnicodeScalarView()
        while true {
            guard index < bytes.count else { throw VJSON.Failure.syntax("unterminated string") }
            let byte = bytes[index]
            index += 1
            switch byte {
            case UInt8(ascii: "\""):
                return String(scalars)
            case UInt8(ascii: "\\"):
                guard index < bytes.count else { throw VJSON.Failure.syntax("bad escape") }
                let escape = bytes[index]
                index += 1
                switch escape {
                case UInt8(ascii: "\""): scalars.append("\"")
                case UInt8(ascii: "\\"): scalars.append("\\")
                case UInt8(ascii: "/"): scalars.append("/")
                case UInt8(ascii: "b"): scalars.append("\u{08}")
                case UInt8(ascii: "f"): scalars.append("\u{0C}")
                case UInt8(ascii: "n"): scalars.append("\n")
                case UInt8(ascii: "r"): scalars.append("\r")
                case UInt8(ascii: "t"): scalars.append("\t")
                case UInt8(ascii: "u"):
                    let first = try hexQuad()
                    if (0xD800...0xDBFF).contains(first) {
                        guard index + 1 < bytes.count, bytes[index] == UInt8(ascii: "\\"), bytes[index + 1] == UInt8(ascii: "u") else {
                            throw VJSON.Failure.syntax("lone surrogate")
                        }
                        index += 2
                        let second = try hexQuad()
                        guard (0xDC00...0xDFFF).contains(second), let combined = UnicodeScalar(0x10000 + ((first - 0xD800) << 10) + (second - 0xDC00)) else {
                            throw VJSON.Failure.syntax("bad surrogate pair")
                        }
                        scalars.append(combined)
                    } else {
                        guard let scalar = UnicodeScalar(first) else { throw VJSON.Failure.syntax("bad \\u escape") }
                        scalars.append(scalar)
                    }
                default: throw VJSON.Failure.syntax("bad escape at \(index)")
                }
            case 0x00...0x1F:
                throw VJSON.Failure.syntax("control character in string")
            default:
                // Append the whole UTF-8 sequence; fixtures are valid UTF-8.
                if byte < 0x80 {
                    scalars.append(UnicodeScalar(byte))
                } else {
                    var sequence = [byte]
                    let extra = byte < 0xE0 ? 1 : (byte < 0xF0 ? 2 : 3)
                    guard index + extra <= bytes.count else { throw VJSON.Failure.syntax("bad utf8") }
                    sequence.append(contentsOf: bytes[index..<(index + extra)])
                    index += extra
                    guard let text = String(bytes: sequence, encoding: .utf8)?.unicodeScalars.first else {
                        throw VJSON.Failure.syntax("bad utf8")
                    }
                    scalars.append(text)
                }
            }
        }
    }
    private mutating func hexQuad() throws -> Int {
        guard index + 4 <= bytes.count else { throw VJSON.Failure.syntax("short \\u escape") }
        var value = 0
        for byte in bytes[index..<(index + 4)] {
            value <<= 4
            switch byte {
            case UInt8(ascii: "0")...UInt8(ascii: "9"): value += Int(byte - UInt8(ascii: "0"))
            case UInt8(ascii: "a")...UInt8(ascii: "f"): value += Int(byte - UInt8(ascii: "a")) + 10
            case UInt8(ascii: "A")...UInt8(ascii: "F"): value += Int(byte - UInt8(ascii: "A")) + 10
            default: throw VJSON.Failure.syntax("bad \\u escape")
            }
        }
        index += 4
        return value
    }
    private mutating func numberValue() throws -> VJSON {
        let start = index
        if index < bytes.count, bytes[index] == UInt8(ascii: "-") { index += 1 }
        var integral = true
        while index < bytes.count {
            let byte = bytes[index]
            if (UInt8(ascii: "0")...UInt8(ascii: "9")).contains(byte) {
                index += 1
            } else if byte == UInt8(ascii: ".") || byte == UInt8(ascii: "e") || byte == UInt8(ascii: "E") || byte == UInt8(ascii: "+") || byte == UInt8(ascii: "-") {
                integral = false
                index += 1
            } else {
                break
            }
        }
        guard index > start, let text = String(bytes: bytes[start..<index], encoding: .utf8) else {
            throw VJSON.Failure.syntax("bad number at \(start)")
        }
        if integral {
            guard let value = Int64(text) else { throw VJSON.Failure.badValue("integer overflow \(text)") }
            return .int(value)
        }
        guard let value = Double(text), value.isFinite else { throw VJSON.Failure.badValue("number \(text)") }
        return .double(value)
    }
}

// MARK: - RFC 3339

/// The sync model is UTC-only. Go emits RFC3339 without nanos for the fixture
/// clocks and `"0001-01-01T00:00:00Z"` for its zero time; both parse here.
enum VectorTime {
    static let zero = Date(timeIntervalSince1970: -62_135_596_800) // Go time.Time{}

    static func parse(_ text: String) throws -> Date {
        // YYYY-MM-DD[T]HH:MM[:SS][.fff...](Z|±HH:MM)
        var view = Substring(text)
        func digits(_ count: Int) throws -> Int {
            guard view.count >= count, let value = Int(view.prefix(count)), view.prefix(count).allSatisfy(\.isNumber) else {
                throw VJSON.Failure.badValue("timestamp \(text)")
            }
            view = view.dropFirst(count)
            return value
        }
        func take(_ char: Character) throws {
            guard view.first == char else { throw VJSON.Failure.badValue("timestamp \(text)") }
            view = view.dropFirst()
        }
        let year = try digits(4); try take("-")
        let month = try digits(2); try take("-")
        let day = try digits(2); try take("T")
        let hour = try digits(2); try take(":")
        let minute = try digits(2); try take(":")
        let second = try digits(2)
        var fraction = 0.0
        if view.first == "." {
            view = view.dropFirst()
            var digitsSeen = 0
            var scale = 1.0
            while let char = view.first, char.isNumber {
                if digitsSeen < 9 { fraction = fraction * 10 + Double(char.wholeNumberValue!); scale *= 10 }
                digitsSeen += 1
                view = view.dropFirst()
            }
            guard digitsSeen > 0 else { throw VJSON.Failure.badValue("timestamp \(text)") }
            fraction /= scale
        }
        var offset = 0.0
        if view.first == "Z" {
            view = view.dropFirst()
        } else if view.first == "+" || view.first == "-" {
            let sign = view.first == "+" ? 1.0 : -1.0
            view = view.dropFirst()
            let oh = try digits(2); try take(":")
            let om = try digits(2)
            offset = sign * Double(oh * 3600 + om * 60)
        }
        guard view.isEmpty else { throw VJSON.Failure.badValue("timestamp \(text)") }
        var components = DateComponents()
        components.calendar = Calendar(identifier: .gregorian)
        components.timeZone = TimeZone(identifier: "UTC")
        components.year = year; components.month = month; components.day = day
        components.hour = hour; components.minute = minute; components.second = second
        guard let date = components.date else { throw VJSON.Failure.badValue("timestamp \(text)") }
        return date.addingTimeInterval(fraction - offset)
    }

    static func string(_ date: Date) -> String {
        if date == zero { return "0001-01-01T00:00:00Z" }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let c = calendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: date)
        return String(format: "%04d-%02d-%02dT%02d:%02d:%02dZ", c.year!, c.month!, c.day!, c.hour!, c.minute!, c.second!)
    }
}

// MARK: - APIValue bridge

extension VJSON {
    /// Hand a fixture/model value to engine types decoding `APIValue`.
    var apiValue: APIValue {
        switch self {
        case .null: return .null
        case .bool(let flag): return .bool(flag)
        case .int(let value): return .integer(value)
        case .double(let value): return .number(value)
        case .string(let string): return .string(string)
        case .array(let array): return .array(array.map { $0.apiValue })
        case .object(let object): return .object(object.mapValues { $0.apiValue })
        }
    }
    init(_ value: APIValue) {
        switch value {
        case .null: self = .null
        case .bool(let flag): self = .bool(flag)
        case .integer(let number): self = .int(number)
        case .number(let number): self = .double(number)
        case .string(let string): self = .string(string)
        case .array(let array): self = .array(array.map { VJSON($0) })
        case .object(let object): self = .object(object.mapValues { VJSON($0) })
        }
    }
}

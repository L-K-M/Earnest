import Foundation

enum DeepLink {
    static func entryID(_ url: URL) -> Int64? {
        guard url.scheme == "earnest", url.host == "entry", url.user == nil,
              url.password == nil, url.port == nil, url.query == nil, url.fragment == nil else { return nil }
        let path = url.path
        guard path.first == "/", path.dropFirst().allSatisfy({ $0.isASCII && $0.isNumber }),
              let id = Int64(path.dropFirst()), id > 0 else { return nil }
        return id
    }
}

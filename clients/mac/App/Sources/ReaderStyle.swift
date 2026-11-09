import Foundation
import EarnestKit

/// Fixed, enum-driven typography for the reader HTML. Theme, measure and font
/// select one of the server's fixed treatments; no user-supplied CSS reaches
/// the page and the measure is clamped to the server's range.
struct ReaderStyle: Equatable {
    var theme: APIReaderTheme
    var measure: Int64
    var font: APIReaderFont

    static let `default` = ReaderStyle(theme: .system, measure: 66, font: .serif)

    /// Resolve a feed's override over the account-wide settings. A nil
    /// override member inherits, so a partial override keeps the rest.
    static func resolve(_ settings: APIReaderSettings, feedID: Int64?) -> ReaderStyle {
        var theme = settings.theme
        var measure = settings.measure
        var font = settings.font
        if let feedID, let override = settings.feedOverrides[String(feedID)] {
            theme = override.theme ?? theme
            measure = override.measure ?? measure
            font = override.font ?? font
        }
        return ReaderStyle(theme: theme, measure: measure, font: font)
    }

    /// Only fixed values: `light dark` for the system following themes.
    var colorScheme: String {
        switch theme {
        case .system: return "light dark"
        case .light, .eink: return "light"
        case .dark, .black: return "dark"
        }
    }

    private var palette: (background: String, foreground: String) {
        switch theme {
        case .system: return ("Canvas", "CanvasText")
        case .light: return ("#fff", "#202020")
        case .dark: return ("#242424", "#eee")
        case .black: return ("#000", "#fff")
        case .eink: return ("#fff", "#000")
        }
    }

    private var family: String {
        switch font {
        case .serif: return "Georgia,serif"
        case .sans: return "system-ui,sans-serif"
        case .dyslexic: return "Verdana,Arial,sans-serif"
        }
    }

    private var spacing: String {
        font == .dyslexic ? "letter-spacing:.05em;word-spacing:.12em;" : ""
    }

    /// Mirrors the server's reader CSS (`internal/web/content.go`). The
    /// `!important` rules keep the synced palette ahead of publisher styles;
    /// the image policy and CSP stay in the page head.
    var css: String {
        let width = min(72, max(62, measure))
        let palette = palette
        return ":root{color-scheme:\(colorScheme);background:\(palette.background);color:\(palette.foreground)}"
            + "body{max-width:\(width)ch!important;margin:2rem auto;padding:0 1rem;font:1.1rem/1.65 \(family)!important;"
            + "background:\(palette.background)!important;color:\(palette.foreground)!important;\(spacing)overflow-wrap:anywhere}"
            + "img{max-width:100%;height:auto}a{color:inherit}pre{overflow:auto}"
            // Earnest's own header above the publisher HTML; it scrolls with the article.
            + ".earnest-header{margin:0 0 1.75rem;padding-bottom:1rem;border-bottom:1px solid color-mix(in srgb,currentColor 18%,transparent)}"
            + ".earnest-header h1{font-size:1.7em;line-height:1.2;margin:0 0 .45rem;font-weight:650}"
            + ".earnest-header h1 a{text-decoration:none}.earnest-meta{margin:0;font-size:.82em;opacity:.72}"
    }
}

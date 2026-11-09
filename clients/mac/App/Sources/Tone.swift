import AppKit
import SwiftUI

/// The icon's six tones, one named colour set per case in the asset
/// catalogue. Each set carries the spec's light value and a dark variant, so
/// the tones stay legible as icons and dots in both appearances. The sets
/// ship in the app bundle and the tests assert they load.
enum Tone: String {
    case orange = "ToneOrange"
    case amber = "ToneAmber"
    case plum = "TonePlum"
    case purple = "TonePurple"
    case blue = "ToneBlue"
    case green = "ToneGreen"

    var color: Color { Color(rawValue) }

    var nsColor: NSColor {
        guard let color = NSColor(named: rawValue) else {
            fatalError("Missing colour set \(rawValue) in the asset catalog")
        }
        return color
    }
}

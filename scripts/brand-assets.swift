// brand-assets.swift regenerates the app icons and wordmark images the
// clients ship from the two sources in media-sources/. Run it from the
// repository root on macOS after changing a source, then commit the
// outputs with it:
//
//     swift scripts/brand-assets.swift
//
// It needs only CoreGraphics and ImageIO and overwrites its outputs in
// place. media-sources/README.md lists the outputs and the brand colours.

import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

let sRGB = CGColorSpace(name: CGColorSpace.sRGB)!

enum Alpha {
    case opaque, transparent
}

func load(_ path: String) -> CGImage {
    guard let source = CGImageSourceCreateWithURL(URL(fileURLWithPath: path) as CFURL, nil),
          let image = CGImageSourceCreateImageAtIndex(source, 0, nil)
    else { fatalError("Cannot read \(path). Run this from the repository root.") }
    return image
}

func render(_ width: Int, _ height: Int, _ alpha: Alpha = .transparent, _ draw: (CGContext) -> Void) -> CGImage {
    let info = alpha == .opaque ? CGImageAlphaInfo.noneSkipLast : .premultipliedLast
    let context = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
                            space: sRGB, bitmapInfo: info.rawValue)!
    context.interpolationQuality = .high
    draw(context)
    return context.makeImage()!
}

func save(_ image: CGImage, _ path: String) {
    let url = URL(fileURLWithPath: path)
    try! FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
    let destination = CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil)!
    CGImageDestinationAddImage(destination, image, nil)
    guard CGImageDestinationFinalize(destination) else { fatalError("Cannot write \(path).") }
    print(path)
}

func saveText(_ text: String, _ path: String) {
    try! FileManager.default.createDirectory(at: URL(fileURLWithPath: path).deletingLastPathComponent(), withIntermediateDirectories: true)
    try! text.write(toFile: path, atomically: true, encoding: .utf8)
    print(path)
}

/// Apple's icon outline, approximated by the superellipse |x|^5 + |y|^5 = 1.
func squircle(_ rect: CGRect) -> CGPath {
    let path = CGMutablePath()
    let steps = 1440
    for step in 0..<steps {
        let t = Double(step) / Double(steps) * 2 * .pi
        let x = rect.midX + rect.width / 2 * CGFloat(copysign(pow(abs(cos(t)), 0.4), cos(t)))
        let y = rect.midY + rect.height / 2 * CGFloat(copysign(pow(abs(sin(t)), 0.4), sin(t)))
        if step == 0 { path.move(to: CGPoint(x: x, y: y)) } else { path.addLine(to: CGPoint(x: x, y: y)) }
    }
    path.closeSubpath()
    return path
}

/// The art in a squircle filling the canvas, for in-app use and web icons.
func rounded(_ art: CGImage, _ size: Int) -> CGImage {
    render(size, size) { context in
        let rect = CGRect(x: 0, y: 0, width: size, height: size)
        context.addPath(squircle(rect))
        context.clip()
        context.draw(art, in: rect)
    }
}

/// The macOS icon grid: an 824-point body on a 1024-point canvas with
/// the drop shadow from Apple's icon template.
func macIcon(_ art: CGImage, _ size: Int) -> CGImage {
    let scale = CGFloat(size) / 1024
    return render(size, size) { context in
        let body = CGRect(x: 100 * scale, y: 100 * scale, width: 824 * scale, height: 824 * scale)
        context.setShadow(offset: CGSize(width: 0, height: -10 * scale), blur: 20 * scale,
                          color: CGColor(gray: 0, alpha: 0.3))
        context.beginTransparencyLayer(auxiliaryInfo: nil)
        context.addPath(squircle(body))
        context.clip()
        context.draw(art, in: body)
        context.endTransparencyLayer()
    }
}

/// The art scaled to `fraction` of an opaque canvas and mirrored outward
/// to fill the margin, for launchers that crop and move the icon.
func bleed(_ art: CGImage, _ size: Int, _ fraction: CGFloat) -> CGImage {
    render(size, size, .opaque) { context in
        let side = CGFloat(size) * fraction
        let margin = (CGFloat(size) - side) / 2
        for column in -1...1 {
            for row in -1...1 {
                context.saveGState()
                context.translateBy(x: margin + side * (CGFloat(column) + 0.5), y: margin + side * (CGFloat(row) + 0.5))
                context.scaleBy(x: column == 0 ? 1 : -1, y: row == 0 ? 1 : -1)
                context.draw(art, in: CGRect(x: -side / 2, y: -side / 2, width: side, height: side))
                context.restoreGState()
            }
        }
    }
}

/// The lettering of the wordmark, without the icon, as black with the
/// source's coverage in alpha. Clients tint it per theme, because the
/// source's dark teal is unreadable on dark backgrounds.
func lettering(_ wordmark: CGImage) -> CGImage {
    let width = wordmark.width, height = wordmark.height
    var pixels = [UInt8](repeating: 0, count: width * height * 4)
    // The context draws into the array's storage, which stays valid only
    // inside this closure, so the context must not outlive it.
    pixels.withUnsafeMutableBytes { buffer in
        let context = CGContext(data: buffer.baseAddress, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
                                space: sRGB, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        context.draw(wordmark, in: CGRect(x: 0, y: 0, width: width, height: height))
    }
    let alpha = { (x: Int, y: Int) in Int(pixels[(y * width + x) * 4 + 3]) }
    let visible = 16

    // The icon comes first; the lettering starts at the first column
    // with coverage after the first empty column that follows the icon.
    let occupied = (0..<width).map { x in (0..<height).contains { alpha(x, $0) > visible } }
    guard let iconStart = occupied.firstIndex(of: true),
          let gap = occupied[iconStart...].firstIndex(of: false),
          let start = occupied[gap...].firstIndex(of: true)
    else { fatalError("The wordmark has no lettering after the icon.") }

    var top = height, bottom = 0, right = start
    var interior: [Int] = []
    for y in 0..<height {
        for x in start..<width where alpha(x, y) > visible {
            top = min(top, y)
            bottom = max(bottom, y)
            right = max(right, x)
            if alpha(x, y) >= 128 { interior.append(alpha(x, y)) }
        }
    }

    // The source's letters are slightly translucent; stretch their
    // interior coverage to fully opaque.
    guard !interior.isEmpty else { fatalError("The wordmark's lettering is too faint to extract.") }
    interior.sort()
    let full = Double(interior[interior.count * 9 / 10])

    // Pad the lettering's bounds, but never past the source's edges.
    let pad = 4
    let left = max(start - pad, 0), upper = max(top - pad, 0)
    let crop = (x: left, y: upper, width: min(right + pad, width - 1) - left + 1, height: min(bottom + pad, height - 1) - upper + 1)
    var mask = [UInt8](repeating: 0, count: crop.width * crop.height * 4)
    for y in 0..<crop.height {
        for x in 0..<crop.width {
            let source = alpha(crop.x + x, crop.y + y)
            mask[(y * crop.width + x) * 4 + 3] = source > visible ? UInt8(min(255, Double(source) * 255 / full)) : 0
        }
    }
    let provider = CGDataProvider(data: Data(mask) as CFData)!
    return CGImage(width: crop.width, height: crop.height, bitsPerComponent: 8, bitsPerPixel: 32, bytesPerRow: crop.width * 4,
                   space: sRGB, bitmapInfo: CGBitmapInfo(rawValue: CGImageAlphaInfo.premultipliedLast.rawValue),
                   provider: provider, decode: nil, shouldInterpolate: true, intent: .defaultIntent)!
}

func scaled(_ image: CGImage, height: Int) -> CGImage {
    let width = Int((Double(image.width) * Double(height) / Double(image.height)).rounded())
    return render(width, height) { $0.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height)) }
}

let art = load("media-sources/icon.png")
let letters = lettering(load("media-sources/wordmark.png"))

// Mac: the app icon, plus the icon and lettering for the sign-in view.
let catalog = "clients/mac/App/Sources/Assets.xcassets"
var iconEntries: [String] = []
for points in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let name = "icon_\(points)x\(points)\(scale == 2 ? "@2x" : "").png"
        save(macIcon(art, points * scale), "\(catalog)/AppIcon.appiconset/\(name)")
        iconEntries.append(#"    { "filename" : "\#(name)", "idiom" : "mac", "scale" : "\#(scale)x", "size" : "\#(points)x\#(points)" }"#)
    }
}
saveText("{\n  \"images\" : [\n\(iconEntries.joined(separator: ",\n"))\n  ],\n  \"info\" : { \"author\" : \"xcode\", \"version\" : 1 }\n}\n",
         "\(catalog)/AppIcon.appiconset/Contents.json")

func imageSet(_ name: String, _ make: (Int) -> CGImage, height: Int, template: Bool) {
    var entries: [String] = []
    for scale in [1, 2] {
        let file = "\(name.lowercased())\(scale == 2 ? "@2x" : "").png"
        save(make(height * scale), "\(catalog)/\(name).imageset/\(file)")
        entries.append(#"    { "filename" : "\#(file)", "idiom" : "universal", "scale" : "\#(scale)x" }"#)
    }
    let properties = template ? ",\n  \"properties\" : { \"template-rendering-intent\" : \"template\" }" : ""
    saveText("{\n  \"images\" : [\n\(entries.joined(separator: ",\n"))\n  ],\n  \"info\" : { \"author\" : \"xcode\", \"version\" : 1 }\(properties)\n}\n",
             "\(catalog)/\(name).imageset/Contents.json")
}
imageSet("BrandIcon", { rounded(art, $0) }, height: 72, template: false)
imageSet("Wordmark", { scaled(letters, height: $0) }, height: 40, template: true)

// Android: the adaptive icon's foreground layer is 108 dp, of which
// launchers show the middle 72 dp; the art spans 76 dp so the mask never
// reaches its edge, and the mirrored margin covers parallax movement.
let res = "clients/android/app/src/main/res"
for (density, scale) in [("mdpi", 1.0), ("hdpi", 1.5), ("xhdpi", 2.0), ("xxhdpi", 3.0), ("xxxhdpi", 4.0)] {
    save(bleed(art, Int(108 * scale), 76.0 / 108), "\(res)/mipmap-\(density)/ic_launcher_foreground.png")
}
save(rounded(art, 288), "\(res)/drawable-nodpi/brand_icon.png")
save(scaled(letters, height: 160), "\(res)/drawable-nodpi/brand_wordmark.png")

// Android also needs the single-colour glyph: themed launchers tint the
// adaptive icon's monochrome layer, and the status bar draws notification
// icons from alpha alone. Both are vectors built from media-sources/glyph.svg.
func glyphPathData(_ path: String) -> String {
    let svg = try! String(contentsOfFile: path, encoding: .utf8)
    guard svg.contains("viewBox=\"0 0 100 100\""), svg.contains("fill-rule=\"evenodd\""),
          svg.components(separatedBy: "<path").count == 2,
          let data = svg.components(separatedBy: " d=\"").dropFirst().first?.components(separatedBy: "\"").first
    else { fatalError("\(path) must hold one even-odd path in a 0 0 100 100 viewBox.") }
    return data
}

/// A white vector of the glyph, scaled by `scale` and offset by `offset` inside the viewport.
func glyphVector(size: Int, viewport: Int, scale: Double, offset: Double, _ pathData: String) -> String {
    """
    <?xml version="1.0" encoding="utf-8"?>
    <!-- Generated by scripts/brand-assets.swift from media-sources/glyph.svg. -->
    <vector xmlns:android="http://schemas.android.com/apk/res/android"
        android:width="\(size)dp"
        android:height="\(size)dp"
        android:viewportWidth="\(viewport)"
        android:viewportHeight="\(viewport)">
        <group
            android:scaleX="\(scale)"
            android:scaleY="\(scale)"
            android:translateX="\(offset)"
            android:translateY="\(offset)">
            <path
                android:fillColor="#FFFFFFFF"
                android:fillType="evenOdd"
                android:pathData="\(pathData)" />
        </group>
    </vector>

    """
}

let glyph = glyphPathData("media-sources/glyph.svg")
// The monochrome layer shares the 108 dp grid; 54 dp keeps the glyph well
// inside the 66 dp safe zone. Notification icons keep 2 dp of padding.
saveText(glyphVector(size: 108, viewport: 108, scale: 0.54, offset: 27, glyph), "\(res)/drawable/ic_launcher_monochrome.xml")
saveText(glyphVector(size: 24, viewport: 100, scale: 0.84, offset: 8, glyph), "\(res)/drawable/ic_notification.xml")

// Web: favicons, the iOS home screen icon, manifest icons, and the header
// lettering. The maskable icon's art spans 84%, so the 80% safe zone that
// every mask shows holds only art, never the mirrored margin.
let web = "web/public"
save(rounded(art, 32), "\(web)/favicon-32.png")
save(render(180, 180, .opaque) { $0.draw(art, in: CGRect(x: 0, y: 0, width: 180, height: 180)) }, "\(web)/apple-touch-icon.png")
save(rounded(art, 192), "\(web)/icon-192.png")
save(rounded(art, 512), "\(web)/icon-512.png")
save(bleed(art, 512, 0.84), "\(web)/icon-maskable-512.png")
save(scaled(letters, height: 160), "\(web)/wordmark.png")

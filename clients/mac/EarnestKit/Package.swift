// swift-tools-version: 6.0
import PackageDescription
let package = Package(name: "EarnestKit", platforms: [.macOS(.v15)], products: [.library(name: "EarnestKit", targets: ["EarnestKit"])], targets: [.target(name: "EarnestKit"), .testTarget(name: "EarnestKitTests", dependencies: ["EarnestKit"])])

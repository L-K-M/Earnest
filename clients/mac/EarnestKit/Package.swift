// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "EarnestKit",
    platforms: [.macOS(.v15)],
    products: [.library(name: "EarnestKit", targets: ["EarnestKit"])],
    dependencies: [
        .package(url: "https://github.com/apple/swift-openapi-generator", exact: "1.14.0"),
        .package(url: "https://github.com/apple/swift-openapi-runtime", exact: "1.13.0"),
        .package(url: "https://github.com/apple/swift-http-types", exact: "1.7.0"),
    ],
    targets: [
        .target(
            name: "EarnestKit",
            dependencies: [
                .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
                .product(name: "HTTPTypes", package: "swift-http-types"),
            ],
            plugins: [.plugin(name: "OpenAPIGenerator", package: "swift-openapi-generator")]
        ),
        .testTarget(name: "EarnestKitTests", dependencies: ["EarnestKit"]),
    ]
)

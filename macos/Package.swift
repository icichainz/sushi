// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "Sushi",
    platforms: [.macOS(.v12)],
    dependencies: [
        .package(url: "https://github.com/migueldeicaza/SwiftTerm.git", from: "1.2.0"),
    ],
    targets: [
        .executableTarget(
            name: "Sushi",
            dependencies: [.product(name: "SwiftTerm", package: "SwiftTerm")],
            path: "Sources/Sushi"
        ),
    ]
)

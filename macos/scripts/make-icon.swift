// Renders an SVG into a macOS .iconset folder, which iconutil turns into
// an .icns file. Usage: swift make-icon.swift icon.svg Out.iconset
import AppKit

let args = CommandLine.arguments
guard args.count == 3 else {
    FileHandle.standardError.write("usage: make-icon.swift <icon.svg> <out.iconset>\n".data(using: .utf8)!)
    exit(2)
}
guard let image = NSImage(contentsOfFile: args[1]) else {
    FileHandle.standardError.write("cannot read \(args[1])\n".data(using: .utf8)!)
    exit(1)
}
try FileManager.default.createDirectory(atPath: args[2], withIntermediateDirectories: true)

func render(_ pixels: Int, to name: String) throws {
    let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels,
        bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
        colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    rep.size = NSSize(width: pixels, height: pixels)

    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    NSGraphicsContext.current?.imageInterpolation = .high
    image.draw(in: NSRect(x: 0, y: 0, width: pixels, height: pixels), from: .zero, operation: .copy, fraction: 1)
    NSGraphicsContext.restoreGraphicsState()

    let png = rep.representation(using: .png, properties: [:])!
    try png.write(to: URL(fileURLWithPath: args[2]).appendingPathComponent(name))
}

// Each size in points, at 1x and 2x
for points in [16, 32, 128, 256, 512] {
    try render(points, to: "icon_\(points)x\(points).png")
    try render(points * 2, to: "icon_\(points)x\(points)@2x.png")
}

// Generates CardHaul's icons. Run from the repo root: swift tools/make-icons.swift
//   build/appicon.png       1024px app icon (blue tile, SD card + up arrow)
//   build/tray-template.png menu bar template icon (black glyph, macOS tints it)
//   build/tray-color.png    Windows tray icon
import AppKit

func png(_ size: Int, _ draw: (CGFloat) -> Void) -> Data {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size, bitsPerSample: 8,
                               samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB,
                               bytesPerRow: 0, bitsPerPixel: 0)!
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    draw(CGFloat(size))
    NSGraphicsContext.restoreGraphicsState()
    return rep.representation(using: .png, properties: [:])!
}

func symbol(_ name: String, _ points: CGFloat, _ color: NSColor, center: CGPoint, weight: NSFont.Weight = .semibold) {
    guard let image = NSImage(systemSymbolName: name, accessibilityDescription: nil)?
        .withSymbolConfiguration(NSImage.SymbolConfiguration(pointSize: points, weight: weight)
            .applying(.init(paletteColors: [color]))) else { return }
    let s = image.size
    image.draw(in: NSRect(x: center.x - s.width / 2, y: center.y - s.height / 2, width: s.width, height: s.height))
}

func tile(_ size: CGFloat) {
    let rect = NSRect(x: 0, y: 0, width: size, height: size)
    let inner = rect.insetBy(dx: size * 0.1, dy: size * 0.1)
    NSGradient(starting: NSColor(red: 0.98, green: 0.55, blue: 0.20, alpha: 1),
               ending: NSColor(red: 0.90, green: 0.30, blue: 0.25, alpha: 1))!
        .draw(in: NSBezierPath(roundedRect: inner, xRadius: size * 0.2, yRadius: size * 0.2), angle: -90)
    symbol("sdcard.fill", size * 0.30, .white, center: CGPoint(x: rect.midX, y: rect.midY - size * 0.08))
    symbol("arrow.up", size * 0.16, .white, center: CGPoint(x: rect.midX, y: rect.midY + size * 0.22))
}

try! png(1024, tile).write(to: URL(fileURLWithPath: "build/appicon.png"))
try! png(64, tile).write(to: URL(fileURLWithPath: "build/tray-color.png"))
try! png(36) { size in
    symbol("sdcard", size * 0.5, .black, center: CGPoint(x: size / 2, y: size / 2), weight: .medium)
}.write(to: URL(fileURLWithPath: "build/tray-template.png"))

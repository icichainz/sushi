import AppKit

// Sushi.app is a window around the sushi terminal program, which is bundled
// in the app's Resources
let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.run()

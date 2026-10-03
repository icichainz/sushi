import AppKit
import CoreText

final class AppDelegate: NSObject, NSApplicationDelegate {
    /// The open windows, in the order they were opened
    private(set) var windows: [TerminalWindowController] = []

    /// The home window opened at launch, which a folder arriving in the
    /// next seconds replaces rather than opening a second window: when
    /// Finder's service starts the app, the folder comes after the launch.
    private weak var launchWindow: TerminalWindowController?
    private var launchWindowExpiry = Date.distantPast

    private let defaultFontSize: CGFloat = 13
    private let fontSizeKey = "fontSize"

    func applicationWillFinishLaunching(_ notification: Notification) {
        buildMenu()
        // Before the launch is done, in case the app was started for it
        NSApp.servicesProvider = self
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Folders opened from Finder, or with open -a, arrive before this
        if windows.isEmpty {
            launchWindow = openWindow(at: NSHomeDirectory())
            launchWindowExpiry = Date().addingTimeInterval(5)
        }
        NSApp.activate(ignoringOtherApps: true)
    }

    /// Folders opened from Finder (Open With, a drop on the Dock icon), with
    /// open -a Sushi, or with the service. Each opens in a window of its own.
    func application(_ application: NSApplication, open urls: [URL]) {
        // Files of the same folder open it once
        var opened = Set<String>()
        for url in urls {
            let directory = Self.directory(for: url)
            guard opened.insert(directory).inserted else { continue }
            if let window = launchWindow, Date() < launchWindowExpiry {
                launchWindow = nil
                window.open(directory)
            } else {
                openWindow(at: directory)
            }
        }
        NSApp.activate(ignoringOtherApps: true)
    }

    /// sushi opens folders, so a file opens the folder it is in. So does a
    /// package, such as an app, which Finder shows as a file.
    private static func directory(for url: URL) -> String {
        let values = try? url.resourceValues(forKeys: [.isDirectoryKey, .isPackageKey])
        if values?.isDirectory == true && values?.isPackage != true {
            return url.path
        }
        return url.deletingLastPathComponent().path
    }

    @discardableResult
    private func openWindow(at directory: String) -> TerminalWindowController? {
        guard let sushi = Bundle.main.path(forResource: "sushi", ofType: nil) else {
            fail("The sushi program is missing from the app. Reinstall Sushi.")
            return nil
        }
        let controller = TerminalWindowController(
            sushi: sushi, number: freeNumber(), font: terminalFont(size: fontSize), after: windows.last?.window)
        controller.onClose = { [weak self] closed in
            // Released once AppKit is done closing its window
            DispatchQueue.main.async {
                self?.windows.removeAll { $0 === closed }
            }
        }
        windows.append(controller)
        controller.open(directory)
        return controller
    }

    /// The lowest number no open window has, so that a new window takes the
    /// place of a closed one
    private func freeNumber() -> Int {
        var number = 1
        while windows.contains(where: { $0.number == number }) {
            number += 1
        }
        return number
    }

    @objc func newWindow(_ sender: Any?) {
        openWindow(at: NSHomeDirectory())
    }

    // MARK: Font

    /// The Nerd Font bundled with the app, so file icons show without the
    /// user installing a font
    private func terminalFont(size: CGFloat) -> NSFont {
        if let url = Bundle.main.url(forResource: "JetBrainsMonoNerdFontMono-Regular", withExtension: "ttf", subdirectory: "Fonts"),
           let descriptors = CTFontManagerCreateFontDescriptorsFromURL(url as CFURL) as? [CTFontDescriptor],
           let descriptor = descriptors.first {
            return CTFontCreateWithFontDescriptor(descriptor, size, nil) as NSFont
        }
        return NSFont.monospacedSystemFont(ofSize: size, weight: .regular)
    }

    private var fontSize: CGFloat {
        let saved = UserDefaults.standard.double(forKey: fontSizeKey)
        return saved >= 8 && saved <= 40 ? CGFloat(saved) : defaultFontSize
    }

    /// Sets the text size of every window, and of those opened later
    private func setFontSize(_ size: CGFloat) {
        let size = min(max(size, 8), 40)
        UserDefaults.standard.set(Double(size), forKey: fontSizeKey)
        let font = terminalFont(size: size)
        windows.forEach { $0.setFont(font) }
    }

    @objc func biggerText(_ sender: Any?) { setFontSize(fontSize + 1) }
    @objc func smallerText(_ sender: Any?) { setFontSize(fontSize - 1) }
    @objc func defaultText(_ sender: Any?) { setFontSize(defaultFontSize) }

    // MARK: Menu

    private func buildMenu() {
        let main = NSMenu()

        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "About Sushi", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Hide Sushi", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        let hideOthers = appMenu.addItem(withTitle: "Hide Others", action: #selector(NSApplication.hideOtherApplications(_:)), keyEquivalent: "h")
        hideOthers.keyEquivalentModifierMask = [.command, .option]
        appMenu.addItem(withTitle: "Show All", action: #selector(NSApplication.unhideAllApplications(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Quit Sushi", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        main.addItem(submenu(appMenu, title: "Sushi"))

        let file = NSMenu(title: "File")
        file.addItem(withTitle: "New Window", action: #selector(newWindow(_:)), keyEquivalent: "n")
        file.addItem(.separator())
        file.addItem(withTitle: "Close Window", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        main.addItem(submenu(file, title: "File"))

        let edit = NSMenu(title: "Edit")
        edit.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        edit.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        edit.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        main.addItem(submenu(edit, title: "Edit"))

        let view = NSMenu(title: "View")
        view.addItem(withTitle: "Bigger Text", action: #selector(biggerText(_:)), keyEquivalent: "+")
        view.addItem(withTitle: "Smaller Text", action: #selector(smallerText(_:)), keyEquivalent: "-")
        view.addItem(withTitle: "Default Text Size", action: #selector(defaultText(_:)), keyEquivalent: "0")
        view.addItem(.separator())
        let fullScreen = view.addItem(withTitle: "Enter Full Screen", action: #selector(NSWindow.toggleFullScreen(_:)), keyEquivalent: "f")
        fullScreen.keyEquivalentModifierMask = [.command, .control]
        main.addItem(submenu(view, title: "View"))

        // AppKit lists the open windows below these
        let windowMenu = NSMenu(title: "Window")
        windowMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "Zoom", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
        windowMenu.addItem(.separator())
        windowMenu.addItem(withTitle: "Bring All to Front", action: #selector(NSApplication.arrangeInFront(_:)), keyEquivalent: "")
        main.addItem(submenu(windowMenu, title: "Window"))
        NSApp.windowsMenu = windowMenu

        NSApp.mainMenu = main
    }

    private func submenu(_ menu: NSMenu, title: String) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        menu.title = title
        item.submenu = menu
        return item
    }

    // MARK: Lifecycle

    /// Quitting sushi in the last window, or closing it, quits the app
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }

    func applicationWillTerminate(_ notification: Notification) {
        // Quitting the app must not leave any sushi running in the background
        windows.forEach { $0.stop() }
    }

    private func fail(_ message: String) {
        let alert = NSAlert()
        alert.alertStyle = .critical
        alert.messageText = "Sushi can't start"
        alert.informativeText = message
        alert.runModal()
        NSApp.terminate(nil)
    }
}

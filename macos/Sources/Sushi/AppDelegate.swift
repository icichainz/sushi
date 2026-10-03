import AppKit
import CoreText

final class AppDelegate: NSObject, NSApplicationDelegate {
    /// The open windows, in the order they were opened
    private(set) var windows: [TerminalWindowController] = []
    /// The sushis still exiting, from closed windows too, for quitting to
    /// wait for
    private let exits = PendingExits()
    /// Quitting waits for them, and opens no more windows meanwhile
    private var quitting = false

    /// The home window opened when the app was started for something other
    /// than a click on its icon, such as Finder's service. The folder comes
    /// after the launch then, and takes the window over in the next seconds
    /// rather than open a second one, unless the user has typed or clicked
    /// in it.
    private weak var launchWindow: TerminalWindowController?
    private var launchWindowWatch: Any?

    private let defaultFontSize: CGFloat = 13
    private let fontSizeKey = "fontSize"

    func applicationWillFinishLaunching(_ notification: Notification) {
        buildMenu()
        // Before the launch is done, as the app may have been started to run
        // the service or for a click on a notification
        NSApp.servicesProvider = self
        setUpNotifications()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Folders opened from Finder, or with open -a, arrive before this
        if windows.isEmpty, let window = openWindow(NSHomeDirectory(), in: NSHomeDirectory()),
           notification.userInfo?[NSApplication.launchIsDefaultUserInfoKey] as? Bool == false {
            keepReplaceable(window)
        }
        NSApp.activate(ignoringOtherApps: true)
    }

    /// Lets a folder arriving in the next 5 seconds take the launch window
    /// over, until the user types or clicks in it
    private func keepReplaceable(_ window: TerminalWindowController) {
        launchWindow = window
        launchWindowWatch = NSEvent.addLocalMonitorForEvents(matching: [.keyDown, .leftMouseDown, .rightMouseDown]) { [weak self] event in
            if let self, let launch = self.launchWindow, event.window === launch.window {
                self.forgetLaunchWindow()
            }
            return event
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 5) { [weak self] in
            self?.forgetLaunchWindow()
        }
    }

    private func forgetLaunchWindow() {
        launchWindow = nil
        if let watch = launchWindowWatch {
            NSEvent.removeMonitor(watch)
            launchWindowWatch = nil
        }
    }

    /// Folders and files opened from Finder (Open With, a drop on the Dock
    /// icon), with open -a Sushi, or with the service. Each folder opens in
    /// a window of its own, and so do the files of each folder, together.
    func application(_ application: NSApplication, open urls: [URL]) {
        for place in Self.places(for: urls) {
            if let window = launchWindow {
                forgetLaunchWindow()
                window.open(place.path, in: place.directory)
            } else {
                openWindow(place.path, in: place.directory)
            }
        }
        NSApp.activate(ignoringOtherApps: true)
    }

    /// Where sushi starts: on path, in directory, the folder it shows
    struct Place {
        let path: String
        let directory: String
    }

    /// Where to start sushi for what was opened, a window for each folder
    /// shown. A folder opens as it is. Files open the folder they are in,
    /// once for all of them, with the cursor on the first (sushi, given a
    /// file, puts the cursor on it). A package, such as an app, which
    /// Finder shows as a file but sushi as a folder, opens the folder it is
    /// in, at the top.
    static func places(for urls: [URL]) -> [Place] {
        var places: [Place] = []
        for url in urls {
            let place: Place
            switch kind(of: url) {
            case .folder:
                place = Place(path: url.path, directory: url.path)
            case .file:
                place = Place(path: url.path, directory: url.deletingLastPathComponent().path)
            case .package:
                let parent = url.deletingLastPathComponent().path
                place = Place(path: parent, directory: parent)
            }
            if let i = places.firstIndex(where: { $0.directory == place.directory }) {
                // The folder is already opening: a file puts the cursor on
                // itself, if no other file has
                if places[i].path == places[i].directory {
                    places[i] = place
                }
            } else {
                places.append(place)
            }
        }
        return places
    }

    private enum Kind { case folder, file, package }

    private static func kind(of url: URL) -> Kind {
        // Resolved, so a link such as /tmp is seen as the folder it points
        // to; the link's own path is still what sushi gets
        let values = try? url.resolvingSymlinksInPath().resourceValues(forKeys: [.isDirectoryKey, .isPackageKey])
        if values?.isPackage == true {
            return .package
        }
        // A folder the app may not read yet still has a URL ending in /
        return (values?.isDirectory ?? url.hasDirectoryPath) ? .folder : .file
    }

    @discardableResult
    private func openWindow(_ path: String, in directory: String) -> TerminalWindowController? {
        guard !quitting else { return nil }
        guard let sushi = Bundle.main.path(forResource: "sushi", ofType: nil) else {
            fail("The sushi program is missing from the app. Reinstall Sushi.")
            return nil
        }
        let controller = TerminalWindowController(
            sushi: sushi, number: freeNumber(), font: terminalFont(size: fontSize), after: windows.last?.window, exits: exits)
        controller.onClose = { [weak self] closed in
            // Released once AppKit is done closing its window
            DispatchQueue.main.async {
                self?.windows.removeAll { $0 === closed }
            }
        }
        controller.onNotice = { [weak self] window, notice in
            self?.post(notice, from: window)
        }
        windows.append(controller)
        controller.open(path, in: directory)
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
        openWindow(NSHomeDirectory(), in: NSHomeDirectory())
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
        // Cmd+= too, without Shift, as + is Shift+= on most keyboards
        let bigger = view.addItem(withTitle: "Bigger Text", action: #selector(biggerText(_:)), keyEquivalent: "=")
        bigger.isHidden = true
        bigger.allowsKeyEquivalentWhenHidden = true
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

    /// Quitting stops sushi in every window, and waits for each to exit,
    /// those of windows closed just before too: one in the middle of a copy
    /// cancels it and cleans up, for up to 10 s, after which it gives up.
    /// Otherwise a sushi still cleaning up would carry on unseen once the
    /// app had gone. Asked again meanwhile, the app quits at once.
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if quitting {
            return .terminateNow
        }
        windows.forEach { $0.stop() }
        guard exits.pending > 0 else { return .terminateNow }
        quitting = true
        var replied = false
        let reply = {
            guard !replied else { return }
            replied = true
            NSApp.reply(toApplicationShouldTerminate: true)
        }
        exits.wait(reply)
        DispatchQueue.main.asyncAfter(deadline: .now() + 11, execute: reply)
        return .terminateLater
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

/// Counts the sushis told to stop that haven't exited yet, whether their
/// window is still open or not
final class PendingExits {
    private(set) var pending = 0
    private var waiting: [() -> Void] = []

    /// Counts one more, and returns what to call once it has exited
    func begin() -> () -> Void {
        pending += 1
        var ended = false
        return { [weak self] in
            guard let self, !ended else { return }
            ended = true
            self.pending -= 1
            if self.pending == 0 {
                let done = self.waiting
                self.waiting = []
                done.forEach { $0() }
            }
        }
    }

    /// Calls done once none is left, at once if none is
    func wait(_ done: @escaping () -> Void) {
        if pending == 0 {
            done()
        } else {
            waiting.append(done)
        }
    }
}

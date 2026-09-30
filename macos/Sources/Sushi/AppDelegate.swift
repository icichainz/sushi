import AppKit
import CoreText
import SwiftTerm

final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, LocalProcessTerminalViewDelegate {
    private var window: NSWindow!
    private var terminal: LocalProcessTerminalView!

    private let defaultFontSize: CGFloat = 13
    private let fontSizeKey = "fontSize"

    // The nori background and rice text of sushi's default theme
    private let background = NSColor(srgbRed: 0x14 / 255, green: 0x15 / 255, blue: 0x1a / 255, alpha: 1)
    private let foreground = NSColor(srgbRed: 0xeb / 255, green: 0xe7 / 255, blue: 0xdc / 255, alpha: 1)
    private let caret = NSColor(srgbRed: 0xff / 255, green: 0x94 / 255, blue: 0x78 / 255, alpha: 1)

    func applicationDidFinishLaunching(_ notification: Notification) {
        buildMenu()

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1120, height: 700),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false)
        window.title = "Sushi"
        window.backgroundColor = background
        window.minSize = NSSize(width: 480, height: 300)
        window.delegate = self
        window.center()
        // Reopen where the window was left
        window.setFrameAutosaveName("SushiMainWindow")

        terminal = LocalProcessTerminalView(frame: window.contentView!.bounds)
        terminal.autoresizingMask = [.width, .height]
        terminal.processDelegate = self
        terminal.nativeBackgroundColor = background
        terminal.nativeForegroundColor = foreground
        terminal.caretColor = caret
        terminal.font = terminalFont(size: savedFontSize())
        window.contentView!.addSubview(terminal)

        guard let sushi = Bundle.main.path(forResource: "sushi", ofType: nil) else {
            fail("The sushi program is missing from the app. Reinstall Sushi.")
            return
        }
        startSushi(at: sushi)

        window.makeKeyAndOrderFront(nil)
        window.makeFirstResponder(terminal)
        NSApp.activate(ignoringOtherApps: true)
    }

    /// Runs sushi through the user's login shell, so it sees the same PATH,
    /// EDITOR and other settings as in a terminal. Apps opened from Finder
    /// otherwise get a bare environment, and plugins and editors go missing.
    private func startSushi(at sushi: String) {
        let home = NSHomeDirectory()
        var environment = ProcessInfo.processInfo.environment
        environment["TERM"] = "xterm-256color"
        environment["COLORTERM"] = "truecolor"
        environment["TERM_PROGRAM"] = "Sushi"
        if environment["LANG"] == nil {
            environment["LANG"] = "en_US.UTF-8"
        }

        let shell = environment["SHELL"] ?? "/bin/zsh"
        // exec replaces the shell, so quitting sushi ends the process. The
        // paths are quoted into the command, since fish has no "$0"/"$1".
        let command = "exec \(quoted(sushi)) \(quoted(home))"
        terminal.startProcess(
            executable: shell,
            args: ["-l", "-i", "-c", command],
            environment: environment.map { "\($0.key)=\($0.value)" },
            execName: nil,
            currentDirectory: home)
    }

    /// Single-quotes a string for zsh, bash and fish
    private func quoted(_ s: String) -> String {
        "'" + s.replacingOccurrences(of: "'", with: "'\\''") + "'"
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

    private func savedFontSize() -> CGFloat {
        let saved = UserDefaults.standard.double(forKey: fontSizeKey)
        return saved >= 8 && saved <= 40 ? CGFloat(saved) : defaultFontSize
    }

    private func setFontSize(_ size: CGFloat) {
        let size = min(max(size, 8), 40)
        UserDefaults.standard.set(Double(size), forKey: fontSizeKey)
        terminal.font = terminalFont(size: size)
    }

    @objc func biggerText(_ sender: Any?) { setFontSize(terminal.font.pointSize + 1) }
    @objc func smallerText(_ sender: Any?) { setFontSize(terminal.font.pointSize - 1) }
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

        let windowMenu = NSMenu(title: "Window")
        windowMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "Zoom", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
        windowMenu.addItem(withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
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

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }

    func applicationWillTerminate(_ notification: Notification) {
        // Closing the window must not leave sushi running in the background
        terminal?.terminate()
    }

    private func fail(_ message: String) {
        let alert = NSAlert()
        alert.alertStyle = .critical
        alert.messageText = "Sushi can't start"
        alert.informativeText = message
        alert.runModal()
        NSApp.terminate(nil)
    }

    // MARK: LocalProcessTerminalViewDelegate

    func sizeChanged(source: LocalProcessTerminalView, newCols: Int, newRows: Int) {}

    func setTerminalTitle(source: LocalProcessTerminalView, title: String) {
        window.title = title.isEmpty ? "Sushi" : title
    }

    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}

    /// Quitting sushi (q) quits the app
    func processTerminated(source: TerminalView, exitCode: Int32?) {
        NSApp.terminate(nil)
    }
}

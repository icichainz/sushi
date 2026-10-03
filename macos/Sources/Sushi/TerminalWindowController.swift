import AppKit
import SwiftTerm

/// One window and the sushi running in it
final class TerminalWindowController: NSObject, NSWindowDelegate, LocalProcessTerminalViewDelegate {
    /// Names the window in its notifications, to bring it to the front
    let id = UUID().uuidString
    /// Numbered from 1, for the frame saved for the window
    let number: Int
    let window: NSWindow
    var onClose: ((TerminalWindowController) -> Void)?
    /// sushi has news for a user who isn't looking at the window
    var onNotice: ((TerminalWindowController, SushiNotice) -> Void)?

    private let sushi: String
    private var font: NSFont
    private var terminal: LocalProcessTerminalView?
    private var relay: TerminalRelay?
    /// Whether sushi is running. LocalProcess still says it is while it
    /// reports the exit, when its process id may already be reused.
    private var running = false

    // The nori background and rice text of sushi's default theme
    private static let background = NSColor(srgbRed: 0x14 / 255, green: 0x15 / 255, blue: 0x1a / 255, alpha: 1)
    private static let foreground = NSColor(srgbRed: 0xeb / 255, green: 0xe7 / 255, blue: 0xdc / 255, alpha: 1)
    private static let caret = NSColor(srgbRed: 0xff / 255, green: 0x94 / 255, blue: 0x78 / 255, alpha: 1)

    init(sushi: String, number: Int, font: NSFont, after previous: NSWindow?) {
        self.sushi = sushi
        self.number = number
        self.font = font
        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1120, height: 700),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false)
        super.init()
        window.title = "Sushi"
        window.backgroundColor = Self.background
        window.minSize = NSSize(width: 480, height: 300)
        // The controller keeps the window; AppKit must not release it too
        window.isReleasedWhenClosed = false
        window.delegate = self
        place(after: previous)
    }

    /// Reopens the window where the window with its number was left. One
    /// opened for the first time goes below and to the right of the
    /// previous window, or in the middle of the screen.
    private func place(after previous: NSWindow?) {
        // The first window keeps the frame saved when there was only one
        let name = number == 1 ? "SushiMainWindow" : "SushiWindow\(number)"
        if !window.setFrameUsingName(name) {
            if let previous {
                // The first call puts the window over the previous one and
                // returns the point one step down, where the second puts it
                let next = window.cascadeTopLeft(from: NSPoint(x: previous.frame.minX, y: previous.frame.maxY))
                window.cascadeTopLeft(from: next)
            } else {
                window.center()
            }
        }
        window.setFrameAutosaveName(name)
    }

    /// Shows the folder in the window, in a new sushi if one was running
    func open(_ directory: String) {
        stop()
        if let old = terminal {
            old.processDelegate = nil
            old.removeFromSuperview()
        }

        let terminal = LocalProcessTerminalView(frame: window.contentView!.bounds)
        terminal.autoresizingMask = [.width, .height]
        terminal.processDelegate = self
        terminal.nativeBackgroundColor = Self.background
        terminal.nativeForegroundColor = Self.foreground
        terminal.caretColor = Self.caret
        terminal.font = font
        relay = TerminalRelay(
            view: terminal,
            bell: { [weak self] in self?.bell() },
            iTermContent: { [weak self] in self?.received($0) })
        window.contentView!.addSubview(terminal)
        self.terminal = terminal

        bringToFront()
        window.makeFirstResponder(terminal)
        start(terminal, in: directory)
    }

    /// Runs sushi through the user's login shell, so it sees the same PATH,
    /// EDITOR and other settings as in a terminal. Apps opened from Finder
    /// otherwise get a bare environment, and plugins and editors go missing.
    private func start(_ terminal: LocalProcessTerminalView, in directory: String) {
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
        let command = "exec \(quoted(sushi)) \(quoted(directory))"
        // Set first, as a sushi that can't start reports its exit at once
        running = true
        terminal.startProcess(
            executable: shell,
            args: ["-l", "-i", "-c", command],
            environment: environment.map { "\($0.key)=\($0.value)" },
            execName: nil,
            currentDirectory: directory)
    }

    /// Single-quotes a string for zsh, bash and fish
    private func quoted(_ s: String) -> String {
        "'" + s.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    /// Ends sushi, when its window closes or the app quits
    func stop() {
        guard running else { return }
        running = false
        terminal?.terminate()
    }

    func bringToFront() {
        if window.isMiniaturized {
            window.deminiaturize(nil)
        }
        window.makeKeyAndOrderFront(nil)
    }

    func setFont(_ font: NSFont) {
        self.font = font
        terminal?.font = font
    }

    // MARK: Bell and notices

    /// In the background, the bell bounces the Dock icon instead of beeping
    private func bell() {
        if NSApp.isActive {
            NSSound.beep()
        } else {
            NSApp.requestUserAttention(.informationalRequest)
        }
    }

    /// OSC 1337 payloads; sushi sends a notice when a long job finishes
    private func received(_ content: ArraySlice<UInt8>) {
        guard let notice = SushiNotice(content) else { return }
        // Not while the user is looking at the window
        if NSApp.isActive && window.isKeyWindow { return }
        onNotice?(self, notice)
    }

    // MARK: NSWindowDelegate

    func windowWillClose(_ notification: Notification) {
        // Closing the window must not leave sushi running in the background
        stop()
        onClose?(self)
    }

    // MARK: LocalProcessTerminalViewDelegate

    func sizeChanged(source: LocalProcessTerminalView, newCols: Int, newRows: Int) {}

    func setTerminalTitle(source: LocalProcessTerminalView, title: String) {
        window.title = title.isEmpty ? "Sushi" : title
    }

    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}

    /// Quitting sushi (q) closes its window
    func processTerminated(source: TerminalView, exitCode: Int32?) {
        running = false
        window.close()
    }
}

/// LocalProcessTerminalView is its own TerminalViewDelegate, and leaves the
/// bell and OSC 1337 to SwiftTerm's defaults, which a subclass can't
/// override. The relay takes its place, as SwiftTerm's documentation
/// suggests: it handles those two and hands everything else back.
private final class TerminalRelay: TerminalViewDelegate {
    private unowned let view: LocalProcessTerminalView
    private let onBell: () -> Void
    private let onITermContent: (ArraySlice<UInt8>) -> Void

    init(view: LocalProcessTerminalView, bell: @escaping () -> Void, iTermContent: @escaping (ArraySlice<UInt8>) -> Void) {
        self.view = view
        onBell = bell
        onITermContent = iTermContent
        // A weak reference: the window controller keeps the relay
        view.terminalDelegate = self
    }

    func bell(source: TerminalView) { onBell() }
    func iTermContent(source: TerminalView, content: ArraySlice<UInt8>) { onITermContent(content) }

    func send(source: TerminalView, data: ArraySlice<UInt8>) { view.send(source: source, data: data) }
    func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) { view.sizeChanged(source: source, newCols: newCols, newRows: newRows) }
    func setTerminalTitle(source: TerminalView, title: String) { view.setTerminalTitle(source: source, title: title) }
    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) { view.hostCurrentDirectoryUpdate(source: source, directory: directory) }
    func scrolled(source: TerminalView, position: Double) { view.scrolled(source: source, position: position) }
    func rangeChanged(source: TerminalView, startY: Int, endY: Int) { view.rangeChanged(source: source, startY: startY, endY: endY) }
    func requestOpenLink(source: TerminalView, link: String, params: [String: String]) { view.requestOpenLink(source: source, link: link, params: params) }
    func clipboardCopy(source: TerminalView, content: Data) { view.clipboardCopy(source: source, content: content) }
    func clipboardRead(source: TerminalView) -> Data? { view.clipboardRead(source: source) }
}

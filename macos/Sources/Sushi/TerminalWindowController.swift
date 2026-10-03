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
    private let exits: PendingExits
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

    init(sushi: String, number: Int, font: NSFont, after previous: NSWindow?, exits: PendingExits) {
        self.sushi = sushi
        self.exits = exits
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

    /// Starts sushi on path, a folder or a file to put the cursor on, in
    /// directory, the folder it shows; a sushi running in the window is
    /// stopped first
    func open(_ path: String, in directory: String) {
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
        start(terminal, on: path, in: directory)
    }

    /// The login shells sushi is started through. Others, such as tcsh,
    /// take other options, so their users get the system's zsh.
    private static let shells: Set<String> = ["zsh", "bash", "fish", "sh", "ksh", "dash"]

    /// Runs sushi through the user's login shell, so it sees the same PATH,
    /// EDITOR and other settings as in a terminal. Apps opened from Finder
    /// otherwise get a bare environment, and plugins and editors go missing.
    private func start(_ terminal: LocalProcessTerminalView, on path: String, in directory: String) {
        var environment = ProcessInfo.processInfo.environment
        environment["TERM"] = "xterm-256color"
        environment["COLORTERM"] = "truecolor"
        environment["TERM_PROGRAM"] = "Sushi"
        // Left by a terminal the app was started from, they would have
        // sushi take the window for that terminal, or for tmux
        for name in ["TERM_PROGRAM_VERSION", "TMUX", "TMUX_PANE", "STY"] {
            environment[name] = nil
        }
        if environment["LANG"] == nil {
            environment["LANG"] = "en_US.UTF-8"
        }
        // The paths go through the environment, which every shell expands
        // the same way, rather than be quoted into the command for each
        environment["SUSHI_BIN"] = sushi
        environment["SUSHI_START"] = path

        var shell = environment["SHELL"] ?? ""
        if !Self.shells.contains((shell as NSString).lastPathComponent) || !FileManager.default.isExecutableFile(atPath: shell) {
            shell = "/bin/zsh"
        }
        // Set first, as a sushi that can't start reports its exit at once
        running = true
        // exec replaces the shell, so quitting sushi ends the process
        terminal.startProcess(
            executable: shell,
            args: ["-l", "-i", "-c", "exec \"$SUSHI_BIN\" \"$SUSHI_START\""],
            environment: environment.map { "\($0.key)=\($0.value)" },
            execName: nil,
            currentDirectory: directory)
    }

    /// Ends sushi, when its window closes or the app quits. The app waits
    /// for it to exit, through exits, before it quits.
    func stop() {
        guard running, let terminal else { return }
        running = false
        let process: LocalProcess = terminal.process
        let pid = process.shellPid
        // The terminal's foreground job, which a hangup reaches too, as
        // something the shell runs from its startup files
        let foreground = process.childfd >= 0 ? tcgetpgrp(process.childfd) : -1
        // Stops reading the terminal, and sends sushi SIGTERM
        terminal.terminate()
        Self.hangUp(pid, foreground: foreground, done: exits.begin())
    }

    /// Hangs up on sushi as a terminal does when its window closes, which
    /// SwiftTerm doesn't: terminate() leaves the terminal open until sushi
    /// next writes to it. Its SIGTERM isn't enough either: an interactive
    /// shell ignores it while it reads its startup files, and would then
    /// start a sushi nobody can see, and an editor or plugin sushi runs
    /// has the terminal until it exits. SIGHUP to the window's process
    /// group reaches the shell, sushi, which stops a running job and
    /// cleans up after it (for up to 10 s), and what sushi runs. Anything
    /// still there after 15 s is killed. done runs once sushi has exited.
    private static func hangUp(_ pid: pid_t, foreground: pid_t, done: @escaping () -> Void) {
        // Never 0 or 1: kill(0) is the app's own group, and kill(-1) every
        // process the user has
        guard pid > 1 else {
            done()
            return
        }
        if kill(-pid, SIGHUP) != 0 {
            kill(pid, SIGHUP)
        }
        if foreground > 1 && foreground != pid {
            kill(-foreground, SIGHUP)
        }

        // SwiftTerm stops watching a process it terminates, which would
        // stay a zombie while the app runs
        var reaped = false
        let source = DispatchSource.makeProcessSource(identifier: pid, eventMask: .exit, queue: .main)
        // The handler keeps the source until the process is gone
        source.setEventHandler {
            var status: Int32 = 0
            waitpid(pid, &status, WNOHANG)
            reaped = true
            source.cancel()
            done()
        }
        source.activate()
        DispatchQueue.main.asyncAfter(deadline: .now() + 15) {
            // Until it is reaped, pid can name no other process or group
            if !reaped && kill(-pid, SIGKILL) != 0 {
                kill(pid, SIGKILL)
            }
        }
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

    /// sushi reports the folder it shows (OSC 7); the window takes its
    /// name and shows the folder's icon in the title bar
    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {
        guard let directory, let url = URL(string: directory), url.isFileURL else { return }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        var name = url.lastPathComponent
        if url.path == home {
            name = "~"
        } else if url.path == "/" {
            name = "/"
        }
        window.title = name
        window.representedURL = url
    }

    /// Quitting sushi (q) closes its window. If it failed, or couldn't
    /// start, the window stays open on what it printed, as a panic or a
    /// shell's error, until the user closes it.
    func processTerminated(source: TerminalView, exitCode: Int32?) {
        running = false
        // SwiftTerm passes the wait status, not the exit code
        guard let status = exitCode, let failure = Self.failure(status) else {
            window.close()
            return
        }
        // After what sushi printed last, which may still be on its way
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) { [weak source] in
            // Shows the cursor, and leaves the mouse to select text with
            let reset = "\u{1b}[0m\u{1b}[?25h\u{1b}[?1000l\u{1b}[?1002l\u{1b}[?1003l\u{1b}[?1006l"
            source?.feed(text: reset + "\r\n[sushi exited: \(failure)]\r\n")
        }
    }

    /// What a wait status says went wrong: the exit code if it isn't 0, or
    /// the signal that ended the process; nil if it exited with 0
    static func failure(_ status: Int32) -> String? {
        let signal = status & 0x7f
        if signal == 0 {
            let code = (status >> 8) & 0xff
            return code == 0 ? nil : "\(code)"
        }
        return "signal \(signal)"
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

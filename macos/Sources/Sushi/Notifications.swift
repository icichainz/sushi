import AppKit
import UserNotifications

/// What sushi has to say when a long job (a copy, a move, a delete...)
/// finishes. With TERM_PROGRAM=Sushi, it writes
///
///     ESC ] 1337 ; SushiNotify=<title>|<body> BEL
///
/// with the title and the body percent-encoded UTF-8, so that only ASCII
/// is sent: SwiftTerm would take a UTF-8 byte in 0x80-0x9F that starts a
/// read for a C1 control, and lose the notice.
struct SushiNotice {
    let title: String
    let body: String

    private static let key = "SushiNotify="

    /// Reads an OSC 1337 payload, the part after "1337;"
    init?(_ content: ArraySlice<UInt8>) {
        let text = String(decoding: content, as: UTF8.self)
        guard text.hasPrefix(Self.key) else { return nil }
        let parts = text.dropFirst(Self.key.count).split(separator: "|", maxSplits: 1, omittingEmptySubsequences: false)
        title = Self.decoded(parts[0])
        body = parts.count > 1 ? Self.decoded(parts[1]) : ""
    }

    /// A part as sushi had it; one that isn't valid percent-encoding, as
    /// from an older sushi, as it is
    private static func decoded(_ part: Substring) -> String {
        String(part).removingPercentEncoding ?? String(part)
    }
}

extension AppDelegate: UNUserNotificationCenterDelegate {
    private static let windowKey = "window"

    /// None for a bare build product, as UNUserNotificationCenter raises
    /// an exception in a program with no bundle identifier
    private var notificationCenter: UNUserNotificationCenter? {
        Bundle.main.bundleIdentifier == nil ? nil : .current()
    }

    func setUpNotifications() {
        notificationCenter?.delegate = self
    }

    /// Shows a notice from a window. Permission is asked for the first
    /// time; macOS asks the user once and then gives the same answer.
    func post(_ notice: SushiNotice, from window: TerminalWindowController) {
        guard let center = notificationCenter else { return }
        let content = UNMutableNotificationContent()
        content.title = notice.title.isEmpty ? "Sushi" : notice.title
        content.body = notice.body
        content.sound = .default
        content.userInfo = [Self.windowKey: window.id]
        let request = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)

        center.requestAuthorization(options: [.alert, .sound]) { granted, _ in
            if granted {
                center.add(request)
            } else {
                // Notifications turned off: bounce the Dock icon instead
                DispatchQueue.main.async {
                    NSApp.requestUserAttention(.informationalRequest)
                }
            }
        }
    }

    /// macOS hides the notifications of the active app unless told to show
    /// them. Sushi posts only for a window the user isn't looking at.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .list, .sound])
    }

    /// A click on a notification brings its window to the front
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let id = response.notification.request.content.userInfo[Self.windowKey] as? String
        DispatchQueue.main.async {
            NSApp.activate(ignoringOtherApps: true)
            self.windows.first { $0.id == id }?.bringToFront()
            completionHandler()
        }
    }
}

import AppKit

extension AppDelegate {
    /// "Open in Sushi", in the Services menu of folders in Finder, as
    /// NSServices in Info.plist declares it. macOS may list it only once it
    /// is turned on in System Settings > Keyboard > Keyboard Shortcuts >
    /// Services.
    @objc(openFolders:userData:error:)
    func openFolders(_ pasteboard: NSPasteboard, userData: String?, error: AutoreleasingUnsafeMutablePointer<NSString?>) {
        let urls = pasteboard.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        guard !urls.isEmpty else {
            error.pointee = "Sushi was given no folder to open."
            return
        }
        application(NSApp, open: urls)
    }
}

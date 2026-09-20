import Foundation

/// 通过用户级 LaunchAgent 实现「登录时自动启动」。
///
/// 相比 SMAppService，LaunchAgent 对 ad-hoc 签名的本地构建应用更可靠，
/// 且完全落在用户目录内，卸载时只需删除一个 plist。
enum LaunchAtLogin {

    static let label = "cn.workbuddy.status"

    static var plistURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/LaunchAgents", isDirectory: true)
            .appendingPathComponent("\(label).plist")
    }

    /// 已安装的 App 可执行文件路径（优先使用 /Applications 下的正式副本）
    private static var executablePath: String {
        let installed = "/Applications/WorkBuddyStatus.app/Contents/MacOS/WorkBuddyStatus"
        if FileManager.default.isExecutableFile(atPath: installed) { return installed }
        return Bundle.main.executablePath ?? CommandLine.arguments[0]
    }

    static var isEnabled: Bool {
        FileManager.default.fileExists(atPath: plistURL.path)
    }

    @discardableResult
    static func enable() -> String? {
        let plist = """
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0">
        <dict>
            <key>Label</key>
            <string>\(label)</string>
            <key>ProgramArguments</key>
            <array>
                <string>\(executablePath)</string>
            </array>
            <key>RunAtLoad</key>
            <true/>
            <key>ProcessType</key>
            <string>Interactive</string>
            <key>LimitLoadToSessionType</key>
            <string>Aqua</string>
        </dict>
        </plist>
        """

        let dir = plistURL.deletingLastPathComponent()
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)

        do {
            try plist.write(to: plistURL, atomically: true, encoding: .utf8)
        } catch {
            return "写入启动项失败：\(error.localizedDescription)"
        }

        // 先尝试卸载旧定义，再加载新定义
        _ = shell("/bin/launchctl", ["bootout", "gui/\(getuid())", plistURL.path])
        let result = shell("/bin/launchctl", ["bootstrap", "gui/\(getuid())", plistURL.path])
        if result.status != 0 {
            // 兼容旧版写法
            let legacy = shell("/bin/launchctl", ["load", "-w", plistURL.path])
            if legacy.status != 0 {
                return "注册启动项失败：\(result.output.isEmpty ? legacy.output : result.output)"
            }
        }
        return nil
    }

    @discardableResult
    static func disable() -> String? {
        _ = shell("/bin/launchctl", ["bootout", "gui/\(getuid())", plistURL.path])
        _ = shell("/bin/launchctl", ["unload", "-w", plistURL.path])
        do {
            if FileManager.default.fileExists(atPath: plistURL.path) {
                try FileManager.default.removeItem(at: plistURL)
            }
        } catch {
            return "移除启动项失败：\(error.localizedDescription)"
        }
        return nil
    }

    @discardableResult
    static func toggle() -> String? {
        isEnabled ? disable() : enable()
    }

    // MARK: - 辅助

    private static func shell(_ launchPath: String, _ args: [String]) -> (status: Int32, output: String) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: launchPath)
        process.arguments = args
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe
        do {
            try process.run()
        } catch {
            return (-1, error.localizedDescription)
        }
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        return (process.terminationStatus, String(data: data, encoding: .utf8) ?? "")
    }
}

import AppKit
import Foundation

// MARK: - 偏好设置

enum DisplayMode: Int {
    case value = 0      // ⚡ 1513
    case percent = 1    // ⚡ 9%
    case iconOnly = 2   // ⚡
}

enum Prefs {
    private static let displayModeKey = "displayMode"
    private static let refreshIntervalKey = "refreshInterval"

    static var displayMode: DisplayMode {
        get { DisplayMode(rawValue: UserDefaults.standard.integer(forKey: displayModeKey)) ?? .value }
        set { UserDefaults.standard.set(newValue.rawValue, forKey: displayModeKey) }
    }

    /// 自动刷新间隔（秒），0 表示仅手动刷新
    static var refreshInterval: TimeInterval {
        get {
            if UserDefaults.standard.object(forKey: refreshIntervalKey) == nil { return 300 }
            return UserDefaults.standard.double(forKey: refreshIntervalKey)
        }
        set { UserDefaults.standard.set(newValue, forKey: refreshIntervalKey) }
    }
}

// MARK: - AppDelegate

final class AppDelegate: NSObject, NSApplicationDelegate, NSMenuDelegate {

    private var statusItem: NSStatusItem!
    private let menu = NSMenu()
    private var refreshTimer: Timer?

    private let client = CreditsClient()
    private var credential: WorkBuddyCredential?
    private var summary: CreditSummary?
    private var checkin: CheckinStatus?
    private var lastError: String?
    private var lastUpdated: Date?
    private var isLoading = false

    // MARK: 生命周期

    func applicationDidFinishLaunching(_ notification: Notification) {
        AppConfig.writeTemplateIfNeeded()

        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        if let button = statusItem.button {
            button.title = "⚡ …"
            button.font = NSFont.monospacedDigitSystemFont(ofSize: 12, weight: .medium)
            button.toolTip = "WorkBuddy 积分余额"
        }

        menu.delegate = self
        menu.autoenablesItems = false
        statusItem.menu = menu

        configureRefreshTimer()
        reload()

        // 自检用：以 --auto-open-menu 启动时，启动后自动展开菜单（便于截图 / 排查）
        if CommandLine.arguments.contains("--auto-open-menu") {
            DispatchQueue.main.asyncAfter(deadline: .now() + 4.0) { [weak self] in
                guard let self = self else { return }
                AppConfig.log("自检模式: 自动展开菜单")
                NSApp.activate(ignoringOtherApps: true)
                self.rebuildMenu()
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) {
                    self.statusItem.button?.performClick(nil)
                }
            }
        }
    }

    // MARK: 数据刷新

    @objc func reload() {
        if isLoading { return }
        isLoading = true
        renderStatusTitle()

        let config = AppConfig.load()
        client.endpoint = config.endpoint

        let cred: WorkBuddyCredential
        do {
            cred = try CredentialLoader.load(config: config)
        } catch {
            isLoading = false
            credential = nil
            summary = nil
            checkin = nil
            lastError = (error as? LocalizedError)?.errorDescription ?? "\(error)"
            AppConfig.log("凭据加载失败: \(lastError ?? "")")
            renderStatusTitle()
            return
        }

        credential = cred

        client.fetchSummary(credential: cred) { [weak self] result in
            DispatchQueue.main.async {
                guard let self = self else { return }
                self.isLoading = false
                self.lastUpdated = Date()
                switch result {
                case .success(let value):
                    self.summary = value
                    self.lastError = nil
                    AppConfig.log("刷新成功: 剩余 \(AppDelegate.number(value.totalRemain)) / \(AppDelegate.number(value.totalCapacity))")
                case .failure(let error):
                    self.lastError = (error as? LocalizedError)?.errorDescription ?? "\(error)"
                    AppConfig.log("积分获取失败: \(self.lastError ?? "")")
                    if case CreditsError.unauthorized = error { self.summary = nil }
                }
                self.renderStatusTitle()
            }
        }

        client.fetchCheckin(credential: cred) { [weak self] result in
            guard let self = self else { return }
            DispatchQueue.main.async {
                if case .success(let value) = result { self.checkin = value }
            }
        }
    }

    // MARK: 定时器

    private func configureRefreshTimer() {
        refreshTimer?.invalidate()
        refreshTimer = nil
        let interval = Prefs.refreshInterval
        guard interval > 0 else { return }
        let timer = Timer(timeInterval: interval, repeats: true) { [weak self] _ in
            self?.reload()
        }
        RunLoop.main.add(timer, forMode: .common)
        refreshTimer = timer
    }

    // MARK: 菜单栏标题

    private func renderStatusTitle() {
        guard let button = statusItem.button else { return }
        let mode = Prefs.displayMode

        if let value = summary {
            switch mode {
            case .iconOnly:
                button.title = "⚡"
            case .percent:
                button.title = "⚡ " + String(format: "%.0f%%", value.usageRatio * 100)
            case .value:
                button.title = "⚡ " + Self.compactNumber(value.totalRemain)
            }
        } else if isLoading {
            button.title = "⚡ …"
        } else if lastError != nil {
            button.title = "⚡ ⚠︎"
        } else {
            button.title = "⚡ --"
        }
    }

    // MARK: 菜单构建

    func menuNeedsUpdate(_ menu: NSMenu) {
        rebuildMenu()
    }

    private func rebuildMenu() {
        menu.removeAllItems()

        // —— 账户 ——
        let name = credential?.displayName ?? "未找到 WorkBuddy 登录信息"
        menu.addItem(info("👤 " + name, enabled: true))

        // —— 余额 ——
        if let value = summary {
            let item = NSMenuItem()
            item.attributedTitle = Self.balanceTitle(remain: value.totalRemain, capacity: value.totalCapacity)
            item.action = #selector(noop)
            item.target = self
            menu.addItem(item)
        } else if let error = lastError {
            menu.addItem(info("⚠️ " + error, enabled: true))
        } else {
            menu.addItem(info("⏳ 正在获取积分…", enabled: true))
        }

        if let value = summary, let updated = lastUpdated {
            let formatter = DateFormatter()
            formatter.dateFormat = "HH:mm:ss"
            let state = value.isPaidUser ? "付费用户" : "免费用户"
            menu.addItem(info("🕘 \(state) · 更新于 \(formatter.string(from: updated))", enabled: false))
        }

        // —— 明细 ——
        if let value = summary, !value.packages.isEmpty {
            menu.addItem(.separator())
            let active = value.activePackages
            let unused = value.packages.count - active.count
            let title = "📦 积分明细（可用 \(active.count) 个\(unused > 0 ? "，已用尽 \(unused) 个" : "")）"
            let parent = NSMenuItem(title: title, action: nil, keyEquivalent: "")
            let sub = NSMenu()
            sub.autoenablesItems = false
            for pkg in active {
                let line = "\(PackageNames.display(pkg.code))　\(Self.number(pkg.remain)) / \(Self.number(pkg.total))"
                let item = info(line, enabled: true)
                item.toolTip = pkg.code
                sub.addItem(item)
                let detail = info("    已用 \(Self.number(pkg.used))", enabled: false)
                sub.addItem(detail)
            }
            if unused > 0 {
                sub.addItem(.separator())
                let exhausted = value.packages.filter { $0.remain <= 0.0001 }
                    .map { PackageNames.display($0.code) }
                    .joined(separator: "、")
                sub.addItem(info("已用尽：\(exhausted)", enabled: false))
            }
            parent.submenu = sub
            menu.addItem(parent)
        }

        // —— 签到 ——
        if let checkin = checkin, checkin.active {
            menu.addItem(.separator())
            let text = checkin.todayCheckedIn
                ? "✅ 今日已签到 · 连续 \(checkin.streakDays) 天 · 本周 \(checkin.weekCheckinDays) 天"
                : "📝 今日未签到 · 签可得 \(Self.number(checkin.dailyCredit)) 积分"
            menu.addItem(info(text, enabled: true))
        }

        // —— 操作 ——
        menu.addItem(.separator())

        let refresh = NSMenuItem(title: isLoading ? "🔄 正在刷新…" : "🔄 立即刷新",
                                 action: #selector(reload),
                                 keyEquivalent: "r")
        refresh.target = self
        refresh.isEnabled = !isLoading
        menu.addItem(refresh)

        menu.addItem(makeIntervalMenu())
        menu.addItem(makeDisplayModeMenu())

        let login = NSMenuItem(title: "🚀 登录时启动", action: #selector(toggleLaunchAtLogin(_:)), keyEquivalent: "")
        login.target = self
        login.state = LaunchAtLogin.isEnabled ? .on : .off
        menu.addItem(login)

        menu.addItem(.separator())

        let openApp = NSMenuItem(title: "🪟 打开 WorkBuddy", action: #selector(openWorkBuddy), keyEquivalent: "")
        openApp.target = self
        menu.addItem(openApp)

        let openConfig = NSMenuItem(title: "📁 打开配置文件夹", action: #selector(openConfigFolder), keyEquivalent: "")
        openConfig.target = self
        menu.addItem(openConfig)

        let copy = NSMenuItem(title: "📋 复制积分信息", action: #selector(copySummary), keyEquivalent: "")
        copy.target = self
        copy.isEnabled = summary != nil
        menu.addItem(copy)

        menu.addItem(.separator())

        let quit = NSMenuItem(title: "退出", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        quit.target = NSApp
        menu.addItem(quit)
    }

    private func makeIntervalMenu() -> NSMenuItem {
        let options: [(String, TimeInterval)] = [
            ("关闭（仅手动刷新）", 0),
            ("每 1 分钟", 60),
            ("每 5 分钟", 300),
            ("每 15 分钟", 900),
            ("每 30 分钟", 1800),
        ]
        let parent = NSMenuItem(title: "⏱ 自动刷新", action: nil, keyEquivalent: "")
        let sub = NSMenu()
        sub.autoenablesItems = false
        let current = Prefs.refreshInterval
        for (title, seconds) in options {
            let item = NSMenuItem(title: title, action: #selector(setRefreshInterval(_:)), keyEquivalent: "")
            item.target = self
            item.tag = Int(seconds)
            item.state = (abs(current - seconds) < 0.5) ? .on : .off
            sub.addItem(item)
        }
        parent.submenu = sub
        return parent
    }

    private func makeDisplayModeMenu() -> NSMenuItem {
        let options: [(String, DisplayMode)] = [
            ("显示积分数值", .value),
            ("显示剩余百分比", .percent),
            ("仅显示图标", .iconOnly),
        ]
        let parent = NSMenuItem(title: "🎚 菜单栏显示", action: nil, keyEquivalent: "")
        let sub = NSMenu()
        sub.autoenablesItems = false
        let current = Prefs.displayMode
        for (title, mode) in options {
            let item = NSMenuItem(title: title, action: #selector(setDisplayMode(_:)), keyEquivalent: "")
            item.target = self
            item.tag = mode.rawValue
            item.state = (current == mode) ? .on : .off
            sub.addItem(item)
        }
        parent.submenu = sub
        return parent
    }

    // MARK: 动作

    @objc private func noop() {}

    @objc private func setRefreshInterval(_ sender: NSMenuItem) {
        Prefs.refreshInterval = TimeInterval(sender.tag)
        configureRefreshTimer()
        if sender.tag > 0 { reload() }
    }

    @objc private func setDisplayMode(_ sender: NSMenuItem) {
        Prefs.displayMode = DisplayMode(rawValue: sender.tag) ?? .value
        renderStatusTitle()
    }

    @objc private func toggleLaunchAtLogin(_ sender: NSMenuItem) {
        if let error = LaunchAtLogin.toggle() {
            AppConfig.log("启动项设置失败: \(error)")
            let alert = NSAlert()
            alert.messageText = "无法修改启动项"
            alert.informativeText = error
            alert.alertStyle = .warning
            alert.addButton(withTitle: "好")
            NSApp.activate(ignoringOtherApps: true)
            alert.runModal()
        } else {
            AppConfig.log("登录时启动: \(LaunchAtLogin.isEnabled ? "已开启" : "已关闭")")
        }
        sender.state = LaunchAtLogin.isEnabled ? .on : .off
    }

    @objc private func openWorkBuddy() {
        let candidates = ["com.tencent.workbuddy.mac", "cn.com.tencent.workbuddy", "com.tencent.codebuddy"]
        for id in candidates {
            if let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: id) {
                NSWorkspace.shared.openApplication(at: url, configuration: NSWorkspace.OpenConfiguration())
                return
            }
        }
        let path = URL(fileURLWithPath: "/Applications/WorkBuddy.app")
        if FileManager.default.fileExists(atPath: path.path) {
            NSWorkspace.shared.openApplication(at: path, configuration: NSWorkspace.OpenConfiguration())
        } else {
            NSWorkspace.shared.open(URL(string: "https://www.workbuddy.cn")!)
        }
    }

    @objc private func openConfigFolder() {
        AppConfig.writeTemplateIfNeeded()
        NSWorkspace.shared.activateFileViewerSelecting([AppConfig.fileURL])
    }

    @objc private func copySummary() {
        var lines: [String] = []
        if let cred = credential { lines.append("账户：\(cred.displayName)") }
        if let value = summary {
            lines.append("剩余积分：\(Self.number(value.totalRemain)) / \(Self.number(value.totalCapacity))")
            for pkg in value.activePackages {
                lines.append("  - \(PackageNames.display(pkg.code)): \(Self.number(pkg.remain)) / \(Self.number(pkg.total))")
            }
        }
        if let error = lastError { lines.append("错误：\(error)") }
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString(lines.joined(separator: "\n"), forType: .string)
    }

    // MARK: 辅助

    private func info(_ title: String, enabled: Bool) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: enabled ? #selector(noop) : nil, keyEquivalent: "")
        if enabled { item.target = self }
        item.isEnabled = enabled
        return item
    }

    private static func balanceTitle(remain: Double, capacity: Double) -> NSAttributedString {
        let result = NSMutableAttributedString()
        result.append(NSAttributedString(
            string: "💰 剩余 ",
            attributes: [.font: NSFont.systemFont(ofSize: 13, weight: .regular)]
        ))
        result.append(NSAttributedString(
            string: number(remain),
            attributes: [
                .font: NSFont.monospacedDigitSystemFont(ofSize: 15, weight: .semibold),
                .foregroundColor: NSColor.systemGreen,
            ]
        ))
        result.append(NSAttributedString(
            string: " / \(number(capacity))",
            attributes: [
                .font: NSFont.monospacedDigitSystemFont(ofSize: 12, weight: .regular),
                .foregroundColor: NSColor.secondaryLabelColor,
            ]
        ))
        return result
    }

    /// 千分位格式化
    static func number(_ value: Double) -> String {
        let formatter = NumberFormatter()
        formatter.numberStyle = .decimal
        formatter.groupingSeparator = ","
        formatter.usesGroupingSeparator = true
        if abs(value) >= 100 {
            formatter.maximumFractionDigits = 0
            formatter.minimumFractionDigits = 0
        } else {
            formatter.maximumFractionDigits = 2
            formatter.minimumFractionDigits = 0
        }
        return formatter.string(from: NSNumber(value: value)) ?? String(format: "%.2f", value)
    }

    /// 菜单栏用的紧凑格式
    static func compactNumber(_ value: Double) -> String {
        let absValue = abs(value)
        if absValue >= 100_000_000 { return String(format: "%.1f亿", value / 100_000_000) }
        if absValue >= 10_000 { return String(format: "%.2f万", value / 10_000) }
        if absValue >= 100 { return String(format: "%.0f", value) }
        if absValue >= 10 { return String(format: "%.1f", value) }
        return String(format: "%.2f", value)
    }
}

// MARK: - 入口

if CommandLine.arguments.contains("--check") || CommandLine.arguments.contains("--diagnose") {
    Diagnostics.run()
    exit(0)
}

// 供脚本 / 安装流程调用，无需打开界面即可开关登录自启
if CommandLine.arguments.contains("--enable-login") {
    if let error = LaunchAtLogin.enable() {
        AppConfig.log("登录时启动 开启失败: \(error)")
        FileHandle.standardError.write(Data("开启失败：\(error)\n".utf8))
        exit(1)
    }
    print("已开启登录时启动 → \(LaunchAtLogin.plistURL.path)")
    exit(0)
}

if CommandLine.arguments.contains("--disable-login") {
    if let error = LaunchAtLogin.disable() {
        AppConfig.log("登录时启动 关闭失败: \(error)")
        FileHandle.standardError.write(Data("关闭失败：\(error)\n".utf8))
        exit(1)
    }
    print("已关闭登录时启动")
    exit(0)
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.accessory)
app.run()

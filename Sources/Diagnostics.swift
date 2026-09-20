import Foundation

// MARK: - 命令行诊断模式
//
// 运行：WorkBuddyStatus.app/Contents/MacOS/WorkBuddyStatus --check
// 作用：不开界面，直接把登录信息探测结果与积分余额打印到终端，便于排查问题。

enum Diagnostics {

    static func run() {
        print("WorkBuddy 积分状态栏小工具 · 诊断模式")
        print(String(repeating: "─", count: 46))

        let config = AppConfig.load()
        print("接口地址: \(config.endpoint)")

        let credential: WorkBuddyCredential
        do {
            credential = try CredentialLoader.load(config: config)
        } catch {
            print("❌ 登录信息: \((error as? LocalizedError)?.errorDescription ?? "\(error)")")
            exit(1)
        }

        print("👤 账户  : \(credential.displayName)")
        print("🆔 用户ID: \(credential.userId)")
        print("🔑 令牌  : \(mask(credential.accessToken))")
        if let exp = credential.expiresAt {
            let formatter = DateFormatter()
            formatter.dateFormat = "yyyy-MM-dd HH:mm"
            print("⏳ 有效期: \(formatter.string(from: exp))\(exp.timeIntervalSinceNow < 0 ? "（已过期）" : "")")
        }
        print("📄 来源  : \(credential.source)")
        print(String(repeating: "─", count: 46))

        let client = CreditsClient()
        client.endpoint = credential.endpoint

        var summaryResult: Result<CreditSummary, Error>?
        var checkinResult: Result<CheckinStatus, Error>?

        let group = DispatchGroup()
        group.enter()
        client.fetchSummary(credential: credential) { result in
            summaryResult = result
            group.leave()
        }
        group.enter()
        client.fetchCheckin(credential: credential) { result in
            checkinResult = result
            group.leave()
        }

        if group.wait(timeout: .now() + 35) == .timedOut {
            print("❌ 请求超时")
            exit(1)
        }

        switch summaryResult {
        case .success(let summary):
            print("💰 剩余积分: \(AppDelegate.number(summary.totalRemain)) / \(AppDelegate.number(summary.totalCapacity))")
            print("   已消耗  : \(AppDelegate.number(summary.totalUsed))")
            print("   付费用户: \(summary.isPaidUser ? "是" : "否")")
            print("   可用套餐:")
            for pkg in summary.activePackages {
                print("     • \(PackageNames.display(pkg.code))  \(AppDelegate.number(pkg.remain)) / \(AppDelegate.number(pkg.total))   [\(pkg.code)]")
            }
        case .failure(let error):
            print("❌ 积分获取失败: \((error as? LocalizedError)?.errorDescription ?? "\(error)")")
            exit(1)
        case .none:
            print("❌ 积分获取失败: 无响应")
            exit(1)
        }

        if case .success(let checkin) = checkinResult {
            print("📅 签到    : \(checkin.todayCheckedIn ? "今日已签到" : "今日未签到") · 连续 \(checkin.streakDays) 天 · 本周 \(checkin.weekCheckinDays) 天")
        }

        print(String(repeating: "─", count: 46))
        print("✅ 数据链路正常")
    }

    private static func mask(_ token: String) -> String {
        guard token.count > 24 else { return "***" }
        return "\(token.prefix(12))…\(token.suffix(8))（共 \(token.count) 字符）"
    }
}

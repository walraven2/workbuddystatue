import Foundation

// MARK: - 错误类型

enum WorkBuddyStatusError: LocalizedError {
    case authFileMissing
    case authFileUnreadable(String)
    case tokenMissing
    case tokenExpired(Date)

    var errorDescription: String? {
        switch self {
        case .authFileMissing:
            return "未找到 WorkBuddy 登录信息，请先打开并登录 WorkBuddy 桌面端"
        case .authFileUnreadable(let path):
            return "无法读取登录信息：\(path)"
        case .tokenMissing:
            return "登录信息中没有访问令牌，请在 WorkBuddy 中重新登录"
        case .tokenExpired(let date):
            let f = DateFormatter()
            f.dateFormat = "MM-dd HH:mm"
            return "登录令牌已于 \(f.string(from: date)) 过期，请在 WorkBuddy 中重新登录"
        }
    }
}

// MARK: - 配置

/// 可选的用户配置：~/.workbuddy-status/config.json
///
/// ```json
/// {
///   "endpoint": "https://copilot.tencent.com",
///   "accessToken": "可选，留空则自动从 WorkBuddy 读取",
///   "userId": "可选",
///   "refreshInterval": 300
/// }
/// ```
struct AppConfig {
    var endpoint: String = AppConfig.defaultEndpoint
    var accessToken: String?
    var userId: String?
    var refreshInterval: TimeInterval?

    static let defaultEndpoint = "https://copilot.tencent.com"

    static var fileURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".workbuddy-status", isDirectory: true)
            .appendingPathComponent("config.json")
    }

    /// 出错时写入的日志文件，便于排查
    static var logURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".workbuddy-status", isDirectory: true)
            .appendingPathComponent("last-error.log")
    }

    static func log(_ message: String) {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd HH:mm:ss"
        let line = "[\(formatter.string(from: Date()))] \(message)\n"
        FileHandle.standardError.write(Data(line.utf8))

        let url = logURL
        let dir = url.deletingLastPathComponent()
        if !FileManager.default.fileExists(atPath: dir.path) {
            try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        }
        if let handle = try? FileHandle(forWritingTo: url) {
            handle.seekToEndOfFile()
            handle.write(Data(line.utf8))
            try? handle.close()
        } else {
            try? line.write(to: url, atomically: true, encoding: .utf8)
        }
    }

    static func load() -> AppConfig {
        var config = AppConfig()

        if let path = ProcessInfo.processInfo.environment["WORKBUDDY_ENDPOINT"], !path.isEmpty {
            config.endpoint = path
        }

        if let data = try? Data(contentsOf: fileURL),
           let root = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] {
            if let v = root["endpoint"] as? String, !v.isEmpty { config.endpoint = v }
            if let v = root["accessToken"] as? String, !v.isEmpty { config.accessToken = v }
            if let v = root["userId"] as? String, !v.isEmpty { config.userId = v }
            if let n = root["refreshInterval"] as? NSNumber { config.refreshInterval = n.doubleValue }
        }

        if let v = ProcessInfo.processInfo.environment["WORKBUDDY_ACCESS_TOKEN"], !v.isEmpty {
            config.accessToken = v
        }
        if let v = ProcessInfo.processInfo.environment["WORKBUDDY_USER_ID"], !v.isEmpty {
            config.userId = v
        }

        return config
    }

    /// 生成一份带注释的示例配置（用于“打开配置文件”菜单项）
    static func writeTemplateIfNeeded() {
        let fm = FileManager.default
        let url = fileURL
        let dir = url.deletingLastPathComponent()
        if !fm.fileExists(atPath: dir.path) {
            try? fm.createDirectory(at: dir, withIntermediateDirectories: true)
        }
        guard !fm.fileExists(atPath: url.path) else { return }
        let template = """
        {
          "//": "WorkBuddy 状态栏小工具配置。accessToken / userId 留空时会自动从 WorkBuddy 桌面端读取，通常无需填写。",
          "endpoint": "https://copilot.tencent.com",
          "accessToken": "",
          "userId": "",
          "//refreshInterval": "单位秒，留空使用菜单中的设置",
          "refreshInterval": 0
        }
        """
        try? template.write(to: url, atomically: true, encoding: .utf8)
    }
}

// MARK: - 凭据

struct WorkBuddyCredential {
    let accessToken: String
    let userId: String
    let nickname: String
    let expiresAt: Date?
    let endpoint: String
    let source: String

    var displayName: String {
        nickname.isEmpty ? (userId.isEmpty ? "WorkBuddy 用户" : userId) : nickname
    }
}

// MARK: - 凭据加载

enum CredentialLoader {

    /// 自动探测 WorkBuddy 桌面端的登录信息目录
    static var candidateAuthDirectories: [URL] {
        let home = FileManager.default.homeDirectoryForCurrentUser
        return [
            // WorkBuddy / CodeBuddy 桌面端（macOS）
            home.appendingPathComponent("Library/Application Support/CodeBuddyExtension/Data/Public/auth"),
            home.appendingPathComponent("Library/Application Support/CodeBuddyExtension/Data/Public"),
            // 兼容旧版本 / 其他布局
            home.appendingPathComponent(".workbuddy/auth"),
            home.appendingPathComponent(".codebuddy/auth"),
        ]
    }

    static func load(config: AppConfig = .load()) throws -> WorkBuddyCredential {
        // 1) 显式配置的令牌优先
        if let token = config.accessToken, !token.isEmpty {
            let cred = WorkBuddyCredential(
                accessToken: token,
                userId: config.userId ?? "",
                nickname: "",
                expiresAt: Self.jwtExpiry(token),
                endpoint: config.endpoint,
                source: "配置文件 / 环境变量"
            )
            try validate(cred)
            return cred
        }

        // 2) 扫描 WorkBuddy 登录信息文件
        for dir in candidateAuthDirectories {
            guard let files = try? FileManager.default.contentsOfDirectory(
                at: dir,
                includingPropertiesForKeys: [.contentModificationDateKey],
                options: [.skipsHiddenFiles]
            ) else { continue }

            let infos = files
                .filter { $0.pathExtension.lowercased() == "info" }
                .sorted { a, b in
                    let da = (try? a.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast
                    let db = (try? b.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast
                    return da > db
                }

            for file in infos {
                if let cred = parseAuthFile(file, endpoint: config.endpoint) {
                    try validate(cred)
                    return cred
                }
            }
        }

        throw WorkBuddyStatusError.authFileMissing
    }

    // MARK: - 私有

    private static func validate(_ cred: WorkBuddyCredential) throws {
        if cred.accessToken.isEmpty { throw WorkBuddyStatusError.tokenMissing }
        if let exp = cred.expiresAt, exp.timeIntervalSinceNow < 0 {
            throw WorkBuddyStatusError.tokenExpired(exp)
        }
    }

    static func parseAuthFile(_ url: URL, endpoint: String) -> WorkBuddyCredential? {
        guard let data = try? Data(contentsOf: url),
              let root = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let auth = root["auth"] as? [String: Any],
              let token = auth["accessToken"] as? String,
              !token.isEmpty
        else { return nil }

        let account = (root["account"] as? [String: Any]) ?? [:]
        let uid = (account["uid"] as? String) ?? (account["userId"] as? String) ?? ""
        let nickname = (account["nickname"] as? String) ?? (account["userName"] as? String) ?? ""

        var expires: Date?
        if let n = auth["expiresAt"] as? NSNumber {
            expires = Self.date(fromEpoch: n.doubleValue)
        }
        if expires == nil { expires = Self.jwtExpiry(token) }

        return WorkBuddyCredential(
            accessToken: token,
            userId: uid,
            nickname: nickname,
            expiresAt: expires,
            endpoint: endpoint,
            source: url.path
        )
    }

    /// 兼容秒 / 毫秒两种时间戳
    static func date(fromEpoch value: Double) -> Date? {
        guard value > 0 else { return nil }
        return Date(timeIntervalSince1970: value > 100_000_000_000 ? value / 1000.0 : value)
    }

    /// 从 JWT 的 exp 字段解析过期时间（本地解析，不做签名校验）
    static func jwtExpiry(_ token: String) -> Date? {
        let parts = token.split(separator: ".")
        guard parts.count >= 2 else { return nil }
        var payload = String(parts[1])
        // base64url -> base64
        payload = payload.replacingOccurrences(of: "-", with: "+")
                         .replacingOccurrences(of: "_", with: "/")
        while payload.count % 4 != 0 { payload += "=" }
        guard let data = Data(base64Encoded: payload),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let exp = obj["exp"] as? NSNumber
        else { return nil }
        return Date(timeIntervalSince1970: exp.doubleValue)
    }
}

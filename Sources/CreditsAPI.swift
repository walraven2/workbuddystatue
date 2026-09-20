import Foundation

// MARK: - 数据模型

struct CreditPackage {
    let code: String
    let total: Double
    let remain: Double
    let used: Double
    let frozen: Double
    let unit: String
}

struct CreditSummary {
    let packages: [CreditPackage]
    let isPaidUser: Bool
    let subscriptionCode: String

    var totalRemain: Double { packages.reduce(0) { $0 + $1.remain } }
    var totalCapacity: Double { packages.reduce(0) { $0 + $1.total } }
    var totalUsed: Double { packages.reduce(0) { $0 + $1.used } }
    /// 仍有额度的套餐（用于明细展示，按剩余量排序）
    var activePackages: [CreditPackage] {
        packages.filter { $0.remain > 0.0001 }.sorted { $0.remain > $1.remain }
    }
    var usageRatio: Double {
        guard totalCapacity > 0 else { return 0 }
        return min(max(totalRemain / totalCapacity, 0), 1)
    }
}

struct CheckinStatus {
    let active: Bool
    let todayCheckedIn: Bool
    let streakDays: Int
    let dailyCredit: Double
    let todayCredit: Double
    let weekCheckinDays: Int
}

// MARK: - 套餐名称映射（对应 WorkBuddy 内置 COMMODITY_CODES）

enum PackageNames {
    private static let map: [String: String] = [
        "TCACA_code_001_PqouKr6QWV": "免费版",
        "TCACA_code_002_AkiJS3ZHF5": "专业版（月）",
        "TCACA_code_003_FAnt7lcmRT": "专业版（年）",
        "TCACA_code_005_maRGyrHhw1": "专业版 Plus（月）",
        "TCACA_code_006_DbXS0lrypC": "专业版试用",
        "TCACA_code_007_nzdH5h4Nl0": "成长计划（活动）",
        "TCACA_code_008_cfWoLwvjU4": "专业版（按日）",
        "TCACA_code_009_0XmEQc2xOf": "积分加油包",
        "TCACA_code_023_4xbGhMrE6q": "青春版",
        "TCACA_code_026_BaESVICNoi": "高级版",
        "TCACA_code_027_0FCGVA6vSa": "旗舰版",
        "TCACA_code_028_NtpWi0jzXs": "奖励积分 A",
        "TCACA_code_029_6wCGEWquYy": "奖励积分 B",
        "TCACA_code_030_BjSt89qTvr": "奖励积分 C",
        "TCACA_code_035_ArVxJcGDsm": "专业版（国际）",
        "TCACA_code_036_lupO5WgNdG": "积分包（国际）",
        "TCACA_code_037_WxOD3MpI2o": "奖励积分（国际）",
        "TCACA_code_038_OhvqZtiPKr": "积分包 D",
        "TCACA_code_039_KRcQj7wUat": "专业版试用（月）",
        "TCACA_code_040_mi9rCYg46x": "专业版试用（年）",
    ]

    static func display(_ code: String) -> String {
        if let name = map[code] { return name }
        // 兜底：TCACA_code_0xx_xxxx -> 套餐 0xx
        let parts = code.split(separator: "_")
        if let idx = parts.firstIndex(of: "code"), idx + 1 < parts.count {
            return "套餐 \(parts[idx + 1])"
        }
        return code
    }
}

// MARK: - 错误

enum CreditsError: LocalizedError {
    case unauthorized
    case badEndpoint
    case http(Int, String)
    case api(String, String)
    case decoding(String)
    case network(String)

    var errorDescription: String? {
        switch self {
        case .unauthorized:
            return "登录已失效，请在 WorkBuddy 中重新登录"
        case .badEndpoint:
            return "接口地址无效（请检查 ~/.workbuddy-status/config.json）"
        case .http(let code, let body):
            return "接口返回 HTTP \(code)：\(body)"
        case .api(let code, let msg):
            return "接口错误 \(code)：\(msg)"
        case .decoding(let detail):
            return "返回数据解析失败：\(detail)"
        case .network(let detail):
            return "网络请求失败：\(detail)"
        }
    }
}

// MARK: - 客户端

final class CreditsClient {

    var endpoint: String = AppConfig.defaultEndpoint

    private let session: URLSession

    init(timeout: TimeInterval = 20) {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.timeoutIntervalForRequest = timeout
        cfg.timeoutIntervalForResource = timeout + 10
        cfg.waitsForConnectivity = false
        session = URLSession(configuration: cfg)
    }

    // MARK: 公开接口

    /// POST /billing/meter/get-user-resource-summary
    func fetchSummary(credential: WorkBuddyCredential,
                      completion: @escaping (Result<CreditSummary, Error>) -> Void) {
        post(path: "/billing/meter/get-user-resource-summary",
             body: [:],
             credential: credential) { result in
            switch result {
            case .failure(let err):
                completion(.failure(err))
            case .success(let json):
                do {
                    completion(.success(try Self.parseSummary(json)))
                } catch {
                    completion(.failure(error))
                }
            }
        }
    }

    /// POST /billing/meter/checkin-activity-status
    func fetchCheckin(credential: WorkBuddyCredential,
                      completion: @escaping (Result<CheckinStatus, Error>) -> Void) {
        post(path: "/billing/meter/checkin-activity-status",
             body: [:],
             credential: credential) { result in
            switch result {
            case .failure(let err):
                completion(.failure(err))
            case .success(let json):
                guard let data = json["data"] as? [String: Any] else {
                    completion(.failure(CreditsError.decoding("缺少 data 字段")))
                    return
                }
                let status = CheckinStatus(
                    active: Self.bool(data["active"]),
                    todayCheckedIn: Self.bool(data["today_checked_in"]),
                    streakDays: Self.int(data["streak_days"]),
                    dailyCredit: Self.double(data["daily_credit"]),
                    todayCredit: Self.double(data["today_credit"]),
                    weekCheckinDays: Self.int(data["week_checkin_days"])
                )
                completion(.success(status))
            }
        }
    }

    // MARK: 私有

    private func post(path: String,
                      body: [String: Any],
                      credential: WorkBuddyCredential,
                      completion: @escaping (Result<[String: Any], Error>) -> Void) {
        guard let url = URL(string: endpoint.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + path) else {
            completion(.failure(CreditsError.badEndpoint))
            return
        }

        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("Bearer \(credential.accessToken)", forHTTPHeaderField: "Authorization")
        request.setValue("zh", forHTTPHeaderField: "Accept-Language")
        request.setValue("WorkBuddyStatus/1.0 (macOS)", forHTTPHeaderField: "User-Agent")
        if !credential.userId.isEmpty {
            request.setValue(credential.userId, forHTTPHeaderField: "X-User-Id")
        }
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)

        session.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(CreditsError.network(error.localizedDescription)))
                return
            }
            let http = response as? HTTPURLResponse
            let status = http?.statusCode ?? 0
            let text = data.flatMap { String(data: $0, encoding: .utf8) } ?? ""

            if status == 401 || status == 403 {
                completion(.failure(CreditsError.unauthorized))
                return
            }
            guard let data = data,
                  let json = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
                completion(.failure(CreditsError.http(status, text.isEmpty ? "空响应" : String(text.prefix(200)))))
                return
            }
            guard status == 200 else {
                completion(.failure(CreditsError.http(status, String(text.prefix(200)))))
                return
            }
            let code = (json["code"] as? NSNumber)?.intValue ?? 0
            guard code == 0 else {
                let msg = (json["msg"] as? String) ?? "未知错误"
                if code == 401 || code == 40301 { completion(.failure(CreditsError.unauthorized)) }
                else { completion(.failure(CreditsError.api(String(code), msg))) }
                return
            }
            completion(.success(json))
        }.resume()
    }

    // MARK: 解析

    static func parseSummary(_ json: [String: Any]) throws -> CreditSummary {
        guard let data = json["data"] as? [String: Any] else {
            throw CreditsError.decoding("缺少 data 字段")
        }
        let rawPackages = (data["Packages"] as? [[String: Any]]) ?? []
        let packages = rawPackages.map { item -> CreditPackage in
            CreditPackage(
                code: (item["PackageCode"] as? String) ?? "",
                total: double(item["CycleTotalCapacity"]),
                remain: double(item["CycleRemainCapacity"]),
                used: double(item["CycleUsedCapacity"]),
                frozen: double(item["CycleFrozenCapacity"]),
                unit: (item["CapacityUnit"] as? String) ?? "credits"
            )
        }
        return CreditSummary(
            packages: packages,
            isPaidUser: bool(data["IsPaidUser"]),
            subscriptionCode: (data["SubscriptionPackageCode"] as? String) ?? ""
        )
    }

    // MARK: 类型转换（接口全部返回字符串数字）

    static func double(_ any: Any?) -> Double {
        if let n = any as? NSNumber { return n.doubleValue }
        if let s = any as? String { return Double(s) ?? 0 }
        return 0
    }

    static func int(_ any: Any?) -> Int {
        if let n = any as? NSNumber { return n.intValue }
        if let s = any as? String { return Int(s) ?? 0 }
        return 0
    }

    static func bool(_ any: Any?) -> Bool {
        if let b = any as? Bool { return b }
        if let n = any as? NSNumber { return n.boolValue }
        if let s = any as? String { return s == "true" || s == "1" }
        return false
    }
}

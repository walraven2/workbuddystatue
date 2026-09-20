#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
WorkBuddy 积分状态栏 —— Linux 版（XFCE 面板）

与 macOS 版共用同一套后端接口与凭据逻辑，显示载体换成 XFCE 面板的
Generic Monitor（genmon）插件：面板上常驻 `⚡1723`，点击弹出积分明细。

子命令
    daemon    后台常驻，按刷新间隔拉取接口并写入缓存
    genmon    供 genmon 插件调用，读缓存瞬时返回面板标记
    detail    弹出积分明细窗口（zenity）
    check     命令行诊断，打印账户 / 令牌有效期 / 积分明细
    fetch     直接调用接口并打印原始 JSON

凭据解析顺序
    1. ~/.workbuddy-status/config.json 里的 accessToken
    2. 环境变量 WORKBUDDY_ACCESS_TOKEN
    3. ~/.config/CodeBuddy CN/automations/automations.db（CodeBuddy 登录令牌）
"""

import base64
import hashlib
import json
import os
import re
import shutil
import sqlite3
import subprocess
import sys
import time
import urllib.error
import urllib.request

# ---------------------------------------------------------------- 路径常量

HOME = os.path.expanduser("~")
STATE_DIR = os.path.join(HOME, ".workbuddy-status")
CACHE_FILE = os.path.join(STATE_DIR, "cache.json")
CONFIG_FILE = os.path.join(STATE_DIR, "config.json")
LOG_FILE = os.path.join(STATE_DIR, "last-error.log")
LOCK_FILE = os.path.join(STATE_DIR, "daemon.lock")
PREF_FILE = os.path.join(STATE_DIR, ".token-fingerprint")
# 本地"今日已签到"标记（存日期，隔天自动失效）
CHECKIN_MARK_FILE = os.path.join(STATE_DIR, ".checkin-mark")

# 签到接口（与技能广场 workbuddy-checkin / leon-daily-checkin 交叉核对一致）
CHECKIN_PATH = "/billing/meter/daily-checkin"
CHECKIN_STATUS_PATH = "/billing/meter/checkin-status"
# 业务码：0=成功；10001=当日已签到（幂等拒绝，按成功处理）
CHECKIN_ALREADY_CODE = 10001

DEFAULT_ENDPOINT = "https://copilot.tencent.com"
DEFAULT_REFRESH = 300          # 秒
DEFAULT_ICON = "⚡"
DEFAULT_MODE = "credits"       # credits | percent | icon

# CodeBuddy CN 的登录令牌存放位置（SQLite，令牌在 automation_runs.runs_json 内）
AUTOMATION_DBS = [
    os.path.join(HOME, ".config/CodeBuddy CN/automations/automations.db"),
]

_JWT_PATTERN = r"eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{16,}\.[A-Za-z0-9_\-]{8,}"
JWT_RE = re.compile(_JWT_PATTERN)                 # 文本
JWT_RE_BYTES = re.compile(_JWT_PATTERN.encode())  # 原始字节

# ------------------------------------------------- 套餐代码 -> 中文名（同 macOS 版）

PACKAGE_NAMES = {
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
}


def package_name(code):
    if code in PACKAGE_NAMES:
        return PACKAGE_NAMES[code]
    parts = code.split("_")
    if "code" in parts:
        i = parts.index("code")
        if i + 1 < len(parts):
            return "套餐 %s" % parts[i + 1]
    return code or "未知套餐"


# ---------------------------------------------------------------- 日志

def ensure_state_dir():
    os.makedirs(STATE_DIR, exist_ok=True)


def log(message):
    line = "[%s] %s\n" % (time.strftime("%Y-%m-%d %H:%M:%S"), message)
    sys.stderr.write(line)
    try:
        ensure_state_dir()
        with open(LOG_FILE, "a", encoding="utf-8") as fh:
            fh.write(line)
    except OSError:
        pass


# ---------------------------------------------------------------- 配置

class Config(object):
    def __init__(self):
        self.endpoint = DEFAULT_ENDPOINT
        self.access_token = None
        self.user_id = None
        self.refresh = DEFAULT_REFRESH
        self.icon = DEFAULT_ICON
        self.mode = DEFAULT_MODE


def load_config():
    cfg = Config()
    if os.path.exists(CONFIG_FILE):
        try:
            with open(CONFIG_FILE, "r", encoding="utf-8") as fh:
                root = json.load(fh)
            if root.get("endpoint"):
                cfg.endpoint = root["endpoint"]
            if root.get("accessToken"):
                cfg.access_token = root["accessToken"]
            if root.get("userId"):
                cfg.user_id = root["userId"]
            if root.get("refreshInterval"):
                cfg.refresh = int(root["refreshInterval"])
            if root.get("icon"):
                cfg.icon = root["icon"]
            if root.get("mode") in ("credits", "percent", "icon"):
                cfg.mode = root["mode"]
        except (OSError, ValueError) as exc:
            log("配置文件读取失败：%s" % exc)

    if os.environ.get("WORKBUDDY_ENDPOINT"):
        cfg.endpoint = os.environ["WORKBUDDY_ENDPOINT"]
    if os.environ.get("WORKBUDDY_ACCESS_TOKEN"):
        cfg.access_token = os.environ["WORKBUDDY_ACCESS_TOKEN"]
    if os.environ.get("WORKBUDDY_USER_ID"):
        cfg.user_id = os.environ["WORKBUDDY_USER_ID"]
    return cfg


# ---------------------------------------------------------------- JWT

def jwt_payload(token):
    """本地解析 JWT payload，不校验签名。"""
    try:
        parts = token.split(".")
        if len(parts) < 2:
            return {}
        pad = parts[1] + "=" * (-len(parts[1]) % 4)
        return json.loads(base64.urlsafe_b64decode(pad).decode("utf-8", "replace"))
    except Exception:
        return {}


def jwt_expiry(token):
    exp = jwt_payload(token).get("exp")
    return int(exp) if isinstance(exp, (int, float)) else None


# ---------------------------------------------------------------- 凭据发现

def _tokens_in_sqlite(path):
    """从 SQLite 库里捞出所有 JWT（含原始字节兜底，覆盖 WAL 里的内容）。"""
    found = set()
    if not os.path.exists(path):
        return found
    # 1) 正规查询
    try:
        uri = "file:%s?mode=ro" % path.replace("?", "%3f")
        con = sqlite3.connect(uri, uri=True)
        cur = con.cursor()
        cur.execute("SELECT name FROM sqlite_master WHERE type='table'")
        tables = [r[0] for r in cur.fetchall()]
        for table in tables:
            try:
                cur.execute("SELECT * FROM %s" % table)
                for row in cur.fetchall():
                    for value in row:
                        if isinstance(value, str):
                            found.update(JWT_RE.findall(value))
                        elif isinstance(value, bytes):
                            found.update(m.decode() for m in JWT_RE_BYTES.findall(value))
            except sqlite3.Error:
                continue
        con.close()
    except sqlite3.Error as exc:
        log("查询 %s 失败：%s" % (path, exc))
    # 2) 原始字节兜底（未 checkpoint 的 WAL 内容）
    for extra in (path, path + "-wal"):
        try:
            with open(extra, "rb") as fh:
                found.update(m.decode() for m in JWT_RE_BYTES.findall(fh.read()))
        except OSError:
            pass
    return found


def read_preferred_fingerprint():
    try:
        with open(PREF_FILE, "r", encoding="utf-8") as fh:
            return fh.read().strip()
    except OSError:
        return ""


def write_preferred_fingerprint(fingerprint):
    try:
        ensure_state_dir()
        with open(PREF_FILE, "w", encoding="utf-8") as fh:
            fh.write(fingerprint)
    except OSError:
        pass


# ------------------------------------------------ 「今日已签到」本地标记
# 服务端的 today_checked_in 实测不可靠（签到成功后仍可能返回 false），
# 所以成功签到后在这里落一个日期戳，当天一直认；跨天自动失效。

def _today_str():
    return time.strftime("%Y-%m-%d")


def mark_checked_in_today():
    try:
        ensure_state_dir()
        with open(CHECKIN_MARK_FILE, "w", encoding="utf-8") as fh:
            fh.write(_today_str())
    except OSError:
        pass


def locally_checked_in():
    """今天是否已经成功签到过（本地标记）。"""
    try:
        with open(CHECKIN_MARK_FILE, "r", encoding="utf-8") as fh:
            return fh.read().strip() == _today_str()
    except OSError:
        return False


def candidate_credentials(cfg):
    """按「越可能有效」排序返回候选凭据列表。

    注意：CodeBuddy 的登录库里会残留同一个令牌被截断的残片（声明相同、
    签名长度不同），仅凭 claims 无法判断哪个是完整的，所以这里不猜——
    按 新→旧、长→短 排序，交给调用方实际打一次接口来验证。
    """
    items = []
    seen = set()
    now = int(time.time())

    def push(token, source, explicit_uid=""):
        if not token or token in seen:
            return
        payload = jwt_payload(token)
        exp = payload.get("exp")
        exp = int(exp) if isinstance(exp, (int, float)) else 0
        if exp and exp < now:
            return
        iss = str(payload.get("iss", ""))
        if source != "配置文件 / 环境变量" and "codebuddy" not in iss and "copilot" not in iss:
            return
        seen.add(token)
        items.append({
            "token": token,
            "uid": explicit_uid or payload.get("sub") or "",
            "source": source,
            "exp": exp,
            "iat": int(payload.get("iat") or 0),
            "fingerprint": hashlib.md5(token.encode()).hexdigest(),
        })

    if cfg.access_token:
        push(cfg.access_token, "配置文件 / 环境变量", cfg.user_id or "")

    for db in AUTOMATION_DBS:
        for token in _tokens_in_sqlite(db):
            push(token, db, cfg.user_id or "")

    preferred = read_preferred_fingerprint()
    items.sort(key=lambda it: (it["fingerprint"] != preferred, -it["iat"], -len(it["token"])))
    return items


def fetch_with_fallback(cfg, timeout=25):
    """依次尝试候选凭据，返回 (数据, 命中凭据)。全部失败则抛最后一个错误。"""
    candidates = candidate_credentials(cfg)
    if not candidates:
        raise RuntimeError("未找到登录令牌，请先在 CodeBuddy 桌面端登录")

    last_error = None
    for cred in candidates[:12]:
        try:
            data = fetch_summary(cfg, cred["token"], cred["uid"], timeout=timeout)
        except urllib.error.HTTPError as exc:
            last_error = exc
            if exc.code in (401, 403):
                log("令牌 %s… 不可用（HTTP %s），换下一个" % (cred["fingerprint"][:8], exc.code))
                continue
            raise
        write_preferred_fingerprint(cred["fingerprint"])
        return data, cred

    if last_error is not None:
        raise last_error
    raise RuntimeError("所有候选令牌都不可用")


# ---------------------------------------------------------------- 接口

def api_post(cfg, path, token, uid, timeout=25):
    url = cfg.endpoint.rstrip("/") + path
    body = json.dumps({}).encode("utf-8")
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Accept", "application/json")
    req.add_header("Accept-Language", "zh")
    req.add_header("User-Agent", "WorkBuddyStatus/1.0 (Linux)")
    req.add_header("Authorization", "Bearer %s" % token)
    if uid:
        req.add_header("X-User-Id", uid)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        raw = resp.read().decode("utf-8", "replace")
    data = json.loads(raw)
    if data.get("code") not in (0, None):
        raise RuntimeError("接口错误 %s：%s" % (data.get("code"), data.get("msg")))
    return data


def _post_checkin(cfg, token, uid, timeout=25):
    """POST 签到接口，返回 (payload, http_status)。

    ⚠️ 关键：服务端用 HTTP 400 承载业务码 10001（"今天已签到，请明天再来"）。
    所以 4xx 绝不能直接当失败——必须把响应体解析出来看业务码，
    否则「今日已签」会被误报成签到失败。
    """
    url = cfg.endpoint.rstrip("/") + CHECKIN_PATH
    body = json.dumps({}).encode("utf-8")
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Accept", "application/json")
    req.add_header("Accept-Language", "zh")
    req.add_header("User-Agent", "WorkBuddyStatus/1.0 (Linux)")
    req.add_header("Authorization", "Bearer %s" % token)
    if uid:
        req.add_header("X-User-Id", uid)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8", "replace")
            status = resp.getcode()
    except urllib.error.HTTPError as exc:
        try:
            raw = exc.read().decode("utf-8", "replace")
        except Exception:
            raw = ""
        status = exc.code
    try:
        return json.loads(raw or "{}"), status
    except ValueError:
        return {"code": None, "msg": (raw or "")[:200]}, status


def _num(value):
    try:
        return float(value)
    except (TypeError, ValueError):
        return 0.0


def fetch_summary(cfg, token, uid, timeout=25):
    payload = api_post(cfg, "/billing/meter/get-user-resource-summary", token, uid, timeout)
    data = payload.get("data") or {}
    packages = []
    for item in data.get("Packages") or []:
        packages.append({
            "code": item.get("PackageCode") or "",
            "name": package_name(item.get("PackageCode") or ""),
            "total": _num(item.get("CycleTotalCapacity")),
            "remain": _num(item.get("CycleRemainCapacity")),
            "used": _num(item.get("CycleUsedCapacity")),
            "frozen": _num(item.get("CycleFrozenCapacity")),
            "unit": item.get("CapacityUnit") or "credits",
        })

    checkin = None
    try:
        cpayload = api_post(cfg, "/billing/meter/checkin-activity-status", token, uid, timeout)
        cdata = cpayload.get("data") or {}
        checkin = {
            "active": bool(cdata.get("active")),
            "today": bool(cdata.get("today_checked_in")),
            "streak": int(_num(cdata.get("streak_days"))),
            "daily": _num(cdata.get("daily_credit")),
            "week": int(_num(cdata.get("week_checkin_days"))),
            "season": cdata.get("season"),
            "activity": cdata.get("activity_name") or "",
        }
        # 服务端 today_checked_in 不可靠：本地标记优先
        if locally_checked_in():
            checkin["today"] = True
            checkin["local_mark"] = True
    except Exception as exc:
        log("签到接口失败：%s" % exc)

    total_remain = sum(p["remain"] for p in packages)
    total_cap = sum(p["total"] for p in packages)
    return {
        "ok": True,
        "updated_at": int(time.time()),
        "token_expires_at": jwt_expiry(token),
        "is_paid": bool(data.get("IsPaidUser")),
        "packages": packages,
        "checkin": checkin,
        "total_remain": total_remain,
        "total_capacity": total_cap,
        "total_used": sum(p["used"] for p in packages),
        "ratio": (total_remain / total_cap) if total_cap > 0 else 0.0,
    }


# ---------------------------------------------------------------- 签到

def do_checkin(cfg, timeout=25):
    """执行每日签到，返回结果字典。

    幂等性：接口对"今日已签到"返回 code=10001，按成功处理，绝不重复领取。
    先复用 fetch_with_fallback 探出一个真能用的令牌，避免拿残片令牌去签到。
    """
    try:
        _summary, cred = fetch_with_fallback(cfg, timeout=timeout)
    except urllib.error.HTTPError as exc:
        return {"status": "error", "code": exc.code,
                "message": "获取可用令牌失败（HTTP %s），请先在 CodeBuddy 桌面端重新登录" % exc.code}
    except Exception as exc:
        return {"status": "error", "message": "获取可用令牌失败：%s" % exc}

    try:
        payload, http = _post_checkin(cfg, cred["token"], cred["uid"], timeout)
    except Exception as exc:
        return {"status": "error", "message": "签到请求失败：%s" % exc}

    code = payload.get("code")
    msg = payload.get("msg") or payload.get("message") or ""
    data = payload.get("data") or {}

    if code == 0:
        result = {
            "status": "ok",
            "credit": _num(data.get("credit")),
            "streak": int(_num(data.get("streak_days"))),
            "message": msg or "签到成功",
        }
    elif code == CHECKIN_ALREADY_CODE or "已签到" in str(msg):
        result = {"status": "already", "code": code, "message": "今日已签到，明天再来"}
    elif http in (401, 403):
        return {"status": "error", "code": http,
                "message": "令牌已过期或无权限（HTTP %s），请在桌面端重新登录" % http}
    else:
        return {"status": "error", "code": code, "http": http,
                "message": "签到接口返回 code=%s（HTTP %s）：%s" % (code, http, msg or "(无消息)")}

    mark_checked_in_today()
    return result


# ---------------------------------------------------------------- 缓存

def write_cache(data):
    ensure_state_dir()
    tmp = CACHE_FILE + ".tmp"
    with open(tmp, "w", encoding="utf-8") as fh:
        json.dump(data, fh, ensure_ascii=False)
    os.replace(tmp, CACHE_FILE)


def read_cache():
    try:
        with open(CACHE_FILE, "r", encoding="utf-8") as fh:
            return json.load(fh)
    except (OSError, ValueError):
        return None


def cache_or_error(cfg):
    """拿缓存；没有就现场抓一次。"""
    data = read_cache()
    if data and data.get("ok"):
        return data
    try:
        data, _cred = fetch_with_fallback(cfg)
        write_cache(data)
        return data
    except Exception as exc:
        return {"ok": False, "error": str(exc)}


# ---------------------------------------------------------------- 展示

def _fmt(value):
    """1223.02 -> 1223.02；500.0 -> 500"""
    if abs(value - round(value)) < 0.005:
        return "%d" % round(value)
    return ("%.2f" % value).rstrip("0").rstrip(".")


def _fmt_short(value):
    """面板上用的紧凑数字：大于 100 就取整，避免占太宽。"""
    if abs(value) >= 100:
        return "%.0f" % value
    return _fmt(value)


def panel_text(data, cfg):
    if not data.get("ok"):
        return "%s--" % cfg.icon
    if cfg.mode == "icon":
        return cfg.icon
    if cfg.mode == "percent":
        return "%s%d%%" % (cfg.icon, round(data.get("ratio", 0) * 100))
    return "%s%s" % (cfg.icon, _fmt_short(data.get("total_remain", 0)))


def tooltip_text(data):
    if not data.get("ok"):
        return "WorkBuddy：%s" % data.get("error", "未知错误")
    lines = ["WorkBuddy 积分", "剩余 %s / %s（%.1f%%）" % (
        _fmt(data.get("total_remain", 0)),
        _fmt(data.get("total_capacity", 0)),
        data.get("ratio", 0) * 100,
    )]
    for pkg in sorted(data.get("packages", []), key=lambda p: -p["remain"]):
        if pkg["remain"] > 0.0001:
            lines.append("· %s  %s / %s" % (pkg["name"], _fmt(pkg["remain"]), _fmt(pkg["total"])))
    checkin = data.get("checkin")
    if checkin:
        lines.append("签到：%s（连续 %d 天，本周 %d 天）" % (
            "今日已签" if checkin["today"] else "今日未签", checkin["streak"], checkin["week"]))
    updated = data.get("updated_at")
    if updated:
        lines.append("更新于 %s" % time.strftime("%m-%d %H:%M", time.localtime(updated)))
    return "\n".join(lines)


def xml_escape(text):
    return text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


# ---------------------------------------------------------------- 子命令

def cmd_genmon(cfg):
    """genmon 插件调用：读缓存秒回，输出 genmon 标记。"""
    data = read_cache() or {"ok": False, "error": "尚未刷新"}
    text = panel_text(data, cfg)
    tip = tooltip_text(data)
    script = os.path.abspath(__file__)
    print("<txt>%s</txt>" % xml_escape(text))
    print("<tool>%s</tool>" % xml_escape(tip))
    print("<click>%s detail</click>" % script)
    return 0


def cmd_detail(cfg):
    """弹出明细窗口。"""
    data = cache_or_error(cfg)
    if not data.get("ok"):
        body = "读取失败\n\n%s" % data.get("error", "")
        title = "WorkBuddy 积分"
    else:
        title = "WorkBuddy 积分"
        rows = ["<b>剩余 %s / %s</b>   （%.1f%%）" % (
            _fmt(data["total_remain"]), _fmt(data["total_capacity"]), data["ratio"] * 100), ""]
        rows.append("<b>套餐明细</b>")
        for pkg in sorted(data["packages"], key=lambda p: -p["remain"]):
            rows.append("  · %s：<tt>%s</tt> / %s（已用 %s）" % (
                pkg["name"], _fmt(pkg["remain"]), _fmt(pkg["total"]), _fmt(pkg["used"])))
        checkin = data.get("checkin")
        if checkin:
            rows += ["", "<b>签到</b>",
                     "  今日：%s" % ("已签到" if checkin["today"] else "未签到"),
                     "  连续 %d 天，本周 %d 天，每日 %s 积分" % (
                         checkin["streak"], checkin["week"], _fmt(checkin["daily"]))]
            if checkin.get("activity"):
                rows.append("  活动：%s" % checkin["activity"])
        exp = data.get("token_expires_at")
        rows += ["", "<small>更新于 %s%s</small>" % (
            time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(data["updated_at"])),
            "，令牌有效至 %s" % time.strftime("%Y-%m-%d", time.localtime(exp)) if exp else "")]
        body = "\n".join(rows)

    zenity = shutil.which("zenity")
    if zenity:
        subprocess.run([zenity, "--info", "--title=%s" % title,
                        "--width=460", "--text=%s" % body], check=False)
    else:
        print(body)
    return 0


def cmd_check(cfg):
    print("WorkBuddy 积分状态栏（Linux 版）诊断")
    print("=" * 46)
    print("接口地址   : %s" % cfg.endpoint)
    print("候选令牌   : %d 个" % len(candidate_credentials(cfg)))

    cred = None
    data = None
    try:
        data, cred = fetch_with_fallback(cfg)
        write_cache(data)
    except Exception as exc:
        cached = read_cache()
        if cached and cached.get("ok"):
            print("实时拉取失败 : %s" % exc)
            print("已退回到本地缓存")
            data = cached
        else:
            print("读取失败   : %s" % exc)
            return 1

    if cred is not None:
        print("命中令牌   : %s… （来源：%s）" % (cred["fingerprint"][:8], cred["source"]))
        print("用户 UID   : %s" % cred["uid"])
        if cred["exp"]:
            left = cred["exp"] - int(time.time())
            print("令牌有效期 : %s（剩余 %.1f 天）" % (
                time.strftime("%Y-%m-%d %H:%M", time.localtime(cred["exp"])), left / 86400.0))
    print("显示模式   : %s    图标: %s    刷新: %ss" % (cfg.mode, cfg.icon, cfg.refresh))
    print("-" * 46)
    print("剩余积分   : %s / %s（%.1f%%）" % (
        _fmt(data["total_remain"]), _fmt(data["total_capacity"]), data["ratio"] * 100))
    for pkg in sorted(data["packages"], key=lambda p: -p["remain"]):
        print("  · %-16s %s / %s" % (pkg["name"], _fmt(pkg["remain"]), _fmt(pkg["total"])))
    checkin = data.get("checkin")
    if checkin:
        print("签到       : %s，连续 %d 天，本周 %d 天" % (
            "已签到" if checkin["today"] else "未签到", checkin["streak"], checkin["week"]))
    print("更新时间   : %s" % time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(data["updated_at"])))
    return 0


def cmd_fetch(cfg):
    data, cred = fetch_with_fallback(cfg)
    write_cache(data)
    print(json.dumps({"命中令牌": cred["fingerprint"][:8], "来源": cred["source"],
                      "摘要": data}, ensure_ascii=False, indent=2))
    return 0


def cmd_daemon(cfg):
    """常驻进程：定时刷新缓存。带单实例锁。"""
    import fcntl
    ensure_state_dir()
    lock = open(LOCK_FILE, "w")
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        log("已有实例在运行，退出")
        return 0
    lock.write("%d\n" % os.getpid())
    lock.flush()

    log("守护进程启动，间隔 %ss" % cfg.refresh)
    while True:
        try:
            data, cred = fetch_with_fallback(cfg)
            write_cache(data)
            log("刷新成功：剩余 %s / %s（令牌 %s…）" % (
                _fmt(data["total_remain"]), _fmt(data["total_capacity"]),
                cred["fingerprint"][:8]))
        except urllib.error.HTTPError as exc:
            log("刷新失败 HTTP %s：%s" % (exc.code, exc.reason))
        except Exception as exc:
            log("刷新失败：%s" % exc)
        time.sleep(max(30, cfg.refresh))
    return 0


def cmd_checkin(cfg):
    """签到并顺带刷新缓存。stdout 只吐一行 JSON，供 GUI 解析。"""
    result = do_checkin(cfg)
    # 无论成功与否都刷新一次缓存，好让余额立刻反映新领到的积分
    try:
        data, cred = fetch_with_fallback(cfg)
        write_cache(data)
        result["summary"] = {
            "remain": data["total_remain"],
            "capacity": data["total_capacity"],
            "ratio": data["ratio"],
        }
    except Exception as exc:
        result["summary_error"] = str(exc)

    print(json.dumps(result, ensure_ascii=False))
    return 0 if result["status"] in ("ok", "already") else 1


COMMANDS = {
    "genmon": cmd_genmon,
    "detail": cmd_detail,
    "check": cmd_check,
    "fetch": cmd_fetch,
    "checkin": cmd_checkin,
    "daemon": cmd_daemon,
}


def main(argv):
    command = argv[1] if len(argv) > 1 else "check"
    handler = COMMANDS.get(command)
    if handler is None:
        sys.stderr.write("用法: %s {%s}\n" % (os.path.basename(argv[0]), "|".join(COMMANDS)))
        return 2
    return handler(load_config()) or 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))

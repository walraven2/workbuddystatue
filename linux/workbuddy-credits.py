#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
WorkBuddy 积分 —— 桌面小组件（GTK3 / PyGObject）

与桌面环境无关：XFCE / GNOME / KDE / MATE / Cinnamon 都能跑。
不修改任何面板配置，因此不会影响你的桌面布局。

数据来源：~/.workbuddy-status/cache.json
    （由 workbuddy-status daemon 定时刷新，本程序只读缓存 + 按需触发刷新）

用法：
    workbuddy-credits                打开积分窗口
    workbuddy-credits --tray         以托盘图标常驻（左键点击开窗）
    workbuddy-credits --make-icon    生成图标文件后退出
    workbuddy-credits --version
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import threading
import time

try:
    import gi

    # 必须先钉住版本，否则 PyGObject 可能挑中 Gdk 4.0 与 Gtk 3.0 打架
    gi.require_version("Gdk", "3.0")
    gi.require_version("GdkPixbuf", "2.0")
    gi.require_version("Gtk", "3.0")
    from gi.repository import Gdk, GdkPixbuf, GLib, Gtk
except Exception as exc:  # pragma: no cover
    sys.stderr.write(
        "无法加载 GTK3 / PyGObject：%s\n请先安装：sudo apt install -y python3-gi gir1.2-gtk-3.0\n" % exc
    )
    sys.exit(2)

APP_NAME = "WorkBuddy 积分"
APP_ID = "workbuddy-credits"
VERSION = "1.0.0"

HOME = os.path.expanduser("~")
STATE_DIR = os.path.join(HOME, ".workbuddy-status")
CACHE_FILE = os.path.join(STATE_DIR, "cache.json")
CONFIG_FILE = os.path.join(STATE_DIR, "config.json")
ERROR_LOG = os.path.join(STATE_DIR, "last-error.log")
SHOW_FLAG = os.path.join(STATE_DIR, ".show-request")
LOCK_FILE = os.path.join(STATE_DIR, ".credits.lock")

CLI = os.path.join(HOME, ".local", "bin", "workbuddy-status")
ICON_FILE = os.path.join(HOME, ".local", "share", "icons", APP_ID + ".png")

DEFAULT_REFRESH = 300


# --------------------------------------------------------------------------- #
# 工具
# --------------------------------------------------------------------------- #

def load_json(path, default=None):
    try:
        with open(path, "r", encoding="utf-8") as fh:
            return json.load(fh)
    except Exception:
        return default


def read_config():
    cfg = load_json(CONFIG_FILE, {}) or {}
    try:
        cfg["refreshInterval"] = int(cfg.get("refreshInterval") or DEFAULT_REFRESH)
    except Exception:
        cfg["refreshInterval"] = DEFAULT_REFRESH
    cfg.setdefault("icon", "⚡")
    return cfg


def fmt(value):
    """积分格式：整数不带小数点，小数保留两位，长数字加千分位。"""
    try:
        value = float(value)
    except Exception:
        return "-"
    if abs(value - round(value)) < 0.005:
        return "{:,}".format(int(round(value)))
    return "{:,.2f}".format(value)


def fmt_time(ts):
    try:
        return time.strftime("%m-%d %H:%M", time.localtime(int(ts)))
    except Exception:
        return "-"


def ago_text(ts):
    try:
        delta = max(0, int(time.time()) - int(ts))
    except Exception:
        return ""
    if delta < 60:
        return "刚刚更新"
    if delta < 3600:
        return "%d 分钟前更新" % (delta // 60)
    if delta < 86400:
        return "%d 小时前更新" % (delta // 3600)
    return "%d 天前更新" % (delta // 86400)


def level_class(ratio):
    """剩余比例 -> 进度条配色等级。"""
    if ratio <= 0.05:
        return "crit"
    if ratio <= 0.20:
        return "low"
    return "ok"


# --------------------------------------------------------------------------- #
# CSS
# --------------------------------------------------------------------------- #

CSS = b"""
window.credits-win { background-color: #f4f6fb; }

.card {
    background-color: #ffffff;
    border: 1px solid #e3e7ef;
    border-radius: 12px;
}

.app-title   { font-size: 14px; font-weight: 700; color: #1f2937; }
.app-sub     { font-size: 11px; color: #8a93a5; }

.hero-num    { font-size: 38px; font-weight: 700; color: #1b2436; }
.hero-unit   { font-size: 12px; color: #8a93a5; }
.hero-pct    { font-size: 16px; font-weight: 700; }
.hero-pct.ok  { color: #2f7d5d; }
.hero-pct.low { color: #b7791f; }
.hero-pct.crit{ color: #c0392b; }

.pkg-name    { font-size: 12px; font-weight: 600; color: #374151; }
.pkg-vals    { font-size: 11px; color: #6b7280; }

.sect-title  { font-size: 11px; font-weight: 700; color: #8a93a5; }

.checkin-main{ font-size: 13px; font-weight: 600; color: #1f2937; }
.checkin-sub { font-size: 11px; color: #6b7280; }

.foot        { font-size: 11px; color: #8a93a5; }
.err         { font-size: 12px; color: #c0392b; }

button.flat-btn {
    background-image: none;
    background-color: #ffffff;
    border: 1px solid #d6dcea;
    border-radius: 8px;
    padding: 4px 12px;
    color: #3b4358;
    font-size: 12px;
}
button.flat-btn:hover  { background-color: #eef2fb; }
button.flat-btn:active { background-color: #e2e8f7; }

/* checkin button: clickable=accent blue, done=soft green (disabled) */
button.primary-btn {
    background-image: none;
    background-color: #4f7bff;
    border: 1px solid #4f7bff;
    border-radius: 8px;
    padding: 4px 14px;
    color: #ffffff;
    font-size: 12px;
    font-weight: 600;
}
button.primary-btn:hover    { background-color: #3f6ae8; border-color: #3f6ae8; }
button.primary-btn:active   { background-color: #3559c9; border-color: #3559c9; }
button.primary-btn:disabled {
    background-color: #dfe4ef;
    border-color: #dfe4ef;
    color: #9aa3b5;
}
button.primary-btn.done,
button.primary-btn.done:disabled {
    background-color: #eaf7f0;
    border-color: #bfe3d0;
    color: #2f7d5d;
}

.note-ok  { font-size: 11px; font-weight: 600; color: #2f7d5d; }
.note-err { font-size: 11px; font-weight: 600; color: #c0392b; }

progressbar.bar trough {
    min-height: 9px;
    border: none;
    border-radius: 5px;
    background-color: #e8ebf3;
}
progressbar.bar progress {
    min-height: 9px;
    border: none;
    border-radius: 5px;
}
progressbar.bar.ok   progress { background-color: #4f7bff; }
progressbar.bar.low  progress { background-color: #e8a33d; }
progressbar.bar.crit progress { background-color: #d9534f; }

.hero-bar trough   { min-height: 12px; border-radius: 6px; background-color: #e8ebf3; }
.hero-bar progress { min-height: 12px; border-radius: 6px; }
.hero-bar.ok   progress { background-image: linear-gradient(to right, #4f7bff, #7c4dff); }
.hero-bar.low  progress { background-color: #e8a33d; }
.hero-bar.crit progress { background-color: #d9534f; }
"""


def install_css():
    provider = Gtk.CssProvider()
    provider.load_from_data(CSS)
    screen = Gdk.Screen.get_default()
    if screen is not None:
        Gtk.StyleContext.add_provider_for_screen(
            screen, provider, Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION
        )


# --------------------------------------------------------------------------- #
# 抓取
# --------------------------------------------------------------------------- #

def cli_subcommands():
    """向 CLI 要一次用法行，解析出它真正支持哪些子命令。

    该 CLI 无参数时默认执行 check 并打印诊断，不会输出用法，
    所以这里故意给一个非法子命令，逼它把 {a|b|c} 用法行吐到 stderr。
    """
    try:
        proc = subprocess.run(
            [sys.executable, CLI, "__list_commands__"],
            capture_output=True, timeout=15, text=True,
        )
        text = (proc.stdout or "") + (proc.stderr or "")
    except Exception:
        return []
    match = re.search(r"\{([a-z0-9_,|\- ]+)\}", text)
    if not match:
        return []
    return [c.strip() for c in re.split(r"[|,]", match.group(1)) if c.strip()]


def run_refresh(force=False):
    """调用 CLI 抓取一次最新数据。返回 (成功?, 提示信息)。"""
    if not os.path.exists(CLI):
        return False, "找不到命令行工具：%s" % CLI

    subs = cli_subcommands()
    cmd = None
    for name in ("fetch", "refresh", "check"):
        if name in subs:
            cmd = [sys.executable, CLI, name]
            break
    if cmd is None:
        # check 是无参数时的默认命令，必然存在
        cmd = [sys.executable, CLI, "check"]

    try:
        proc = subprocess.run(cmd, capture_output=True, timeout=75, text=True)
    except subprocess.TimeoutExpired:
        return False, "抓取超时（网络较慢？）"
    except Exception as exc:
        return False, "抓取失败：%s" % exc
    if proc.returncode != 0:
        tail = (proc.stderr or proc.stdout or "").strip().splitlines()
        return False, (tail[-1] if tail else "抓取返回码 %d" % proc.returncode)
    return True, ""


def run_checkin():
    """调用 CLI 执行签到。返回 (status, 提示文案)，status ∈ ok|already|error。"""
    if not os.path.exists(CLI):
        return "error", "找不到命令行工具：%s" % CLI

    subs = cli_subcommands()
    if subs and "checkin" not in subs:
        return "error", "命令行工具版本过旧，不支持签到（请重跑 install-credits.sh）"

    try:
        proc = subprocess.run(
            [sys.executable, CLI, "checkin"],
            capture_output=True, timeout=90, text=True,
        )
    except subprocess.TimeoutExpired:
        return "error", "签到超时（网络较慢？）"
    except Exception as exc:
        return "error", "签到失败：%s" % exc

    # CLI 的 stdout 只吐一行 JSON；从后往前找第一条能解析的
    payload = None
    for line in reversed((proc.stdout or "").splitlines()):
        line = line.strip()
        if line.startswith("{"):
            try:
                payload = json.loads(line)
                break
            except ValueError:
                continue

    if payload is None:
        tail = (proc.stderr or proc.stdout or "").strip().splitlines()
        return "error", (tail[-1] if tail else "签到返回码 %d" % proc.returncode)

    status = payload.get("status") or "error"
    if status == "ok":
        msg = "签到成功！+%s 积分" % fmt(payload.get("credit") or 0)
        if payload.get("streak"):
            msg += "（连续 %s 天）" % payload["streak"]
        return "ok", msg
    if status == "already":
        return "already", "今日已签到，无需重复领取"
    return "error", payload.get("message") or "签到失败"


def read_error_log():
    try:
        with open(ERROR_LOG, "r", encoding="utf-8", errors="replace") as fh:
            lines = [ln.strip() for ln in fh if ln.strip()]
        return lines[-1] if lines else ""
    except Exception:
        return ""


def cache_updated_at():
    data = load_json(CACHE_FILE, None)
    if isinstance(data, dict):
        try:
            return int(data.get("updated_at") or 0)
        except Exception:
            return 0
    return 0


def checkin_done_today():
    """缓存里今日是否已签到（CLI 已把本地标记合并进 checkin.today）。"""
    data = load_json(CACHE_FILE, None)
    if not isinstance(data, dict):
        return False
    ci = data.get("checkin") or {}
    return bool(ci.get("today"))


# --------------------------------------------------------------------------- #
# 图标生成
# --------------------------------------------------------------------------- #

def make_icon(path=ICON_FILE):
    """用 PIL 画一个渐变圆角方块 + 白色闪电，作为图标。"""
    try:
        from PIL import Image, ImageDraw
    except Exception:
        return False

    size = 256
    c1, c2 = (79, 123, 255), (124, 77, 255)  # #4F7BFF -> #7C4DFF

    grad = Image.new("RGB", (1, size))
    px = grad.load()
    for y in range(size):
        t = y / float(size - 1)
        px[0, y] = (
            int(c1[0] + (c2[0] - c1[0]) * t),
            int(c1[1] + (c2[1] - c1[1]) * t),
            int(c1[2] + (c2[2] - c1[2]) * t),
        )
    grad = grad.resize((size, size))

    mask = Image.new("L", (size, size), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, size - 1, size - 1], radius=58, fill=255)

    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    img.paste(grad, (0, 0), mask)

    draw = ImageDraw.Draw(img)
    bolt = [
        (0.585, 0.075), (0.275, 0.565), (0.462, 0.565),
        (0.400, 0.925), (0.725, 0.425), (0.532, 0.425),
    ]
    draw.polygon([(x * size, y * size) for x, y in bolt], fill=(255, 255, 255, 255))

    os.makedirs(os.path.dirname(path), exist_ok=True)
    img.save(path, "PNG")
    return True


def load_icon_pixbuf(size=48):
    try:
        return GdkPixbuf.Pixbuf.new_from_file_at_scale(ICON_FILE, size, size, True)
    except Exception:
        return None


# --------------------------------------------------------------------------- #
# 主窗口
# --------------------------------------------------------------------------- #

class CreditsWindow(Gtk.Window):
    def __init__(self, tray_mode=False):
        super().__init__(title=APP_NAME)
        self.tray_mode = tray_mode
        self.cfg = read_config()
        self._busy = False
        # 以当前时间戳为基准：只有"启动之后"新写入的请求才算数，
        # 否则上次留下的 .show-request 会让托盘模式一开机就自己弹窗。
        try:
            self._last_show_mtime = os.path.getmtime(SHOW_FLAG)
        except OSError:
            self._last_show_mtime = 0

        self.set_border_width(14)
        self.set_default_size(400, -1)
        self.set_resizable(False)
        self.get_style_context().add_class("credits-win")

        pix = load_icon_pixbuf(64)
        if pix is not None:
            self.set_icon(pix)

        outer = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=10)
        self.add(outer)

        outer.pack_start(self._build_header(), False, False, 0)
        outer.pack_start(self._build_hero(), False, False, 0)
        outer.pack_start(self._build_packages(), False, False, 0)
        outer.pack_start(self._build_checkin(), False, False, 0)
        outer.pack_start(self._build_footer(), False, False, 0)

        self.connect("delete-event", self._on_delete)
        self.connect("key-press-event", self._on_key)

        self.reload()
        # 缓存里的更新时间 / 倒计时每秒动一下
        GLib.timeout_add_seconds(1, self._tick)
        # 每 20 秒重新读一次缓存（daemon 是后台更新的）
        GLib.timeout_add_seconds(20, self._poll_cache)
        # 监听“再次点击桌面图标”的请求文件
        GLib.timeout_add_seconds(1, self._poll_show_flag)
        # 数据过期就自己抓一次（托盘常驻时也要生效）
        GLib.timeout_add_seconds(30, self._auto_refresh_timer)
        # 按需触发后台刷新
        self._maybe_auto_refresh()

    # ---------------- 界面 ----------------

    def _build_header(self):
        box = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)

        text = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=0)
        title = Gtk.Label(label="%s WorkBuddy 积分" % self.cfg.get("icon", "⚡"))
        title.set_xalign(0)
        title.get_style_context().add_class("app-title")
        self.lbl_token = Gtk.Label(label="")
        self.lbl_token.set_xalign(0)
        self.lbl_token.get_style_context().add_class("app-sub")
        text.pack_start(title, False, False, 0)
        text.pack_start(self.lbl_token, False, False, 0)
        box.pack_start(text, True, True, 0)

        self.btn_refresh = Gtk.Button(label="刷新")
        self.btn_refresh.get_style_context().add_class("flat-btn")
        self.btn_refresh.set_tooltip_text("立即抓取一次最新积分")
        self.btn_refresh.connect("clicked", self._on_refresh_clicked)
        box.pack_end(self.btn_refresh, False, False, 0)

        return box

    def _build_hero(self):
        card = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=2)
        card.get_style_context().add_class("card")
        card.set_border_width(14)

        row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=6)
        self.lbl_remain = Gtk.Label(label="—")
        self.lbl_remain.set_xalign(0)
        self.lbl_remain.get_style_context().add_class("hero-num")
        row.pack_start(self.lbl_remain, False, False, 0)

        unit = Gtk.Label(label="/ — credits")
        unit.set_xalign(0)
        unit.set_valign(Gtk.Align.END)
        self.lbl_total = unit
        unit.get_style_context().add_class("hero-unit")
        row.pack_start(unit, False, False, 0)

        self.lbl_ratio = Gtk.Label(label="—")
        self.lbl_ratio.set_xalign(1)
        self.lbl_ratio.set_valign(Gtk.Align.CENTER)
        self.lbl_ratio.get_style_context().add_class("hero-pct")
        row.pack_end(self.lbl_ratio, False, False, 0)

        self.bar_hero = Gtk.ProgressBar()
        self.bar_hero.get_style_context().add_class("hero-bar")

        card.pack_start(row, False, False, 0)
        card.pack_start(self.bar_hero, False, False, 6)
        return card

    def _build_packages(self):
        card = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=9)
        card.get_style_context().add_class("card")
        card.set_border_width(14)

        head = Gtk.Label(label="积分明细")
        head.set_xalign(0)
        head.get_style_context().add_class("sect-title")
        card.pack_start(head, False, False, 0)

        self.pkg_box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=9)
        card.pack_start(self.pkg_box, False, False, 0)

        self.lbl_no_pkg = Gtk.Label(label="暂无积分包数据")
        self.lbl_no_pkg.set_xalign(0)
        self.lbl_no_pkg.get_style_context().add_class("pkg-vals")
        # 关键：否则 win.show_all() 会把它重新显示出来，和积分包列表同时出现
        self.lbl_no_pkg.set_no_show_all(True)
        card.pack_start(self.lbl_no_pkg, False, False, 0)

        return card

    def _build_checkin(self):
        card = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=3)
        card.get_style_context().add_class("card")
        card.set_border_width(14)

        top = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
        head = Gtk.Label(label="每日签到")
        head.set_xalign(0)
        head.get_style_context().add_class("sect-title")
        top.pack_start(head, True, True, 0)

        self.btn_checkin = Gtk.Button(label="立即签到")
        self.btn_checkin.get_style_context().add_class("primary-btn")
        self.btn_checkin.set_tooltip_text("领取今天的签到积分（每天一次，重复点击安全）")
        self.btn_checkin.connect("clicked", self._on_checkin_clicked)
        top.pack_end(self.btn_checkin, False, False, 0)
        card.pack_start(top, False, False, 0)

        self.lbl_checkin = Gtk.Label(label="—")
        self.lbl_checkin.set_xalign(0)
        self.lbl_checkin.get_style_context().add_class("checkin-main")
        card.pack_start(self.lbl_checkin, False, False, 0)

        self.lbl_checkin_sub = Gtk.Label(label="")
        self.lbl_checkin_sub.set_xalign(0)
        self.lbl_checkin_sub.set_line_wrap(True)
        self.lbl_checkin_sub.get_style_context().add_class("checkin-sub")
        card.pack_start(self.lbl_checkin_sub, False, False, 0)

        # 签到结果反馈行（平时隐藏）
        self.lbl_checkin_note = Gtk.Label(label="")
        self.lbl_checkin_note.set_xalign(0)
        self.lbl_checkin_note.set_line_wrap(True)
        self.lbl_checkin_note.get_style_context().add_class("note-ok")
        self.lbl_checkin_note.set_no_show_all(True)
        card.pack_start(self.lbl_checkin_note, False, False, 0)

        return card

    def _set_checkin_button(self, checked, available=True):
        ctx = self.btn_checkin.get_style_context()
        if not available:
            self.btn_checkin.set_label("不可签")
            self.btn_checkin.set_sensitive(False)
            return
        if checked:
            ctx.add_class("done")
            self.btn_checkin.set_label("今日已签到")
            self.btn_checkin.set_sensitive(False)
        else:
            ctx.remove_class("done")
            self.btn_checkin.set_label("立即签到")
            self.btn_checkin.set_sensitive(not self._busy)

    def _show_note(self, text, ok=True):
        ctx = self.lbl_checkin_note.get_style_context()
        ctx.remove_class("note-ok")
        ctx.remove_class("note-err")
        ctx.add_class("note-ok" if ok else "note-err")
        if text:
            self.lbl_checkin_note.set_text(text)
            self.lbl_checkin_note.show()
        else:
            self.lbl_checkin_note.hide()

    def _build_footer(self):
        box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=2)
        self.lbl_footer = Gtk.Label(label="")
        self.lbl_footer.set_xalign(0)
        self.lbl_footer.get_style_context().add_class("foot")
        self.lbl_err = Gtk.Label(label="")
        self.lbl_err.set_xalign(0)
        self.lbl_err.set_line_wrap(True)
        self.lbl_err.get_style_context().add_class("err")
        box.pack_start(self.lbl_footer, False, False, 0)
        box.pack_start(self.lbl_err, False, False, 0)
        return box

    # ---------------- 数据渲染 ----------------

    def reload(self):
        data = load_json(CACHE_FILE, None)
        self._render(data)

    def _render(self, data):
        if not isinstance(data, dict):
            self.lbl_remain.set_text("—")
            self.lbl_total.set_text("/ — credits")
            self.lbl_ratio.set_text("无数据")
            self.bar_hero.set_fraction(0.0)
            self.lbl_footer.set_text("还没有缓存数据，点「刷新」获取")
            self.lbl_err.set_text(read_error_log())
            return

        total = float(data.get("total_capacity") or 0)
        remain = float(data.get("total_remain") or 0)
        ratio = float(data.get("ratio") or 0) if total else 0.0

        self.lbl_remain.set_text(fmt(remain))
        self.lbl_total.set_text("/ %s credits" % fmt(total))

        lvl = level_class(ratio)
        ctx = self.lbl_ratio.get_style_context()
        for cls in ("ok", "low", "crit"):
            ctx.remove_class(cls)
        ctx.add_class(lvl)
        self.lbl_ratio.set_text("%.1f%%" % (ratio * 100))

        hctx = self.bar_hero.get_style_context()
        for cls in ("ok", "low", "crit"):
            hctx.remove_class(cls)
        hctx.add_class(lvl)
        self.bar_hero.set_fraction(max(0.0, min(1.0, ratio)))

        # ---- 积分包 ----
        for child in self.pkg_box.get_children():
            self.pkg_box.remove(child)

        packages = data.get("packages") or []
        if not packages:
            self.lbl_no_pkg.show()
        else:
            self.lbl_no_pkg.hide()
            for pkg in packages:
                self.pkg_box.pack_start(self._make_pkg_row(pkg), False, False, 0)
        self.pkg_box.show_all()

        # ---- 签到 ----
        ci = data.get("checkin") or {}
        if ci.get("active"):
            checked = bool(ci.get("today"))
            today = "今日已签到 ✅" if checked else "今日未签到"
            self.lbl_checkin.set_text(today)
            bits = []
            if ci.get("daily"):
                bits.append("每日 %s 积分" % fmt(ci.get("daily")))
            bits.append("连续 %s 天" % ci.get("streak", 0))
            bits.append("本周 %s 天" % ci.get("week", 0))
            if ci.get("activity"):
                bits.append("活动：%s" % ci["activity"])
            if ci.get("season"):
                bits.append("赛季 %s" % ci["season"])
            self.lbl_checkin_sub.set_text(" · ".join(bits))
            self._set_checkin_button(checked)
        else:
            self.lbl_checkin.set_text("当前没有签到活动")
            self.lbl_checkin_sub.set_text("")
            self._set_checkin_button(False, available=False)

        # ---- 页脚 ----
        ts = data.get("updated_at")
        self._updated_at = int(ts) if ts else 0
        self.lbl_footer.set_text("更新于 %s（%s）" % (fmt_time(ts), ago_text(ts)))
        # 只在数据确实过期时才显示错误，免得一条历史报错一直挂在界面上
        stale_after = max(600, self.cfg["refreshInterval"] * 2)
        if self._updated_at and (time.time() - self._updated_at) > stale_after:
            self.lbl_err.set_text(read_error_log())
        else:
            self.lbl_err.set_text("")

        # ---- 令牌 ----
        exp = data.get("token_expires_at")
        if exp:
            left = int(exp) - int(time.time())
            if left <= 0:
                self.lbl_token.set_text("登录令牌已过期，请重新登录 CodeBuddy")
            elif left < 86400 * 3:
                self.lbl_token.set_text("登录令牌 %d 小时后过期" % (left // 3600))
            else:
                self.lbl_token.set_text("登录状态正常")
        else:
            self.lbl_token.set_text("")

    def _make_pkg_row(self, pkg):
        box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=3)

        top = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=6)
        name = Gtk.Label(label=str(pkg.get("name") or pkg.get("code") or "积分包"))
        name.set_xalign(0)
        name.get_style_context().add_class("pkg-name")
        top.pack_start(name, True, True, 0)

        total = float(pkg.get("total") or 0)
        remain = float(pkg.get("remain") or 0)
        pct = (remain / total * 100.0) if total else 0.0

        vals = Gtk.Label(label="%s / %s" % (fmt(remain), fmt(total)))
        vals.set_xalign(1)
        vals.get_style_context().add_class("pkg-vals")
        top.pack_end(vals, False, False, 0)

        bar = Gtk.ProgressBar()
        bar.get_style_context().add_class("bar")
        r = (remain / total) if total else 0.0
        bar.get_style_context().add_class(level_class(r))
        bar.set_fraction(max(0.0, min(1.0, r)))

        box.pack_start(top, False, False, 0)
        box.pack_start(bar, False, False, 0)
        box.set_tooltip_text("%s：剩余 %s / 共 %s（%.1f%%）" % (
            pkg.get("name", ""), fmt(remain), fmt(total), pct))
        return box

    # ---------------- 事件 ----------------

    def _on_key(self, _widget, event):
        if event.keyval == Gdk.KEY_Escape:
            self.hide()
            return True
        return False

    def _on_delete(self, *_args):
        if self.tray_mode:
            self.hide()
            return True
        Gtk.main_quit()
        return False

    def _on_refresh_clicked(self, _btn):
        self.do_refresh(manual=True)

    def do_refresh(self, manual=False):
        if self._busy:
            return
        self._busy = True
        self.btn_refresh.set_sensitive(False)
        self.btn_refresh.set_label("刷新中…")
        before = cache_updated_at()

        def worker():
            ok, msg = run_refresh()
            GLib.idle_add(self._on_refresh_done, ok, msg, manual, before)

        threading.Thread(target=worker, daemon=True).start()

    def _on_refresh_done(self, ok, msg, manual, before):
        self._busy = False
        self.btn_refresh.set_sensitive(True)
        self.btn_refresh.set_label("刷新")
        self.reload()
        # 命令行可能返回非 0，但只要缓存时间戳前进了就算成功
        if not ok and cache_updated_at() > before:
            ok, msg = True, ""
        if ok:
            self.lbl_err.set_text("")
        else:
            self.lbl_err.set_text(msg or read_error_log())
        return False

    def _maybe_auto_refresh(self):
        if time.time() - cache_updated_at() > self.cfg["refreshInterval"]:
            self.do_refresh(manual=False)

    # ---------------- 签到 ----------------

    def _on_checkin_clicked(self, _btn):
        self.do_checkin()

    def do_checkin(self):
        if self._busy:
            return
        self._busy = True
        self.btn_refresh.set_sensitive(False)
        self.btn_checkin.set_sensitive(False)
        self.btn_checkin.set_label("签到中…")
        self._show_note("正在签到，请稍候…", ok=True)

        def worker():
            status, msg = run_checkin()
            GLib.idle_add(self._on_checkin_done, status, msg)

        threading.Thread(target=worker, daemon=True).start()

    def _on_checkin_done(self, status, msg):
        self._busy = False
        self.btn_refresh.set_sensitive(True)
        # 先 reload：cmd_checkin 已经顺带刷新过缓存，余额/连续天数都会更新，
        # 按钮状态也由 _render 按最新 checkin.today 决定
        self.reload()
        self._show_note(msg, ok=(status != "error"))
        return False

    def _tick(self):
        if not self.get_visible():
            return True
        ts = getattr(self, "_updated_at", 0)
        if ts:
            self.lbl_footer.set_text("更新于 %s（%s）" % (fmt_time(ts), ago_text(ts)))
        return True

    def _auto_refresh_timer(self):
        # 与窗口是否显示无关：托盘常驻时也要自己盯着数据新鲜度
        self._maybe_auto_refresh()
        return True

    def _poll_cache(self):
        if self.get_visible():
            self.reload()
        return True

    def show_panel(self):
        """弹出窗口（托盘模式下第一次点击时才会真正显示）。"""
        self.show_all()
        self.present()
        self.reload()

    def _poll_show_flag(self):
        try:
            mtime = os.path.getmtime(SHOW_FLAG)
        except OSError:
            return True
        if mtime > self._last_show_mtime:
            self._last_show_mtime = mtime
            self.show_panel()
        return True


# --------------------------------------------------------------------------- #
# 托盘
# --------------------------------------------------------------------------- #

class Tray(object):
    def __init__(self, window):
        self.window = window
        self.icon = Gtk.StatusIcon()
        pix = load_icon_pixbuf(48)
        if pix is not None:
            self.icon.set_from_pixbuf(pix)
        else:
            self.icon.set_from_icon_name("utilities-system-monitor")
        self.icon.set_tooltip_markup(self._tooltip())
        self.icon.set_visible(True)
        self.icon.connect("activate", self._on_activate)
        self.icon.connect("popup-menu", self._on_popup)
        GLib.timeout_add_seconds(20, self._refresh_tooltip)

    def _tooltip(self):
        data = load_json(CACHE_FILE, None)
        if not isinstance(data, dict):
            return "%s\n暂无数据" % APP_NAME
        return "%s\n剩余 %s / %s（%.1f%%）\n点击打开面板" % (
            APP_NAME,
            fmt(data.get("total_remain")),
            fmt(data.get("total_capacity")),
            float(data.get("ratio") or 0) * 100,
        )

    def _refresh_tooltip(self):
        self.icon.set_tooltip_markup(self._tooltip())
        return True

    def _on_activate(self, _icon):
        self.window.show_panel()

    def _on_popup(self, _icon, button, activate_time):
        menu = Gtk.Menu()
        checked = checkin_done_today()
        for label, cb, sens in (
            ("打开积分面板", lambda *_: self.window.show_panel(), True),
            ("立即签到", lambda *_: self._tray_checkin(), not checked),
            ("立即刷新", lambda *_: self.window.do_refresh(manual=True), True),
            (None, None, True),
            ("退出", lambda *_: Gtk.main_quit(), True),
        ):
            if label is None:
                menu.append(Gtk.SeparatorMenuItem())
                continue
            item = Gtk.MenuItem(label=label)
            item.set_sensitive(sens)
            item.connect("activate", cb)
            menu.append(item)
        menu.show_all()
        menu.popup(None, None, None, None, button, activate_time)

    def _tray_checkin(self):
        """托盘点签到：先开面板让用户看到反馈，再触发签到。"""
        self.window.show_panel()
        self.window.do_checkin()


# --------------------------------------------------------------------------- #
# 单实例
# --------------------------------------------------------------------------- #

def request_show():
    """已有实例在跑时，写一个时间戳文件通知它把窗口弹出来。"""
    try:
        os.makedirs(STATE_DIR, exist_ok=True)
        with open(SHOW_FLAG, "w", encoding="utf-8") as fh:
            fh.write(str(time.time()))
        return True
    except Exception:
        return False


def acquire_lock():
    import fcntl

    os.makedirs(STATE_DIR, exist_ok=True)
    fh = open(LOCK_FILE, "w")
    try:
        fcntl.flock(fh, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        return None
    fh.write(str(os.getpid()))
    fh.flush()
    return fh


def main(argv):
    args = set(argv[1:])

    if "--version" in args:
        print("%s %s" % (APP_ID, VERSION))
        return 0

    if "--make-icon" in args:
        ok = make_icon()
        if ok:
            print("图标已生成：%s" % ICON_FILE)
        else:
            print("生成失败：缺少 Pillow（sudo apt install -y python3-pil）")
        return 0 if ok else 1

    if "--help" in args or "-h" in args:
        print(__doc__)
        return 0

    tray_mode = "--tray" in args or "-t" in args

    lock = acquire_lock()
    if lock is None:
        # 已经在运行：通知它弹窗，然后退出
        request_show()
        return 0

    if not os.path.exists(ICON_FILE):
        make_icon()

    install_css()
    win = CreditsWindow(tray_mode=tray_mode)

    if tray_mode:
        win.connect("destroy", lambda *_: Gtk.main_quit())
        tray = Tray(win)  # noqa: F841  持有引用，防止被 GC
        # 托盘模式下不主动显示窗口，点托盘图标才弹出（也避免开机闪一下）
    else:
        win.show_all()
        win.present()

    try:
        Gtk.main()
    finally:
        try:
            lock.close()
        except Exception:
            pass
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))

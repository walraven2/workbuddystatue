#!/bin/bash
#
# WorkBuddy 积分状态栏 —— Linux / XFCE 安装脚本
#
#   ./install.sh              安装（程序本体 + 刷新服务 + 自启；不动面板）
#   ./install.sh --with-panel 额外注册 XFCE genmon 面板插件（默认关闭，见下）
#   ./install.sh --uninstall  卸载（移除面板插件、自启、程序与配置）
#
# 为什么默认不注册面板插件：
#   xfce4-genmon-plugin 4.1.1 在 xfce4-panel 4.18 上不会读取 xfconf 里的 command，
#   面板只显示占位符 "(genmon)XXX"（已用 dbus-monitor 抓包确认：它一次 xfconf 都没读）。
#   想要"状态栏上一直看得见积分"，请用桌面小组件：
#           ./install-credits.sh
#
# 说明：apt 安装 xfce4-genmon-plugin 这一步需要 root，本脚本不代劳，
#       请在运行前先执行：
#           sudo apt-get install -y xfce4-genmon-plugin
#
set -u

SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="$HOME/.local/bin"
BIN="$BIN_DIR/workbuddy-status"
STATE_DIR="$HOME/.workbuddy-status"
AUTOSTART_DIR="$HOME/.config/autostart"
AUTOSTART_FILE="$AUTOSTART_DIR/workbuddy-status-daemon.desktop"
PANEL_XML="$HOME/.config/xfce4/xfconf/xfce-perchannel-xml/xfce4-panel.xml"
PANEL_CHANNEL="xfce4-panel"
PANEL="panel-1"

# 面板插件 id：从 30 往后找第一个没被占用的
find_free_plugin_id() {
    local id=30
    while xfconf-query -c "$PANEL_CHANNEL" -p "/plugins/plugin-$id" >/dev/null 2>&1; do
        id=$((id + 1))
    done
    echo "$id"
}

# ---------------------------------------------------------------------------
# plugin-ids 是一个 xfconf int 数组。
# 注意：xfconf-query 没有"追加到数组"的选项！
#   -a / --create 只是"属性不存在时创建"，对数组而言它会把整个数组覆盖成单个元素。
#   这曾把用户原来的 22 个插件全部清空，务必用下面的函数改数组：先读整份、再整份写回。
# ---------------------------------------------------------------------------

# 读出数组里的所有整数（每行一个，兼容中英文 locale）
panel_ids_get() {
    xfconf-query -c "$PANEL_CHANNEL" -p "/panels/$PANEL/plugin-ids" 2>/dev/null \
        | grep -E '^[0-9]+$' || true
}

# 整份写回数组；为空则删除该属性
panel_ids_set() {
    local ids=("$@")
    if [ ${#ids[@]} -eq 0 ]; then
        xfconf-query -c "$PANEL_CHANNEL" -p "/panels/$PANEL/plugin-ids" -r 2>/dev/null || true
        return 0
    fi
    local args=()
    local id
    for id in "${ids[@]}"; do
        args+=(-t int -s "$id")
    done
    xfconf-query -c "$PANEL_CHANNEL" -p "/panels/$PANEL/plugin-ids" "${args[@]}"
}

# 追加（幂等）
panel_ids_add() {
    local want="$1" id
    local ids=()
    while read -r id; do
        [ -n "$id" ] && ids+=("$id")
    done < <(panel_ids_get)
    for id in "${ids[@]}"; do
        [ "$id" = "$want" ] && { echo "  $PANEL 里已存在插件 $want"; return 0; }
    done
    ids+=("$want")
    panel_ids_set "${ids[@]}"
}

# 移除（幂等）
panel_ids_remove() {
    local want="$1" id
    local ids=()
    while read -r id; do
        [ -n "$id" ] || continue
        [ "$id" = "$want" ] && continue
        ids+=("$id")
    done < <(panel_ids_get)
    panel_ids_set "${ids[@]}"
}

# 从正在运行的 xfce4-panel 进程里取出它所属的 DISPLAY / DBUS 会话
detect_gui_env() {
    local pid
    pid=$(pgrep -x xfce4-panel | head -1)
    if [ -z "$pid" ]; then
        echo "  ! 没找到正在运行的 xfce4-panel，面板插件将无法即时生效" >&2
        return 1
    fi
    DISPLAY_GUI=$(tr '\0' '\n' < "/proc/$pid/environ" 2>/dev/null | sed -n 's/^DISPLAY=//p' | head -1)
    DBUS_GUI=$(tr '\0' '\n' < "/proc/$pid/environ" 2>/dev/null | sed -n 's/^DBUS_SESSION_BUS_ADDRESS=//p' | head -1)
    export DISPLAY="${DISPLAY_GUI:-:10}"
    export DBUS_SESSION_BUS_ADDRESS="$DBUS_GUI"
    echo "  检测到面板会话：DISPLAY=$DISPLAY"
}

restart_panel() {
    if command -v xfce4-panel >/dev/null 2>&1 && pgrep -x xfce4-panel >/dev/null; then
        xfce4-panel --restart >/dev/null 2>&1 &
        echo "  已重启 xfce4-panel"
    fi
}

# ------------------------------------------------------------------ 安装

do_install() {
    echo "==> 1/6 检查依赖"
    if ! python3 -c 'import sqlite3' >/dev/null 2>&1; then
        echo "  ! 缺少 python3 sqlite3 模块" >&2
    fi
    if ! command -v zenity >/dev/null 2>&1; then
        echo "  ! 未装 zenity，点击面板只会在终端打印明细（sudo apt-get install zenity）"
    fi
    HAVE_GENMON=0
    if [ -f /usr/lib/aarch64-linux-gnu/xfce4/panel/plugins/libgenmon.so ] \
       || [ -f /usr/lib/x86_64-linux-gnu/xfce4/panel/plugins/libgenmon.so ]; then
        HAVE_GENMON=1
        echo "  xfce4-genmon-plugin 已安装"
    else
        echo "  ! 未检测到 genmon 插件，请先安装："
        echo "      sudo apt-get install -y xfce4-genmon-plugin"
        echo "    （仍会继续安装程序本体与自启项）"
    fi

    echo "==> 2/6 安装程序到 $BIN"
    mkdir -p "$BIN_DIR" "$STATE_DIR"
    install -m 755 "$SRC_DIR/workbuddy-status.py" "$BIN"

    echo "==> 3/6 写入配置模板"
    if [ ! -f "$STATE_DIR/config.json" ]; then
        cat > "$STATE_DIR/config.json" <<'JSON'
{
  "//": "WorkBuddy 积分状态栏配置。accessToken 留空时自动从 CodeBuddy 登录库读取，通常无需填写。",
  "endpoint": "https://copilot.tencent.com",
  "accessToken": "",
  "userId": "",
  "//refreshInterval": "刷新间隔（秒）",
  "refreshInterval": 300,
  "//icon": "面板上的前缀符号，面板字体渲染不出 ⚡ 时可改成 WB 之类",
  "icon": "⚡",
  "//mode": "credits=显示积分数值 | percent=显示百分比 | icon=只显示图标",
  "mode": "credits"
}
JSON
        echo "  已创建 $STATE_DIR/config.json"
    else
        echo "  配置已存在，保留不动"
    fi

    echo "==> 4/6 配置登录自启"
    mkdir -p "$AUTOSTART_DIR"
    cat > "$AUTOSTART_FILE" <<DESKTOP
[Desktop Entry]
Type=Application
Version=1.0
Name=WorkBuddy 积分状态栏（刷新服务）
Comment=定时拉取 WorkBuddy 积分并写入本地缓存，供面板显示
Exec=$BIN daemon
Terminal=false
NoDisplay=false
Hidden=false
X-GNOME-Autostart-enabled=true
DESKTOP
    echo "  已写入 $AUTOSTART_FILE"

    echo "==> 5/6 注册 XFCE 面板插件"
    if [ "$WITH_PANEL" != "1" ]; then
        echo "  跳过（默认不改动面板配置，避免动到你的桌面布局）"
        echo "  原因：xfce4-genmon-plugin 4.1.1 在 xfce4-panel 4.18 上不读取 xfconf 配置，"
        echo "        面板只会显示占位符 (genmon)XXX。已在用户机器上复现确认。"
        echo "  推荐：改用桌面小组件 ——  ./install-credits.sh"
        echo "  坚持要试面板集成：./install.sh --with-panel"
    elif [ "$HAVE_GENMON" = "1" ] && detect_gui_env; then
        # 备份面板配置
        if [ -f "$PANEL_XML" ]; then
            cp "$PANEL_XML" "$PANEL_XML.bak.$(date +%Y%m%d%H%M%S)"
            echo "  已备份面板配置"
        fi
        PLUGIN_ID=$(find_free_plugin_id)
        echo "  使用插件 id: $PLUGIN_ID"
        xfconf-query -c "$PANEL_CHANNEL" -p "/plugins/plugin-$PLUGIN_ID" -t string -s genmon --create
        xfconf-query -c "$PANEL_CHANNEL" -p "/plugins/plugin-$PLUGIN_ID/command" -t string -s "$BIN genmon" --create
        xfconf-query -c "$PANEL_CHANNEL" -p "/plugins/plugin-$PLUGIN_ID/period" -t uint -s 60 --create
        xfconf-query -c "$PANEL_CHANNEL" -p "/plugins/plugin-$PLUGIN_ID/use-label" -t bool -s false --create
        xfconf-query -c "$PANEL_CHANNEL" -p "/plugins/plugin-$PLUGIN_ID/update-on-click" -t bool -s true --create

        # 加入 panel-1 的 plugin-ids（整份数组写回，绝不用 -a）
        panel_ids_add "$PLUGIN_ID" && echo "  已加入 $PANEL 的插件列表"
        echo "__PLUGIN_ID__=$PLUGIN_ID" > "$STATE_DIR/.panel-plugin-id"
    else
        echo "  跳过（genmon 未安装或找不到面板会话）"
    fi

    echo "==> 6/6 启动刷新服务"
    pkill -f "$BIN daemon" >/dev/null 2>&1
    sleep 1
    if detect_gui_env >/dev/null 2>&1; then
        nohup "$BIN" daemon >/dev/null 2>&1 &
        echo "  已启动守护进程（pid $!）"
    else
        nohup "$BIN" daemon >/dev/null 2>&1 &
        echo "  已启动守护进程（pid $!）"
    fi

    echo
    echo "完成。等首次刷新后，面板上会出现类似 ⚡1723 的积分显示。"
    echo "立即验证：  $BIN check"
    echo "面板没出现时：xfce4-panel --restart"
}

# ------------------------------------------------------------------ 卸载

do_uninstall() {
    echo "==> 卸载 WorkBuddy 积分状态栏"
    pkill -f "$BIN daemon" >/dev/null 2>&1 && echo "  已停止守护进程"

    if [ -f "$STATE_DIR/.panel-plugin-id" ]; then
        # shellcheck disable=SC1090
        . "$STATE_DIR/.panel-plugin-id"
    fi
    PLUGIN_ID="${__PLUGIN_ID__:-}"

    if detect_gui_env >/dev/null 2>&1; then
        if [ -z "$PLUGIN_ID" ]; then
            # 兜底：扫一遍所有 genmon 插件，命令里含 workbuddy-status 的即是
            for id in $(xfconf-query -c "$PANEL_CHANNEL" -lv 2>/dev/null \
                        | sed -n 's#^/plugins/plugin-\([0-9]*\)[[:space:]].*#\1#p' | sort -u); do
                cmd=$(xfconf-query -c "$PANEL_CHANNEL" -p "/plugins/plugin-$id/command" 2>/dev/null)
                case "$cmd" in *workbuddy-status*) PLUGIN_ID="$id" ;; esac
            done
        fi
        if [ -n "$PLUGIN_ID" ]; then
            panel_ids_remove "$PLUGIN_ID"
            xfconf-query -c "$PANEL_CHANNEL" -p "/plugins/plugin-$PLUGIN_ID" -r -R 2>/dev/null
            echo "  已移除面板插件 plugin-$PLUGIN_ID"
            restart_panel
        else
            echo "  没找到对应的面板插件"
        fi
    fi

    rm -f "$AUTOSTART_FILE" && echo "  已移除自启项"
    rm -f "$BIN" && echo "  已删除程序"
    rm -rf "$STATE_DIR" && echo "  已删除配置与缓存"
    echo "完成。"
}

# ------------------------------------------------------------------ 入口

WITH_PANEL=0
case "${1:-install}" in
    --uninstall|uninstall|-u) do_uninstall ;;
    --with-panel)             WITH_PANEL=1; do_install ;;
    *)                        do_install ;;
esac

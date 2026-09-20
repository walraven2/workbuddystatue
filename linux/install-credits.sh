#!/usr/bin/env bash
#
# 安装 / 卸载 WorkBuddy 积分桌面小组件
#
#   ./install-credits.sh              安装（程序 + 桌面图标 + 菜单项 + 托盘自启）
#   ./install-credits.sh --no-desktop 安装，但不在桌面放图标
#   ./install-credits.sh --uninstall  卸载
#
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="$HOME/.local/bin"
APP_DIR="$HOME/.local/share/applications"
ICON_DIR="$HOME/.local/share/icons"
AUTOSTART_DIR="$HOME/.config/autostart"
ICON_FILE="$ICON_DIR/workbuddy-credits.png"
MENU_FILE="$APP_DIR/workbuddy-credits.desktop"
TRAY_FILE="$AUTOSTART_DIR/workbuddy-credits-tray.desktop"
DESKTOP_DIR="$(xdg-user-dir DESKTOP 2>/dev/null || true)"
[ -n "$DESKTOP_DIR" ] || DESKTOP_DIR="$HOME/Desktop"
DESK_FILE="$DESKTOP_DIR/WorkBuddy积分.desktop"

WANT_DESKTOP=1
for arg in "$@"; do
    case "$arg" in
        --no-desktop) WANT_DESKTOP=0 ;;
        --uninstall) WANT_DESKTOP=2 ;;
    esac
done

if [ "$WANT_DESKTOP" = "2" ]; then
    echo "== 卸载 WorkBuddy 积分小组件 =="
    pkill -f "workbuddy-credits" 2>/dev/null || true
    rm -f "$BIN_DIR/workbuddy-credits" "$MENU_FILE" "$TRAY_FILE" "$ICON_FILE" "$DESK_FILE"
    for d in "$HOME/Desktop" "$HOME/桌面"; do
        rm -f "$d/WorkBuddy积分.desktop" 2>/dev/null || true
    done
    echo "已卸载。（缓存与配置仍保留在 ~/.workbuddy-status/）"
    exit 0
fi

echo "== 安装 WorkBuddy 积分小组件 =="
mkdir -p "$BIN_DIR" "$APP_DIR" "$ICON_DIR" "$AUTOSTART_DIR"

echo "1/5  安装程序 -> $BIN_DIR/workbuddy-credits"
install -m 755 "$HERE/workbuddy-credits.py" "$BIN_DIR/workbuddy-credits"

echo "2/5  生成图标"
"$BIN_DIR/workbuddy-credits" --make-icon || echo "     (图标生成失败，将使用系统默认图标)"

echo "3/5  写入菜单项"
cat > "$MENU_FILE" <<EOF
[Desktop Entry]
Type=Application
Version=1.0
Name=WorkBuddy 积分
Name[en]=WorkBuddy Credits
GenericName=积分查看器
Comment=查看 WorkBuddy 剩余积分与每日签到状态
Exec=$BIN_DIR/workbuddy-credits
Icon=$ICON_FILE
Terminal=false
Categories=Utility;Monitor;Office;
Keywords=WorkBuddy;Credits;积分;
StartupNotify=true
EOF
chmod 644 "$MENU_FILE"

if [ "$WANT_DESKTOP" = "1" ]; then
    echo "4/5  放置桌面图标 -> $DESK_FILE"
    mkdir -p "$DESKTOP_DIR"
    sed 's/^Name=/Name=/' "$MENU_FILE" > "$DESK_FILE"
    chmod 755 "$DESK_FILE"
    if command -v gio >/dev/null 2>&1; then
        gio set "$DESK_FILE" metadata::trusted true 2>/dev/null || true
    fi
else
    echo "4/5  跳过桌面图标（--no-desktop）"
fi

echo "5/5  托盘常驻自启（状态栏一直能看到积分数）"
cat > "$TRAY_FILE" <<EOF
[Desktop Entry]
Type=Application
Version=1.0
Name=WorkBuddy 积分托盘
Comment=在系统托盘显示 WorkBuddy 剩余积分
Exec=$BIN_DIR/workbuddy-credits --tray
Icon=$ICON_FILE
Terminal=false
X-GNOME-Autostart-enabled=true
EOF
chmod 644 "$TRAY_FILE"

# 桌面环境刷新一下菜单缓存
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$APP_DIR" 2>/dev/null || true
fi

echo
echo "完成。"
echo "  · 现在就能用：双击桌面上的「WorkBuddy 积分」图标"
echo "  · 或从「应用程序 → 附件/实用工具」里打开"
echo "  · 托盘图标会随登录自动启动；不想要就删掉："
echo "      rm $TRAY_FILE"
echo "  · 卸载：$HERE/install-credits.sh --uninstall"

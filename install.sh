#!/bin/bash
#
# WorkBuddy 积分状态栏小工具 —— 安装脚本
#
# 用法：
#   ./install.sh                 安装当前目录 build/ 下的构建产物
#   ./install.sh <zip 路径>       从发行包 zip 安装
#   ./install.sh --uninstall     卸载（含登录自启）
#
set -euo pipefail

APP_NAME="WorkBuddyStatus"
TARGET="/Applications/$APP_NAME.app"
SRC="${1:-}"

uninstall() {
    echo "==> 卸载"
    if [[ -x "$TARGET/Contents/MacOS/$APP_NAME" ]]; then
        "$TARGET/Contents/MacOS/$APP_NAME" --disable-login >/dev/null 2>&1 || true
    fi
    pkill -f "$APP_NAME.app/Contents/MacOS" 2>/dev/null || true
    sleep 1
    rm -rf "$TARGET" "$HOME/.workbuddy-status"
    echo "    已删除 $TARGET 与 ~/.workbuddy-status"
    exit 0
}

if [[ "$SRC" == "--uninstall" ]]; then
    uninstall
fi

if [[ -z "$SRC" ]]; then
    SRC="$(cd "$(dirname "$0")" && pwd)/build/$APP_NAME.app"
fi

TMP=""
cleanup() {
    if [[ -n "$TMP" && -d "$TMP" ]]; then rm -rf "$TMP"; fi
}
trap cleanup EXIT

if [[ "$SRC" == *.zip ]]; then
    echo "==> 解压发行包：$SRC"
    TMP="$(mktemp -d)"
    ditto -x -k "$SRC" "$TMP"
    SRC="$TMP/$APP_NAME.app"
fi

if [[ ! -d "$SRC" ]]; then
    echo "错误：找不到 $SRC，请先执行 ./build.sh" >&2
    exit 1
fi

echo "==> 停止正在运行的实例"
pkill -f "$APP_NAME.app/Contents/MacOS" 2>/dev/null || true
sleep 1

echo "==> 安装到 /Applications"
rm -rf "$TARGET"
cp -R "$SRC" "$TARGET"

# 下载来的包会带隔离属性，清掉可避免 Gatekeeper 弹「无法验证开发者」
xattr -dr com.apple.quarantine "$TARGET" 2>/dev/null || true

echo "==> 启动应用"
open "$TARGET"
sleep 2

echo
echo "完成。状态栏应该已经出现 ⚡ 图标。"
echo "  登录自启：点击状态栏图标 → 「🚀 登录时启动」"
echo "  诊断命令：\"$TARGET/Contents/MacOS/$APP_NAME\" --check"

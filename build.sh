#!/bin/bash
#
# WorkBuddy 积分状态栏小工具 —— 构建脚本
#
# 用法：
#   ./build.sh            编译并打包成 WorkBuddyStatus.app
#   ./build.sh run        编译后立即运行
#   ./build.sh install    编译后安装到 /Applications 并启动
#   ./build.sh dist       编译并产出发行包 dist/WorkBuddyStatus-<版本>-macos-<架构>.zip
#   ./build.sh clean      清理构建产物
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
APP_NAME="WorkBuddyStatus"
BUILD_DIR="$ROOT/build"
DIST_DIR="$ROOT/dist"
APP_DIR="$BUILD_DIR/$APP_NAME.app"
BIN="$APP_DIR/Contents/MacOS/$APP_NAME"

ACTION="${1:-build}"

if [[ "$ACTION" == "clean" ]]; then
    rm -rf "$BUILD_DIR" "$DIST_DIR"
    echo "已清理 build/ 与 dist/"
    exit 0
fi

if ! command -v swiftc >/dev/null 2>&1; then
    echo "错误：未找到 swiftc。请先安装 Xcode 命令行工具：xcode-select --install" >&2
    exit 1
fi

VERSION="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$ROOT/Resources/Info.plist" 2>/dev/null || echo "0.0.0")"
ARCH="$(uname -m)"

echo "==> 工具链：$(swiftc --version | head -1)"
echo "==> 目标平台：$ARCH / macOS $(sw_vers -productVersion)"
echo "==> 版本：v$VERSION"

rm -rf "$APP_DIR"
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"

echo "==> 编译 Swift 源码"
swiftc \
    -swift-version 5 \
    -O \
    -framework AppKit \
    -framework Foundation \
    -o "$BIN" \
    "$ROOT"/Sources/*.swift

cp "$ROOT/Resources/Info.plist" "$APP_DIR/Contents/Info.plist"
cp "$ROOT/Resources/AppIcon.icns" "$APP_DIR/Contents/Resources/AppIcon.icns"
chmod +x "$BIN"

echo "==> 代码签名（ad-hoc）"
codesign --force --deep --sign - --timestamp=none "$APP_DIR" >/dev/null 2>&1 \
    && echo "    已签名" \
    || echo "    跳过（不影响本地运行）"

/usr/bin/plutil -lint "$APP_DIR/Contents/Info.plist" >/dev/null && echo "    Info.plist 校验通过"

echo "==> 构建完成：$APP_DIR"
du -sh "$APP_DIR" | awk '{print "    体积：" $1}'

case "$ACTION" in
    run)
        echo "==> 启动应用"
        open "$APP_DIR"
        ;;
    install)
        echo "==> 安装到 /Applications"
        rm -rf "/Applications/$APP_NAME.app"
        cp -R "$APP_DIR" "/Applications/$APP_NAME.app"
        echo "==> 启动应用"
        open "/Applications/$APP_NAME.app"
        echo "    提示：开机自启可在状态栏菜单中勾选「🚀 登录时启动」"
        ;;
    dist)
        mkdir -p "$DIST_DIR"
        ZIP="$DIST_DIR/$APP_NAME-$VERSION-macos-$ARCH.zip"
        rm -f "$ZIP"
        echo "==> 打开发行包"
        # ditto 会保留符号链接与扩展属性，比 zip 命令更适合分发 .app
        ditto -c -k --sequesterRsrc --keepParent "$APP_DIR" "$ZIP"
        echo "==> 已生成：$ZIP"
        du -sh "$ZIP" | awk '{print "    体积：" $1}'
        echo "    校验和：$(shasum -a 256 "$ZIP" | awk '{print $1}')"
        ;;
    build)
        echo "==> 运行：open \"$APP_DIR\""
        ;;
    *)
        echo "未知参数：$ACTION（可用：build / run / install / dist / clean）" >&2
        exit 1
        ;;
esac

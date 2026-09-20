#!/usr/bin/env bash
#
# 在 macOS / Linux 上交叉编译出 Windows exe。
#
# 全流程不需要 Windows，也不需要 cgo：程序只用 Go 标准库，
# 所有 Win32 调用都走 syscall.NewLazyDLL，因此交叉编译是完整的。
#
# 用法：
#   ./build-windows.sh              # 生成 dist/WorkBuddyStatus.exe 与 zip
#   ./build-windows.sh --no-zip     # 只要 exe
#   ./build-windows.sh --arm64      # 编译 ARM64 版本
#
set -uo pipefail

cd "$(dirname "$0")" || exit 1

VERSION="$(grep -m1 '"version"' ../Resources/Info.plist 2>/dev/null \
    | sed -n 's/.*<string>\(.*\)<\/string>.*/\1/p')"
VERSION="${VERSION:-1.2.0}"

ARCH="amd64"
DO_ZIP=1
for arg in "$@"; do
    case "$arg" in
        --arm64)  ARCH="arm64" ;;
        --amd64)  ARCH="amd64" ;;
        --no-zip) DO_ZIP=0 ;;
        -h|--help)
            sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
            exit 0 ;;
        *) echo "未知参数：$arg" >&2; exit 1 ;;
    esac
done

# ── 定位 Go 工具链
if [ -x "$HOME/.workbuddy/binaries/go/go/bin/go" ]; then
    export GOROOT="$HOME/.workbuddy/binaries/go/go"
    export GOPATH="$HOME/.workbuddy/binaries/go/gopath"
    export GOCACHE="$HOME/.workbuddy/binaries/go/gocache"
    GO="$GOROOT/bin/go"
elif command -v go >/dev/null 2>&1; then
    GO="$(command -v go)"
else
    echo "找不到 Go 工具链。请安装 Go，或把 GOROOT 指到已有安装。" >&2
    exit 1
fi
export PATH="$(dirname "$GO"):$PATH"

echo "工具链   : $("$GO" version)"
echo "版本号   : $VERSION"
echo "目标平台 : windows/$ARCH"
echo

# ── 1. 生成资源目标文件（图标 + 清单 + 版本信息）
echo "[1/4] 生成资源文件…"
"$GO" run ./tools/mkrsrc \
    -ico Resources/AppIcon.ico \
    -manifest Resources/app.manifest \
    -version "$VERSION" \
    -o rsrc_windows_${ARCH}.syso || exit 1

# ── 2. 格式与静态检查
echo "[2/4] 检查代码…"
UNFORMATTED="$("$GO" fmt ./... 2>/dev/null | grep -v '^$' || true)"
[ -n "$UNFORMATTED" ] && echo "  已格式化：$UNFORMATTED"
GOOS=windows GOARCH="$ARCH" "$GO" vet ./... || exit 1
echo "  vet 通过"

# ── 3. 编译
echo "[3/4] 编译 exe…"
mkdir -p dist
OUT="dist/WorkBuddyStatus.exe"
GOOS=windows GOARCH="$ARCH" CGO_ENABLED=0 "$GO" build \
    -trimpath \
    -ldflags "-s -w -H windowsgui" \
    -o "$OUT" . || exit 1

SIZE="$(wc -c < "$OUT" | tr -d ' ')"
echo "  $OUT  $(printf '%s' "$SIZE" | awk '{printf "%'"'"'d", $1}') 字节"
file "$OUT" | sed 's/^/  /'

# ── 4. 打包
if [ "$DO_ZIP" = "1" ]; then
    echo "[4/4] 打包…"
    ZIP="dist/WorkBuddyStatus-${VERSION}-windows-${ARCH}.zip"
    rm -f "$ZIP"
    (cd dist && zip -q "$(basename "$ZIP")" WorkBuddyStatus.exe) || exit 1
    echo "  $ZIP  $(wc -c < "$ZIP" | tr -d ' ') 字节"
    echo "  sha256: $(shasum -a 256 "$ZIP" | awk '{print $1}')"
else
    echo "[4/4] 跳过打包"
fi

echo
echo "完成。"
echo
echo "注意：Win32 界面层无法在 macOS 上执行验证，只有平台无关的逻辑跑过单元测试。"
echo "      第一次在 Windows 上运行建议先执行："
echo "        WorkBuddyStatus.exe --check"

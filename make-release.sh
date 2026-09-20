#!/bin/bash
#
# 在 GitHub 上创建 / 更新 Release 并上传发行包
#
# 用法：
#   GITHUB_TOKEN=xxx ./make-release.sh
#   ./make-release.sh <token>
#   ./make-release.sh <token> --clean-tag V1.1      # 顺带删掉废弃的 Release + 标签
#
# 特性：
#   - 幂等：Release 已存在则更新名称与说明，不会重复创建
#   - 附件同名时先删除再上传，可反复执行
#   - 版本号从 Resources/Info.plist 读取，避免多处不一致
#
set -euo pipefail

REPO="walraven2/workbuddystatue"
ROOT="$(cd "$(dirname "$0")" && pwd)"
TAG="V1.2"
NOTES="$ROOT/release-notes.md"

TOKEN="${1:-${GITHUB_TOKEN:-}}"
CLEAN_TAG=""
if [[ "${2:-}" == "--clean-tag" && -n "${3:-}" ]]; then
    CLEAN_TAG="$3"
fi

if [[ -z "$TOKEN" ]]; then
    echo "错误：缺少 GitHub token" >&2
    echo "用法：GITHUB_TOKEN=xxx ./make-release.sh" >&2
    exit 1
fi
if [[ ! -f "$NOTES" ]]; then
    NOTES="/tmp/release-notes.md"
fi
if [[ ! -f "$NOTES" ]]; then
    echo "错误：找不到 Release 说明文件 release-notes.md" >&2
    exit 1
fi

VERSION="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$ROOT/Resources/Info.plist" 2>/dev/null || echo "0.0.0")"
ASSET="$ROOT/dist/WorkBuddyStatus-$VERSION-macos-universal.zip"
if [[ ! -f "$ASSET" ]]; then
    echo "错误：找不到发行包 $ASSET，请先执行 ./build.sh dist" >&2
    exit 1
fi

API="https://api.github.com/repos/$REPO"
PYTHON="$(command -v python3 || echo /usr/bin/python3)"

# 小工具：从 JSON 里取字段，避免正则被转义和嵌套 JSON 坑到
jget() {
    "$PYTHON" -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for k in sys.argv[1].split("."):
    if isinstance(d, list):
        d = d[0] if d else {}
    d = d.get(k) if isinstance(d, dict) else None
    if d is None:
        break
print(d if d is not None else "")
' "$1"
}

api() {
    local method="$1" path="$2"
    shift 2
    curl -s -m 120 -X "$method" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Accept: application/vnd.github+json" \
        -H "X-GitHub-Api-Version: 2022-11-28" \
        "$@" "$path"
}

echo "==> 校验 token"
ME="$(api GET https://api.github.com/user)"
LOGIN="$(jget login <<< "$ME")"
if [[ -z "$LOGIN" ]]; then
    echo "token 校验失败：$ME" >&2
    exit 1
fi
echo "    身份：$LOGIN"
echo "    版本：$VERSION   标签：$TAG"

# --- 可选：清理废弃标签对应的 Release ---
if [[ -n "$CLEAN_TAG" ]]; then
    echo "==> 清理旧 Release：$CLEAN_TAG"
    OLD_ID="$(api GET "$API/releases/tags/$CLEAN_TAG" | jget id)"
    if [[ -n "$OLD_ID" ]]; then
        api DELETE "$API/releases/$OLD_ID" >/dev/null && echo "    已删除 Release $OLD_ID"
    else
        echo "    没有名为 $CLEAN_TAG 的 Release，跳过"
    fi
    if api DELETE "$API/git/refs/tags/$CLEAN_TAG" >/dev/null 2>&1; then
        echo "    已删除标签 $CLEAN_TAG"
    else
        echo "    标签 $CLEAN_TAG 不存在或删除失败，跳过"
    fi
fi

# --- 创建或更新 Release ---
BODY="$("$PYTHON" - "$NOTES" "$TAG" "$VERSION" <<'PY'
import json, sys
notes = open(sys.argv[1], encoding='utf-8').read()
print(json.dumps({
    "tag_name": sys.argv[2],
    "name": f"{sys.argv[2]} - WorkBuddy 积分状态栏小工具 v{sys.argv[3]}",
    "body": notes,
    "draft": False,
    "prerelease": False,
}, ensure_ascii=False))
PY
)"

EXISTING="$(api GET "$API/releases/tags/$TAG")"
RELEASE_ID="$(jget id <<< "$EXISTING")"

if [[ -n "$RELEASE_ID" ]]; then
    echo "==> Release $TAG 已存在（ID=$RELEASE_ID），更新名称与说明"
    RESP="$(api PATCH "$API/releases/$RELEASE_ID" -H "Content-Type: application/json" -d "$BODY")"
else
    echo "==> 创建 Release $TAG"
    RESP="$(api POST "$API/releases" -H "Content-Type: application/json" -d "$BODY")"
fi

RELEASE_ID="$(jget id <<< "$RESP")"
HTML_URL="$(jget html_url <<< "$RESP")"
if [[ -z "$RELEASE_ID" ]]; then
    echo "Release 创建/更新失败：$RESP" >&2
    exit 1
fi
echo "    $HTML_URL"

# --- 上传附件（同名先删）---
NAME="$(basename "$ASSET")"
echo "==> 处理附件 $NAME"

EXIST_ASSET_ID="$(api GET "$API/releases/$RELEASE_ID/assets?per_page=100" \
    | "$PYTHON" -c '
import json, sys
name = sys.argv[1]
try:
    assets = json.load(sys.stdin)
except Exception:
    assets = []
for a in assets:
    if a.get("name") == name:
        print(a["id"]); break
' "$NAME")"

if [[ -n "$EXIST_ASSET_ID" ]]; then
    echo "    删除旧附件 ID=$EXIST_ASSET_ID"
    api DELETE "$API/releases/assets/$EXIST_ASSET_ID" >/dev/null
fi

echo "==> 上传（$(du -h "$ASSET" | awk '{print $1}')）"
UP="$(curl -s -m 600 -X POST \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    -H "Content-Type: application/zip" \
    --data-binary "@$ASSET" \
    "https://uploads.github.com/repos/$REPO/releases/$RELEASE_ID/assets?name=$NAME")"

DL="$(jget browser_download_url <<< "$UP")"
if [[ -z "$DL" ]]; then
    echo "上传失败：$UP" >&2
    exit 1
fi
echo "    $DL"
echo
echo "完成：$HTML_URL"

#!/bin/bash
#
# 在 GitHub 上创建 Release 并上传发行包
#
# 用法：
#   GITHUB_TOKEN=xxx ./make-release.sh
#   或  ./make-release.sh <token>
#
set -euo pipefail

REPO="walraven2/workbuddystatue"
TAG="v1.0.0"
VERSION="1.0.0"
ROOT="$(cd "$(dirname "$0")" && pwd)"
ASSET="$ROOT/dist/WorkBuddyStatus-$VERSION-macos-universal.zip"
NOTES="/tmp/release-notes.md"

TOKEN="${1:-${GITHUB_TOKEN:-}}"
if [[ -z "$TOKEN" ]]; then
    echo "错误：缺少 GitHub token" >&2
    exit 1
fi
if [[ ! -f "$ASSET" ]]; then
    echo "错误：找不到发行包 $ASSET，请先执行 ./build.sh dist" >&2
    exit 1
fi

API="https://api.github.com/repos/$REPO"
AUTH=(-H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28")

echo "==> 校验 token 与仓库权限"
ME="$(curl -s -m 20 "${AUTH[@]}" https://api.github.com/user)"
if ! grep -q '"login"' <<< "$ME"; then
    echo "token 校验失败：$ME" >&2
    exit 1
fi
echo "    身份：$(sed -n 's/.*"login": *"\([^"]*\)".*/\1/p' <<< "$ME" | head -1)"

echo "==> 创建 Release $TAG"
# 用 python 安全地构造 JSON，避免中文与换行转义问题
PYTHON="$(command -v python3 || echo /usr/bin/python3)"
BODY="$("$PYTHON" - "$NOTES" "$TAG" <<'PY'
import json, sys
notes = open(sys.argv[1], encoding='utf-8').read()
print(json.dumps({
    "tag_name": sys.argv[2],
    "name": "v1.0.0 — WorkBuddy 积分状态栏小工具",
    "body": notes,
    "draft": False,
    "prerelease": False,
}, ensure_ascii=False))
PY
)"

RESP="$(curl -s -m 60 -X POST "${AUTH[@]}" \
    -H "Content-Type: application/json" \
    -d "$BODY" "$API/releases")"

RELEASE_ID="$(sed -n 's/.*"id": *\([0-9]*\),.*/\1/p' <<< "$RESP" | head -1)"
HTML_URL="$(sed -n 's/.*"html_url": *"\([^"]*\)".*/\1/p' <<< "$RESP" | head -1)"

if [[ -z "$RELEASE_ID" ]]; then
    echo "创建 Release 失败：$RESP" >&2
    exit 1
fi
echo "    Release ID=$RELEASE_ID"
echo "    $HTML_URL"

echo "==> 上传发行包（$(du -h "$ASSET" | awk '{print $1}')）"
NAME="$(basename "$ASSET")"
UP="$(curl -s -m 300 -X POST "${AUTH[@]}" \
    -H "Content-Type: application/zip" \
    --data-binary "@$ASSET" \
    "https://uploads.github.com/repos/$REPO/releases/$RELEASE_ID/assets?name=$NAME")"

DL="$(sed -n 's/.*"browser_download_url": *"\([^"]*\)".*/\1/p' <<< "$UP" | head -1)"
if [[ -z "$DL" ]]; then
    echo "上传失败：$UP" >&2
    exit 1
fi
echo "    $DL"
echo
echo "完成：$HTML_URL"

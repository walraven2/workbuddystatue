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
#   - 清理旧标签是「尽力而为」，失败不影响主流程
#
set -uo pipefail

REPO="walraven2/workbuddystatue"
ROOT="$(cd "$(dirname "$0")" && pwd)"
TAG="V1.2"
NOTES="$ROOT/release-notes.md"

# 参数解析：token 可来自 $GITHUB_TOKEN 或第一个位置参数；
# --clean-tag <TAG> 可在任意位置出现
TOKEN="${GITHUB_TOKEN:-}"
CLEAN_TAG=""
POSITIONAL=()
_i=1
while [[ $_i -le $# ]]; do
    _a="${!_i}"
    if [[ "$_a" == "--clean-tag" ]]; then
        _next=$((_i + 1))
        CLEAN_TAG="${!_next:-}"
        _i=$((_i + 2))
    else
        POSITIONAL+=("$_a")
        _i=$((_i + 1))
    fi
done
if [[ -z "$TOKEN" && ${#POSITIONAL[@]} -gt 0 ]]; then
    TOKEN="${POSITIONAL[0]}"
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

STATUS=""
# 调用 API：正文写 stdout，HTTP 状态码写入 $STATUS
api() {
    local method="$1" path="$2"
    shift 2
    local out
    out="$(curl -s -m 600 -w $'\n__HTTP__%{http_code}' -X "$method" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Accept: application/vnd.github+json" \
        -H "X-GitHub-Api-Version: 2022-11-28" \
        "$@" "$path" 2>/dev/null)"
    STATUS="${out##*__HTTP__}"
    printf '%s' "${out%$'\n'__HTTP__*}"
}

ok() { [[ "$STATUS" =~ ^2 ]]; }

# 从 JSON 取字段
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

echo "==> 校验 token"
ME="$(api GET https://api.github.com/user)"
LOGIN="$(jget login <<< "$ME")"
if [[ -z "$LOGIN" ]]; then
    echo "token 校验失败（HTTP $STATUS）：$ME" >&2
    exit 1
fi
echo "    身份：$LOGIN"
echo "    版本：$VERSION   标签：$TAG"

# --- 可选：清理废弃标签对应的 Release（失败不阻断）---
if [[ -n "$CLEAN_TAG" ]]; then
    echo "==> 清理旧 Release：$CLEAN_TAG"
    OLD_ID="$(api GET "$API/releases/tags/$CLEAN_TAG" | jget id)"
    if [[ -n "$OLD_ID" ]]; then
        api DELETE "$API/releases/$OLD_ID" >/dev/null
        if ok; then echo "    已删除 Release ID=$OLD_ID"; else echo "    删除 Release 失败（HTTP $STATUS），跳过"; fi
    else
        echo "    没有名为 $CLEAN_TAG 的 Release，跳过"
    fi
    api DELETE "$API/git/refs/tags/$CLEAN_TAG" >/dev/null
    if ok; then echo "    已删除标签 $CLEAN_TAG"; else echo "    标签 $CLEAN_TAG 不存在（HTTP $STATUS），跳过"; fi
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
    echo "Release 创建/更新失败（HTTP $STATUS）：$RESP" >&2
    exit 1
fi
echo "    $HTML_URL"

# --- 上传附件（同名先删）---
NAME="$(basename "$ASSET")"
LOCAL_SIZE="$(stat -f%z "$ASSET")"
echo "==> 处理附件 $NAME（$(( LOCAL_SIZE / 1024 / 1024 )) MB）"

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
    echo "    删除同名旧附件 ID=$EXIST_ASSET_ID"
    api DELETE "$API/releases/assets/$EXIST_ASSET_ID" >/dev/null
fi

echo "==> 上传中…"
UP="$(curl -s -m 600 -w $'\n__HTTP__%{http_code}' -X POST \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    -H "Content-Type: application/zip" \
    --data-binary "@$ASSET" \
    "https://uploads.github.com/repos/$REPO/releases/$RELEASE_ID/assets?name=$NAME" 2>/dev/null)"
STATUS="${UP##*__HTTP__}"
UP="${UP%$'\n'__HTTP__*}"

DL="$(jget browser_download_url <<< "$UP")"
REMOTE_SIZE="$(jget size <<< "$UP")"
if [[ -z "$DL" ]]; then
    echo "上传失败（HTTP $STATUS）：$UP" >&2
    exit 1
fi

echo "    $DL"
if [[ "$REMOTE_SIZE" == "$LOCAL_SIZE" ]]; then
    echo "    大小校验通过：$LOCAL_SIZE 字节"
else
    echo "    注意：远端 $REMOTE_SIZE 字节 vs 本地 $LOCAL_SIZE 字节" >&2
fi
echo
echo "完成：$HTML_URL"

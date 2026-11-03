#!/bin/sh
# 在 CI 里创建 Gitea Release 并上传发布包。
#
# 为什么不用 softprops/action-gh-release：那个 action 调 api.github.com，
# Gitea 上跑不通。Gitea 的 Release API 形似但不同：附件走 multipart 的
# `attachment` 字段（不是 GitHub 的 upload_url + raw body）。
#
# 由 .gitea/workflows/release.yml 调用：
#   GITEA_TOKEN=xxx GITEA_API=http://gitea:3000/api/v1 \
#   GITEA_REPO=owner/repo ./scripts/ci-make-release.sh v1.1.3 [附件目录]
#
# 幂等：Release 已存在则复用；同名附件先删后传。
set -eu

VERSION="${1:-}"
[ -n "$VERSION" ] || { echo "用法：$0 <tag> [附件目录]" >&2; exit 1; }

: "${GITEA_TOKEN:?需要 GITEA_TOKEN}"
: "${GITEA_API:?需要 GITEA_API}"
: "${GITEA_REPO:?需要 GITEA_REPO}"

DIST="${2:-dist-release}"
[ -d "$DIST" ] || { echo "错误：找不到 $DIST，先跑 scripts/build-release.sh" >&2; exit 1; }

API="$GITEA_API/repos/$GITEA_REPO"
AUTH="Authorization: token $GITEA_TOKEN"
NUM="${VERSION#v}"

# ---------- 1. Release 正文：优先取 CHANGELOG 里该版本的一节 ----------
BODY_FILE=$(mktemp)
PAYLOAD=$(mktemp)
trap 'rm -f "$BODY_FILE" "$PAYLOAD"' EXIT

python3 - "$NUM" "$BODY_FILE" <<'PY'
import os, re, sys
num, out = sys.argv[1], sys.argv[2]
body = ''
if os.path.exists('CHANGELOG.md'):
    text = open('CHANGELOG.md', encoding='utf-8').read()
    m = re.search(r'^##\s*\[?v?' + re.escape(num) + r'\]?.*?$', text, re.M)
    if m:
        rest = text[m.end():]
        nxt = re.search(r'^##\s', rest, re.M)
        body = (rest[:nxt.start()] if nxt else rest).strip()
if not body:
    body = f'版本 {num} 发布，详见仓库 CHANGELOG.md。'
hint = os.environ.get('IMAGE_HINT', '')
if hint:
    body += f'\n\n---\n\n**容器镜像**（本地 Gitea）：\n```\ndocker pull {hint}:{num}\n```\n'
open(out, 'w', encoding='utf-8').write(body + '\n')
print(f'Release 正文 {len(body)} 字符')
PY

python3 - "$VERSION" "$BODY_FILE" > "$PAYLOAD" <<'PY'
import json, sys
version, body_file = sys.argv[1], sys.argv[2]
print(json.dumps({
    'tag_name': version,
    'name': version,
    'body': open(body_file, encoding='utf-8').read(),
    'draft': False,
    'prerelease': False,
}, ensure_ascii=False))
PY

# ---------- 2. 创建（或复用）Release ----------
echo "== 创建 Gitea Release $VERSION =="
HTTP_CODE=$(curl -sS -o /tmp/_rel.json -w '%{http_code}' \
    -X POST "$API/releases" \
    -H "$AUTH" -H 'Content-Type: application/json' \
    --data-binary @"$PAYLOAD")

RELEASE_ID=''
if [ "$HTTP_CODE" = "201" ] || [ "$HTTP_CODE" = "200" ]; then
    RELEASE_ID=$(python3 -c "import json;print(json.load(open('/tmp/_rel.json')).get('id',''))")
    echo "已创建 Release id=$RELEASE_ID"
else
    echo "创建返回 HTTP $HTTP_CODE，按「已存在」处理"
    RELEASE_ID=$(curl -sS "$API/releases/tags/$VERSION" -H "$AUTH" \
        | python3 -c "import json,sys;print(json.load(sys.stdin).get('id',''))" 2>/dev/null || true)
    [ -n "$RELEASE_ID" ] || { echo "错误：Release 既没建成也没查到：" >&2; cat /tmp/_rel.json >&2; exit 1; }
    echo "复用已有 Release id=$RELEASE_ID"
fi

# ---------- 3. 上传附件（同名先删，保证幂等）----------
echo "== 上传发布包 =="
EXISTING=$(curl -sS "$API/releases/$RELEASE_ID/assets" -H "$AUTH" \
    | python3 -c "import json,sys;print(' '.join(str(a['id'])+'\t'+a['name'] for a in json.load(sys.stdin)))" 2>/dev/null || echo "")

for f in "$DIST"/*; do
    [ -f "$f" ] || continue
    name=$(basename "$f")

    echo "$EXISTING" | while IFS="$(printf '\t')" read -r aid aname; do
        [ "$aname" = "$name" ] || continue
        echo "-- 删旧附件 $name (id=$aid)"
        curl -sS -o /dev/null -X DELETE "$API/releases/$RELEASE_ID/assets/$aid" -H "$AUTH" || true
    done

    echo "-- 上传 $name ($(wc -c < "$f") 字节)"
    code=$(curl -sS -o /tmp/_asset.json -w '%{http_code}' \
        -X POST "$API/releases/$RELEASE_ID/assets?name=$name" \
        -H "$AUTH" -F "attachment=@$f")
    case "$code" in
        200|201) echo "   ok" ;;
        *) echo "   失败 HTTP $code" >&2; cat /tmp/_asset.json >&2; exit 1 ;;
    esac
done

echo "== Release 完成：${GITEA_API%/api/v1}/$GITEA_REPO/releases/tag/$VERSION =="

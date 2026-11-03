#!/bin/sh
# 在 Gitea 上创建 Release 并上传发布包。
#
# 为什么不用 softprops/action-gh-release：
#   那个 action 调的是 api.github.com，Gitea 上跑不通。
#   Gitea 的 Release API 与 GitHub 高度相似但不完全相同，
#   附件走 multipart 的 `attachment` 字段（不是 GitHub 的 upload_url + raw body）。
#
# 用法（CI 里由 workflow 调用）：
#   GITEA_TOKEN=xxx GITEA_API=http://host/api/v1 GITEA_REPO=owner/repo \
#     ./scripts/release-gitea.sh v1.1.3 [附件目录，默认 dist-release]
#
# 幂等：Release 已存在时复用，附件同名先删后传。
set -eu

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
    echo "用法：$0 <version，例如 v1.1.3>" >&2
    exit 1
fi

: "${GITEA_TOKEN:?需要 GITEA_TOKEN}"
: "${GITEA_API:?需要 GITEA_API，例如 http://127.0.0.1:3000/api/v1}"
: "${GITEA_REPO:?需要 GITEA_REPO，例如 st1341815/uart_sms_gateway}"

DIST="${2:-dist-release}"
if [ ! -d "$DIST" ]; then
    echo "错误：找不到 $DIST 目录，请先跑 scripts/build-release.sh" >&2
    exit 1
fi

API="$GITEA_API/repos/$GITEA_REPO"
AUTH="Authorization: token $GITEA_TOKEN"

# ---------- 1. Release 正文 ----------
# 优先取 CHANGELOG 里该版本的一节，取不到就用一句通用描述。
BODY_FILE=$(mktemp)
trap 'rm -f "$BODY_FILE"' EXIT

python3 - "$VERSION" "$BODY_FILE" <<'PY'
import os, re, sys

version, out = sys.argv[1], sys.argv[2]
num = version.lstrip('v')
body = ''
if os.path.exists('CHANGELOG.md'):
    text = open('CHANGELOG.md', encoding='utf-8').read()
    # 匹配 "## [1.1.3]" / "## 1.1.3" / "## v1.1.3" 开头的节，截到下一个同级标题
    pat = re.compile(r'^##\s*\[?v?' + re.escape(num) + r'\]?.*?$', re.M)
    m = pat.search(text)
    if m:
        rest = text[m.end():]
        nxt = re.search(r'^##\s', rest, re.M)
        body = (rest[:nxt.start()] if nxt else rest).strip()

if not body:
    body = f'版本 {num} 发布。详见仓库 CHANGELOG.md。'

# 附上镜像地址与发布包清单，方便使用者直接复制
body += '\n\n---\n\n**容器镜像**（本地 Gitea）：\n```\ndocker pull ' + \
        os.environ.get('IMAGE_HINT', '<gitea-host>/<owner>/<repo>') + ':' + num + '\n```\n'

open(out, 'w', encoding='utf-8').write(body + '\n')
print(f'Release 正文 {len(body)} 字符')
PY

# ---------- 2. 创建（或复用）Release ----------
echo "== 创建 Gitea Release $VERSION =="
# 用 python 生成合法 JSON —— Release 正文是多行 markdown，手拼 JSON 一定会坏在转义上
python3 - "$VERSION" "$BODY_FILE" > /tmp/_rel_payload.json <<'PY'
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

HTTP_CODE=$(curl -sS -o /tmp/_rel.json -w '%{http_code}' \
    -X POST "$API/releases" \
    -H "$AUTH" -H 'Content-Type: application/json' \
    --data-binary @/tmp/_rel_payload.json)

RELEASE_ID=''
if [ "$HTTP_CODE" = "201" ] || [ "$HTTP_CODE" = "200" ]; then
    RELEASE_ID=$(python3 -c "import json;print(json.load(open('/tmp/_rel.json')).get('id',''))")
    echo "已创建 Release id=$RELEASE_ID"
else
    echo "创建返回 HTTP $HTTP_CODE，尝试按已存在处理"
    RELEASE_ID=$(curl -sS "$API/releases/tags/$VERSION" -H "$AUTH" \
        | python3 -c "import json,sys;d=json.load(sys.stdin);print(d.get('id',''))" 2>/dev/null || true)
    if [ -z "$RELEASE_ID" ]; then
        echo "错误：Release 既没建成也没查到，响应如下：" >&2
        cat /tmp/_rel.json >&2
        exit 1
    fi
    echo "复用已有 Release id=$RELEASE_ID"
fi

# ---------- 3. 上传附件（同名先删，保证幂等）----------
echo "== 上传发布包 =="
EXISTING=$(curl -sS "$API/releases/$RELEASE_ID/assets" -H "$AUTH" \
    | python3 -c "import json,sys;print(' '.join(str(a['id'])+':'+a['name'] for a in json.load(sys.stdin)))" 2>/dev/null || echo "")

for f in "$DIST"/*; do
    [ -f "$f" ] || continue
    name=$(basename "$f")

    for pair in $EXISTING; do
        aid="${pair%%:*}"
        aname="${pair#*:}"
        if [ "$aname" = "$name" ]; then
            echo "-- 删除旧附件 $name (id=$aid)"
            curl -sS -o /dev/null -X DELETE "$API/releases/$RELEASE_ID/assets/$aid" -H "$AUTH"
        fi
    done

    echo "-- 上传 $name ($(wc -c < "$f") 字节)"
    code=$(curl -sS -o /tmp/_asset.json -w '%{http_code}' \
        -X POST "$API/releases/$RELEASE_ID/assets?name=$name" \
        -H "$AUTH" \
        -F "attachment=@$f")
    if [ "$code" = "201" ] || [ "$code" = "200" ]; then
        echo "   ok"
    else
        echo "   失败 HTTP $code" >&2
        cat /tmp/_asset.json >&2
        exit 1
    fi
done

echo "== Release 完成 =="
echo "查看：${GITEA_API%/api/v1}/$GITEA_REPO/releases"

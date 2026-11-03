#!/bin/sh
# 一键发版到本地 Gitea。
#
# 做的事（顺序即依赖）：
#   1. 校验工作区干净 + 版本号三处一致（web/src/lib/version.ts / internal/version/version.go / CHANGELOG.md）
#   2. 推 master
#   3. 推 tag vX.Y.Z（Gitea Actions 借此触发 release 流水线）
#   4. 轮询该 tag 的 workflow run，等它跑完（构建镜像 + 发布包 + 建 Release）
#   5. 打印镜像地址、Release 链接、附件清单
#
# 用法：
#   GITEA_TOKEN=xxx ./scripts/release-gitea.sh [tag] 
#
#   tag 省略时取 scripts/check_version.py --print（即当前版本号）→ v<version>
#
# 前置（只需做一次，见 docs/gitea-release.md）：
#   * Gitea 已开启 Actions：[actions] ENABLED = true
#   * act_runner 已注册且在线
#   * git remote 里有 gitea（脚本会自动补）
#
# 退出码：0 成功；非 0 = 哪一步挂了（每步都有明确提示）
set -eu

# ---------- 0. 参数与校验 ----------
GITEA_REPO_SLUG="${GITEA_REPO_SLUG:-st1341815/uart_sms_gateway}"
GITEA_URL="${GITEA_URL:-http://127.0.0.1:3000}"
REMOTE="${GITEA_REMOTE:-gitea}"

: "${GITEA_TOKEN:?请先设置 GITEA_TOKEN（Gitea 设置 → 应用 → 生成令牌，权限勾选 repo）}"

cd "$(cd "$(dirname "$0")/.." && pwd)"

if [ ! -d .git ]; then
    echo "错误：不在 git 仓库根目录" >&2
    exit 1
fi

if [ -n "$(git status --porcelain)" ]; then
    echo "错误：工作区有未提交改动，先提交再发版" >&2
    git status --short >&2
    exit 1
fi

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
    RAW=$(python3 scripts/check_version.py --print)
    case "$RAW" in
        v*) VERSION="$RAW" ;;
        *)  VERSION="v$RAW" ;;
    esac
fi
NUM="${VERSION#v}"

echo "== 发版 $VERSION =="

echo "-- 版本号一致性"
python3 scripts/check_version.py

echo "-- 补 gitea remote（已存在则跳过）"
if ! git remote get-url "$REMOTE" >/dev/null 2>&1; then
    git remote add "$REMOTE" "${GITEA_URL}/${GITEA_REPO_SLUG}.git"
fi

# token 走 URL，但只在本进程的临时 remote 里用，不写进 .git/config
PUSH_URL=$(printf '%s' "${GITEA_URL}/${GITEA_REPO_SLUG}.git" | sed "s|://|://${GITEA_REPO_SLUG%%/*}:${GITEA_TOKEN}@|")

echo "-- 推 master"
GIT_TERMINAL_PROMPT=0 git push "$PUSH_URL" "HEAD:refs/heads/master"

echo "-- 推 tag $VERSION"
# tag 必须落在 HEAD 上，否则流水线会构建旧代码
if git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null; then
    TAG_COMMIT=$(git rev-parse "$VERSION^{commit}")
    HEAD_COMMIT=$(git rev-parse HEAD)
    if [ "$TAG_COMMIT" != "$HEAD_COMMIT" ]; then
        echo "错误：tag $VERSION 指向 $TAG_COMMIT，但 HEAD 是 $HEAD_COMMIT" >&2
        echo "      先 git tag -d $VERSION && git tag -a $VERSION -m '...' 重建" >&2
        exit 1
    fi
else
    git tag -a "$VERSION" -m "$VERSION"
fi
GIT_TERMINAL_PROMPT=0 git push "$PUSH_URL" "$VERSION"

# ---------- 等流水线 ----------
API="${GITEA_URL}/api/v1/repos/${GITEA_REPO_SLUG}"
AUTH="Authorization: token ${GITEA_TOKEN}"

echo "-- 等待 Actions 流水线"
RUN_ID=''
i=1
while [ "$i" -le 30 ]; do
    RUN_ID=$(curl -sS "$API/actions/runs?limit=10" -H "$AUTH" \
        | python3 -c "
import json,sys
tag='$VERSION'
try:
    d=json.load(sys.stdin)
except Exception:
    sys.exit(0)
for r in d.get('workflow_runs', []):
    if (r.get('head_branch') or '') == tag:
        print(r['id']); break
" 2>/dev/null || true)
    [ -n "$RUN_ID" ] && break
    sleep 2
    i=$((i + 1))
done

if [ -z "$RUN_ID" ]; then
    echo "警告：没找到 $VERSION 的流水线 run（Actions 可能没开或 runner 离线）" >&2
    echo "      请到 ${GITEA_URL}/${GITEA_REPO_SLUG}/actions 查看" >&2
    exit 2
fi

echo "   run id = $RUN_ID（${GITEA_URL}/${GITEA_REPO_SLUG}/actions/runs/$RUN_ID）"
i=1
while [ "$i" -le 150 ]; do
    STATUS=$(curl -sS "$API/actions/runs/$RUN_ID" -H "$AUTH" \
        | python3 -c "import json,sys;print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
    case "$STATUS" in
        completed|failure|failed|success|cancelled) break ;;
    esac
    printf '.'
    sleep 4
    i=$((i + 1))
done
echo

CONCL=$(curl -sS "$API/actions/runs/$RUN_ID" -H "$AUTH" \
    | python3 -c "import json,sys;d=json.load(sys.stdin);print(d.get('conclusion') or d.get('status') or '')" 2>/dev/null || echo "")

case "$CONCL" in
    success)
        echo "== 流水线成功 ==" ;;
    *)
        echo "== 流水线未成功（$CONCL），日志： ==" >&2
        echo "   ${GITEA_URL}/${GITEA_REPO_SLUG}/actions/runs/$RUN_ID" >&2
        curl -sS "$API/actions/runs/$RUN_ID/logs" -H "$AUTH" -o /tmp/_rel_logs.zip || true
        [ -s /tmp/_rel_logs.zip ] && unzip -oq /tmp/_rel_logs.zip -d /tmp/_rel_logs 2>/dev/null && \
            tail -40 /tmp/_rel_logs/*.log 2>/dev/null | sed 's/^/   /' >&2 || true
        exit 3 ;;
esac

# ---------- 汇总 ----------
echo
echo "镜像（本机 Gitea registry）："
echo "  docker pull ${GITEA_URL#http*://}/${GITEA_REPO_SLUG}:${NUM}"
echo "  docker pull ${GITEA_URL#http*://}/${GITEA_REPO_SLUG}:latest"
echo
echo "Release："
echo "  ${GITEA_URL}/${GITEA_REPO_SLUG}/releases/tag/${VERSION}"
echo
echo "附件："
curl -sS "$API/releases/tags/$VERSION" -H "$AUTH" \
    | python3 -c "
import json,sys
try:
    d=json.load(sys.stdin)
except Exception:
    sys.exit(0)
for a in d.get('assets', []):
    print(f\"  {a['name']}  {a['size']} 字节\")
" 2>/dev/null || echo "  （Release 详情查询失败，去网页看）"

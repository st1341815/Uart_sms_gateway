#!/usr/bin/env bash
#
# 标准发版流程：构建 → 部署 → 验证 → 打 tag → 推送 → 建 Release
#
# 为什么需要这个脚本：
#   这套动作原先全靠手敲。一次完整发版要十几条 ssh / docker / git 命令，
#   顺序错一步就翻车（镜像 tag 打错、compose 没备份、tag 打错 commit、
#   Release 正文忘了写……）。v1.4.0 那次光「构建慢」就被误判成「任务卡住」，
#   因为中间没有阶段化的进度输出。
#
# 用法：
#   ./scripts/release.sh                       # 全流程（版本取当前版本号）
#   ./scripts/release.sh --version 1.4.2       # 指定版本
#   ./scripts/release.sh --skip-deploy         # 只构建镜像，不碰线上
#   ./scripts/release.sh --skip-publish        # 构建+部署，不推 GitHub
#   ./scripts/release.sh --skip-binaries       # 发布，但不生成/上传二进制附件
#   ./scripts/release.sh --dry-run             # 只打印将要做什么
#   ./scripts/release.sh --commit "msg"        # 有未提交改动时自动提交
#
# 必需环境：
#   - 本机：git、python3、node/npm（构建前端）、ssh、tar
#   - 远程构建/部署机：docker（含 compose 插件）
#   - GITHUB_TOKEN：仅发布阶段需要（推送 + 建 Release），不落盘
#
# 可覆盖的变量（环境变量）：
#   REMOTE_HOST     默认 s21               ssh config 别名或 user@ip
#   SSH_CONFIG      默认 $HOME/.ssh/config（存在则用）     需要 -F 指定配置时填
#   REMOTE_DIR      默认 /home/unit3/sms-multi   线上部署目录
#   CONTAINER       默认 sms-multi         容器名
#   IMAGE_REPO      默认 smstext-multi-device    镜像仓库名
#   GITHUB_REPO     默认 st1341815/Uart_sms_gateway
#   HEALTH_URL      默认 http://<REMOTE_HOST>:8080/health（自动解析成真实 IP）
#   PUSH_MODE       默认 auto              推送方式：auto/ssh/https
#                                          auto = remote 是 ssh:// 且配置了 SSH_CONFIG 就用 ssh
#
set -euo pipefail

# ---------- 配置 ----------
REMOTE_HOST="${REMOTE_HOST:-s21}"
SSH_CONFIG="${SSH_CONFIG:-}"
# OpenSSH 是按 passwd 里记录的 home 找 ~/.ssh/config，不认 $HOME。
# 本机 HOME 被 Octop 挪到 /vol1/.../data，ssh 直接连 "s21" 会报 Could not resolve hostname。
# 显式 -F 与被 ssh 自动读取等价，所以这里兜底探测，不影响正常环境。
if [ -z "$SSH_CONFIG" ] && [ -r "$HOME/.ssh/config" ]; then
    SSH_CONFIG="$HOME/.ssh/config"
fi
REMOTE_DIR="${REMOTE_DIR:-/home/unit3/sms-multi}"
CONTAINER="${CONTAINER:-sms-multi}"
IMAGE_REPO="${IMAGE_REPO:-smstext-multi-device}"
GITHUB_REPO="${GITHUB_REPO:-st1341815/Uart_sms_gateway}"
HEALTH_URL="${HEALTH_URL:-}"
PUSH_MODE="${PUSH_MODE:-auto}"
BUILD_ARG_WEB_PREBUILT=1

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

# ---------- 参数 ----------
VERSION=""
DO_BUILD=1
DO_DEPLOY=1
DO_PUBLISH=1
DO_BINARIES=1
DRY_RUN=0
COMMIT_MSG=""

usage() { sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'; exit 0; }

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="${2:?}"; shift 2 ;;
        --version=*) VERSION="${1#*=}"; shift ;;
        --skip-build) DO_BUILD=0; shift ;;
        --skip-deploy) DO_DEPLOY=0; shift ;;
        --skip-publish) DO_PUBLISH=0; shift ;;
        --skip-binaries) DO_BINARIES=0; shift ;;
        --dry-run) DRY_RUN=1; shift ;;
        --commit) COMMIT_MSG="${2:?}"; shift 2 ;;
        --commit=*) COMMIT_MSG="${1#*=}"; shift ;;
        -h|--help) usage ;;
        *) echo "未知参数: $1（--help 查看用法）" >&2; exit 2 ;;
    esac
done

# ---------- 输出 ----------
if [ -t 1 ]; then
    C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_ERR=$'\033[31m'; C_STEP=$'\033[36m'; C_DIM=$'\033[2m'; C_END=$'\033[0m'
else
    C_OK=; C_WARN=; C_ERR=; C_STEP=; C_DIM=; C_END=
fi
STEP_NO=0
step()  { STEP_NO=$((STEP_NO+1)); printf '\n%s[%d] %s%s\n' "$C_STEP" "$STEP_NO" "$*" "$C_END"; }
ok()    { printf '  %s✓%s %s\n' "$C_OK" "$C_END" "$*"; }
warn()  { printf '  %s⚠%s  %s\n' "$C_WARN" "$C_END" "$*"; }
info()  { printf '  %s%s%s\n' "$C_DIM" "$*" "$C_END"; }
die()   { printf '\n%s✗ 失败:%s %s\n' "$C_ERR" "$C_END" "$*" >&2; exit 1; }
run()   {
    if [ "$DRY_RUN" = 1 ]; then
        printf '  %s[dry-run]%s %s\n' "$C_DIM" "$C_END" "$*"
    else
        eval "$@"
    fi
}
now()   { date +%s; }
elapsed() { echo "$(( $(now) - $1 ))s"; }

# 远程执行（stdin 传脚本，避免多层引号地狱）
remote() {
    if [ -n "$SSH_CONFIG" ]; then
        ssh -F "$SSH_CONFIG" -o BatchMode=yes -o ConnectTimeout=10 "$REMOTE_HOST" "$@"
    else
        ssh -o BatchMode=yes -o ConnectTimeout=10 "$REMOTE_HOST" "$@"
    fi
}

# ---------- 0. 预检 ----------
step "预检"

if [ -z "$VERSION" ]; then
    VERSION=$(python3 scripts/check_version.py --print 2>/dev/null | tr -d '[:space:]') \
        || die "读取当前版本号失败"
fi
VERSION="${VERSION#v}"
TAG="v${VERSION}"
# 推送用实际分支名，不写死 master —— 仓库改主分支或从维护分支发版时不会推错
BRANCH=$(git rev-parse --abbrev-ref HEAD)
ok "目标版本 $VERSION（tag $TAG，分支 $BRANCH）"

python3 scripts/check_version.py >/dev/null 2>&1 \
    || die "版本号不一致（前端 / 后端 / CHANGELOG 三处），先跑 make bump-version 或手工对齐"
ok "版本号三处一致"

for bin in git python3 tar ssh; do
    command -v "$bin" >/dev/null 2>&1 || die "缺少命令：$bin"
done
command -v npm >/dev/null 2>&1 || command -v yarn >/dev/null 2>&1 || die "缺少 npm/yarn（构建前端需要）"

if ! remote true 2>/dev/null; then
    die "连不上远程主机 $REMOTE_HOST（ssh 别名/密钥/网络？）"
fi
ok "远程主机 $REMOTE_HOST 可达"

remote "docker compose version" >/dev/null 2>&1 \
    || die "远程主机缺少 docker compose 插件"
ok "远程 docker compose 可用"

# 工作区状态：tag 必须打在「实际被构建的代码」上，所以脏工作区必须先落盘
if [ -n "$(git status --porcelain)" ]; then
    if [ -n "$COMMIT_MSG" ]; then
        step "提交未落盘的改动"
        run "git add -A && git commit -q -m \"$COMMIT_MSG\""
        ok "已提交：$COMMIT_MSG"
    else
        warn "工作区有未提交改动："
        git status --short | sed 's/^/      /'
        die "请先提交，或用 --commit '说明' 让脚本代提交（否则 tag 与构建内容对不上）"
    fi
fi
ok "工作区干净（HEAD $(git rev-parse --short HEAD)）"

# 健康检查地址必须用**能解析**的主机名。
# REMOTE_HOST 通常是 ssh 别名（s21），curl 解析不了它，于是 20 次轮询全部落空，
# 一次成功的部署被误判成失败、白白回滚 —— 2026-11-06 真实踩过。
# ssh -G 能把别名还原成真实地址（s21 → 192.168.11.21）。
if [ -z "$HEALTH_URL" ]; then
    if [ -n "$SSH_CONFIG" ]; then
        _HOST_ADDR=$(ssh -F "$SSH_CONFIG" -G "$REMOTE_HOST" 2>/dev/null | awk '$1=="hostname"{print $2; exit}' || true)
    else
        _HOST_ADDR=$(ssh -G "$REMOTE_HOST" 2>/dev/null | awk '$1=="hostname"{print $2; exit}' || true)
    fi
    [ -n "${_HOST_ADDR:-}" ] || _HOST_ADDR="${REMOTE_HOST%%@*}"
    HEALTH_URL="http://${_HOST_ADDR}:8080/health"
fi

if [ "$DO_PUBLISH" = 1 ] && [ "$DRY_RUN" = 0 ]; then
    if [ -z "${GITHUB_TOKEN:-}" ]; then
        die "发布阶段需要 GITHUB_TOKEN（推送 tag + 建 Release）。只想构建部署请加 --skip-publish"
    fi
    ok "GITHUB_TOKEN 已提供（${#GITHUB_TOKEN} 字符）"
fi

# ---------- 1. 前端 ----------
step "构建前端"
if [ "$DO_BUILD" = 0 ]; then
    info "跳过（--skip-build）"
else
    T0=$(now)
    run "(cd web && npm run build)" || die "前端构建失败"
    run "make prepare-embed" >/dev/null || die "同步 web/dist → web/embed_dir 失败"
    [ -f web/embed_dir/index.html ] || die "web/embed_dir/index.html 不存在（前端产物没同步进去）"
    ok "前端产物已嵌入（耗时 $(elapsed "$T0")）"
    # 前端产物必须进 commit：tag 指向的 commit 得能重建出同样的页面。
    # 版本号一变 bundle 哈希就变，所以这个目录每次发版必然脏 —— 在这里自动提交，
    # 而不是只 warn（只提示的话，tag 会指向没有新页面的 commit）。
    if [ -n "$(git status --porcelain web/embed_dir)" ]; then
        if [ "$DRY_RUN" = 1 ]; then
            info "[dry-run] git add web/embed_dir && git commit -m 'build(web): 更新 $VERSION 前端产物'"
        else
            git add web/embed_dir || die "暂存前端产物失败"
            git commit -q -m "build(web): 更新 $VERSION 前端产物" \
                || die "提交前端产物失败（检查 git user.name / user.email）"
            ok "前端产物已提交（HEAD $(git rev-parse --short HEAD)）"
        fi
    fi
fi

# ---------- 2. 上传源码 ----------
step "同步源码到构建机"
if [ "$DO_BUILD" = 0 ]; then
    info "跳过（--skip-build）"
else
    TARBALL="/tmp/uart-sms-${VERSION}.tar.gz"
    REMOTE_BUILD_DIR="${REMOTE_DIR}/.build-${VERSION}"
    run "tar czf '$TARBALL' \
        --exclude=./web/node_modules --exclude=./web/dist \
        --exclude=./.git --exclude=./bin --exclude=./screenshots ." \
        || die "打包失败"
    # 注意 pipefail：dry-run 下 tar 没生成文件，du 会返回 1，
    # 整条 pipeline 跟着非 0，set -e 直接把脚本静默干掉（stderr 还被丢了）。
    # 所以大小统计只在真跑时做，且显式兜底。
    if [ "$DRY_RUN" = 0 ]; then
        SIZE=$(du -h "$TARBALL" 2>/dev/null | cut -f1 || true)
        info "包大小 ${SIZE:-?}"
    fi

    if [ "$DRY_RUN" = 0 ]; then
        if [ -n "$SSH_CONFIG" ]; then SCP_OPTS="-F $SSH_CONFIG"; else SCP_OPTS=""; fi
        scp $SCP_OPTS -o BatchMode=yes -o ConnectTimeout=10 "$TARBALL" "$REMOTE_HOST:/tmp/" \
            || die "scp 失败"
        remote bash -s <<EOF || die "远程解包失败"
set -e
rm -rf '$REMOTE_BUILD_DIR'
mkdir -p '$REMOTE_BUILD_DIR'
tar xzf '/tmp/$(basename "$TARBALL")' -C '$REMOTE_BUILD_DIR'
EOF
        ok "源码已就位：$REMOTE_BUILD_DIR"
    else
        info "[dry-run] scp $TARBALL $REMOTE_HOST:/tmp/ 并解包到 $REMOTE_BUILD_DIR"
    fi
fi

# ---------- 3. 构建镜像 ----------
step "构建镜像 $IMAGE_REPO:$VERSION"
if [ "$DO_BUILD" = 0 ]; then
    info "跳过（--skip-build）"
else
    T0=$(now)
    if [ "$DRY_RUN" = 1 ]; then
        info "[dry-run] 远程 docker build -f Dockerfile.multi --build-arg WEB_PREBUILT=$BUILD_ARG_WEB_PREBUILT -t $IMAGE_REPO:$VERSION"
    else
        remote bash -s <<EOF || die "docker build 失败（日志见远端 /tmp/build-${VERSION}.log）"
set -e
cd '$REMOTE_BUILD_DIR'
docker build -f Dockerfile.multi --build-arg WEB_PREBUILT=$BUILD_ARG_WEB_PREBUILT \
    -t '$IMAGE_REPO:$VERSION' . > /tmp/build-${VERSION}.log 2>&1 || {
        tail -30 /tmp/build-${VERSION}.log
        exit 1
    }
EOF
        ok "镜像构建完成（耗时 $(elapsed "$T0")）"
    fi
fi

# ---------- 4. 部署 ----------
step "部署到 $REMOTE_HOST"
if [ "$DO_DEPLOY" = 0 ]; then
    info "跳过（--skip-deploy）"
else
    PREV_TAG=$(remote "docker inspect $CONTAINER --format '{{.Config.Image}}'" 2>/dev/null | sed 's/.*://' || true)
    info "当前线上版本：${PREV_TAG:-未知}"

    if [ "$DRY_RUN" = 1 ]; then
        info "[dry-run] 打回滚点 $IMAGE_REPO:pre-$VERSION、备份 compose、切镜像、compose up -d"
    else
        BACKUP_TS=$(date +%Y%m%d-%H%M%S)
        remote bash -s <<EOF || die "部署失败"
set -e
cd '$REMOTE_DIR'
# 回滚点：把当前线上镜像多打一个 tag，出问题一条命令切回去
if [ -n "${PREV_TAG:-}" ] && [ "${PREV_TAG:-}" != "none" ]; then
    docker tag '$IMAGE_REPO:${PREV_TAG}' '$IMAGE_REPO:pre-$VERSION' 2>/dev/null || true
fi
cp -a docker-compose.yml "docker-compose.yml.bak-pre${VERSION//./}-${BACKUP_TS}"
# 只替换 image: 行的值，不动其他内容
sed -i -E "s|^([[:space:]]*image:[[:space:]]*).*|\1$IMAGE_REPO:$VERSION|" docker-compose.yml
grep -n 'image:' docker-compose.yml
docker compose up -d
EOF
        ok "已切换镜像并重启容器"
    fi
fi

# ---------- 5. 验证 ----------
step "验证服务健康"
if [ "$DO_DEPLOY" = 0 ] || [ "$DRY_RUN" = 1 ]; then
    info "跳过（未部署 / dry-run）"
else
    HEALTHY=0
    for i in $(seq 1 20); do
        sleep 2
        BODY=$(curl -s -m 5 "$HEALTH_URL" 2>/dev/null || true)
        if printf '%s' "$BODY" | grep -q '"status"[[:space:]]*:[[:space:]]*"ok"'; then
            HEALTHY=1
            break
        fi
    done
    if [ "$HEALTHY" != 1 ]; then
        warn "健康检查未通过（探测地址 $HEALTH_URL）"
        # 先取证再回滚：不然「为什么失败」这个最关键的信息随着容器被重建一起蒸发
        info "容器状态：$(remote "docker inspect $CONTAINER --format '{{.State.Status}} restarts={{.RestartCount}}'" 2>/dev/null || echo 未知)"
        info "最近 25 行日志："
        remote "docker logs --tail 25 $CONTAINER" 2>&1 | sed 's/^/      /' || true
        if [ -n "${PREV_TAG:-}" ]; then
            warn "回滚到 $PREV_TAG"
            remote bash -s <<EOF || true
set -e
cd '$REMOTE_DIR'
sed -i -E "s|^([[:space:]]*image:[[:space:]]*).*|\1$IMAGE_REPO:${PREV_TAG}|" docker-compose.yml
docker compose up -d
EOF
            # 回滚同样要验证：改回 compose 不等于服务真的起来了
            sleep 4
            _RB=$(curl -s -m 5 "$HEALTH_URL" 2>/dev/null || true)
            if printf '%s' "$_RB" | grep -q '"status"[[:space:]]*:[[:space:]]*"ok"'; then
                die "新版本健康检查失败，已回滚到 $PREV_TAG（回滚后服务正常）"
            fi
            die "回滚到 $PREV_TAG 后仍不健康，请立刻人工介入：$REMOTE_DIR"
        fi
        # 拿不到上一版镜像时绝不能瞎 sed —— 那会把 image 行写成空 tag，
        # 下一轮 compose up 直接拉不到镜像，故障从「服务不健康」升级成「起不来」
        die "部署后健康检查失败，且无法确定上一版镜像（PREV_TAG 为空），请人工检查 $REMOTE_DIR/docker-compose.yml"
    fi
    ok "健康检查通过：$HEALTH_URL"

    REAL_IMAGE=$(remote "docker inspect $CONTAINER --format '{{.Config.Image}}'" 2>/dev/null || true)
    case "$REAL_IMAGE" in
        *":$VERSION") ok "运行中镜像确认：$REAL_IMAGE" ;;
        *) die "运行中镜像与目标版本不符：$REAL_IMAGE" ;;
    esac
fi

# ---------- 6. tag ----------
step "打 tag $TAG"
if [ "$DO_PUBLISH" = 0 ]; then
    info "跳过（--skip-publish）"
elif git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
    EXISTING=$(git rev-list -n1 "$TAG")
    if [ "$EXISTING" != "$(git rev-parse HEAD)" ]; then
        if [ "$DRY_RUN" = 1 ]; then
            # dry-run 的目的是把后面几步（推送/Release/清理）也演练到，
            # 真跑时这里是硬错误，所以只降级成警告，别中断演练
            warn "[dry-run] tag $TAG 已存在且指向 $EXISTING（真跑会拒绝覆盖，需先 bump 版本或删旧 tag）"
        else
            die "tag $TAG 已存在且指向别的 commit（$EXISTING），拒绝覆盖。确认后手工 git tag -d $TAG 再跑"
        fi
    else
        ok "tag 已存在且指向当前 HEAD，无需重建"
    fi
else
    run "git tag -a '$TAG' -m 'UART SMS Gateway $TAG'" || die "打 tag 失败"
    ok "tag 已创建"
fi

# ---------- 发布包（二进制附件） ----------
step "生成发布包"
DIST_DIR="dist-release"
if [ "$DO_BINARIES" = 0 ]; then
    info "跳过（--skip-binaries）"
elif [ "$DO_PUBLISH" = 0 ]; then
    info "跳过（--skip-publish）"
elif [ "$DRY_RUN" = 1 ]; then
    info "[dry-run] 从 $IMAGE_REPO:$VERSION 抽 /app/uart_sms_forwarder，再用 build-release.sh 打包到 $DIST_DIR/"
else
    # 二进制直接从已构建的镜像里抽，而不是重新交叉编译：
    #   1) 与线上跑的可执行文件字节一致，不会出现「镜像里是 A、发布包里是 B」
    #   2) 构建机不需要 Go 工具链（s21 实测没有 go），省掉 go mod download 那几分钟
    # Dockerfile.multi 是裸 go build，而 internal/version 的默认值就是发行版本号，
    # 所以抽出来的二进制自带正确版本，不需要 -ldflags 注入。
    REMOTE_BIN="/tmp/uart_sms_forwarder-${VERSION}"
    LOCAL_BIN="/tmp/uart_sms_forwarder-${VERSION}"
    CID="relbin-$(printf %s "$VERSION" | tr '.' '-')"
    remote bash -s <<EOF || die "从镜像抽取二进制失败"
set -e
docker rm -f '$CID' >/dev/null 2>&1 || true
docker create --name '$CID' '$IMAGE_REPO:$VERSION' >/dev/null
docker cp '$CID:/app/uart_sms_forwarder' '$REMOTE_BIN'
docker rm -f '$CID' >/dev/null
chmod +x '$REMOTE_BIN'
EOF
    if [ -n "$SSH_CONFIG" ]; then SCP_OPTS="-F $SSH_CONFIG"; else SCP_OPTS=""; fi
    scp $SCP_OPTS -o BatchMode=yes -o ConnectTimeout=10 \
        "$REMOTE_HOST:$REMOTE_BIN" "$LOCAL_BIN" || die "拉取二进制失败"
    if BINARY="$LOCAL_BIN" ./scripts/build-release.sh "$VERSION" > /tmp/pack-${VERSION}.log 2>&1; then
        ok "发布包：$(ls "$DIST_DIR" 2>/dev/null | tr '\n' ' ')"
    else
        tail -20 /tmp/pack-${VERSION}.log
        die "打包失败（日志 /tmp/pack-${VERSION}.log）"
    fi
    remote "rm -f '$REMOTE_BIN'" 2>/dev/null || true
fi

# ---------- 推送 + Release ----------
step "推送到 GitHub"
if [ "$DO_PUBLISH" = 0 ]; then
    info "跳过（--skip-publish）"
elif [ "$DRY_RUN" = 1 ]; then
    info "[dry-run] git push $BRANCH + $TAG，创建 Release 并上传 dist-release/ 附件"
else
    # 推送方式：auto 时优先 SSH。
    # 起因：本机 git 2.39.5 走 https 推 github 会 `GnuTLS recv error (-110)` 断连
    # （ls-remote 也一样挂），而同一网络的 SSH 通路稳定 —— 别在这上面反复重试 https。
    if [ "$PUSH_MODE" = "auto" ]; then
        case "$(git remote get-url origin 2>/dev/null || echo '')" in
            ssh://*|git@*)
                if [ -n "$SSH_CONFIG" ]; then PUSH_MODE=ssh; else PUSH_MODE=https; fi ;;
            *) PUSH_MODE=https ;;
        esac
    fi

    if [ "$PUSH_MODE" = "ssh" ]; then
        info "推送方式：ssh（$REMOTE_HOST 同源配置）"
        GIT_SSH_COMMAND="ssh -F $SSH_CONFIG -o StrictHostKeyChecking=accept-new" \
            git push origin "$BRANCH" 2>&1 | tail -3 || die "git push $BRANCH 失败（ssh）"
        GIT_SSH_COMMAND="ssh -F $SSH_CONFIG -o StrictHostKeyChecking=accept-new" \
            git push origin "$TAG" 2>&1 | tail -3 || die "git push $TAG 失败（ssh）"
    else
        info "推送方式：https + token"
        PUSH_URL="https://${GITHUB_TOKEN}@github.com/${GITHUB_REPO}.git"
        # token 只出现在这条命令里，不写进 .git/config；输出里抹掉，避免泄漏到日志
        git push "$PUSH_URL" "$BRANCH" 2>&1 | sed "s|${GITHUB_TOKEN}|***|g" | tail -3 \
            || die "git push $BRANCH 失败（https，可试 PUSH_MODE=ssh）"
        git push "$PUSH_URL" "$TAG" 2>&1 | sed "s|${GITHUB_TOKEN}|***|g" | tail -3 \
            || die "git push $TAG 失败（https，可试 PUSH_MODE=ssh）"
    fi
    ok "$BRANCH 与 $TAG 已推送"

    step "创建 GitHub Release 并上传附件"
    REL_RC=0
    GITHUB_TOKEN="$GITHUB_TOKEN" python3 - "$VERSION" "$GITHUB_REPO" <<'PY' || REL_RC=$?
import json, os, re, pathlib, sys, urllib.request, urllib.error, urllib.parse
ver, repo = sys.argv[1], sys.argv[2]
token = os.environ.get("GITHUB_TOKEN", "")
API = "https://api.github.com"
HDR = {"Authorization": f"token {token}", "Accept": "application/vnd.github+json"}

def call(url, data=None, method="GET", ctype="application/json", timeout=60):
    req = urllib.request.Request(url, data=data, method=method,
                                 headers=dict(HDR, **{"Content-Type": ctype}))
    return urllib.request.urlopen(req, timeout=timeout)

text = pathlib.Path("CHANGELOG.md").read_text(encoding="utf-8")
m = re.search(r'^## \[' + re.escape(ver) + r'\].*?$\n(.*?)(?=^## \[|\Z)', text, re.M | re.S)
body = (m.group(1).strip() if m else "")
if not body:
    print(f"  ⚠ CHANGELOG 里没有 {ver} 的正文，Release 正文将为空", file=sys.stderr)

payload = json.dumps({
    "tag_name": f"v{ver}", "name": f"UART SMS Gateway v{ver}",
    "body": body, "draft": False, "prerelease": False,
}).encode()

try:
    with call(f"{API}/repos/{repo}/releases", payload, "POST") as r:
        rel = json.load(r)
    print(f"  ✓ Release 已创建：{rel['html_url']}")
except urllib.error.HTTPError as e:
    msg = e.read().decode()[:300]
    if e.code == 422 and "already_exists" in msg:
        # 已存在时不能直接 return：附件还得传，必须先把 upload_url 取回来
        with call(f"{API}/repos/{repo}/releases/tags/v{ver}") as r:
            rel = json.load(r)
        print(f"  ✓ Release 已存在，沿用：{rel['html_url']}")
    else:
        print(f"  ✗ 创建 Release 失败 HTTP {e.code}: {msg}", file=sys.stderr)
        sys.exit(1)

d = pathlib.Path("dist-release")
files = sorted((x for x in d.glob("*") if x.is_file()), key=lambda x: x.name) if d.is_dir() else []
if not files:
    print("  · dist-release/ 为空，没有附件要传")
upload = rel["upload_url"].split("{")[0]
failed = []
for f in files:
    try:
        with call(f"{upload}?name={urllib.parse.quote(f.name)}",
                  f.read_bytes(), "POST", "application/octet-stream", 300) as r:
            a = json.load(r)
        print(f"  ✓ 附件 {f.name}（{a['size'] / 1048576:.1f} MB）")
    except urllib.error.HTTPError as e:
        msg = e.read().decode()[:200]
        if e.code == 422 and "already_exists" in msg:
            print(f"  · 附件 {f.name} 已存在，跳过")
        else:
            failed.append(f.name)
            print(f"  ✗ 附件 {f.name} HTTP {e.code}: {msg}", file=sys.stderr)
    except Exception as e:
        failed.append(f.name)
        print(f"  ✗ 附件 {f.name}: {e}", file=sys.stderr)

if failed:
    sys.exit(2)
PY
    if [ "$REL_RC" = 2 ]; then
        warn "Release 正文已发布，但有附件上传失败（见上）—— 可到网页手工补传"
    elif [ "$REL_RC" != 0 ]; then
        die "创建 Release 失败（退出码 $REL_RC）"
    fi
fi

# ---------- 收尾 ----------
step "清理构建残留"
if [ "$DRY_RUN" = 1 ]; then
    info "[dry-run] 删除远端 .build-$VERSION 与本地 tar 包"
else
    remote "rm -rf '$REMOTE_DIR/.build-$VERSION'" 2>/dev/null || true
    rm -f "/tmp/uart-sms-${VERSION}.tar.gz"
    ok "已清理"
fi

printf '\n%s完成：%s%s\n' "$C_OK" "$IMAGE_REPO:$VERSION" "$C_END"
# 注意：这里必须用 if 而不是 `[ ... ] && printf ...`。
# 在 set -e 下，条件为假时整条 && 链返回非零，脚本会以「成功做完却退出码 1」结束，
# 上层调用方（make、CI、别的脚本）会误判成本次发版失败。
if [ "$DO_DEPLOY" = 1 ] && [ "$DRY_RUN" = 0 ]; then
    printf '  线上：%s\n' "$HEALTH_URL"
fi
if [ "$DO_PUBLISH" = 1 ] && [ "$DRY_RUN" = 0 ]; then
    printf '  Release：https://github.com/%s/releases/tag/%s\n' "$GITHUB_REPO" "$TAG"
fi
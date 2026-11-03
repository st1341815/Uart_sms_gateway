#!/bin/sh
# 构建多平台二进制并打成发布包。
#
# 为什么不用 `make build-servers`：
#   Makefile 里那套依赖 `upx`（压缩）+ `yarn`，CI 容器里都没有，
#   而 upx 压缩属于「可选的体积优化」，失败也只是 `|| true`。
#   这里直接用 go build，行为对 CI 更确定；本地想压体积仍可 `make build-servers`。
#
# 用法：./scripts/build-release.sh v1.1.3
# 产物：dist-release/uart_sms_forwarder-<platform>.tar.gz（Windows 为 .zip）
set -eu

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
    echo "用法：$0 <version，例如 v1.1.3>" >&2
    exit 1
fi

# 去掉 git tag 的 v 前缀写进二进制，和 Web 侧侧边栏显示的版本号一致
VER_NUM="${VERSION#v}"

GOOS_LIST="linux windows darwin freebsd"
# 注意：freebsd 只出 amd64；arm64/arm 在 freebsd 上不是本项目目标平台
PLATFORMS="
linux/amd64
linux/arm64
linux/arm
windows/amd64
windows/arm64
darwin/amd64
darwin/arm64
freebsd/amd64
"

OUT=dist-release
BIN_TMP=.release-bin
rm -rf "$OUT" "$BIN_TMP"
mkdir -p "$OUT" "$BIN_TMP"

# 前端产物必须先就位（go:embed 引用 web/embed_dir）
if [ ! -f web/embed_dir/index.html ]; then
    echo "错误：web/embed_dir/index.html 不存在，请先构建前端" >&2
    exit 1
fi

echo "== 构建二进制 $VERSION (VER_NUM=$VER_NUM) =="
for P in $PLATFORMS; do
    OS="${P%%/*}"
    ARCH="${P##*/}"

    case "$OS/$ARCH" in
        linux/arm) GOARM=7 ;;
        *)         GOARM="" ;;
    esac

    NAME="uart_sms_forwarder"
    [ "$OS" = "windows" ] && NAME="uart_sms_forwarder.exe"

    OUTNAME="uart_sms_forwarder-${OS}-${ARCH}"
    [ "$OS" = "windows" ] && OUTNAME="${OUTNAME}.exe"

    LABEL="$P"
    [ -n "$GOARM" ] && LABEL="$P (GOARM=$GOARM)"
    echo "-- $LABEL"

    CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" GOARM="$GOARM" \
        go build -trimpath -ldflags="-s -w -X github.com/dushixiang/uart_sms_forwarder/internal/version.Version=${VER_NUM}" \
        -o "${BIN_TMP}/${OUTNAME}" ./cmd/serv
done

echo "== 打包 =="
for P in $PLATFORMS; do
    OS="${P%%/*}"
    ARCH="${P##*/}"

    DIR="uart_sms_forwarder-${OS}-${ARCH}"
    rm -rf "$DIR"
    mkdir -p "$DIR"

    if [ "$OS" = "windows" ]; then
        cp "${BIN_TMP}/uart_sms_forwarder-${OS}-${ARCH}.exe" "$DIR/uart_sms_forwarder.exe"
    else
        cp "${BIN_TMP}/uart_sms_forwarder-${OS}-${ARCH}" "$DIR/uart_sms_forwarder"
    fi

    cp config.example.yaml "$DIR/config.yaml"
    [ -f README.md ] && cp README.md "$DIR/README.md"
    [ -f CHANGELOG.md ] && cp CHANGELOG.md "$DIR/CHANGELOG.md"

    if [ "$OS" = "windows" ]; then
        # zip 在 CI 镜像里可能没有，用 python 兜底（python3 一定有，check_version 就在用）
        python3 - "$DIR" "$OUT/${DIR}.zip" <<'PY'
import os, sys, zipfile
src, dst = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(dst, 'w', zipfile.ZIP_DEFLATED) as z:
    for root, _, files in os.walk(src):
        for f in files:
            p = os.path.join(root, f)
            z.write(p, os.path.relpath(p, os.path.dirname(src)))
PY
    else
        tar -czf "$OUT/${DIR}.tar.gz" "$DIR"
    fi

    rm -rf "$DIR"
    echo "-- $OUT/${DIR}.*"
done

rm -rf "$BIN_TMP"

echo "== 完成 =="
ls -lh "$OUT"

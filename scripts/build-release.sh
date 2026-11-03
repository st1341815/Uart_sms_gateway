#!/bin/sh
# 构建多平台二进制 + 发布包（Gitea / GitHub 发版共用）
#
# 用法：VERSION=v1.1.3 ./scripts/build-release.sh [版本号]
# 产物：dist-release/
#   uart_sms_forwarder-<ver>-linux-amd64.tar.gz   （内含二进制 + config.yaml + README）
#   uart_sms_forwarder-<ver>-linux-arm64.tar.gz
#   uart_sms_forwarder-<ver>-linux-armv7.tar.gz
#   uart_sms_forwarder-<ver>-windows-amd64.zip
#   uart_sms_forwarder-<ver>-darwin-amd64.tar.gz
#   uart_sms_forwarder-<ver>-darwin-arm64.tar.gz
#   SHA256SUMS.txt
#
# 与 `make build-servers` 的区别（为什么不用它）：
#   Makefile 那套依赖 upx 压缩和 yarn 构建前端，CI 镜像里不一定有，
#   而且 upx 只是体积优化、不是功能必需。这里只做「能装能跑」的最小可用包，
#   前端由调用方提前构建好并同步进 web/embed_dir（见工作流/README）。

set -eu

VERSION="${1:-${VERSION:-}}"
if [ -z "$VERSION" ]; then
    VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
fi
# tag 形如 v1.1.3 → 二进制里存 1.1.3
VERSION_NUM="${VERSION#v}"

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

OUT="$ROOT/dist-release"
rm -rf "$OUT"
mkdir -p "$OUT" bin

if [ ! -f web/embed_dir/index.html ]; then
    echo "⚠️  web/embed_dir 不存在，打包出来的二进制只有占位页（后端 API 仍可用）" >&2
fi

LDFLAGS="-s -w -X github.com/dushixiang/uart_sms_forwarder/internal/version.Version=${VERSION_NUM}"

# 目标平台：OS/ARCH/GOARM/后缀标识
build_one() {
    os=$1; arch=$2; goarm=$3; label=$4
    name="bin/uart_sms_forwarder-${label}"
    case "$os" in windows) name="${name}.exe" ;; esac
    echo "→ building ${label}"
    env CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" ${goarm:+GOARM="$goarm"} \
        go build -trimpath -buildvcs=false -ldflags="$LDFLAGS" -o "$name" ./cmd/serv
}

pack_tar() {
    label=$1; inner=$2
    dir="$OUT/uart_sms_forwarder-${VERSION_NUM}-${label}"
    rm -rf "$dir"; mkdir -p "$dir"
    cp "bin/$inner" "$dir/uart_sms_forwarder"
    cp config.example.yaml "$dir/config.yaml"
    cat > "$dir/README.txt" <<EOF
UART SMS Gateway ${VERSION_NUM} (${label})
==========================================

1. 改 config.yaml：串口设备路径、账号密码、Web 端口。
   设备路径建议用 by-path（插拔/换口不变），见 config.example.yaml 注释。
2. 启动：
     ./uart_sms_forwarder -config config.yaml
3. 浏览器打开 http://<设备IP>:8080 ，默认账号见 config.yaml 的 auth 段。
4. 数据落在 ./data/，日志 ./logs/。

⚠️ 首次启动会在 data/ 下生成 .jwt_secret（0600）。**这个文件必须跟 data/ 一起备份**——
   API Key 的 Secret 密文是用它派生的密钥加密的，丢了它，已下发的 SK 就永久解不开。
   也可以用环境变量 JWT_SECRET_FILE 指定到别处。
EOF
    tar -C "$OUT" -czf "$OUT/uart_sms_forwarder-${VERSION_NUM}-${label}.tar.gz" \
        "uart_sms_forwarder-${VERSION_NUM}-${label}"
    rm -rf "$dir"
    echo "  ✓ $(basename "$OUT")/uart_sms_forwarder-${VERSION_NUM}-${label}.tar.gz"
}

pack_zip() {
    label=$1; inner=$2
    dir="$OUT/uart_sms_forwarder-${VERSION_NUM}-${label}"
    rm -rf "$dir"; mkdir -p "$dir"
    cp "bin/$inner" "$dir/uart_sms_forwarder.exe"
    cp config.example.yaml "$dir/config.yaml"
    cat > "$dir/README.txt" <<EOF
UART SMS Gateway ${VERSION_NUM} (${label})
==========================================

1. 改 config.yaml（串口 COM 号、账号密码、端口）。
2. 在 PowerShell / cmd 里运行：
     uart_sms_forwarder.exe -config config.yaml
3. 浏览器打开 http://127.0.0.1:8080

⚠️ 首次启动会在 data\\ 下生成 .jwt_secret，**必须跟 data\\ 一起备份**。
EOF
    if command -v zip >/dev/null 2>&1; then
        ( cd "$OUT" && zip -qr "uart_sms_forwarder-${VERSION_NUM}-${label}.zip" \
            "uart_sms_forwarder-${VERSION_NUM}-${label}" )
        rm -rf "$dir"
        echo "  ✓ uart_sms_forwarder-${VERSION_NUM}-${label}.zip"
    else
        # 某些构建镜像（如 golang:1.25）没装 zip。保留待打包目录，
        # 由调用方（CI 作业容器里的 python3 / 本地）压成 .zip。
        echo "  ! 无 zip 命令，保留目录待外部打包：$(basename "$dir")" >&2
    fi
}

# 只出 linux/amd64（2026-11-03 按用户要求收窄）。
#
# 为什么砍掉其余 5 个平台：部署目标就是 x86 服务器（.21 宝塔主机 / 飞牛 NAS），
# 其余产物从来没人下过，却让每次发版多花 5 次交叉编译的时间。
# 恢复多平台只需把下面被注释掉的行放回来 —— build_one / pack_tar / pack_zip
# 三个函数本身是通用的，没有硬编码任何单一平台。
build_one linux  amd64 ""   linux-amd64
# build_one linux  arm64 ""   linux-arm64
# build_one linux  arm   7    linux-armv7
# build_one windows amd64 ""  windows-amd64
# build_one darwin amd64 ""   darwin-amd64
# build_one darwin arm64 ""   darwin-arm64

pack_tar linux-amd64   uart_sms_forwarder-linux-amd64
# pack_tar linux-arm64   uart_sms_forwarder-linux-arm64
# pack_tar linux-armv7   uart_sms_forwarder-linux-armv7
# pack_tar darwin-amd64  uart_sms_forwarder-darwin-amd64
# pack_tar darwin-arm64  uart_sms_forwarder-darwin-arm64
# pack_zip windows-amd64 uart_sms_forwarder-windows-amd64.exe

# 只对真实存在的产物算校验和（无 zip 命令时 *.zip 可能为空）
sha256_of() {
    for f in "$@"; do
        [ -f "$f" ] || continue
        if command -v sha256sum >/dev/null 2>&1; then
            sha256sum "$f"
        else
            shasum -a 256 "$f"
        fi
    done
}
( cd "$OUT" && sha256_of ./*.tar.gz ./*.zip > SHA256SUMS.txt )

echo
echo "==== 发布包 ===="
ls -lh "$OUT"

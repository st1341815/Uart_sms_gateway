#!/usr/bin/env python3
"""把目录压成同名 .zip（跨平台，不依赖 zip 命令）。

用法：python3 scripts/zip_dir.py dist-release/uart_sms_forwarder-1.1.3-windows-amd64

产物：<dir>.zip，压缩包内的顶层目录名与 <dir> 一致（Windows 用户解压即得同名文件夹）。
存在原因：构建镜像 golang:1.25 里没有 zip 命令，CI 在作业容器里用 python3 兜底打包。
"""
import os
import sys
import zipfile


def main(argv):
    if len(argv) != 2:
        print("用法: zip_dir.py <目录>", file=sys.stderr)
        return 2
    d = argv[1].rstrip("/")
    if not os.path.isdir(d):
        print(f"不是目录: {d}", file=sys.stderr)
        return 1
    out = d + ".zip"
    base = os.path.dirname(os.path.abspath(d))
    count = 0
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
        for root, _dirs, files in os.walk(d):
            for f in sorted(files):
                p = os.path.join(root, f)
                z.write(p, os.path.relpath(p, base))
                count += 1
    print(f"zipped {out} {os.path.getsize(out)} bytes ({count} files)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))

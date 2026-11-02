#!/usr/bin/env python3
"""检查前后端版本号是否一致。

- 前端：web/src/lib/version.ts 的 APP_VERSION（侧边栏展示的就是它）
- 后端：internal/version/version.go 的默认值（裸 go build 时用它）

发版前跑一次：python3 scripts/check_version.py
"""
import io
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SEMVER = re.compile(r"^\d+\.\d+\.\d+$")


def read_frontend():
    p = os.path.join(ROOT, "web", "src", "lib", "version.ts")
    s = io.open(p, encoding="utf-8").read()
    m = re.search(r"APP_VERSION\s*=\s*'([^']+)'", s)
    if not m:
        raise SystemExit(f"✗ 在 {p} 里没找到 APP_VERSION")
    return m.group(1)


def read_backend():
    p = os.path.join(ROOT, "internal", "version", "version.go")
    s = io.open(p, encoding="utf-8").read()
    m = re.search(r'var\s+Version\s*=\s*"([^"]*)"', s)
    if not m:
        raise SystemExit(f"✗ 在 {p} 里没找到 Version 默认值")
    return m.group(1)


def main():
    fe = read_frontend()
    be = read_backend()
    ok = True

    print(f"前端 APP_VERSION  = {fe}")
    print(f"后端 Version 默认 = {be}")

    if not SEMVER.match(fe):
        print(f"✗ 前端版本号不是 semver：{fe}")
        ok = False
    if be != fe:
        print(f"✗ 前后端版本号不一致（后端是 {be}，前端是 {fe}）")
        print("  → 改 internal/version/version.go 的 var Version = \"<版本>\"")
        ok = False

    print("✓ 版本号一致" if ok else "✗ 版本号检查失败")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""版本号一致性检查。

三处必须一致：
  1. web/src/lib/version.ts        APP_VERSION        （侧边栏展示的就是它）
  2. internal/version/version.go   Version 默认值      （裸 go build / GET /api/version）
  3. CHANGELOG.md                  含 `## [x.y.z]` 一节（发版记录）

用法：
  python3 scripts/check_version.py           检查，不一致退出码 1
  python3 scripts/check_version.py --print   只打印当前版本号（给 Makefile 用）
  python3 scripts/check_version.py --bump 1.2.0   同时改上面三处（CHANGELOG 加空模板）

为什么要有这个脚本：1.1.0 之后连发 2 个版本忘了改号，侧边栏一直显示 v1.1.0，
与实际部署代码对不上。把「版本号」变成一处定义 + 自动校验，才不会再漂。
"""
import datetime
import io
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SEMVER = re.compile(r"^\d+\.\d+\.\d+$")

FE_FILE = os.path.join("web", "src", "lib", "version.ts")
BE_FILE = os.path.join("internal", "version", "version.go")
CL_FILE = "CHANGELOG.md"


def _p(rel):
    return os.path.join(ROOT, rel)


def read_frontend():
    s = io.open(_p(FE_FILE), encoding="utf-8").read()
    m = re.search(r"APP_VERSION\s*=\s*'([^']+)'", s)
    if not m:
        raise SystemExit(f"✗ {FE_FILE} 里没找到 APP_VERSION")
    return m.group(1)


def read_backend():
    s = io.open(_p(BE_FILE), encoding="utf-8").read()
    m = re.search(r'var\s+Version\s*=\s*"([^"]*)"', s)
    if not m:
        raise SystemExit(f"✗ {BE_FILE} 里没找到 Version 默认值")
    return m.group(1)


def changelog_versions():
    if not os.path.exists(_p(CL_FILE)):
        return []
    s = io.open(_p(CL_FILE), encoding="utf-8").read()
    return re.findall(r"^##\s*\[(\d+\.\d+\.\d+)\]", s, re.M)


def check():
    fe, be = read_frontend(), read_backend()
    cl = changelog_versions()
    ok = True

    print(f"前端 APP_VERSION  = {fe}")
    print(f"后端 Version 默认 = {be}")
    print(f"CHANGELOG 收录    = {', '.join(cl) if cl else '(空)'}")

    if not SEMVER.match(fe):
        print(f"✗ 前端版本号不是 x.y.z 形式：{fe}")
        ok = False
    if be != fe:
        print(f"✗ 前后端版本号不一致（后端 {be} ≠ 前端 {fe}）")
        ok = False
    if fe not in cl:
        print(f"✗ CHANGELOG.md 里没有 `## [{fe}]` 一节 —— 发版必须写更新记录")
        ok = False

    print("✓ 版本号一致" if ok else "✗ 检查失败")
    return 0 if ok else 1


def bump(new):
    if not SEMVER.match(new):
        raise SystemExit(f"✗ 版本号必须是 x.y.z：{new}")

    # 1) 前端
    s = io.open(_p(FE_FILE), encoding="utf-8").read()
    s2 = re.sub(r"(APP_VERSION\s*=\s*')[^']+(')", rf"\g<1>{new}\g<2>", s, count=1)
    if s2 == s:
        raise SystemExit(f"✗ 改 {FE_FILE} 失败")
    io.open(_p(FE_FILE), "w", encoding="utf-8").write(s2)

    # 2) 后端
    s = io.open(_p(BE_FILE), encoding="utf-8").read()
    s2 = re.sub(r'(var\s+Version\s*=\s*")[^"]*(")', rf"\g<1>{new}\g<2>", s, count=1)
    if s2 == s:
        raise SystemExit(f"✗ 改 {BE_FILE} 失败")
    io.open(_p(BE_FILE), "w", encoding="utf-8").write(s2)

    # 3) CHANGELOG：在 `## [未发布]` 之后插入新节（已存在则跳过）
    s = io.open(_p(CL_FILE), encoding="utf-8").read()
    if f"## [{new}]" not in s:
        today = datetime.date.today().isoformat()
        marker = "## [未发布]"
        block = f"## [{new}] — {today}\n\n（待补充）\n\n"
        if marker in s:
            i = s.index(marker)
            j = s.index("\n---\n", i) + len("\n---\n\n")
            s = s[:j] + block + s[j:]
        else:
            s = s.replace("---\n", "---\n\n" + block, 1)
        io.open(_p(CL_FILE), "w", encoding="utf-8").write(s)

    print(f"✓ 已把版本号改为 {new}（前端 / 后端 / CHANGELOG 占位）")
    print("  → 记得把 CHANGELOG 里的「（待补充）」写成实际更新内容")


def main(argv):
    if "--print" in argv:
        print(read_frontend())
        return 0
    if "--bump" in argv:
        i = argv.index("--bump")
        if i + 1 >= len(argv):
            raise SystemExit("用法：--bump x.y.z")
        bump(argv[i + 1])
        return check()
    return check()


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

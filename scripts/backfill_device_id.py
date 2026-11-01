#!/usr/bin/env python3
"""历史短信 device_id 回填工具

用途：
    新版本给 text_messages 增加了 device_id 列，但历史数据该列为 NULL。
    本脚本按「发件人号码 → 设备」的映射把历史数据归属到具体设备。

用法（在宿主机上执行，容器需已启动过一次完成迁移）：
    python3 backfill_device_id.py --db ./data/app.db --dry-run   # 先看效果
    python3 backfill_device_id.py --db ./data/app.db             # 实际写入

映射规则（--rules 可覆盖）：
    联通 15105513113  → sms1
    其他中国大陆号段   → sms①默认
    无法判断           → 保持 NULL（不猜测）

安全：
    - 只 UPDATE device_id IS NULL 的行，不覆盖已有值
    - 写入前自动备份到 <db>.bak-<时间戳>
    - --dry-run 可以只统计不写库
"""
import argparse
import os
import shutil
import sqlite3
import sys
import time
from collections import Counter

# 默认映射：号码前缀 → 设备 ID
#
# ⚠️ 按你实际的 config.yaml 调整。以下映射基于 192.168.11.21 线上实测：
#    - 该机会话里的短信来自「联通 15105513113」(sms1) 与「O2 UK 漫游卡」(sms2)
#    - 10086 / 1008640086 / 10086910 也是 sms1 收到的（联通卡收移动服务号，
#      属于携号转网/异网短信场景，不要想当然按运营商映射到「移动卡」）
#    - O2 UK 卡收到的短信多为境外号码（+44...）或空发件人，难以反推，保持 NULL
DEFAULT_RULES = [
    # (匹配模式, 设备ID, 说明)
    ("15105513113", "sms1", "联通 15105513113"),
    ("10010", "sms1", "联通服务号"),
    ("1065", "sms1", "联通政企短信"),
    ("10086", "sms1", "移动服务号（本机由联通卡接收）"),
    ("1062", "sms1", "移动短信端口"),
]


def match_device(sender: str, rules):
    """返回匹配到的设备 ID，匹配不到返回 None。"""
    if not sender:
        return None
    s = sender.lstrip("+").replace(" ", "").replace("-", "")
    # 中国号码：+8615105513113 / 8615105513113 / 15105513113
    if s.startswith("86") and len(s) > 11:
        s = s[2:]
    for pattern, device_id, _ in rules:
        if s.startswith(pattern) or pattern in s:
            return device_id
    return None


def main():
    ap = argparse.ArgumentParser(description="回填 text_messages.device_id")
    ap.add_argument("--db", default="./data/app.db", help="SQLite 数据库路径")
    ap.add_argument("--dry-run", action="store_true", help="只统计不写入")
    ap.add_argument("--default-device", default="",
                    help="匹配不到时统一归到这个设备（默认留空=保持 NULL）")
    args = ap.parse_args()

    if not os.path.exists(args.db):
        print(f"❌ 数据库不存在: {args.db}")
        return 1

    conn = sqlite3.connect(args.db)
    conn.row_factory = sqlite3.Row

    cols = {r[1] for r in conn.execute("PRAGMA table_info(text_messages)")}
    if "device_id" not in cols:
        print("❌ text_messages 没有 device_id 列。请先启动一次新版本完成迁移。")
        return 1

    total = conn.execute("select count(*) from text_messages").fetchone()[0]
    already = conn.execute(
        "select count(*) from text_messages where device_id is not null and device_id != ''"
    ).fetchone()[0]
    pending = conn.execute(
        "select count(*) from text_messages where device_id is null or device_id = ''"
    ).fetchone()[0]

    print(f"总消息数      : {total}")
    print(f"已有 device_id: {already}")
    print(f"待回填        : {pending}")

    if pending == 0:
        print("✅ 无需回填")
        return 0

    # 统计待回填的发件人分布
    rows = conn.execute(
        "select id, \"from\" as sender from text_messages "
        "where device_id is null or device_id = ''"
    ).fetchall()

    plan = []          # (msg_id, device_id)
    unmatched = Counter()
    for r in rows:
        dev = match_device(r["sender"] or "", DEFAULT_RULES)
        if dev:
            plan.append((r["id"], dev))
        elif args.default_device:
            plan.append((r["id"], args.default_device))
        else:
            unmatched[r["sender"] or "(空)"] += 1

    counter = Counter(dev for _, dev in plan)
    print("\n=== 回填计划 ===")
    for dev, cnt in counter.most_common():
        print(f"  {dev}: {cnt} 条")
    if unmatched:
        print(f"\n无法判断（保持 NULL）: {sum(unmatched.values())} 条")
        for sender, cnt in unmatched.most_common(10):
            print(f"  {sender}: {cnt} 条")

    if args.dry_run:
        print("\n[--dry-run] 未写入数据库")
        return 0

    if not plan:
        print("\n没有可回填的记录")
        return 0

    # 备份
    backup = f"{args.db}.bak-{time.strftime('%Y%m%d-%H%M%S')}"
    shutil.copy2(args.db, backup)
    print(f"\n已备份: {backup}")

    with conn:
        conn.executemany(
            "update text_messages set device_id = ? where id = ? "
            "and (device_id is null or device_id = '')",
            [(dev, mid) for mid, dev in plan],
        )

    after = conn.execute(
        "select count(*) from text_messages where device_id is not null and device_id != ''"
    ).fetchone()[0]
    print(f"✅ 回填完成，现有 device_id 的记录: {after} / {total}")
    return 0


if __name__ == "__main__":
    sys.exit(main())

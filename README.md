<div align="center">

# UART SMS Gateway

**合宙Air780系列短信网关 —— 一个容器管理多张 SIM 卡，内置 AK/SK 开放接口**

基于 [dushixiang/uart_sms_forwarder](https://github.com/dushixiang/uart_sms_forwarder) 二次开发

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)
![TypeScript](https://img.shields.io/badge/TypeScript-5.9-3178C6?logo=typescript&logoColor=white)
![Tailwind](https://img.shields.io/badge/Tailwind-4-06B6D4?logo=tailwindcss&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)

</div>

---

## ⚠️ 关于本项目

本项目是 **[dushixiang/uart_sms_forwarder](https://github.com/dushixiang/uart_sms_forwarder)** 的**二次开发分支**（fork & derivative work）。

原项目是成熟的**单设备** UART 短信转发器，本项目在其基础上：

- 重写了**多设备**内核（同时管理多张 SIM 卡）
- 补齐了原 fork 中被删掉的一批**安全网**（写锁、短写保护、状态快照等）
- 新增了**开放接口（AK/SK 签名）**与**在线改密**两个能力
- 按原项目的设计语言重做了**整套前端 UI**

> 原项目的全部核心逻辑、串口协议（`main.lua` 固件侧脚本）、整体架构均来自原作者 **dushixiang**。
> 上游项目的贡献者同样值得致谢。本项目**不是**原项目的官方版本，也不代表原作者观点。

上游项目：<https://github.com/dushixiang/uart_sms_forwarder>

---

## 这个项目解决什么问题

你有一堆 Air780/AirM2M 之类的 USB 蜂窝模块，想用它们：

1. **收**验证码 / 通知短信，转发到钉钉、飞书、企业微信、Telegram、邮件或自定义 Webhook
2. **发**短信，并且能指定从**哪一张卡**发出（避免误用境外漫游卡产生国际资费）
3. 让**别的系统**通过 API 调用你发验证码 —— 像阿里云/腾讯云短信那样，但卡是你自己的

本项目的第三个能力是原项目没有的，也是它和「短信平台」定位最接近的地方。

---

## 核心特性

### 🔌 多设备内核

| 能力 | 说明 |
|---|---|
| 多卡并行 | 一个进程同时驱动多张 SIM 卡，各自独立连接、独立限流、独立状态 |
| `device_id` 全链路必填 | 发信 / 飞行模式 / 重启 / 定时任务都必须指定设备，留空直接 `400` 并列出可选设备 |
| 确定性设备顺序 | 按配置声明顺序遍历，不再依赖 Go map 的随机顺序（旧实现「不指定设备」= **随机选卡**） |
| by-path 绑定 | 用 `/dev/serial/by-path/...` 而非 `/dev/ttyACM*`，避免插拔后设备编号漂移导致「哪张卡是哪张」错乱 |
| 消息溯源 | `text_messages.device_id` 落库，UI 上每条消息带来源设备标签 |

### 🔐 开放接口（AK/SK）

让任意语言、任意系统调用你发短信，签名方式类似阿里云。

```http
POST /openapi/v1/sms/send

X-Api-Key:   ak_xxxxxxxxxxxxxxxx
X-Timestamp: 1793600000
X-Nonce:     a1b2c3d4e5f6...
X-Signature: <HMAC-SHA256 hex>
```

签名串：

```
StringToSign = METHOD + "\n" + PATH + "\n" + SORTED_QUERY
             + "\n" + SHA256_HEX(body) + "\n" + TIMESTAMP + "\n" + NONCE
Signature    = HMAC-SHA256(SecretKey, StringToSign)   // hex 小写
```

请求体（模板模式 / 直发模式二选一）：

```json
{ "phone": "13800138000", "template": "verify_code", "params": { "code": "123456" } }
{ "phone": "13800138000", "content": "直接写完整内容" }
```

**配套能力**

- **密钥管理页**：新建 / 停用 / 删除 / 重置 Secret；Secret 只显示一次，库里存密文 + 末 4 位提示
- **短信模板**：`{code}` 占位符，参数自动解析；模板可单独启停
- **三层防护**：IP 白名单（CIDR） + 单密钥每分钟限流 + 单设备令牌桶
- **防重放**：时间戳 5 分钟窗口 + Nonce 去重
- **调用记录**：手机号与正文**落库即脱敏**

错误码：`UNAUTHORIZED` `INVALID_PHONE` `TEMPLATE_NOT_FOUND` `TEMPLATE_PARAM_MISSING` `PHONE_TOO_FREQUENT` `PHONE_DAILY_LIMIT` `KEY_RATE_LIMIT` `DEVICE_UNAVAILABLE`

### 🔑 在线改密

`/account` 页面可直接改管理员密码，**改完立即生效、重启不丢**。

设计要点：`config.yaml` 在容器里是只读挂载、且启动时解析完毕，改它无法即时生效。因此口令哈希存在数据库 `user_credentials` 表，`config.yaml` 的 `Users` 仅作为**首次启动的种子**（库里已有记录则永不覆盖）。

强度要求：≥8 位、≤72 位（bcrypt 上限）、必须含字母 + 数字、不能与旧密码相同。

### 🛡️ 可靠性（相对上游 fork 补齐）

| 问题 | 处理 |
|---|---|
| 并发写串口导致帧交错 | 恢复 `writeMu` 写锁 + `writeAll` 短写循环 |
| 发信流程竞态 | 恢复 `smsSendMu` 整条流程串行化 |
| `GetStatus` 返回共享指针 | 返回副本，避免多卡状态串台（A 卡状态里出现 B 卡 ICCID） |
| 配置缺字段导致 panic | `cfgString`/`cfgBool` 安全取值；通知 goroutine 加 `recover` |
| 凭据进日志 | 删除明文打印 |
| 日志刷屏泄隐私 | 串口原始数据只打字节数，不打 IMSI/ICCID/正文 |
| 帧解析遇换行错位 | `FrameReader` 按 `SMS_START:`/`SMS_END` 边界扫描，正确处理半包/粘包/正文含换行 |
| 断连高速空转 | 断连返回 error 走 backoff |
| 发送状态永久卡 `sending` | 90 秒超时转 `unknown` |
| MQTT/定时任务随机选卡 | 定时任务 `deviceId` 必填 |

**测试**：33+ 用例覆盖写锁、帧解析、限流、device_id 必填、凭据种子与持久化。

---

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.25 · Echo v4 · GORM · SQLite · zap · go.bug.st/serial |
| 前端 | React 19 · TypeScript 5.9 · Vite 7 · Tailwind CSS 4 · Radix UI · TanStack Query |
| 部署 | Docker（单二进制 + `go:embed` 内嵌前端，无 nginx 依赖） |

---

## 快速开始

### 1. 准备配置

```bash
cp config.example.yaml config.yaml
```

必改三处：

```yaml
App:
  JWT:
    Secret: "<openssl rand -base64 32>"
  Users:
    admin: "<bcrypt 哈希>"
log:
  level: info
```

生成 bcrypt 哈希：

```bash
docker run --rm httpd:alpine htpasswd -nbBC 12 "" '你的密码' | tr -d ':\n'
```

### 2. 配置串口设备

```bash
# 先看实际映射（强烈建议用 by-path，不要用 ttyACM*）
ls -l /dev/serial/by-path/
```

```yaml
App:
  Serial:
    Devices:
      - ID: "sms1"
        Name: "联通3113"
        Port: "/dev/serial/by-path/pci-0000:07:1b.0-usb-0:2:1.6"
        Description: "中国联通"
        Enabled: true
```

> ⚠️ 多个 AirM2M 模块的 `ID_SERIAL` 完全相同，`by-id` 不可用，只能用 `by-path`。
> 详细说明见 [`docs/port-binding-by-path.md`](docs/port-binding-by-path.md)。

### 3. 启动

```bash
docker compose up -d
curl http://127.0.0.1:8080/health
```

`docker-compose.yml` 的关键点：`network_mode: host`、`privileged: true`、挂载 `/dev/serial:ro`。

### 4. 从源码构建

```bash
# 前端（产物需提交到仓库，因为 go:embed 依赖它）
cd web && npm ci && npm run build

# 后端（-X 注入版本号）
cd .. && CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/dushixiang/uart_sms_forwarder/internal/version.Version=v1.0.0" -o uart_sms_forwarder ./cmd/serv
```

或直接用多阶段构建：

```bash
docker build -f Dockerfile.multi -t uart-sms-gateway:latest .
```

---

## 目录结构

```
├── cmd/serv/                 入口
├── config/                   配置解析
├── internal/
│   ├── handler/              HTTP 处理器
│   │   ├── auth_handler.go       登录 / OIDC
│   │   ├── credential_handler.go 改密（/api/auth/me、/api/auth/password）
│   │   ├── openapi_handler.go    对外发信（/openapi/v1/sms/send）
│   │   ├── openapi_admin_handler.go 密钥与模板管理
│   │   └── serial_handler.go     设备控制
│   ├── service/
│   │   ├── serial_service.go     串口主循环（写锁 / 短写 / 重连）
│   │   ├── serial_frame.go       帧解析器（边界扫描）
│   │   ├── device_manager.go     多设备管理
│   │   ├── credential_service.go 口令（库存储 + 种子）
│   │   ├── api_key_service.go    AK/SK（HMAC + nonce）
│   │   ├── rate_limit.go         每设备令牌桶
│   │   ├── template_service.go   短信模板
│   │   ├── notifier.go           通知渠道
│   │   └── scheduler_service.go  定时任务
│   ├── models/               数据模型
│   └── middleware/           JWT
├── web/                      React 前端（dist 入库供 embed）
├── main.lua                  固件侧串口协议脚本（来自上游）
├── docs/                     文档
└── scripts/                  数据回填等工具
```

---

## API 速览

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/login` | 登录，返回 JWT |
| `GET` | `/api/auth/me` | 当前用户 |
| `POST` | `/api/auth/password` | 修改密码 |
| `GET` | `/api/serial/status` | 全部设备状态 |
| `POST` | `/api/serial/sms` | 发短信（`device_id` 必填） |
| `POST` | `/api/serial/flymode` | 飞行模式 |
| `POST` | `/api/serial/reboot` | 重启模块 |
| `GET` | `/api/messages/conversations` | 会话列表 |
| `GET` | `/api/openapi/keys` | 密钥管理（CRUD） |
| `GET` | `/api/openapi/templates` | 模板管理（CRUD） |
| `GET` | `/api/openapi/logs` | 调用记录 |
| `POST` | `/openapi/v1/sms/send` | **对外发信（AK/SK 签名，无需 JWT）** |

---

## 与上游的差异

| 能力 | 上游 v0.2.1 | 本项目 |
|---|---|---|
| 多设备 | ❌ 单串口 | ✅ 多卡并行 |
| 对外发信 API | ❌ | ✅ AK/SK 签名 + 模板 |
| 在线改密 | ❌ | ✅ 库存储 |
| 消息设备溯源 | ❌ | ✅ `device_id` 落库 |
| 写锁 / 短写保护 | ✅ | ✅ 保留 |
| 帧解析 | ⚠️ `ReadString('\n')` | ✅ 边界扫描 |
| 前端 UI | 基础 | 重构（侧栏 + 设备矩阵） |

---

## 常见问题

**Q：为什么不指定设备也能发？**
现在不行了 —— `device_id` 全链路必填。多卡场景下「随机选一张」意味着可能从境外漫游卡发出，产生国际资费。

**Q：改了 `config.yaml` 的密码为什么不生效？**
改密请看 `/account` 页面。`config.yaml` 的 `Users` 只在**数据库没有该用户记录时**作为种子使用一次。

**Q：容器里看不到串口？**
检查三件事：① `config.yaml` 用的是 `by-path`；② compose 挂载了 `/dev/serial:/dev/serial:ro`；③ 宿主机 `ModemManager` 已禁用（会争抢重置 modem）。

**Q：`/openapi` 直接访问 404？**
已知问题。后端 `/openapi` 路由组优先匹配了裸路径，只能从侧栏进入。`/openapi/v1/*` 接口本身不受影响。

---

## 致谢与许可

- **上游项目**：[dushixiang/uart_sms_forwarder](https://github.com/dushixiang/uart_sms_forwarder) —— 本项目的根基，串口协议与整体架构均源自于此
- 本项目在 MIT 协议下发布，与上游保持一致

```
MIT License

Copyright (c) dushixiang (upstream project)
Copyright (c) st1341815 (derivative work)

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.
```

---

<div align="center">
<sub>基于 dushixiang/uart_sms_forwarder 二次开发 · MIT</sub>
</div>

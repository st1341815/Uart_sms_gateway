<div align="center">

# UART SMS Gateway

**合宙Air780系列短信网关 —— 一个容器管理多张 SIM 卡，内置 AK/SK 开放接口**

基于 [dushixiang/uart_sms_forwarder](https://github.com/dushixiang/uart_sms_forwarder) 二次开发

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)
![TypeScript](https://img.shields.io/badge/TypeScript-5.9-3178C6?logo=typescript&logoColor=white)
![Tailwind](https://img.shields.io/badge/Tailwind-4-06B6D4?logo=tailwindcss&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-green)

[![Docker Hub](https://img.shields.io/badge/Docker%20Hub-13uncle%2Fuart__sms__gateway-2496ED?logo=docker&logoColor=white)](https://hub.docker.com/r/13uncle/uart_sms_gateway)
[![GHCR](https://img.shields.io/badge/GHCR-st1341815%2Fuart__sms__gateway-181717?logo=github&logoColor=white)](https://github.com/st1341815/Uart_sms_gateway/pkgs/container/uart_sms_gateway)

```bash
docker pull 13uncle/uart_sms_gateway:latest(或 docker pull ghcr.io/st1341815/uart_sms_gateway:latest）
```

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

**测试**：78 个用例覆盖写锁、帧解析、限流、device_id 必填、凭据种子与持久化、AK/SK 签名与防重放、登录限流、JWT 密钥持久化。CI（`.github/workflows/ci.yml`）会跑 `go vet` + `go test` + 前端 `eslint` / `tsc -b && vite build`。

### 🚨 安全基线

| 项 | 现状 |
|---|---|
| 容器特权 | 不使用 `privileged`；`devices:` + `/dev/serial:ro` + `no-new-privileges:true` 即可 |
| JWT 密钥 | 留空则首次启动生成并持久化到 `data/.jwt_secret`（0600）复用；过长/占位值（如 `***REDACTED***`）会拒绝启动。它同时是 AK/SK 密文的派生源，**换钥 = 所有 Secret 失效** |
| 登录爆破 | 同 IP+账号 5 次、同 IP 跨账号 20 次失败 → 锁 5 分钟，返回 `429` + `Retry-After` |
| 日志 | 不打印 `Authorization` 头原文、token、口令、短信正文/IMSI/ICCID |
| 请求体 | 上限 64 KB；`ReadHeaderTimeout` 10s（防慢速头攻击） |
| AccessKey 探测 | 不存在 / 已停用 / 签名错误 一律返回「认证失败」，不做区分 |
| 示例配置 | 不含真实号码、内网 IP、部署路径 |

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

必改两处 + 一处可选：

```yaml
App:
  JWT:
    # 可留空：首次启动自动生成 32 字节随机密钥，写到 ./data/.jwt_secret（0600）后复用
    Secret: ""
  Users:
    admin: "<bcrypt 哈希>"
log:
  level: info
```

> ⚠️ JWT Secret 同时是 AK/SK 密文的加密密钥派生源 —— **换一次，库里所有 AccessKey 的
> Secret 就再也解不开**（业务方必须逐个重置）。所以：留空让它自动持久化是安全的；
> 手动填写的话请把 `data/.jwt_secret`（或你自己的密钥）和数据库一起备份。

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
        Name: "卡1"
        Port: "/dev/serial/by-path/pci-0000:07:1b.0-usb-0:2:1.6"
        Description: "中国联通"
        Enabled: true
```

> ⚠️ 多个 AirM2M 模块的 `ID_SERIAL` 完全相同，`by-id` 不可用，只能用 `by-path`。
> 详细说明见 [`docs/port-binding-by-path.md`](docs/port-binding-by-path.md)。

### 3. 启动

**方式 A：直接用已发布的镜像（推荐）**

镜像在 Docker Hub 和 GHCR 双发布，均为 `linux/amd64` + `linux/arm64` 双架构：

```bash
# Docker Hub
docker pull 13uncle/uart_sms_gateway:latest

# 或 GHCR
docker pull ghcr.io/st1341815/uart_sms_gateway:latest
```

`docker-compose.yml` 里把 `image:` 换成对应地址后：

```bash
docker compose up -d
curl http://127.0.0.1:8080/health
```

> 容器内已内置 `config.example.yaml`；首次运行请把 `config.yaml` 挂载进去（`:ro`），
> 并挂载 `/dev/serial:/dev/serial:ro` 以便解析 `by-path` 符号链接。

**方式 B：本地构建**

```bash
docker compose -f docker-compose.yml up -d --build
```

`docker-compose.yml` 的关键点：`network_mode: host`（IP 白名单要用真实来源 IP）、`devices:` + 挂载 `/dev/serial:ro`（解析 by-path）、`no-new-privileges:true`。
**不需要 `privileged: true`** —— 特权模式会把宿主机内核能力整体交出去，而串口访问靠 `devices` 就够。

### 4. 从源码构建

```bash
# 1) 前端
cd web && npm ci && npm run build

# 2) 同步到 go:embed 目录
#    ⚠️ web/assets.go 嵌的是 web/embed_dir，不是 web/dist。
#    漏了这一步，编出来的二进制里是「仓库里的旧前端」。
cd .. && rm -rf web/embed_dir && cp -a web/dist web/embed_dir

# 3) 后端
#    版本号已写死在 internal/version/version.go 的默认值里，
#    裸 go build 出来的二进制就带正确版本（不再依赖 -ldflags 注入）
CGO_ENABLED=0 go build -o uart_sms_forwarder ./cmd/serv
```

或者直接 `make build-server`（已把 `prepare-embed` 和 `check-version` 做成前置依赖，会自动同步并校验版本号）。

> **构建可复现**：`web/src/index.css` 里用 `@source not` 把 `web/embed_dir`、`web/dist`
> 排除出 Tailwind 的自动内容扫描。不加这两行的话，上一次构建的 bundle 会被当源码再扫一遍，
> 同一份源码会产出 72 KB / 84 KB 两种 CSS（而且每次构建都在往 CSS 里添无用 class）。
> 现在无论目录里有没有 `embed_dir`，`npm run build` 的产物哈希都一致。

或直接用多阶段构建：

```bash
docker build -f Dockerfile.multi -t uart-sms-gateway:latest .
```

> 国内网络下 `proxy.golang.org` / `registry.npmjs.org` 常常不通。
> `Dockerfile.multi` 的 `GOPROXY`、`NPM_REGISTRY` 做成了 build-arg，**默认就是
> `goproxy.cn` / `npmmirror`**；需要直连官方时显式覆盖：
> `--build-arg GOPROXY=https://proxy.golang.org,direct`。

### 5. 发版流程（版本号只有一处定义，别手改三遍）

版本号存在于三处，**必须同步**，否则会出现「侧边栏显示的版本 ≠ 实际代码」：

| 位置 | 作用 |
|---|---|
| `web/src/lib/version.ts` 的 `APP_VERSION` | 侧边栏 / 分享页等 UI 展示 |
| `internal/version/version.go` 的 `Version` 默认值 | `GET /api/version`、裸 `go build` 的产物 |
| `CHANGELOG.md` 里对应版本的一节 | 更新记录（写清改了什么） |

一条命令改完三处：

```bash
make bump-version V=1.2.0     # 改号 + 在 CHANGELOG 插入占位节
make check-version            # 校验三处一致（不一致退出码 1）
make version                  # 打印当前版本号
```

`check-version` 已挂到 `build-web` / `build-server` / `build-servers` 的前置依赖，
以及 CI 的第一步 —— 漏改版本号，构建直接失败。

> 历史教训：1.1.0 之后连续发了 2 个版本忘了改号，Docker 镜像都重建了，
> 侧边栏还显示 v1.1.0。这就是 `CHANGELOG.md` 和这套检查的由来。

### 6. 发版流程

发版走 **GitHub**。原来这一套要手敲十几条 `ssh` / `docker` / `git` 命令，
现在固化成了一条命令（`scripts/release.sh`，Makefile 里叫 `make release`）：

```bash
# 1) 改版本号（三处一起改，再补 CHANGELOG）
make bump-version V=1.4.2
$EDITOR CHANGELOG.md

# 2) 一条命令走完全流程
GITHUB_TOKEN=ghp_xxx make release
```

脚本按 10 个阶段执行，任一步失败即停下并给出可操作的提示：

| 阶段 | 做什么 | 失败时 |
|---|---|---|
| 1 预检 | 版本三处一致、依赖命令、远程 SSH + docker compose、工作区是否干净 | 直接退出，不产生任何副作用 |
| 2 前端 | `npm run build` → `make prepare-embed`，校验 `web/embed_dir/index.html` | 停 |
| 3 同步 | tar 打包源码（排除 `node_modules`/`dist`/`.git`/`bin`）→ `scp` → 远端解包到 `.build-$VERSION` | 停，不碰线上 |
| 4 构建 | 远端 `docker build -f Dockerfile.multi --build-arg WEB_PREBUILT=1` | 打印日志末尾 30 行 |
| 5 部署 | 记下当前镜像 tag → 打回滚点 `pre-$VERSION` → 备份 compose → 切镜像 → `compose up -d` | — |
| 6 验证 | 轮询 `/health`（20 × 2s），再 `docker inspect` 核对运行镜像 | **自动回滚**到上一版并重启 |
| 7 tag | `git tag -a v$VERSION` | 已存在且指向别的 commit 时拒绝覆盖 |
| 8 发布包 | 从刚构建的镜像 `docker cp` 出二进制 → 打成 tar.gz + `SHA256SUMS.txt` | 停（不留半成品包） |
| 9 推送 | push 分支 + tag，建 Release 并逐个上传附件 | 附件失败只告警，正文照发；`422 already_exists` 视为跳过 |
| 10 清理 | 删远端 `.build-$VERSION`、临时二进制与本地 tar 包 | — |

常用参数：

```bash
./scripts/release.sh --dry-run              # 只演练，不构建不部署不推送
./scripts/release.sh --skip-publish         # 构建 + 部署，但不推 GitHub
./scripts/release.sh --skip-binaries        # 发布，但不生成/上传二进制附件
./scripts/release.sh --skip-build           # 复用远端已有构建目录（调试用）
./scripts/release.sh --commit "说明"        # 工作区脏时代提交，保证 tag 与内容一致
./scripts/release.sh --version 1.4.2        # 不读文件里的版本号，显式指定
```

默认配置（全部可用环境变量覆盖）：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `REMOTE_HOST` | `s21` | 构建/部署机（ssh config 别名或 `user@ip`） |
| `SSH_CONFIG` | `$HOME/.ssh/config`（存在则用） | 见下方踩坑注记 |
| `REMOTE_DIR` | `/home/unit3/sms-multi` | 线上部署目录 |
| `CONTAINER` | `sms-multi` | 容器名 |
| `IMAGE_REPO` | `smstext-multi-device` | 镜像仓库名 |
| `HEALTH_URL` | `http://$REMOTE_HOST:8080/health` | 健康检查地址 |

> **本机踩坑：OpenSSH 不读 `$HOME/.ssh/config`。** 它按 passwd 里记录的 home 找配置，
> 而本机 `HOME` 已被 Octop 挪到 `/vol1/@appshare/octop-native/data`，
> 于是 `ssh s21` 直接报 `Could not resolve hostname`。脚本因此默认加 `-F $HOME/.ssh/config`
> 兜底；`git push` 走 `https + token`，不受影响。

> **防呆：** 工作区不干净会直接拒绝发版——否则 tag 指向的 commit 和实际构建的源码对不上，
> 事后回溯版本会完全错乱。急用时用 `--commit "说明"` 让脚本代提交。

仓库：<https://github.com/st1341815/Uart_sms_gateway>

发布包由脚本自动生成并挂到 Release：`dist-release/` 下的
`uart_sms_forwarder-<ver>-linux-amd64.tar.gz` 与 `SHA256SUMS.txt`。

> **二进制不是重新交叉编译的，而是从刚构建好的镜像里 `docker cp` 出来的。**
> 两个好处：发布包与线上跑的可执行文件**字节一致**（不会出现「镜像里是 A、包里是 B」）；
> 构建机不必装 Go（s21 实测没有 `go`，Dockerfile.multi 本来就是在容器里编的）。
> `internal/version` 的默认值就是发行版本号，所以裸 `go build` 出来的二进制自带正确版本。

只想单独出包（不部署、不发版）：

```bash
BINARY=<已有二进制路径> ./scripts/build-release.sh 1.4.2
```

> 曾经有一套「推 tag → 本地 Gitea Actions 自动出镜像 + 发布包 + Release」的流水线，
> 2026-11-03 整体移除：act_runner + buildx + 明文 registry 的组合在本机反复失败
> （先连不上 registry，改地址后撞 https/http 不匹配，换回普通 docker build 才通，
> 但整条链路仍然脆弱）。需要自动化时再从头设计，别照搬旧配置。

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
├── web/                      React 前端（embed_dir 入库供 embed）
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
已修复（v1.0.1）。根因是静态中间件的 `Skipper` 按 `/openapi` 前缀整体放行，把前端 SPA 路由 `/openapi` 也一起跳过了，于是它拿不到 `index.html` 回退。现在只放行 `/openapi/`（带斜杠），裸路径正常回退到前端。

---

## 安全加固记录（v1.0.1）

第三方安全评估报告的建议**没有照单全收**：核实后成立的问题才修，判断错误的条目没有改
（改了反而是倒退）。逐条结论与改法见 [`FIXES-2026-11-01.md`](./FIXES-2026-11-01.md) 第六章。

**已修复（真实存在、影响可验证）**

| 级别 | 问题 | 处理 |
|---|---|---|
| P0 | `JWT.Secret` 为空时每次启动生成随机 `uuid` —— 而它同时是 AK/SK 密文的派生源，重启一次库里所有 Secret 永久解不开 | 改为生成 32 字节随机密钥落盘（`data/.jwt_secret`，0600）并复用 |
| P0 | 真实手机号 / 内网 IP / 部署路径进了公开仓库 | 全部替换为示例值 |
| P1 | JWT 中间件把 `Authorization` 原文写进日志 | 只记泛化原因，不输出任何凭据 |
| P1 | 登录接口零限流，可无限爆破 | 同 IP+账号 5 次 / 同 IP 跨账号 20 次 → 锁 5 分钟（429 + `Retry-After`） |
| P1 | 前端构建产物与二进制里的不一致 | Dockerfile / Makefile 强制 `dist → embed_dir`，并做成构建前置依赖 |
| P2 | 无请求体上限、无读超时 | `BodyLimit 64K`、`ReadHeaderTimeout 10s`、`IdleTimeout 60s` |
| P2 | 停用 / 不存在 / 签名错 返回不同文案 → 可枚举 AK | 统一「认证失败」 |
| P2 | CI 只构建镜像、从不跑测试 | 新增 `ci.yml`：`go vet` + `go test` + 前端 lint + build |

**报告里判断错误、因此没有改的**（详见 FIXES 第六章）

- 「帧解析用 `ReadString('\n')`」——那是**上游**的写法，本项目已重写为边界扫描（7 个测试覆盖半包/粘包/正文含换行）。
- 「SQLite 单写者 = 吞吐瓶颈」——发信速率被限流压到每设备 3 秒 1 条，压力不在库上；真实短板是**单点无 failover**，那是架构问题，不是换个数据库能解决的。
- 「`web/dist` 入库」——`git ls-files web/dist` 为 0，`.gitignore` 里就有 `dist`；真正入库的是 `web/embed_dir`。
- 「主密钥来源未交代」——代码首行即写明由 JWT Secret 派生（`SHA256("sms-api-secret-encryption:" + jwtSecret)` → AES-256-GCM）。

**已知残留（需要你决策）**

1. **历史 commit 里还能 `git log -p` 查到原手机号** —— 当前 HEAD 已清理，但历史没重写。彻底清除要 `git filter-repo` + force push（影响所有 clone/fork）。**建议优先级：先换号，再考虑重写历史。**
2. `Logout` 空实现 —— 改密码后旧 JWT 仍然有效，直到 7 天过期。
3. Nonce 缓存在内存 —— 重启后 5 分钟窗口内的请求理论上可重放一次（已在代码注释里明示取舍）。

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

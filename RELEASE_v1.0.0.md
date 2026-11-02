# v1.0.0

首个正式发布版本。基于 [dushixiang/uart_sms_forwarder](https://github.com/dushixiang/uart_sms_forwarder) 二次开发。

## 亮点

### 🔌 多设备内核
- 一个进程管理多张 SIM 卡，每张卡独立连接、独立限流、独立状态
- `device_id` **全链路必填**：发信 / 飞行模式 / 重启 / 定时任务，留空返回 400 并列出可选设备
- 设备遍历改为**配置声明顺序**，修复旧实现「不指定设备 = 随机选卡」（Go map 遍历顺序不稳定）
- `by-path` 串口绑定，避免插拔后 `ttyACM*` 编号漂移
- `text_messages.device_id` 落库，前端消息气泡显示来源设备标签

### 🔐 开放接口（AK/SK 签名）
外部系统可用任意语言调用本网关发送短信验证码：

```
POST /openapi/v1/sms/send
X-Api-Key / X-Timestamp / X-Nonce / X-Signature
```

- **签名算法**：HMAC-SHA256，签名串 = `METHOD\nPATH\nQUERY\nSHA256(body)\nTimestamp\nNonce`
- **密钥管理**：新建 / 停用 / 删除 / 重置 Secret；Secret 仅显示一次，库内存密文 + 末 4 位提示
- **短信模板**：`{code}` 占位符自动解析，支持启停
- **三层防护**：IP 白名单（CIDR）+ 单密钥每分钟限流 + 单设备令牌桶
- **防重放**：5 分钟时间戳窗口 + Nonce 去重
- **调用记录**：手机号与正文落库即脱敏（`189****1111`）
- 内置 Bash / Python / Node.js 三语言示例与完整错误码说明

### 🔑 在线改密
- `/account` 页面直接改管理员密码，立即生效、重启不丢
- 口令哈希存数据库 `user_credentials` 表；`config.yaml` 的 `Users` 仅作首次启动种子
- 强度校验：≥8 位 / ≤72 位 / 含字母+数字 / 不与旧密码相同
- 用户不存在时做等时 bcrypt 比较，防时序侧信道探测

### 🛡️ 可靠性补齐
| 问题 | 修复 |
|---|---|
| 并发写串口帧交错 | 恢复 `writeMu` 写锁 + `writeAll` 短写循环 |
| 发信流程竞态 | 恢复 `smsSendMu` 串行化 |
| 多卡状态串台 | `GetStatus` 返回副本 |
| 配置缺字段 panic | `cfgString`/`cfgBool` 安全取值 + goroutine recover |
| 凭据进日志 | 删除明文打印 |
| 日志泄隐私 | 串口数据只打字节数 |
| 正文含换行导致帧错位 | `FrameReader` 边界扫描，处理半包/粘包 |
| 断连高速空转 | 断连返回 error 走 backoff |
| 发送状态卡 `sending` | 90 秒超时转 `unknown` |

### 🎨 前端重构
- 侧栏 + 顶栏统一骨架，7 个页面：概览 / 短信中心 / 串口控制 / 通知渠道 / 计划任务 / 开放接口 / 账号安全
- 概览页「设备矩阵」：每张卡独立卡片，显示在线状态、运营商、号码、信号条
- 参考上游项目设计语言重做，浅色 + 深色侧栏

## 技术栈

Go 1.25 · Echo v4 · GORM · SQLite · zap　/　React 19 · TypeScript 5.9 · Vite 7 · Tailwind CSS 4

## 测试

```
go build ./...   ✅
go vet ./...     ✅
go test ./...    ✅  33+ 用例（写锁 / 帧解析 / 限流 / device_id 必填 / 凭据持久化 / 开放接口）
tsc -b           ✅
vite build       ✅
```

## 升级注意

- 数据库新增表 `user_credentials` / `api_keys` / `sms_templates` / `api_send_logs`，`text_messages` 新增 `device_id` 列（纯增量，GORM AutoMigrate 自动处理）
- 配置新增 `App.Serial.Devices` 多设备结构；旧的单设备 `Port` 字段仍保留解析兼容
- 前端产物 `web/dist` 已入库，干净 clone 即可 `go build`

## 致谢

本项目的全部根基来自 **[dushixiang/uart_sms_forwarder](https://github.com/dushixiang/uart_sms_forwarder)** —— 串口协议、固件侧 `main.lua`、整体架构均出自原作者。感谢 upstream 的贡献者。

本项目不是上游官方版本，也不代表原作者观点。

## License

MIT（与上游一致）

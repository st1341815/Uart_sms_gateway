# 更新记录

版本号规则：主版本.次版本.修订号（语义化版本）。
- 修订号：修 bug / 构建链 / 文案，不改功能与接口
- 次版本：新增功能（新页面、新接口、新 UI 元素），向后兼容
- 主版本：破坏性变更（配置格式、API 协议、数据库结构不兼容）

**每次发版必须同时做三件事**（`scripts/check_version.py` 会检查）：

1. 改 `web/src/lib/version.ts` 的 `APP_VERSION`
2. 改 `internal/version/version.go` 的 `Version` 默认值
3. 在本文件**顶部**加一节 `## [x.y.z] — YYYY-MM-DD`

> 历史教训：1.1.0 之后连续发了 2 个版本却没改版本号，侧边栏一直显示 v1.1.0，
> 与实际部署的代码对不上（用户发现）。此文件就是为此建立。

---

## [1.1.7] — 2026-11-03

短信中心：分组支持折叠，号码多时不用一路滚下去。

- `feat` 会话列表的主题分组头（按 SIM 卡/设备分组那一行）改为可点击按钮，
  点一下折叠/展开该组；左侧 `ChevronDown` 图标折叠时旋转 -90° 指右，
  150ms 过渡。带 `aria-expanded`，读屏能判断状态。
- `feat` 搜索框下方新增「N 个号码 · M 个会话」统计，右侧「全部折叠 / 全部展开」
  按钮联动所有分组（分组数 ≤1 时隐藏，省得白占一行）。
- `feat` 折叠后分组头下方显示「已折叠 N 个会话」，避免列表突然空一块让人以为没加载。
- `impl` 折叠状态用 `Set<string>` 存「已折叠的设备 ID」，默认空集 = 全部展开。
  `deviceId` 可能是空串（历史数据 / 已删设备），统一经 `groupKeyOf()` 映射成
  `__unknown__`，避免和真实 ID 撞键。
- `impl` 刻意**不做持久化**（不写 localStorage）：折叠是临时浏览状态，刷新恢复
  全展开更符合直觉；也不因折叠而清掉 `peerSelection`（正在读的会话不该被折叠动作打断）。
- `build` 版本号 bump 至 1.1.7（`web/src/lib/version.ts` + `internal/version/version.go`）。

---

## [1.1.6] — 2026-11-03

修 1.1.5 引入的「本机号码永远显示 —」：前后端字段命名不一致。

- `fix` 后端 `DeviceStatus` 的 JSON tag 是蛇形（`phone_number` / `is_roaming`），
  前端类型声明的是驼峰（`phoneNumber` / `isRoaming`），而 `apiClient` 是裸 `fetch`
  **不做任何字段名转换** → `device.phoneNumber` 永远 `undefined`，界面固定显示「—」。
  连带 `openPhoneDialog` 预填值也恒为空串，不输内容直接保存会误走「清除补录」分支，
  表现为「点补录就提示已清除」。
- `fix` 在 API 层统一映射：`getAllDevicesStatus` / `getDeviceStatus` 把后端原始
  响应经 `normalizeDeviceStatus` 转成前端驼峰结构；`setDevicePhoneNumber` 的返回值
  同步兼容 `phone_number` / `phoneNumber` 两种写法。
  改一处，串口控制 / 短信中心 / 仪表盘三个页面一起生效。
- `chore` 清空 `properties.device_phone_numbers` 的历史手工补录值：此前补录让
  sms1 填成了 `+8615505513113`（SIM 实读 `+8615105513113`，`1510` 误打成 `1550`），
  sms2（O2 UK 境外卡）也被套用了联通号。现在两张卡都回到「SIM 卡自动读取」，
  读不到时再手工补录。
- `build` 版本号 bump 至 1.1.6（`web/src/lib/version.ts` + `internal/version/version.go`）。

---

## [1.1.5] — 2026-11-03

多卡场景两处体验修正：本机号码可读/可补录，短信中心按卡分开看。

**1. 串口控制页的本机号码**

- `feat` 新增 `PUT /api/devices/:id/phone-number`：手工补录本机号码。
  背景是 `mobile.number(0)` 底层走 AT+CNUM 读 SIM 卡的 EF_MSISDN 字段——
  国内三大运营商会写，多数境外卡/漫游卡不写，返回空串不是 bug、改 Lua 也绕不过。
  所以给一条兜底路径：DB 覆盖号 > SIM MSISDN。
- `feat` 号码存 `properties` 表（`ID = device_phone_numbers`，值 `map[deviceID]number`），
  启动加载为覆盖层；`GetAllStatus` / `GetDeviceStatus` 都走 `displayPhoneNumber`，
  保证列表页与详情页一致。校验只允许数字、`+`、`-`、空格，按字符计长 ≤32；
  提交空串 = 清除补录、回退 SIM 读取值。先落库成功再更新内存，避免「重启后变回空」。
- `feat` 前端本机号码卡片右上角加铅笔按钮（hover/键盘聚焦显现），弹窗内补录，
  并 invalidate `allDevicesStatus` / `devices` 让短信中心的卡头一起刷新。

**2. 短信中心按卡分组**

- `feat` `Conversation` 新增 `deviceId`，会话唯一键从「对方号码」改为
  「设备 + 对方号码」：同一号码可能同时和两张卡有往来，只按号码聚合会把两条
  独立的通信线搅在一起。
- `feat` `GET /messages/conversations/:peer/messages` 与
  `DELETE /messages/conversations/:peer` 支持可选 `device_id`：
  带上只作用于该卡，不带维持旧行为（所有卡合并 / 全删）。
- `feat` 前端会话列表按设备分组，组头显示卡名 + 本机号码 + 在线状态 + 会话数，
  分组顺序跟 `devices` 配置顺序一致，历史数据（`deviceId` 为空）落「未标记设备」。
- `fix` 发送设备未手选时回落到「当前会话所属的卡」（`effectiveSendDeviceId`）——
  打开哪张卡的会话、回复就从哪张卡发出，避免忘选而回错卡产生国际资费；
  手选仍优先，切换会话不会覆盖用户的选择。
- `build` 版本号 bump 至 1.1.5（`web/src/lib/version.ts` + `internal/version/version.go`）。

---

## [1.1.4] — 2026-11-03

串口控制页支持修改 SIM 卡名称（界面上显示的「联通3113」这类别名）。

- `feat` 新增 `PUT /api/devices/:id/name`，修改设备显示名称；`DeviceManager.RenameDevice`
  校验非空、按**字符**计长（≤32，中文不会被算成 3 倍），先落库成功再更新内存，
  避免「界面显示新名、重启后变回旧名」。
- `feat` 名称存 `properties` 表（`ID = device_names`，值 `map[deviceID]name`），
  启动时加载为覆盖层。**没有走 config.yaml**：容器内它是只读挂载，程序写不回去。
  优先级：DB 覆盖名 > `config.yaml` 的 `Name` > 设备 ID。
- `feat` 前端 `SerialControl` 设备卡片加铅笔按钮（桌面端 hover/键盘聚焦显现，
  触屏常显），弹窗内改名即时生效，并 invalidate `allDevicesStatus` 让消息页
  的设备下拉一起刷新。
- `fix` `GET /api/devices` 的 `name` 字段改走 `GetDeviceDisplayName`，
  此前直接读 `config.DeviceConfig.Name`，会导致列表页显示旧名。
- `build` 版本号 bump 至 1.1.4（`web/src/lib/version.ts` + `internal/version/version.go`）。

---

## [未发布]

构建链瘦身：CI 不再容器内重复构建前端，镜像默认只出 amd64。

- `build` `Dockerfile.multi` 新增 `WEB_PREBUILT`（默认 `1`）：直接 `go:embed`
  CI 已产出的 `web/embed_dir`，容器内不再 `apk add git nodejs npm` + `npm ci`。
  本地无预构建产物时传 `--build-arg WEB_PREBUILT=0` 走旧路径。
- `ci` `release.yml` 新增 `env.IMAGE_PLATFORMS`（默认 `linux/amd64`），
  需要 `linux/arm64` 时改一个变量即可（arm64 阶段走 QEMU，耗时数倍）。
- `ci` buildx builder 改为「有注册就复用、没有才创建」；act_runner 作业容器
  `~/.docker/buildx` 不持久需重建时，只删容器、保留 `*_state` 卷，
  使 buildkit 缓存跨版本复用（此前每版 `rm` + `create` 全丢缓存）。
- `docs` `docs/gitea-release.md`、`README.md` 同步镜像架构口径与踩坑记录。

---

## [1.1.3] — 2026-11-03

补齐版本号治理，防止再出现「显示的版本 ≠ 实际代码」。

- `feat` 新增 `CHANGELOG.md`（本文件），回溯记录 1.0.0 → 1.1.3 全部版本
- `feat` `scripts/check_version.py` 升级：除前后端一致性外，
  还检查 **CHANGELOG 里有当前版本号的一节**（漏写记录 = 检查失败）
- `feat` 新增 `--bump x.y.z`：一条命令同时改前端 `APP_VERSION`、
  后端 `Version` 默认值、并在 CHANGELOG 插入占位节，三者不会再各改各的
- `feat` `--print`：给 `make version` 用，构建脚本可读当前版本号

## [1.1.2] — 2026-11-03

侧边栏版本号变成 GitHub 仓库入口。

- `feat` 版本号补回 `UART SMS Gateway` 全称，并链接到 GitHub 仓库
  （`target=_blank` + `rel="noopener noreferrer"`；仓库地址抽到 `APP_REPO_URL`）
- 提交：`09c826c`

## [1.1.1] — 2026-11-03

修侧边栏布局溢出。

- `fix` 版本号与「退出登录」并排时把按钮挤出 248px 侧栏 → 改为上下两行，
  版本号独占一行居中；文本临时缩为 `vX.Y.Z`（1.1.2 起恢复全称，一行放得下）
- 提交：`a9f06a3`

## [1.1.0] — 2026-11-03

侧边栏终于显示真实版本号。

- `feat` 原来读后端 `/api/version`，而 `internal/version.Version` 默认是 `dev`，
  只有 make/CI 用 `-ldflags` 注入才有值 —— Dockerfile 走的是裸 `go build`，
  所以 Docker 部署的实例侧边栏永远是 `DEV`。改为前端构建期常量
  `web/src/lib/version.ts:APP_VERSION`，后端默认值同步改掉
- `feat` 新增 `scripts/check_version.py`（比对前后端版本号）+ `make check-version`，
  并挂到 `build-web` / `build-server` / `build-servers` 前置与 CI 第一步
- 提交：`2258149`

## [1.0.3] — 2026-11-03

构建链在国内网络可用。

- `build` `Dockerfile.multi` 的 `GOPROXY` / npm registry 改成 build-arg，
  默认走 `goproxy.cn` / `npmmirror`（部署机上 `proxy.golang.org` 直接 EOF，
  导致 `go mod download` 失败、镜像构建挂掉）
- 提交：`db43696`

## [1.0.2] — 2026-11-03

构建可复现 + 补实测记录。

- `fix` **前端构建不可复现**：Tailwind v4 自动扫描项目目录（只跳过 `.gitignore`），
  把入库的 `web/embed_dir` 当源码再扫，CSS 从 72 kB 涨到 84 kB（历史上到 105 kB）。
  `index.css` 加 `@source not "../embed_dir"` / `"../dist"`
- `fix` **日志注入**：`zap.String("scheme", parts[0])` 里 `parts[0]` 客户端可控，
  可塞 `\r\n` 污染日志 → 改 `zap.Bool("bearer_scheme", ...)`
- `docs` `FIXES-2026-11-01.md` 补「端到端实测」20 项；README 补「安全加固记录」
  （哪些报告建议采纳、哪些判断错误未采纳）
- 提交：`dc8a9d2`、`131a22b`、`0bae1d4`、`f4e0fe5`

## [1.0.1] — 2026-11-03

第三方安全评估报告的修复批次。

- `P0` **JWT Secret 换钥炸弹**：`Secret` 留空时每次启动生成随机 uuid，而它同时
  是 AK/SK 密文的派生源 → 重启一次库里所有 `SecretEnc` 永久解不开。
  改为生成 32 字节随机值落盘 `data/.jwt_secret`（0600）复用
- `P0` 真实手机号 / 内网 IP / 部署路径从仓库移除（示例值替换）
- `P1` JWT 中间件不再打印 `Authorization` 原文
- `P1` 登录接口新增限流（IP+账号 5 次 / IP 跨账号 20 次 → 锁 5 分钟，429 + `Retry-After`）
- `P1` Dockerfile / Makefile 强制 `dist → embed_dir`，并把 `prepare-embed` 设为前置依赖
- `P2` `BodyLimit 64K`、`ReadHeaderTimeout 10s`、`IdleTimeout 60s`、
  `/openapi` 前端路由 404 修复、AK 枚举统一「认证失败」、SQLite WAL 示例、
  新增 CI（`go vet` + `go test` + 前端 lint + build）
- 提交：`a2de051`

## [1.0.0] — 2026-11-02

首个正式发布版：多设备内核 + AK/SK 开放接口 + 账号安全页 + UI 重做。

- 详见 `RELEASE_v1.0.0.md`

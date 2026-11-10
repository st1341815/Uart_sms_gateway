# 更新记录

版本号规则：主版本.次版本.修订号（语义化版本）。
- 修订号：修 bug / 构建链 / 文案，不改功能与接口
- 次版本：新增功能（新页面、新接口、新 UI 元素），向后兼容
- 主版本：破坏性变更（配置格式、API 协议、数据库结构不兼容）

**每次发版必须同时做四件事**（`scripts/check_version.py` 会检查前三件）：

1. 改 `web/src/lib/version.ts` 的 `APP_VERSION`
2. 改 `internal/version/version.go` 的 `Version` 默认值
3. 在本文件**顶部**加一节 `## [x.y.z] — YYYY-MM-DD`
4. 建 GitHub Release，**标题统一为 `UART SMS Gateway vX.Y.Z`**

> Release 标题只放「项目名 + 版本号」，不要往里塞功能描述——
> 那是正文第一行的事。标题会在 releases 列表里并排显示，长短不一很难扫读。
> （2026-11-04 统一过一次：此前是 `v1.1.8 — 移动端短信中心修复` 这类带副标题的写法。）

> 历史教训：1.1.0 之后连续发了 2 个版本却没改版本号，侧边栏一直显示 v1.1.0，
> 与实际部署的代码对不上（用户发现）。此文件就是为此建立。

---

## [1.6.1] — 2026-11-10

**名字改了，通知里还是旧的**：1.6.0 动手做「设备自定义名称」（落 `properties`
表的覆盖层）时，界面、设备选择器、状态卡片都走了新名字，唯独推送没跟上——
新短信、来电、SIM 上下线、补发失败回执里给出去的 `{{device_name}}` 仍是
`config.yaml` 里的开机默认名。因为容器内该文件是只读挂载，界面改名根本写不回去，
于是「我明明改了名，webhook 里还是『移动新卡』」。

- `fix` `SerialService` 新增 `displayName()`，统一按
  **DB 自定义名 > config 的 Name > 设备 ID** 三级回退取值。
- `fix` 5 处推送 + 2 处日志全部改用 `displayName()`：
  `serial_handlers_sms.go`（新短信 / 转发失败）、`serial_handlers_call.go`（来电）、
  `serial_handlers_sim.go`（SIM 上下线）、`serial_pending.go`（补发与补发失败）。
- `fix` `DeviceManager.Initialize` 给每台设备注入名称解析器
  （`SetDisplayNameResolver`），改名立即生效、无需重启；注入时特意走
  `dm.displayName`（只碰 `nameMu`）而不是 `GetDeviceDisplayName`（要 `RLock dm.mu`，
  而此时正持写锁）。
- `test` 新增 `internal/service/device_display_name_test.go`：覆盖三级优先级、
  改名后 `displayName()` 立刻跟随、config 快照不被篡改，并用源码扫描做护栏，
  防止以后再把 `s.deviceConfig.Name` 写回推送点。

## [1.6.0] — 2026-11-09

**新卡自检答了「宿主机上多了一张卡」，这一版接着答「这张卡是哪张」**：新增 L1 模组探测——
对未被任何配置引用的端口发一次 `get_status`，实测出 AT 口、运营商、号码与信号，
并把结论直接写成能粘进 `config.yaml` 的片段。

- `feat` 新增 `internal/service/probe_service.go`（L1）：对自检报告里未被配置的模组，
  按 `:1.6 → :1.2 → :1.4 → 其余按号` 的顺序逐口试探——谁回 `status_response` 谁就是 AT 口。
  命中后给出运营商（IMSI 前 5 位 PLMN）、号码、ICCID、CSQ/RSRP、Lua 版本，
  以及带实测设备名的 Devices 片段（ID 从 USB 端口派生，撞号自动顺延为 `usb01_2`）。
- `feat` 三条安全约束，每条都对应一次真实事故场景：① 已配置端口一律拒绝探测——
  在跑的卡被 probe 会打成「重连中」，掉线期间的短信只能进补发队列；
  ② 同端口 30 秒冷却 + 全局串行——连点按钮会在同一 UART 上开两个 fd，读到交错数据；
  ③ 容器 `/dev` 里没有的节点直接跳过并说明原因，不白等满 2 秒超时。
- `feat` 新增 `POST /api/serial/probe`（`internal/handler/probe_handler.go`）：
  「卡不回话」是**正常业务结论**，走 200 + `success=false` + 逐口 `tried` 明细供排查；
  只有端口不存在（404）、已配置或已有探测在跑（409）、冷却中（429）才用 4xx，
  否则前端会把「这张卡没上电」误报成接口坏了。
- `feat` 自检卡片里的未配置模组新增「探测 AT 口」按钮：探测时转圈、结果就地展开
  （一句话结论 + 下一步 + 逐口试探记录），成功后多给一个「复制探测到的 config 片段」按钮——
  比自检给的片段多了实测的运营商与号码作设备名，加卡后不用再自己起名。
- `refactor` 运营商推断抽成 `operatorFromIMSI`（`internal/service/oper.go`），
  状态帧处理与探测共用同一份映射：两处各写一份，早晚出现「界面显示中国移动、
  探测结果显示 46000」，用户会以为插了两张卡。
- `fix` 探测失败不再被当成异常：`probeOnce` 用 `(nil, nil)` 表达「口能开但卡不回话」，
  与「连 open 都失败」分开，两种故障的下一步提示完全不同。
- `test` 新增 `probe_service_test.go`（14 个测试函数）：试探顺序与兜底命中、已配置拒绝、
  冷却与并发拒绝、容器内缺节点、切帧（噪声/半包/老固件裸 JSON）、命令帧格式、
  串口必关、ID 冲突顺延。为此把 `SelfCheckRunner` / `DeviceRegistry` / `PortOpener`
  三处依赖收成可注入接口——真实串口在 CI 与开发机上都不存在。

## [1.5.0] — 2026-11-09

**加卡之后不再靠人肉 `docker exec` 排查**：新增设备自检（L0 一致性体检），
只读比对「宿主机 by-path ↔ 容器 /dev ↔ config.yaml」，把「为什么看不到新卡」变成界面上一句结论。

- `feat` 新增 `internal/service/selfcheck_service.go`：扫描 /dev/serial/by-path 与
  /sys/class/tty，按物理 USB 端口（`usb-<bus>:<port>`）聚合成「模组」——三个 AirM2M
  模组 VID/PID/SerialNumber 完全相同，by-id 不可用，端口号是唯一能区分的依据。
  与 `config.yaml` 的 Devices 交叉比对，分出 `ok / not_mapped / missing_on_host /
  node_only / disabled / auto / invalid` 七种设备状态，外加「未被任何配置引用」的新模组。
- `feat` 新增 `GET /api/serial/selfcheck`（`internal/handler/selfcheck_handler.go`）：
  设备名走 `GetDeviceDisplayName`，保证体检结论里的名字和界面上一致。
- `feat` `SerialControl` 页新增「设备自检」卡片：问题按 error/warn 平铺、正常项可折叠、
  60 秒自动复检；发现未配置模组时直接给出**可复制的 `docker-compose.yml` devices 片段
  与 `config.yaml` Devices 片段**，不用手抄 by-path。
- `feat` 启动时跑一次自检并写日志（只读，不打开串口，不会和在跑的收发抢设备），
  加卡后重启容器即可在 `docker logs` 里看到结论。
- `fix` 复制按钮兼容内网 http 环境：非安全上下文下 `navigator.clipboard` 不可用，
  用 `execCommand` 兜底，否则这个功能在用户真实部署里是死的。
- `docs` 新增 `docs/新卡自检方案.md`：现状约束（config 只读挂载、容器 devices 创建时固定、
  /sys 与 /dev/serial 可见宿主机全部节点）、L0/L1/L2 能力分层、L1 的两种宿主机改法与取舍。

## [1.4.3] — 2026-11-06

**顶栏信号读数不再自相矛盾**：4G 用 RSRP 显示真实电平，CSQ 换算只作兜底，
并修掉了 `−-91dBm` 这种双负号和「无信号时显示 +85dBm」的荒谬读数。

- `fix` 顶栏 `主卡` 读数出现两个负号（`−-91dBm`）：渲染时手写了 `−` 前缀，
  而换算结果本身已带负号。改为只显示绝对值。
- `fix` CSQ 缺失/未知（`undefined`、0、99）时旧逻辑按 99 兜底，换算出
  `-113 + 198 = +85`，于是「没信号」被画成「+85dBm」。现在无效即显示 `—`。
- `feat` **优先显示 RSRP**：4G 下 `RSRP` 才是真实信号质量指标，顶栏数字改为
  RSRP（有则用），小字标注来源 `dBm RSRP`；无 RSRP 时回退 CSQ 换算并标 `dBm≈`
  表示估算值。CSQ 原始值（`11/31`）与百分比照旧保留。
- `feat` 后端 `DeviceStatus` 透传 `rsrp` 字段；`normalizeRsrp` 过滤掉模组未上报的
  占位值（Lua 侧 `mobile.rsrp()` 兜底 `-140`、未初始化为 `0`），避免把 -140dBm
  当成实测电平画出来。
- `docs` 顶栏信号逻辑集中到一处并注明口径：RSRP 有效区间 `(-140, -44) dBm`，
  CSQ 换算 `RSSI = -113 + 2×CSQ` 出自 3GPP 27.007（2G 口径，仅作近似）。

## [1.4.1] — 2026-11-06

**构建提速**：`Dockerfile.multi` 挂上 BuildKit 编译缓存，改代码后重新构建从
约 2 分钟降到几十秒。

- `build` `go mod download` 与 `go build` 两层都加了 `--mount=type=cache`
  （`/go/pkg/mod` + `/root/.cache/go-build`），缓存跨 build 持久化在构建机上。
- 起因：1.4.0 那次构建实测约 2 分钟，其中 `modernc.org/sqlite`（纯 Go 移植的 SQLite）
  单个包编译就要 1~2 分钟，而 BuildKit 在此期间**零输出**——看起来跟卡死一样，
  排查时确实误判过一次「任务卡住」。
- 缓存只影响编译速度，产物仍由 builder 阶段 `COPY` 出去，最终镜像里不含缓存目录。
- 未引入 `# syntax=docker/dockerfile:1`：那会额外拉取 docker/dockerfile 前端镜像，
  在受限网络里多一个失败点；Docker 内置前端已支持 cache mount。

## [1.4.0] — 2026-11-06

**卡掉线时不再让短信石沉大海**：离线期间提交的短信自动排队，卡恢复后补发；
掉线后长时间等不到上线消息时，主动补报一次真实状态。

- `feat` **离线补发队列**。提交发送时若「串口未连接 / 开了飞行模式 / 卡未注册」，
  短信不再直接判 `failed`，而是落库为 `pending` 并记下补发截止时间。
  卡恢复注册后由后台自动补发（每轮最多 5 条，条间留 2 秒间隔）。
  判「能不能发」只看这三个硬条件，不拿信号强度做门槛——信号弱不等于发不出去。
- `feat` **补发有效期 60 分钟**。超期未发出的 `pending` 会被判 `failed` 并推送一条
  汇总通知。不让离线一整夜的短信在第二天早上突然发出去——那时内容早已失效。
- `feat` **掉线兜底状态上报**。掉线通知送达后开始计时，**65 分钟**内若上线通知
  仍未送达，强制推一次「此刻真实的卡状态」（已恢复 / 仍在掉线，附持续时间）。
  留 5 分钟余量是为了让正常的成对通知先有机会发出，不制造重复消息。
  上线通知一旦送达即撤销该计时——成对闭环了就不必再补。
- `feat` 前端新增两个状态标识：`⏳ 待补发`（pending）、`? 结果未知`（unknown）。
  之前 unknown 在界面里什么都不显示，看不出「发出去了但没回执」。
- `fix` 进程重启时把卡在 `sending` 的补发记录退回 `pending`，避免中途崩溃导致
  记录永久卡死（既不再补发，也不会超期判失败）。
- `refactor` 状态迁移全部用「带前置条件的一条 UPDATE」实现（CAS 语义）。
  补发由「SIM 上线事件」和「后台巡检」两条路径触发，先查后改会重复发送。

> 已知边界：补发队列按设备隔离，补发只在该设备自己的后台巡检里跑；
> 上报「已送达」以通知渠道调用成功为准，单渠道失败时无法回退重试。

## [1.3.1] — 2026-11-06

**修掉一个会让告警静默的 bug**：掉线后很快恢复时，上线通知被掉线的冷却窗口吞掉。

- `fix` 上下线通知改为**按方向各自计时**。1.3.0 里两者共用一个
  `lastSimNotifyAt`，实测「飞行模式开关一次」的往返只有 **21 秒**
  （日志：`00:20:26` 掉线通知发出 → `00:20:47` 卡重新就绪），
  而冷却窗口是 90 秒 —— 上线通知落进同一个窗口直接被丢弃。
  掉线后很快恢复恰恰是最该完整成对的场景（一次中断的始末），
  共用计时器等于只报开头不报结束。
- `fix` 冷却窗口 90 秒 → 20 秒。模块重连抖动通常只持续数秒，20 秒足以压住；
  同时不会误伤真实的快速往返。
- `fix` 冷却跳过的日志由 `Debug` 提升到 `Info`。生产日志级别是 info，
  原先这一行看不见，只能靠现象反推原因。

## [1.3.0] — 2026-11-06

**SIM 卡上下线可以推送了**：卡注册上线/掉线时走「通知渠道」发一条通知，并给了一个总开关。

- `feat` 新增 SIM 上下线通知。判定条件是 `mobile.is_registered && mobile.sim_ready` ——
  只看 `is_registered` 不够：卡没插好时模块可能沿用上一次的注册状态，只有 `sim_ready`
  才代表「卡在位且可用」。每个状态帧（10 秒一次）与上一次比对，**只在翻转时**推送。
- `feat` 串口断开时补发一次掉线通知。状态帧只在设备在线时才有，设备整机掉电 /
  USB 松脱 / 卡被停机时上行是静默的，只靠状态帧会漏掉最常见的一类掉线。
- `feat` 通知内容：运营商、号码、ICCID（只露后 4 位）、CSQ 信号、是否漫游、时间。
  若掉线是由串口断开触发的，则写明「原因: 设备串口连接已断开」，不编造运营商与信号。
- `feat` 新增开关 `sim_status_notify`（property），放在「通知渠道」页顶部，
  **切换即时生效**，不与渠道的「保存配置」按钮绑定 —— 用户想关掉告警时，
  不该还要再点一次保存才能确认生效。配置缺失时按「开启」处理，避免老库升级后告警被静默。
- `fix` 防抖两处：同一设备 90 秒冷却；首个状态帧只建立基线不推送。
  否则模块重连会让状态帧反复翻转刷屏，服务重启也会误报一条「SIM 卡上线」。

## [1.2.5] — 2026-11-05

**计划任务页与全站视觉对齐**：修正页头、字号、颜色三处偏差，另修掉一个深色主题 bug。

- `fix`（bug）`ScheduledTasksConfig.tsx` 卡片头写死了
  `bg-gradient-to-br from-white to-surface/30` —— 全站唯一一处 `from-white`。
  浅色主题下看不出来，**深色主题下卡片头会从纯白渐变到深色，非常刺眼**。
  改为 `bg-surface-2`。
- `fix` 手写页头换成统一 `PageHeader`：原为 `text-2xl`（24px），且
  `bg-gradient-to-r from-brand to-brand bg-clip-text text-transparent` 首尾同色、
  渐变实际无效，还带着「打印/高对比度模式下文字消失」的风险。
  替换后统一为 22px + `tracking-[-0.02em]`，与其余 6 个页面一致。
- `fix` 字号回到全站基准。实测全站主力档位是 `text-xs`(12px) 77 处 +
  `text-sm`(14px) 46 处，`text-[10px]`/`[11px]` 等自定义 px 只是零星微调。
  真正的偏差只有三处大档：
  - 卡片标题 `text-base`(16px) → `text-sm`(14px)（全站 `CardTitle` 以 `text-sm` 为主）
  - 弹窗标题 `text-xl`(20px) → 组件默认 `text-lg`(18px)（其余 3 处 `<DialogTitle>` 均不带字号）
  - 页面标题 24px → 22px（由第一项 `PageHeader` 接管）
- `fix` 硬编码绿/红改成语义 token：`text-green-600` / `bg-green-50` /
  `text-red-600` / `bg-red-50` / `bg-green-500` →
  `text-ok` / `bg-ok/10` / `text-err` / `bg-err/10` / `bg-ok`；
  悬停态 `hover:bg-green-50 hover:border-green-300 hover:text-green-700` →
  `hover:bg-ok/10 hover:border-ok/40 hover:text-ok`，删除按钮 `hover:bg-red-700` →
  `hover:bg-err/90`，必填星号与内容提示统一到 `text-err`。
- `chore` `w-N h-N` → `size-N`（8 处），间隔天数与手机号改用 `.num`
  （等宽 + `tabular-nums`，数字不跳动）。

## [1.2.4] — 2026-11-05

**概览页手机端横向溢出的真正根因**：裸 `grid` 的隐式 track 宽度是 `auto`。

- `fix`（根因）`Dashboard.tsx` 双栏容器的 className 是
  `grid ... lg:grid-cols-[minmax(0,1fr)_380px]` —— **只在 `lg` 断点声明了列模板**。
  `<1024px` 时它是一个没有 `grid-template-columns` 的裸 `grid`，退化成
  **隐式单列、track 宽度 `auto`**，而 `auto` 由内容 max-content 决定，不会被压缩。
  `min-w-0` 只能约束容器自身，管不到 track 的 `auto` 尺寸 —— 所以之前几轮
  加 `min-w-0`、改 `minmax(0,1fr)`（都只写在 `lg` 里）全部无效。
  实测该容器在 390px 视口下被撑到 **852px**，`scrollWidth` 达 864。
- `fix` 补上 `grid-cols-1`（Tailwind 展开为 `repeat(1, minmax(0,1fr))`），
  track 才真正可收缩。修复后 390px 下 `scrollWidth` 由 864 → 390。
- `docs` 在该元素上方留了注释，说明「裸 grid 必须显式写 grid-cols-1」，
  避免以后有人把 `grid-cols-1` 当成冗余删掉。

> 为什么只有概览页出问题：其他页面根容器用的是 `flex`，flex 子项可被 `min-w-0`
> 收缩；只有概览页在手机断点用了裸 `grid`。

验证：CDP 设备模拟 320 / 360 / 390 / 414 / 430px 五档，注入长设备名 /
长运营商 / `+86` 号码 / 超长短信内容等极端数据后，
`scrollWidth` 全部等于视口宽，溢出元素 0 个。

## [1.2.3] — 2026-11-05

手机端概览页不再溢出：去掉两处硬约束，设备矩阵在窄屏改为卡片式。

- `fix`（根因）`index.css` 的 `html { min-width: 320px }` 与 `body { min-width: 320px }`
  是最直接的元凶：一旦有效 CSS 宽度低于 320px（小屏设备、系统字体放大、
  浏览器缩放），页面就被钉死在 320px 无法收缩，只能横向溢出。两处均已删除。
- `fix`（根因）`1fr` 默认 `min-width` 是 `auto` 而非 `0`，会被 `<input>` 的固有宽度
  撑破栅格。快捷发送改为 `minmax(0,1fr)`，主栅格补 `min-w-0`。
- `fix`（掩盖问题的兜底已撤）先前给 `app-content` 加的 `overflow-x-clip` 会把
  超宽内容直接裁掉，`scrollWidth` 反而看起来正常，等于把 bug 藏起来了。已移除。
- `refactor`（设备矩阵）窄屏从「7 列表格压成 3 列」改为**卡片式两行**：
  第一行 状态点 + 设备名 + 网络制式 + 最后活动，第二行 运营商 + 本机号码 + 信号。
  不再依赖任何固定列宽，窄到 280px 依然成立；桌面 ≥640px 仍是七列表格行。

验证：CDP 设备模拟 280 / 320 / 360 / 390 / 414 / 430px 六档，
`documentElement.scrollWidth` 全部等于视口宽，溢出元素 0 个。

## [1.2.2] — 2026-11-05

把顶栏那个「用户首字母头像」从装饰元素改成真正的账号菜单。

- `feat`（顶栏）头像改为按钮 + 下拉菜单，收纳三项：当前账号（完整用户名）、
  账号安全、退出登录。点击外部或按 `Esc` 关闭，补了 `aria-haspopup` /
  `aria-expanded` / `aria-label`，键盘可达。
- `fix`（可用性）此前该头像是个裸 `div`：没有 `onClick`、没有 `title`、没有 `role`，
  点击毫无反应，但位置上又完全符合「点头像出用户菜单」的惯例，属于典型的死元素。
  而真正的「退出登录」埋在侧边栏抽屉里、「账号安全」收在 rail 底部齿轮里。
  移动端最别扭：抽屉默认收起，要退出得先开抽屉，眼前那个头像却点不动。
- `refactor` 复用既有 `handleLogout`，不动后端与接口。

## [1.2.1] — 2026-11-05

概览页两处布局细节修复，不改接口、不改功能。

- `fix`（设备矩阵）列宽从固定 px 改为弹性 `fr` 分配：串口 / 运营商 / 本机号码 / 信号 / 最后活动
  按内容比例伸缩。运营商中文名与 11 位号码统一 `whitespace-nowrap`，不再折行挤成两行；
  串口 by-path 路径过长时 `truncate` + `title` 保留全值。
- `fix`（移动端）整体栅格补 `min-w-0`，快捷发送的 `1fr` 改为 `minmax(0,1fr)`。
  **根因**：`1fr` 的默认 `min-width` 是 `auto`，会被 input 的固有宽度撑破，
  这是窄屏横向溢出的真正来源，只加 `overflow` 掩盖不掉。
- `fix`（布局）`app-content` 加 `overflow-x-clip` 兜底，杜绝窄屏横向滚动条。
- `chy` 前端产物同步进 `web/embed_dir`（`go:embed` 的输入目录）。

验证：CDP 设备模拟 320 / 360 / 390px 三档实测，`documentElement.scrollWidth` 均等于视口宽。

## [1.2.0] — 2026-11-05

UI 全面重构：从「模板化白底后台」换成方案 A「NOC 驾驶舱」，并补上深浅双主题。

- `feat` 新增深/亮双主题（token 化）：`index.css` 用一套语义变量（canvas / surface / line /
  fg / brand / ok / warn / err / info），深色为默认值，JS 未执行时也不会闪白
- `feat` 新增三态主题切换 `<ThemeToggle/>`（亮色 / 跟随系统 / 深色），挂在顶栏读数带右端；
  接 `next-themes`（项目早就装了但一直没用），`defaultTheme="dark"`、`storageKey="uartsms-theme"`
- `refactor` `Layout` 从 248px 深蓝侧栏改为 56px 图标 rail（hover 出中文名）+ 顶部读数带
  （在线 / 今日 / 未读 / 主卡 / 异常），版本号移到 rail 底部竖排，仍指向 GitHub 仓库
- `refactor` `Dashboard` 改为设备矩阵表（状态灯 · 串口 · 运营商 · 号码 · 信号柱 · 最后活动）
  + 未读短信 + 快捷发送三段式；去掉 4 张等宽 KPI 卡片
- `refactor` 7 个页面全部清理硬编码颜色类（`bg-[#0b2a55]`、`text-emerald-600`、`rounded-2xl`…），
  统一走语义 token，暗色下不再漏白
- `fix` 接口示例代码块（`<pre>`）固定为深色 —— 这是「机器输出」，跟随主题反而难读
- `feat` 账号安全页底部显示 `v{APP_VERSION} · 仓库地址`，排障时可直接报版本

> 取舍：亮色的琥珀强调色压暗到 `#A05E00`（深色是 `#E9A227`），因为亮琥珀压白底只有 2.1:1，
> 不可用；两套色值同色相不同明度，切换时「品牌感」不变。

---

## [1.1.9] — 2026-11-04

账号安全页新增「短信重置密码」：一键生成随机新口令，用本机的 SIM 卡发到管理员手机上。
面向「人在外面、只能用手机操作」的场景——新密码直接躺在短信里，不用再去翻服务器配置。

- `feat` 新增 `POST /api/auth/password-reset/sms`：生成 16 位随机口令（`crypto/rand`，
  含大小写 + 数字）→ 通过指定设备发短信 → 成功后才覆盖数据库里的口令。
- `feat` 新增 `GET/PUT /api/auth/password-reset/phone`：安全手机号落到 `properties` 表
  （键 `password_reset_phone`）。**不能放 `config.yaml`**——容器内它是只读挂载，
  程序写不回去，改了也不即时生效。首次使用的号码在重置成功后自动记住。
- `feat` 前端账号安全页新增「短信重置密码」卡片：手机号 + 发送设备 + 二次确认弹窗。
  重置成功后清 token 并跳登录页（旧口令已失效）。
- `impl` `CredentialService` 抽出 `persistPassword`，并新增 `ResetPassword`（不校验旧口令）。
  普通改密仍走 `ChangePassword`——那条路必须校验旧口令，不能混用。
- `impl` 发送能力抽成 `SMSSender` 接口，让「发送失败时口令不变」这条承诺可被单元测试覆盖
  （真机发短信没法在测试里跑）。`*DeviceManager` 天然满足该接口。

**为什么坚持「先发短信、后落库」**：反过来写的话，一旦短信发不出去
（设备离线 / 卡欠费 / 号码填错），旧口令已经被覆盖，用户就被彻底锁在门外了。
当前实现里发送失败 = 口令保持原样，错误信息也明确写「密码未修改」。

**其它防护**：
- 同一账号 60 秒冷却期，防误点和脚本刷话费（设备层另有 `sendRateLimiters`）。
- 强制显式指定发送设备，不给「默认设备」兜底——多卡场景下随机挑一张可能挑到境外漫游卡，
  一条短信就是一条国际资费（与 `DeviceManager.SendSMS` 既有的立场一致）。
- 完整手机号不返回前端、不写日志，只回显 `138****8000` 形式；新口令不进日志、只走短信。
- 口令字符集剔除了 `0/O/1/l/I`——这串字符要从手机屏幕手抄到电脑上，一次看错就是一次登录失败。

**测试**：`password_gen_test.go`（长度 / 必含三类 / 无歧义字符 / 不重复）、
`password_reset_test.go`（成功路径改密生效、**发送失败不改密**、设备离线/不存在/号码非法/
缺设备/冷却期各拒绝一次、号码归一化与持久化、脱敏规则）。

## [1.1.8] — 2026-11-04

手机端短信中心修 bug：进页面就被锁在默认会话里，切换号卡的入口消失。

**根因**：`selectedConvKey` 在用户没点选过时会回落到 `conversations[0]`
（`peerSelection ?? conversations[0]`）。桌面端这是有意的「自动选中第一条」，
但移动端是「列表 / 详情」二选一布局，列表显隐由 `selectedPeer` 控制
（选中即 `hidden`）。于是进入页面就直接落到详情、列表被隐藏；
而返回按钮执行的是 `setPeerSelection(null)`，随即又被回落逻辑选回第一条，
**返回变成空操作**，用户被永久困在同一个会话里，看不到按号卡分组的列表。

- `fix` 自动回落改为**仅桌面端生效**：`peerSelection ?? (isDesktop && conversations[0] ? ... : null)`。
  移动端以「未选中」为初始态，先展示分组列表；点会话进详情、点返回回列表，
  可以自由在各号卡之间切换。
- `feat` 会话详情头部补上**当前所属的卡**（名称 + 在线状态点 + 消息数）。
  多卡共用一个界面，移动端切进详情后列表已隐藏，没有其他位置能看出正在用哪张卡通信。
- `impl` 新增 `web/src/lib/use-media-query.ts`：用 `useSyncExternalStore` 订阅
  `matchMedia`，而不是 `useState + useEffect`。两个原因——`matchMedia` 是外部可变源，
  订阅式读取语义才对，且不会出现首帧渲染错误值再纠正的闪烁；
  同时避开 `react-hooks/set-state-in-effect`（effect 内同步 setState 会级联渲染）。

**验证**：建了带 2 张卡 / 3 个会话（含两卡同号）的测试库，用 CDP 手机视口 390×844 实测。
修复前：初始 `list=none, detail=flex`，点返回后仍 `list=none, detail=flex`（卡住）。
修复后：初始 `list=flex(356px), detail=none`，点会话 → 详情、点返回 → 列表恢复，
可在卡1/卡2 会话间来回切换。

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

## [1.4.2] — 2026-11-06

**发版从「手敲十几条命令」变成一条命令**，Release 顺带挂上二进制包。

- `feat` 新增 `scripts/release.sh`（Makefile 入口 `make release`），把构建 → 部署 →
  健康验证 → 打 tag → 推送 → 建 Release 固化成 10 个阶段：预检、前端构建、
  源码同步、远端 `docker build`、打回滚点后切镜像、轮询 `/health` 验证（失败自动
  回滚）、打 tag、生成发布包、推送并上传附件、清理。`--dry-run` 能完整演练十步
  而不产生任何副作用。
- `feat` Release 现在挂 `linux/amd64` 二进制包与 `SHA256SUMS.txt`。二进制不是重新
  交叉编译的，而是从刚构建好的镜像里 `docker cp` 出来的——发布包与线上跑的可执行
  文件字节一致，构建机也不需要装 Go（s21 实测没有 `go`）。
- `fix` `.gitignore` 漏了 `bin/`，交叉编译的中间产物会出现在 `git status` 里，
  有误提交 23 MB 二进制的风险。
- `docs` README 第 6 节重写：10 阶段表、参数表、环境变量表，并记下两个本机坑
  ——OpenSSH 不读 `$HOME/.ssh/config`；工作区不干净时拒绝发版的原因。

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

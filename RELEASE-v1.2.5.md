# UART SMS Gateway v1.2.5

**计划任务页与全站视觉对齐**：修正页头、字号、颜色三处偏差，另修掉一个深色主题 bug。

## 修复

- **深色主题卡片头刺眼**（bug）— `ScheduledTasksConfig.tsx` 的卡片头写死了
  `bg-gradient-to-br from-white to-surface/30`，是全站唯一一处 `from-white`。
  浅色主题下看不出来，深色主题下卡片头会从纯白渐变到深色。改为 `bg-surface-2`。
- **页头不统一** — 手写 `text-2xl`（24px）标题换成统一 `PageHeader`（22px +
  `tracking-[-0.02em]`），与其余 6 个页面一致。同时去掉
  `bg-gradient-to-r from-brand to-brand bg-clip-text text-transparent`
  ——首尾同色的无效渐变，且带「打印/高对比度模式下文字消失」的风险。
- **字号偏离全站基准** — 实测全站主力档位是 `text-xs`(12px) 77 处 +
  `text-sm`(14px) 46 处，`text-[10px]`/`[11px]` 等自定义 px 只是零星微调。
  本页原本的 `xs`/`sm` 体系没问题，真正偏大的只有三处：
  - 卡片标题 `text-base`(16px) → `text-sm`(14px)（全站 `CardTitle` 以 `text-sm` 为主）
  - 弹窗标题 `text-xl`(20px) → 组件默认 `text-lg`(18px)（其余 3 处 `<DialogTitle>` 均不带字号）
  - 页面标题 24px → 22px（由 `PageHeader` 接管）
- **硬编码颜色** — `text-green-600` / `bg-green-50` / `text-red-600` / `bg-red-50` /
  `bg-green-500` → `text-ok` / `bg-ok/10` / `text-err` / `bg-err/10` / `bg-ok`；
  悬停态与必填星号、内容提示一并收进语义 token。

## 其他

- `w-N h-N` → `size-N`（8 处）
- 间隔天数与手机号改用 `.num`（等宽 + `tabular-nums`，数字不跳动）

## 部署

已部署至 192.168.11.21，容器 `sms-multi`（`smstext-multi-device:1.2.5`，healthy）。

## 完整记录

见 [CHANGELOG.md](CHANGELOG.md)。

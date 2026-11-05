// 版本号：单一来源。
//
// 为什么在前端硬编码一份：侧边栏要显示版本号，而这个值必须**构建时就确定**。
// 之前读的是后端 `/api/version`，但 `internal/version.Version` 默认是 "dev"，
// 只有 make / CI 用 -ldflags 注入才会变成真实版本 —— 直接用 `go build`
// 或 `docker build`（Dockerfile.multi 就是裸 go build）打出来的镜像，
// 侧边栏永远显示 DEV，等于没版本信息。
//
// 发版时同步改这里（vite 会把它打进 bundle，构建产物哈希随之变化，
// 天然带着版本可追溯）。
export const APP_VERSION = '1.2.3';

// 项目主页 / 源码仓库（侧边栏版本号指向这里）
export const APP_REPO_URL = 'https://github.com/st1341815/Uart_sms_gateway';

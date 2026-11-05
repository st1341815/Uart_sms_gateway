package version

// Version 服务端版本号。
//
// 默认值就是发行版本号：裸 `go build` / `docker build`（Dockerfile.multi 就是裸
// go build，不走 make）打出来的二进制也应带正确版本，而不是 "dev"。
// make / CI 仍会用 -ldflags 注入（git describe 的结果），两者应当一致 ——
// 前端侧边栏展示的版本号在 web/src/lib/version.ts，发版时一并改，
// 一致性由 `python3 scripts/check_version.py` 检查。
var Version = "1.2.0"

// GetVersion 获取服务端版本号
func GetVersion() string {
	if Version == "" {
		return "dev"
	}
	return Version
}

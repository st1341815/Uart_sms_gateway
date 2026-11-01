package version

// Version 服务端版本号（通过 -ldflags 注入）
var Version = "dev"

// GetVersion 获取服务端版本号
func GetVersion() string {
	if Version == "" {
		return "dev"
	}
	return Version
}

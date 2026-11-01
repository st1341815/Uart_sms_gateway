package config

type AppConfig struct {
	JWT    JWTConfig         `json:"JWT"`
	Users  map[string]string `json:"Users"`  // 用户名 -> bcrypt加密的密码
	Serial SerialConfig      `json:"Serial"` // 串口配置（支持多设备）
	OIDC   *OIDCConfig       `json:"OIDC"`   // OIDC配置（可选）
}

// JWTConfig JWT配置
type JWTConfig struct {
	Secret       string `json:"Secret"`
	ExpiresHours int    `json:"ExpiresHours"`
}

// SerialConfig 串口配置（多设备支持）
type SerialConfig struct {
	Devices []DeviceConfig `json:"Devices"` // 多设备列表
	// 保留旧字段兼容性（可选）
	Port string `json:"Port"` // 旧配置：单个串口路径，为空则自动检测
}

// DeviceConfig 单设备配置
type DeviceConfig struct {
	ID          string `json:"ID"`           // 设备唯一标识，如 "sms1", "sms2"
	Name        string `json:"Name"`         // 设备名称，如 "英国漫游卡", "联通151"
	Port        string `json:"Port"`         // 串口路径，如 "/dev/ttyACM2"
	Description string `json:"Description"`  // 可选描述
	Enabled     bool   `json:"Enabled"`      // 是否启用
}

// OIDCConfig OIDC认证配置
type OIDCConfig struct {
	Enabled      bool   `json:"Enabled"`      // 是否启用OIDC
	Issuer       string `json:"Issuer"`       // OIDC Provider的Issuer URL
	ClientID     string `json:"ClientID"`     // Client ID
	ClientSecret string `json:"ClientSecret"` // Client Secret
	RedirectURL  string `json:"RedirectURL"`  // 回调URL
}

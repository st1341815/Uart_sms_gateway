package models

// APIKey 对外开放接口的调用凭证。
//
// 认证方式参考阿里云/腾讯云：AccessKey + SecretKey + HMAC 签名。
// 因为 HMAC 验签需要**原始 secret**，所以 SecretEnc 是可解密的密文
// （AES-GCM，密钥由 JWT Secret 派生），而不是像用户口令那样只存哈希。
type APIKey struct {
	ID      string `gorm:"primaryKey;size:64" json:"id"`
	Name    string `gorm:"size:128;not null" json:"name"`        // 用途说明，如「HVAccRM 验证码」
	AccessKey string `gorm:"size:64;uniqueIndex;not null" json:"accessKey"` // AK，公开标识
	// SecretEnc AES-GCM 密文（base64）。明文只在创建时返回一次。
	SecretEnc string `gorm:"type:text;not null" json:"-"`
	// SecretHint 仅用于界面辨认，如 "****a1b2"
	SecretHint string `gorm:"size:32" json:"secretHint"`

	// DeviceID 该 Key 固定从哪张卡发出。必填，避免误用境外卡。
	DeviceID string `gorm:"size:32;not null" json:"deviceId"`

	// AllowedIPs 逗号分隔的 IP / CIDR 白名单；留空表示不限制来源 IP。
	AllowedIPs string `gorm:"size:512" json:"allowedIps"`

	// RateLimitPerMinute 该 Key 每分钟最多调用次数（0 = 使用默认值）
	RateLimitPerMinute int `json:"rateLimitPerMinute"`

	Enabled   bool  `gorm:"not null;default:true" json:"enabled"`
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`

	// LastUsedAt 最近一次成功调用时间（便于排查停用的 Key）
	LastUsedAt int64 `json:"lastUsedAt"`
}

func (APIKey) TableName() string { return "api_keys" }

// SMSTemplate 短信模板，支持 {变量} 占位符。
type SMSTemplate struct {
	ID      string `gorm:"primaryKey;size:64" json:"id"`
	Code    string `gorm:"size:64;uniqueIndex;not null" json:"code"` // 调用方使用的模板编码
	Name    string `gorm:"size:128;not null" json:"name"`
	// Content 模板正文，变量用花括号包裹，如：您的验证码是{code}，5分钟内有效。
	Content     string `gorm:"type:text;not null" json:"content"`
	Description string `gorm:"size:512" json:"description"`
	Enabled     bool   `gorm:"not null;default:true" json:"enabled"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

func (SMSTemplate) TableName() string { return "sms_templates" }

// APISendLog 通过开放接口发出的短信记录。
// 同时承担两件事：① 审计  ② 双维度限流（按手机号 / 按 Key）的计数来源。
type APISendLog struct {
	ID         string `gorm:"primaryKey;size:64" json:"id"`
	KeyID      string `gorm:"index:idx_api_send_key_time,priority:1;size:64" json:"keyId"`
	AccessKey  string `gorm:"size:64" json:"accessKey"`
	DeviceID   string `gorm:"size:32" json:"deviceId"`
	Phone      string `gorm:"index:idx_api_send_phone_time,priority:1;size:32" json:"phone"`
	TemplateID string `gorm:"size:64" json:"templateId"`
	Content    string `gorm:"type:text" json:"content"`
	// Status: accepted（已提交串口）/ rejected（被限流或校验拒绝）/ failed
	Status string `gorm:"size:16;index" json:"status"`
	// Reason 拒绝原因（限流、模板错误等）
	Reason string `gorm:"size:255" json:"reason"`
	// MessageID 对应 text_messages.id，便于串联
	MessageID string `gorm:"size:64;index" json:"messageId"`
	ClientIP  string `gorm:"size:64" json:"clientIp"`
	CreatedAt int64  `gorm:"index:idx_api_send_key_time,priority:2;index:idx_api_send_phone_time,priority:2" json:"createdAt"`
}

func (APISendLog) TableName() string { return "api_send_logs" }

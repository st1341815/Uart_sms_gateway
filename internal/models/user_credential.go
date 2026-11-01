package models

// UserCredential 管理员登录凭据。
//
// 存在数据库而非配置文件的原因：
//   - 容器内 config.yaml 是只读挂载，程序无法写回；
//   - 配置在启动时解析完毕，改了也不会即时生效。
//
// config.yaml 的 Users 仅作为首次启动的种子值。
type UserCredential struct {
	Username     string `gorm:"primaryKey;size:64" json:"username"`
	PasswordHash string `gorm:"size:255" json:"-"` // bcrypt 哈希，绝不返回给前端
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

func (UserCredential) TableName() string {
	return "user_credentials"
}

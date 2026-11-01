package models

type MessageType string

const (
	MessageTypeIncoming MessageType = "incoming" // 收到
	MessageTypeOutgoing MessageType = "outgoing" // 发送
)

type MessageStatus string

const (
	MessageStatusReceived MessageStatus = "received" // 接收成功
	MessageStatusSending  MessageStatus = "sending"  // 发送中（已写入串口，等待设备回执）
	MessageStatusSent     MessageStatus = "sent"     // 发送成功
	MessageStatusFailed   MessageStatus = "failed"   // 发送失败
	// MessageStatusUnknown 发送结果未知：写入串口成功，但设备在超时时间内
	// 没有回 sms_send_result（固件丢包/设备重启等）。
	// 不能当成成功，也不能直接判失败——短信可能已经发出去了。
	MessageStatusUnknown MessageStatus = "unknown"
)

// TextMessage 短信记录
type TextMessage struct {
	ID        string        `gorm:"primaryKey" json:"id"`                   // UUID
	From      string        `gorm:"index" json:"from"`                      // 发送方号码
	To        string        `gorm:"index" json:"to"`                        // 接收方号码
	Content   string        `gorm:"type:text" json:"content"`               // 短信内容
	Type      MessageType   `gorm:"index" json:"type"`                      // 消息类型：incoming（收到）、outgoing（发送）
	Status    MessageStatus `gorm:"index" json:"status"`                    // 状态：received、sent、failed
	DeviceID  string        `gorm:"index;size:32" json:"deviceId"`          // 收发该消息的设备ID（sms1/sms2...），历史数据为 NULL
	CreatedAt int64         `json:"createdAt" gorm:"autoCreateTime:milli"`  // 创建时间
	UpdatedAt int64         `json:"updatedAt" gorm:"autoUpdateTime:milli"`  // 更新时间
}

// TableName 指定表名
func (TextMessage) TableName() string {
	return "text_messages"
}

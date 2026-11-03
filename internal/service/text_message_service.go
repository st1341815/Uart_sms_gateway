package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/dushixiang/uart_sms_forwarder/internal/repo"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TextMessageService 短信服务
type TextMessageService struct {
	repo   *repo.TextMessageRepo
	logger *zap.Logger
}

// NewTextMessageService 创建短信服务实例
func NewTextMessageService(logger *zap.Logger, repo *repo.TextMessageRepo) *TextMessageService {
	return &TextMessageService{
		repo:   repo,
		logger: logger,
	}
}

// Stats 统计信息
type Stats struct {
	TotalCount    int64 `json:"totalCount"`
	IncomingCount int64 `json:"incomingCount"`
	OutgoingCount int64 `json:"outgoingCount"`
	TodayCount    int64 `json:"todayCount"`
}

// Conversation 会话信息
type Conversation struct {
	Peer string `json:"peer"` // 对方号码
	// DeviceID 该会话归属的收发设备（sms1/sms2...）。
	// 同一号码可能同时与两张卡有往来，因此会话的唯一键是「设备 + 对方号码」，
	// 前端据此按卡分组展示。历史数据（deviceId 字段引入前）为空串。
	DeviceID     string              `json:"deviceId"`
	LastMessage  *models.TextMessage `json:"lastMessage"`  // 最后一条消息
	MessageCount int64               `json:"messageCount"` // 消息总数
	UnreadCount  int64               `json:"unreadCount"`  // 未读数量（暂时为0）
}

// conversationKey 会话分组键：设备 + 对方号码。
// 用 \x00 分隔，避免号码里出现分隔符导致的键碰撞。
func conversationKey(deviceID, peer string) string {
	return deviceID + "\x00" + peer
}

// Save 保存短信记录
func (s *TextMessageService) Save(ctx context.Context, msg *models.TextMessage) error {
	if err := s.repo.Save(ctx, msg); err != nil {
		s.logger.Error("保存短信记录失败", zap.Error(err), zap.String("id", msg.ID))
		return fmt.Errorf("保存短信记录失败: %w", err)
	}
	return nil
}

// Get 获取单条短信记录
func (s *TextMessageService) Get(ctx context.Context, id string) (*models.TextMessage, error) {
	msg, err := s.repo.FindById(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("短信记录不存在")
		}
		s.logger.Error("获取短信记录失败", zap.Error(err), zap.String("id", id))
		return nil, fmt.Errorf("获取短信记录失败: %w", err)
	}
	return &msg, nil
}

// Delete 删除单条短信记录
func (s *TextMessageService) Delete(ctx context.Context, id string) error {
	if err := s.repo.DeleteById(ctx, id); err != nil {
		s.logger.Error("删除短信记录失败", zap.Error(err), zap.String("id", id))
		return fmt.Errorf("删除短信记录失败: %w", err)
	}
	s.logger.Info("删除短信记录成功", zap.String("id", id))
	return nil
}

// Clear 清空所有短信记录
func (s *TextMessageService) Clear(ctx context.Context) error {
	db := s.repo.GetDB(ctx)
	if err := db.Where("1 = 1").Delete(&models.TextMessage{}).Error; err != nil {
		s.logger.Error("清空短信记录失败", zap.Error(err))
		return fmt.Errorf("清空短信记录失败: %w", err)
	}
	s.logger.Info("清空短信记录成功")
	return nil
}

// GetStats 获取统计信息
func (s *TextMessageService) GetStats(ctx context.Context) (*Stats, error) {
	db := s.repo.GetDB(ctx)

	stats := &Stats{}

	// 总数
	if err := db.Model(&models.TextMessage{}).Count(&stats.TotalCount).Error; err != nil {
		return nil, fmt.Errorf("统计总数失败: %w", err)
	}

	// 接收数量
	if err := db.Model(&models.TextMessage{}).Where("type = ?", "incoming").Count(&stats.IncomingCount).Error; err != nil {
		return nil, fmt.Errorf("统计接收数量失败: %w", err)
	}

	// 发送数量
	if err := db.Model(&models.TextMessage{}).Where("type = ?", "outgoing").Count(&stats.OutgoingCount).Error; err != nil {
		return nil, fmt.Errorf("统计发送数量失败: %w", err)
	}

	// 今日数量（按 created_at 字段）
	// 注意：不能用 time.Now().Truncate(24*time.Hour)——那是对 UTC epoch 取整，
	// 在 UTC+8 下统计的其实是「北京时间 08:00 之后」，每天都会错。
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
	if err := db.Model(&models.TextMessage{}).Where("created_at >= ?", todayStart).Count(&stats.TodayCount).Error; err != nil {
		return nil, fmt.Errorf("统计今日数量失败: %w", err)
	}

	return stats, nil
}

func (s *TextMessageService) UpdateStatusById(ctx context.Context, id string, status models.MessageStatus) error {
	return s.repo.UpdateColumnsById(ctx, id, map[string]interface{}{
		"status": status,
	})
}

// UpdateStatusIfSending 仅当当前状态为 sending 时才更新，返回受影响行数。
// 用于「超时置 unknown」：如果设备回执正好先到并已改成 sent/failed，
// 这里就不会覆盖它（避免把成功的结果盖成 unknown）。
func (s *TextMessageService) UpdateStatusIfSending(ctx context.Context, id string, status models.MessageStatus) (int64, error) {
	db := s.repo.GetDB(ctx)
	res := db.Model(&models.TextMessage{}).
		Where("id = ? AND status = ?", id, models.MessageStatusSending).
		Update("status", status)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// ExpireStaleSending 把「卡在 sending 超过 timeout」的记录置为 unknown。
//
// 背景：SendSMS 写串口成功后就置 sending，等设备异步回 sms_send_result 再改成
// sent/failed。若设备固件丢包、模块重启，回执永远不来，这条记录会永远显示
// 「发送中」——既误导用户，也没法区分「没发出去」和「发出去了但没回执」。
//
// 返回值：被更新的条数。
func (s *TextMessageService) ExpireStaleSending(ctx context.Context, timeout time.Duration) (int64, error) {
	db := s.repo.GetDB(ctx)
	cutoff := time.Now().Add(-timeout).UnixMilli()

	res := db.Model(&models.TextMessage{}).
		Where("status = ? AND created_at < ?", models.MessageStatusSending, cutoff).
		Update("status", models.MessageStatusUnknown)
	if res.Error != nil {
		return 0, fmt.Errorf("超时扫描失败: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		s.logger.Warn("短信发送超时未收到设备回执，状态置为 unknown",
			zap.Int64("count", res.RowsAffected),
			zap.Duration("timeout", timeout))
	}
	return res.RowsAffected, nil
}

// GetConversations 获取会话列表（按对方号码分组）
func (s *TextMessageService) GetConversations(ctx context.Context) ([]*Conversation, error) {
	db := s.repo.GetDB(ctx)

	// 获取所有短信记录，按创建时间倒序
	var messages []models.TextMessage
	if err := db.Order("created_at DESC").Find(&messages).Error; err != nil {
		s.logger.Error("获取短信记录失败", zap.Error(err))
		return nil, fmt.Errorf("获取短信记录失败: %w", err)
	}

	// 按「设备 + 对方号码」分组：
	// 同一号码可能同时与两张卡有往来（例如一张国内卡、一张境外卡），
	// 只按号码聚合会把两条独立的通信线搅在一起，也没法在界面上区分来源卡。
	conversationMap := make(map[string]*Conversation)
	for i := range messages {
		msg := &messages[i]

		// 确定对方号码
		var peer string
		if msg.Type == models.MessageTypeIncoming {
			peer = msg.From
		} else {
			peer = msg.To
		}

		if peer == "" {
			continue
		}

		key := conversationKey(msg.DeviceID, peer)

		// 如果会话不存在，创建新会话
		if _, exists := conversationMap[key]; !exists {
			conversationMap[key] = &Conversation{
				Peer:         peer,
				DeviceID:     msg.DeviceID,
				LastMessage:  msg,
				MessageCount: 0,
				UnreadCount:  0,
			}
		}

		// 更新消息数量
		conversationMap[key].MessageCount++

		// 更新最后一条消息（取最新的）
		if msg.CreatedAt > conversationMap[key].LastMessage.CreatedAt {
			conversationMap[key].LastMessage = msg
		}
	}

	// 转换为切片并按最后消息时间排序
	conversations := make([]*Conversation, 0, len(conversationMap))
	for _, conv := range conversationMap {
		conversations = append(conversations, conv)
	}

	// 按最后消息时间倒序排序
	for i := 0; i < len(conversations)-1; i++ {
		for j := i + 1; j < len(conversations); j++ {
			if conversations[i].LastMessage.CreatedAt < conversations[j].LastMessage.CreatedAt {
				conversations[i], conversations[j] = conversations[j], conversations[i]
			}
		}
	}

	return conversations, nil
}

// GetConversationMessages 获取指定会话的所有消息
//
// deviceID 非空时只返回该设备的消息：同一号码在两张卡上各有会话时，
// 不带设备过滤会把两条通信线混成一个列表。
func (s *TextMessageService) GetConversationMessages(ctx context.Context, peer string, deviceID string) ([]models.TextMessage, error) {
	db := s.repo.GetDB(ctx)

	var messages []models.TextMessage

	// 查询条件：(type=incoming AND from=peer) OR (type=outgoing AND to=peer)
	query := db.Where("(type = ? AND \"from\" = ?) OR (type = ? AND \"to\" = ?)",
		models.MessageTypeIncoming, peer,
		models.MessageTypeOutgoing, peer,
	)
	if deviceID != "" {
		query = query.Where("device_id = ?", deviceID)
	}

	if err := query.Order("created_at ASC").Find(&messages).Error; err != nil {
		s.logger.Error("获取会话消息失败", zap.Error(err), zap.String("peer", peer), zap.String("device_id", deviceID))
		return nil, fmt.Errorf("获取会话消息失败: %w", err)
	}

	return messages, nil
}

// DeleteConversation 删除整个会话（与某个联系人的所有消息）
//
// deviceID 非空时只删该设备的消息，避免误删另一张卡上同名会话。
func (s *TextMessageService) DeleteConversation(ctx context.Context, peer string, deviceID string) error {
	db := s.repo.GetDB(ctx)

	// 删除条件：(type=incoming AND from=peer) OR (type=outgoing AND to=peer)
	query := db.Where("(type = ? AND \"from\" = ?) OR (type = ? AND \"to\" = ?)",
		models.MessageTypeIncoming, peer,
		models.MessageTypeOutgoing, peer,
	)
	if deviceID != "" {
		query = query.Where("device_id = ?", deviceID)
	}

	result := query.Delete(&models.TextMessage{})

	if result.Error != nil {
		s.logger.Error("删除会话失败", zap.Error(result.Error), zap.String("peer", peer), zap.String("device_id", deviceID))
		return fmt.Errorf("删除会话失败: %w", result.Error)
	}

	s.logger.Info("删除会话成功",
		zap.String("peer", peer),
		zap.String("device_id", deviceID),
		zap.Int64("deleted_count", result.RowsAffected))
	return nil
}

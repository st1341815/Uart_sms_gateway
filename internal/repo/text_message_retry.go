package repo

import (
	"context"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"gorm.io/gorm"
)

// 本文件实现「离线补发队列」的数据操作。
//
// 设计约束：
//   - 队列状态即 TextMessage.Status，没有独立的队列表——短信记录本身就是队列，
//     用户能在界面里看到「待补发」，不需要再去别处找。
//   - 所有状态迁移都用「带前置条件的一条 UPDATE」完成（CAS 语义），
//     因为补发可能被「SIM 上线事件」和「后台巡检」两条路径同时触发。
//     先查后改会重复发送：两条路径都读到 pending，都发一次。

// ListPendingRetry 取待补发的短信，按创建时间升序（先来的先补）。
func (r *TextMessageRepo) ListPendingRetry(ctx context.Context, deviceID string, limit int) ([]models.TextMessage, error) {
	db := r.GetDB(ctx)

	var messages []models.TextMessage
	query := db.Where("status = ?", models.MessageStatusPending)
	if deviceID != "" {
		query = query.Where("device_id = ?", deviceID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Order("created_at ASC").Find(&messages).Error; err != nil {
		return nil, err
	}
	return messages, nil
}

// ClaimPendingRetry 原子地把一条 pending 抢占为 sending。
//
// 返回 false 表示这条已被别的流程取走（或状态已不是 pending），调用方必须跳过，
// 否则同一条短信会被发两次。
func (r *TextMessageRepo) ClaimPendingRetry(ctx context.Context, id string) (bool, error) {
	db := r.GetDB(ctx)

	result := db.Model(&models.TextMessage{}).
		Where("id = ? AND status = ?", id, models.MessageStatusPending).
		Update("status", models.MessageStatusSending)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// RequeuePendingRetry 补发过程中写串口失败时退回 pending，等下一轮再试。
//
// 只回退「当前还是 sending」的记录：如果这期间设备回了失败回执
// （handleSMSSendResult 已把它改成 failed），不能覆盖成 pending 反复重试。
func (r *TextMessageRepo) RequeuePendingRetry(ctx context.Context, id string) error {
	db := r.GetDB(ctx)

	return db.Model(&models.TextMessage{}).
		Where("id = ? AND status = ?", id, models.MessageStatusSending).
		Update("status", models.MessageStatusPending).Error
}

// ExpirePendingRetry 把已过补发截止时间的 pending 判为失败，返回处理条数。
//
// 截止时间用 retry_until（毫秒）。retry_until = 0 的老数据不参与，
// 避免把历史记录里状态异常的数据一起清掉。
func (r *TextMessageRepo) ExpirePendingRetry(ctx context.Context, deviceID string, nowMs int64) (int64, error) {
	db := r.GetDB(ctx)

	query := db.Model(&models.TextMessage{}).
		Where("status = ? AND retry_until > 0 AND retry_until <= ?", models.MessageStatusPending, nowMs)
	if deviceID != "" {
		query = query.Where("device_id = ?", deviceID)
	}

	result := query.Update("status", models.MessageStatusFailed)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// RecoverStalePendingRetry 进程重启后把停在 sending 的补发记录捞回来。
//
// 补发是「抢占 pending → 写串口 → 等回执」的三段流程，进程若在中间挂掉，
// 记录会永久卡在 sending（既不会被补发，也不会超时判失败）。
// 启动时统一退回 pending 是安全的：真的已经发出去的，回执早就不存在了，
// 但重复补发一条的可能性远小于永远卡住的概率。
func (r *TextMessageRepo) RecoverStalePendingRetry(ctx context.Context, deviceID string) (int64, error) {
	db := r.GetDB(ctx)

	query := db.Model(&models.TextMessage{}).
		Where("status = ? AND retry_until > 0", models.MessageStatusSending)
	if deviceID != "" {
		query = query.Where("device_id = ?", deviceID)
	}

	result := query.Update("status", models.MessageStatusPending)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// CountPendingRetry 统计当前排队条数（用于通知与日志）。
func (r *TextMessageRepo) CountPendingRetry(ctx context.Context, deviceID string) (int64, error) {
	db := r.GetDB(ctx)

	var count int64
	query := db.Model(&models.TextMessage{}).Where("status = ?", models.MessageStatusPending)
	if deviceID != "" {
		query = query.Where("device_id = ?", deviceID)
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// 确保 gorm 被引用（GetDB 定义在 orz.Repository 上，这里只用其返回的 *gorm.DB）
var _ = gorm.ErrRecordNotFound

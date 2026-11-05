package service

import (
	"context"
	"fmt"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"go.uber.org/zap"
)

// 本文件是「离线补发队列」的业务封装，数据操作见 repo/text_message_retry.go。
//
// 为什么补发要单拎一层：补发时的状态迁移都是条件更新（CAS），
// 上层只关心「抢到了没有 / 退回 / 过期清理」这三件事的语义，
// 不该在串口服务里拼 SQL。

// ListPendingRetry 取待补发的短信（按创建时间升序）。
func (s *TextMessageService) ListPendingRetry(ctx context.Context, deviceID string, limit int) ([]models.TextMessage, error) {
	msgs, err := s.repo.ListPendingRetry(ctx, deviceID, limit)
	if err != nil {
		s.logger.Error("查询待补发短信失败",
			zap.String("device_id", deviceID),
			zap.Error(err))
		return nil, fmt.Errorf("查询待补发短信失败: %w", err)
	}
	return msgs, nil
}

// ClaimPendingRetry 抢占一条待补发记录。返回 false 表示被别人取走，必须跳过。
func (s *TextMessageService) ClaimPendingRetry(ctx context.Context, id string) (bool, error) {
	ok, err := s.repo.ClaimPendingRetry(ctx, id)
	if err != nil {
		s.logger.Error("抢占待补发记录失败", zap.String("id", id), zap.Error(err))
		return false, err
	}
	return ok, nil
}

// RequeuePendingRetry 写串口失败时退回排队状态。
func (s *TextMessageService) RequeuePendingRetry(ctx context.Context, id string) error {
	if err := s.repo.RequeuePendingRetry(ctx, id); err != nil {
		s.logger.Error("退回待补发状态失败", zap.String("id", id), zap.Error(err))
		return err
	}
	return nil
}

// ExpirePendingRetry 把超过补发有效期的记录判为失败，返回条数。
func (s *TextMessageService) ExpirePendingRetry(ctx context.Context, deviceID string, nowMs int64) (int64, error) {
	n, err := s.repo.ExpirePendingRetry(ctx, deviceID, nowMs)
	if err != nil {
		s.logger.Error("清理超期补发记录失败",
			zap.String("device_id", deviceID), zap.Error(err))
		return 0, err
	}
	return n, nil
}

// RecoverStalePendingRetry 启动时回收卡在 sending 的补发记录。
func (s *TextMessageService) RecoverStalePendingRetry(ctx context.Context, deviceID string) (int64, error) {
	n, err := s.repo.RecoverStalePendingRetry(ctx, deviceID)
	if err != nil {
		s.logger.Error("回收残留补发记录失败",
			zap.String("device_id", deviceID), zap.Error(err))
		return 0, err
	}
	return n, nil
}

// CountPendingRetry 当前排队条数。
func (s *TextMessageService) CountPendingRetry(ctx context.Context, deviceID string) (int64, error) {
	return s.repo.CountPendingRetry(ctx, deviceID)
}

package service

import (
	"context"
	"fmt"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"go.uber.org/zap"
)

// 本文件实现两件与「SIM 掉线」相关的补偿逻辑：
//
//  1. 离线补发：SIM 不在线时提交的短信不直接判失败，而是入库排队（status=pending），
//     等卡恢复注册后自动补发。有效期 pendingRetryWindow，超期判失败——
//     否则离线一整夜的短信在第二天早上突然发出去，对收件人毫无意义。
//
//  2. 掉线兜底上报：掉线通知发出后，若 offlineFallbackDelay 内始终没有
//     「上线通知」送达（卡一直没恢复，或恢复时通知被冷却吞掉），
//     到点强制推一次当前卡状态。用户要的是「别让我一直不知道卡到底怎么样了」，
//     所以这一条不看在线与否，报了再说。
//
// 两者的定时驱动共用同一个 ticker，避免每个设备起一堆 goroutine。

const (
	// pendingRetryWindow 离线短信的补发有效期
	pendingRetryWindow = 60 * time.Minute

	// offlineFallbackDelay 掉线后兜底状态上报的等待时长。
	//
	// 用户要求「60 分钟内没收到上线通知就上报」，定 65 分钟是为了给正常的
	// 上线通知留 5 分钟余量：真恢复时那条通知一般秒级就到，
	// 等到 65 分钟还没送达，说明情况确实不对，该强制报一次了。
	offlineFallbackDelay = 65 * time.Minute

	// pendingSweepInterval 后台巡检间隔。
	// 1 分钟足够细：补发的时效本来就在分钟级，再密只是白烧 CPU。
	pendingSweepInterval = time.Minute

	// pendingRetryBatch 单轮最多补发条数，避免恢复瞬间把几十条短信
	// 一股脑推给模块——模块侧的发送队列会堵，回执超时后集体变 unknown。
	pendingRetryBatch = 5

	// pendingRetryGap 连续补发之间的间隔，给模块留出发送与回执的时间。
	pendingRetryGap = 2 * time.Second
)

// startPendingSupervisor 启动后台巡检（每设备一次，进程级生命周期）。
func (s *SerialService) startPendingSupervisor() {
	s.supervisorOnce.Do(func() {
		ctx := context.Background()

		// 进程上次可能在「抢占 pending -> 写串口 -> 等回执」的中间挂掉，
		// 启动时把卡在 sending 的补发记录捞回 pending。
		if n, err := s.textMsgService.RecoverStalePendingRetry(ctx, s.deviceID); err != nil {
			s.logger.Warn("回收残留补发记录失败", zap.Error(err))
		} else if n > 0 {
			s.logger.Info("已回收上次未完成的补发记录",
				zap.String("device_id", s.deviceID), zap.Int64("count", n))
		}

		go func() {
			ticker := time.NewTicker(pendingSweepInterval)
			defer ticker.Stop()
			for range ticker.C {
				// 顺序有讲究：先把超期的判失败，再补发剩下的，
				// 避免刚补发出去的记录在同一轮被判超期。
				s.expirePendingMessages()
				s.flushPendingMessages("periodic")
				s.checkOfflineFallback()
			}
		}()
	})
}

// canSendNow 判断当前是否具备发送条件。
//
// 返回第二个值便于日志定位：到底是串口断了、飞行模式，还是卡没注册。
func (s *SerialService) canSendNow() (bool, string) {
	if _, connected := s.getConnectionInfo(); !connected {
		return false, "serial_disconnected"
	}
	if s.flyMode.Load() {
		return false, "flymode_on"
	}
	if s.simOnline.Load() != 1 {
		return false, "sim_offline"
	}
	return true, ""
}

// flushPendingMessages 尝试把排队的短信发出去。
func (s *SerialService) flushPendingMessages(reason string) {
	if ok, why := s.canSendNow(); !ok {
		// 不满足条件时静默返回：后台每分钟都会来一次，
		// 在这里打日志会把日志刷满。
		_ = why
		return
	}

	ctx := context.Background()
	msgs, err := s.textMsgService.ListPendingRetry(ctx, s.deviceID, pendingRetryBatch)
	if err != nil || len(msgs) == 0 {
		return
	}

	s.logger.Info("开始补发离线期间排队的短信",
		zap.String("device_id", s.deviceID),
		zap.String("reason", reason),
		zap.Int("count", len(msgs)))

	sent := 0
	for i := range msgs {
		if ok, why := s.canSendNow(); !ok {
			s.logger.Warn("补发中断：设备当前不可发送",
				zap.String("device_id", s.deviceID),
				zap.String("reason", why))
			break
		}
		if s.retryPendingOne(&msgs[i]) {
			sent++
			time.Sleep(pendingRetryGap)
		}
	}

	if sent > 0 {
		s.logger.Info("本轮补发完成",
			zap.String("device_id", s.deviceID),
			zap.Int("sent", sent))
	}
}

// retryPendingOne 补发单条。返回 true 表示已成功写入串口（结果仍等设备回执）。
func (s *SerialService) retryPendingOne(m *models.TextMessage) bool {
	// 与正常发信共用同一把锁：UART 上两条命令的字节交错会让模块收到半截帧，
	// 而且「写串口 -> 等回执」需要保持串行语义。
	s.smsSendMu.Lock()
	defer s.smsSendMu.Unlock()

	ctx := context.Background()

	// 条件更新抢占：后台巡检和「SIM 上线事件」可能同时触发补发，
	// 不抢占就会同一条发两遍。
	claimed, err := s.textMsgService.ClaimPendingRetry(ctx, m.ID)
	if err != nil || !claimed {
		return false
	}

	cmd := map[string]any{
		"action":     "send_sms",
		"to":         m.To,
		"content":    m.Content,
		"request_id": m.ID, // 沿用原消息 ID，回执仍能对上这条记录
	}
	if err := s.sendJSONCommand(cmd); err != nil {
		// 写串口失败：退回排队，下一轮再试。有效期由 expire 逻辑兜底。
		if requeueErr := s.textMsgService.RequeuePendingRetry(ctx, m.ID); requeueErr != nil {
			s.logger.Error("退回补发状态失败，该条可能卡在 sending",
				zap.String("id", m.ID), zap.Error(requeueErr))
		}
		s.logger.Warn("补发写串口失败，已退回队列",
			zap.String("device_id", s.deviceID),
			zap.String("id", m.ID),
			zap.Error(err))
		return false
	}

	s.logger.Info("补发短信已写入串口",
		zap.String("device_id", s.deviceID),
		zap.String("id", m.ID),
		zap.String("to", m.To))
	return true
}

// expirePendingMessages 把超过有效期的排队短信判失败，并汇总通知一次。
func (s *SerialService) expirePendingMessages() {
	ctx := context.Background()
	n, err := s.textMsgService.ExpirePendingRetry(ctx, s.deviceID, time.Now().UnixMilli())
	if err != nil || n == 0 {
		return
	}

	s.logger.Warn("排队短信超过补发有效期，已判失败",
		zap.String("device_id", s.deviceID),
		zap.Int64("count", n))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("通知 goroutine panic（已恢复）",
					zap.String("device_id", s.deviceID),
					zap.Any("recover", r))
			}
		}()
		s.sendNotificationMessage(context.Background(), NotificationMessage{
			Type: "sms",
			From: "",
			Content: fmt.Sprintf("%d 条短信在 SIM 离线期间未能补发，已超过 %d 分钟有效期判为失败",
				n, int(pendingRetryWindow.Minutes())),
			Timestamp:  time.Now().Unix(),
			DeviceID:   s.deviceID,
			DeviceName: s.displayName(),
		})
	}()
}

// checkOfflineFallback 掉线兜底状态上报。
//
// 触发条件：掉线通知已送出，但 offlineFallbackDelay 内没有「上线通知」送出。
// 这时不管卡此刻是否已恢复，都强制报一次真实状态——报的是「现在到底怎么样」，
// 而不是「当初掉了」。
func (s *SerialService) checkOfflineFallback() {
	since := s.offlineNotifySince.Load()
	if since == 0 {
		return
	}

	elapsed := time.Since(time.Unix(since, 0))
	if elapsed < offlineFallbackDelay {
		return
	}

	// 只报一次：CAS 抢占，避免重复上报。
	if !s.offlineFallbackFired.CompareAndSwap(false, true) {
		return
	}

	s.pushOfflineFallbackReport(elapsed)
}

// pushOfflineFallbackReport 组装并推送兜底状态。
func (s *SerialService) pushOfflineFallbackReport(offlineFor time.Duration) {
	ctx := context.Background()

	if !s.propertyService.GetSimStatusNotifyEnabled(ctx) {
		return
	}

	online := s.simOnline.Load() == 1

	var st *StatusData
	if cached, ok := s.deviceCache.Get(CacheKeyDeviceStatus); ok {
		st = cached
	}

	phone, detail := describeSimStatus(st)
	minutes := int(offlineFor.Minutes())

	headline := fmt.Sprintf("掉线已持续 %d 分钟，仍未恢复", minutes)
	if online {
		headline = fmt.Sprintf("掉线约 %d 分钟后已恢复，但上线通知未送达，此处补报", minutes)
	}

	s.sendNotificationMessage(ctx, NotificationMessage{
		Type:       "sim_status_report",
		From:       phone,
		Content:    fmt.Sprintf("%s\n%s", headline, detail),
		Timestamp:  time.Now().Unix(),
		DeviceID:   s.deviceID,
		DeviceName: s.displayName(),
	})

	s.logger.Info("已推送 SIM 状态兜底上报",
		zap.String("device_id", s.deviceID),
		zap.Bool("online", online),
		zap.Int("offline_minutes", minutes))
}

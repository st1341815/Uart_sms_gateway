package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// simNotifyCooldown 同一方向（上线、掉线各自独立）两次通知的最小间隔。
//
// 定 20 秒的依据：模块重连抖动通常只持续数秒，20 秒足以压住；
// 而实测「飞行模式开关一次」的掉线→恢复往返约 21 秒，90 秒的旧值会把
// 紧随其后的上线通知一并吞掉——掉线后很快恢复恰恰是最该完整成对的场景。
const simNotifyCooldown = 20 * time.Second

// checkSimStatusChange 比较本次状态帧与上次观测到的注册状态，变化时推送通知。
//
// 状态判定用 `is_registered && sim_ready` 而不是只看其中一个：
// 卡没插好时 is_registered 可能仍为 true（模块沿用了上一次的注册状态），
// 只有 sim_ready 才是"卡本身在位且可用"。
//
// 首次状态帧（进程刚启动）只建立基线不通知 —— 否则每次重启服务都会误推一条
// 「SIM 卡上线」，用户无法区分是真的上线还是服务重启。
func (s *SerialService) checkSimStatusChange(st *StatusData) {
	online := st.Mobile.IsRegistered && st.Mobile.SimReady
	cur := int32(0)
	if online {
		cur = 1
	}

	prev := s.simOnline.Swap(cur)
	if prev < 0 {
		s.logger.Info("已记录 SIM 初始状态（仅建立基线，不推送）",
			zap.String("device_id", s.deviceID),
			zap.Bool("online", online),
			zap.String("operator", st.Mobile.Operator))
		return
	}
	if prev == cur {
		return
	}

	// 卡恢复在线：离线期间排队的短信可以补发了。
	// 异步执行，不阻塞状态帧处理——补发要逐条写串口，耗时可到分钟级。
	if online {
		go s.flushPendingMessages("sim_online")
	}

	s.notifySimStatus(online, st)
}

// markSimOfflineOnDisconnect 串口断开时补发一次掉线通知。
//
// 状态帧只在设备在线时才会上报：设备整机掉线（断电、USB 松脱、卡被停机）时
// 上行链路是静默的，只靠状态帧会漏掉这类最常见的掉线场景，所以由连接层补一刀。
func (s *SerialService) markSimOfflineOnDisconnect() {
	// 之前不在线就不必通知；Swap 同时把状态归位成离线
	if s.simOnline.Swap(0) != 1 {
		return
	}
	s.notifySimStatus(false, nil)
}

// notifySimStatus 组装并发送 SIM 上线/掉线通知
func (s *SerialService) notifySimStatus(online bool, st *StatusData) {
	ctx := context.Background()

	if !s.propertyService.GetSimStatusNotifyEnabled(ctx) {
		s.logger.Debug("SIM 上下线通知已关闭，跳过推送",
			zap.String("device_id", s.deviceID),
			zap.Bool("online", online))
		return
	}

	now := time.Now()
	// 按方向取各自的计时器：掉线的冷却不该拦住随后的上线通知
	lastAt := &s.lastOfflineNotifyAt
	if online {
		lastAt = &s.lastOnlineNotifyAt
	}
	if last := lastAt.Load(); last > 0 {
		if since := now.Sub(time.Unix(last, 0)); since < simNotifyCooldown {
			s.logger.Info("SIM 状态通知处于冷却期，跳过",
				zap.String("device_id", s.deviceID),
				zap.Bool("online", online),
				zap.Duration("since_last", since))
			return
		}
	}
	lastAt.Store(now.Unix())

	msgType := "sim_offline"
	if online {
		msgType = "sim_online"
	}

	phone, detail := describeSimStatus(st)

	s.sendNotificationMessage(ctx, NotificationMessage{
		Type:       msgType,
		From:       phone,
		Content:    detail,
		Timestamp:  now.Unix(),
		DeviceID:   s.deviceID,
		DeviceName: s.displayName(),
	})

	s.logger.Info("SIM 状态变化，已推送通知",
		zap.String("device_id", s.deviceID),
		zap.Bool("online", online),
		zap.String("phone", phone))

	// 维护「掉线悬空事件」的计时：
	//   掉线通知送达 -> 开始计时，65 分钟内若没有上线通知送达，强制补报一次真实状态；
	//   上线通知送达 -> 这次中断已经成对闭环，撤销兜底计时。
	// 注意这里只能以「已送达通知渠道」为准 —— sendNotificationMessage 不返回成败，
	// 各渠道的发送结果是它自己记日志的，这里拿不到更细的粒度。
	if online {
		s.offlineNotifySince.Store(0)
		s.offlineFallbackFired.Store(false)
	} else {
		s.offlineNotifySince.Store(now.Unix())
		s.offlineFallbackFired.Store(false)
	}
}

// describeSimStatus 生成通知正文里的卡信息。
//
// st 为 nil 表示「因串口断开而判定掉线」，此时没有最新状态帧，
// 只能说明原因，不能编造运营商/信号。
func describeSimStatus(st *StatusData) (phone string, detail string) {
	if st == nil {
		return "", "原因: 设备串口连接已断开"
	}

	phone = st.Mobile.Number
	if phone == "" {
		phone = "未读取到号码"
	}

	operator := st.Mobile.Operator
	if operator == "" {
		operator = "未知运营商"
	}

	iccid := st.Mobile.Iccid
	switch {
	case iccid == "":
		iccid = "未读取到"
	case len(iccid) > 8:
		// 只露后 4 位：ICCID 是卡的唯一标识，完整值泄露等于暴露卡片身份
		iccid = "****" + iccid[len(iccid)-4:]
	}

	lines := []string{
		fmt.Sprintf("运营商: %s", operator),
		fmt.Sprintf("号码: %s", phone),
		fmt.Sprintf("ICCID: %s", iccid),
		fmt.Sprintf("信号: CSQ %d", st.Mobile.Csq),
	}
	if st.Mobile.IsRoaming {
		lines = append(lines, "漫游: 是")
	}

	return phone, strings.Join(lines, "\n")
}

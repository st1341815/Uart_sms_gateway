package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/config"
	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/go-orz/cache"
	"github.com/google/uuid"
	"github.com/jpillora/backoff"
	"go.bug.st/serial"
	"go.uber.org/zap"
)

const (
	// 缓存键
	CacheKeyDeviceStatus = "device_status"
	// 缓存刷新间隔
	CacheRefreshInterval = 10 * time.Second
	// 缓存过期时间
	CacheTTL = 5 * time.Minute
)

type ScheduledTaskStatusUpdater func(ctx context.Context, msgID string, status models.LastRunStatus) error

// SerialService 串口管理服务（单设备）
type SerialService struct {
	logger                     *zap.Logger
	deviceID                   string              // 设备唯一标识
	deviceConfig               config.DeviceConfig // 设备配置信息
	config                     config.SerialConfig
	port                       serial.Port
	textMsgService             *TextMessageService
	notifier                   *Notifier
	propertyService            *PropertyService
	handlers                   map[string]messageHandler
	scheduledTaskStatusUpdater ScheduledTaskStatusUpdater
	wg                         sync.WaitGroup
	// 设备信息缓存
	deviceCache cache.Cache[string, *StatusData]
	// 连接状态管理
	mu        sync.RWMutex
	portName  string // 当前使用的串口名称
	connected bool   // 连接状态

	// writeMu 保护串口写操作：定时状态查询、发信、飞行模式/重启命令都会写同一个串口，
	// 不加锁会让两条命令的字节在 UART 上交错，设备收到半截帧（静默失败）。
	writeMu sync.Mutex
	// smsSendMu 串行化「整条发信流程」（入库 -> 写串口 -> 等回执），
	// 避免多条短信同时发出时 request_id 与回执错配。
	smsSendMu sync.Mutex

	// 设备的飞行模式查询永远返回 false，无奈只能在应用层处理
	flyMode atomic.Bool

	// SIM 上下线监控
	//
	// simOnline 记录上一次观测到的在线状态：-1 = 未知（进程刚启动，尚未收到
	// 第一个状态帧），0 = 离线，1 = 在线。首次状态帧只记录不通知——否则每次
	// 重启服务都会误推一条"SIM 卡上线"。
	simOnline atomic.Int32
	// 上下线各自独立计时（Unix 秒）。
	//
	// 早期版本共用一个 lastSimNotifyAt，结果「掉线 → 21 秒后恢复」这种
	// 真实事件里，上线通知被掉线的冷却窗口吞掉——而掉线后很快恢复恰恰是
	// 最该完整成对的场景（一次中断的始末）。按方向分别计时后，
	// 同方向的抖动仍被抑制，反方向的成对事件不再互相干扰。
	lastOnlineNotifyAt  atomic.Int64
	lastOfflineNotifyAt atomic.Int64

	// offlineNotifySince 掉线通知真正送达用户的时刻（Unix 秒）；
	// 0 表示当前没有「发了掉线但还没等到上线」的悬空事件。
	// 由 checkOfflineFallback 依据它判断是否到了强制补报的点。
	offlineNotifySince atomic.Int64
	// offlineFallbackFired 本轮掉线是否已经补报过（一次中断只报一次）。
	offlineFallbackFired atomic.Bool
	// supervisorOnce 保证后台巡检 goroutine 每个设备只起一次
	// （Start() 在串口断开会反复循环，不能每次都 go 一个）。
	supervisorOnce sync.Once
}

// NewSerialService 创建串口服务实例（兼容旧模式）
func NewSerialService(
	logger *zap.Logger,
	serialConfig config.SerialConfig,
	textMsgService *TextMessageService,
	notifier *Notifier,
	propertyService *PropertyService,
) *SerialService {
	return NewSerialServiceWithDeviceID(logger, "default", serialConfig, config.DeviceConfig{}, textMsgService, notifier, propertyService)
}

// NewSerialServiceWithDeviceID 创建带设备ID的串口服务实例
func NewSerialServiceWithDeviceID(
	logger *zap.Logger,
	deviceID string,
	serialConfig config.SerialConfig,
	deviceConfig config.DeviceConfig,
	textMsgService *TextMessageService,
	notifier *Notifier,
	propertyService *PropertyService,
) *SerialService {
	// 如果 deviceConfig.Name 为空，使用 deviceID 作为名称
	if deviceConfig.Name == "" {
		deviceConfig.Name = deviceID
	}
	if deviceConfig.ID == "" {
		deviceConfig.ID = deviceID
	}

	service := &SerialService{
		logger:          logger,
		deviceID:        deviceID,
		deviceConfig:    deviceConfig,
		config:          serialConfig,
		textMsgService:  textMsgService,
		notifier:        notifier,
		propertyService: propertyService,
		deviceCache:     cache.New[string, *StatusData](CacheTTL),
	}
	service.initMessageHandlers()
	// -1 = 未知：首个状态帧只用于建立基线，不推送通知
	service.simOnline.Store(-1)
	return service
}

func (s *SerialService) SetScheduledTaskStatusUpdater(updater ScheduledTaskStatusUpdater) {
	s.scheduledTaskStatusUpdater = updater
}

// GetDeviceID 获取设备ID
func (s *SerialService) GetDeviceID() string {
	return s.deviceID
}

// GetDeviceConfig 获取设备配置
func (s *SerialService) GetDeviceConfig() config.DeviceConfig {
	return s.deviceConfig
}

// Start 启动串口服务（使用 backoff 重连机制）
func (s *SerialService) Start() {
	// 补发队列巡检 + 掉线兜底上报（每台设备只起一次，
	// Start 因串口断开会被反复回到这里）。
	s.startPendingSupervisor()

	// 启动主循环
	b := &backoff.Backoff{
		Min:    5 * time.Second,
		Max:    1 * time.Minute,
		Factor: 2,
		Jitter: true,
	}

	for {
		err := s.runOnce(b.Reset)

		// 连接失败或断开，使用 backoff 重试
		if err != nil {
			s.setConnected(false)
			// 状态帧只在设备在线时才会上报，整机掉线时上行静默，
			// 这里补一刀，避免"卡掉了但没人知道"。
			s.markSimOfflineOnDisconnect()
			retryAfter := b.Duration()
			s.logger.Warn("串口连接异常，将重试",
				zap.Error(err),
				zap.Duration("retry_after", retryAfter))
			s.deviceCache.Delete(CacheKeyDeviceStatus)

			time.Sleep(retryAfter)
		}
	}
}

// setConnected 设置连接状态
func (s *SerialService) setConnected(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = connected
}

// setPortName 设置串口名称
func (s *SerialService) setPortName(portName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.portName = portName
}

// getConnectionInfo 获取连接信息
func (s *SerialService) getConnectionInfo() (portName string, connected bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.portName, s.connected
}

// runOnce 执行一次连接尝试
func (s *SerialService) runOnce(resetBackoff func()) error {
	// 获取串口列表
	ports, err := serial.GetPortsList()
	if err != nil {
		return fmt.Errorf("获取串口列表失败: %w", err)
	}

	if len(ports) == 0 {
		return fmt.Errorf("未发现可用串口")
	}

	s.logger.Debug("发现可用串口", zap.Strings("ports", ports))

	// 确定使用的串口
	var selectedPort string
	if s.config.Port != "" {
		// 使用配置的串口
		selectedPort = s.config.Port
		s.logger.Info("使用配置的串口", zap.String("port", selectedPort))
	} else {
		// 自动检测
		s.logger.Info("开始自动检测串口...")
		selectedPort, err = s.autoDetectPort(ports)
		if err != nil {
			return fmt.Errorf("自动检测串口失败: %w", err)
		}
		s.logger.Info("自动检测到可用串口", zap.String("port", selectedPort))
	}

	// 连接串口
	if err := s.connectSerial(selectedPort); err != nil {
		return fmt.Errorf("连接串口失败: %w", err)
	}

	// 设置连接状态和串口名称
	s.setPortName(selectedPort)
	s.setConnected(true)

	// 重置 backoff（连接成功）
	resetBackoff()

	s.logger.Info("串口连接成功", zap.String("port", selectedPort))

	// 为本次连接创建独立的 context，用于管理连接的生命周期
	connCtx, connCancel := context.WithCancel(context.Background())
	defer connCancel() // 确保退出时取消 context

	// 启动监听 goroutine
	s.wg.Add(1)
	go s.listenSerialData(connCtx, connCancel)

	// 启动定时更新缓存的 goroutine
	s.wg.Add(1)
	go s.periodicCacheUpdate(connCtx)

	// 首次立即发送缓存更新请求
	go s.RequestCacheUpdate()

	// 等待连接断开
	s.wg.Wait()

	// 连接已断开，更新状态
	s.setConnected(false)

	// ⚠️ 这里必须返回 error，不能返回 nil。
	// Start() 的循环是 `for { err := runOnce(); if err != nil { sleep(backoff) } }`，
	// 返回 nil 会导致不 sleep 立即重连 —— 串口反复打开/关闭高速空转，
	// 既刷爆日志又可能让设备来不及重新枚举。
	return fmt.Errorf("串口连接已断开: %s", selectedPort)
}

// connectSerial 连接串口
func (s *SerialService) connectSerial(portName string) error {
	mode := &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		Parity:   serial.NoParity,
	}

	port, err := serial.Open(portName, mode)
	if err != nil {
		return err
	}

	// 与写操作共用锁：保证赋值 s.port 时没有并发的写/关闭
	s.writeMu.Lock()
	s.port = port
	s.writeMu.Unlock()
	return nil
}

// autoDetectPort 自动检测可用串口
func (s *SerialService) autoDetectPort(ports []string) (string, error) {
	for _, portName := range ports {
		s.logger.Debug("测试串口", zap.String("port", portName))

		mode := &serial.Mode{
			BaudRate: 115200,
			DataBits: 8,
			StopBits: serial.OneStopBit,
			Parity:   serial.NoParity,
		}

		port, err := serial.Open(portName, mode)
		if err != nil {
			s.logger.Debug("打开串口失败", zap.String("port", portName), zap.Error(err))
			continue
		}

		// 设置读取超时
		port.SetReadTimeout(1 * time.Second)

		// 发送测试命令（使用正确的协议格式）
		testCmd := map[string]string{"action": "get_status"}
		jsonData, _ := json.Marshal(testCmd)
		// 添加协议包围标志
		message := fmt.Sprintf("CMD_START:%s:CMD_END\r\n", string(jsonData))

		_, err = port.Write([]byte(message))
		if err != nil {
			port.Close()
			continue
		}

		// 等待响应
		time.Sleep(500 * time.Millisecond)

		buffer := make([]byte, 4096)
		n, err := port.Read(buffer)
		port.Close()

		if err == nil && n > 0 {
			response := string(buffer[:n])
			if isValidResponse(response) {
				s.logger.Debug("检测到可用串口", zap.String("port", portName))
				return portName, nil
			}
		}
	}

	return "", fmt.Errorf("未检测到可用串口")
}

// listenSerialData 监听串口数据（在独立 goroutine 中运行）
func (s *SerialService) listenSerialData(connCtx context.Context, connCancel context.CancelFunc) {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("串口监听 goroutine panic", zap.Any("recover", r))
		}
		// 关闭串口（closeSerial 内部持 writeMu，避免与写操作竞争）
		s.closeSerial()
		// 取消连接 context，通知其他 goroutine 连接已断开
		connCancel()
	}()

	// 取端口快照：整个过程只用本地变量，
	// 避免 s.port 被并发置空导致 nil 解引用。
	s.writeMu.Lock()
	port := s.port
	s.writeMu.Unlock()
	if port == nil {
		return
	}

	// 按 SMS_START:/SMS_END 边界扫描，而不是按行切分：
	// 短信正文可含换行，按行切会让帧错位且永不自愈。
	reader := NewFrameReader(port)

	for {
		select {
		case <-connCtx.Done():
			s.logger.Info("串口监听停止")
			return
		default:
			frame, err := reader.ReadFrame()
			if err != nil {
				if errors.Is(err, io.EOF) {
					s.logger.Warn("串口读取 EOF，设备可能已断开",
						zap.String("device_id", s.deviceID))
					return
				}
				if errors.Is(err, ErrFrameTooLarge) {
					s.logger.Error("串口帧超长，判定数据流异常，触发重连",
						zap.String("device_id", s.deviceID))
					return
				}
				if connCtx.Err() != nil {
					return
				}
				s.logger.Error("读取串口数据错误，退出监听",
					zap.String("device_id", s.deviceID),
					zap.Error(err))
				return
			}

			s.processReceivedData(frame)
		}
	}
}

// periodicCacheUpdate 定时更新缓存
func (s *SerialService) periodicCacheUpdate(connCtx context.Context) {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("定时更新缓存 goroutine panic", zap.Any("recover", r))
		}
	}()

	ticker := time.NewTicker(CacheRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-connCtx.Done():
			s.logger.Info("停止定时更新缓存")
			return
		case <-ticker.C:
			s.RequestCacheUpdate()
		}
	}
}

// RequestCacheUpdate 请求更新缓存（只发送命令，不等待响应）
func (s *SerialService) RequestCacheUpdate() {
	s.logger.Debug("发送缓存更新请求")

	// 发送获取设备状态命令（包含移动网络信息）
	if err := s.sendJSONCommand(map[string]string{"action": "get_status"}); err != nil {
		s.logger.Error("发送设备状态请求失败", zap.Error(err))
	}
}

// processReceivedData 处理接收到的数据
func (s *SerialService) processReceivedData(data []byte) {
	// 只记录长度，不打印原文：原始帧含 IMSI/ICCID/短信正文，
	// 且每 10 秒每设备都会收到状态帧，全量打印会把日志刷到百万行。
	s.logger.Debug("收到串口数据",
		zap.String("device_id", s.deviceID),
		zap.Int("bytes", len(data)))
	msg, err := parseSMSFrame(string(data))
	if err != nil {
		if errors.Is(err, errNotSMSFrame) {
			return
		}
		if errors.Is(err, errMissingType) {
			s.logger.Warn("消息类型缺失",
				zap.String("device_id", s.deviceID),
				zap.Int("bytes", len(data)))
			return
		}
		s.logger.Error("解析串口消息失败",
			zap.String("device_id", s.deviceID),
			zap.Error(err),
			zap.Int("bytes", len(data)))
		return
	}

	s.routeMessage(msg)
}

// SendSMS 发送短信
func (s *SerialService) SendSMS(to, content string) (string, error) {
	// 同一设备的发信流程必须串行：保存记录 -> 写串口 -> 等设备回执，
	// 并发调用会让两条命令的字节在 UART 上交错，导致帧损坏。
	s.smsSendMu.Lock()
	defer s.smsSendMu.Unlock()

	// 先判断当前能不能发：串口连着、没开飞行模式、卡已注册，三个都满足才算。
	// 不拿信号强度做门槛——信号弱不等于发不出去，判了会误伤。
	sendable, notSendableReason := s.canSendNow()

	// 先保存发送记录，状态为 "sending"
	ctx := context.Background()
	msgID := uuid.NewString()
	msg := &models.TextMessage{
		ID:        msgID,
		From:      "", // 发送方是本机
		To:        to,
		Content:   content,
		Type:      models.MessageTypeOutgoing,
		Status:    models.MessageStatusSending, // 初始状态为发送中
		DeviceID:  s.deviceID,                  // 记录发送设备，多卡场景可追溯
		CreatedAt: time.Now().UnixMilli(),
	}
	if !sendable {
		// 卡不在线：不判失败，入库排队等卡恢复后补发。
		// 直接失败会让「离线期间的告警短信」全部石沉大海——恰恰是最需要送达的内容。
		msg.Status = models.MessageStatusPending
		msg.RetryUntil = time.Now().Add(pendingRetryWindow).UnixMilli()
	}

	if err := s.textMsgService.Save(ctx, msg); err != nil {
		s.logger.Error("保存短信发送记录失败", zap.Error(err))
		return "", err
	}

	if !sendable {
		s.logger.Info("SIM 当前不可发送，短信已排队等待补发",
			zap.String("device_id", s.deviceID),
			zap.String("to", to),
			zap.String("request_id", msgID),
			zap.String("reason", notSendableReason),
			zap.Time("retry_until", time.UnixMilli(msg.RetryUntil)))
		// 返回 nil 表示「已受理」：调用方不该因为卡暂时离线就当成发送失败，
		// 短信会在补发有效期内的任意时刻真正发出。
		return msgID, nil
	}

	// 发送命令，使用消息 ID 作为 request_id
	cmd := map[string]any{
		"action":     "send_sms",
		"to":         to,
		"content":    content,
		"request_id": msgID,
	}

	if err := s.sendJSONCommand(cmd); err != nil {
		s.logger.Error("发送短信命令失败", zap.Error(err))
		// 更新状态为失败
		_ = s.textMsgService.UpdateStatusById(ctx, msgID, models.MessageStatusFailed)
		return "", err
	}

	s.logger.Info("发送短信命令成功",
		zap.String("device_id", s.deviceID),
		zap.String("to", to),
		zap.String("request_id", msgID))

	return msgID, nil
}

// GetStatus 获取设备状态（从缓存读取，包含 mobile 信息和串口连接状态）
func (s *SerialService) GetStatus() (*StatusData, error) {
	// 获取连接信息
	portName, connected := s.getConnectionInfo()

	// 从缓存读取
	if status, ok := s.deviceCache.Get(CacheKeyDeviceStatus); ok {
		// 缓存中的对象可能被多个 HTTP 请求并发读取，
		// 直接修改并返回同一指针会造成 data race（状态串台），
		// 因此返回副本。
		snapshot := *status
		snapshot.PortName = portName
		snapshot.Connected = connected
		snapshot.Flymode = s.FlyMode()
		return &snapshot, nil
	}

	// 缓存未命中，但仍然返回连接状态
	status := &StatusData{
		PortName:  portName,
		Connected: connected,
	}
	return status, nil
}

func (s *SerialService) FlyMode() bool {
	// 返回当前飞行模式状态
	return s.flyMode.Load()
}

// SetFlymode 设置飞行模式
// enabled: true 表示启用飞行模式，false 表示禁用飞行模式
func (s *SerialService) SetFlymode(enabled bool) error {
	cmd := map[string]any{
		"action":  "set_flymode",
		"enabled": enabled,
	}
	if err := s.sendJSONCommand(cmd); err != nil {
		return err
	}
	// 更新飞行模式状态
	s.flyMode.Store(enabled)
	return nil
}

// RebootMcu 重启模块
func (s *SerialService) RebootMcu() error {
	cmd := map[string]string{"action": "reboot_mcu"}
	if err := s.sendJSONCommand(cmd); err != nil {
		return err
	}
	// 重启后，飞行模式默认关闭
	s.flyMode.Store(false)
	return nil
}

// sendJSONCommand 发送JSON命令到设备
// 全程持有 writeMu：保证一次命令的字节完整写出后才轮到下一条，
// 否则并发写会让 UART 上出现交错的半截帧。
func (s *SerialService) sendJSONCommand(cmd any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.port == nil {
		return fmt.Errorf("串口未连接")
	}

	message, _, err := buildCommandMessage(cmd)
	if err != nil {
		return err
	}

	if err := writeAll(s.port, message); err != nil {
		return fmt.Errorf("串口写入失败: %w", err)
	}
	s.logger.Debug("串口命令已发送",
		zap.String("device_id", s.deviceID),
		zap.Int("payload_bytes", len(message)))

	return nil
}

// writeAll 完整写出全部字节。
// 串口在缓冲区满时会「短写」（返回 n < len(data) 且 err == nil），
// 直接丢弃 n 会导致命令被截断，因此必须循环补齐。
func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// closeSerial 关闭串口。与写操作共用 writeMu，
// 避免断连时「关闭端口」与「写端口」产生竞争。
func (s *SerialService) closeSerial() {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.port == nil {
		return
	}
	if err := s.port.Close(); err != nil {
		s.logger.Debug("关闭串口失败",
			zap.String("device_id", s.deviceID),
			zap.Error(err))
	}
	s.port = nil
}

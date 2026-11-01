package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/dushixiang/uart_sms_forwarder/config"
	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"go.uber.org/zap"
)

// DeviceManager 多设备管理器
type DeviceManager struct {
	logger       *zap.Logger
	config       config.SerialConfig
	devices      map[string]*SerialService // 设备ID -> 服务实例
	deviceConfig map[string]config.DeviceConfig
	mu           sync.RWMutex

	// 公共服务（共享）
	textMsgService  *TextMessageService
	notifier        *Notifier
	propertyService *PropertyService

	// 定时任务状态更新器
	scheduledTaskStatusUpdater ScheduledTaskStatusUpdater
}

// DeviceStatus 设备状态摘要
type DeviceStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Port      string `json:"port"`
	Connected bool   `json:"connected"`
	Flymode   bool   `json:"flymode"`
	// 继承 StatusData 的其他字段
	ICCID       string `json:"iccid,omitempty"`
	IMSI        string `json:"imsi,omitempty"`
	Operator    string `json:"operator,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Signal      int    `json:"signal,omitempty"`
	IsRoaming   bool   `json:"is_roaming,omitempty"`
}

// NewDeviceManager 创建设备管理器
func NewDeviceManager(
	logger *zap.Logger,
	serialConfig config.SerialConfig,
	textMsgService *TextMessageService,
	notifier *Notifier,
	propertyService *PropertyService,
) *DeviceManager {
	dm := &DeviceManager{
		logger:          logger,
		config:          serialConfig,
		devices:         make(map[string]*SerialService),
		deviceConfig:    make(map[string]config.DeviceConfig),
		textMsgService:  textMsgService,
		notifier:        notifier,
		propertyService: propertyService,
	}

	// 初始化设备配置映射
	for _, dc := range serialConfig.Devices {
		dm.deviceConfig[dc.ID] = dc
	}

	return dm
}

// SetScheduledTaskStatusUpdater 设置定时任务状态更新器
func (dm *DeviceManager) SetScheduledTaskStatusUpdater(updater ScheduledTaskStatusUpdater) {
	dm.scheduledTaskStatusUpdater = updater
	// 同步设置到所有设备
	dm.mu.RLock()
	for _, ss := range dm.devices {
		ss.SetScheduledTaskStatusUpdater(updater)
	}
	dm.mu.RUnlock()
}

// Initialize 初始化所有设备
// 兼容模式：如果使用旧配置 Port，创建单设备模式
func (dm *DeviceManager) Initialize() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// 兼容旧配置：如果 Devices 为空但 Port 有值
	if len(dm.config.Devices) == 0 && dm.config.Port != "" {
		dm.logger.Info("使用旧配置模式（单设备）", zap.String("port", dm.config.Port))
		dc := config.DeviceConfig{
			ID:      "default",
			Name:    "默认设备",
			Port:    dm.config.Port,
			Enabled: true,
		}
		dm.config.Devices = []config.DeviceConfig{dc}
		dm.deviceConfig["default"] = dc
	}

	// 如果 Devices 也为空，自动检测模式
	if len(dm.config.Devices) == 0 {
		dm.logger.Info("自动检测模式：未配置设备，将使用自动检测")
		dc := config.DeviceConfig{
			ID:      "auto",
			Name:    "自动检测设备",
			Port:    "", // 自动检测
			Enabled: true,
		}
		dm.config.Devices = []config.DeviceConfig{dc}
		dm.deviceConfig["auto"] = dc
	}

	// 为每个启用的设备创建 SerialService
	for _, dc := range dm.config.Devices {
		if !dc.Enabled {
			dm.logger.Info("设备已禁用，跳过", zap.String("id", dc.ID))
			continue
		}

		// 创建设备级别的配置（转换为旧格式供 SerialService 使用）
		deviceSerialConfig := config.SerialConfig{
			Port: dc.Port,
		}

		ss := NewSerialServiceWithDeviceID(
			dm.logger,
			dc.ID,
			deviceSerialConfig,
			dc,
			dm.textMsgService,
			dm.notifier,
			dm.propertyService,
		)

		if dm.scheduledTaskStatusUpdater != nil {
			ss.SetScheduledTaskStatusUpdater(dm.scheduledTaskStatusUpdater)
		}

		dm.devices[dc.ID] = ss
		dm.logger.Info("设备服务已初始化",
			zap.String("id", dc.ID),
			zap.String("name", dc.Name),
			zap.String("port", dc.Port))
	}

	if len(dm.devices) == 0 {
		return fmt.Errorf("没有可用的设备配置")
	}

	return nil
}

// StartAll 启动所有设备服务
func (dm *DeviceManager) StartAll() {
	dm.mu.RLock()
	for id, ss := range dm.devices {
		dm.logger.Info("启动设备服务", zap.String("id", id))
		go ss.Start()
	}
	dm.mu.RUnlock()
}

// GetDevice 获取指定设备的服务实例
func (dm *DeviceManager) GetDevice(id string) (*SerialService, bool) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	ss, ok := dm.devices[id]
	return ss, ok
}

// GetFirstEnabledDevice 获取第一个启用的设备（用于默认发送）
func (dm *DeviceManager) GetFirstEnabledDevice() (*SerialService, string) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	for id, ss := range dm.devices {
		return ss, id
	}
	return nil, ""
}

// GetAllDeviceIDs 获取所有设备ID列表
func (dm *DeviceManager) GetAllDeviceIDs() []string {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	ids := make([]string, 0, len(dm.devices))
	for id := range dm.devices {
		ids = append(ids, id)
	}
	return ids
}

// GetAllDeviceConfigs 获取所有设备配置
func (dm *DeviceManager) GetAllDeviceConfigs() []config.DeviceConfig {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
 configs := make([]config.DeviceConfig, 0, len(dm.deviceConfig))
	for _, dc := range dm.deviceConfig {
	 configs = append(configs, dc)
	}
	return configs
}

// GetAllStatus 获取所有设备状态
func (dm *DeviceManager) GetAllStatus() []*DeviceStatus {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	statuses := make([]*DeviceStatus, 0, len(dm.devices))
	for id, ss := range dm.devices {
		status, err := ss.GetStatus()
		deviceConfig := dm.deviceConfig[id]

		ds := &DeviceStatus{
			ID:        id,
			Name:      deviceConfig.Name,
			Port:      deviceConfig.Port,
			Connected: status.Connected,
			Flymode:   status.Flymode,
		}

		if err == nil {
			ds.ICCID = status.Mobile.Iccid
			ds.IMSI = status.Mobile.Imsi
			ds.Operator = status.Mobile.Operator
			ds.PhoneNumber = status.Mobile.Number
			ds.Signal = status.Mobile.SignalLevel
			ds.IsRoaming = status.Mobile.IsRoaming
		}

		statuses = append(statuses, ds)
	}

	return statuses
}

// GetDeviceStatus 获取单个设备状态
func (dm *DeviceManager) GetDeviceStatus(id string) (*DeviceStatus, error) {
	dm.mu.RLock()
	ss, ok := dm.devices[id]
	dc := dm.deviceConfig[id]
	dm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("设备不存在: %s", id)
	}

	status, err := ss.GetStatus()
	ds := &DeviceStatus{
		ID:        id,
		Name:      dc.Name,
		Port:      dc.Port,
		Connected: status.Connected,
		Flymode:   status.Flymode,
	}

	if err == nil {
		ds.ICCID = status.Mobile.Iccid
		ds.IMSI = status.Mobile.Imsi
		ds.Operator = status.Mobile.Operator
		ds.PhoneNumber = status.Mobile.Number
		ds.Signal = status.Mobile.SignalLevel
		ds.IsRoaming = status.Mobile.IsRoaming
	}

	return ds, nil
}

// SendSMS 通过指定设备发送短信
func (dm *DeviceManager) SendSMS(deviceID, to, content string) (string, error) {
	// 如果未指定设备，使用第一个启用的设备
	if deviceID == "" {
		ss, id := dm.GetFirstEnabledDevice()
		if ss == nil {
			return "", fmt.Errorf("没有可用的设备")
		}
		deviceID = id
	}

	dm.mu.RLock()
	ss, ok := dm.devices[deviceID]
	dm.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("设备不存在: %s", deviceID)
	}

	return ss.SendSMS(to, content)
}

// SetFlymode 设置指定设备的飞行模式
func (dm *DeviceManager) SetFlymode(deviceID string, enabled bool) error {
	if deviceID == "" {
		deviceID, _ = dm.GetFirstEnabledDeviceID()
	}

	dm.mu.RLock()
	ss, ok := dm.devices[deviceID]
	dm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("设备不存在: %s", deviceID)
	}

	return ss.SetFlymode(enabled)
}

// RebootMcu 重启指定设备模块
func (dm *DeviceManager) RebootMcu(deviceID string) error {
	if deviceID == "" {
		deviceID, _ = dm.GetFirstEnabledDeviceID()
	}

	dm.mu.RLock()
	ss, ok := dm.devices[deviceID]
	dm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("设备不存在: %s", deviceID)
	}

	return ss.RebootMcu()
}

// RequestCacheUpdate 请求更新所有设备缓存
func (dm *DeviceManager) RequestCacheUpdate() {
	dm.mu.RLock()
	for _, ss := range dm.devices {
		go ss.RequestCacheUpdate()
	}
	dm.mu.RUnlock()
}

// GetFirstEnabledDeviceID 获取第一个启用的设备ID
func (dm *DeviceManager) GetFirstEnabledDeviceID() (string, bool) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	for id := range dm.devices {
		return id, true
	}
	return "", false
}

// SaveTextMessage 保存短信记录（带设备ID）
func (dm *DeviceManager) SaveTextMessage(ctx context.Context, deviceID string, msg *models.TextMessage) error {
	// 可以在消息中标记设备来源
	// 这里直接使用共享的 textMsgService
	return dm.textMsgService.Save(ctx, msg)
}
package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

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
	// order 保存设备的**配置声明顺序**。Go 的 map 遍历顺序是随机的，
	// 所有「第一个设备」「设备列表」都必须走 order，不能直接遍历 map，
	// 否则「未指定设备」时会随机选中某张卡（可能导致国际漫游计费）。
	order []string
	// limiters 每设备一个发信限流器
	limiters *sendRateLimiters
	mu       sync.RWMutex

	// 公共服务（共享）
	textMsgService  *TextMessageService
	notifier        *Notifier
	propertyService *PropertyService

	// 定时任务状态更新器
	scheduledTaskStatusUpdater ScheduledTaskStatusUpdater

	// nameOverrides 运行时改名覆盖层：设备 ID -> 用户自定义名称
	//
	// 为什么不用「直接改 config.DeviceConfig.Name」：
	//   config.yaml 在容器里是**只读挂载**（见 models/user_credential.go 的注释），
	//   程序写不回去，改了也丢。所以自定义名称落库到 properties 表
	//   （ID = device_names，值是 map[deviceID]name），启动时加载，运行期在内存里覆盖。
	//   优先级：DB 覆盖名 > config.yaml 的 Name > ID。
	nameOverrides map[string]string
	// nameMu 只保护 nameOverrides，避免与 dm.mu 嵌套（GetAllStatus 持 dm.mu 时也要读名字）
	nameMu sync.RWMutex

	// phoneOverrides 运行时本机号码覆盖层：设备 ID -> 用户手工填写的号码
	//
	// 为什么需要它：Lua 侧用 mobile.number(0) 取号，底层是 AT+CNUM（读 SIM 卡
	// EF_MSISDN 字段）。国内三大运营商会写这个字段，多数境外卡/漫游卡不写，
	// 于是 number 返回空串——这不是 bug，也无法通过改 Lua 绕过（字段本身是空的）。
	// 因此提供手工覆盖：DB 覆盖号 > SIM MSISDN。
	phoneOverrides map[string]string
	// phoneMu 只保护 phoneOverrides，理由同 nameMu
	phoneMu sync.RWMutex
}

// PropertyIDDeviceNames 设备自定义名称的 property 键
const PropertyIDDeviceNames = "device_names"

// PropertyIDDevicePhoneNumbers 设备本机号码覆盖的 property 键
//
// 值是 map[deviceID]phoneNumber。用于境外卡读不到 MSISDN 时手工补录，
// 让「短信中心按卡分组」有可读的卡标识。
const PropertyIDDevicePhoneNumbers = "device_phone_numbers"

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
	Signal      int    `json:"signal,omitempty"` // CSQ (0-31)，99=未知
	// Rsrp 参考信号接收功率 (dBm，-44 ~ -140)，4G 下比 CSQ 更能反映真实信号质量。
	// 取不到（模组未上报/为默认值 -140 或 0）时省略，前端回退到 CSQ 换算。
	Rsrp      int  `json:"rsrp,omitempty"`
	IsRoaming bool `json:"is_roaming,omitempty"`
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
		order:           make([]string, 0, len(serialConfig.Devices)),
		limiters:        newSendRateLimiters(),
		nameOverrides:   make(map[string]string),
		phoneOverrides:  make(map[string]string),
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
		dm.order = append(dm.order, dc.ID)
		dm.logger.Info("设备服务已初始化",
			zap.String("id", dc.ID),
			zap.String("name", dc.Name),
			zap.String("port", dc.Port))
	}

	if len(dm.devices) == 0 {
		return fmt.Errorf("没有可用的设备配置")
	}

	// 加载用户自定义的设备名称（DB 覆盖层）
	dm.loadNameOverridesLocked()
	// 加载用户手工补录的本机号码（DB 覆盖层）
	dm.loadPhoneOverridesLocked()

	return nil
}

// loadNameOverridesLocked 从 properties 表加载设备自定义名称。
// 调用方必须已持有 dm.mu 写锁（Initialize 里是这么用的），故这里只碰 nameMu。
func (dm *DeviceManager) loadNameOverridesLocked() {
	if dm.propertyService == nil {
		return
	}

	overrides := make(map[string]string)
	// 配置项不存在时 GetValue 直接返回 nil（不报错），此时就是「没有任何改名」
	if err := dm.propertyService.GetValue(context.Background(), PropertyIDDeviceNames, &overrides); err != nil {
		dm.logger.Warn("加载设备自定义名称失败，回退到配置名称", zap.Error(err))
		return
	}

	dm.nameMu.Lock()
	dm.nameOverrides = overrides
	dm.nameMu.Unlock()

	if len(overrides) > 0 {
		dm.logger.Info("已加载设备自定义名称", zap.Int("count", len(overrides)))
	}
}

// loadPhoneOverridesLocked 从 properties 表加载手工补录的本机号码。
// 调用方必须已持有 dm.mu 写锁（Initialize 里是这么用的），故这里只碰 phoneMu。
func (dm *DeviceManager) loadPhoneOverridesLocked() {
	if dm.propertyService == nil {
		return
	}

	overrides := make(map[string]string)
	if err := dm.propertyService.GetValue(context.Background(), PropertyIDDevicePhoneNumbers, &overrides); err != nil {
		dm.logger.Warn("加载本机号码覆盖失败，回退到 SIM 卡读取值", zap.Error(err))
		return
	}

	dm.phoneMu.Lock()
	dm.phoneOverrides = overrides
	dm.phoneMu.Unlock()

	if len(overrides) > 0 {
		dm.logger.Info("已加载本机号码覆盖", zap.Int("count", len(overrides)))
	}
}

// displayPhoneNumber 计算设备的最终本机号码：手工覆盖 > SIM 卡 MSISDN > 空。
//
// SIM 卡读不到号码是常态（多数境外/漫游卡不写 EF_MSISDN 字段），
// 此时返回空串，由调用方或前端决定怎么展示。
func (dm *DeviceManager) displayPhoneNumber(id, simNumber string) string {
	dm.phoneMu.RLock()
	custom, hasCustom := dm.phoneOverrides[id]
	dm.phoneMu.RUnlock()

	if hasCustom && custom != "" {
		return custom
	}
	return simNumber
}

// GetDevicePhoneNumber 获取设备的最终本机号码（供 Handler / 短信分组复用）
func (dm *DeviceManager) GetDevicePhoneNumber(id string) string {
	dm.mu.RLock()
	ss, ok := dm.devices[id]
	dm.mu.RUnlock()

	var simNumber string
	if ok {
		if status, err := ss.GetStatus(); err == nil {
			simNumber = status.Mobile.Number
		}
	}
	return dm.displayPhoneNumber(id, simNumber)
}

// SetDevicePhoneNumber 手工设置设备本机号码（落库，重启不丢）
//
// 用于 SIM 卡不写 MSISDN 字段（境外/漫游卡）时补录；纯展示用途，
// 不影响实际收发（收发用的是 SIM 卡本身）。
func (dm *DeviceManager) SetDevicePhoneNumber(id, number string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("必须指定设备 ID")
	}

	dm.mu.RLock()
	_, exists := dm.deviceConfig[id]
	dm.mu.RUnlock()
	if !exists {
		return "", fmt.Errorf("设备不存在: %s（可选: %v）", id, dm.GetAllDeviceIDs())
	}

	number = strings.TrimSpace(number)
	// 允许空串：表示「清除手工覆盖，回退到 SIM 卡读取值」
	if number != "" {
		if utf8.RuneCountInString(number) > 32 {
			return "", fmt.Errorf("号码过长（最多 32 个字符）")
		}
		for _, r := range number {
			if !(r >= '0' && r <= '9') && r != '+' && r != '-' && r != ' ' {
				return "", fmt.Errorf("号码只能包含数字、+、- 和空格")
			}
		}
	}

	// 复制一份再改，避免把正在被读的 map 原地写坏
	dm.phoneMu.Lock()
	next := make(map[string]string, len(dm.phoneOverrides)+1)
	for k, v := range dm.phoneOverrides {
		next[k] = v
	}
	if number == "" {
		delete(next, id)
	} else {
		next[id] = number
	}
	dm.phoneMu.Unlock()

	if dm.propertyService != nil {
		if err := dm.propertyService.Set(context.Background(), PropertyIDDevicePhoneNumbers, "设备本机号码覆盖", next); err != nil {
			return "", fmt.Errorf("保存本机号码失败: %w", err)
		}
	}

	// 只有落库成功才更新内存，保证「显示的号码」与「重启后的号码」一致
	dm.phoneMu.Lock()
	dm.phoneOverrides = next
	dm.phoneMu.Unlock()

	dm.logger.Info("设备本机号码已更新",
		zap.String("id", id),
		zap.String("number", number))

	return number, nil
}

// displayName 计算设备的最终显示名：DB 覆盖名 > config.yaml 的 Name > ID。
// 不加锁，由调用方保证不持有 nameMu。
func (dm *DeviceManager) displayName(id, configName string) string {
	dm.nameMu.RLock()
	custom, hasCustom := dm.nameOverrides[id]
	dm.nameMu.RUnlock()

	if hasCustom && custom != "" {
		return custom
	}
	if configName != "" {
		return configName
	}
	return id
}

// GetDeviceDisplayName 获取设备的最终显示名（供 Handler 复用）
func (dm *DeviceManager) GetDeviceDisplayName(id string) string {
	dm.mu.RLock()
	dc := dm.deviceConfig[id]
	dm.mu.RUnlock()
	return dm.displayName(id, dc.Name)
}

// RenameDevice 修改设备显示名称（落库，重启不丢）
//
// 校验：
//   - 设备必须存在（避免给不存在的 ID 写入脏数据）
//   - 名称去空格后不能为空，且不宜过长（前端也限制，这里兜底）
func (dm *DeviceManager) RenameDevice(id, name string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("必须指定设备 ID")
	}

	dm.mu.RLock()
	_, exists := dm.deviceConfig[id]
	dm.mu.RUnlock()
	if !exists {
		return "", fmt.Errorf("设备不存在: %s（可选: %v）", id, dm.GetAllDeviceIDs())
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("名称不能为空")
	}
	// 按「字符」而不是字节计长度，避免中文被算成 3 倍
	if utf8.RuneCountInString(name) > 32 {
		return "", fmt.Errorf("名称过长（最多 32 个字符）")
	}

	// 复制一份再改，避免把正在被读的 map 原地写坏
	dm.nameMu.Lock()
	next := make(map[string]string, len(dm.nameOverrides)+1)
	for k, v := range dm.nameOverrides {
		next[k] = v
	}
	next[id] = name
	dm.nameMu.Unlock()

	if dm.propertyService != nil {
		if err := dm.propertyService.Set(context.Background(), PropertyIDDeviceNames, "设备自定义名称", next); err != nil {
			return "", fmt.Errorf("保存设备名称失败: %w", err)
		}
	}

	// 只有落库成功才更新内存，保证「显示的名字」与「重启后的名字」一致
	dm.nameMu.Lock()
	dm.nameOverrides = next
	dm.nameMu.Unlock()

	dm.logger.Info("设备名称已更新",
		zap.String("id", id),
		zap.String("name", name))

	return name, nil
}

// StartAll 启动所有设备服务
func (dm *DeviceManager) StartAll() {
	dm.mu.RLock()
	// 按配置声明顺序启动，保证日志顺序稳定可读
	for _, id := range dm.order {
		ss, ok := dm.devices[id]
		if !ok {
			continue
		}
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
// 注意：必须按配置声明顺序取第一个，绝不能遍历 map（顺序随机）。
func (dm *DeviceManager) GetFirstEnabledDevice() (*SerialService, string) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	for _, id := range dm.order {
		if ss, ok := dm.devices[id]; ok {
			return ss, id
		}
	}
	return nil, ""
}

// GetAllDeviceIDs 获取所有设备ID列表（按配置声明顺序）
func (dm *DeviceManager) GetAllDeviceIDs() []string {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	ids := make([]string, 0, len(dm.order))
	for _, id := range dm.order {
		if _, ok := dm.devices[id]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// GetAllDeviceConfigs 获取所有设备配置（按配置声明顺序）
func (dm *DeviceManager) GetAllDeviceConfigs() []config.DeviceConfig {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	configs := make([]config.DeviceConfig, 0, len(dm.order))
	for _, id := range dm.order {
		dc, ok := dm.deviceConfig[id]
		if !ok {
			continue
		}
		configs = append(configs, dc)
	}
	return configs
}

// GetAllStatus 获取所有设备状态（按配置声明顺序，保证前端下拉顺序稳定）
func (dm *DeviceManager) GetAllStatus() []*DeviceStatus {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	statuses := make([]*DeviceStatus, 0, len(dm.order))
	for _, id := range dm.order {
		ss, ok := dm.devices[id]
		if !ok {
			continue
		}
		status, err := ss.GetStatus()
		deviceConfig := dm.deviceConfig[id]

		ds := &DeviceStatus{
			ID:        id,
			Name:      dm.displayName(id, deviceConfig.Name),
			Port:      deviceConfig.Port,
			Connected: status.Connected,
			Flymode:   status.Flymode,
		}

		if err == nil {
			ds.ICCID = status.Mobile.Iccid
			ds.IMSI = status.Mobile.Imsi
			ds.Operator = status.Mobile.Operator
			// SIM 卡不写 MSISDN 时（境外/漫游卡常见）回退到手工补录的号码
			ds.PhoneNumber = dm.displayPhoneNumber(id, status.Mobile.Number)
			ds.Signal = status.Mobile.SignalLevel
			ds.Rsrp = normalizeRsrp(status.Mobile.Rsrp)
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
		Name:      dm.displayName(id, dc.Name),
		Port:      dc.Port,
		Connected: status.Connected,
		Flymode:   status.Flymode,
	}

	if err == nil {
		ds.ICCID = status.Mobile.Iccid
		ds.IMSI = status.Mobile.Imsi
		ds.Operator = status.Mobile.Operator
		// SIM 卡不写 MSISDN 时（境外/漫游卡常见）回退到手工补录的号码
		ds.PhoneNumber = dm.displayPhoneNumber(id, status.Mobile.Number)
		ds.Signal = status.Mobile.SignalLevel
		ds.Rsrp = normalizeRsrp(status.Mobile.Rsrp)
		ds.IsRoaming = status.Mobile.IsRoaming
	}

	return ds, nil
}

// normalizeRsrp 过滤掉「模组没上报」的占位值。
//
// Lua 侧 mobile.rsrp() 取不到时会兜底成 -140，未初始化时是 0，两者都不是真实测量值，
// 直接透传会让前端把 -140dBm 当成实测电平画出来。有效区间取 3GPP 的 (-140, -44)。
func normalizeRsrp(rsrp int) int {
	if rsrp >= -44 || rsrp <= -140 {
		return 0
	}
	return rsrp
}

// SendSMS 通过指定设备发送短信
// device_id 为必填：多卡场景下「默认设备」会导致短信从非预期的号码
// （例如境外漫游卡）发出并产生国际资费，因此不做任何兜底。
func (dm *DeviceManager) SendSMS(deviceID, to, content string) (string, error) {
	if deviceID == "" {
		return "", dm.missingDeviceErr()
	}

	ss, ok := dm.GetDevice(deviceID)
	if !ok {
		return "", fmt.Errorf("设备不存在: %s（可选: %v）", deviceID, dm.GetAllDeviceIDs())
	}

	// 按设备限流，防止脚本刷爆话费
	if allowed, _, err := dm.limiters.get(deviceID).Allow(); !allowed {
		return "", err
	}

	return ss.SendSMS(to, content)
}

// SetFlymode 设置指定设备的飞行模式（device_id 必填）
func (dm *DeviceManager) SetFlymode(deviceID string, enabled bool) error {
	if deviceID == "" {
		return dm.missingDeviceErr()
	}

	ss, ok := dm.GetDevice(deviceID)
	if !ok {
		return fmt.Errorf("设备不存在: %s（可选: %v）", deviceID, dm.GetAllDeviceIDs())
	}

	return ss.SetFlymode(enabled)
}

// RebootMcu 重启指定设备模块（device_id 必填）
func (dm *DeviceManager) RebootMcu(deviceID string) error {
	if deviceID == "" {
		return dm.missingDeviceErr()
	}

	ss, ok := dm.GetDevice(deviceID)
	if !ok {
		return fmt.Errorf("设备不存在: %s（可选: %v）", deviceID, dm.GetAllDeviceIDs())
	}

	return ss.RebootMcu()
}

// missingDeviceErr 统一构造「必须指定设备」的错误，并附上可选项。
func (dm *DeviceManager) missingDeviceErr() error {
	return fmt.Errorf("必须指定 device_id（可选: %v）", dm.GetAllDeviceIDs())
}

// RequestCacheUpdate 请求更新所有设备缓存（按配置声明顺序）
func (dm *DeviceManager) RequestCacheUpdate() {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	for _, id := range dm.order {
		if ss, ok := dm.devices[id]; ok {
			go ss.RequestCacheUpdate()
		}
	}
}

// GetFirstEnabledDeviceID 获取第一个启用的设备ID
// 注意：按配置声明顺序返回，不能遍历 map（顺序随机）。
func (dm *DeviceManager) GetFirstEnabledDeviceID() (string, bool) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	for _, id := range dm.order {
		if _, ok := dm.devices[id]; ok {
			return id, true
		}
	}
	return "", false
}

// SaveTextMessage 保存短信记录（带设备ID）
func (dm *DeviceManager) SaveTextMessage(ctx context.Context, deviceID string, msg *models.TextMessage) error {
	// 记录消息来源设备，便于多卡场景追溯
	if deviceID != "" {
		msg.DeviceID = deviceID
	}
	return dm.textMsgService.Save(ctx, msg)
}
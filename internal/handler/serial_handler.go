package handler

import (
	"net/http"

	"github.com/dushixiang/uart_sms_forwarder/internal/service"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// SerialHandler 串口控制API处理器（多设备支持）
type SerialHandler struct {
	logger        *zap.Logger
	deviceManager *service.DeviceManager
}

// NewSerialHandler 创建串口Handler实例
func NewSerialHandler(logger *zap.Logger, deviceManager *service.DeviceManager) *SerialHandler {
	return &SerialHandler{
		logger:        logger,
		deviceManager: deviceManager,
	}
}

// SendSMSRequest 发送短信请求（device_id 必填）
type SendSMSRequest struct {
	To       string `json:"to"`
	Content  string `json:"content"`
	DeviceID string `json:"device_id"` // 必填，指定从哪张卡发出
}

// SendSMS 发送短信
// POST /api/serial/sms
func (h *SerialHandler) SendSMS(c echo.Context) error {
	var req SendSMSRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}

	if req.To == "" || req.Content == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "手机号和内容不能为空"})
	}

	// device_id 必填：避免多卡场景下短信从非预期号码发出
	if req.DeviceID == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error":   "必须指定 device_id",
			"devices": h.deviceManager.GetAllDeviceIDs(),
		})
	}

	msgID, err := h.deviceManager.SendSMS(req.DeviceID, req.To, req.Content)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message":   "发送成功",
		"msg_id":    msgID,
		"device_id": req.DeviceID,
	})
}

// GetStatus 获取所有设备状态（保持原始格式兼容）
// GET /api/serial/status
func (h *SerialHandler) GetStatus(c echo.Context) error {
	statuses := h.deviceManager.GetAllStatus()
	firstDeviceID, _ := h.deviceManager.GetFirstEnabledDeviceID()

	result := map[string]any{
		"connected": len(statuses) > 0 && statuses[0].Connected,
		"devices":   statuses,
		"total":     len(statuses),
	}

	// 添加第一个设备的详细信息
	if firstDeviceID != "" {
		status, _ := h.deviceManager.GetDeviceStatus(firstDeviceID)
		if status != nil {
			result["flymode"] = status.Flymode
			result["port"] = status.Port
			result["mobile"] = map[string]any{
				"iccid":         status.ICCID,
				"imsi":          status.IMSI,
				"is_registered": status.Connected,
				"is_roaming":    status.IsRoaming,
				"operator":      status.Operator,
				"phone_number":  status.PhoneNumber,
				"rssi":          -status.Signal,
				"signal":        status.Signal,
				"signal_desc":   getSignalDesc(status.Signal),
				"signal_level":  status.Signal,
				"sim_ready":     status.Connected,
			}
		}
	}

	return c.JSON(http.StatusOK, result)
}

func getSignalDesc(signal int) string {
	if signal >= 20 {
		return "强"
	} else if signal >= 15 {
		return "中"
	} else if signal >= 10 {
		return "弱"
	}
	return "无信号"
}

// ListDevices 获取设备列表
// GET /api/devices
func (h *SerialHandler) ListDevices(c echo.Context) error {
	configs := h.deviceManager.GetAllDeviceConfigs()
	devices := make([]map[string]any, 0, len(configs))
	for _, dc := range configs {
		devices = append(devices, map[string]any{
			"id":          dc.ID,
			"name":        h.deviceManager.GetDeviceDisplayName(dc.ID),
			"port":        dc.Port,
			"description": dc.Description,
			"enabled":     dc.Enabled,
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"devices": devices, "total": len(devices)})
}

// RenameDeviceRequest 修改设备（SIM 卡）显示名称请求
type RenameDeviceRequest struct {
	Name string `json:"name"`
}

// RenameDevice 修改设备显示名称（SIM 卡别名，如「联通3113」）
//
// PUT /api/devices/:id/name
//
// 名称落库到 properties 表，重启不丢；config.yaml 在容器里只读，改不了也写不回。
func (h *SerialHandler) RenameDevice(c echo.Context) error {
	deviceID := c.Param("id")
	if deviceID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "缺少设备 ID"})
	}

	var req RenameDeviceRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}

	name, err := h.deviceManager.RenameDevice(deviceID, req.Name)
	if err != nil {
		h.logger.Warn("修改设备名称失败",
			zap.String("device_id", deviceID),
			zap.Error(err))
		// 校验类错误（空名/超长/设备不存在）都属于客户端问题
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"message":   "名称已修改",
		"id":        deviceID,
		"name":      name,
		"device_id": deviceID,
	})
}

// SetPhoneNumberRequest 修改设备本机号码请求
type SetPhoneNumberRequest struct {
	// Number 为空串表示「清除手工补录，回退到 SIM 卡读取值」
	Number string `json:"number"`
}

// SetDevicePhoneNumber 手工设置设备本机号码
//
// PUT /api/devices/:id/phone-number
//
// 背景：Lua 侧 mobile.number(0) 底层是 AT+CNUM，读 SIM 卡 EF_MSISDN 字段。
// 国内运营商会写，多数境外/漫游卡不写，于是返回空串——不是 bug，改 Lua 也绕不过。
// 这里提供手工补录，落库到 properties 表，纯展示用途，不影响实际收发。
func (h *SerialHandler) SetDevicePhoneNumber(c echo.Context) error {
	deviceID := c.Param("id")
	if deviceID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "缺少设备 ID"})
	}

	var req SetPhoneNumberRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}

	number, err := h.deviceManager.SetDevicePhoneNumber(deviceID, req.Number)
	if err != nil {
		h.logger.Warn("设置本机号码失败",
			zap.String("device_id", deviceID),
			zap.Error(err))
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"message":      "本机号码已更新",
		"id":           deviceID,
		"device_id":    deviceID,
		"phone_number": number,
	})
}

// GetDeviceStatus 获取单个设备状态
// GET /api/devices/:id/status
func (h *SerialHandler) GetDeviceStatus(c echo.Context) error {
	deviceID := c.Param("id")
	status, err := h.deviceManager.GetDeviceStatus(deviceID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, status)
}

// SetFlymodeRequest 设置飞行模式请求
type SetFlymodeRequest struct {
	Enabled  bool   `json:"enabled"`
	DeviceID string `json:"device_id"`
}

// SetFlymode 设置飞行模式
// POST /api/devices/:id/flymode（推荐） 或 POST /api/serial/flymode + body.device_id
func (h *SerialHandler) SetFlymode(c echo.Context) error {
	deviceID := c.Param("id")
	var req SetFlymodeRequest
	_ = c.Bind(&req)
	if deviceID == "" && req.DeviceID != "" {
		deviceID = req.DeviceID
	}

	if deviceID == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error":   "必须指定 device_id",
			"devices": h.deviceManager.GetAllDeviceIDs(),
		})
	}

	if err := h.deviceManager.SetFlymode(deviceID, req.Enabled); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	go h.deviceManager.RequestCacheUpdate()
	return c.JSON(http.StatusOK, map[string]any{"device_id": deviceID, "flymode": req.Enabled})
}

// RebootMcu 重启模块
// POST /api/devices/:id/reboot（推荐） 或 POST /api/serial/reboot + body.device_id
func (h *SerialHandler) RebootMcu(c echo.Context) error {
	deviceID := c.Param("id")
	var req struct {
		DeviceID string `json:"device_id"`
	}
	_ = c.Bind(&req)
	if deviceID == "" && req.DeviceID != "" {
		deviceID = req.DeviceID
	}

	if deviceID == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error":   "必须指定 device_id",
			"devices": h.deviceManager.GetAllDeviceIDs(),
		})
	}

	if err := h.deviceManager.RebootMcu(deviceID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	go h.deviceManager.RequestCacheUpdate()
	return c.JSON(http.StatusOK, map[string]any{"device_id": deviceID, "message": "重启命令已发送"})
}

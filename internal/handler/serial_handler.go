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

// SendSMSRequest 发送短信请求（支持设备选择）
type SendSMSRequest struct {
	To       string `json:"to"`
	Content  string `json:"content"`
	DeviceID string `json:"device_id"` // 可选，指定设备ID
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

	msgID, err := h.deviceManager.SendSMS(req.DeviceID, req.To, req.Content)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	deviceID := req.DeviceID
	if deviceID == "" {
		deviceID, _ = h.deviceManager.GetFirstEnabledDeviceID()
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "发送成功", "msg_id": msgID, "device_id": deviceID})
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
			"name":        dc.Name,
			"port":        dc.Port,
			"description": dc.Description,
			"enabled":     dc.Enabled,
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"devices": devices, "total": len(devices)})
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
// POST /api/serial/flymode 或 POST /api/devices/:id/flymode
func (h *SerialHandler) SetFlymode(c echo.Context) error {
	deviceID := c.Param("id")
	var req SetFlymodeRequest
	if c.Bind(&req) == nil && req.DeviceID != "" {
		deviceID = req.DeviceID
	}

	if err := h.deviceManager.SetFlymode(deviceID, req.Enabled); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	go h.deviceManager.RequestCacheUpdate()
	return c.JSON(http.StatusOK, map[string]any{"device_id": deviceID, "flymode": req.Enabled})
}

// RebootMcu 重启模块
// POST /api/serial/reboot 或 POST /api/devices/:id/reboot
func (h *SerialHandler) RebootMcu(c echo.Context) error {
	deviceID := c.Param("id")
	var req struct{ DeviceID string `json:"device_id"` }
	if c.Bind(&req) == nil && req.DeviceID != "" {
		deviceID = req.DeviceID
	}

	if err := h.deviceManager.RebootMcu(deviceID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	go h.deviceManager.RequestCacheUpdate()
	return c.JSON(http.StatusOK, map[string]any{"device_id": deviceID, "message": "重启命令已发送"})
}

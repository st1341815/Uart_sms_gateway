package handler

import (
	"net/http"

	"github.com/dushixiang/uart_sms_forwarder/internal/service"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// SelfCheckHandler 设备自检接口
//
// 只读：不打开任何串口，因此不会打扰正在收发短信的设备，
// 前端的「重新自检」按钮可以随便点。
type SelfCheckHandler struct {
	logger        *zap.Logger
	deviceManager *service.DeviceManager
	selfCheck     *service.SelfCheckService
}

// NewSelfCheckHandler 创建设备自检 Handler
func NewSelfCheckHandler(
	logger *zap.Logger,
	deviceManager *service.DeviceManager,
	selfCheck *service.SelfCheckService,
) *SelfCheckHandler {
	return &SelfCheckHandler{
		logger:        logger,
		deviceManager: deviceManager,
		selfCheck:     selfCheck,
	}
}

// Get 执行一次设备自检
// GET /api/serial/selfcheck
func (h *SelfCheckHandler) Get(c echo.Context) error {
	configs := h.deviceManager.GetAllDeviceConfigs()
	// 把 config.yaml 里的 Name 换成最终显示名（DB 改名覆盖层优先），
	// 否则体检结论里的名字和界面上的名字会对不上。
	for i := range configs {
		configs[i].Name = h.deviceManager.GetDeviceDisplayName(configs[i].ID)
	}

	report := h.selfCheck.Run(configs)
	return c.JSON(http.StatusOK, report)
}

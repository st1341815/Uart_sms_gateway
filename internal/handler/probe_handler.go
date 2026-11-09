package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/dushixiang/uart_sms_forwarder/internal/service"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// ProbeHandler 模组探测接口（L1）
//
// 与自检（L0）的分工：
//
//	L0 只读目录，能说清「宿主机上有这张卡，但容器 /dev 或 config.yaml 里没有」；
//	L1 会**真的打开串口**发一次 get_status，能说清「哪个接口是 AT 口、这是哪张卡」。
//
// 因为 L1 有副作用（短暂占用串口），它有三条限制：只探测未被配置引用的端口、
// 同端口 30 秒冷却、全局串行。L0 没有这些限制，前端可以随便刷新。
type ProbeHandler struct {
	logger *zap.Logger
	probe  *service.ProbeService
}

// NewProbeHandler 创建模组探测 Handler
func NewProbeHandler(logger *zap.Logger, probe *service.ProbeService) *ProbeHandler {
	return &ProbeHandler{logger: logger, probe: probe}
}

// probeRequest 探测请求体
type probeRequest struct {
	// UsbPort 来自自检报告的 modules[].usb_port，形如 "usb-0:1"
	UsbPort string `json:"usb_port"`
}

// Probe 探测某个 USB 端口上的模组
// POST /api/serial/probe
//
// 返回 200 只表示「探测流程跑完了」，结论看响应体里的 success：
// 探测失败（口打不开 / 卡不回话）是**正常的业务结果**，附 tried 明细供排查，
// 不该用 4xx/5xx 表达——否则前端会把「这张卡没上电」当成接口坏了。
func (h *ProbeHandler) Probe(c echo.Context) error {
	var req probeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}

	usbPort := strings.TrimSpace(req.UsbPort)
	if usbPort == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "缺少 usb_port（形如 usb-0:1，来自自检报告的 modules[].usb_port）",
		})
	}

	result, err := h.probe.Probe(c.Request().Context(), usbPort)
	if err != nil {
		status, logLevel := probeErrorStatus(err)
		if logLevel == "warn" {
			h.logger.Warn("模组探测被拒绝",
				zap.String("usb_port", usbPort),
				zap.Error(err))
		} else if logLevel == "error" {
			h.logger.Error("模组探测异常",
				zap.String("usb_port", usbPort),
				zap.Error(err))
		}
		return c.JSON(status, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, result)
}

// probeErrorStatus 把服务层哨兵错误映射成 HTTP 状态码。
//
// 映射原则：客户端**换一下输入就能成功**的错误用 4xx，
// 服务端自身状态的错误用 5xx。冷却与并发冲突都算「等一下就好」，用 429/409 区分开。
func probeErrorStatus(err error) (status int, logLevel string) {
	switch {
	case errors.Is(err, service.ErrProbeNoTarget):
		return http.StatusNotFound, "info"
	case errors.Is(err, service.ErrProbeConfigured):
		// 409：资源状态冲突（这个口正在被一台已配置的设备使用），不是参数错
		return http.StatusConflict, "info"
	case errors.Is(err, service.ErrProbeBusy):
		return http.StatusConflict, "info"
	case errors.Is(err, service.ErrProbeCooldown):
		return http.StatusTooManyRequests, "info"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// 浏览器切页面 / 关了 tab：不是故障，别在日志里刷 warn
		return http.StatusRequestTimeout, "info"
	default:
		return http.StatusInternalServerError, "error"
	}
}

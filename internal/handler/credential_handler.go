package handler

import (
	"net/http"

	"github.com/dushixiang/uart_sms_forwarder/internal/middleware"
	"github.com/dushixiang/uart_sms_forwarder/internal/service"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// CredentialHandler 口令管理（修改管理员密码）
type CredentialHandler struct {
	logger         *zap.Logger
	accountService *service.AccountService
	// passwordReset 短信重置密码（可为 nil，表示该能力未启用）
	passwordReset *service.PasswordResetService
	// maskPhone 与 service 层保持同一套打码规则
	maskPhone func(string) string
}

func NewCredentialHandler(
	logger *zap.Logger,
	accountService *service.AccountService,
	passwordReset *service.PasswordResetService,
) *CredentialHandler {
	return &CredentialHandler{
		logger:         logger,
		accountService: accountService,
		passwordReset:  passwordReset,
		maskPhone:      service.MaskPhone,
	}
}

// Me 返回当前登录用户信息
// GET /api/auth/me
func (h *CredentialHandler) Me(c echo.Context) error {
	username := middleware.GetUsername(c)
	if username == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "未登录"})
	}
	return c.JSON(http.StatusOK, map[string]any{"username": username})
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangePassword 修改当前登录用户的密码
// POST /api/auth/password
func (h *CredentialHandler) ChangePassword(c echo.Context) error {
	username := middleware.GetUsername(c)
	if username == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "未登录"})
	}

	var req ChangePasswordRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}

	if req.OldPassword == "" || req.NewPassword == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "当前密码和新密码都不能为空"})
	}

	if err := h.accountService.ChangePassword(c.Request().Context(), username, req.OldPassword, req.NewPassword); err != nil {
		// 业务校验失败返回 400，附带具体原因（如「当前密码错误」）
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	h.logger.Info("用户修改密码成功", zap.String("username", username))

	// 提示前端需要重新登录：旧 token 仍有效（无吊销机制），
	// 但出于安全习惯建议用户重新登录。
	return c.JSON(http.StatusOK, map[string]any{
		"message":           "密码修改成功",
		"require_relogin":   true,
	})
}

// ===== 短信重置密码 =====

// GetResetPhone 读取已保存的安全手机号（脱敏返回）
// GET /api/auth/password-reset/phone
func (h *CredentialHandler) GetResetPhone(c echo.Context) error {
	if h.passwordReset == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "短信重置密码未启用"})
	}

	phone, err := h.passwordReset.GetPhone(c.Request().Context())
	if err != nil {
		h.logger.Error("读取安全手机号失败", zap.Error(err))
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	resp := map[string]any{
		"phone_set": phone != "",
	}
	if phone != "" {
		resp["masked_phone"] = h.maskPhone(phone)
	}
	return c.JSON(http.StatusOK, resp)
}

// SetResetPhoneRequest 保存安全手机号请求
type SetResetPhoneRequest struct {
	Phone string `json:"phone"`
}

// SetResetPhone 保存安全手机号
// PUT /api/auth/password-reset/phone
func (h *CredentialHandler) SetResetPhone(c echo.Context) error {
	if h.passwordReset == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "短信重置密码未启用"})
	}
	if middleware.GetUsername(c) == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "未登录"})
	}

	var req SetResetPhoneRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}

	phone, err := h.passwordReset.SetPhone(c.Request().Context(), req.Phone)
	if err != nil {
		// 格式错误属于用户输入问题，返回 400 带上具体原因
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"message":      "安全手机号已保存",
		"masked_phone": h.maskPhone(phone),
	})
}

// ResetBySMSRequest 短信重置密码请求
type ResetBySMSRequest struct {
	DeviceID string `json:"device_id"`
	// Phone 可选：留空则用已保存的安全手机号
	Phone string `json:"phone"`
}

// ResetPasswordBySMS 生成新密码并通过短信下发
//
// POST /api/auth/password-reset/sms
//
// 语义是「重置」而非「修改」：不校验旧密码，凭「已登录会话 + 手机能收到短信」放行。
// 服务端保证「短信发送成功才覆盖原密码」，因此发送失败时用户不会被锁死。
func (h *CredentialHandler) ResetPasswordBySMS(c echo.Context) error {
	if h.passwordReset == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "短信重置密码未启用"})
	}

	username := middleware.GetUsername(c)
	if username == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "未登录"})
	}

	var req ResetBySMSRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}

	result, err := h.passwordReset.ResetBySMS(c.Request().Context(), username, req.DeviceID, req.Phone)
	if err != nil {
		// 设备离线 / 频率限制 / 号码非法等都属于「本次没成」，返回 400 让前端直接提示
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"message":        "新密码已通过短信发送",
		"masked_phone":   result.MaskedPhone,
		"device_id":      result.DeviceID,
		"device_name":    result.DeviceName,
		"sent_at":        result.SentAt,
		"require_relogin": true,
	})
}

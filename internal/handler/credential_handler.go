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
}

func NewCredentialHandler(logger *zap.Logger, accountService *service.AccountService) *CredentialHandler {
	return &CredentialHandler{
		logger:         logger,
		accountService: accountService,
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

package handler

import (
	"net/http"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/dushixiang/uart_sms_forwarder/internal/service"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// OpenAPIAdminHandler 开放接口的管理端（Key 与模板维护）
type OpenAPIAdminHandler struct {
	logger    *zap.Logger
	apiKeys   *service.APIKeyService
	templates *service.TemplateService
	openAPI   *service.OpenAPIService
}

func NewOpenAPIAdminHandler(logger *zap.Logger, apiKeys *service.APIKeyService, templates *service.TemplateService, openAPI *service.OpenAPIService) *OpenAPIAdminHandler {
	return &OpenAPIAdminHandler{
		logger:    logger,
		apiKeys:   apiKeys,
		templates: templates,
		openAPI:   openAPI,
	}
}

// ---------- API Key ----------

// ListKeys GET /api/openapi/keys
func (h *OpenAPIAdminHandler) ListKeys(c echo.Context) error {
	list, err := h.apiKeys.ListAPIKeys(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "查询失败"})
	}
	if list == nil {
		list = []models.APIKey{}
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"keys": list, "total": len(list)})
}

type createKeyRequest struct {
	Name               string `json:"name"`
	DeviceID           string `json:"deviceId"`
	AllowedIPs         string `json:"allowedIps"`
	RateLimitPerMinute int    `json:"rateLimitPerMinute"`
}

// CreateKey POST /api/openapi/keys
// 返回的 secret **只在本次响应中出现一次**
func (h *OpenAPIAdminHandler) CreateKey(c echo.Context) error {
	var req createKeyRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}
	key, secret, err := h.apiKeys.CreateAPIKey(c.Request().Context(), req.Name, req.DeviceID, req.AllowedIPs, req.RateLimitPerMinute)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, map[string]interface{}{
		"key":    key,
		"secret": secret, // 仅此一次
		"notice": "Secret 仅显示这一次，请立即保存。丢失后只能重置。",
	})
}

type updateKeyRequest struct {
	Name               string `json:"name"`
	DeviceID           string `json:"deviceId"`
	AllowedIPs         string `json:"allowedIps"`
	RateLimitPerMinute int    `json:"rateLimitPerMinute"`
	Enabled            bool   `json:"enabled"`
}

// UpdateKey PUT /api/openapi/keys/:id
func (h *OpenAPIAdminHandler) UpdateKey(c echo.Context) error {
	id := c.Param("id")
	var req updateKeyRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}
	if err := h.apiKeys.UpdateAPIKey(c.Request().Context(), id, req.Name, req.DeviceID, req.AllowedIPs, req.RateLimitPerMinute, req.Enabled); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "已更新"})
}

// DeleteKey DELETE /api/openapi/keys/:id
func (h *OpenAPIAdminHandler) DeleteKey(c echo.Context) error {
	if err := h.apiKeys.DeleteAPIKey(c.Request().Context(), c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "已删除"})
}

// ResetSecret POST /api/openapi/keys/:id/reset-secret
func (h *OpenAPIAdminHandler) ResetSecret(c echo.Context) error {
	secret, err := h.apiKeys.ResetSecret(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"secret": secret,
		"notice": "Secret 已重置，旧 Secret 立即失效。请立即保存新值。",
	})
}

// ---------- 模板 ----------

// ListTemplates GET /api/openapi/templates
func (h *OpenAPIAdminHandler) ListTemplates(c echo.Context) error {
	list, err := h.templates.List(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "查询失败"})
	}
	type tplView struct {
		ID          string   `json:"id"`
		Code        string   `json:"code"`
		Name        string   `json:"name"`
		Content     string   `json:"content"`
		Description string   `json:"description"`
		Enabled     bool     `json:"enabled"`
		Params      []string `json:"params"`
		CreatedAt   int64    `json:"createdAt"`
		UpdatedAt   int64    `json:"updatedAt"`
	}
	out := make([]tplView, 0, len(list))
	for _, t := range list {
		out = append(out, tplView{
			ID: t.ID, Code: t.Code, Name: t.Name, Content: t.Content,
			Description: t.Description, Enabled: t.Enabled,
			Params:    service.ExtractVars(t.Content),
			CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		})
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"templates": out, "total": len(out)})
}

type tplRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Content     string `json:"content"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// CreateTemplate POST /api/openapi/templates
func (h *OpenAPIAdminHandler) CreateTemplate(c echo.Context) error {
	var req tplRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}
	t, err := h.templates.Create(c.Request().Context(), req.Code, req.Name, req.Content, req.Description, req.Enabled)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, map[string]interface{}{
		"template": t,
		"params":   service.ExtractVars(t.Content),
	})
}

// UpdateTemplate PUT /api/openapi/templates/:id
func (h *OpenAPIAdminHandler) UpdateTemplate(c echo.Context) error {
	var req tplRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "请求参数错误"})
	}
	if err := h.templates.Update(c.Request().Context(), c.Param("id"), req.Code, req.Name, req.Content, req.Description, req.Enabled); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "已更新"})
}

// DeleteTemplate DELETE /api/openapi/templates/:id
func (h *OpenAPIAdminHandler) DeleteTemplate(c echo.Context) error {
	if err := h.templates.Delete(c.Request().Context(), c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "已删除"})
}

// ListSendLogs GET /api/openapi/logs?limit=50
func (h *OpenAPIAdminHandler) ListSendLogs(c echo.Context) error {
	limit := 50
	if v := c.QueryParam("limit"); v != "" {
		if n, err := parseInt(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	logs, err := h.openAPI.RecentLogs(c.Request().Context(), limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "查询失败"})
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"logs": logs, "total": len(logs)})
}

func parseInt(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, nil
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, echo.ErrBadRequest
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

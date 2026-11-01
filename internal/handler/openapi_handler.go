package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/dushixiang/uart_sms_forwarder/internal/service"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// OpenAPIHandler 对外开放短信接口（供其他应用调用，AK/SK 签名认证）
type OpenAPIHandler struct {
	logger     *zap.Logger
	apiKeys    *service.APIKeyService
	openAPI    *service.OpenAPIService
	templates  *service.TemplateService
}

func NewOpenAPIHandler(logger *zap.Logger, apiKeys *service.APIKeyService, openAPI *service.OpenAPIService, templates *service.TemplateService) *OpenAPIHandler {
	return &OpenAPIHandler{
		logger:    logger,
		apiKeys:   apiKeys,
		openAPI:   openAPI,
		templates: templates,
	}
}

// apiResponse 统一响应结构
type apiResponse struct {
	Success bool        `json:"success"`
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	// RequestId 便于调用方与我们联查
	RequestId string `json:"requestId,omitempty"`
}

// Send 发送短信
// POST /openapi/v1/sms/send
func (h *OpenAPIHandler) Send(c echo.Context) error {
	// 读 body（验签需要对原始字节做 sha256，且后续还要解析）
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 64*1024))
	if err != nil {
		return c.JSON(http.StatusBadRequest, apiResponse{
			Success: false, Code: "BAD_BODY", Message: "读取请求体失败",
		})
	}
	// 复原 body 供后续可能的中间件读取
	c.Request().Body = io.NopCloser(bytes.NewReader(body))

	// 认证：AK/SK 签名 + IP 白名单
	key, err := h.authenticate(c, body)
	if err != nil {
		e := service.AsAPIError(err)
		h.logger.Warn("开放接口认证失败",
			zap.String("ip", clientIP(c)),
			zap.String("access_key", c.Request().Header.Get("X-Api-Key")),
			zap.Error(err))
		return c.JSON(e.HTTPStatus, apiResponse{
			Success: false, Code: e.Code, Message: e.Message,
		})
	}

	// 解析请求
	var req service.SendRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return c.JSON(http.StatusBadRequest, apiResponse{
			Success: false, Code: "INVALID_JSON", Message: "请求体不是合法 JSON",
		})
	}

	result, err := h.openAPI.Send(c.Request().Context(), key, req, clientIP(c))
	if err != nil {
		e := service.AsAPIError(err)
		return c.JSON(e.HTTPStatus, apiResponse{
			Success: false, Code: e.Code, Message: e.Message,
		})
	}

	return c.JSON(http.StatusOK, apiResponse{
		Success: true,
		Code:    "OK",
		Message: "短信已提交发送",
		Data:    result,
	})
}

// ListTemplates 供调用方查询可用模板
// GET /openapi/v1/templates
func (h *OpenAPIHandler) ListTemplates(c echo.Context) error {
	// 验签（GET 无 body）
	if _, err := h.authenticate(c, nil); err != nil {
		e := service.AsAPIError(err)
		return c.JSON(e.HTTPStatus, apiResponse{Success: false, Code: e.Code, Message: e.Message})
	}

	list, err := h.templates.List(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, apiResponse{
			Success: false, Code: "INTERNAL_ERROR", Message: "查询模板失败",
		})
	}

	// 只暴露启用中的模板，且不带内部 ID
	out := make([]map[string]interface{}, 0, len(list))
	for _, t := range list {
		if !t.Enabled {
			continue
		}
		params := service.ExtractVars(t.Content)
		out = append(out, map[string]interface{}{
			"code":        t.Code,
			"name":        t.Name,
			"content":     t.Content,
			"description": t.Description,
			"params":      params,
		})
	}
	return c.JSON(http.StatusOK, apiResponse{
		Success: true, Code: "OK", Message: "ok", Data: out,
	})
}

// authenticate 校验签名，返回对应的 APIKey
func (h *OpenAPIHandler) authenticate(c echo.Context, body []byte) (*models.APIKey, error) {
	req := c.Request()

	// 原始请求路径（签名用，不含 query）
	path := req.URL.Path
	sortedQuery := service.SortedQuery(req.URL.Query())
	bodyHash := service.SHA256Hex(body)

	key, err := h.apiKeys.VerifyRequest(
		c.Request().Context(),
		req.Header.Get("X-Api-Key"),
		req.Header.Get("X-Signature"),
		req.Header.Get("X-Timestamp"),
		req.Header.Get("X-Nonce"),
		req.Method,
		path,
		sortedQuery,
		bodyHash,
		clientIP(c),
	)
	if err != nil {
		return nil, service.AuthError(err.Error())
	}
	return key, nil
}

// clientIP 取真实客户端 IP（优先 X-Real-IP，其次 X-Forwarded-For，最后 RemoteAddr）
func clientIP(c echo.Context) string {
	if v := c.Request().Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if v := c.Request().Header.Get("X-Forwarded-For"); v != "" {
		// 取第一个
		if i := len(v); i > 0 {
			for j := 0; j < len(v); j++ {
				if v[j] == ',' {
					return trimSpace(v[:j])
				}
			}
		}
		return trimSpace(v)
	}
	host, _, err := net.SplitHostPort(c.Request().RemoteAddr)
	if err != nil {
		return c.Request().RemoteAddr
	}
	return host
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

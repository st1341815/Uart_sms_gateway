package middleware

import (
	"net/http"
	"strings"

	"github.com/dushixiang/uart_sms_forwarder/internal/util"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

const (
	// ContextKeyUsername Context 中用户名的 key
	ContextKeyUsername = "username"
)

// JWTMiddleware JWT 认证中间件
func JWTMiddleware(secret string, logger *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// 获取 Authorization header
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				logger.Warn("缺少 Authorization header")
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "缺少认证信息",
				})
			}

			// 提取 Bearer token
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				// ⚠️ 绝不打印 header 原文：日志会落盘（logs/sms.log），
				// 一旦日志被贴到 issue/截图里，等于把有效 token 送人。
				logger.Warn("Authorization header 格式错误",
					zap.Int("length", len(authHeader)),
					zap.String("scheme", parts[0]))
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "认证信息格式错误",
				})
			}

			tokenString := parts[1]

			// 验证 token
			claims, err := util.VerifyToken(tokenString, secret)
			if err != nil {
				// 同理：只记错误类别，不记 token 内容，也不把细节回给客户端
				logger.Warn("token 验证失败", zap.String("reason", err.Error()))
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "认证失败",
				})
			}

			// 将用户名存入 context
			c.Set(ContextKeyUsername, claims.Username)

			// 继续处理请求
			return next(c)
		}
	}
}

// GetUsername 从 context 中获取用户名
func GetUsername(c echo.Context) string {
	if username, ok := c.Get(ContextKeyUsername).(string); ok {
		return username
	}
	return ""
}

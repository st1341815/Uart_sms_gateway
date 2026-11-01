package service

import (
	"fmt"
	"net/http"
)

// APIError 带 HTTP 状态码与业务错误码的错误。
// 对外开放接口用它区分「参数错误 400 / 限流 429 / 服务端 500」，
// 便于调用方按 code 做重试策略。
type APIError struct {
	HTTPStatus int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	return e.Message
}

// NewAPIError 构造 APIError
func NewAPIError(httpStatus int, code, message string) *APIError {
	return &APIError{HTTPStatus: httpStatus, Code: code, Message: message}
}

// AsAPIError 断言，非 APIError 统一按 500 处理
func AsAPIError(err error) *APIError {
	if err == nil {
		return nil
	}
	if e, ok := err.(*APIError); ok {
		return e
	}
	return NewAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
}

// ValidationError 参数校验错误（供中间件层使用）
func ValidationError(msg string) *APIError {
	return NewAPIError(http.StatusBadRequest, "INVALID_PARAMETER", msg)
}

// AuthError 认证错误
func AuthError(msg string) *APIError {
	return NewAPIError(http.StatusUnauthorized, "UNAUTHORIZED", msg)
}

// Ensure fmt is used (keeps import stable if messages change)
var _ = fmt.Sprintf

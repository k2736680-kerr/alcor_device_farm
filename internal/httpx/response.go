package httpx

import (
	"encoding/json"
	"net/http"
	"unicode"

	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
)

type Envelope struct {
	RequestID string    `json:"request_id"`
	Data      any       `json:"data"`
	Error     *APIError `json:"error"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
	Retryable bool   `json:"retryable"`
}

func WriteData(writer http.ResponseWriter, request *http.Request, status int, data any) {
	writeJSON(writer, status, Envelope{
		RequestID: correlation.FromContext(request.Context()).RequestID,
		Data:      data,
		Error:     nil,
	})
}

func WriteError(writer http.ResponseWriter, request *http.Request, status int, apiError APIError) {
	apiError.Message = chineseAPIMessage(apiError.Code, apiError.Message)
	writeJSON(writer, status, Envelope{
		RequestID: correlation.FromContext(request.Context()).RequestID,
		Data:      nil,
		Error:     &apiError,
	})
}

// chineseAPIMessage is the final user-facing boundary. Lower layers may wrap
// third-party or standard-library errors in English, but public API prompts
// must remain Chinese while the stable error code carries machine semantics.
func chineseAPIMessage(code, message string) string {
	for _, value := range message {
		if unicode.Is(unicode.Han, value) {
			return message
		}
	}
	switch code {
	case "UNAUTHORIZED", "UNAUTHENTICATED":
		return "请先登录或提供有效凭证"
	case "FORBIDDEN":
		return "没有权限执行此操作"
	case "CSRF_VALIDATION_FAILED":
		return "请求安全校验失败，请刷新页面后重试"
	case "INVALID_ARGUMENT", "INVALID_CREDENTIALS":
		return "请求参数或凭证无效"
	case "NOT_FOUND", "IOS_SESSION_NOT_FOUND", "REMOTE_CONTROL_NOT_FOUND":
		return "未找到指定资源"
	case "METHOD_NOT_ALLOWED":
		return "不支持当前请求方法"
	case "SERVICE_UNAVAILABLE", "REMOTE_CONTROL_UNAVAILABLE", "IOS_SESSION_FENCE_UNAVAILABLE", "BUILD_AGENT_UNAVAILABLE":
		return "服务暂时不可用，请稍后重试"
	case "LOGIN_RATE_LIMITED":
		return "登录失败次数过多，请稍后重试"
	case "INTERNAL", "INTERNAL_ERROR":
		return "服务器内部错误"
	default:
		return "请求处理失败，请稍后重试"
	}
}

func writeJSON(writer http.ResponseWriter, status int, value Envelope) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/consoleauth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/consolequery"
	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
	"github.com/Ad-Quanta/alcor-device-farm/internal/remotecontrol"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
)

const remoteControlRequestTimeout = 10 * time.Second

type consoleHandler struct {
	auth    *consoleauth.Service
	queries *consolequery.Service
	remote  *remotecontrol.Service
}

func RegisterConsole(mux *http.ServeMux, authentication *consoleauth.Service, queries *consolequery.Service, remotes ...*remotecontrol.Service) {
	var remote *remotecontrol.Service
	if len(remotes) > 0 {
		remote = remotes[0]
	}
	handler := &consoleHandler{auth: authentication, queries: queries, remote: remote}
	mux.HandleFunc("POST /console/api/v1/sessions", handler.login)
	mux.HandleFunc("GET /console/api/v1/me", handler.me)
	mux.HandleFunc("DELETE /console/api/v1/sessions/current", handler.logout)
	mux.HandleFunc("GET /api/v1/device-audit-events", handler.listAuditEvents)
	mux.HandleFunc("GET /api/v1/devices/{id}/health-events", handler.listHealthEvents)
	mux.HandleFunc("POST /console/api/v1/devices/{id}/remote-control", handler.startRemoteControl)
	mux.HandleFunc("GET /console/api/v1/devices/{id}/remote-control", handler.getRemoteControl)
	mux.HandleFunc("POST /console/api/v1/devices/{id}/remote-control/heartbeat", handler.heartbeatRemoteControl)
	mux.HandleFunc("DELETE /console/api/v1/devices/{id}/remote-control", handler.endRemoteControl)
	// Alcor's authenticated gateway uses the normal service credential and the
	// audited actor header. These routes deliberately reuse the same remote
	// control service as the standalone Console.
	mux.HandleFunc("POST /api/v1/devices/{id}/remote-control", handler.startRemoteControl)
	mux.HandleFunc("GET /api/v1/devices/{id}/remote-control", handler.getRemoteControl)
	mux.HandleFunc("POST /api/v1/devices/{id}/remote-control/heartbeat", handler.heartbeatRemoteControl)
	mux.HandleFunc("DELETE /api/v1/devices/{id}/remote-control", handler.endRemoteControl)
}

func (handler *consoleHandler) startRemoteControl(writer http.ResponseWriter, request *http.Request) {
	if !handler.remoteAdmin(writer, request) || !requireIdempotencyKey(writer, request) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), remoteControlRequestTimeout)
	defer cancel()
	value, err := handler.remote.Start(ctx, requestActor(request), request.Header.Get("Idempotency-Key"), request.PathValue("id"))
	handler.writeRemote(writer, request, http.StatusAccepted, value, err)
}

func (handler *consoleHandler) getRemoteControl(writer http.ResponseWriter, request *http.Request) {
	if !handler.remoteAdmin(writer, request) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), remoteControlRequestTimeout)
	defer cancel()
	value, err := handler.remote.Get(ctx, requestActor(request).ID, request.PathValue("id"))
	handler.writeRemote(writer, request, http.StatusOK, value, err)
}

func (handler *consoleHandler) heartbeatRemoteControl(writer http.ResponseWriter, request *http.Request) {
	if !handler.remoteAdmin(writer, request) || !requireIdempotencyKey(writer, request) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), remoteControlRequestTimeout)
	defer cancel()
	value, err := handler.remote.Heartbeat(
		ctx, requestActor(request), request.Header.Get("Idempotency-Key"),
		correlation.FromContext(request.Context()).RequestID, request.PathValue("id"),
	)
	handler.writeRemote(writer, request, http.StatusOK, value, err)
}

func (handler *consoleHandler) endRemoteControl(writer http.ResponseWriter, request *http.Request) {
	if !handler.remoteAdmin(writer, request) || !requireIdempotencyKey(writer, request) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), remoteControlRequestTimeout)
	defer cancel()
	value, err := handler.remote.End(
		ctx, requestActor(request), request.Header.Get("Idempotency-Key"),
		correlation.FromContext(request.Context()).RequestID, request.PathValue("id"),
	)
	handler.writeRemote(writer, request, http.StatusOK, value, err)
}

func (handler *consoleHandler) remoteAdmin(writer http.ResponseWriter, request *http.Request) bool {
	principal, ok := auth.FromContext(request.Context())
	allowed := ok && (principal.Role == auth.RoleService ||
		(principal.Role == auth.RoleConsole && principal.ConsoleRole == auth.ConsoleAdmin))
	if !allowed {
		writeForbidden(writer, request, "只有可信平台服务或控制台管理员可以远程控制设备")
		return false
	}
	if handler.remote == nil {
		handler.writeRemote(writer, request, http.StatusOK, nil, remotecontrol.ErrUnavailable)
		return false
	}
	return true
}

func (handler *consoleHandler) writeRemote(writer http.ResponseWriter, request *http.Request, status int, data any, err error) {
	noStore(writer)
	writer.Header().Set("Referrer-Policy", "no-referrer")
	if err == nil {
		httpx.WriteData(writer, request, status, data)
		return
	}
	responseStatus := http.StatusInternalServerError
	apiError := httpx.APIError{Code: "INTERNAL_ERROR", Message: "远程控制操作失败"}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		responseStatus, apiError = http.StatusGatewayTimeout, httpx.APIError{Code: "REMOTE_CONTROL_TIMEOUT", Message: "远程控制操作超时", Retryable: true}
	case errors.Is(err, remotecontrol.ErrUnavailable):
		responseStatus, apiError = http.StatusServiceUnavailable, httpx.APIError{Code: "REMOTE_CONTROL_UNAVAILABLE", Message: "远程控制服务暂时不可用", Retryable: true}
	case errors.Is(err, remotecontrol.ErrNotFound):
		responseStatus, apiError = http.StatusNotFound, httpx.APIError{Code: "REMOTE_CONTROL_NOT_FOUND", Message: "当前没有活动的远程控制连接"}
	case errors.Is(err, remotecontrol.ErrConflict):
		responseStatus, apiError = http.StatusConflict, httpx.APIError{Code: "REMOTE_CONTROL_CONFLICT", Message: "设备当前状态无法启动远程控制", Retryable: true}
	case errors.Is(err, reservation.ErrInvalidArgument):
		responseStatus, apiError = http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: "远程控制请求无效"}
	case errors.Is(err, reservation.ErrForbidden):
		responseStatus, apiError = http.StatusForbidden, httpx.APIError{Code: "FORBIDDEN", Message: "远程控制连接所有者不匹配"}
	}
	httpx.WriteError(writer, request, responseStatus, apiError)
}

func (handler *consoleHandler) login(writer http.ResponseWriter, request *http.Request) {
	if handler.auth == nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "控制台服务尚未配置", Retryable: true})
		return
	}
	var input struct {
		UserID   string `json:"user_id"`
		Password string `json:"password"`
	}
	if !decode(writer, request, &input) {
		return
	}
	if strings.TrimSpace(input.UserID) == "" || input.Password == "" {
		writeInvalid(writer, request, "user_id and password are required")
		return
	}
	created, err := handler.auth.Login(request.Context(), input.UserID, input.Password, clientAddress(request))
	if err != nil {
		if errors.Is(err, consoleauth.ErrRateLimited) {
			writer.Header().Set("Retry-After", "900")
			httpx.WriteError(writer, request, http.StatusTooManyRequests, httpx.APIError{Code: "LOGIN_RATE_LIMITED", Message: "登录失败次数过多，请稍后重试", Retryable: true})
			return
		}
		if errors.Is(err, consoleauth.ErrInvalidCredentials) {
			httpx.WriteError(writer, request, http.StatusUnauthorized, httpx.APIError{Code: "INVALID_CREDENTIALS", Message: "用户账号或密码错误"})
			return
		}
		httpx.WriteError(writer, request, http.StatusInternalServerError, httpx.APIError{Code: "INTERNAL_ERROR", Message: "无法创建控制台会话"})
		return
	}
	http.SetCookie(writer, handler.auth.Cookie(consoleauth.SessionCookieName, created.Token, true, created.View.ExpiresAt))
	http.SetCookie(writer, handler.auth.Cookie(consoleauth.CSRFCookieName, created.CSRFToken, false, created.View.ExpiresAt))
	noStore(writer)
	httpx.WriteData(writer, request, http.StatusCreated, created.View)
}

func (handler *consoleHandler) me(writer http.ResponseWriter, request *http.Request) {
	view, err := handler.auth.Current(request)
	if err != nil {
		httpx.WriteError(writer, request, http.StatusUnauthorized, httpx.APIError{Code: "UNAUTHORIZED", Message: "控制台会话尚未登录"})
		return
	}
	noStore(writer)
	httpx.WriteData(writer, request, http.StatusOK, view)
}

func (handler *consoleHandler) logout(writer http.ResponseWriter, request *http.Request) {
	if err := handler.auth.Logout(request); err != nil {
		httpx.WriteError(writer, request, http.StatusUnauthorized, httpx.APIError{Code: "UNAUTHORIZED", Message: "控制台会话尚未登录"})
		return
	}
	http.SetCookie(writer, handler.auth.ExpiredCookie(consoleauth.SessionCookieName, true))
	http.SetCookie(writer, handler.auth.ExpiredCookie(consoleauth.CSRFCookieName, false))
	noStore(writer)
	httpx.WriteData(writer, request, http.StatusOK, map[string]bool{"revoked": true})
}

func (handler *consoleHandler) listAuditEvents(writer http.ResponseWriter, request *http.Request) {
	if handler.queries == nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "审计事件暂时不可用", Retryable: true})
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "page must be positive and page_size must be between 1 and 200")
		return
	}
	items, total, err := handler.queries.ListAuditEvents(request.Context(), page)
	if err != nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "审计事件暂时不可用", Retryable: true})
		return
	}
	httpx.WriteData(writer, request, http.StatusOK, paging.NewResult(items, page, total))
}

func (handler *consoleHandler) listHealthEvents(writer http.ResponseWriter, request *http.Request) {
	if handler.queries == nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "健康事件暂时不可用", Retryable: true})
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "page must be positive and page_size must be between 1 and 200")
		return
	}
	items, total, err := handler.queries.ListHealthEvents(request.Context(), request.PathValue("id"), page)
	if err != nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "健康事件暂时不可用", Retryable: true})
		return
	}
	httpx.WriteData(writer, request, http.StatusOK, paging.NewResult(items, page, total))
}

func noStore(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
}

// clientAddress returns the address of the client that originated the request.
//
// When the device farm server sits behind the documented same-host reverse
// proxy, the proxy connects from loopback and sets X-Forwarded-For. We only
// trust that header for loopback peers. Trusting every private peer would let
// a client on the same LAN spoof the address used by login rate limiting.
func clientAddress(request *http.Request) string {
	peer := net.ParseIP(strings.TrimSpace(request.RemoteAddr))
	if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		peer = net.ParseIP(strings.TrimSpace(host))
	}
	if peer != nil && peer.IsLoopback() {
		if forwarded := request.Header.Get("X-Forwarded-For"); forwarded != "" {
			if candidate := net.ParseIP(strings.TrimSpace(strings.Split(forwarded, ",")[0])); candidate != nil {
				return candidate.String()
			}
		}
	}
	if peer != nil {
		return peer.String()
	}
	return "127.0.0.1"
}

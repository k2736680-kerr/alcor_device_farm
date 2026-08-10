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
	principal, _ := auth.FromContext(request.Context())
	ctx, cancel := context.WithTimeout(request.Context(), remoteControlRequestTimeout)
	defer cancel()
	value, err := handler.remote.Get(ctx, principal.SubjectID, request.PathValue("id"))
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
	if !ok || principal.Role != auth.RoleConsole || principal.ConsoleRole != auth.ConsoleAdmin {
		writeForbidden(writer, request, "only console admins can control devices remotely")
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
	apiError := httpx.APIError{Code: "INTERNAL_ERROR", Message: "unable to manage remote control"}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		responseStatus, apiError = http.StatusGatewayTimeout, httpx.APIError{Code: "REMOTE_CONTROL_TIMEOUT", Message: "remote control operation timed out", Retryable: true}
	case errors.Is(err, remotecontrol.ErrUnavailable):
		responseStatus, apiError = http.StatusServiceUnavailable, httpx.APIError{Code: "REMOTE_CONTROL_UNAVAILABLE", Message: "remote control is unavailable", Retryable: true}
	case errors.Is(err, remotecontrol.ErrNotFound):
		responseStatus, apiError = http.StatusNotFound, httpx.APIError{Code: "REMOTE_CONTROL_NOT_FOUND", Message: "remote control is not active"}
	case errors.Is(err, remotecontrol.ErrConflict):
		responseStatus, apiError = http.StatusConflict, httpx.APIError{Code: "REMOTE_CONTROL_CONFLICT", Message: "device cannot start remote control in its current state", Retryable: true}
	case errors.Is(err, reservation.ErrInvalidArgument):
		responseStatus, apiError = http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: "invalid remote control request"}
	case errors.Is(err, reservation.ErrForbidden):
		responseStatus, apiError = http.StatusForbidden, httpx.APIError{Code: "FORBIDDEN", Message: "remote control owner does not match"}
	}
	httpx.WriteError(writer, request, responseStatus, apiError)
}

func (handler *consoleHandler) login(writer http.ResponseWriter, request *http.Request) {
	if handler.auth == nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "console is not configured", Retryable: true})
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
			httpx.WriteError(writer, request, http.StatusTooManyRequests, httpx.APIError{Code: "LOGIN_RATE_LIMITED", Message: "too many failed login attempts", Retryable: true})
			return
		}
		if errors.Is(err, consoleauth.ErrInvalidCredentials) {
			httpx.WriteError(writer, request, http.StatusUnauthorized, httpx.APIError{Code: "INVALID_CREDENTIALS", Message: "invalid user ID or password"})
			return
		}
		httpx.WriteError(writer, request, http.StatusInternalServerError, httpx.APIError{Code: "INTERNAL_ERROR", Message: "unable to create console session"})
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
		httpx.WriteError(writer, request, http.StatusUnauthorized, httpx.APIError{Code: "UNAUTHORIZED", Message: "console session is not authenticated"})
		return
	}
	noStore(writer)
	httpx.WriteData(writer, request, http.StatusOK, view)
}

func (handler *consoleHandler) logout(writer http.ResponseWriter, request *http.Request) {
	if err := handler.auth.Logout(request); err != nil {
		httpx.WriteError(writer, request, http.StatusUnauthorized, httpx.APIError{Code: "UNAUTHORIZED", Message: "console session is not authenticated"})
		return
	}
	http.SetCookie(writer, handler.auth.ExpiredCookie(consoleauth.SessionCookieName, true))
	http.SetCookie(writer, handler.auth.ExpiredCookie(consoleauth.CSRFCookieName, false))
	noStore(writer)
	httpx.WriteData(writer, request, http.StatusOK, map[string]bool{"revoked": true})
}

func (handler *consoleHandler) listAuditEvents(writer http.ResponseWriter, request *http.Request) {
	if handler.queries == nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "audit events are unavailable", Retryable: true})
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "page must be positive and page_size must be between 1 and 200")
		return
	}
	items, total, err := handler.queries.ListAuditEvents(request.Context(), page)
	if err != nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "audit events are unavailable", Retryable: true})
		return
	}
	httpx.WriteData(writer, request, http.StatusOK, paging.NewResult(items, page, total))
}

func (handler *consoleHandler) listHealthEvents(writer http.ResponseWriter, request *http.Request) {
	if handler.queries == nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "health events are unavailable", Retryable: true})
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "page must be positive and page_size must be between 1 and 200")
		return
	}
	items, total, err := handler.queries.ListHealthEvents(request.Context(), request.PathValue("id"), page)
	if err != nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "health events are unavailable", Retryable: true})
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

package api

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/Ad-Quanta/alcor-device-farm/internal/consoleauth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/consolequery"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
)

type consoleHandler struct {
	auth    *consoleauth.Service
	queries *consolequery.Service
}

func RegisterConsole(mux *http.ServeMux, authentication *consoleauth.Service, queries *consolequery.Service) {
	handler := &consoleHandler{auth: authentication, queries: queries}
	mux.HandleFunc("POST /console/api/v1/sessions", handler.login)
	mux.HandleFunc("GET /console/api/v1/me", handler.me)
	mux.HandleFunc("DELETE /console/api/v1/sessions/current", handler.logout)
	mux.HandleFunc("GET /api/v1/device-audit-events", handler.listAuditEvents)
	mux.HandleFunc("GET /api/v1/devices/{id}/health-events", handler.listHealthEvents)
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
// When the device farm server sits behind a reverse proxy, the proxy connects
// from a loopback or private address and sets X-Forwarded-For. We only trust
// that header when the immediate peer is loopback/private, so a directly
// exposed server cannot be tricked into accepting a spoofed address. The
// address feeds console login rate limiting, so it must not be attacker
// controlled unless a trusted proxy is provably in front.
func clientAddress(request *http.Request) string {
	peer := net.ParseIP(strings.TrimSpace(request.RemoteAddr))
	if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		peer = net.ParseIP(strings.TrimSpace(host))
	}
	if peer != nil && (peer.IsLoopback() || peer.IsPrivate()) {
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

package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
)

type Role string

const (
	RoleService Role = "service"
	RoleAgent   Role = "agent"
	RoleConsole Role = "console"
)

type ConsoleRole string

const (
	ConsoleViewer   ConsoleRole = "viewer"
	ConsoleOperator ConsoleRole = "operator"
	ConsoleAdmin    ConsoleRole = "admin"
)

type Principal struct {
	Role        Role
	SubjectID   string
	DisplayName string
	ConsoleRole ConsoleRole
}

type ConsoleAuthenticator interface {
	Authenticate(*http.Request) (Principal, error)
	ValidateCSRF(*http.Request, Principal) error
}

type principalContextKey struct{}

// RouteMiddleware keeps health endpoints public, protects the northbound API
// with the service token, and protects the host-agent API with the agent token.
func RouteMiddleware(security config.SecurityConfig, next http.Handler, consoleAuthenticators ...ConsoleAuthenticator) http.Handler {
	var consoleAuthenticator ConsoleAuthenticator
	if len(consoleAuthenticators) > 0 {
		consoleAuthenticator = consoleAuthenticators[0]
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var required Role
		switch {
		case request.URL.Path == "/console/api/v1/sessions" && request.Method == http.MethodPost:
			next.ServeHTTP(writer, request)
			return
		case strings.HasPrefix(request.URL.Path, "/console/api/v1/"):
			required = RoleConsole
		case strings.HasPrefix(request.URL.Path, "/api/v1/"):
			required = RoleService
		case strings.HasPrefix(request.URL.Path, "/internal/v1/"):
			required = RoleAgent
		default:
			next.ServeHTTP(writer, request)
			return
		}

		principal, ok := authenticate(request.Header.Get("Authorization"), security)
		if required == RoleConsole || (required == RoleService && !ok) {
			if consoleAuthenticator != nil {
				var err error
				principal, err = consoleAuthenticator.Authenticate(request)
				ok = err == nil && principal.Role == RoleConsole
			}
		}
		if !ok {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="device-farm"`)
			httpx.WriteError(writer, request, http.StatusUnauthorized, httpx.APIError{
				Code:      "UNAUTHORIZED",
				Message:   "valid bearer token required",
				Retryable: false,
			})
			return
		}
		if principal.Role != required && !(required == RoleService && principal.Role == RoleConsole) {
			httpx.WriteError(writer, request, http.StatusForbidden, httpx.APIError{
				Code:      "FORBIDDEN",
				Message:   "token is not allowed to access this API",
				Retryable: false,
			})
			return
		}
		if principal.Role == RoleConsole {
			if !consoleAllowed(request, principal) {
				httpx.WriteError(writer, request, http.StatusForbidden, httpx.APIError{
					Code: "FORBIDDEN", Message: "console role is not allowed to perform this operation", Retryable: false,
				})
				return
			}
			if requiresCSRF(request) && consoleAuthenticator.ValidateCSRF(request, principal) != nil {
				httpx.WriteError(writer, request, http.StatusForbidden, httpx.APIError{
					Code: "CSRF_VALIDATION_FAILED", Message: "valid CSRF token required", Retryable: false,
				})
				return
			}
		}

		ctx := context.WithValue(request.Context(), principalContextKey{}, principal)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

func consoleAllowed(request *http.Request, principal Principal) bool {
	if strings.HasPrefix(request.URL.Path, "/console/api/v1/") {
		return true
	}
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		if request.URL.Path == "/api/v1/device-audit-events" {
			return principal.ConsoleRole == ConsoleAdmin
		}
		return true
	}
	if strings.HasPrefix(request.URL.Path, "/api/v1/device-reservations") {
		return principal.ConsoleRole == ConsoleOperator || principal.ConsoleRole == ConsoleAdmin
	}
	return principal.ConsoleRole == ConsoleAdmin
}

func requiresCSRF(request *http.Request) bool {
	return request.Method != http.MethodGet && request.Method != http.MethodHead && request.Method != http.MethodOptions
}

func FromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

func authenticate(header string, security config.SecurityConfig) (Principal, bool) {
	token, ok := bearerToken(header)
	if !ok {
		return Principal{}, false
	}
	if constantTimeEqual(token, security.ServiceToken) || constantTimeEqual(token, security.ServicePreviousToken) {
		return Principal{Role: RoleService}, true
	}
	if constantTimeEqual(token, security.AgentToken) || constantTimeEqual(token, security.AgentPreviousToken) {
		return Principal{Role: RoleAgent}, true
	}
	return Principal{}, false
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func constantTimeEqual(actual, expected string) bool {
	if actual == "" || expected == "" || len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

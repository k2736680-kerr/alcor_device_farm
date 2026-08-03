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
)

type Principal struct {
	Role Role
}

type principalContextKey struct{}

// RouteMiddleware keeps health endpoints public, protects the northbound API
// with the service token, and protects the host-agent API with the agent token.
func RouteMiddleware(security config.SecurityConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var required Role
		switch {
		case strings.HasPrefix(request.URL.Path, "/api/v1/"):
			required = RoleService
		case strings.HasPrefix(request.URL.Path, "/internal/v1/"):
			required = RoleAgent
		default:
			next.ServeHTTP(writer, request)
			return
		}

		principal, ok := authenticate(request.Header.Get("Authorization"), security)
		if !ok {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="device-farm"`)
			httpx.WriteError(writer, request, http.StatusUnauthorized, httpx.APIError{
				Code:      "UNAUTHORIZED",
				Message:   "valid bearer token required",
				Retryable: false,
			})
			return
		}
		if principal.Role != required {
			httpx.WriteError(writer, request, http.StatusForbidden, httpx.APIError{
				Code:      "FORBIDDEN",
				Message:   "token is not allowed to access this API",
				Retryable: false,
			})
			return
		}

		ctx := context.WithValue(request.Context(), principalContextKey{}, principal)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
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
	if constantTimeEqual(token, security.ServiceToken) {
		return Principal{Role: RoleService}, true
	}
	if constantTimeEqual(token, security.AgentToken) {
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

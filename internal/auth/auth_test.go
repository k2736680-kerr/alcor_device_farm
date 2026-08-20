package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
)

func TestRouteMiddlewareSeparatesServiceAndAgentIdentities(t *testing.T) {
	security := config.SecurityConfig{ServiceToken: "service-secret", AgentToken: "agent-secret"}
	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := FromContext(request.Context())
		if !ok {
			t.Fatal("protected request has no principal")
		}
		httpx.WriteData(writer, request, http.StatusOK, map[string]string{"role": string(principal.Role)})
	})
	handler := correlation.Middleware(RouteMiddleware(security, next))

	tests := []struct {
		name       string
		path       string
		token      string
		wantStatus int
		wantCode   string
		wantRole   string
	}{
		{name: "missing service token", path: "/api/v1/devices", wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "invalid token", path: "/api/v1/devices", token: "wrong", wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "agent cannot use northbound API", path: "/api/v1/devices", token: "agent-secret", wantStatus: http.StatusForbidden, wantCode: "FORBIDDEN"},
		{name: "service uses northbound API", path: "/api/v1/devices", token: "service-secret", wantStatus: http.StatusOK, wantRole: "service"},
		{name: "service cannot use agent API", path: "/internal/v1/device-hosts/host_1/heartbeats", token: "service-secret", wantStatus: http.StatusForbidden, wantCode: "FORBIDDEN"},
		{name: "agent uses internal API", path: "/internal/v1/device-hosts/host_1/heartbeats", token: "agent-secret", wantStatus: http.StatusOK, wantRole: "agent"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}

			var envelope httpx.Envelope
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if envelope.RequestID == "" {
				t.Fatal("response request_id is empty")
			}
			if test.wantCode != "" && (envelope.Error == nil || envelope.Error.Code != test.wantCode) {
				t.Fatalf("error = %#v, want code %q", envelope.Error, test.wantCode)
			}
			if test.wantStatus == http.StatusUnauthorized && response.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("401 response has no WWW-Authenticate header")
			}
			if test.wantRole != "" {
				data, ok := envelope.Data.(map[string]any)
				if !ok || data["role"] != test.wantRole {
					t.Fatalf("data = %#v, want role %q", envelope.Data, test.wantRole)
				}
			}
			if test.token != "" && contains(response.Body.String(), test.token) {
				t.Fatal("response leaked bearer token")
			}
		})
	}
}

func TestRouteMiddlewareLeavesPublicPathsUnauthenticated(t *testing.T) {
	called := false
	handler := RouteMiddleware(config.SecurityConfig{}, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		called = true
		writer.WriteHeader(http.StatusNoContent)
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if !called || response.Code != http.StatusNoContent {
		t.Fatalf("public endpoint status = %d, called = %v", response.Code, called)
	}
}

func TestIOSRemoteGatewayAcceptsOnlyAuthenticatedConsoleOrService(t *testing.T) {
	security := config.SecurityConfig{ServiceToken: "service-secret"}
	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := FromContext(request.Context())
		if !ok {
			t.Fatal("iOS 远控网关没有认证身份")
		}
		writer.Header().Set("X-Test-Role", string(principal.Role))
		writer.WriteHeader(http.StatusNoContent)
	})
	handler := RouteMiddleware(security, next)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/console/remote/ios/ticket/control", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/console/remote/ios/ticket/control", nil)
	request.Header.Set("Authorization", "Bearer service-secret")
	service := httptest.NewRecorder()
	handler.ServeHTTP(service, request)
	if service.Code != http.StatusNoContent || service.Header().Get("X-Test-Role") != string(RoleService) {
		t.Fatalf("service status=%d role=%q", service.Code, service.Header().Get("X-Test-Role"))
	}
}

func TestEmptyConfiguredTokensFailClosed(t *testing.T) {
	handler := correlation.Middleware(RouteMiddleware(config.SecurityConfig{}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("request with empty configured tokens reached protected handler")
	})))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	request.Header.Set("Authorization", "Bearer anything")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestTokenRotationAcceptsPreviousOnlyDuringConfiguredGraceWindow(t *testing.T) {
	next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	security := config.SecurityConfig{
		ServiceToken: "service-new", ServicePreviousToken: "service-old",
		AgentToken: "agent-new", AgentPreviousToken: "agent-old",
	}
	for _, test := range []struct{ path, token string }{
		{path: "/api/v1/devices", token: "service-new"}, {path: "/api/v1/devices", token: "service-old"},
		{path: "/internal/v1/device-hosts/id/heartbeats", token: "agent-new"},
		{path: "/internal/v1/device-hosts/id/heartbeats", token: "agent-old"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Authorization", "Bearer "+test.token)
		response := httptest.NewRecorder()
		RouteMiddleware(security, next).ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("path=%s token=%s status=%d", test.path, test.token, response.Code)
		}
	}
	security.ServicePreviousToken, security.AgentPreviousToken = "", ""
	for _, test := range []struct{ path, token string }{{"/api/v1/devices", "service-old"}, {"/internal/v1/device-hosts/id/heartbeats", "agent-old"}} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Authorization", "Bearer "+test.token)
		response := httptest.NewRecorder()
		RouteMiddleware(security, next).ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("removed previous token path=%s status=%d", test.path, response.Code)
		}
	}
}

func contains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}

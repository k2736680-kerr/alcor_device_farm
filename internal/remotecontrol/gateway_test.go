package remotecontrol

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/iossession"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
)

type gatewayAuthenticator struct{ principal auth.Principal }

func (stub gatewayAuthenticator) Authenticate(*http.Request) (auth.Principal, error) {
	return stub.principal, nil
}
func (gatewayAuthenticator) ValidateCSRF(*http.Request, auth.Principal) error { return nil }

func TestGatewayShowsOnlyTargetSimulatorPageAndRequiresOwningAdmin(t *testing.T) {
	service, view, closeFence, now := newIOSGatewayService(t)
	defer closeFence()
	mux := http.NewServeMux()
	RegisterGateway(mux, service)
	handler := gatewayRoute(mux, "admin", auth.ConsoleAdmin)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, view.URL, nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "目标 iOS 模拟器实时画面") ||
		strings.Contains(strings.ToLower(recorder.Body.String()), "vnc") || strings.Contains(recorder.Body.String(), "Mac 远程管理密码") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || strings.Contains(view.URL, "127.0.0.1") {
		t.Fatalf("headers=%v url=%s now=%v", recorder.Header(), view.URL, now)
	}

	viewer := gatewayRoute(mux, "admin", auth.ConsoleViewer)
	viewerRecorder := httptest.NewRecorder()
	viewer.ServeHTTP(viewerRecorder, httptest.NewRequest(http.MethodGet, view.URL, nil))
	if viewerRecorder.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d", viewerRecorder.Code)
	}
}

func TestGatewayAllowsTrustedPlatformProxyOnlyForBoundActor(t *testing.T) {
	service, view, closeFence, _ := newIOSGatewayService(t)
	defer closeFence()
	mux := http.NewServeMux()
	RegisterGateway(mux, service)
	handler := auth.RouteMiddleware(config.SecurityConfig{ServiceToken: "service-token"}, mux)

	allowed := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, view.URL, nil)
	request.Header.Set("Authorization", "Bearer service-token")
	request.Header.Set("X-Device-Farm-Actor-Id", "admin")
	handler.ServeHTTP(allowed, request)
	if allowed.Code != http.StatusOK {
		t.Fatalf("platform proxy status=%d body=%s", allowed.Code, allowed.Body.String())
	}

	for _, actor := range []string{"", "other-admin"} {
		denied := httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodGet, view.URL, nil)
		request.Header.Set("Authorization", "Bearer service-token")
		if actor != "" {
			request.Header.Set("X-Device-Farm-Actor-Id", actor)
		}
		handler.ServeHTTP(denied, request)
		if denied.Code != http.StatusForbidden && denied.Code != http.StatusUnauthorized {
			t.Fatalf("actor=%q status=%d", actor, denied.Code)
		}
	}
}

func TestGatewayProxiesOnlyWhitelistedTargetSessionAction(t *testing.T) {
	service, view, closeFence, _ := newIOSGatewayService(t)
	defer closeFence()
	mux := http.NewServeMux()
	RegisterGateway(mux, service)
	handler := gatewayRoute(mux, "admin", auth.ConsoleAdmin)
	actionURL := strings.TrimSuffix(view.URL, "control") + "actions"
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, actionURL, strings.NewReader(`{"type":"tap","x":0.5,"y":0.5}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"value":null`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	forbidden := httptest.NewRecorder()
	handler.ServeHTTP(forbidden, httptest.NewRequest(http.MethodPost,
		strings.TrimSuffix(view.URL, "control")+"execute", strings.NewReader(`{"script":"mobile: shell"}`)))
	if forbidden.Code != http.StatusNotFound {
		t.Fatalf("arbitrary command status=%d", forbidden.Code)
	}
}

func TestGatewayRejectsExpiredPageEntry(t *testing.T) {
	service, view, closeFence, now := newIOSGatewayService(t)
	defer closeFence()
	mux := http.NewServeMux()
	RegisterGateway(mux, service)
	handler := gatewayRoute(mux, "admin", auth.ConsoleAdmin)
	*now = now.Add(time.Minute)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, view.URL, nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expired status=%d", recorder.Code)
	}
}

func TestGatewayKeepsLoadedSessionAliveAfterEntryTicketExpires(t *testing.T) {
	service, view, closeFence, now := newIOSGatewayService(t)
	defer closeFence()
	mux := http.NewServeMux()
	RegisterGateway(mux, service)
	handler := gatewayRoute(mux, "admin", auth.ConsoleAdmin)
	*now = now.Add(31 * time.Second)

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, view.URL, nil))
	if page.Code != http.StatusUnauthorized {
		t.Fatalf("expired entry page status=%d", page.Code)
	}
	client := httptest.NewRecorder()
	handler.ServeHTTP(client, httptest.NewRequest(http.MethodGet,
		strings.TrimSuffix(view.URL, "control")+"client.js", nil))
	if client.Code != http.StatusOK {
		t.Fatalf("loaded session client status=%d body=%s", client.Code, client.Body.String())
	}
	action := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, strings.TrimSuffix(view.URL, "control")+"actions",
		strings.NewReader(`{"type":"tap","x":0.5,"y":0.5}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(action, request)
	if action.Code != http.StatusOK {
		t.Fatalf("loaded session action status=%d body=%s", action.Code, action.Body.String())
	}
}

func newIOSGatewayService(t *testing.T) (*Service, View, func(), *time.Time) {
	t.Helper()
	var receivedPath string
	fence := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedPath = request.URL.Path
		if request.Header.Get("Authorization") != "Bearer test-agent-token-at-least-16-bytes" ||
			request.Header.Get("X-Device-Farm-Host-Id") != "host_000000000000001" ||
			!strings.HasSuffix(receivedPath, "/actions") {
			http.Error(writer, "not authorized", http.StatusForbidden)
			return
		}
		raw, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(raw), `"type":"tap"`) {
			http.Error(writer, "bad action", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"value":null}`))
	}))
	nowValue := time.Date(2026, 8, 18, 8, 0, 0, 0, time.UTC)
	deviceID := "device_00000000000001"
	current := activeReservation(deviceID)
	expires := nowValue.Add(time.Minute)
	current.ExpiresAt = &expires
	sessions := &fakeIOSSessions{binding: iossession.RemoteBindingView{
		ReservationID: current.ID, DeviceID: deviceID, HostID: "host_000000000000001",
		FenceEndpoint: fence.URL, AppiumSessionID: "appium-session-1",
	}}
	service, err := New(&fakeReservations{current: current}, fakeDevices{device: management.Device{
		ID: deviceID, HostID: "host_000000000000001", Platform: "ios", DeviceKind: "simulator",
		ProviderType: "appium_device_farm_ios", ProviderRef: "00000000-0000-0000-0000-000000000001",
	}}, Config{
		IOSEnabled: true, IOSGatewaySecret: "test-ios-gateway-secret-at-least-32-bytes",
		IOSGatewayTokenTTL: 30 * time.Second, AgentToken: "test-agent-token-at-least-16-bytes",
		IOSCreateTimeout: time.Minute, Lease: time.Minute, Heartbeat: 15 * time.Second,
		Now: func() time.Time { return nowValue },
	}, sessions)
	if err != nil {
		fence.Close()
		t.Fatal(err)
	}
	view, err := service.Start(context.Background(), audit.Console("admin"), "remote-start-key", deviceID)
	if err != nil {
		fence.Close()
		t.Fatal(err)
	}
	return service, view, fence.Close, &nowValue
}

func gatewayRoute(mux http.Handler, subject string, role auth.ConsoleRole) http.Handler {
	return auth.RouteMiddleware(config.SecurityConfig{}, mux, gatewayAuthenticator{principal: auth.Principal{
		Role: auth.RoleConsole, SubjectID: subject, ConsoleRole: role,
	}})
}

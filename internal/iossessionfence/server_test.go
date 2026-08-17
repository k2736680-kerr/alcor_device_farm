package iossessionfence

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
)

const testGrant = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestFenceCreatesAndBindsOnlyPinnedSession(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamCalls.Add(1)
		if request.Method != http.MethodPost || request.URL.Path != "/session" || request.Header.Get("Authorization") != "" {
			http.Error(writer, "unexpected upstream request", http.StatusBadRequest)
			return
		}
		raw, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(raw), `"appium:udid":"SIM-1"`) || !strings.Contains(string(raw), `"df:udids":"SIM-1"`) {
			http.Error(writer, "routing was not pinned", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"value":{"sessionId":"appium-session-1","capabilities":{}}}`))
	}))
	defer upstream.Close()

	var bound atomic.Bool
	control := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer agent-token-00000001" {
			http.Error(writer, "missing Agent token", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/internal/v1/ios-session-fence/grants/consumptions":
			writeEnvelope(writer, map[string]any{"reservation_id": "reservation_0001", "device_id": "device_0000000001",
				"upstream_endpoint": upstream.URL, "request": json.RawMessage(`{"capabilities":{"alwaysMatch":{"platformName":"iOS","appium:udid":"SIM-1","df:udids":"SIM-1"},"firstMatch":[{}]}}`)})
		case "/internal/v1/ios-session-fence/sessions/bindings":
			bound.Store(true)
			writeEnvelope(writer, map[string]bool{"bound": true})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer control.Close()

	server, err := New(Config{ListenAddress: "127.0.0.1:0", AdvertiseURL: "http://127.0.0.1:19001",
		ControlServerURL: control.URL, AgentToken: "agent-token-00000001", HostID: "host_000000000001",
		UpstreamEndpoint: upstream.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(`{"capabilities":{}}`))
	request.Header.Set("Authorization", "Session-Grant "+testGrant)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bound.Load() || upstreamCalls.Load() != 1 || !strings.Contains(response.Body.String(), "appium-session-1") {
		t.Fatalf("status=%d bound=%v upstream=%d body=%s", response.Code, bound.Load(), upstreamCalls.Load(), response.Body.String())
	}
}

func TestFenceRejectsDashboardAndProtectsCleanup(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	control := httptest.NewServer(http.NotFoundHandler())
	defer control.Close()
	server, err := New(Config{ListenAddress: "127.0.0.1:0", AdvertiseURL: "http://127.0.0.1:19001",
		ControlServerURL: control.URL, AgentToken: "agent-token-00000001", HostID: "host_000000000001",
		UpstreamEndpoint: upstream.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	dashboard := httptest.NewRequest(http.MethodGet, "/device-farm/", nil)
	dashboard.Header.Set("Authorization", "Session-Grant "+testGrant)
	dashboardResponse := httptest.NewRecorder()
	server.ServeHTTP(dashboardResponse, dashboard)
	if dashboardResponse.Code != http.StatusNotFound {
		t.Fatalf("dashboard status=%d", dashboardResponse.Code)
	}
	cleanup := httptest.NewRequest(http.MethodDelete, "/internal/v1/ios-session-fence/sessions/appium-session-1", nil)
	cleanupResponse := httptest.NewRecorder()
	server.ServeHTTP(cleanupResponse, cleanup)
	if cleanupResponse.Code != http.StatusForbidden {
		t.Fatalf("cleanup status=%d", cleanupResponse.Code)
	}
	cleanup = httptest.NewRequest(http.MethodDelete, "/internal/v1/ios-session-fence/sessions/appium-session-1", nil)
	cleanup.Header.Set("Authorization", "Bearer agent-token-00000001")
	cleanup.Header.Set("X-Device-Farm-Host-Id", "host_000000000001")
	cleanupResponse = httptest.NewRecorder()
	server.ServeHTTP(cleanupResponse, cleanup)
	if cleanupResponse.Code != http.StatusNoContent {
		t.Fatalf("authorized cleanup status=%d", cleanupResponse.Code)
	}
}

func writeEnvelope(writer http.ResponseWriter, data any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(httpx.Envelope{RequestID: "request_0000000001", Data: data})
}

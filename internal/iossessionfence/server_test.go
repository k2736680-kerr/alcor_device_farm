package iossessionfence

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		if !strings.Contains(string(raw), `"appium:udid":"SIM-1"`) || !strings.Contains(string(raw), `"df:udids":"SIM-1"`) ||
			!strings.Contains(string(raw), `"appium:mjpegServerPort":`) {
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

func TestFenceRemoteActionsAreNormalizedAndWhitelisted(t *testing.T) {
	var actionPayload map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/window/rect"):
			_, _ = writer.Write([]byte(`{"value":{"width":400,"height":800}}`))
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/actions"):
			_ = json.NewDecoder(request.Body).Decode(&actionPayload)
			_, _ = writer.Write([]byte(`{"value":null}`))
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/screenshot"):
			encoded := base64.StdEncoding.EncodeToString([]byte("png-image"))
			_, _ = writer.Write([]byte(`{"value":"` + encoded + `"}`))
		default:
			http.NotFound(writer, request)
		}
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
	action := httptest.NewRequest(http.MethodPost, "/internal/v1/ios-remote/sessions/appium-session-1/actions",
		strings.NewReader(`{"type":"tap","x":0.5,"y":0.25}`))
	action.Header.Set("Authorization", "Bearer agent-token-00000001")
	action.Header.Set("X-Device-Farm-Host-Id", "host_000000000001")
	actionResponse := httptest.NewRecorder()
	server.ServeHTTP(actionResponse, action)
	if actionResponse.Code != http.StatusOK {
		t.Fatalf("action status=%d body=%s", actionResponse.Code, actionResponse.Body.String())
	}
	encoded, _ := json.Marshal(actionPayload)
	if !strings.Contains(string(encoded), `"x":200`) || !strings.Contains(string(encoded), `"y":200`) {
		t.Fatalf("translated action=%s", encoded)
	}

	frame := httptest.NewRequest(http.MethodGet, "/internal/v1/ios-remote/sessions/appium-session-1/frame", nil)
	frame.Header.Set("Authorization", "Bearer agent-token-00000001")
	frame.Header.Set("X-Device-Farm-Host-Id", "host_000000000001")
	frameResponse := httptest.NewRecorder()
	server.ServeHTTP(frameResponse, frame)
	if frameResponse.Code != http.StatusOK || frameResponse.Body.String() != "png-image" || frameResponse.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("frame status=%d headers=%v body=%s", frameResponse.Code, frameResponse.Header(), frameResponse.Body.String())
	}

	arbitrary := httptest.NewRequest(http.MethodPost, "/internal/v1/ios-remote/sessions/appium-session-1/execute", strings.NewReader(`{}`))
	arbitrary.Header.Set("Authorization", "Bearer agent-token-00000001")
	arbitrary.Header.Set("X-Device-Farm-Host-Id", "host_000000000001")
	arbitraryResponse := httptest.NewRecorder()
	server.ServeHTTP(arbitraryResponse, arbitrary)
	if arbitraryResponse.Code != http.StatusNotFound {
		t.Fatalf("arbitrary status=%d", arbitraryResponse.Code)
	}
}

func TestFenceRecoversMJPEGPortFromBoundSessionAfterRestart(t *testing.T) {
	mjpeg := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
		_, _ = writer.Write([]byte("--frame\r\nContent-Type: image/jpeg\r\n\r\nimage\r\n--frame--\r\n"))
	}))
	defer mjpeg.Close()
	port, err := strconv.Atoi(strings.TrimPrefix(mjpeg.URL, "http://127.0.0.1:"))
	if err != nil {
		t.Fatal(err)
	}
	var capabilityReads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/session/appium-session-restart" {
			http.NotFound(writer, request)
			return
		}
		capabilityReads.Add(1)
		_, _ = writer.Write([]byte(`{"value":{"capabilities":{"appium:mjpegServerPort":` + strconv.Itoa(port) + `}}}`))
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
	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodGet,
			"/internal/v1/ios-remote/sessions/appium-session-restart/stream", nil)
		request.Header.Set("Authorization", "Bearer agent-token-00000001")
		request.Header.Set("X-Device-Farm-Host-Id", "host_000000000001")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "image") {
			t.Fatalf("attempt=%d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if capabilityReads.Load() != 1 {
		t.Fatalf("capability reads=%d want=1", capabilityReads.Load())
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

func TestSessionCreateFailureClassificationDoesNotPersistUpstreamBody(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{name: "开发者模式", body: `{"value":{"message":"Developer Mode is disabled"}}`, want: "IOS_DEVELOPER_MODE_DISABLED"},
		{name: "未信任", body: `{"value":{"message":"Device is not trusted"}}`, want: "IOS_PHYSICAL_NOT_TRUSTED"},
		{name: "Xcode 不兼容", body: `{"value":{"message":"Could not locate Device Support files"}}`, want: "IOS_XCODE_INCOMPATIBLE"},
		{name: "WDA 签名", body: `{"value":{"message":"Signing requires a provisioning profile"}}`, want: "WDA_SIGNING_FAILED"},
		{name: "WDA 启动", body: `{"value":{"message":"WebDriverAgent did not start"}}`, want: "WDA_START_FAILED"},
		{name: "其他 Appium 失败", body: `{"value":{"message":"unknown upstream failure"}}`, want: "APPIUM_SESSION_CREATE_REJECTED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifySessionCreateFailure([]byte(test.body)); got != test.want {
				t.Fatalf("失败分类=%s，预期=%s", got, test.want)
			}
		})
	}
}

func writeEnvelope(writer http.ResponseWriter, data any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(httpx.Envelope{RequestID: "request_0000000001", Data: data})
}

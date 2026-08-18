package appiumdevicefarm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

func TestInventoryIsReadOnlyAllowlistedAndPreservesBusy(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodGet || request.URL.Path != "/device-farm/api/device/ios" || request.Header.Get("Authorization") != "" {
			http.Error(writer, "unexpected request", http.StatusBadRequest)
			return
		}
		_, _ = writer.Write([]byte(`[
			{"udid":"SIM-2","name":"iPhone 17","state":"Shutdown","sdk":"26.3","platform":"ios","deviceType":"simulator","busy":false,"realDevice":false},
			{"udid":"SIM-1","name":"iPhone 16","state":"Booted","sdk":"18.6","platform":"ios","deviceType":"simulator","busy":true,"realDevice":false},
			{"udid":"TV-1","platform":"tvos","deviceType":"simulator"}
		]`))
	}))
	defer server.Close()
	client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, AllowUDIDs: []string{"SIM-1"}})
	if err != nil {
		t.Fatal(err)
	}
	devices, err := client.Inventory(context.Background())
	if err != nil || len(devices) != 2 || devices[0].UDID != "SIM-1" || !devices[0].Allowed || !devices[0].Busy || devices[1].Allowed || requests != 1 {
		t.Fatalf("devices=%+v requests=%d error=%v", devices, requests, err)
	}
}

func TestInventoryMergesHubNodeDuplicateUDID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`[
			{"udid":"SIM-DUPLICATE","name":"iPhone 17 Pro","state":"Shutdown","sdk":"26.3","platform":"ios","deviceType":"simulator","busy":false},
			{"udid":"SIM-DUPLICATE","name":"iPhone 17 Pro","state":"Booted","sdk":"26.3","platform":"ios","deviceType":"simulator","busy":true}
		]`))
	}))
	defer server.Close()
	client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, AllowUDIDs: []string{"SIM-DUPLICATE"}})
	if err != nil {
		t.Fatal(err)
	}
	devices, err := client.Inventory(context.Background())
	if err != nil || len(devices) != 1 || devices[0].State != "Booted" || !devices[0].Busy {
		t.Fatalf("合并后的 Hub/Node 设备=%#v，错误=%v", devices, err)
	}
}

func TestInventoryRejectsConflictingDuplicateUDIDWithChineseMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`[
			{"udid":"SIM-DUPLICATE","name":"iPhone 17 Pro","platform":"ios","deviceType":"simulator"},
			{"udid":"SIM-DUPLICATE","name":"iPhone 16 Pro","platform":"ios","deviceType":"simulator"}
		]`))
	}))
	defer server.Close()
	client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, AllowUDIDs: []string{"SIM-DUPLICATE"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Inventory(context.Background()); err == nil || !strings.Contains(err.Error(), "身份冲突的重复 UDID") {
		t.Fatalf("重复 UDID 冲突错误=%v", err)
	}
}

func TestProviderRequiresAllowlistAndHealthyNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/device-farm/api/device/ios":
			_, _ = writer.Write([]byte(`[{"udid":"SIM-1","name":"iPhone","state":"Booted","sdk":"26.3","platform":"ios","deviceType":"simulator","busy":false,"realDevice":false}]`))
		case "/status":
			_, _ = writer.Write([]byte(`{"value":{"ready":true}}`))
		case "/device-farm/api/status":
			_, _ = writer.Write([]byte(`{"status":"ok","version":"12.0.1"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	allowed, _ := New(Config{Endpoint: server.URL, Timeout: time.Second, AllowUDIDs: []string{"SIM-1"},
		CommandRunner: &simulatorRunner{state: "Booted"}})
	snapshots, err := allowed.Discover(context.Background(), "host_000000000000001")
	if err != nil || len(snapshots) != 1 || !snapshots[0].Ready() || snapshots[0].Platform != providers.PlatformIOS || snapshots[0].Connection.ADBEndpoint != "" {
		t.Fatalf("snapshots=%+v error=%v", snapshots, err)
	}
	unknown, _ := New(Config{Endpoint: server.URL, Timeout: time.Second})
	snapshots, err = unknown.Discover(context.Background(), "host_000000000000001")
	if err != nil || len(snapshots) != 1 || snapshots[0].Ready() || snapshots[0].State != providers.StateStopped {
		t.Fatalf("unknown snapshots=%+v error=%v", snapshots, err)
	}
	if _, err := unknown.Create(context.Background(), providers.CreateRequest{}); providers.ErrorCode(err) != "INVALID_ARGUMENT" {
		t.Fatalf("unexpected mutation error: %v", err)
	}
}

func TestBusyDeviceRemainsTechnicallyHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/device-farm/api/device/ios":
			_, _ = writer.Write([]byte(`[{"udid":"SIM-1","name":"iPhone","state":"Booted","sdk":"26.3","platform":"ios","deviceType":"simulator","busy":true,"realDevice":false}]`))
		case "/status":
			_, _ = writer.Write([]byte(`{"value":{"ready":true}}`))
		case "/device-farm/api/status":
			_, _ = writer.Write([]byte(`{"status":"ok","version":"12.0.1"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, AllowUDIDs: []string{"SIM-1"},
		CommandRunner: &simulatorRunner{state: "Booted"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := client.Discover(context.Background(), "host_000000000000001")
	if err != nil || len(snapshots) != 1 || !snapshots[0].Ready() {
		t.Fatalf("snapshots=%+v error=%v", snapshots, err)
	}
	providerBusy, ok := snapshots[0].Capabilities["providerBusy"].(bool)
	if !ok || !providerBusy {
		t.Fatalf("providerBusy capability=%v, want true", snapshots[0].Capabilities["providerBusy"])
	}
}

func TestHealthRejectsVersionDriftAndRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/status" {
			_, _ = writer.Write([]byte(`{"value":{"ready":true}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"status":"ok","version":"11.0.0"}`))
	}))
	defer server.Close()
	client, _ := New(Config{Endpoint: server.URL, Timeout: time.Second})
	health, err := client.Health(context.Background())
	if err != nil || !health.Ready() || health.PluginVersion != "11.0.0" {
		t.Fatalf("health=%+v error=%v", health, err)
	}
	if _, err := New(Config{Endpoint: "http://user:secret@127.0.0.1:4723", Timeout: time.Second}); err == nil {
		t.Fatal("credential URL must be rejected")
	}
	if _, err := New(Config{Endpoint: "http://192.0.2.10:4723", Timeout: time.Second}); err == nil {
		t.Fatal("non-loopback Node endpoint must be rejected")
	}
}

func TestCustomHTTPClientCannotEnableRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	custom := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
	client, err := New(Config{Endpoint: redirect.URL, Timeout: time.Second, HTTPClient: custom})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Inventory(context.Background()); err == nil {
		t.Fatal("custom HTTP client must not enable redirects")
	}
	if custom.CheckRedirect == nil {
		t.Fatal("caller-owned HTTP client must not be mutated")
	}
}

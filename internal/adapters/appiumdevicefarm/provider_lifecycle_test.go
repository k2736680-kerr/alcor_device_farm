package appiumdevicefarm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

type simulatorRunner struct {
	mutex          sync.Mutex
	state          string
	commands       []string
	hangBootStatus bool
}

func (runner *simulatorRunner) Run(ctx context.Context, binary string, arguments ...string) ([]byte, error) {
	runner.mutex.Lock()
	runner.commands = append(runner.commands, binary+" "+strings.Join(arguments, " "))
	var output []byte
	if len(arguments) >= 2 && arguments[0] == "simctl" {
		switch arguments[1] {
		case "boot":
			runner.state = "Booted"
		case "shutdown":
			runner.state = "Shutdown"
		case "bootstatus":
			if runner.hangBootStatus {
				runner.mutex.Unlock()
				<-ctx.Done()
				return nil, ctx.Err()
			}
		case "list":
			udid := "SIM-ALLOWED"
			if len(arguments) >= 4 && arguments[3] != "-j" {
				udid = arguments[3]
			}
			output, _ = json.Marshal(map[string]any{"devices": map[string]any{"com.apple.CoreSimulator.SimRuntime.iOS-26-3": []map[string]any{{
				"udid": udid, "name": "iPhone 17 Pro", "state": runner.state,
				"deviceTypeIdentifier": "com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro",
			}}}})
		}
	}
	runner.mutex.Unlock()
	return output, nil
}

func (runner *simulatorRunner) inventoryState() string {
	runner.mutex.Lock()
	defer runner.mutex.Unlock()
	return runner.state
}

func simulatorServer(t *testing.T, runner *simulatorRunner, realDevice bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/device-farm/api/device/ios":
			deviceType := "simulator"
			if realDevice {
				deviceType = "real"
			}
			_ = json.NewEncoder(writer).Encode([]map[string]any{{
				"udid": "SIM-ALLOWED", "name": "iPhone 17 Pro", "state": runner.inventoryState(),
				"sdk": "26.3", "platform": "ios", "deviceType": deviceType,
				"busy": false, "realDevice": realDevice,
			}})
		case "/status":
			_, _ = writer.Write([]byte(`{"value":{"ready":true}}`))
		case "/device-farm/api/status":
			_, _ = writer.Write([]byte(`{"status":"ok","version":"12.0.1"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestSimulatorLifecycleUsesOnlyControlledSimctlCommands(t *testing.T) {
	runner := &simulatorRunner{state: "Shutdown"}
	server := simulatorServer(t, runner, false)
	client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, AllowUDIDs: []string{"SIM-ALLOWED"},
		XcrunBinary: "/usr/bin/xcrun", LifecyclePollInterval: time.Millisecond, ReadinessStableDuration: 2 * time.Millisecond, CommandRunner: runner})
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := client.Discover(context.Background(), "host_000000000000001")
	if err != nil || len(discovered) != 1 || discovered[0].State != providers.StateStopped || discovered[0].Ready() {
		t.Fatalf("停止状态发现结果=%+v 错误=%v", discovered, err)
	}
	started, err := client.Start(context.Background(), "SIM-ALLOWED")
	if err != nil || !started.Ready() {
		t.Fatalf("启动结果=%+v 错误=%v", started, err)
	}
	stopped, err := client.Stop(context.Background(), "SIM-ALLOWED")
	if err != nil || stopped.State != providers.StateStopped || stopped.Ready() {
		t.Fatalf("停止结果=%+v 错误=%v", stopped, err)
	}
	restarted, err := client.Restart(context.Background(), "SIM-ALLOWED")
	if err != nil || !restarted.Ready() {
		t.Fatalf("重启结果=%+v 错误=%v", restarted, err)
	}
	runner.mutex.Lock()
	commands := append([]string(nil), runner.commands...)
	runner.mutex.Unlock()
	wantMutations := []string{
		"/usr/bin/xcrun simctl boot SIM-ALLOWED", "/usr/bin/xcrun simctl bootstatus SIM-ALLOWED -b",
		"/usr/bin/xcrun simctl shutdown SIM-ALLOWED",
		"/usr/bin/xcrun simctl boot SIM-ALLOWED", "/usr/bin/xcrun simctl bootstatus SIM-ALLOWED -b",
	}
	mutations := make([]string, 0, len(wantMutations))
	for _, command := range commands {
		if command == "/usr/bin/xcrun simctl list devices SIM-ALLOWED -j" || command == "/usr/bin/xcrun simctl list devices -j" {
			continue
		}
		if command != "/usr/bin/xcrun simctl boot SIM-ALLOWED" &&
			command != "/usr/bin/xcrun simctl bootstatus SIM-ALLOWED -b" &&
			command != "/usr/bin/xcrun simctl shutdown SIM-ALLOWED" {
			t.Fatalf("出现未批准的 Simulator 命令：%q", command)
		}
		mutations = append(mutations, command)
	}
	if strings.Join(mutations, "\n") != strings.Join(wantMutations, "\n") {
		t.Fatalf("受控变更命令不匹配：\n实际=%q\n预期=%q", mutations, wantMutations)
	}
}

func TestSimulatorLifecycleRejectsUnknownAndPhysicalDevices(t *testing.T) {
	for _, test := range []struct {
		name, requested string
		realDevice      bool
		wantCode        string
	}{
		{name: "未列入 allowlist", requested: "SIM-UNKNOWN", wantCode: "PROVIDER_DEVICE_NOT_FOUND"},
		{name: "真机不能调用 Simulator 生命周期", requested: "SIM-ALLOWED", realDevice: true, wantCode: "IOS_SIMULATOR_OPERATION_REQUIRED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &simulatorRunner{state: "Shutdown"}
			server := simulatorServer(t, runner, test.realDevice)
			client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, AllowUDIDs: []string{"SIM-ALLOWED"}, CommandRunner: runner})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Start(context.Background(), test.requested); providers.ErrorCode(err) != test.wantCode {
				t.Fatalf("错误=%v，错误码应为 %s", err, test.wantCode)
			}
			for _, command := range runner.commands {
				if !strings.Contains(command, " simctl list devices ") {
					t.Fatalf("非法设备触发了变更命令：%v", runner.commands)
				}
			}
		})
	}
}

func TestSimulatorBootTimeoutHasStableChineseFailure(t *testing.T) {
	runner := &simulatorRunner{state: "Shutdown", hangBootStatus: true}
	server := simulatorServer(t, runner, false)
	client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, AllowUDIDs: []string{"SIM-ALLOWED"}, CommandRunner: runner})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = client.Start(ctx, "SIM-ALLOWED")
	if providers.ErrorCode(err) != "IOS_SIMULATOR_BOOT_TIMEOUT" || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("启动超时错误=%v", err)
	}
}

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

const (
	testRuntimeID    = "com.apple.CoreSimulator.SimRuntime.iOS-26-3"
	testDeviceTypeID = "com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro"
	testLegacyTypeID = "com.apple.CoreSimulator.SimDeviceType.iPhone-15-Pro"
	testDynamicUDID  = "11111111-2222-3333-4444-555555555555"
)

type dynamicRunner struct {
	mu             sync.Mutex
	created        bool
	staleInventory bool
	state          string
	name           string
	commands       []string
	unregistered   int
}

func (runner *dynamicRunner) Run(_ context.Context, binary string, arguments ...string) ([]byte, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.commands = append(runner.commands, binary+" "+strings.Join(arguments, " "))
	if len(arguments) < 2 || arguments[0] != "simctl" {
		return nil, nil
	}
	switch arguments[1] {
	case "create":
		runner.created, runner.state, runner.name = true, "Shutdown", arguments[2]
		return []byte(testDynamicUDID + "\n"), nil
	case "boot":
		runner.state = "Booted"
	case "shutdown":
		runner.state = "Shutdown"
	case "delete":
		runner.created = false
	case "list":
		if len(arguments) < 3 {
			return nil, nil
		}
		switch arguments[2] {
		case "runtimes":
			return []byte(`{"runtimes":[{"identifier":"` + testRuntimeID + `","name":"iOS 26.3","version":"26.3","isAvailable":true,"supportedDeviceTypes":[{"identifier":"` + testDeviceTypeID + `"}]}]}`), nil
		case "devicetypes":
			return []byte(`{"devicetypes":[{"identifier":"` + testDeviceTypeID + `","name":"iPhone 17 Pro","isAvailable":true},{"identifier":"` + testLegacyTypeID + `","name":"iPhone 15 Pro","isAvailable":true}]}`), nil
		case "devices":
			items := []map[string]any{}
			if runner.created {
				items = append(items, map[string]any{"udid": testDynamicUDID, "name": runner.name, "state": runner.state,
					"deviceTypeIdentifier": testDeviceTypeID, "isAvailable": true})
			}
			return json.Marshal(map[string]any{"devices": map[string]any{testRuntimeID: items}})
		}
	}
	return nil, nil
}

func (runner *dynamicRunner) inventory() []map[string]any {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if !runner.created && !runner.staleInventory {
		return []map[string]any{}
	}
	return []map[string]any{{"udid": testDynamicUDID, "name": runner.name, "state": runner.state, "sdk": "26.3",
		"platform": "ios", "deviceType": "simulator", "busy": false, "realDevice": false, "host": "http://127.0.0.1:4724"}}
}

func dynamicServer(t *testing.T, runner *dynamicRunner) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/device-farm/api/device/ios":
			_ = json.NewEncoder(writer).Encode(runner.inventory())
		case "/device-farm/api/register":
			if request.Method != http.MethodPost || request.URL.Query().Get("type") != "remove" {
				http.Error(writer, "unexpected registration", http.StatusBadRequest)
				return
			}
			var devices []map[string]any
			if json.NewDecoder(request.Body).Decode(&devices) != nil || len(devices) != 1 || devices[0]["udid"] != testDynamicUDID {
				http.Error(writer, "unexpected unregister payload", http.StatusBadRequest)
				return
			}
			runner.mu.Lock()
			runner.staleInventory = false
			runner.unregistered++
			runner.mu.Unlock()
			_, _ = writer.Write([]byte(`{"success":true}`))
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

func TestDynamicSimulatorDeleteIgnoresStalePluginInventory(t *testing.T) {
	runner := &dynamicRunner{created: false, staleInventory: true, state: "Booted", name: "Alcor-DF-stale"}
	server := dynamicServer(t, runner)
	client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, CommandRunner: runner,
		AllowedRuntimeIDs: []string{testRuntimeID}, AllowedDeviceTypeIDs: []string{testDeviceTypeID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(context.Background(), testDynamicUDID); err != nil {
		t.Fatalf("CoreSimulator 已不存在时，插件残留 inventory 不应阻止幂等删除：%v", err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.unregistered != 1 || runner.staleInventory {
		t.Fatalf("Appium Device Farm 残留清单未注销：count=%d stale=%v", runner.unregistered, runner.staleInventory)
	}
	for _, command := range runner.commands {
		if strings.Contains(command, " simctl delete ") || strings.Contains(command, " simctl shutdown ") {
			t.Fatalf("CoreSimulator 已不存在时不应再次执行删除命令：%v", runner.commands)
		}
	}
}

func TestDynamicSimulatorDeleteUnregistersHubAndNodeInventories(t *testing.T) {
	runner := &dynamicRunner{created: false, staleInventory: true, state: "Booted", name: "Alcor-DF-stale"}
	type endpointState struct {
		mu      sync.Mutex
		present bool
		posts   int
	}
	newEndpoint := func(state *endpointState) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			state.mu.Lock()
			defer state.mu.Unlock()
			switch request.URL.Path {
			case "/device-farm/api/device/ios":
				items := []map[string]any{}
				if state.present {
					items = append(items, map[string]any{"udid": testDynamicUDID, "name": "Alcor-DF-stale", "host": "http://127.0.0.1:4724", "platform": "ios", "deviceType": "simulator"})
				}
				_ = json.NewEncoder(writer).Encode(items)
			case "/device-farm/api/register":
				state.posts++
				state.present = false
				_, _ = writer.Write([]byte(`{"success":true}`))
			default:
				http.NotFound(writer, request)
			}
		}))
		t.Cleanup(server.Close)
		return server
	}
	hubState, nodeState := &endpointState{present: true}, &endpointState{present: true}
	hub, node := newEndpoint(hubState), newEndpoint(nodeState)
	client, err := New(Config{Endpoint: hub.URL, RegistrationEndpoints: []string{hub.URL, node.URL}, Timeout: time.Second,
		CommandRunner: runner, AllowedRuntimeIDs: []string{testRuntimeID}, AllowedDeviceTypeIDs: []string{testDeviceTypeID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(context.Background(), testDynamicUDID); err != nil {
		t.Fatal(err)
	}
	hubState.mu.Lock()
	hubPosts := hubState.posts
	hubState.mu.Unlock()
	nodeState.mu.Lock()
	nodePosts := nodeState.posts
	nodeState.mu.Unlock()
	if hubPosts != 1 || nodePosts != 1 {
		t.Fatalf("Hub/Node 注销次数=%d/%d", hubPosts, nodePosts)
	}
}

func TestDynamicSimulatorCreateRebuildDeleteUsesControlledCatalog(t *testing.T) {
	runner := &dynamicRunner{}
	server := dynamicServer(t, runner)
	client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, CommandRunner: runner, LifecyclePollInterval: time.Millisecond,
		ReadinessStableDuration: 2 * time.Millisecond,
		AllowedRuntimeIDs:       []string{testRuntimeID}, AllowedDeviceTypeIDs: []string{testDeviceTypeID}})
	if err != nil {
		t.Fatal(err)
	}
	request := providers.CreateRequest{DeviceID: "device_0000000000001", HostID: "host_000000000000001", Platform: providers.PlatformIOS,
		DeviceKind: "simulator", Capabilities: map[string]any{"runtimeId": testRuntimeID, "deviceTypeId": testDeviceTypeID}}
	created, err := client.Create(context.Background(), request)
	if err != nil || created.ProviderRef != testDynamicUDID || created.State != providers.StateStopped {
		t.Fatalf("创建结果=%+v 错误=%v", created, err)
	}
	// 相同 Device ID 重放必须复用同一个 CoreSimulator。
	if replay, replayErr := client.Create(context.Background(), request); replayErr != nil || replay.ProviderRef != testDynamicUDID {
		t.Fatalf("幂等重放结果=%+v 错误=%v", replay, replayErr)
	}
	if started, startErr := client.Start(context.Background(), testDynamicUDID); startErr != nil || !started.Ready() {
		t.Fatalf("启动结果=%+v 错误=%v", started, startErr)
	}
	if rebuilt, rebuildErr := client.Rebuild(context.Background(), testDynamicUDID); rebuildErr != nil || !rebuilt.Ready() {
		t.Fatalf("重建结果=%+v 错误=%v", rebuilt, rebuildErr)
	}
	if err := client.Delete(context.Background(), testDynamicUDID); err != nil {
		t.Fatal(err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	creates := 0
	for _, command := range runner.commands {
		if strings.Contains(command, " simctl create ") {
			creates++
		}
	}
	if creates != 1 || runner.created {
		t.Fatalf("create 次数=%d，删除后仍存在=%v，命令=%v", creates, runner.created, runner.commands)
	}
}

func TestDynamicSimulatorRejectsCatalogBypassBeforeMutation(t *testing.T) {
	runner := &dynamicRunner{}
	server := dynamicServer(t, runner)
	client, _ := New(Config{Endpoint: server.URL, Timeout: time.Second, CommandRunner: runner,
		AllowedRuntimeIDs: []string{testRuntimeID}, AllowedDeviceTypeIDs: []string{testDeviceTypeID}})
	_, err := client.Create(context.Background(), providers.CreateRequest{DeviceID: "device_0000000000001", Platform: providers.PlatformIOS,
		DeviceKind: "simulator", Capabilities: map[string]any{"runtimeId": "../../任意命令", "deviceTypeId": testDeviceTypeID}})
	if providers.ErrorCode(err) != "IOS_RUNTIME_NOT_ALLOWED" {
		t.Fatalf("错误=%v", err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	for _, command := range runner.commands {
		if strings.Contains(command, " simctl create ") {
			t.Fatalf("非法目录项触发了创建：%v", runner.commands)
		}
	}
}

func TestDynamicSimulatorRejectsIncompatibleRuntimeAndDeviceTypeBeforeMutation(t *testing.T) {
	runner := &dynamicRunner{}
	server := dynamicServer(t, runner)
	client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, CommandRunner: runner,
		AllowedRuntimeIDs: []string{testRuntimeID}, AllowedDeviceTypeIDs: []string{testDeviceTypeID, testLegacyTypeID}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Create(context.Background(), providers.CreateRequest{DeviceID: "device_0000000000001", Platform: providers.PlatformIOS,
		DeviceKind: "simulator", Capabilities: map[string]any{"runtimeId": testRuntimeID, "deviceTypeId": testLegacyTypeID}})
	if providers.ErrorCode(err) != "IOS_SIMULATOR_COMBINATION_UNSUPPORTED" || !strings.Contains(err.Error(), "不兼容") {
		t.Fatalf("错误=%v", err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	for _, command := range runner.commands {
		if strings.Contains(command, " simctl create ") {
			t.Fatalf("不兼容组合触发了创建：%v", runner.commands)
		}
	}
}

func TestDynamicSimulatorCreateFailureCleanupDoesNotDependOnAppiumInventory(t *testing.T) {
	runner := &dynamicRunner{created: true, state: "Booted", name: "Alcor-DF-device_0000000000001"}
	client, err := New(Config{Endpoint: "http://127.0.0.1:1", Timeout: time.Millisecond, CommandRunner: runner,
		ManagedNamePrefix: "Alcor-DF-", AllowedRuntimeIDs: []string{testRuntimeID}, AllowedDeviceTypeIDs: []string{testDeviceTypeID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CleanupCreated(context.Background(), testDynamicUDID); err != nil {
		t.Fatal(err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.created {
		t.Fatal("创建失败补偿后仍残留 CoreSimulator")
	}
	commands := strings.Join(runner.commands, "\n")
	if !strings.Contains(commands, "simctl shutdown "+testDynamicUDID) || !strings.Contains(commands, "simctl delete "+testDynamicUDID) {
		t.Fatalf("失败补偿命令不完整：%s", commands)
	}
}

func TestDynamicSimulatorDeleteIsIdempotentWhenCreateNeverAllocatedAProvider(t *testing.T) {
	runner := &dynamicRunner{}
	server := dynamicServer(t, runner)
	client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, CommandRunner: runner,
		AllowedRuntimeIDs: []string{testRuntimeID}, AllowedDeviceTypeIDs: []string{testDeviceTypeID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(context.Background(), "pending:device_0000000000001"); err != nil {
		t.Fatalf("删除未创建成功的 Simulator 应幂等完成：%v", err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	for _, command := range runner.commands {
		if strings.Contains(command, " simctl delete ") || strings.Contains(command, " simctl shutdown ") {
			t.Fatalf("不存在的 Simulator 不应触发变更命令：%v", runner.commands)
		}
	}
}

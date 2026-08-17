package agent_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/agent"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	providermock "github.com/Ad-Quanta/alcor-device-farm/internal/providers/mock"
)

type fakeClient struct {
	mu            sync.Mutex
	commands      []hostcommand.Command
	completions   []hostcommand.CompletionInput
	heartbeats    int
	lastHeartbeat hostcommand.HeartbeatInput
}

func (client *fakeClient) Heartbeat(_ context.Context, _ string, input hostcommand.HeartbeatInput) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.heartbeats++
	client.lastHeartbeat = input
	return nil
}

type fakeCapacityProbe struct{}

type fakeEnvironmentProbe struct {
	values map[string]any
	err    error
}

func (probe fakeEnvironmentProbe) Snapshot(context.Context) (map[string]any, error) {
	return probe.values, probe.err
}

func (fakeCapacityProbe) Snapshot(context.Context) (map[string]any, map[string]any, error) {
	return map[string]any{"resource_model": "dynamic_v1", "cpu_cores": 8, "memory_total_mb": 16000,
			"memory_available_mb": 9000, "disk_total_mb": 100000, "disk_available_mb": 30000},
		map[string]any{"kvm": true, "gpu_render": true}, nil
}
func (client *fakeClient) Claim(ctx context.Context, _ string, input hostcommand.ClaimInput) ([]hostcommand.Command, error) {
	client.mu.Lock()
	if len(client.commands) > 0 {
		count := input.MaxCommands
		if count > len(client.commands) {
			count = len(client.commands)
		}
		values := append([]hostcommand.Command(nil), client.commands[:count]...)
		client.commands = client.commands[count:]
		client.mu.Unlock()
		return values, nil
	}
	client.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}
func (*fakeClient) Extend(context.Context, string, hostcommand.LeaseExtensionInput) error { return nil }
func (client *fakeClient) Complete(_ context.Context, _ string, input hostcommand.CompletionInput) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.completions = append(client.completions, input)
	return nil
}

type trackingSleeper struct {
	mu      sync.Mutex
	active  int
	maximum int
	started chan struct{}
	release chan struct{}
}

func TestAgentRequiresExplicitProvider(t *testing.T) {
	_, err := agent.New(agent.Config{
		HostID: "host_000000000000001", HeartbeatInterval: time.Second,
		LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1,
		CommandTimeout: time.Second, ShutdownTimeout: time.Second,
	}, &fakeClient{}, providermock.New(providermock.Config{}), nil)
	if err == nil {
		t.Fatal("agent accepted an empty provider type")
	}
}

func TestAgentAcceptsLongRunningCommandsBecauseLeasesAreRenewed(t *testing.T) {
	_, err := agent.New(agent.Config{
		HostID: "host_000000000000001", ProviderType: "mock", HeartbeatInterval: time.Second,
		LeaseSeconds: 60, WaitSeconds: 1, Concurrency: 1,
		CommandTimeout: 60 * time.Second, ShutdownTimeout: time.Second,
	}, &fakeClient{}, providermock.New(providermock.Config{}), nil)
	if err != nil {
		t.Fatalf("agent rejected a renewable long-running command: %v", err)
	}
}

func TestAgentHeartbeatUsesMeasuredCapacityInsteadOfCommandConcurrency(t *testing.T) {
	client := &fakeClient{}
	runtime, err := agent.New(agent.Config{
		HostID: "host_000000000000001", ProviderType: "mock", HeartbeatInterval: 10 * time.Millisecond,
		LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1, CommandTimeout: time.Second, ShutdownTimeout: time.Second,
		Capacity: map[string]any{"device_slots": 1}, CapacityProbe: fakeCapacityProbe{},
	}, client, providermock.New(providermock.Config{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	time.Sleep(25 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.lastHeartbeat.Capacity["resource_model"] != "dynamic_v1" || client.lastHeartbeat.Capacity["device_slots"] != nil ||
		client.lastHeartbeat.Environment["gpu_render"] != true {
		t.Fatalf("heartbeat=%+v", client.lastHeartbeat)
	}
}

func TestAgentHeartbeatIncludesHostReadinessAndDoesNotChargeIOSAsAndroidEmulator(t *testing.T) {
	provider := providermock.New(providermock.Config{SharedAppiumEndpoint: "http://127.0.0.1:4723"})
	if _, err := provider.Create(context.Background(), providers.CreateRequest{DeviceID: "device_0000000000001", HostID: "host_000000000000001",
		Platform: providers.PlatformIOS, DeviceKind: "simulator", ProviderRef: "IOS-UDID-1", Serial: "IOS-UDID-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Start(context.Background(), "IOS-UDID-1"); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{}
	runtime, err := agent.New(agent.Config{HostID: "host_000000000000001", ProviderType: "appium_device_farm_ios",
		HeartbeatInterval: 10 * time.Millisecond, LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1,
		CommandTimeout: time.Second, ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 1},
		EnvironmentProbe: fakeEnvironmentProbe{values: map[string]any{"host_os": "macos", "host_arch": "arm64",
			"host_readiness": map[string]any{"ready": true, "reasons": []any{}}}},
	}, client, provider, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	time.Sleep(15 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.lastHeartbeat.Environment["host_os"] != "macos" || client.lastHeartbeat.Environment["host_arch"] != "arm64" ||
		len(client.lastHeartbeat.Devices) != 1 || len(client.lastHeartbeat.Devices[0].RuntimeProfile) != 0 ||
		client.lastHeartbeat.Devices[0].Platform != "ios" || client.lastHeartbeat.Devices[0].Connection["adb_endpoint"] != nil {
		t.Fatalf("heartbeat=%+v", client.lastHeartbeat)
	}
}

func TestAgentReportsMaintenanceReadinessWhenIOSInventoryFails(t *testing.T) {
	client := &fakeClient{}
	provider := providermock.New(providermock.Config{Scenario: providermock.Scenario{Offline: true}})
	runtime, err := agent.New(agent.Config{HostID: "host_000000000000001", ProviderType: "appium_device_farm_ios",
		HeartbeatInterval: 10 * time.Millisecond, LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1,
		CommandTimeout: time.Second, ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 1},
		EnvironmentProbe: fakeEnvironmentProbe{values: map[string]any{"host_os": "macos", "host_arch": "arm64",
			"host_readiness": map[string]any{"ready": true, "reasons": []any{}}}},
	}, client, provider, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	time.Sleep(15 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	readiness, _ := client.lastHeartbeat.Environment["host_readiness"].(map[string]any)
	if readiness["ready"] != false || len(client.lastHeartbeat.Devices) != 0 {
		t.Fatalf("heartbeat=%+v", client.lastHeartbeat)
	}
}

func (sleeper *trackingSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	sleeper.mu.Lock()
	sleeper.active++
	if sleeper.active > sleeper.maximum {
		sleeper.maximum = sleeper.active
	}
	sleeper.mu.Unlock()
	sleeper.started <- struct{}{}
	defer func() {
		sleeper.mu.Lock()
		sleeper.active--
		sleeper.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-sleeper.release:
	}
	return nil
}

func TestAgentStopsClaimingAndFinishesInflightCommands(t *testing.T) {
	sleeper := &trackingSleeper{started: make(chan struct{}, 3), release: make(chan struct{})}
	provider := providermock.New(providermock.Config{Sleeper: sleeper, Scenario: providermock.Scenario{
		Delays: map[providers.Operation]time.Duration{providers.OperationRestart: time.Second},
	}})
	commands := make([]hostcommand.Command, 0, 3)
	for index := 0; index < 3; index++ {
		ref := fmt.Sprintf("container-%d", index)
		if _, err := provider.Create(context.Background(), providers.CreateRequest{
			DeviceID: fmt.Sprintf("device_%019d", index), HostID: "host_000000000000001",
			ImageID: "image_00000000000001", ProviderRef: ref,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Start(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
		token := fmt.Sprintf("lease_token_%016d", index)
		commands = append(commands, hostcommand.Command{ID: fmt.Sprintf("command_%016d", index),
			CommandType: "restart", Payload: map[string]any{"provider_ref": ref}, LeaseToken: &token, Attempt: 1})
	}
	client := &fakeClient{commands: commands}
	runtime, err := agent.New(agent.Config{
		HostID: "host_000000000000001", ProviderType: "mock", HeartbeatInterval: 10 * time.Millisecond,
		LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 2,
		CommandTimeout: 10 * time.Second, ShutdownTimeout: 500 * time.Millisecond, Capacity: map[string]any{"device_slots": 2},
	}, client, provider, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	<-sleeper.started
	<-sleeper.started
	cancel()
	defer close(sleeper.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.completions) != 2 || len(client.commands) != 1 || client.heartbeats < 1 {
		t.Fatalf("completions=%d unclaimed=%d heartbeats=%d", len(client.completions), len(client.commands), client.heartbeats)
	}
	sleeper.mu.Lock()
	defer sleeper.mu.Unlock()
	if sleeper.maximum != 2 {
		t.Fatalf("maximum provider concurrency=%d", sleeper.maximum)
	}
}

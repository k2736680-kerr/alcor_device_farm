package mock

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

func TestMockProviderHappyLifecycleCreatesReadyDevice(t *testing.T) {
	provider := New(Config{BaseADBPort: 6000, BaseAppiumPort: 7000})
	first, err := provider.Create(context.Background(), testCreateRequest("device_0000000000001", "mock-device-1"))
	if err != nil {
		t.Fatal(err)
	}
	if first.State != providers.StateCreated || first.Ready() {
		t.Fatalf("created snapshot = %#v", first)
	}
	first.Capabilities["apiLevel"] = 1

	started, err := provider.Start(context.Background(), "mock-device-1")
	if err != nil {
		t.Fatal(err)
	}
	if !started.Ready() {
		t.Fatalf("started device is not ready: %#v", started)
	}
	health, err := provider.InspectHealth(context.Background(), "mock-device-1")
	if err != nil || !health.Ready() {
		t.Fatalf("health = %#v, error = %v", health, err)
	}
	connection, err := provider.GetConnectionInfo(context.Background(), "mock-device-1")
	if err != nil {
		t.Fatal(err)
	}
	if connection.Serial != "mock-mock-device-1" || connection.ADBEndpoint != "127.0.0.1:6000" ||
		connection.AppiumEndpoint != "http://127.0.0.1:7000" || connection.AppiumUDID != connection.Serial {
		t.Fatalf("connection = %#v", connection)
	}

	second, err := provider.Create(context.Background(), testCreateRequest("device_0000000000002", "mock-device-2"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Connection.ADBEndpoint == first.Connection.ADBEndpoint || second.Connection.AppiumEndpoint == first.Connection.AppiumEndpoint {
		t.Fatal("mock devices received duplicate ports")
	}
	discovered, err := provider.Discover(context.Background(), "host_000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 2 || discovered[0].Capabilities["apiLevel"] != 34 {
		t.Fatalf("discover = %#v", discovered)
	}

	rebuilt, err := provider.Rebuild(context.Background(), "mock-device-1")
	if err != nil || rebuilt.Generation != 2 || !rebuilt.Ready() {
		t.Fatalf("rebuilt = %#v, error = %v", rebuilt, err)
	}
	stopped, err := provider.Stop(context.Background(), "mock-device-1")
	if err != nil || stopped.State != providers.StateStopped || stopped.Ready() {
		t.Fatalf("stopped = %#v, error = %v", stopped, err)
	}
	restarted, err := provider.Restart(context.Background(), "mock-device-1")
	if err != nil || !restarted.Ready() {
		t.Fatalf("restarted = %#v, error = %v", restarted, err)
	}
	if err := provider.Delete(context.Background(), "mock-device-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.GetConnectionInfo(context.Background(), "mock-device-1"); providers.ErrorCode(err) != "PROVIDER_DEVICE_NOT_FOUND" {
		t.Fatalf("deleted device lookup error = %v", err)
	}
}

func TestMockProviderFailureScenariosAreDeterministic(t *testing.T) {
	tests := []struct {
		name      string
		scenario  Scenario
		operation func(*Provider) error
		wantCode  string
	}{
		{
			name: "create failure", scenario: Scenario{CreateFailure: true}, wantCode: "EMULATOR_CREATE_FAILED",
			operation: func(provider *Provider) error {
				_, err := provider.Create(context.Background(), testCreateRequest("device_0000000000001", "mock-device-1"))
				return err
			},
		},
		{
			name: "start failure", scenario: Scenario{StartFailure: true}, wantCode: "EMULATOR_START_FAILED",
			operation: func(provider *Provider) error {
				_, err := provider.Start(context.Background(), "mock-device-1")
				return err
			},
		},
		{
			name: "offline", scenario: Scenario{Offline: true}, wantCode: "HOST_OFFLINE",
			operation: func(provider *Provider) error {
				_, err := provider.Start(context.Background(), "mock-device-1")
				return err
			},
		},
		{
			name: "boot timeout", scenario: Scenario{BootTimeout: true}, wantCode: "DEVICE_BOOT_TIMEOUT",
			operation: func(provider *Provider) error {
				_, err := provider.Start(context.Background(), "mock-device-1")
				if err != nil {
					return err
				}
				_, err = provider.InspectHealth(context.Background(), "mock-device-1")
				return err
			},
		},
		{
			name: "Appium unhealthy", scenario: Scenario{AppiumUnhealthy: true}, wantCode: "APPIUM_UNHEALTHY",
			operation: func(provider *Provider) error {
				_, err := provider.Start(context.Background(), "mock-device-1")
				if err != nil {
					return err
				}
				_, err = provider.InspectHealth(context.Background(), "mock-device-1")
				return err
			},
		},
		{
			name: "delete failure", scenario: Scenario{DeleteFailure: true}, wantCode: "EMULATOR_DELETE_FAILED",
			operation: func(provider *Provider) error {
				return provider.Delete(context.Background(), "mock-device-1")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := New(Config{})
			if test.name != "create failure" {
				if _, err := provider.Create(context.Background(), testCreateRequest("device_0000000000001", "mock-device-1")); err != nil {
					t.Fatal(err)
				}
			}
			provider.SetScenario(test.scenario)
			first := test.operation(provider)
			second := test.operation(provider)
			if providers.ErrorCode(first) != test.wantCode || providers.ErrorCode(second) != test.wantCode {
				t.Fatalf("errors = %v / %v, want code %s", first, second, test.wantCode)
			}
		})
	}
}

func TestMockProviderDelayAndTimeoutInjection(t *testing.T) {
	sleeper := &recordingSleeper{}
	provider := New(Config{
		Sleeper:  sleeper,
		Scenario: Scenario{Delays: map[providers.Operation]time.Duration{providers.OperationCreate: 17 * time.Second}},
	})
	if _, err := provider.Create(context.Background(), testCreateRequest("device_0000000000001", "mock-device-1")); err != nil {
		t.Fatal(err)
	}
	if len(sleeper.durations) != 1 || sleeper.durations[0] != 17*time.Second {
		t.Fatalf("recorded delays = %v", sleeper.durations)
	}

	timeoutProvider := New(Config{
		Sleeper:  deadlineSleeper{},
		Scenario: Scenario{Delays: map[providers.Operation]time.Duration{providers.OperationCreate: time.Minute}},
	})
	_, err := timeoutProvider.Create(context.Background(), testCreateRequest("device_0000000000001", "mock-device-1"))
	if providers.ErrorCode(err) != "PROVIDER_OPERATION_TIMEOUT" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestMockProviderCreatesNoBackgroundGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	for index := 0; index < 100; index++ {
		provider := New(Config{})
		ref := fmt.Sprintf("mock-device-%d", index)
		if _, err := provider.Create(context.Background(), testCreateRequest(fmt.Sprintf("device_%016d", index), ref)); err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Start(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
		if err := provider.Delete(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
	}
	runtime.Gosched()
	after := runtime.NumGoroutine()
	if after > before {
		t.Fatalf("goroutines grew from %d to %d", before, after)
	}
}

func testCreateRequest(deviceID, providerRef string) providers.CreateRequest {
	return providers.CreateRequest{
		DeviceID: deviceID, HostID: "host_000000000000001", ImageID: "image_00000000000001",
		ProviderRef: providerRef, Capabilities: map[string]any{"apiLevel": 34, "platformName": "Android"},
	}
}

type recordingSleeper struct{ durations []time.Duration }

func (sleeper *recordingSleeper) Sleep(_ context.Context, duration time.Duration) error {
	sleeper.durations = append(sleeper.durations, duration)
	return nil
}

type deadlineSleeper struct{}

func (deadlineSleeper) Sleep(context.Context, time.Duration) error {
	return context.DeadlineExceeded
}

package agent

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	providermock "github.com/Ad-Quanta/alcor-device-farm/internal/providers/mock"
)

func TestAgentCreateCompletionReturnsProviderSnapshot(t *testing.T) {
	client := &completionClient{}
	runtime, err := New(Config{
		HostID: "host_000000000000001", ProviderType: "mock", HeartbeatInterval: time.Second,
		LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1, CommandTimeout: time.Second,
		ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 1},
	}, client, providermock.New(providermock.Config{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	token := "lease_token_000000000001"
	runtime.execute(hostcommand.Command{
		ID: "command_0000000000002", CommandType: "create", LeaseToken: &token, Attempt: 1,
		Payload: map[string]any{
			"device_id": "device_0000000000001", "image_id": "image_00000000000001",
			"provider_ref": "emulator-1", "capabilities": map[string]any{"apiLevel": float64(34)},
		},
	})
	if client.completion.Status != "succeeded" || client.completion.Error != nil {
		t.Fatalf("completion=%#v", client.completion)
	}
	if client.completion.Result["provider_ref"] != "emulator-1" || client.completion.Result["state"] != "created" ||
		client.completion.Result["generation"] != 1 {
		t.Fatalf("result=%#v", client.completion.Result)
	}
	connection, ok := client.completion.Result["connection"].(map[string]any)
	if !ok || connection["serial"] != "mock-emulator-1" || connection["adb_endpoint"] == "" {
		t.Fatalf("connection=%#v", client.completion.Result["connection"])
	}
}

func TestProviderHeartbeatStatusDoesNotMarkBootingDeviceReady(t *testing.T) {
	booting := providers.Snapshot{State: providers.StateRunning, Health: providers.Health{Online: true}}
	if providerLifecycle(booting) != "booting" || providerHealth(booting) != "unknown" {
		t.Fatalf("booting lifecycle=%s health=%s", providerLifecycle(booting), providerHealth(booting))
	}
	ready := providers.Snapshot{State: providers.StateRunning, Health: providers.Health{
		Online: true, ADBOnline: true, BootCompleted: true, AppiumHealthy: true,
	}}
	if providerLifecycle(ready) != "ready" || providerHealth(ready) != "healthy" {
		t.Fatalf("ready lifecycle=%s health=%s", providerLifecycle(ready), providerHealth(ready))
	}
}

func TestAgentCreatePassesCapabilitiesAndPreservesProviderRetryability(t *testing.T) {
	client := &completionClient{}
	provider := &createFailureProvider{}
	runtime, err := New(Config{
		HostID: "host_000000000000001", ProviderType: "docker", HeartbeatInterval: time.Second,
		LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1, CommandTimeout: time.Second,
		ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 1},
	}, client, provider, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	token := "lease_token_000000000001"
	runtime.execute(hostcommand.Command{
		ID: "command_0000000000001", CommandType: "create", LeaseToken: &token, Attempt: 2,
		Payload: map[string]any{
			"device_id": "device_0000000000001", "image_id": "image_00000000000001",
			"provider_ref": "emulator-1", "capabilities": map[string]any{"apiLevel": float64(34)},
		},
	})
	if provider.request.Capabilities["apiLevel"] != float64(34) {
		t.Fatalf("create request=%#v", provider.request)
	}
	if client.completion.Status != "failed" || client.completion.Error == nil ||
		client.completion.Error.Code != "IMAGE_NOT_VALIDATED" || client.completion.Error.Retryable {
		t.Fatalf("completion=%#v", client.completion)
	}
	if client.completion.LeaseToken != token || client.completion.Attempt != 2 {
		t.Fatalf("lease completion=%#v", client.completion)
	}
}

type completionClient struct{ completion hostcommand.CompletionInput }

func (*completionClient) Heartbeat(context.Context, string, hostcommand.HeartbeatInput) error {
	return nil
}
func (*completionClient) Claim(context.Context, string, hostcommand.ClaimInput) ([]hostcommand.Command, error) {
	return nil, nil
}
func (client *completionClient) Complete(_ context.Context, _ string, input hostcommand.CompletionInput) error {
	client.completion = input
	return nil
}

type createFailureProvider struct{ request providers.CreateRequest }

func (*createFailureProvider) Discover(context.Context, string) ([]providers.Snapshot, error) {
	return nil, nil
}
func (provider *createFailureProvider) Create(_ context.Context, request providers.CreateRequest) (providers.Snapshot, error) {
	provider.request = request
	return providers.Snapshot{}, &providers.Error{
		Operation: providers.OperationCreate, Code: "IMAGE_NOT_VALIDATED", Message: "image is not validated", Retryable: false,
	}
}
func (*createFailureProvider) Start(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, nil
}
func (*createFailureProvider) Stop(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, nil
}
func (*createFailureProvider) Restart(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, nil
}
func (*createFailureProvider) Rebuild(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, nil
}
func (*createFailureProvider) Delete(context.Context, string) error { return nil }
func (*createFailureProvider) InspectHealth(context.Context, string) (providers.Health, error) {
	return providers.Health{}, nil
}
func (*createFailureProvider) GetConnectionInfo(context.Context, string) (providers.ConnectionInfo, error) {
	return providers.ConnectionInfo{}, nil
}

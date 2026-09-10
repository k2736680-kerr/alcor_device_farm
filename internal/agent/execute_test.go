package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	providermock "github.com/Ad-Quanta/alcor-device-farm/internal/providers/mock"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
)

func TestAgentCreateCompletionReturnsProviderSnapshot(t *testing.T) {
	client := &completionClient{}
	registrar := &fakeRegistrar{}
	runtime, err := New(Config{
		HostID: "host_000000000000001", ProviderType: "mock", HeartbeatInterval: time.Second,
		LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1, CommandTimeout: time.Second,
		ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 1}, STFADBRegistrar: registrar,
	}, client, providermock.New(providermock.Config{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	token := "lease_token_000000000001"
	runtime.execute(context.Background(), hostcommand.Command{
		ID: "command_0000000000002", CommandType: "create", LeaseToken: &token, Attempt: 1,
		Payload: map[string]any{
			"device_id": "device_0000000000001", "image_id": "image_00000000000001",
			"provider_ref": "emulator-1", "capabilities": map[string]any{"apiLevel": float64(34)},
		},
	})
	if client.completion.Status != "succeeded" || client.completion.Error != nil {
		t.Fatalf("completion=%#v", client.completion)
	}
	if client.completion.Result["provider_ref"] != "emulator-1" || client.completion.Result["state"] != "running" ||
		client.completion.Result["generation"] != 1 {
		t.Fatalf("result=%#v", client.completion.Result)
	}
	connection, ok := client.completion.Result["connection"].(map[string]any)
	if !ok || connection["serial"] != "mock-emulator-1" || connection["adb_endpoint"] == "" || connection["appium_udid"] != "mock-emulator-1" {
		t.Fatalf("connection=%#v", client.completion.Result["connection"])
	}
	if registrar.calls != 1 || registrar.endpoint == "" {
		t.Fatalf("registrar=%+v", registrar)
	}
}

func TestAgentIOSCreateUsesCoreSimulatorUDIDReturnedByProvider(t *testing.T) {
	client := &completionClient{}
	provider := &dynamicIOSProvider{Provider: providermock.New(providermock.Config{})}
	runtime, err := New(Config{
		HostID: "ios_host_000000000001", ProviderType: "appium_device_farm_ios", HeartbeatInterval: time.Second,
		LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1, CommandTimeout: time.Second,
		ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 4},
	}, client, provider, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	token := "lease_token_000000000001"
	runtime.execute(context.Background(), hostcommand.Command{
		ID: "command_0000000000100", CommandType: "create", LeaseToken: &token, Attempt: 1,
		Payload: map[string]any{
			"device_id": "device_0000000000100", "platform": "ios", "device_kind": "simulator",
			"provider_ref": "pending:device_0000000000100", "capabilities": map[string]any{
				"runtimeId":    "com.apple.CoreSimulator.SimRuntime.iOS-26-3",
				"deviceTypeId": "com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro",
			},
		},
	})
	if provider.startedRef != provider.udid || provider.request.Platform != providers.PlatformIOS || provider.request.DeviceKind != "simulator" {
		t.Fatalf("provider request=%#v started_ref=%q udid=%q", provider.request, provider.startedRef, provider.udid)
	}
	if client.completion.Status != "succeeded" || client.completion.Result["provider_ref"] != provider.udid {
		t.Fatalf("completion=%#v", client.completion)
	}
	connection, ok := client.completion.Result["connection"].(map[string]any)
	if !ok || connection["provider_id"] != provider.udid || connection["serial"] != provider.udid {
		t.Fatalf("connection=%#v", client.completion.Result["connection"])
	}
}

type dynamicIOSProvider struct {
	providers.Provider
	request    providers.CreateRequest
	startedRef string
	udid       string
}

func (provider *dynamicIOSProvider) Create(_ context.Context, request providers.CreateRequest) (providers.Snapshot, error) {
	provider.request = request
	provider.udid = "11111111-2222-3333-4444-555555555555"
	return providers.Snapshot{DeviceID: request.DeviceID, HostID: request.HostID, Platform: providers.PlatformIOS,
		DeviceKind: "simulator", ProviderRef: provider.udid, State: providers.StateCreated, Generation: 1}, nil
}

func (provider *dynamicIOSProvider) Start(_ context.Context, providerRef string) (providers.Snapshot, error) {
	provider.startedRef = providerRef
	return providers.Snapshot{DeviceID: provider.request.DeviceID, HostID: provider.request.HostID, Platform: providers.PlatformIOS,
		DeviceKind: "simulator", ProviderRef: providerRef, State: providers.StateRunning, Generation: 1}, nil
}

func (provider *dynamicIOSProvider) InspectHealth(_ context.Context, providerRef string) (providers.Health, error) {
	if providerRef != provider.udid {
		return providers.Health{}, errors.New("使用了错误的 Simulator UDID")
	}
	return providers.Health{Platform: providers.PlatformIOS, Online: true, BootCompleted: true, AppiumHealthy: true,
		Components: map[string]providers.ProbeStatus{providers.ProbeTransport: providers.ProbePassed,
			providers.ProbeOSReady: providers.ProbePassed, providers.ProbeAutomation: providers.ProbePassed, providers.ProbeRouter: providers.ProbePassed}}, nil
}

func (provider *dynamicIOSProvider) GetConnectionInfo(_ context.Context, providerRef string) (providers.ConnectionInfo, error) {
	return providers.ConnectionInfo{Platform: providers.PlatformIOS, Serial: providerRef, DeviceUDID: providerRef, ProviderID: providerRef,
		AppiumEndpoint: "http://127.0.0.1:4723", AppiumUDID: providerRef}, nil
}

func TestLongImagePreparationRenewsCommandLease(t *testing.T) {
	client := &completionClient{}
	runtime, err := New(Config{
		HostID: "host_000000000000001", ProviderType: "mock", HeartbeatInterval: time.Second,
		LeaseSeconds: 5, WaitSeconds: 1, Concurrency: 1, CommandTimeout: time.Second, ImagePrepareTimeout: 3 * time.Second,
		ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 1}, ImagePreparer: slowImagePreparer{},
	}, client, providermock.New(providermock.Config{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	token := "lease_token_000000000001"
	runtime.execute(context.Background(), hostcommand.Command{ID: "command_0000000000099", CommandType: "prepare_android_image", LeaseToken: &token, Attempt: 1,
		Payload: map[string]any{"package_name": "system-images;android-36;google_apis;x86_64", "revision": "16"}})
	if client.extensions < 1 {
		t.Fatalf("lease extensions=%d, want at least one", client.extensions)
	}
	if client.completion.Status != "succeeded" {
		t.Fatalf("completion=%+v", client.completion)
	}
}

func TestReimageRollbackGetsFreshDeadlineAfterTargetTimeout(t *testing.T) {
	provider := &rollbackDeadlineProvider{}
	runtime := &Agent{config: Config{ProviderType: "mock", CommandTimeout: time.Second}, provider: provider}
	targetContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	profile := map[string]any{"container_cpu_cores": 4, "container_memory_mb": 5120, "guest_cpu_cores": 4,
		"guest_memory_mb": 4096, "data_disk_mb": 4096, "graphics": "host"}
	result, err := runtime.reimage(targetContext, map[string]any{
		"device_id": "device_0000000000001", "host_id": "host_000000000000001", "image_id": "target_image_0000001",
		"provider_ref": "emulator-1", "runtime_profile": profile, "capabilities": map[string]any{"apiLevel": 36},
		"rollback": map[string]any{"image_id": "previous_image_001", "runtime_profile": profile,
			"capabilities": map[string]any{"apiLevel": 35}},
	})
	if providers.ErrorCode(err) != "REIMAGE_TARGET_FAILED" || result["rollback_restored"] != true ||
		provider.currentImage != "previous_image_001" || provider.currentCapabilities["apiLevel"] != 35 {
		t.Fatalf("result=%#v error=%v current image=%q capabilities=%#v", result, err, provider.currentImage, provider.currentCapabilities)
	}
}

func TestRuntimeProfileUpdateRestoresPreviousProfileAfterTargetFailure(t *testing.T) {
	provider := &runtimeProfileUpdateProvider{}
	runtime := &Agent{config: Config{CommandTimeout: time.Second}, provider: provider}
	result, err := runtime.updateRuntimeProfile(context.Background(), runtimeProfileUpdatePayload())
	if providers.ErrorCode(err) != "RUNTIME_PROFILE_TARGET_FAILED" || result["rollback_restored"] != true || provider.calls != 2 {
		t.Fatalf("result=%#v error=%v calls=%d", result, err, provider.calls)
	}
	if provider.profiles[0].ContainerMemoryMB != 6144 || provider.profiles[1].ContainerMemoryMB != 5120 {
		t.Fatalf("profiles=%#v", provider.profiles)
	}
}

func TestRuntimeProfileUpdateUsesProviderStartedRollbackOnlyOnce(t *testing.T) {
	provider := &runtimeProfileUpdateProvider{rollbackAlreadyStarted: true}
	runtime := &Agent{config: Config{CommandTimeout: time.Second}, provider: provider}
	result, err := runtime.updateRuntimeProfile(context.Background(), runtimeProfileUpdatePayload())
	if providers.ErrorCode(err) != "RUNTIME_PROFILE_TARGET_FAILED" || result["rollback_restored"] != true || provider.calls != 1 {
		t.Fatalf("result=%#v error=%v calls=%d", result, err, provider.calls)
	}
}

func runtimeProfileUpdatePayload() map[string]any {
	target := runtimeprofile.Default()
	target.ContainerMemoryMB = 6144
	target.GuestMemoryMB = 5120
	previous := runtimeprofile.Default()
	return map[string]any{
		"provider_ref": "emulator-profile", "runtime_profile": target.Map(),
		"rollback": map[string]any{"runtime_profile": previous.Map()},
	}
}

type runtimeProfileUpdateProvider struct {
	providers.Provider
	calls                  int
	rollbackAlreadyStarted bool
	profiles               []runtimeprofile.Profile
}

func (provider *runtimeProfileUpdateProvider) RestartWithProfile(_ context.Context, providerRef string, profile runtimeprofile.Profile) (providers.Snapshot, error) {
	provider.calls++
	provider.profiles = append(provider.profiles, profile)
	snapshot := providers.Snapshot{DeviceID: "device-profile", HostID: "host-profile", Platform: providers.PlatformAndroid,
		ProviderRef: providerRef, State: providers.StateRunning, Generation: provider.calls + 1, RuntimeProfile: profile}
	if provider.calls == 1 {
		if provider.rollbackAlreadyStarted {
			return snapshot, &providers.Error{Operation: providers.OperationRestart, Code: "RUNTIME_PROFILE_TARGET_CREATE_FAILED_ROLLBACK_STARTED", Message: "old container restored"}
		}
		return providers.Snapshot{}, &providers.Error{Operation: providers.OperationRestart, Code: "EMULATOR_OPERATION_FAILED", Message: "target failed"}
	}
	return snapshot, nil
}

func (*runtimeProfileUpdateProvider) InspectHealth(context.Context, string) (providers.Health, error) {
	return providers.Health{Online: true, ADBOnline: true, BootCompleted: true, AppiumHealthy: true}, nil
}

func (*runtimeProfileUpdateProvider) GetConnectionInfo(context.Context, string) (providers.ConnectionInfo, error) {
	return providers.ConnectionInfo{Serial: "serial-profile", ADBEndpoint: "127.0.0.1:5555",
		AppiumEndpoint: "http://127.0.0.1:4723", AppiumUDID: "emulator-5554"}, nil
}

type rollbackDeadlineProvider struct {
	currentImage        string
	currentCapabilities map[string]any
}

func (provider *rollbackDeadlineProvider) Discover(context.Context, string) ([]providers.Snapshot, error) {
	return nil, nil
}
func (provider *rollbackDeadlineProvider) Create(_ context.Context, request providers.CreateRequest) (providers.Snapshot, error) {
	provider.currentImage, provider.currentCapabilities = request.ImageID, request.Capabilities
	return providers.Snapshot{DeviceID: request.DeviceID, HostID: request.HostID, ImageID: request.ImageID,
		ProviderRef: request.ProviderRef, State: providers.StateCreated, RuntimeProfile: request.RuntimeProfile}, nil
}
func (provider *rollbackDeadlineProvider) Start(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{DeviceID: "device_0000000000001", HostID: "host_000000000000001", ImageID: provider.currentImage,
		ProviderRef: "emulator-1", State: providers.StateRunning}, nil
}
func (*rollbackDeadlineProvider) Stop(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, nil
}
func (*rollbackDeadlineProvider) Restart(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, nil
}
func (*rollbackDeadlineProvider) Rebuild(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, nil
}
func (*rollbackDeadlineProvider) Delete(context.Context, string) error { return nil }
func (provider *rollbackDeadlineProvider) InspectHealth(context.Context, string) (providers.Health, error) {
	if provider.currentImage == "previous_image_001" {
		return providers.Health{Online: true, ADBOnline: true, BootCompleted: true, AppiumHealthy: true}, nil
	}
	return providers.Health{}, nil
}
func (provider *rollbackDeadlineProvider) GetConnectionInfo(context.Context, string) (providers.ConnectionInfo, error) {
	return providers.ConnectionInfo{Serial: "serial-1", ADBEndpoint: "127.0.0.1:5555",
		AppiumEndpoint: "http://127.0.0.1:4723", AppiumUDID: "emulator-5554"}, nil
}

func TestAgentKeepsReadyEmulatorWhenSTFRegistrationIsTemporarilyUnavailable(t *testing.T) {
	client := &completionClient{}
	provider := providermock.New(providermock.Config{})
	runtime, err := New(Config{
		HostID: "host_000000000000001", ProviderType: "mock", HeartbeatInterval: time.Second,
		LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1, CommandTimeout: time.Second,
		ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 1},
		STFADBRegistrar: &fakeRegistrar{err: errors.New("STF ADB unavailable")},
	}, client, provider, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	token := "lease_token_000000000001"
	runtime.execute(context.Background(), hostcommand.Command{
		ID: "command_000000000008", CommandType: "create", LeaseToken: &token, Attempt: 1,
		Payload: map[string]any{"device_id": "device_0000000000001", "image_id": "image_00000000000001",
			"provider_ref": "emulator-stf-retry"},
	})
	if client.completion.Status != "failed" || client.completion.Error == nil ||
		client.completion.Error.Code != "STF_ADB_CONNECT_FAILED" || !client.completion.Error.Retryable {
		t.Fatalf("completion=%#v", client.completion)
	}
	if _, err := provider.GetConnectionInfo(context.Background(), "emulator-stf-retry"); err != nil {
		t.Fatalf("ready emulator was deleted after registrar failure: %v", err)
	}
}

func TestProviderHeartbeatStatusDoesNotMarkBootingDeviceReady(t *testing.T) {
	booting := providers.Snapshot{State: providers.StateRunning, Health: providers.Health{Online: true}}
	if providerLifecycle(booting) != "booting" || providerHealth(booting) != "unhealthy" {
		t.Fatalf("booting lifecycle=%s health=%s", providerLifecycle(booting), providerHealth(booting))
	}
	ready := providers.Snapshot{State: providers.StateRunning, Health: providers.Health{
		Online: true, ADBOnline: true, BootCompleted: true, AppiumHealthy: true,
	}}
	if providerLifecycle(ready) != "ready" || providerHealth(ready) != "healthy" {
		t.Fatalf("ready lifecycle=%s health=%s", providerLifecycle(ready), providerHealth(ready))
	}
}

func TestHeartbeatUsesDomainProviderTypeForDocker(t *testing.T) {
	if got := heartbeatProviderType(" Docker "); got != "docker_emulator" {
		t.Fatalf("heartbeat provider type=%q", got)
	}
	if got := heartbeatProviderType("appium_device_farm_ios"); got != "appium_device_farm_ios" {
		t.Fatalf("iOS heartbeat provider type=%q", got)
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
	runtime.execute(context.Background(), hostcommand.Command{
		ID: "command_0000000000001", CommandType: "create", LeaseToken: &token, Attempt: 2,
		Payload: map[string]any{
			"device_id": "device_0000000000001", "image_id": "image_00000000000001",
			"provider_ref": "emulator-1", "docker_image": "registry.example/alcor/android-emulator:api34",
			"docker_digest": "sha256:" + strings.Repeat("a", 64), "capabilities": map[string]any{"apiLevel": float64(34)},
		},
	})
	if provider.request.Capabilities["apiLevel"] != float64(34) || provider.request.RuntimeImage != "registry.example/alcor/android-emulator:api34" {
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

func TestAgentValidatesDigestReadinessAndCleansTemporaryEmulator(t *testing.T) {
	client := &completionClient{}
	base := providermock.New(providermock.Config{})
	registrar := &fakeRegistrar{}
	provider := &digestVerifyingProvider{Provider: base, expectedImage: "registry.example/alcor/android-emulator:api34", expected: "sha256:" + strings.Repeat("a", 64)}
	runtime, err := New(Config{
		HostID: "host_000000000000001", ProviderType: "docker", HeartbeatInterval: time.Second,
		LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1, CommandTimeout: time.Second,
		ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 1}, STFADBRegistrar: registrar,
	}, client, provider, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	token := "lease_token_000000000001"
	runtime.execute(context.Background(), hostcommand.Command{ID: "command_0000000000003", CommandType: "validate_image", LeaseToken: &token, Attempt: 1,
		Payload: map[string]any{"device_id": "validation-device-01", "image_id": "image_00000000000001",
			"provider_ref": "validation-command-01", "docker_image": provider.expectedImage, "docker_digest": provider.expected,
			"capabilities": map[string]any{"apiLevel": float64(34)}}})
	if client.completion.Status != "succeeded" || client.completion.Result["digest_verified"] != true || client.completion.Result["ready"] != true ||
		client.completion.Result["stf_registered"] != true || registrar.calls != 1 || registrar.endpoint == "" {
		t.Fatalf("completion=%#v", client.completion)
	}
	values, err := base.Discover(context.Background(), "host_000000000000001")
	if err != nil || len(values) != 0 {
		t.Fatalf("temporary validation devices=%d error=%v", len(values), err)
	}
}

func TestAgentCreateWaitsForReadinessClassifiesFailureAndCleans(t *testing.T) {
	tests := []struct {
		name     string
		scenario providermock.Scenario
		wantCode string
	}{
		{name: "boot timeout", scenario: providermock.Scenario{BootTimeout: true}, wantCode: "DEVICE_BOOT_TIMEOUT"},
		{name: "Appium unhealthy", scenario: providermock.Scenario{AppiumUnhealthy: true}, wantCode: "APPIUM_UNHEALTHY"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &completionClient{}
			provider := providermock.New(providermock.Config{Scenario: test.scenario})
			runtime, err := New(Config{
				HostID: "host_000000000000001", ProviderType: "mock", HeartbeatInterval: time.Second,
				LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 1,
				CommandTimeout: 20 * time.Millisecond, ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 1},
			}, client, provider, nil)
			if err != nil {
				t.Fatal(err)
			}
			token := "lease_token_000000000001"
			runtime.execute(context.Background(), hostcommand.Command{
				ID: "command_0000000000001", CommandType: "create", LeaseToken: &token, Attempt: 1,
				Payload: map[string]any{"device_id": "device_0000000000001", "image_id": "image_00000000000001",
					"provider_ref": "emulator-readiness-test", "capabilities": map[string]any{"apiLevel": float64(34)}},
			})
			if client.completion.Status != "failed" || client.completion.Error == nil ||
				client.completion.Error.Code != test.wantCode || !client.completion.Error.Retryable {
				t.Fatalf("completion=%#v", client.completion)
			}
			if _, lookupErr := provider.GetConnectionInfo(context.Background(), "emulator-readiness-test"); providers.ErrorCode(lookupErr) != "PROVIDER_DEVICE_NOT_FOUND" {
				t.Fatalf("failed emulator was not cleaned: %v", lookupErr)
			}
		})
	}
}

type completionClient struct {
	completion hostcommand.CompletionInput
	extensions int
}

type slowImagePreparer struct{}

func (slowImagePreparer) SyncCatalog(context.Context) (map[string]any, error) {
	return map[string]any{"entries": []any{}}, nil
}
func (slowImagePreparer) Prepare(ctx context.Context, _, _ string) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
	}
	return map[string]any{"docker_image": "registry.example/android:api36", "docker_digest": "sha256:" + strings.Repeat("a", 64), "image_disk_mb": 8192}, nil
}

type fakeRegistrar struct {
	endpoint string
	calls    int
	err      error
}

func (registrar *fakeRegistrar) Register(_ context.Context, endpoint string) error {
	registrar.endpoint = endpoint
	registrar.calls++
	return registrar.err
}

func (*completionClient) Heartbeat(context.Context, string, hostcommand.HeartbeatInput) error {
	return nil
}
func (*completionClient) Claim(context.Context, string, hostcommand.ClaimInput) ([]hostcommand.Command, error) {
	return nil, nil
}
func (client *completionClient) Extend(context.Context, string, hostcommand.LeaseExtensionInput) error {
	client.extensions++
	return nil
}
func (client *completionClient) Complete(_ context.Context, _ string, input hostcommand.CompletionInput) error {
	client.completion = input
	return nil
}

type createFailureProvider struct{ request providers.CreateRequest }

type digestVerifyingProvider struct {
	providers.Provider
	expectedImage string
	expected      string
}

func (provider *digestVerifyingProvider) VerifyImageDigest(_ context.Context, runtimeImage, digest string) error {
	if runtimeImage != provider.expectedImage || digest != provider.expected {
		return &providers.Error{Operation: providers.OperationValidateImage, Code: "IMAGE_DIGEST_MISMATCH", Message: "digest mismatch"}
	}
	return nil
}

func (*createFailureProvider) Discover(context.Context, string) ([]providers.Snapshot, error) {
	return nil, nil
}
func (*createFailureProvider) VerifyImageDigest(context.Context, string, string) error { return nil }
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

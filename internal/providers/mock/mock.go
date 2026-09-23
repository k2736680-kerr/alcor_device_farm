package mock

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
)

type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}

type realSleeper struct{}

func (realSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type Scenario struct {
	Delays          map[providers.Operation]time.Duration
	CreateFailure   bool
	StartFailure    bool
	Offline         bool
	BootTimeout     bool
	AppiumUnhealthy bool
	DeleteFailure   bool
}

type Config struct {
	BaseADBPort          int
	BaseAppiumPort       int
	SharedAppiumEndpoint string
	Sleeper              Sleeper
	Scenario             Scenario
}

type Provider struct {
	mu                   sync.RWMutex
	devices              map[string]device
	nextPortOffset       int
	baseADBPort          int
	baseAppiumPort       int
	sharedAppiumEndpoint string
	sleeper              Sleeper
	scenario             Scenario
}

type device struct {
	request    providers.CreateRequest
	state      providers.State
	generation int
	connection providers.ConnectionInfo
}

func New(config Config) *Provider {
	if config.BaseADBPort == 0 {
		config.BaseADBPort = 5555
	}
	if config.BaseAppiumPort == 0 {
		config.BaseAppiumPort = 4723
	}
	if config.Sleeper == nil {
		config.Sleeper = realSleeper{}
	}
	return &Provider{
		devices:              make(map[string]device),
		baseADBPort:          config.BaseADBPort,
		baseAppiumPort:       config.BaseAppiumPort,
		sharedAppiumEndpoint: config.SharedAppiumEndpoint,
		sleeper:              config.Sleeper,
		scenario:             cloneScenario(config.Scenario),
	}
}

func (provider *Provider) SetScenario(scenario Scenario) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.scenario = cloneScenario(scenario)
}

func (provider *Provider) Discover(ctx context.Context, hostID string) ([]providers.Snapshot, error) {
	if err := provider.before(ctx, providers.OperationDiscover); err != nil {
		return nil, err
	}
	provider.mu.RLock()
	defer provider.mu.RUnlock()
	if provider.scenario.Offline {
		return nil, providerError(providers.OperationDiscover, "HOST_OFFLINE", "mock host is offline", true, nil)
	}
	result := make([]providers.Snapshot, 0, len(provider.devices))
	for _, value := range provider.devices {
		if value.request.HostID == hostID {
			result = append(result, provider.snapshotLocked(value))
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ProviderRef < result[right].ProviderRef })
	return result, nil
}

func (provider *Provider) Create(ctx context.Context, request providers.CreateRequest) (providers.Snapshot, error) {
	if err := provider.before(ctx, providers.OperationCreate); err != nil {
		return providers.Snapshot{}, err
	}
	if request.Platform == "" {
		request.Platform = providers.PlatformAndroid
	}
	if request.DeviceKind == "" {
		request.DeviceKind = "emulator"
		if request.Platform == providers.PlatformIOS {
			request.DeviceKind = "simulator"
		}
	}
	if request.DeviceID == "" || request.HostID == "" || request.ProviderRef == "" ||
		(request.Platform == providers.PlatformAndroid && request.ImageID == "") {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "INVALID_ARGUMENT", "device, host, provider ref and Android image are required", false, nil)
	}
	if request.Platform != providers.PlatformAndroid && request.Platform != providers.PlatformIOS {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "INVALID_ARGUMENT", "unsupported device platform", false, nil)
	}
	if request.Platform == providers.PlatformAndroid && request.RuntimeProfile == (runtimeprofile.Profile{}) {
		request.RuntimeProfile = runtimeprofile.Default()
	}
	if request.Platform == providers.PlatformAndroid {
		if err := request.RuntimeProfile.Validate(); err != nil {
			return providers.Snapshot{}, providerError(providers.OperationCreate, "INVALID_ARGUMENT", err.Error(), false, err)
		}
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.scenario.CreateFailure {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "EMULATOR_CREATE_FAILED", "mock create failure", true, nil)
	}
	if _, exists := provider.devices[request.ProviderRef]; exists {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "PROVIDER_REF_CONFLICT", "provider ref already exists", false, nil)
	}
	if request.Serial == "" {
		request.Serial = "mock-" + request.ProviderRef
	}
	offset := provider.nextPortOffset
	provider.nextPortOffset++
	adbEndpoint := fmt.Sprintf("127.0.0.1:%d", provider.baseADBPort+offset*2)
	appiumEndpoint := fmt.Sprintf("http://127.0.0.1:%d", provider.baseAppiumPort+offset)
	if request.Platform == providers.PlatformIOS {
		adbEndpoint = ""
		if provider.sharedAppiumEndpoint != "" {
			appiumEndpoint = provider.sharedAppiumEndpoint
		}
	}
	value := device{
		request:    request,
		state:      providers.StateCreated,
		generation: 1,
		connection: providers.ConnectionInfo{
			Platform:       request.Platform,
			Serial:         request.Serial,
			DeviceUDID:     request.Serial,
			ProviderID:     request.ProviderRef,
			ADBEndpoint:    adbEndpoint,
			AppiumEndpoint: appiumEndpoint,
			AppiumUDID:     request.Serial,
		},
	}
	provider.devices[request.ProviderRef] = value
	return provider.snapshotLocked(value), nil
}

func (provider *Provider) Start(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	if err := provider.before(ctx, providers.OperationStart); err != nil {
		return providers.Snapshot{}, err
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.scenario.Offline {
		return providers.Snapshot{}, providerError(providers.OperationStart, "HOST_OFFLINE", "mock host is offline", true, nil)
	}
	if provider.scenario.StartFailure {
		return providers.Snapshot{}, providerError(providers.OperationStart, "EMULATOR_START_FAILED", "mock start failure", true, nil)
	}
	value, err := provider.deviceLocked(providers.OperationStart, providerRef)
	if err != nil {
		return providers.Snapshot{}, err
	}
	value.state = providers.StateRunning
	provider.devices[providerRef] = value
	return provider.snapshotLocked(value), nil
}

func (provider *Provider) Stop(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	return provider.setState(ctx, providers.OperationStop, providerRef, providers.StateStopped, false)
}

func (provider *Provider) Restart(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	return provider.setState(ctx, providers.OperationRestart, providerRef, providers.StateRunning, false)
}

func (provider *Provider) Rebuild(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	return provider.setState(ctx, providers.OperationRebuild, providerRef, providers.StateRunning, true)
}

func (provider *Provider) Delete(ctx context.Context, providerRef string) error {
	if err := provider.before(ctx, providers.OperationDelete); err != nil {
		return err
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.scenario.DeleteFailure {
		return providerError(providers.OperationDelete, "EMULATOR_DELETE_FAILED", "mock delete failure", true, nil)
	}
	if _, exists := provider.devices[providerRef]; !exists {
		return nil
	}
	delete(provider.devices, providerRef)
	return nil
}

func (provider *Provider) InspectHealth(ctx context.Context, providerRef string) (providers.Health, error) {
	if err := provider.before(ctx, providers.OperationInspectHealth); err != nil {
		return providers.Health{}, err
	}
	provider.mu.RLock()
	defer provider.mu.RUnlock()
	value, err := provider.deviceLocked(providers.OperationInspectHealth, providerRef)
	if err != nil {
		return providers.Health{}, err
	}
	if provider.scenario.Offline {
		return providers.Health{}, providerError(providers.OperationInspectHealth, "DEVICE_OFFLINE", "mock device is offline", true, nil)
	}
	if value.state != providers.StateRunning {
		return providers.Health{}, providerError(providers.OperationInspectHealth, "DEVICE_NOT_RUNNING", "mock device is not running", true, nil)
	}
	if value.request.Platform == providers.PlatformIOS {
		return provider.iosHealthLocked(), provider.iosHealthErrorLocked()
	}
	if provider.scenario.BootTimeout {
		return providers.Health{Online: true, ADBOnline: true}, providerError(
			providers.OperationInspectHealth, "DEVICE_BOOT_TIMEOUT", "mock boot did not complete", true, nil,
		)
	}
	if provider.scenario.AppiumUnhealthy {
		return providers.Health{Online: true, ADBOnline: true, BootCompleted: true}, providerError(
			providers.OperationInspectHealth, "APPIUM_UNHEALTHY", "mock Appium endpoint is unhealthy", true, nil,
		)
	}
	return providers.Health{Online: true, ADBOnline: true, BootCompleted: true, AppiumHealthy: true}, nil
}

func (provider *Provider) GetConnectionInfo(ctx context.Context, providerRef string) (providers.ConnectionInfo, error) {
	if err := provider.before(ctx, providers.OperationConnectionInfo); err != nil {
		return providers.ConnectionInfo{}, err
	}
	provider.mu.RLock()
	defer provider.mu.RUnlock()
	value, err := provider.deviceLocked(providers.OperationConnectionInfo, providerRef)
	if err != nil {
		return providers.ConnectionInfo{}, err
	}
	return value.connection, nil
}

func (provider *Provider) setState(
	ctx context.Context,
	operation providers.Operation,
	providerRef string,
	state providers.State,
	incrementGeneration bool,
) (providers.Snapshot, error) {
	if err := provider.before(ctx, operation); err != nil {
		return providers.Snapshot{}, err
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.scenario.Offline {
		return providers.Snapshot{}, providerError(operation, "HOST_OFFLINE", "mock host is offline", true, nil)
	}
	value, err := provider.deviceLocked(operation, providerRef)
	if err != nil {
		return providers.Snapshot{}, err
	}
	value.state = state
	if incrementGeneration {
		value.generation++
	}
	provider.devices[providerRef] = value
	return provider.snapshotLocked(value), nil
}

func (provider *Provider) before(ctx context.Context, operation providers.Operation) error {
	provider.mu.RLock()
	delay := provider.scenario.Delays[operation]
	provider.mu.RUnlock()
	if err := provider.sleeper.Sleep(ctx, delay); err != nil {
		code := "PROVIDER_OPERATION_CANCELED"
		if errors.Is(err, context.DeadlineExceeded) {
			code = "PROVIDER_OPERATION_TIMEOUT"
		}
		return providerError(operation, code, "mock operation did not complete", true, err)
	}
	return nil
}

func (provider *Provider) deviceLocked(operation providers.Operation, providerRef string) (device, error) {
	value, exists := provider.devices[providerRef]
	if !exists {
		return device{}, providerError(operation, "PROVIDER_DEVICE_NOT_FOUND", "mock device not found", false, nil)
	}
	return value, nil
}

func (provider *Provider) snapshotLocked(value device) providers.Snapshot {
	health := providers.Health{Platform: value.request.Platform}
	if value.state == providers.StateRunning && !provider.scenario.Offline {
		if value.request.Platform == providers.PlatformIOS {
			health, _ = provider.iosHealthLocked(), provider.iosHealthErrorLocked()
		} else {
			health.Online = true
			health.ADBOnline = true
			health.BootCompleted = !provider.scenario.BootTimeout
			health.AppiumHealthy = health.BootCompleted && !provider.scenario.AppiumUnhealthy
		}
	}
	return providers.Snapshot{
		DeviceID: value.request.DeviceID, HostID: value.request.HostID, ImageID: value.request.ImageID,
		Platform:    value.request.Platform,
		ProviderRef: value.request.ProviderRef, State: value.state, Generation: value.generation,
		Capabilities: cloneMap(value.request.Capabilities), RuntimeProfile: value.request.RuntimeProfile,
		Health: health, Connection: value.connection,
	}
}

func (provider *Provider) iosHealthLocked() providers.Health {
	components := map[string]providers.ProbeStatus{
		providers.ProbeTransport:     providers.ProbePassed,
		providers.ProbeOSReady:       providers.ProbePassed,
		providers.ProbeAutomation:    providers.ProbePassed,
		providers.ProbeRouter:        providers.ProbePassed,
		providers.ProbeRemoteControl: providers.ProbeUnsupported,
	}
	if provider.scenario.BootTimeout {
		components[providers.ProbeOSReady] = providers.ProbeFailed
	}
	if provider.scenario.AppiumUnhealthy {
		components[providers.ProbeRouter] = providers.ProbeFailed
	}
	return providers.Health{Platform: providers.PlatformIOS, Components: components, Online: true,
		BootCompleted: components[providers.ProbeOSReady] == providers.ProbePassed,
		AppiumHealthy: components[providers.ProbeRouter] == providers.ProbePassed}
}

func (provider *Provider) iosHealthErrorLocked() error {
	if provider.scenario.BootTimeout {
		return providerError(providers.OperationInspectHealth, "DEVICE_BOOT_TIMEOUT", "mock iOS device did not become ready", true, nil)
	}
	if provider.scenario.AppiumUnhealthy {
		return providerError(providers.OperationInspectHealth, "APPIUM_UNHEALTHY", "mock Appium Device Farm endpoint is unhealthy", true, nil)
	}
	return nil
}

func providerError(operation providers.Operation, code, message string, retryable bool, cause error) error {
	return &providers.Error{Operation: operation, Code: code, Message: message, Retryable: retryable, Cause: cause}
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneScenario(source Scenario) Scenario {
	result := source
	if source.Delays != nil {
		result.Delays = make(map[providers.Operation]time.Duration, len(source.Delays))
		for operation, delay := range source.Delays {
			result.Delays[operation] = delay
		}
	}
	return result
}

var _ providers.Provider = (*Provider)(nil)

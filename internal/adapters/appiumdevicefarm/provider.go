package appiumdevicefarm

import (
	"context"
	"strings"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

func (client *Client) Discover(ctx context.Context, hostID string) ([]providers.Snapshot, error) {
	devices, err := client.Inventory(ctx)
	if err != nil {
		return nil, providerError(providers.OperationDiscover, "DEVICE_FARM_INVENTORY_FAILED", "cannot read local iOS inventory", true, err)
	}
	health, healthErr := client.Health(ctx)
	result := make([]providers.Snapshot, 0, len(devices))
	for _, device := range devices {
		result = append(result, client.snapshot(hostID, device, health, healthErr))
	}
	return result, nil
}

func (client *Client) Create(context.Context, providers.CreateRequest) (providers.Snapshot, error) {
	return providers.Snapshot{}, unsupported(providers.OperationCreate)
}
func (client *Client) Start(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, unsupported(providers.OperationStart)
}
func (client *Client) Stop(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, unsupported(providers.OperationStop)
}
func (client *Client) Restart(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, unsupported(providers.OperationRestart)
}
func (client *Client) Rebuild(context.Context, string) (providers.Snapshot, error) {
	return providers.Snapshot{}, unsupported(providers.OperationRebuild)
}
func (client *Client) Delete(context.Context, string) error {
	return unsupported(providers.OperationDelete)
}

func (client *Client) InspectHealth(ctx context.Context, providerRef string) (providers.Health, error) {
	device, node, nodeErr, err := client.find(ctx, providerRef)
	if err != nil {
		return providers.Health{}, err
	}
	snapshot := client.snapshot("", device, node, nodeErr)
	if !snapshot.Health.Ready() {
		return snapshot.Health, providerError(providers.OperationInspectHealth, "IOS_DEVICE_NOT_READY", "iOS device or local Appium node is not ready", true, nodeErr)
	}
	return snapshot.Health, nil
}

func (client *Client) GetConnectionInfo(ctx context.Context, providerRef string) (providers.ConnectionInfo, error) {
	device, _, _, err := client.find(ctx, providerRef)
	if err != nil {
		return providers.ConnectionInfo{}, err
	}
	if !device.Allowed {
		return providers.ConnectionInfo{}, providerError(providers.OperationConnectionInfo, "IOS_DEVICE_NOT_ALLOWED", "iOS device is not in the Host allowlist", false, nil)
	}
	return providers.ConnectionInfo{
		Platform: providers.PlatformIOS, Serial: device.UDID, DeviceUDID: device.UDID,
		ProviderID: device.UDID, AppiumEndpoint: client.endpoint.String(), AppiumUDID: device.UDID,
	}, nil
}

func (client *Client) find(ctx context.Context, providerRef string) (Device, NodeHealth, error, error) {
	devices, err := client.Inventory(ctx)
	if err != nil {
		return Device{}, NodeHealth{}, nil, providerError(providers.OperationInspectHealth, "DEVICE_FARM_INVENTORY_FAILED", "cannot read local iOS inventory", true, err)
	}
	for _, device := range devices {
		if device.UDID == strings.TrimSpace(providerRef) {
			node, nodeErr := client.Health(ctx)
			return device, node, nodeErr, nil
		}
	}
	return Device{}, NodeHealth{}, nil, providerError(providers.OperationInspectHealth, "PROVIDER_DEVICE_NOT_FOUND", "iOS device is absent from local inventory", true, nil)
}

func (client *Client) snapshot(hostID string, device Device, node NodeHealth, nodeErr error) providers.Snapshot {
	transport := providers.ProbePassed
	if device.Offline {
		transport = providers.ProbeFailed
	}
	osReady := providers.ProbePassed
	if (!device.RealDevice && !strings.EqualFold(device.State, "Booted")) || (device.RealDevice && device.Offline) {
		osReady = providers.ProbeFailed
	}
	automation := providers.ProbePassed
	if nodeErr != nil || !node.AppiumReady {
		automation = providers.ProbeFailed
	}
	router := providers.ProbePassed
	if nodeErr != nil || !node.PluginReady || device.UserBlocked {
		router = providers.ProbeFailed
	}
	if !device.Allowed {
		transport, osReady, automation, router = providers.ProbeUnknown, providers.ProbeUnknown, providers.ProbeUnknown, providers.ProbeUnknown
	}
	components := map[string]providers.ProbeStatus{
		providers.ProbeTransport: transport, providers.ProbeOSReady: osReady,
		providers.ProbeAutomation: automation, providers.ProbeRouter: router,
		providers.ProbeRemoteControl: providers.ProbeUnsupported,
	}
	state := providers.StateRunning
	if !device.Allowed {
		state = providers.StateStopped
	}
	capabilities := map[string]any{
		"platformName": "iOS", "platformVersion": device.PlatformVersion, "automationName": "XCUITest",
		"deviceClass": "phone", "realDevice": device.RealDevice, "model": device.ProductModel,
		"deviceName": device.Name, "providerBusy": device.Busy, "providerBlocked": device.UserBlocked,
		"providerState": device.State, "allowlisted": device.Allowed,
	}
	return providers.Snapshot{
		HostID: hostID, Platform: providers.PlatformIOS, DeviceKind: device.DeviceType,
		ProviderRef: device.UDID, State: state, Capabilities: capabilities,
		Health: providers.Health{Platform: providers.PlatformIOS, Components: components, Online: transport == providers.ProbePassed,
			BootCompleted: osReady == providers.ProbePassed, AppiumHealthy: automation == providers.ProbePassed && router == providers.ProbePassed},
		Connection: providers.ConnectionInfo{Platform: providers.PlatformIOS, Serial: device.UDID, DeviceUDID: device.UDID,
			ProviderID: device.UDID, AppiumEndpoint: client.endpoint.String(), AppiumUDID: device.UDID},
	}
}

func unsupported(operation providers.Operation) error {
	return providerError(operation, "IOS_FIXED_INVENTORY_OPERATION_UNSUPPORTED", "DF-041 only supports read-only iOS inventory and health", false, nil)
}

func providerError(operation providers.Operation, code, message string, retryable bool, cause error) error {
	return &providers.Error{Operation: operation, Code: code, Message: message, Retryable: retryable, Cause: cause}
}

var _ providers.Provider = (*Client)(nil)

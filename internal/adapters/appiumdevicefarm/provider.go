package appiumdevicefarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

func (client *Client) Discover(ctx context.Context, hostID string) ([]providers.Snapshot, error) {
	devices, err := client.devices(ctx)
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
func (client *Client) Start(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	device, err := client.allowedSimulator(ctx, providerRef, providers.OperationStart)
	if err != nil {
		return providers.Snapshot{}, err
	}
	if device.Busy {
		return providers.Snapshot{}, providerError(providers.OperationStart, "IOS_SIMULATOR_BUSY", "Simulator 当前仍被 Appium 会话占用", true, nil)
	}
	if !strings.EqualFold(device.State, "Booted") {
		if _, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "boot", device.UDID); err != nil {
			return providers.Snapshot{}, client.lifecycleError(providers.OperationStart, "IOS_SIMULATOR_BOOT_FAILED", "IOS_SIMULATOR_BOOT_TIMEOUT", "启动 Simulator 失败", ctx, err)
		}
	}
	if _, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "bootstatus", device.UDID, "-b"); err != nil {
		return providers.Snapshot{}, client.lifecycleError(providers.OperationStart, "IOS_SIMULATOR_BOOT_FAILED", "IOS_SIMULATOR_BOOT_TIMEOUT", "等待 Simulator 启动完成失败", ctx, err)
	}
	return client.waitSimulator(ctx, device.UDID, providers.OperationStart, func(snapshot providers.Snapshot) bool { return snapshot.Ready() })
}
func (client *Client) Stop(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	device, err := client.allowedSimulator(ctx, providerRef, providers.OperationStop)
	if err != nil {
		return providers.Snapshot{}, err
	}
	if device.Busy {
		return providers.Snapshot{}, providerError(providers.OperationStop, "IOS_SIMULATOR_BUSY", "Simulator 当前仍被 Appium 会话占用", true, nil)
	}
	if !strings.EqualFold(device.State, "Shutdown") {
		if _, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "shutdown", device.UDID); err != nil {
			return providers.Snapshot{}, client.lifecycleError(providers.OperationStop, "IOS_SIMULATOR_SHUTDOWN_FAILED", "IOS_SIMULATOR_SHUTDOWN_TIMEOUT", "停止 Simulator 失败", ctx, err)
		}
	}
	return client.waitSimulator(ctx, device.UDID, providers.OperationStop, func(snapshot providers.Snapshot) bool {
		return snapshot.State == providers.StateStopped
	})
}
func (client *Client) Restart(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	if _, err := client.Stop(ctx, providerRef); err != nil {
		return providers.Snapshot{}, err
	}
	return client.Start(ctx, providerRef)
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

func (client *Client) allowedSimulator(ctx context.Context, providerRef string, operation providers.Operation) (Device, error) {
	providerRef = strings.TrimSpace(providerRef)
	devices, err := client.devices(ctx)
	if err != nil {
		return Device{}, providerError(operation, "DEVICE_FARM_INVENTORY_FAILED", "无法读取本机 iOS 设备清单", true, err)
	}
	for _, device := range devices {
		if device.UDID != providerRef {
			continue
		}
		if !device.Allowed {
			return Device{}, providerError(operation, "IOS_DEVICE_NOT_ALLOWED", "该 iOS 设备不在宿主机 allowlist 中", false, nil)
		}
		if device.RealDevice || device.DeviceType != "simulator" {
			return Device{}, providerError(operation, "IOS_SIMULATOR_OPERATION_REQUIRED", "该操作只允许用于固定库存 Simulator", false, nil)
		}
		return device, nil
	}
	return Device{}, providerError(operation, "PROVIDER_DEVICE_NOT_FOUND", "本机设备清单中不存在该 iOS 设备", true, nil)
}

func (client *Client) waitSimulator(ctx context.Context, providerRef string, operation providers.Operation, ready func(providers.Snapshot) bool) (providers.Snapshot, error) {
	ticker := time.NewTicker(client.lifecyclePollInterval)
	defer ticker.Stop()
	for {
		device, node, nodeErr, err := client.find(ctx, providerRef)
		if err == nil {
			snapshot := client.snapshot("", device, node, nodeErr)
			if ready(snapshot) {
				return snapshot, nil
			}
		}
		select {
		case <-ctx.Done():
			code, message := "IOS_SIMULATOR_LIFECYCLE_TIMEOUT", "等待 Simulator 状态收敛超时"
			if operation == providers.OperationStart {
				code, message = "IOS_SIMULATOR_BOOT_TIMEOUT", "等待 Simulator 启动就绪超时"
			} else if operation == providers.OperationStop {
				code, message = "IOS_SIMULATOR_SHUTDOWN_TIMEOUT", "等待 Simulator 停止超时"
			}
			return providers.Snapshot{}, providerError(operation, code, message, true, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (client *Client) lifecycleError(operation providers.Operation, failureCode, timeoutCode, message string, ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return providerError(operation, timeoutCode, message+"：命令执行超时", true, err)
	}
	return providerError(operation, failureCode, message, true, fmt.Errorf("simctl：%w", err))
}

func (client *Client) find(ctx context.Context, providerRef string) (Device, NodeHealth, error, error) {
	devices, err := client.devices(ctx)
	if err != nil {
		return Device{}, NodeHealth{}, nil, providerError(providers.OperationInspectHealth, "DEVICE_FARM_INVENTORY_FAILED", "cannot read local iOS inventory", true, err)
	}
	for _, device := range devices {
		if device.UDID == strings.TrimSpace(providerRef) {
			node, nodeErr := client.Health(ctx)
			return device, node, nodeErr, nil
		}
	}
	return Device{}, NodeHealth{}, nil, providerError(providers.OperationInspectHealth, "PROVIDER_DEVICE_NOT_FOUND", "本机设备清单中不存在该 iOS 设备", true, nil)
}

func (client *Client) devices(ctx context.Context) ([]Device, error) {
	devices, err := client.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	for index := range devices {
		device := &devices[index]
		if !device.Allowed || device.RealDevice || device.DeviceType != "simulator" {
			continue
		}
		state, err := client.simulatorState(ctx, device.UDID)
		if err != nil {
			return nil, err
		}
		device.State = state
	}
	return devices, nil
}

func (client *Client) simulatorState(ctx context.Context, udid string) (string, error) {
	if _, allowed := client.allowUDIDs[udid]; !allowed {
		return "", providerError(providers.OperationDiscover, "IOS_DEVICE_NOT_ALLOWED", "该 iOS 设备不在宿主机 allowlist 中", false, nil)
	}
	output, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "list", "devices", udid, "-j")
	if err != nil {
		return "", providerError(providers.OperationDiscover, "IOS_SIMULATOR_STATE_QUERY_FAILED", "读取 Simulator 实时状态失败", true, err)
	}
	var payload struct {
		Devices map[string][]struct {
			UDID  string `json:"udid"`
			State string `json:"state"`
		} `json:"devices"`
	}
	if len(output) == 0 || len(output) > maxResponseBytes || json.Unmarshal(output, &payload) != nil {
		return "", providerError(providers.OperationDiscover, "IOS_SIMULATOR_STATE_QUERY_FAILED", "Simulator 实时状态响应无效", true, nil)
	}
	state := ""
	for _, runtimeDevices := range payload.Devices {
		for _, device := range runtimeDevices {
			if strings.TrimSpace(device.UDID) != udid {
				continue
			}
			if state != "" {
				return "", providerError(providers.OperationDiscover, "IOS_SIMULATOR_STATE_CONFLICT", "Simulator 实时状态存在重复 UDID", false, nil)
			}
			state = strings.TrimSpace(device.State)
		}
	}
	if state == "" {
		return "", providerError(providers.OperationDiscover, "PROVIDER_DEVICE_NOT_FOUND", "simctl 中不存在该 allowlist Simulator", true, nil)
	}
	return state, nil
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
	if !device.Allowed || (!device.RealDevice && !strings.EqualFold(device.State, "Booted")) {
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
		ProviderRef: device.UDID, State: state, Generation: 1, Capabilities: capabilities,
		Health: providers.Health{Platform: providers.PlatformIOS, Components: components, Online: transport == providers.ProbePassed,
			BootCompleted: osReady == providers.ProbePassed, AppiumHealthy: automation == providers.ProbePassed && router == providers.ProbePassed},
		Connection: providers.ConnectionInfo{Platform: providers.PlatformIOS, Serial: device.UDID, DeviceUDID: device.UDID,
			ProviderID: device.UDID, AppiumEndpoint: client.endpoint.String(), AppiumUDID: device.UDID},
	}
}

func unsupported(operation providers.Operation) error {
	return providerError(operation, "IOS_FIXED_INVENTORY_OPERATION_UNSUPPORTED", "固定 iOS 库存不支持该操作", false, nil)
}

func providerError(operation providers.Operation, code, message string, retryable bool, cause error) error {
	return &providers.Error{Operation: operation, Code: code, Message: message, Retryable: retryable, Cause: cause}
}

var _ providers.Provider = (*Client)(nil)

package appiumdevicefarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

func (client *Client) Discover(ctx context.Context, hostID string) ([]providers.Snapshot, error) {
	devices, err := client.devices(ctx)
	if err != nil {
		return nil, providerError(providers.OperationDiscover, "DEVICE_FARM_INVENTORY_FAILED", "无法读取本机 iOS 设备清单", true, err)
	}
	health, healthErr := client.Health(ctx)
	result := make([]providers.Snapshot, 0, len(devices))
	for _, device := range devices {
		result = append(result, client.snapshot(hostID, device, health, healthErr))
	}
	return result, nil
}

func capabilityString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func (client *Client) Create(ctx context.Context, request providers.CreateRequest) (providers.Snapshot, error) {
	if request.Platform != providers.PlatformIOS || request.DeviceKind != "simulator" || strings.TrimSpace(request.DeviceID) == "" {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "INVALID_ARGUMENT", "动态 iOS 设备必须是带 Device ID 的 Simulator", false, nil)
	}
	runtimeID := capabilityString(request.Capabilities, "runtimeId")
	deviceTypeID := capabilityString(request.Capabilities, "deviceTypeId")
	if _, allowed := client.allowedRuntimeIDs[runtimeID]; !allowed {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "IOS_RUNTIME_NOT_ALLOWED", "所选 iOS Runtime 不在宿主机受控目录中", false, nil)
	}
	if _, allowed := client.allowedDeviceTypeIDs[deviceTypeID]; !allowed {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "IOS_DEVICE_TYPE_NOT_ALLOWED", "所选 iPhone 机型不在宿主机受控目录中", false, nil)
	}
	catalog, err := client.SimulatorCatalog(ctx)
	if err != nil {
		return providers.Snapshot{}, err
	}
	if !catalog.hasRuntime(runtimeID) || !catalog.hasDeviceType(deviceTypeID) {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "IOS_SIMULATOR_CATALOG_STALE", "所选 Runtime 或 iPhone 机型在宿主机上不可用，请刷新目录", true, nil)
	}
	name := client.managedNamePrefix + strings.TrimSpace(request.DeviceID)
	if existing, found, lookupErr := client.managedSimulatorByName(ctx, name); lookupErr != nil {
		return providers.Snapshot{}, lookupErr
	} else if found {
		if existing.RuntimeID != runtimeID || existing.DeviceTypeID != deviceTypeID {
			return providers.Snapshot{}, providerError(providers.OperationCreate, "IOS_SIMULATOR_IDEMPOTENCY_CONFLICT", "同一 Device ID 已对应不同的 Runtime 或机型", false, nil)
		}
		node, nodeErr := client.Health(ctx)
		return client.snapshot(request.HostID, existing, node, nodeErr), nil
	}
	output, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "create", name, deviceTypeID, runtimeID)
	if err != nil {
		return providers.Snapshot{}, client.lifecycleError(providers.OperationCreate, "IOS_SIMULATOR_CREATE_FAILED", "IOS_SIMULATOR_CREATE_TIMEOUT", "创建 Simulator 失败", ctx, err)
	}
	udid := strings.TrimSpace(string(output))
	if udid == "" || len(udid) > 128 || strings.ContainsAny(udid, "\r\n\t /\\") {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "IOS_SIMULATOR_CREATE_RESPONSE_INVALID", "CoreSimulator 未返回有效 UDID", true, nil)
	}
	device, found, err := client.managedSimulatorByName(ctx, name)
	if err != nil || !found || device.UDID != udid {
		_ = client.deleteManagedSimulator(context.WithoutCancel(ctx), udid)
		return providers.Snapshot{}, providerError(providers.OperationCreate, "IOS_SIMULATOR_CREATE_RESPONSE_INVALID", "新建 Simulator 无法通过受控身份复核", true, err)
	}
	node, nodeErr := client.Health(ctx)
	return client.snapshot(request.HostID, device, node, nodeErr), nil
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
func (client *Client) Rebuild(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	device, err := client.allowedSimulator(ctx, providerRef, providers.OperationRebuild)
	if err != nil {
		return providers.Snapshot{}, err
	}
	if device.Busy {
		return providers.Snapshot{}, providerError(providers.OperationRebuild, "IOS_SIMULATOR_BUSY", "Simulator 当前仍被 Appium 会话占用，不能重建", true, nil)
	}
	if !strings.EqualFold(device.State, "Shutdown") {
		if _, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "shutdown", device.UDID); err != nil {
			return providers.Snapshot{}, client.lifecycleError(providers.OperationRebuild, "IOS_SIMULATOR_SHUTDOWN_FAILED", "IOS_SIMULATOR_REBUILD_TIMEOUT", "重建前停止 Simulator 失败", ctx, err)
		}
	}
	if _, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "erase", device.UDID); err != nil {
		return providers.Snapshot{}, client.lifecycleError(providers.OperationRebuild, "IOS_SIMULATOR_ERASE_FAILED", "IOS_SIMULATOR_REBUILD_TIMEOUT", "擦除 Simulator 数据失败", ctx, err)
	}
	if _, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "boot", device.UDID); err != nil {
		return providers.Snapshot{}, client.lifecycleError(providers.OperationRebuild, "IOS_SIMULATOR_BOOT_FAILED", "IOS_SIMULATOR_REBUILD_TIMEOUT", "重建后启动 Simulator 失败", ctx, err)
	}
	if _, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "bootstatus", device.UDID, "-b"); err != nil {
		return providers.Snapshot{}, client.lifecycleError(providers.OperationRebuild, "IOS_SIMULATOR_BOOT_FAILED", "IOS_SIMULATOR_REBUILD_TIMEOUT", "等待重建后的 Simulator 启动完成失败", ctx, err)
	}
	return client.waitSimulator(ctx, device.UDID, providers.OperationRebuild, func(snapshot providers.Snapshot) bool { return snapshot.Ready() })
}
func (client *Client) Delete(ctx context.Context, providerRef string) error {
	device, err := client.allowedSimulator(ctx, providerRef, providers.OperationDelete)
	if err != nil {
		return err
	}
	if !device.Managed {
		return providerError(providers.OperationDelete, "IOS_SIMULATOR_DELETE_NOT_MANAGED", "只允许删除由设备农场动态创建的 Simulator", false, nil)
	}
	if device.Busy {
		return providerError(providers.OperationDelete, "IOS_SIMULATOR_BUSY", "Simulator 当前仍被 Appium 会话占用，不能删除", true, nil)
	}
	if !strings.EqualFold(device.State, "Shutdown") {
		if _, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "shutdown", device.UDID); err != nil {
			return client.lifecycleError(providers.OperationDelete, "IOS_SIMULATOR_SHUTDOWN_FAILED", "IOS_SIMULATOR_DELETE_TIMEOUT", "删除前停止 Simulator 失败", ctx, err)
		}
	}
	return client.deleteManagedSimulator(ctx, device.UDID)
}

// CleanupCreated is the failure-compensation path used only after this Agent
// has just created a managed Simulator. It deliberately reads CoreSimulator
// directly so an Appium Device Farm inventory outage cannot leak the new VM.
func (client *Client) CleanupCreated(ctx context.Context, providerRef string) error {
	device, found, err := client.managedSimulatorByUDID(ctx, providerRef)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if !strings.EqualFold(device.State, "Shutdown") {
		if _, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "shutdown", device.UDID); err != nil {
			return client.lifecycleError(providers.OperationDelete, "IOS_SIMULATOR_SHUTDOWN_FAILED", "IOS_SIMULATOR_DELETE_TIMEOUT", "失败补偿时停止 Simulator 失败", ctx, err)
		}
	}
	return client.deleteManagedSimulator(ctx, device.UDID)
}

func (client *Client) InspectHealth(ctx context.Context, providerRef string) (providers.Health, error) {
	device, node, nodeErr, err := client.find(ctx, providerRef)
	if err != nil {
		return providers.Health{}, err
	}
	snapshot := client.snapshot("", device, node, nodeErr)
	if !snapshot.Health.Ready() {
		return snapshot.Health, providerError(providers.OperationInspectHealth, "IOS_DEVICE_NOT_READY", "iOS 设备或本机 Appium Node 尚未就绪", true, nodeErr)
	}
	return snapshot.Health, nil
}

func (client *Client) GetConnectionInfo(ctx context.Context, providerRef string) (providers.ConnectionInfo, error) {
	device, _, _, err := client.find(ctx, providerRef)
	if err != nil {
		return providers.ConnectionInfo{}, err
	}
	if !device.Allowed {
		return providers.ConnectionInfo{}, providerError(providers.OperationConnectionInfo, "IOS_DEVICE_NOT_ALLOWED", "该 iOS 设备不在宿主机 allowlist 中", false, nil)
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
		return Device{}, NodeHealth{}, nil, providerError(providers.OperationInspectHealth, "DEVICE_FARM_INVENTORY_FAILED", "无法读取本机 iOS 设备清单", true, err)
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
	if len(client.allowUDIDs) == 0 && len(client.allowedRuntimeIDs) == 0 && len(client.allowedDeviceTypeIDs) == 0 {
		return devices, nil
	}
	simulators, err := client.simctlDevices(ctx)
	if err != nil {
		return nil, err
	}
	byUDID := make(map[string]int, len(devices))
	for index := range devices {
		byUDID[devices[index].UDID] = index
	}
	for _, simulator := range simulators {
		_, fixed := client.allowUDIDs[simulator.UDID]
		simulator.Managed = strings.HasPrefix(simulator.Name, client.managedNamePrefix)
		simulator.Allowed = fixed || simulator.Managed
		if index, exists := byUDID[simulator.UDID]; exists {
			devices[index].Name = simulator.Name
			devices[index].State = simulator.State
			devices[index].PlatformVersion = simulator.PlatformVersion
			devices[index].RuntimeID = simulator.RuntimeID
			devices[index].DeviceTypeID = simulator.DeviceTypeID
			devices[index].Managed = simulator.Managed
			devices[index].Allowed = simulator.Allowed
			continue
		}
		if simulator.Allowed {
			devices = append(devices, simulator)
		}
	}
	sort.Slice(devices, func(left, right int) bool { return devices[left].UDID < devices[right].UDID })
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
	if nodeErr != nil || !node.PluginReady || device.UserBlocked || !device.RouterPresent {
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
		"managed": device.Managed, "runtimeId": device.RuntimeID, "deviceTypeId": device.DeviceTypeID,
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

func providerError(operation providers.Operation, code, message string, retryable bool, cause error) error {
	return &providers.Error{Operation: operation, Code: code, Message: message, Retryable: retryable, Cause: cause}
}

var _ providers.Provider = (*Client)(nil)

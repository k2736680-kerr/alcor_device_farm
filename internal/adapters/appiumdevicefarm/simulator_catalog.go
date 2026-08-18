package appiumdevicefarm

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

type SimulatorRuntime struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type SimulatorDeviceType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SimulatorCatalog struct {
	Runtimes    []SimulatorRuntime    `json:"runtimes"`
	DeviceTypes []SimulatorDeviceType `json:"device_types"`
}

func (catalog SimulatorCatalog) hasRuntime(id string) bool {
	for _, item := range catalog.Runtimes {
		if item.ID == id {
			return true
		}
	}
	return false
}

func (catalog SimulatorCatalog) hasDeviceType(id string) bool {
	for _, item := range catalog.DeviceTypes {
		if item.ID == id {
			return true
		}
	}
	return false
}

func (client *Client) SimulatorCatalog(ctx context.Context) (SimulatorCatalog, error) {
	var catalog SimulatorCatalog
	runtimeOutput, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "list", "runtimes", "-j")
	if err != nil {
		return catalog, providerError(providers.OperationDiscover, "IOS_RUNTIME_CATALOG_FAILED", "读取 iOS Runtime 目录失败", true, err)
	}
	var runtimes struct {
		Items []struct {
			ID        string `json:"identifier"`
			Name      string `json:"name"`
			Version   string `json:"version"`
			Available *bool  `json:"isAvailable"`
		} `json:"runtimes"`
	}
	if len(runtimeOutput) == 0 || len(runtimeOutput) > maxResponseBytes || json.Unmarshal(runtimeOutput, &runtimes) != nil {
		return catalog, providerError(providers.OperationDiscover, "IOS_RUNTIME_CATALOG_INVALID", "iOS Runtime 目录响应无效", true, nil)
	}
	for _, item := range runtimes.Items {
		_, allowed := client.allowedRuntimeIDs[strings.TrimSpace(item.ID)]
		if !allowed || (item.Available != nil && !*item.Available) || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(item.Name)), "ios") {
			continue
		}
		catalog.Runtimes = append(catalog.Runtimes, SimulatorRuntime{ID: strings.TrimSpace(item.ID), Name: strings.TrimSpace(item.Name), Version: strings.TrimSpace(item.Version)})
	}

	typeOutput, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "list", "devicetypes", "-j")
	if err != nil {
		return catalog, providerError(providers.OperationDiscover, "IOS_DEVICE_TYPE_CATALOG_FAILED", "读取 iPhone 机型目录失败", true, err)
	}
	var types struct {
		Items []struct {
			ID        string `json:"identifier"`
			Name      string `json:"name"`
			Available *bool  `json:"isAvailable"`
		} `json:"devicetypes"`
	}
	if len(typeOutput) == 0 || len(typeOutput) > maxResponseBytes || json.Unmarshal(typeOutput, &types) != nil {
		return catalog, providerError(providers.OperationDiscover, "IOS_DEVICE_TYPE_CATALOG_INVALID", "iPhone 机型目录响应无效", true, nil)
	}
	for _, item := range types.Items {
		_, allowed := client.allowedDeviceTypeIDs[strings.TrimSpace(item.ID)]
		if !allowed || (item.Available != nil && !*item.Available) || !strings.HasPrefix(strings.TrimSpace(item.Name), "iPhone") {
			continue
		}
		catalog.DeviceTypes = append(catalog.DeviceTypes, SimulatorDeviceType{ID: strings.TrimSpace(item.ID), Name: strings.TrimSpace(item.Name)})
	}
	return catalog, nil
}

func (client *Client) simctlDevices(ctx context.Context) ([]Device, error) {
	output, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "list", "devices", "-j")
	if err != nil {
		return nil, providerError(providers.OperationDiscover, "IOS_SIMULATOR_STATE_QUERY_FAILED", "读取 Simulator 实时状态失败", true, err)
	}
	var payload struct {
		Devices map[string][]struct {
			UDID         string `json:"udid"`
			Name         string `json:"name"`
			State        string `json:"state"`
			DeviceTypeID string `json:"deviceTypeIdentifier"`
			Available    *bool  `json:"isAvailable"`
		} `json:"devices"`
	}
	if len(output) == 0 || len(output) > maxResponseBytes || json.Unmarshal(output, &payload) != nil {
		return nil, providerError(providers.OperationDiscover, "IOS_SIMULATOR_STATE_QUERY_FAILED", "Simulator 实时状态响应无效", true, nil)
	}
	result := make([]Device, 0)
	seen := map[string]struct{}{}
	for runtimeID, items := range payload.Devices {
		if !strings.Contains(runtimeID, ".SimRuntime.iOS-") {
			continue
		}
		version := strings.ReplaceAll(strings.TrimPrefix(runtimeID[strings.LastIndex(runtimeID, ".")+1:], "iOS-"), "-", ".")
		for _, item := range items {
			udid := strings.TrimSpace(item.UDID)
			if udid == "" || (item.Available != nil && !*item.Available) {
				continue
			}
			if _, exists := seen[udid]; exists {
				return nil, providerError(providers.OperationDiscover, "IOS_SIMULATOR_STATE_CONFLICT", "CoreSimulator 目录中存在重复 UDID", false, nil)
			}
			seen[udid] = struct{}{}
			result = append(result, Device{UDID: udid, Name: strings.TrimSpace(item.Name), State: strings.TrimSpace(item.State), Platform: "ios",
				PlatformVersion: version, DeviceType: "simulator", DeviceTypeID: strings.TrimSpace(item.DeviceTypeID), RuntimeID: runtimeID})
		}
	}
	return result, nil
}

func (client *Client) managedSimulatorByName(ctx context.Context, name string) (Device, bool, error) {
	devices, err := client.simctlDevices(ctx)
	if err != nil {
		return Device{}, false, err
	}
	var matched *Device
	for index := range devices {
		if devices[index].Name != name {
			continue
		}
		if matched != nil {
			return Device{}, false, providerError(providers.OperationCreate, "IOS_SIMULATOR_NAME_CONFLICT", "CoreSimulator 中存在重复的设备农场名称", false, nil)
		}
		value := devices[index]
		value.Allowed, value.Managed = true, true
		matched = &value
	}
	if matched == nil {
		return Device{}, false, nil
	}
	return *matched, true, nil
}

func (client *Client) managedSimulatorByUDID(ctx context.Context, udid string) (Device, bool, error) {
	devices, err := client.simctlDevices(ctx)
	if err != nil {
		return Device{}, false, err
	}
	udid = strings.TrimSpace(udid)
	for _, device := range devices {
		if device.UDID != udid {
			continue
		}
		if !strings.HasPrefix(device.Name, client.managedNamePrefix) {
			return Device{}, false, providerError(providers.OperationDelete, "IOS_SIMULATOR_DELETE_NOT_MANAGED", "只允许清理由设备农场动态创建的 Simulator", false, nil)
		}
		device.Allowed, device.Managed = true, true
		return device, true, nil
	}
	return Device{}, false, nil
}

func (client *Client) deleteManagedSimulator(ctx context.Context, udid string) error {
	devices, err := client.simctlDevices(ctx)
	if err != nil {
		return err
	}
	managed := false
	for _, device := range devices {
		if device.UDID == udid {
			managed = strings.HasPrefix(device.Name, client.managedNamePrefix)
			break
		}
	}
	if !managed {
		return providerError(providers.OperationDelete, "IOS_SIMULATOR_DELETE_NOT_MANAGED", "只允许删除由设备农场动态创建的 Simulator", false, nil)
	}
	if _, err := client.commandRunner.Run(ctx, client.xcrunBinary, "simctl", "delete", udid); err != nil {
		return client.lifecycleError(providers.OperationDelete, "IOS_SIMULATOR_DELETE_FAILED", "IOS_SIMULATOR_DELETE_TIMEOUT", "删除 Simulator 失败", ctx, err)
	}
	return nil
}

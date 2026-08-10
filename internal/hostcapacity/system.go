package hostcapacity

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"
)

type System struct {
	DiskPath     string
	RenderDevice string
	DeviceSlots  int
	readMemory   func() (int64, int64, error)
	readDisk     func(string) (int64, int64, error)
}

func NewSystem(diskPath, renderDevice string, deviceSlots int) *System {
	return &System{DiskPath: diskPath, RenderDevice: renderDevice, DeviceSlots: deviceSlots, readMemory: systemMemoryMB, readDisk: systemDiskMB}
}

func (system *System) Snapshot(context.Context) (map[string]any, map[string]any, error) {
	if system == nil || system.readMemory == nil || system.readDisk == nil || system.DiskPath == "" {
		return nil, nil, fmt.Errorf("host capacity probe is not configured")
	}
	memoryTotal, memoryAvailable, err := system.readMemory()
	if err != nil {
		return nil, nil, err
	}
	diskTotal, diskAvailable, err := system.readDisk(system.DiskPath)
	if err != nil {
		return nil, nil, err
	}
	capacity := map[string]any{
		"resource_model": "dynamic_v1", "cpu_cores": runtime.NumCPU(),
		"memory_total_mb": memoryTotal, "memory_available_mb": memoryAvailable,
		"disk_total_mb": diskTotal, "disk_available_mb": diskAvailable,
		"collected_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if system.DeviceSlots > 0 {
		capacity["device_slots"] = system.DeviceSlots
	}
	capabilities := map[string]any{"kvm": true, "gpu_render": false}
	if system.RenderDevice != "" {
		if info, statErr := os.Stat(system.RenderDevice); statErr == nil && !info.IsDir() {
			capabilities["gpu_render"] = true
			capabilities["gpu_render_device"] = system.RenderDevice
		}
	}
	return capacity, capabilities, nil
}

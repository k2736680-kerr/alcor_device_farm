package capacity

import (
	"fmt"
	"math"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
)

const (
	DefaultReserveCPUCores = 1.0
	DefaultReserveMemoryMB = int64(2048)
	DefaultReserveDiskMB   = int64(4096)
)

type Host struct {
	CPUCores          float64   `json:"cpu_cores"`
	MemoryTotalMB     int64     `json:"memory_total_mb"`
	MemoryAvailableMB int64     `json:"memory_available_mb"`
	DiskTotalMB       int64     `json:"disk_total_mb"`
	DiskAvailableMB   int64     `json:"disk_available_mb"`
	DeviceSlots       int       `json:"device_slots,omitempty"`
	CollectedAt       time.Time `json:"collected_at"`
}

func HostFromMap(values map[string]any) (Host, bool) {
	if values == nil || values["resource_model"] != "dynamic_v1" {
		return Host{}, false
	}
	result := Host{
		CPUCores: number(values["cpu_cores"]), MemoryTotalMB: int64(number(values["memory_total_mb"])),
		MemoryAvailableMB: int64(number(values["memory_available_mb"])), DiskTotalMB: int64(number(values["disk_total_mb"])),
		DiskAvailableMB: int64(number(values["disk_available_mb"])), DeviceSlots: int(number(values["device_slots"])),
	}
	if text, ok := values["collected_at"].(string); ok {
		result.CollectedAt, _ = time.Parse(time.RFC3339Nano, text)
	}
	valid := result.CPUCores > 0 && result.MemoryTotalMB > 0 && result.MemoryAvailableMB >= 0 && result.DiskTotalMB > 0 && result.DiskAvailableMB >= 0
	return result, valid
}

type Allocation struct {
	CPUCores float64
	MemoryMB int64
	DiskMB   int64
	Slots    int
}

type Result struct {
	Fits              bool             `json:"fits"`
	Additional        int              `json:"additional_devices"`
	Limiting          string           `json:"limiting_resource,omitempty"`
	Shortfall         map[string]int64 `json:"shortfall,omitempty"`
	AvailableCPU      float64          `json:"available_cpu_cores"`
	AvailableMemoryMB int64            `json:"available_memory_mb"`
	AvailableDiskMB   int64            `json:"available_disk_mb"`
}

// Evaluate is deterministic and intentionally conservative. Existing CPU and
// memory are reserved by configured limits; real-time available memory is an
// additional safety ceiling. DiskAvailable already reflects existing files,
// so only pending allocations and the new device are deducted here.
func Evaluate(host Host, existing Allocation, pending Allocation, requested runtimeprofile.Profile, imageAlreadyCached bool) Result {
	availableCPU := math.Max(0, host.CPUCores-DefaultReserveCPUCores-existing.CPUCores-pending.CPUCores)
	accountedMemory := host.MemoryTotalMB - DefaultReserveMemoryMB - existing.MemoryMB - pending.MemoryMB
	realtimeMemory := host.MemoryAvailableMB - DefaultReserveMemoryMB - pending.MemoryMB
	availableMemory := max64(0, min64(accountedMemory, realtimeMemory))
	availableDisk := max64(0, host.DiskAvailableMB-DefaultReserveDiskMB-pending.DiskMB)
	requestDisk := requested.DataDiskMB
	if !imageAlreadyCached {
		requestDisk += requested.ImageDiskMB
	}
	byCPU := int(math.Floor(availableCPU / requested.ContainerCPUCores))
	byMemory := int(availableMemory / requested.ContainerMemoryMB)
	byDisk := math.MaxInt
	if requestDisk > 0 {
		byDisk = int(availableDisk / requestDisk)
	}
	bySlots := math.MaxInt
	if host.DeviceSlots > 0 {
		bySlots = host.DeviceSlots - existing.Slots - pending.Slots
	}
	additional := max(0, min(byCPU, byMemory, byDisk, bySlots))
	result := Result{Fits: additional > 0, Additional: additional, AvailableCPU: availableCPU, AvailableMemoryMB: availableMemory, AvailableDiskMB: availableDisk, Shortfall: map[string]int64{}}
	limits := []struct {
		name  string
		count int
	}{{"cpu", byCPU}, {"memory", byMemory}, {"disk", byDisk}, {"device_slots", bySlots}}
	minimum := math.MaxInt
	for _, limit := range limits {
		if limit.count < minimum {
			minimum, result.Limiting = limit.count, limit.name
		}
	}
	if availableCPU < requested.ContainerCPUCores {
		result.Shortfall["cpu_millicores"] = int64(math.Ceil((requested.ContainerCPUCores - availableCPU) * 1000))
	}
	if availableMemory < requested.ContainerMemoryMB {
		result.Shortfall["memory_mb"] = requested.ContainerMemoryMB - availableMemory
	}
	if availableDisk < requestDisk {
		result.Shortfall["disk_mb"] = requestDisk - availableDisk
	}
	if host.DeviceSlots > 0 && bySlots < 1 {
		result.Shortfall["device_slots"] = 1
	}
	if result.Fits {
		result.Shortfall = nil
	}
	return result
}

func (result Result) Error() error {
	if result.Fits {
		return nil
	}
	return fmt.Errorf("insufficient %s capacity: shortfall=%v", result.Limiting, result.Shortfall)
}

func min(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}
func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
func min64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
func max64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case int32:
		return float64(typed)
	}
	return 0
}

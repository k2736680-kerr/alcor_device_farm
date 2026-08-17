package capacity

import (
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
)

func TestEvaluateVariesByRequestedMemoryWithoutFixedOneDeviceLimit(t *testing.T) {
	host := Host{CPUCores: 16, MemoryTotalMB: 32768, MemoryAvailableMB: 30000, DiskTotalMB: 100000, DiskAvailableMB: 50000, CollectedAt: time.Now()}
	four, _ := runtimeprofile.Parse(map[string]any{"container_cpu_cores": 2, "container_memory_mb": 4096, "guest_memory_mb": 3072})
	eight, _ := runtimeprofile.Parse(map[string]any{"container_cpu_cores": 2, "container_memory_mb": 8192, "guest_memory_mb": 6144})
	fourResult := Evaluate(host, Allocation{}, Allocation{}, four, true)
	eightResult := Evaluate(host, Allocation{}, Allocation{}, eight, true)
	if fourResult.Additional <= eightResult.Additional || fourResult.Additional < 2 {
		t.Fatalf("4GB=%+v 8GB=%+v", fourResult, eightResult)
	}
}

func TestEvaluateChargesImageOnceButDataDiskPerDevice(t *testing.T) {
	host := Host{CPUCores: 32, MemoryTotalMB: 65536, MemoryAvailableMB: 60000, DiskTotalMB: 30000, DiskAvailableMB: 15000}
	profile, _ := runtimeprofile.Parse(map[string]any{"container_cpu_cores": 1, "container_memory_mb": 2048, "guest_memory_mb": 1536, "data_disk_mb": 4096, "image_disk_mb": 8000})
	cold := Evaluate(host, Allocation{}, Allocation{}, profile, false)
	warm := Evaluate(host, Allocation{}, Allocation{}, profile, true)
	if cold.Additional != 0 || warm.Additional < 2 || cold.Shortfall["disk_mb"] == 0 {
		t.Fatalf("cold=%+v warm=%+v", cold, warm)
	}
}

func TestChineseMessageReportsMemoryAndDiskShortfalls(t *testing.T) {
	result := Result{Limiting: "memory", Shortfall: map[string]int64{"memory_mb": 2048, "disk_mb": 8192}}
	message := ChineseMessage(result)
	if message != "宿主机资源不足：内存还缺 2048 MB，磁盘还缺 8192 MB" {
		t.Fatalf("message=%q", message)
	}
}

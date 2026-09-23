package iossimulator

import (
	"encoding/json"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/capacity"
)

func TestEvaluateHostCapacityReportsExactChineseShortfall(t *testing.T) {
	capacityJSON, err := json.Marshal(map[string]any{
		"cpu_cores": 10, "memory_available_mb": 3000, "disk_available_mb": 10000, "device_slots": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	usedJSON, err := json.Marshal(map[string]any{"device_slots": 2})
	if err != nil {
		t.Fatal(err)
	}
	result := evaluateHostCapacity(capacityJSON, usedJSON, 2, 0)
	if result.Fits || result.Additional != 0 || result.Shortfall["memory_mb"] != 1096 ||
		result.Shortfall["disk_mb"] != 6384 || result.Shortfall["device_slots"] != 1 {
		t.Fatalf("capacity result = %#v", result)
	}
	want := "宿主机资源不足：内存还缺 1096 MB，磁盘还缺 6384 MB，设备名额还缺 1 个"
	if message := capacity.ChineseMessage(result); message != want {
		t.Fatalf("capacity message = %q, want %q", message, want)
	}
}

func TestEvaluateHostCapacityAcceptsSufficientResources(t *testing.T) {
	capacityJSON, _ := json.Marshal(map[string]any{
		"cpu_cores": 10, "memory_available_mb": 8192, "disk_available_mb": 32768, "device_slots": 4,
	})
	usedJSON, _ := json.Marshal(map[string]any{"device_slots": 1})
	result := evaluateHostCapacity(capacityJSON, usedJSON, 1, 0)
	if !result.Fits || result.Additional != 1 || result.Shortfall != nil {
		t.Fatalf("capacity result = %#v", result)
	}
}

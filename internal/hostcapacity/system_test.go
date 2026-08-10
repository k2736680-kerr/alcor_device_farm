package hostcapacity

import (
	"context"
	"testing"
)

func TestSnapshotReportsDynamicFactsAndOptionalSafetySlotLimit(t *testing.T) {
	probe := NewSystem("/docker", "", 3)
	probe.readMemory = func() (int64, int64, error) { return 16000, 9000, nil }
	probe.readDisk = func(string) (int64, int64, error) { return 100000, 25000, nil }
	value, capabilities, err := probe.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value["resource_model"] != "dynamic_v1" || value["memory_available_mb"] != int64(9000) || value["device_slots"] != 3 || capabilities["kvm"] != true {
		t.Fatalf("capacity=%v capabilities=%v", value, capabilities)
	}
}

package appiumdevicefarm

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRealLocalIOSInventory(t *testing.T) {
	endpoint := os.Getenv("DEVICE_FARM_IOS_INTEGRATION_ENDPOINT")
	udid := os.Getenv("DEVICE_FARM_IOS_INTEGRATION_UDID")
	if endpoint == "" || udid == "" {
		t.Skip("real iOS Adapter environment is not configured")
	}
	client, err := New(Config{Endpoint: endpoint, Timeout: 5 * time.Second, AllowUDIDs: []string{udid}})
	if err != nil {
		t.Fatal(err)
	}
	health, err := client.Health(context.Background())
	if err != nil || !health.Ready() {
		t.Fatalf("local Node health=%+v error=%v", health, err)
	}
	devices, err := client.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	allowed := 0
	for _, device := range devices {
		if device.Allowed {
			allowed++
			if device.UDID != udid || device.DeviceType != "simulator" || device.State != "Booted" || device.Busy {
				t.Fatal("allowlisted Simulator inventory is not ready and idle")
			}
		}
	}
	if allowed != 1 {
		t.Fatalf("allowlisted inventory count=%d, want 1", allowed)
	}
	t.Logf("verified %d local iOS inventory entries with one allowlisted booted Simulator", len(devices))
}

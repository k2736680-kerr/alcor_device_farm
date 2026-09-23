package appiumdevicefarm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRealLocalIOSInventory(t *testing.T) {
	endpoint := os.Getenv("DEVICE_FARM_IOS_INTEGRATION_ENDPOINT")
	udids := integrationUDIDs()
	if endpoint == "" || len(udids) == 0 {
		t.Skip("real iOS Adapter environment is not configured")
	}
	client, err := New(Config{Endpoint: endpoint, Timeout: 5 * time.Second, AllowUDIDs: udids})
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
			if !containsString(udids, device.UDID) || device.DeviceType != "simulator" || device.State != "Booted" || device.Busy {
				t.Fatal("allowlisted Simulator inventory is not ready and idle")
			}
		}
	}
	if allowed != len(udids) {
		t.Fatalf("allowlisted inventory count=%d, want %d", allowed, len(udids))
	}
	t.Logf("verified %d local iOS inventory entries with %d allowlisted booted Simulators", len(devices), allowed)
}

func TestRealIOSSimulatorControlledStopAndStart(t *testing.T) {
	if os.Getenv("DEVICE_FARM_IOS_LIFECYCLE_INTEGRATION") != "1" {
		t.Skip("real iOS Simulator lifecycle environment is not enabled")
	}
	endpoint := os.Getenv("DEVICE_FARM_IOS_INTEGRATION_ENDPOINT")
	udids := integrationUDIDs()
	if endpoint == "" || len(udids) < 2 {
		t.Skip("two real iOS Simulators are not configured")
	}
	client, err := New(Config{Endpoint: endpoint, Timeout: 10 * time.Second, AllowUDIDs: udids,
		XcrunBinary: "xcrun", LifecyclePollInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	udid := udids[len(udids)-1]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	defer func() {
		restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer restoreCancel()
		if _, restoreErr := client.Start(restoreCtx, udid); restoreErr != nil {
			t.Errorf("恢复 Simulator 启动状态失败：%v", restoreErr)
		}
	}()
	stopped, err := client.Stop(ctx, udid)
	if err != nil || stopped.State != "stopped" || stopped.Ready() {
		t.Fatalf("停止 Simulator 结果=%+v 错误=%v", stopped, err)
	}
	started, err := client.Start(ctx, udid)
	if err != nil || !started.Ready() {
		t.Fatalf("重新启动 Simulator 结果=%+v 错误=%v", started, err)
	}
	t.Log("固定 allowlist Simulator 已通过受控 shutdown/boot/bootstatus 验证")
}

func integrationUDIDs() []string {
	value := strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_INTEGRATION_UDIDS"))
	if value == "" {
		value = strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_INTEGRATION_UDID"))
	}
	values := make([]string, 0, 2)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" && !containsString(values, item) {
			values = append(values, item)
		}
	}
	return values
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

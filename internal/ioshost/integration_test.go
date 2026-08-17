package ioshost

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/appiumdevicefarm"
)

func TestRealMacOSHostReadiness(t *testing.T) {
	endpoint := os.Getenv("DEVICE_FARM_IOS_INTEGRATION_ENDPOINT")
	appiumBinary := os.Getenv("DEVICE_FARM_APPIUM_BINARY")
	goIOSBinary := os.Getenv("DEVICE_FARM_GO_IOS_BINARY")
	wdaPackage := os.Getenv("DEVICE_FARM_IOS_WDA_PACKAGE_JSON")
	if endpoint == "" || appiumBinary == "" || goIOSBinary == "" || wdaPackage == "" {
		t.Skip("real macOS Host readiness environment is not configured")
	}
	client, err := appiumdevicefarm.New(appiumdevicefarm.Config{Endpoint: endpoint, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	probe, err := New(Config{NodeBinary: os.Getenv("DEVICE_FARM_NODE_BINARY"), AppiumBinary: appiumBinary,
		GoIOSBinary: goIOSBinary, WDAPackageJSON: wdaPackage, NodeHealth: client, CacheTTL: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := probe.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	readiness, ok := snapshot["host_readiness"].(map[string]any)
	if !ok || readiness["ready"] != true {
		t.Fatalf("real macOS Host readiness failed: %v", readiness)
	}
	if snapshot["host_os"] != "macos" || snapshot["host_arch"] != "arm64" {
		t.Fatalf("unexpected Host identity: os=%v arch=%v", snapshot["host_os"], snapshot["host_arch"])
	}
	t.Log("verified pinned macOS/Xcode/iOS Runtime/Node/Appium/Device Farm/XCUITest/WDA/go-ios readiness")
}

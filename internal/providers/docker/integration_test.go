package docker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

func TestDockerProviderLinuxKVMIntegration(t *testing.T) {
	if os.Getenv("DEVICE_FARM_DOCKER_INTEGRATION") != "1" {
		t.Skip("set DEVICE_FARM_DOCKER_INTEGRATION=1 on a Linux KVM host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	config := Config{
		Binary:             envValue("DEVICE_FARM_DOCKER_BINARY", "docker"),
		Image:              os.Getenv("DEVICE_FARM_DOCKER_IMAGE"),
		AdvertiseHost:      os.Getenv("DEVICE_FARM_DOCKER_ADVERTISE_HOST"),
		BindAddress:        envValue("DEVICE_FARM_DOCKER_BIND_ADDRESS", "127.0.0.1"),
		KVMDevice:          envValue("DEVICE_FARM_DOCKER_KVM_DEVICE", "/dev/kvm"),
		ContainerADBSerial: envValue("DEVICE_FARM_DOCKER_ADB_SERIAL", "emulator-5554"),
		DataMountPath:      envValue("DEVICE_FARM_DOCKER_DATA_MOUNT_PATH", "/home/androidusr"),
		Environment:        map[string]string{"APPIUM": "false", "WEB_VNC": "false"},
	}
	if device := strings.TrimSpace(os.Getenv("DEVICE_FARM_DOCKER_EMULATOR_DEVICE")); device != "" {
		config.Environment["EMULATOR_DEVICE"] = device
	}
	provider, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UTC().Format("20060102-150405")
	hostID := "host_integration_" + suffix
	refs := []string{"integration-one-" + suffix, "integration-two-" + suffix}
	for _, ref := range refs {
		ref := ref
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
			defer cleanupCancel()
			_ = provider.Delete(cleanupCtx, ref)
		})
	}

	connections := make([]providers.ConnectionInfo, 0, len(refs))
	for index, ref := range refs {
		_, err := provider.Create(ctx, providers.CreateRequest{
			DeviceID: fmt.Sprintf("device_integration_%d_%s", index, suffix), HostID: hostID,
			ImageID: "image_integration_" + suffix, ProviderRef: ref,
			Capabilities: map[string]any{"platformName": "Android", "abi": "x86_64"},
		})
		if err != nil {
			t.Fatal(err)
		}
		started, err := provider.Start(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, started.Connection)
	}
	if connections[0].Serial == connections[1].Serial || connections[0].ADBEndpoint == connections[1].ADBEndpoint {
		t.Fatalf("Docker assigned duplicate serial or ADB endpoint: %#v", connections)
	}

	for _, ref := range refs {
		waitForAndroidBoot(t, ctx, provider, ref)
	}
	discovered, err := provider.Discover(ctx, hostID)
	if err != nil || len(discovered) != 2 {
		t.Fatalf("discover=%#v error=%v", discovered, err)
	}
	for _, ref := range refs {
		if err := provider.Delete(ctx, ref); err != nil {
			t.Fatal(err)
		}
		assertNoDockerResources(t, ctx, provider, ref)
	}
	discovered, err = provider.Discover(ctx, hostID)
	if err != nil || len(discovered) != 0 {
		t.Fatalf("resources remained after delete: %#v error=%v", discovered, err)
	}
}

func waitForAndroidBoot(t *testing.T, ctx context.Context, provider *Provider, ref string) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		health, err := provider.InspectHealth(ctx, ref)
		if err == nil && health.Online && health.ADBOnline && health.BootCompleted {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Android boot did not complete for %s: health=%#v error=%v context=%v", ref, health, err, ctx.Err())
		case <-ticker.C:
		}
	}
}

func assertNoDockerResources(t *testing.T, ctx context.Context, provider *Provider, ref string) {
	t.Helper()
	client, ok := provider.backend.(*cliBackend)
	if !ok {
		t.Fatal("integration provider is not using the Docker CLI backend")
	}
	filter := "label=" + labelProviderRef + "=" + ref
	for _, kind := range []string{"network", "volume"} {
		output, err := client.run(ctx, kind, "ls", "-q", "--filter", "label="+labelManaged+"=true", "--filter", filter)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(output) != "" {
			t.Fatalf("managed %s resources remained for %s: %s", kind, ref, output)
		}
	}
}

func envValue(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

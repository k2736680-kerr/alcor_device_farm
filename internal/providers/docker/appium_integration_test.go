package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	appiumadapter "github.com/Ad-Quanta/alcor-device-farm/internal/adapters/appium"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

func TestDockerProviderLinuxKVMAppiumIntegration(t *testing.T) {
	if os.Getenv("DEVICE_FARM_APPIUM_INTEGRATION") != "1" {
		t.Skip("set DEVICE_FARM_APPIUM_INTEGRATION=1 on a Linux KVM host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	probe, err := appiumadapter.NewProbe(10 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{
		Binary:             envValue("DEVICE_FARM_DOCKER_BINARY", "docker"),
		Image:              os.Getenv("DEVICE_FARM_DOCKER_IMAGE"),
		AdvertiseHost:      os.Getenv("DEVICE_FARM_DOCKER_ADVERTISE_HOST"),
		BindAddress:        envValue("DEVICE_FARM_DOCKER_BIND_ADDRESS", "127.0.0.1"),
		KVMDevice:          envValue("DEVICE_FARM_DOCKER_KVM_DEVICE", "/dev/kvm"),
		ContainerADBSerial: envValue("DEVICE_FARM_DOCKER_ADB_SERIAL", "emulator-5554"),
		DataMountPath:      envValue("DEVICE_FARM_DOCKER_DATA_MOUNT_PATH", "/home/androidusr"),
		CPUs:               dockerIntegrationCPUs(t),
		Memory:             envValue("DEVICE_FARM_DOCKER_MEMORY", "5g"),
		AppiumProbe:        probe,
		Environment: map[string]string{
			"APPIUM": "true", "WEB_VNC": "false", "WEB_LOG": "false", "USER_BEHAVIOR_ANALYTICS": "false",
		},
	}
	if device := strings.TrimSpace(os.Getenv("DEVICE_FARM_DOCKER_EMULATOR_DEVICE")); device != "" {
		config.Environment["EMULATOR_DEVICE"] = device
	}
	provider, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UTC().Format("20060102-150405")
	hostID := "host_appium_integration_" + suffix
	deviceCount := dockerIntegrationDeviceCount(t)
	refs := make([]string, deviceCount)
	for index := range refs {
		refs[index] = fmt.Sprintf("appium-%d-%s", index+1, suffix)
	}
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
		if _, err := provider.Create(ctx, providers.CreateRequest{
			DeviceID: fmt.Sprintf("device_appium_%d_%s", index, suffix), HostID: hostID,
			ImageID: "image_appium_" + suffix, ProviderRef: ref,
			Capabilities: map[string]any{"platformName": "Android", "automationName": "UiAutomator2"},
		}); err != nil {
			t.Fatal(err)
		}
		started, err := provider.Start(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, started.Connection)
	}
	if connections[0].AppiumEndpoint == "" {
		t.Fatalf("Appium endpoints are not isolated: %#v", connections)
	}
	if len(connections) > 1 && (connections[0].AppiumEndpoint == connections[1].AppiumEndpoint || connections[1].AppiumEndpoint == "") {
		t.Fatalf("Appium endpoints are not isolated: %#v", connections)
	}
	for _, ref := range refs {
		waitForFullDeviceHealth(t, ctx, provider, ref)
	}
	if len(connections) > 1 {
		if _, err := createAppiumSession(ctx, connections[0].AppiumEndpoint, connections[1].Serial); err == nil {
			t.Fatal("the first Appium endpoint accepted the other container's external UDID")
		}
	}

	type sessionResult struct {
		index     int
		sessionID string
		err       error
	}
	results := make(chan sessionResult, len(connections))
	var group sync.WaitGroup
	for index, connection := range connections {
		index, connection := index, connection
		group.Add(1)
		go func() {
			defer group.Done()
			sessionID, err := createAppiumSession(ctx, connection.AppiumEndpoint, connection.AppiumUDID)
			results <- sessionResult{index: index, sessionID: sessionID, err: err}
		}()
	}
	group.Wait()
	close(results)
	sessionIDs := make([]string, len(connections))
	for result := range results {
		if result.err != nil {
			t.Fatalf("create Appium session %d: %v", result.index, result.err)
		}
		sessionIDs[result.index] = result.sessionID
	}
	for index, sessionID := range sessionIDs {
		index, sessionID := index, sessionID
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			_ = deleteAppiumSession(cleanupCtx, connections[index].AppiumEndpoint, sessionID)
		})
	}
}

func waitForFullDeviceHealth(t *testing.T, ctx context.Context, provider *Provider, ref string) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		health, err := provider.InspectHealth(ctx, ref)
		if err == nil && health.Ready() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("device and Appium did not become ready for %s: health=%#v error=%v context=%v", ref, health, err, ctx.Err())
		case <-ticker.C:
		}
	}
}

func createAppiumSession(ctx context.Context, endpoint, udid string) (string, error) {
	payload := map[string]any{"capabilities": map[string]any{
		"alwaysMatch": map[string]any{
			"platformName": "Android", "appium:automationName": "UiAutomator2", "appium:udid": udid,
			"appium:newCommandTimeout": 60, "appium:noReset": true, "appium:adbExecTimeout": 600000,
			"appium:androidInstallTimeout": 600000, "appium:uiautomator2ServerInstallTimeout": 600000,
			"appium:uiautomator2ServerLaunchTimeout": 600000, "appium:skipDeviceInitialization": true,
			"appium:ignoreHiddenApiPolicyError": true, "appium:skipServerInstallation": true,
		},
		"firstMatch": []any{map[string]any{}},
	}}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/session", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("Appium session returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
	}
	var result struct {
		Value struct {
			SessionID string `json:"sessionId"`
			Error     string `json:"error"`
			Message   string `json:"message"`
		} `json:"value"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	sessionID := result.Value.SessionID
	if sessionID == "" {
		sessionID = result.SessionID
	}
	if result.Value.Error != "" || sessionID == "" {
		return "", fmt.Errorf("Appium session failed: %s %s", result.Value.Error, result.Value.Message)
	}
	return sessionID, nil
}

func deleteAppiumSession(ctx context.Context, endpoint, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("Appium session ID is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(endpoint, "/")+"/session/"+sessionID, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("delete Appium session returned HTTP %d", response.StatusCode)
	}
	return nil
}

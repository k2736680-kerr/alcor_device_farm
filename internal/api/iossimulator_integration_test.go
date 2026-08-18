package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/iossimulator"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

const (
	testIOSHostID       = "ios_host_000000000001"
	testIOSPoolID       = "ios_pool_000000000001"
	testIOSRuntimeID    = "com.apple.CoreSimulator.SimRuntime.iOS-26-3"
	testIOSDeviceTypeID = "com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro"
)

func TestIOSSimulatorCreateAPIRegistersAtomicOperation(t *testing.T) {
	environment := newManagementEnvironment(t)
	seedIOSSimulatorHostAndPool(t, environment, true)

	catalogResponse := environment.request(t, http.MethodGet,
		"/api/v1/ios-simulator-catalog?host_id="+testIOSHostID, nil, serviceToken, "")
	assertStatus(t, catalogResponse, http.StatusOK)
	var catalog iossimulator.Catalog
	decodeData(t, catalogResponse, &catalog)
	if catalog.HostID != testIOSHostID || len(catalog.Runtimes) != 1 || len(catalog.DeviceTypes) != 1 {
		t.Fatalf("unexpected simulator catalog: %#v", catalog)
	}

	input := validIOSSimulatorCreateInput()
	createdResponse := environment.request(t, http.MethodPost, "/api/v1/ios-simulators", input, serviceToken, "ios-create-key-0001")
	assertStatus(t, createdResponse, http.StatusAccepted)
	var created iossimulator.Operation
	decodeData(t, createdResponse, &created)
	if created.DeviceID == "" || created.CommandID == "" || created.Status != "creating_simulator" {
		t.Fatalf("unexpected create operation: %#v", created)
	}

	replayedResponse := environment.request(t, http.MethodPost, "/api/v1/ios-simulators", input, serviceToken, "ios-create-key-0001")
	assertStatus(t, replayedResponse, http.StatusAccepted)
	var replayed iossimulator.Operation
	decodeData(t, replayedResponse, &replayed)
	if replayed.DeviceID != created.DeviceID || replayed.CommandID != created.CommandID {
		t.Fatalf("idempotent replay changed operation: created=%#v replayed=%#v", created, replayed)
	}

	var platform, kind, providerType, providerRef, lifecycle, runtimeID, deviceTypeID string
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT platform,device_kind,provider_type,provider_ref,lifecycle_status,
		capabilities->>'runtimeId',capabilities->>'deviceTypeId' FROM devices WHERE id=$1`, created.DeviceID).
		Scan(&platform, &kind, &providerType, &providerRef, &lifecycle, &runtimeID, &deviceTypeID); err != nil {
		t.Fatal(err)
	}
	if platform != "ios" || kind != "simulator" || providerType != "appium_device_farm_ios" || providerRef != "pending:"+created.DeviceID ||
		lifecycle != "provisioning" || runtimeID != testIOSRuntimeID || deviceTypeID != testIOSDeviceTypeID {
		t.Fatalf("unexpected registered device: platform=%s kind=%s provider=%s ref=%s lifecycle=%s runtime=%s type=%s",
			platform, kind, providerType, providerRef, lifecycle, runtimeID, deviceTypeID)
	}
	var commandCount, membershipCount, auditCount, totalTarget int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM device_host_commands WHERE id=$1 AND command_type='create'),
		(SELECT count(*) FROM device_pool_devices WHERE pool_id=$2 AND device_id=$3 AND enabled),
		(SELECT count(*) FROM device_audit_events WHERE resource_id=$3 AND action='create_ios_simulator'),
		(SELECT total_target FROM device_pools WHERE id=$2)`, created.CommandID, testIOSPoolID, created.DeviceID).
		Scan(&commandCount, &membershipCount, &auditCount, &totalTarget); err != nil {
		t.Fatal(err)
	}
	if commandCount != 1 || membershipCount != 1 || auditCount != 1 || totalTarget != 1 {
		t.Fatalf("atomic rows command=%d membership=%d audit=%d target=%d", commandCount, membershipCount, auditCount, totalTarget)
	}

	commands, err := environment.hostCommands.Claim(context.Background(), testIOSHostID, hostcommand.ClaimInput{LeaseSeconds: 30, MaxCommands: 1})
	if err != nil || len(commands) != 1 || commands[0].LeaseToken == nil {
		t.Fatalf("claim create command=%#v error=%v", commands, err)
	}
	udid := "11111111-2222-3333-4444-555555555555"
	components := map[string]string{providers.ProbeTransport: string(providers.ProbePassed), providers.ProbeOSReady: string(providers.ProbePassed),
		providers.ProbeAutomation: string(providers.ProbePassed), providers.ProbeRouter: string(providers.ProbePassed)}
	if _, err := environment.hostCommands.Complete(context.Background(), commands[0].ID, hostcommand.CompletionInput{
		LeaseToken: *commands[0].LeaseToken, Attempt: commands[0].Attempt, Status: "succeeded",
		Result: map[string]any{"platform": "ios", "state": "running", "generation": 1,
			"connection": map[string]any{"serial": udid, "provider_id": udid, "appium_endpoint": "http://127.0.0.1:4723", "appium_udid": udid},
			"health":     map[string]any{"online": true, "boot_completed": true, "appium_healthy": true, "components": components}},
	}); err != nil {
		t.Fatal(err)
	}
	var serial, health string
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT provider_ref,serial,lifecycle_status,health_status
		FROM devices WHERE id=$1`, created.DeviceID).Scan(&providerRef, &serial, &lifecycle, &health); err != nil {
		t.Fatal(err)
	}
	if providerRef != udid || serial != udid || lifecycle != "ready" || health != "healthy" {
		t.Fatalf("completed device ref=%s serial=%s lifecycle=%s health=%s", providerRef, serial, lifecycle, health)
	}
	deleted := environment.request(t, http.MethodDelete, "/api/v1/devices/"+created.DeviceID,
		map[string]any{"reason": "验证删除设备池最后一台 iOS Simulator"}, serviceToken, "ios-delete-last-device-0001")
	assertStatus(t, deleted, http.StatusAccepted)
	var remainingTarget, remainingMinReady, remainingMaxConcurrency int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT total_target,min_ready,max_concurrency
		FROM device_pools WHERE id=$1`, testIOSPoolID).Scan(&remainingTarget, &remainingMinReady, &remainingMaxConcurrency); err != nil {
		t.Fatal(err)
	}
	if remainingTarget != 0 || remainingMinReady != 0 || remainingMaxConcurrency != 1 {
		t.Fatalf("last device delete pool target=%d min_ready=%d max_concurrency=%d", remainingTarget, remainingMinReady, remainingMaxConcurrency)
	}
}

func TestIOSSimulatorCreateAPIRejectsCatalogBypassAndRollsBack(t *testing.T) {
	environment := newManagementEnvironment(t)
	seedIOSSimulatorHostAndPool(t, environment, true)
	input := validIOSSimulatorCreateInput()
	input["runtime_id"] = "com.apple.CoreSimulator.SimRuntime.iOS-99-9"
	response := environment.request(t, http.MethodPost, "/api/v1/ios-simulators", input, serviceToken, "ios-create-key-0002")
	assertStatus(t, response, http.StatusBadRequest)
	assertIOSCreateRows(t, environment, 0)
}

func TestIOSSimulatorCreateAPIRejectsInsufficientCapacityAndRollsBack(t *testing.T) {
	environment := newManagementEnvironment(t)
	seedIOSSimulatorHostAndPool(t, environment, false)
	response := environment.request(t, http.MethodPost, "/api/v1/ios-simulators", validIOSSimulatorCreateInput(), serviceToken, "ios-create-key-0003")
	assertStatus(t, response, http.StatusConflict)
	if response.Error == nil || response.Error.Code != "INSUFFICIENT_HOST_RESOURCES" {
		t.Fatalf("unexpected capacity error: %#v", response.Error)
	}
	if response.Error.Message != "宿主机磁盘不足：磁盘还缺 15360 MB" {
		t.Fatalf("unexpected capacity message: %q", response.Error.Message)
	}
	details, ok := response.Error.Details.(map[string]any)
	if !ok || details["limiting_resource"] != "disk" {
		t.Fatalf("unexpected capacity details: %#v", response.Error.Details)
	}
	shortfall, ok := details["shortfall"].(map[string]any)
	if !ok || shortfall["disk_mb"] != float64(15360) {
		t.Fatalf("unexpected capacity shortfall: %#v", details["shortfall"])
	}
	assertIOSCreateRows(t, environment, 0)
}

func TestIOSSimulatorCreateAPIRejectsAndroidPool(t *testing.T) {
	environment := newManagementEnvironment(t)
	seedIOSSimulatorHostAndPool(t, environment, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_pools SET platform='android' WHERE id=$1`, testIOSPoolID); err != nil {
		t.Fatal(err)
	}
	response := environment.request(t, http.MethodPost, "/api/v1/ios-simulators", validIOSSimulatorCreateInput(), serviceToken, "ios-create-key-0004")
	assertStatus(t, response, http.StatusConflict)
	assertIOSCreateRows(t, environment, 0)
}

func seedIOSSimulatorHostAndPool(t *testing.T, environment *managementEnvironment, enoughCapacity bool) {
	t.Helper()
	capacity := map[string]any{"resource_model": "dynamic_v1", "cpu_cores": 10, "memory_total_mb": 24576,
		"memory_available_mb": 16384, "disk_total_mb": 600000, "disk_available_mb": 500000, "device_slots": 4}
	if !enoughCapacity {
		capacity["disk_available_mb"] = 1024
	}
	capabilities := map[string]any{"ios_simulator_catalog": map[string]any{
		"runtimes":     []map[string]any{{"id": testIOSRuntimeID, "name": "iOS 26.3", "version": "26.3"}},
		"device_types": []map[string]any{{"id": testIOSDeviceTypeID, "name": "iPhone 17 Pro"}},
	}}
	capacityJSON, err := json.Marshal(capacity)
	if err != nil {
		t.Fatal(err)
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := environment.db.Pool().Exec(context.Background(), `INSERT INTO device_hosts
		(id,name,host_type,host_os,host_arch,capabilities,capacity,used_capacity,status,draining,last_heartbeat_at)
		VALUES($1,'ios-mac-host','appium_device_farm_ios','macos','arm64',$2::jsonb,$3::jsonb,'{"device_slots":0}'::jsonb,'online',false,clock_timestamp())`,
		testIOSHostID, capabilitiesJSON, capacityJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.db.Pool().Exec(context.Background(), `INSERT INTO device_pools
		(id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status,total_target,min_ready,platform)
		VALUES($1,'ios-simulator-pool',1800,86400,1,'active',0,0,'ios')`, testIOSPoolID); err != nil {
		t.Fatal(err)
	}
}

func validIOSSimulatorCreateInput() map[string]any {
	return map[string]any{"host_id": testIOSHostID, "pool_id": testIOSPoolID, "runtime_id": testIOSRuntimeID,
		"device_type_id": testIOSDeviceTypeID, "display_name": "自动化 iPhone", "reason": "创建 iOS 自动化测试设备"}
}

func assertIOSCreateRows(t *testing.T, environment *managementEnvironment, want int) {
	t.Helper()
	var devices, commands, memberships int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM devices WHERE platform='ios'),
		(SELECT count(*) FROM device_host_commands WHERE command_type='create'),
		(SELECT count(*) FROM device_pool_devices)`).Scan(&devices, &commands, &memberships); err != nil {
		t.Fatal(err)
	}
	if devices != want || commands != want || memberships != want {
		t.Fatalf("unexpected create rows devices=%d commands=%d memberships=%d want=%d", devices, commands, memberships, want)
	}
}

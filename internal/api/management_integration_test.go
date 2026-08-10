package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	managementpostgres "github.com/Ad-Quanta/alcor-device-farm/internal/management/postgres"
	farmmetrics "github.com/Ad-Quanta/alcor-device-farm/internal/metrics"
	providermock "github.com/Ad-Quanta/alcor-device-farm/internal/providers/mock"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reconcile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/server"
)

const (
	serviceToken = "management-service-token"
	agentToken   = "management-agent-token"
)

func TestManagementAPICompleteMockFlow(t *testing.T) {
	environment := newManagementEnvironment(t)
	assertStatus(t, environment.request(t, http.MethodGet, "/readyz", nil, "", ""), http.StatusOK)

	imageResponse := environment.request(t, http.MethodPost, "/api/v1/device-images", validImageInput(), serviceToken, "image-create-0001")
	assertStatus(t, imageResponse, http.StatusCreated)
	var image management.Image
	decodeData(t, imageResponse, &image)
	replayedImage := environment.request(t, http.MethodPost, "/api/v1/device-images", validImageInput(), serviceToken, "image-create-0001")
	assertStatus(t, replayedImage, http.StatusCreated)
	var replayed management.Image
	decodeData(t, replayedImage, &replayed)
	if replayed.ID != image.ID {
		t.Fatalf("idempotent image IDs differ: %s != %s", image.ID, replayed.ID)
	}
	conflictingImage := validImageInput()
	conflictingImage["name"] = "different-request"
	conflictResponse := environment.request(t, http.MethodPost, "/api/v1/device-images", conflictingImage, serviceToken, "image-create-0001")
	assertStatus(t, conflictResponse, http.StatusConflict)
	if conflictResponse.Error == nil || conflictResponse.Error.Code != "CONFLICT" {
		t.Fatalf("idempotency conflict error = %#v", conflictResponse.Error)
	}
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-images", nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-images/"+image.ID, nil, serviceToken, ""), http.StatusOK)
	imageUpdate := validImageInput()
	imageUpdate["name"] = "android-14-updated"
	assertStatus(t, environment.request(t, http.MethodPut, "/api/v1/device-images/"+image.ID, imageUpdate, serviceToken, ""), http.StatusOK)
	if storedImage, err := environment.store.GetImage(context.Background(), image.ID); err != nil || storedImage.Status != "draft" {
		t.Fatalf("stored image before validation = %#v, error=%v", storedImage, err)
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-images/"+image.ID+"/validations", nil, serviceToken, "image-valid-0001"), http.StatusAccepted)
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-images/"+image.ID+"/validations", nil, serviceToken, "image-valid-0002"), http.StatusConflict)

	hostResponse := environment.request(t, http.MethodPost, "/api/v1/device-hosts", validHostInput(), serviceToken, "host-create-0001")
	assertStatus(t, hostResponse, http.StatusCreated)
	var host management.Host
	decodeData(t, hostResponse, &host)
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-hosts", nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-hosts/"+host.ID, nil, serviceToken, ""), http.StatusOK)
	hostUpdate := validHostInput()
	hostUpdate["address"] = "10.0.0.2"
	hostUpdate["capacity"] = map[string]any{"cpu": 8, "memory_mb": 16384, "device_slots": 1}
	assertStatus(t, environment.request(t, http.MethodPut, "/api/v1/device-hosts/"+host.ID, hostUpdate, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-hosts/"+host.ID+"/drains", reasonBody(), serviceToken, ""), http.StatusConflict)

	poolResponse := environment.request(t, http.MethodPost, "/api/v1/device-pools", validPoolInput(true), serviceToken, "pool-create-0001")
	assertStatus(t, poolResponse, http.StatusCreated)
	var pool management.Pool
	decodeData(t, poolResponse, &pool)
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-pools", nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-pools/"+pool.ID, nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodPut, "/api/v1/device-pools/"+pool.ID, validPoolInput(true), serviceToken, ""), http.StatusOK)
	poolImagePath := "/api/v1/device-pools/" + pool.ID + "/images/" + image.ID
	assertStatus(t, environment.request(t, http.MethodPut, poolImagePath, map[string]any{"min_ready": 1, "max_instances": 2, "enabled": true}, serviceToken, ""), http.StatusBadRequest)
	assertStatus(t, environment.request(t, http.MethodPut, poolImagePath, map[string]any{"min_ready": 2, "max_instances": 2, "enabled": true}, serviceToken, ""), http.StatusOK)
	var poolConcurrency, hostSlots int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT max_concurrency FROM device_pools WHERE id=$1`, pool.ID).Scan(&poolConcurrency); err != nil {
		t.Fatal(err)
	}
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT (capacity->>'device_slots')::int FROM device_hosts WHERE id=$1`, host.ID).Scan(&hostSlots); err != nil {
		t.Fatal(err)
	}
	if poolConcurrency != 2 || hostSlots != 1 {
		t.Fatalf("pool target must not rewrite host capacity: pool=%d host_slots=%d", poolConcurrency, hostSlots)
	}
	heartbeat := map[string]any{
		"agent_time": time.Now().UTC(), "capacity": map[string]any{"cpu": 8, "memory_mb": 16384, "device_slots": 1},
		"environment": map[string]any{"docker": "mock"}, "devices": []any{},
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/internal/v1/device-hosts/"+host.ID+"/heartbeats", heartbeat, agentToken, ""), http.StatusOK)
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT (capacity->>'device_slots')::int FROM device_hosts WHERE id=$1`, host.ID).Scan(&hostSlots); err != nil {
		t.Fatal(err)
	}
	if hostSlots != 1 {
		t.Fatalf("legacy heartbeat overwrote explicit host safety limit: %d", hostSlots)
	}
	assertStatus(t, environment.request(t, http.MethodPut, poolImagePath, map[string]any{"min_ready": 3, "max_instances": 3, "enabled": true}, serviceToken, ""), http.StatusOK)
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT max_concurrency FROM device_pools WHERE id=$1`, pool.ID).Scan(&poolConcurrency); err != nil {
		t.Fatal(err)
	}
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT (capacity->>'device_slots')::int FROM device_hosts WHERE id=$1`, host.ID).Scan(&hostSlots); err != nil {
		t.Fatal(err)
	}
	if poolConcurrency != 3 || hostSlots != 1 {
		t.Fatalf("pool target must stay independent from host capacity: pool=%d host_slots=%d", poolConcurrency, hostSlots)
	}
	if _, err := environment.store.SetPoolImage(context.Background(), management.PoolImage{
		PoolID: pool.ID, ImageID: image.ID, MinReady: 2, MaxInstances: 2, Enabled: true,
	}, management.DeviceAudit{
		ID: "00000000-0000-4000-8000-000000009998", ActorType: "service", ActorID: "integration",
		Action: "set_device_pool_target", RequestID: "integration-race-guard", Reason: "fixed emulator target updated",
	}); !errors.Is(err, management.ErrInvalidArgument) {
		t.Fatalf("transactional scale-down reason guard error=%v", err)
	}
	assertStatus(t, environment.request(t, http.MethodPut, poolImagePath, map[string]any{"min_ready": 2, "max_instances": 2, "enabled": true}, serviceToken, ""), http.StatusBadRequest)
	assertStatus(t, environment.request(t, http.MethodPut, poolImagePath, map[string]any{
		"min_ready": 2, "max_instances": 2, "enabled": true, "reason": "reduce integration target",
	}, serviceToken, ""), http.StatusOK)
	var scaleDownAudits int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_audit_events
		WHERE resource_type='device_pool_image' AND resource_id=$1 AND action='set_device_pool_target'
		AND reason='reduce integration target'`, image.ID).Scan(&scaleDownAudits); err != nil {
		t.Fatal(err)
	}
	if scaleDownAudits != 1 {
		t.Fatalf("scale-down audit rows=%d", scaleDownAudits)
	}
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-pools/"+pool.ID+"/images", nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodDelete, poolImagePath, nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodPut, poolImagePath, map[string]any{"min_ready": 2, "max_instances": 2, "enabled": true}, serviceToken, ""), http.StatusOK)

	if _, err := environment.db.Pool().Exec(context.Background(), "UPDATE device_hosts SET status='online',draining=false WHERE id=$1", host.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.db.Pool().Exec(context.Background(), "UPDATE device_images SET status='ready' WHERE id=$1", image.ID); err != nil {
		t.Fatal(err)
	}
	runtimeUpdate := validImageInput()
	runtimeUpdate["name"] = "android-14-updated"
	runtimeUpdate["docker_image"] = "registry.example/alcor/android-emulator:api34-r2"
	assertStatus(t, environment.request(t, http.MethodPut, "/api/v1/device-images/"+image.ID, runtimeUpdate, serviceToken, ""), http.StatusOK)
	if storedImage, err := environment.store.GetImage(context.Background(), image.ID); err != nil || storedImage.Status != "draft" ||
		storedImage.ValidationError == nil || *storedImage.ValidationError != "IMAGE_REVALIDATION_REQUIRED" {
		t.Fatalf("changed runtime image was not invalidated: %#v error=%v", storedImage, err)
	}
	if _, err := environment.db.Pool().Exec(context.Background(), "UPDATE device_images SET status='ready',validation_error=NULL WHERE id=$1", image.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-hosts/"+host.ID+"/drains", reasonBody(), serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodDelete, "/api/v1/device-hosts/"+host.ID+"/drains", reasonBody(), serviceToken, ""), http.StatusOK)

	device, err := environment.service.ProvisionMockDevice(context.Background(), management.ProvisionMockDeviceInput{
		ID: "device_0000000000001", HostID: host.ID, ImageID: image.ID, ProviderRef: "mock-api-device-1",
		Capabilities: map[string]any{"apiLevel": 34, "platformName": "Android"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/devices", nil, serviceToken, ""), http.StatusOK)
	assertPageTotal(t, environment.request(t, http.MethodGet,
		"/api/v1/devices?lifecycle_status=ready&health_status=healthy", nil, serviceToken, ""), 1)
	assertPageTotal(t, environment.request(t, http.MethodGet,
		"/api/v1/devices?lifecycle_status=deleted", nil, serviceToken, ""), 0)
	assertPageTotal(t, environment.request(t, http.MethodGet,
		"/api/v1/devices?pool_id="+pool.ID, nil, serviceToken, ""), 0)
	assertStatus(t, environment.request(t, http.MethodGet,
		"/api/v1/devices?lifecycle_status=not-a-status", nil, serviceToken, ""), http.StatusBadRequest)
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/devices/"+device.ID, nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-pools/"+pool.ID+"/devices", map[string]any{"device_id": device.ID}, serviceToken, ""), http.StatusOK)
	assertPageTotal(t, environment.request(t, http.MethodGet,
		"/api/v1/devices?pool_id="+pool.ID+"&lifecycle_status=ready&health_status=healthy", nil, serviceToken, ""), 1)
	assertStatus(t, environment.requestAsActor(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/quarantines",
		reasonBody(), serviceToken, "", "token=must-not-be-audit-actor"), http.StatusBadRequest)
	restartResponse := environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/restarts", reasonBody(), serviceToken, "device-restart-01")
	assertStatus(t, restartResponse, http.StatusAccepted)
	var restarting management.Device
	decodeData(t, restartResponse, &restarting)
	if restarting.LifecycleStatus != "stopped" || restarting.HealthStatus != "unknown" {
		t.Fatalf("queued restart device = %#v", restarting)
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/restarts", reasonBody(), serviceToken, "device-restart-01"), http.StatusAccepted)
	assertCommandCount(t, environment.db, device.ID, "restart", 1)
	completeNextManagementCommand(t, environment, host.ID, "restart", true)

	schedulable, err := environment.store.ListSchedulableDevices(context.Background(), pool.ID)
	if err != nil || len(schedulable) != 1 {
		t.Fatalf("schedulable before quarantine = %d, error=%v", len(schedulable), err)
	}
	assertStatus(t, environment.requestAsActor(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/quarantines", reasonBody(), serviceToken, "", "alcor-user-01"), http.StatusOK)
	assertPageTotal(t, environment.request(t, http.MethodGet,
		"/api/v1/devices?lifecycle_status=quarantined", nil, serviceToken, ""), 1)
	assertPageTotal(t, environment.request(t, http.MethodGet,
		"/api/v1/devices?lifecycle_status=ready&health_status=healthy", nil, serviceToken, ""), 0)
	schedulable, err = environment.store.ListSchedulableDevices(context.Background(), pool.ID)
	if err != nil || len(schedulable) != 0 {
		t.Fatalf("schedulable after quarantine = %d, error=%v", len(schedulable), err)
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/restarts", reasonBody(), serviceToken, "device-restart-02"), http.StatusConflict)
	rebuildResponse := environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/rebuilds", reasonBody(), serviceToken, "device-rebuild-01")
	assertStatus(t, rebuildResponse, http.StatusAccepted)
	var rebuilding management.Device
	decodeData(t, rebuildResponse, &rebuilding)
	if rebuilding.LifecycleStatus != "provisioning" || rebuilding.HealthStatus != "unknown" {
		t.Fatalf("queued rebuild device = %#v", rebuilding)
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/rebuilds", reasonBody(), serviceToken, "device-rebuild-01"), http.StatusAccepted)
	assertCommandCount(t, environment.db, device.ID, "rebuild", 1)
	var rebuildImage, rebuildDigest string
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT payload->>'docker_image',payload->>'docker_digest'
		FROM device_host_commands WHERE command_type='rebuild' AND payload->>'device_id'=$1`, device.ID).
		Scan(&rebuildImage, &rebuildDigest); err != nil {
		t.Fatal(err)
	}
	if rebuildImage != "registry.example/alcor/android-emulator:api34-r2" || rebuildDigest != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("rebuild image=%q digest=%q", rebuildImage, rebuildDigest)
	}
	completeNextManagementCommand(t, environment, host.ID, "rebuild", true)

	deleteCandidate, err := environment.service.ProvisionMockDevice(context.Background(), management.ProvisionMockDeviceInput{
		ID: "device_delete_00000001", HostID: host.ID, ImageID: image.ID, ProviderRef: "mock-api-device-delete",
		Capabilities: map[string]any{"apiLevel": 34, "platformName": "Android"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-pools/"+pool.ID+"/devices",
		map[string]any{"device_id": deleteCandidate.ID}, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodDelete, "/api/v1/devices/"+deleteCandidate.ID,
		reasonBody(), serviceToken, "device-delete-ready"), http.StatusConflict)
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+deleteCandidate.ID+"/quarantines",
		reasonBody(), serviceToken, ""), http.StatusOK)
	if _, err := environment.db.Pool().Exec(context.Background(), `INSERT INTO device_reservations
		(id,client_id,pool_id,device_id,owner_type,owner_id,requested_capabilities,lease_seconds,status,
		 idempotency_key,starts_at,expires_at)
		VALUES('reservation_delete_0001','console-test',$1,$2,'manual','owner_delete_00001','{}',1800,'active',
		'delete-active-reservation',clock_timestamp(),clock_timestamp()+interval '30 minutes')`, pool.ID, deleteCandidate.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, environment.request(t, http.MethodDelete, "/api/v1/devices/"+deleteCandidate.ID,
		reasonBody(), serviceToken, "device-delete-active"), http.StatusConflict)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_reservations SET
		status='force_released',released_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE id='reservation_delete_0001'`); err != nil {
		t.Fatal(err)
	}
	deleteResponse := environment.request(t, http.MethodDelete, "/api/v1/devices/"+deleteCandidate.ID,
		reasonBody(), serviceToken, "device-delete-accepted")
	assertStatus(t, deleteResponse, http.StatusAccepted)
	assertStatus(t, environment.request(t, http.MethodDelete, "/api/v1/devices/"+deleteCandidate.ID,
		reasonBody(), serviceToken, "device-delete-accepted"), http.StatusAccepted)
	assertStatus(t, environment.request(t, http.MethodDelete, "/api/v1/devices/"+deleteCandidate.ID,
		reasonBody(), serviceToken, "device-delete-second-command"), http.StatusConflict)
	assertCommandCount(t, environment.db, deleteCandidate.ID, "delete", 1)
	var enabledMemberships int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_pool_devices
		WHERE device_id=$1 AND enabled`, deleteCandidate.ID).Scan(&enabledMemberships); err != nil {
		t.Fatal(err)
	}
	if enabledMemberships != 0 {
		t.Fatalf("delete candidate enabled memberships=%d", enabledMemberships)
	}
	completeNextManagementCommand(t, environment, host.ID, "delete", true)
	deletedDevice, err := environment.store.GetDevice(context.Background(), deleteCandidate.ID)
	if err != nil || deletedDevice.LifecycleStatus != "deleted" || deletedDevice.ADBEndpoint != nil || deletedDevice.AppiumEndpoint != nil || deletedDevice.STFSerial != nil {
		t.Fatalf("deleted management device=%#v error=%v", deletedDevice, err)
	}
	var deleteAudits int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_audit_events
		WHERE resource_type='device' AND resource_id=$1 AND action='delete_device'`, deleteCandidate.ID).Scan(&deleteAudits); err != nil {
		t.Fatal(err)
	}
	if deleteAudits != 1 {
		t.Fatalf("delete device audits=%d", deleteAudits)
	}

	failedDeleteCandidate, err := environment.service.ProvisionMockDevice(context.Background(), management.ProvisionMockDeviceInput{
		ID: "device_delete_00000002", HostID: host.ID, ImageID: image.ID, ProviderRef: "mock-api-device-delete-failure",
		Capabilities: map[string]any{"apiLevel": 34, "platformName": "Android"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+failedDeleteCandidate.ID+"/quarantines",
		reasonBody(), serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodDelete, "/api/v1/devices/"+failedDeleteCandidate.ID,
		reasonBody(), serviceToken, "device-delete-failure"), http.StatusAccepted)
	completeNextManagementCommand(t, environment, host.ID, "delete", false)
	completeNextManagementCommand(t, environment, host.ID, "delete", false)
	completeNextManagementCommand(t, environment, host.ID, "delete", false)
	failedDeleteDevice, err := environment.store.GetDevice(context.Background(), failedDeleteCandidate.ID)
	if err != nil || failedDeleteDevice.LifecycleStatus != "quarantined" || failedDeleteDevice.HealthStatus != "unhealthy" {
		t.Fatalf("failed delete device=%#v error=%v", failedDeleteDevice, err)
	}

	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/restarts", reasonBody(), serviceToken, "device-restart-failure"), http.StatusAccepted)
	completeNextManagementCommand(t, environment, host.ID, "restart", false)
	failedDevice, err := environment.store.GetDevice(context.Background(), device.ID)
	if err != nil || failedDevice.LifecycleStatus != "quarantined" || failedDevice.HealthStatus != "unhealthy" {
		t.Fatalf("failed management command device = %#v, error=%v", failedDevice, err)
	}
	assertStatus(t, environment.request(t, http.MethodDelete, "/api/v1/devices/"+device.ID+"/quarantines", reasonBody(), serviceToken, ""), http.StatusOK)
	var auditedActions, missingFields int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*),count(*) FILTER (
		WHERE actor_type<>'service' OR actor_id='' OR request_id='' OR reason IS NULL OR length(trim(reason))<3)
		FROM device_audit_events WHERE resource_type='device' AND resource_id=$1`, device.ID).
		Scan(&auditedActions, &missingFields); err != nil {
		t.Fatal(err)
	}
	if auditedActions != 5 || missingFields != 0 {
		t.Fatalf("device audit actions=%d missing fields=%d", auditedActions, missingFields)
	}
	var alcorActorActions int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_audit_events
		WHERE resource_type='device' AND resource_id=$1 AND actor_id='alcor-user-01'`, device.ID).Scan(&alcorActorActions); err != nil {
		t.Fatal(err)
	}
	if alcorActorActions != 1 {
		t.Fatalf("Alcor actor audit actions=%d", alcorActorActions)
	}
	var commandEvents int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_health_events
		WHERE device_id=$1 AND event_type IN ('device_management_operation_succeeded','device_management_operation_failed')`, device.ID).
		Scan(&commandEvents); err != nil {
		t.Fatal(err)
	}
	if commandEvents != 3 {
		t.Fatalf("management command health events=%d", commandEvents)
	}

	assertStatus(t, environment.request(t, http.MethodPut, "/api/v1/device-pools/"+pool.ID, validPoolInput(false), serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-pools/"+pool.ID+"/devices", map[string]any{"device_id": device.ID}, serviceToken, ""), http.StatusConflict)
	assertStatus(t, environment.request(t, http.MethodPut, "/api/v1/device-pools/"+pool.ID, validPoolInput(true), serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodDelete, "/api/v1/device-pools/"+pool.ID+"/devices", map[string]any{"device_id": device.ID}, serviceToken, ""), http.StatusOK)

	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-hosts/"+host.ID+"/drains", reasonBody(), serviceToken, ""), http.StatusOK)
	_, err = environment.service.ProvisionMockDevice(context.Background(), management.ProvisionMockDeviceInput{
		ID: "device_0000000000002", HostID: host.ID, ImageID: image.ID, ProviderRef: "mock-api-device-2",
	})
	if !errors.Is(err, management.ErrHostUnavailable) {
		t.Fatalf("provision on draining host error = %v", err)
	}
}

func TestEveryManagementRouteIsProtected(t *testing.T) {
	environment := newManagementEnvironment(t)
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/device-images"}, {http.MethodPost, "/api/v1/device-images"},
		{http.MethodGet, "/api/v1/device-images/id"}, {http.MethodPut, "/api/v1/device-images/id"}, {http.MethodPost, "/api/v1/device-images/id/validations"},
		{http.MethodGet, "/api/v1/device-hosts"}, {http.MethodPost, "/api/v1/device-hosts"},
		{http.MethodGet, "/api/v1/device-hosts/id"}, {http.MethodPut, "/api/v1/device-hosts/id"}, {http.MethodPost, "/api/v1/device-hosts/id/drains"}, {http.MethodDelete, "/api/v1/device-hosts/id/drains"},
		{http.MethodGet, "/api/v1/device-pools"}, {http.MethodPost, "/api/v1/device-pools"},
		{http.MethodGet, "/api/v1/device-pools/id"}, {http.MethodPut, "/api/v1/device-pools/id"}, {http.MethodPost, "/api/v1/device-pools/id/devices"}, {http.MethodDelete, "/api/v1/device-pools/id/devices"},
		{http.MethodGet, "/api/v1/device-pools/id/images"}, {http.MethodPut, "/api/v1/device-pools/id/images/image-id"}, {http.MethodDelete, "/api/v1/device-pools/id/images/image-id"},
		{http.MethodGet, "/api/v1/devices"}, {http.MethodGet, "/api/v1/devices/id"},
		{http.MethodPost, "/api/v1/devices/id/restarts"}, {http.MethodPost, "/api/v1/devices/id/rebuilds"},
		{http.MethodDelete, "/api/v1/devices/id"}, {http.MethodPost, "/api/v1/devices/id/quarantines"}, {http.MethodDelete, "/api/v1/devices/id/quarantines"},
		{http.MethodGet, "/api/v1/device-reservations"}, {http.MethodPost, "/api/v1/device-reservations"},
		{http.MethodGet, "/api/v1/device-reservations/id"},
		{http.MethodPost, "/api/v1/device-reservations/id/extensions"},
		{http.MethodPost, "/api/v1/device-reservations/id/releases"},
	}
	for _, route := range routes {
		assertStatus(t, environment.request(t, route.method, route.path, map[string]any{}, "", ""), http.StatusUnauthorized)
		assertStatus(t, environment.request(t, route.method, route.path, map[string]any{}, agentToken, ""), http.StatusForbidden)
	}
}

func TestManagementAPIRejectsInvalidParameters(t *testing.T) {
	environment := newManagementEnvironment(t)
	sensitiveImage := validImageInput()
	sensitiveImage["resource_config"] = map[string]any{"registry_token": "must-not-be-stored"}
	sensitiveHost := validHostInput()
	sensitiveHost["capabilities"] = map[string]any{"message": "password=must-not-be-stored"}
	tests := []struct {
		method, path string
		body         any
		key          string
	}{
		{http.MethodPost, "/api/v1/device-images", map[string]any{}, "valid-key-01"},
		{http.MethodPost, "/api/v1/device-images", sensitiveImage, "valid-key-sensitive-image"},
		{http.MethodPost, "/api/v1/device-hosts", map[string]any{}, "valid-key-02"},
		{http.MethodPost, "/api/v1/device-hosts", sensitiveHost, "valid-key-sensitive-host"},
		{http.MethodPost, "/api/v1/device-pools", map[string]any{}, "valid-key-03"},
		{http.MethodPost, "/api/v1/device-images/id/validations", nil, ""},
		{http.MethodPost, "/api/v1/device-pools/id/devices", map[string]any{}, ""},
		{http.MethodPut, "/api/v1/device-pools/id/images/image-id", map[string]any{"min_ready": 2, "max_instances": 1, "enabled": true}, ""},
		{http.MethodPost, "/api/v1/devices/id/restarts", map[string]any{}, "valid-key-04"},
		{http.MethodPost, "/api/v1/devices/id/rebuilds", map[string]any{}, "valid-key-05"},
		{http.MethodDelete, "/api/v1/devices/id", map[string]any{}, "valid-key-delete"},
		{http.MethodDelete, "/api/v1/devices/id", reasonBody(), ""},
		{http.MethodPost, "/api/v1/devices/id/quarantines", map[string]any{}, ""},
		{http.MethodPost, "/api/v1/devices/id/quarantines", map[string]any{"reason": "token=must-not-be-stored"}, ""},
	}
	for _, test := range tests {
		assertStatus(t, environment.request(t, test.method, test.path, test.body, serviceToken, test.key), http.StatusBadRequest)
	}

	request, err := http.NewRequest(http.MethodPost, environment.server.URL+"/api/v1/device-images", bytes.NewBufferString("{"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+serviceToken)
	request.Header.Set("Idempotency-Key", "valid-key-06")
	response, err := environment.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid JSON status=%d", response.StatusCode)
	}
}

type managementEnvironment struct {
	db           *database.DB
	store        *managementpostgres.Store
	service      *management.Service
	server       *httptest.Server
	hostCommands *hostcommand.Service
	reservations *reservation.Service
}

func newManagementEnvironment(t *testing.T, controllers ...reservation.STFController) *managementEnvironment {
	t.Helper()
	databaseURL := os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEVICE_FARM_TEST_DATABASE_URL is not set")
	}
	db, err := database.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE
        device_idempotency_records,device_audit_events,device_health_events,device_sessions,
        device_reservations,device_pool_devices,devices,device_pool_images,device_pools,
        device_host_commands,device_hosts,device_images RESTART IDENTITY CASCADE`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	store := managementpostgres.New(db)
	var sequence atomic.Int64
	generator := func() (string, error) { return fmt.Sprintf("00000000-0000-4000-8000-%012d", sequence.Add(1)), nil }
	provider := providermock.New(providermock.Config{})
	service := management.NewService(store, provider, generator)
	reservationService := reservation.NewService(db, generator, controllers...)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	healthService := reconcile.New(db, provider, nil, 3, 0, logger)
	hostCommands := hostcommand.New(db)
	httpServer := httptest.NewServer(server.Handler(config.SecurityConfig{ServiceToken: serviceToken, AgentToken: agentToken}, logger, server.Services{
		Management: service, Reservations: reservationService, Reconcile: healthService, HostCommands: hostCommands, Metrics: farmmetrics.New(db),
	}))
	t.Cleanup(func() { httpServer.Close(); db.Close() })
	return &managementEnvironment{db: db, store: store, service: service, server: httpServer, hostCommands: hostCommands, reservations: reservationService}
}

func (environment *managementEnvironment) request(t *testing.T, method, path string, body any, token, key string) responseEnvelope {
	return environment.requestAsActor(t, method, path, body, token, key, "")
}

func (environment *managementEnvironment) requestAsActor(t *testing.T, method, path string, body any, token, key, actorID string) responseEnvelope {
	t.Helper()
	var content io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		content = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, environment.server.URL+path, content)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	if actorID != "" {
		request.Header.Set("X-Device-Farm-Actor-Id", actorID)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := environment.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope responseEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode %s %s response: %v", method, path, err)
	}
	envelope.Status = response.StatusCode
	return envelope
}

type responseEnvelope struct {
	Status int
	Data   json.RawMessage `json:"data"`
	Error  *httpx.APIError `json:"error"`
}

func assertStatus(t *testing.T, response responseEnvelope, want int) {
	t.Helper()
	if response.Status != want {
		t.Fatalf("status=%d want=%d error=%#v data=%s", response.Status, want, response.Error, response.Data)
	}
}

func assertCommandCount(t *testing.T, db *database.DB, deviceID, commandType string, want int) {
	t.Helper()
	var count int
	if err := db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_host_commands
		WHERE command_type=$1 AND payload->>'device_id'=$2 AND payload->>'operation_source'='management'`, commandType, deviceID).
		Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("management %s command count=%d want=%d", commandType, count, want)
	}
}

func completeNextManagementCommand(t *testing.T, environment *managementEnvironment, hostID, commandType string, success bool) {
	t.Helper()
	commands, err := environment.hostCommands.Claim(context.Background(), hostID, hostcommand.ClaimInput{
		LeaseSeconds: 30, MaxCommands: 1,
	})
	if err != nil || len(commands) != 1 || commands[0].CommandType != commandType || commands[0].LeaseToken == nil {
		t.Fatalf("claimed management command=%#v error=%v", commands, err)
	}
	if commandType == "rebuild" {
		if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE devices SET lifecycle_status='booting'
			WHERE id=(SELECT payload->>'device_id' FROM device_host_commands WHERE id=$1)`, commands[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	completion := hostcommand.CompletionInput{
		LeaseToken: *commands[0].LeaseToken, Attempt: commands[0].Attempt, Status: "succeeded",
		Result: map[string]any{
			"generation": 2,
			"connection": map[string]any{"serial": "10.0.0.1:31000", "adb_endpoint": "10.0.0.1:31000",
				"appium_endpoint": "http://10.0.0.1:32000", "appium_udid": "emulator-5554"},
			"health": map[string]any{"online": true, "adb_online": true, "boot_completed": true, "appium_healthy": true},
		},
	}
	if commandType == "delete" {
		completion.Result = map[string]any{"deleted": true}
	}
	if !success {
		completion.Status = "failed"
		completion.Result = nil
		completion.Error = &hostcommand.CompletionError{Code: "KVM_UNAVAILABLE", Message: "injected provider failure", Retryable: commandType == "delete"}
	}
	if _, err := environment.hostCommands.Complete(context.Background(), commands[0].ID, completion); err != nil {
		t.Fatal(err)
	}
}

func decodeData(t *testing.T, response responseEnvelope, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Data, target); err != nil {
		t.Fatal(err)
	}
}

func assertPageTotal(t *testing.T, response responseEnvelope, expected int) {
	t.Helper()
	assertStatus(t, response, http.StatusOK)
	var page struct {
		Total int `json:"total"`
	}
	decodeData(t, response, &page)
	if page.Total != expected {
		t.Fatalf("page total=%d want=%d", page.Total, expected)
	}
}
func validImageInput() map[string]any {
	return map[string]any{"name": "android-14", "docker_image": "registry.example/alcor/android-emulator:api34", "docker_digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "api_level": 34, "abi": "x86_64", "resolution": "1080x2400", "resource_config": map[string]any{"cpu": 2, "memory_mb": 4096}}
}
func validHostInput() map[string]any {
	return map[string]any{"name": "mock-host", "host_type": "docker_emulator", "address": "10.0.0.1", "capabilities": map[string]any{"kvm": true}, "capacity": map[string]any{"cpu": 8, "memory_mb": 16384, "device_slots": 4}}
}
func validPoolInput(enabled bool) map[string]any {
	return map[string]any{"name": "smoke-pool", "default_lease_seconds": 1800, "max_lease_seconds": 7200, "max_concurrency": 2, "enabled": enabled}
}
func reasonBody() map[string]any { return map[string]any{"reason": "integration acceptance"} }

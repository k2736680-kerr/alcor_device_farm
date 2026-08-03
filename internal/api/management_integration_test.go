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

	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	managementpostgres "github.com/Ad-Quanta/alcor-device-farm/internal/management/postgres"
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
	assertStatus(t, environment.request(t, http.MethodPut, poolImagePath, map[string]any{"min_ready": 2, "max_instances": 2, "enabled": true}, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-pools/"+pool.ID+"/images", nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodDelete, poolImagePath, nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodPut, poolImagePath, map[string]any{"min_ready": 2, "max_instances": 2, "enabled": true}, serviceToken, ""), http.StatusOK)

	if _, err := environment.db.Pool().Exec(context.Background(), "UPDATE device_hosts SET status='online',draining=false WHERE id=$1", host.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.db.Pool().Exec(context.Background(), "UPDATE device_images SET status='ready' WHERE id=$1", image.ID); err != nil {
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
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/devices/"+device.ID, nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-pools/"+pool.ID+"/devices", map[string]any{"device_id": device.ID}, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/restarts", reasonBody(), serviceToken, "device-restart-01"), http.StatusAccepted)

	schedulable, err := environment.store.ListSchedulableDevices(context.Background(), pool.ID)
	if err != nil || len(schedulable) != 1 {
		t.Fatalf("schedulable before quarantine = %d, error=%v", len(schedulable), err)
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/quarantines", reasonBody(), serviceToken, ""), http.StatusOK)
	schedulable, err = environment.store.ListSchedulableDevices(context.Background(), pool.ID)
	if err != nil || len(schedulable) != 0 {
		t.Fatalf("schedulable after quarantine = %d, error=%v", len(schedulable), err)
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/restarts", reasonBody(), serviceToken, "device-restart-02"), http.StatusConflict)
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/rebuilds", reasonBody(), serviceToken, "device-rebuild-01"), http.StatusAccepted)
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/devices/"+device.ID+"/quarantines", reasonBody(), serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodDelete, "/api/v1/devices/"+device.ID+"/quarantines", reasonBody(), serviceToken, ""), http.StatusOK)

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
		{http.MethodPost, "/api/v1/devices/id/quarantines"}, {http.MethodDelete, "/api/v1/devices/id/quarantines"},
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
	tests := []struct {
		method, path string
		body         any
		key          string
	}{
		{http.MethodPost, "/api/v1/device-images", map[string]any{}, "valid-key-01"},
		{http.MethodPost, "/api/v1/device-hosts", map[string]any{}, "valid-key-02"},
		{http.MethodPost, "/api/v1/device-pools", map[string]any{}, "valid-key-03"},
		{http.MethodPost, "/api/v1/device-images/id/validations", nil, ""},
		{http.MethodPost, "/api/v1/device-pools/id/devices", map[string]any{}, ""},
		{http.MethodPut, "/api/v1/device-pools/id/images/image-id", map[string]any{"min_ready": 2, "max_instances": 1, "enabled": true}, ""},
		{http.MethodPost, "/api/v1/devices/id/restarts", map[string]any{}, "valid-key-04"},
		{http.MethodPost, "/api/v1/devices/id/rebuilds", map[string]any{}, "valid-key-05"},
		{http.MethodPost, "/api/v1/devices/id/quarantines", map[string]any{}, ""},
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
}

func newManagementEnvironment(t *testing.T) *managementEnvironment {
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
	reservationService := reservation.NewService(db, generator)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	healthService := reconcile.New(db, provider, nil, 3, logger)
	hostCommands := hostcommand.New(db)
	httpServer := httptest.NewServer(server.Handler(config.SecurityConfig{ServiceToken: serviceToken, AgentToken: agentToken}, logger, server.Services{Management: service, Reservations: reservationService, Reconcile: healthService, HostCommands: hostCommands}))
	t.Cleanup(func() { httpServer.Close(); db.Close() })
	return &managementEnvironment{db: db, store: store, service: service, server: httpServer, hostCommands: hostCommands}
}

func (environment *managementEnvironment) request(t *testing.T, method, path string, body any, token, key string) responseEnvelope {
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

func decodeData(t *testing.T, response responseEnvelope, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Data, target); err != nil {
		t.Fatal(err)
	}
}
func validImageInput() map[string]any {
	return map[string]any{"name": "android-14", "docker_digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "api_level": 34, "abi": "x86_64", "resolution": "1080x2400", "resource_config": map[string]any{"cpu": 2, "memory_mb": 4096}}
}
func validHostInput() map[string]any {
	return map[string]any{"name": "mock-host", "host_type": "docker_emulator", "address": "10.0.0.1", "capabilities": map[string]any{"kvm": true}, "capacity": map[string]any{"cpu": 8, "memory_mb": 16384, "device_slots": 4}}
}
func validPoolInput(enabled bool) map[string]any {
	return map[string]any{"name": "smoke-pool", "default_lease_seconds": 1800, "max_lease_seconds": 7200, "max_concurrency": 2, "enabled": enabled}
}
func reasonBody() map[string]any { return map[string]any{"reason": "integration acceptance"} }

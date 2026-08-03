package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/scheduler"
)

func TestReservationAPIStoresListsAndReplaysPendingRequest(t *testing.T) {
	environment := newManagementEnvironment(t)
	seedReservationPool(t, environment, "active")
	body := map[string]any{
		"pool_id": "pool_000000000000001", "owner_type": "test_run",
		"owner_id": "attempt_000000000001", "lease_seconds": 600,
		"requested_capabilities": map[string]any{"platformName": "Android", "apiLevel": 34},
	}
	createdResponse := environment.request(t, http.MethodPost, "/api/v1/device-reservations", body, serviceToken, "reservation-api-key-01")
	assertStatus(t, createdResponse, http.StatusCreated)
	var created reservation.View
	decodeData(t, createdResponse, &created)
	if created.Status != "pending" || created.DeviceID != nil {
		t.Fatalf("created reservation=%#v", created)
	}

	replayedResponse := environment.request(t, http.MethodPost, "/api/v1/device-reservations", body, serviceToken, "reservation-api-key-01")
	assertStatus(t, replayedResponse, http.StatusCreated)
	var replayed reservation.View
	decodeData(t, replayedResponse, &replayed)
	if replayed.ID != created.ID {
		t.Fatalf("idempotent IDs differ: %s != %s", replayed.ID, created.ID)
	}

	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-reservations/"+created.ID, nil, serviceToken, ""), http.StatusOK)
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-reservations?owner_type=test_run&owner_id=attempt_000000000001", nil, serviceToken, ""), http.StatusOK)

	conflict := map[string]any{}
	for key, value := range body {
		conflict[key] = value
	}
	conflict["owner_id"] = "attempt_000000000002"
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-reservations", conflict, serviceToken, "reservation-api-key-01"), http.StatusConflict)
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-reservations", body, serviceToken, ""), http.StatusBadRequest)
	assertStatus(t, environment.request(t, http.MethodGet, "/api/v1/device-reservations?owner_type=wrong", nil, serviceToken, ""), http.StatusBadRequest)
}

func TestReservationAPIRejectsDisabledPoolAndExcessLease(t *testing.T) {
	environment := newManagementEnvironment(t)
	seedReservationPool(t, environment, "disabled")
	body := map[string]any{
		"pool_id": "pool_000000000000001", "owner_type": "manual",
		"owner_id": "owner_00000000000001", "lease_seconds": 600,
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-reservations", body, serviceToken, "reservation-api-key-02"), http.StatusConflict)
	if _, err := environment.db.Pool().Exec(context.Background(), "UPDATE device_pools SET status='active' WHERE id='pool_000000000000001'"); err != nil {
		t.Fatal(err)
	}
	body["lease_seconds"] = 3601
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-reservations", body, serviceToken, "reservation-api-key-03"), http.StatusBadRequest)
}

func TestReservationAPIExtendsAndReleasesActiveReservation(t *testing.T) {
	environment := newManagementEnvironment(t)
	seedReservationDevice(t, environment)
	body := map[string]any{
		"pool_id": "pool_000000000000001", "owner_type": "test_run",
		"owner_id": "attempt_000000000001", "lease_seconds": 600,
		"requested_capabilities": map[string]any{"platformName": "Android", "apiLevel": 34},
	}
	createdResponse := environment.request(t, http.MethodPost, "/api/v1/device-reservations", body, serviceToken, "reservation-lifecycle-create")
	assertStatus(t, createdResponse, http.StatusCreated)
	var created reservation.View
	decodeData(t, createdResponse, &created)
	if _, err := scheduler.New(environment.db, nil, nil).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	extendedResponse := environment.request(t, http.MethodPost, "/api/v1/device-reservations/"+created.ID+"/extensions",
		map[string]any{"additional_seconds": 300}, serviceToken, "reservation-extension-01")
	assertStatus(t, extendedResponse, http.StatusOK)
	var extended reservation.View
	decodeData(t, extendedResponse, &extended)
	if extended.Status != "active" {
		t.Fatalf("extended status=%s", extended.Status)
	}
	releaseBody := map[string]any{"reason": "API lifecycle test completed"}
	releasedResponse := environment.request(t, http.MethodPost, "/api/v1/device-reservations/"+created.ID+"/releases",
		releaseBody, serviceToken, "reservation-release-01")
	assertStatus(t, releasedResponse, http.StatusOK)
	var released reservation.View
	decodeData(t, releasedResponse, &released)
	if released.Status != "released" || released.ReleasedAt == nil {
		t.Fatalf("released reservation=%#v", released)
	}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-reservations/"+created.ID+"/releases",
		releaseBody, serviceToken, "reservation-release-01"), http.StatusOK)
}

func seedReservationPool(t *testing.T, environment *managementEnvironment, status string) {
	t.Helper()
	_, err := environment.db.Pool().Exec(context.Background(), `
        INSERT INTO device_pools (id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
        VALUES ('pool_000000000000001','reservation-api-pool',600,3600,2,$1)`, status)
	if err != nil {
		t.Fatal(err)
	}
}

func seedReservationDevice(t *testing.T, environment *managementEnvironment) {
	t.Helper()
	statements := []string{
		`INSERT INTO device_hosts (id,name,host_type,status,draining)
            VALUES ('host_000000000000001','reservation-api-host','docker_emulator','online',false)`,
		`INSERT INTO device_images (id,name,docker_digest,api_level,abi,resolution,status)
            VALUES ('image_00000000000001','reservation-api-image','sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',34,'x86_64','1080x2400','ready')`,
		`INSERT INTO device_pools (id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
            VALUES ('pool_000000000000001','reservation-lifecycle-pool',600,1200,1,'active')`,
		`INSERT INTO devices (id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,
            serial,adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status)
            VALUES ('device_0000000000001','host_000000000000001','image_00000000000001','emulator','mock',
            'mock-api-lifecycle','rebuild','emulator-api-lifecycle','127.0.0.1:5555','http://127.0.0.1:4723',
            '{"platformName":"Android","apiLevel":34}'::jsonb,'ready','healthy')`,
		`INSERT INTO device_pool_devices (pool_id,device_id,enabled)
            VALUES ('pool_000000000000001','device_0000000000001',true)`,
	}
	for _, statement := range statements {
		if _, err := environment.db.Pool().Exec(context.Background(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

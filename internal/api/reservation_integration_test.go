package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
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

func seedReservationPool(t *testing.T, environment *managementEnvironment, status string) {
	t.Helper()
	_, err := environment.db.Pool().Exec(context.Background(), `
        INSERT INTO device_pools (id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
        VALUES ('pool_000000000000001','reservation-api-pool',600,3600,2,$1)`, status)
	if err != nil {
		t.Fatal(err)
	}
}

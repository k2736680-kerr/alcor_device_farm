package api_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/stf"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/scheduler"
)

type fakeSTFController struct {
	mutex             sync.Mutex
	releaseErr        error
	claimCalls        int
	releaseCalls      int
	remoteCalls       int
	disconnectCalls   int
	lastReleaseSerial string
}

func (controller *fakeSTFController) Claim(context.Context, string, time.Duration) error {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	controller.claimCalls++
	return nil
}

func (controller *fakeSTFController) Release(_ context.Context, serial string) error {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	controller.releaseCalls++
	controller.lastReleaseSerial = serial
	return controller.releaseErr
}

func (controller *fakeSTFController) RemoteConnect(context.Context, string) (stf.RemoteConnection, error) {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	controller.remoteCalls++
	return stf.RemoteConnection{URL: "10.0.0.20:7401"}, nil
}

func (controller *fakeSTFController) RemoteDisconnect(context.Context, string) error {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	controller.disconnectCalls++
	return nil
}

type retryableSTFError struct{}

func (retryableSTFError) Error() string     { return "temporary STF outage" }
func (retryableSTFError) IsRetryable() bool { return true }

func TestReservationAPIStoresListsAndReplaysPendingRequest(t *testing.T) {
	environment := newManagementEnvironment(t)
	seedReservationPool(t, environment, "active")
	body := map[string]any{
		"pool_id": "pool_000000000000001", "owner_type": "test_run",
		"owner_id": "attempt_000000000001", "lease_seconds": 600,
		"requested_capabilities": map[string]any{"platformName": "Android", "apiLevel": 34},
	}
	sensitiveBody := map[string]any{}
	for key, value := range body {
		sensitiveBody[key] = value
	}
	sensitiveBody["requested_capabilities"] = map[string]any{"platformName": "Android", "api_token": "must-not-be-stored"}
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-reservations", sensitiveBody,
		serviceToken, "reservation-sensitive-capability"), http.StatusBadRequest)
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

func TestReservationAPIReleaseCancelsUnassignedPendingReservation(t *testing.T) {
	environment := newManagementEnvironment(t)
	seedReservationPool(t, environment, "active")
	createdResponse := environment.request(t, http.MethodPost, "/api/v1/device-reservations", map[string]any{
		"pool_id": "pool_000000000000001", "owner_type": "test_run",
		"owner_id": "attempt_000000000099", "lease_seconds": 600,
	}, serviceToken, "pending-cancel-create")
	assertStatus(t, createdResponse, http.StatusCreated)
	var created reservation.View
	decodeData(t, createdResponse, &created)

	releasedResponse := environment.request(t, http.MethodPost, "/api/v1/device-reservations/"+created.ID+"/releases",
		map[string]any{"reason": "Harness canceled while waiting for capacity"}, serviceToken, "pending-cancel-release")
	assertStatus(t, releasedResponse, http.StatusOK)
	var canceled reservation.View
	decodeData(t, releasedResponse, &canceled)
	if canceled.Status != "failed" || canceled.DeviceID != nil || canceled.FailureCode == nil || *canceled.FailureCode != "RESERVATION_CANCELED" {
		t.Fatalf("canceled reservation=%#v", canceled)
	}
	var auditCount int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_audit_events
		WHERE resource_id=$1 AND action='cancel_pending_device_reservation'`, created.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("cancel audit count=%d", auditCount)
	}
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
	assertStatus(t, environment.request(t, http.MethodPost, "/api/v1/device-reservations/"+created.ID+"/releases",
		map[string]any{"reason": "token=must-not-be-stored"}, serviceToken, "reservation-sensitive-release"), http.StatusBadRequest)
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

func TestReservationReleaseKeepsDatabaseActiveUntilSTFReleaseSucceeds(t *testing.T) {
	controller := &fakeSTFController{releaseErr: retryableSTFError{}}
	environment := newManagementEnvironment(t, controller)
	seedReservationDevice(t, environment)
	created := createAndActivateReservation(t, environment, controller, "reservation-stf-release-create")
	releaseBody := map[string]any{"reason": "STF release ordering test"}

	failed := environment.request(t, http.MethodPost, "/api/v1/device-reservations/"+created.ID+"/releases",
		releaseBody, serviceToken, "reservation-stf-release-key")
	assertStatus(t, failed, http.StatusBadGateway)
	stored, err := environment.reservations.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "active" {
		t.Fatalf("reservation status after STF failure=%s", stored.Status)
	}
	var auditCount int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_audit_events
		WHERE resource_id=$1 AND action='stf_release_failed'`, created.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("STF failure audit count=%d", auditCount)
	}

	controller.mutex.Lock()
	controller.releaseErr = nil
	controller.mutex.Unlock()
	succeeded := environment.request(t, http.MethodPost, "/api/v1/device-reservations/"+created.ID+"/releases",
		releaseBody, serviceToken, "reservation-stf-release-key")
	assertStatus(t, succeeded, http.StatusOK)
	stored, err = environment.reservations.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "released" || controller.lastReleaseSerial != "emulator-api-lifecycle" {
		t.Fatalf("reservation=%#v release serial=%q", stored, controller.lastReleaseSerial)
	}
}

func TestRemoteSessionIsOwnerBoundIdempotentAndDisconnectedAfterExpiry(t *testing.T) {
	controller := &fakeSTFController{}
	environment := newManagementEnvironment(t, controller)
	seedReservationDevice(t, environment)
	created := createAndActivateReservation(t, environment, controller, "reservation-remote-create")
	path := "/api/v1/device-reservations/" + created.ID + "/remote-sessions"
	input := map[string]any{
		"owner_type": "test_run", "owner_id": "attempt_000000000001", "ttl_seconds": 30,
	}
	response := environment.request(t, http.MethodPost, path, input, serviceToken, "remote-session-key-01")
	assertStatus(t, response, http.StatusCreated)
	var remote reservation.RemoteSessionView
	decodeData(t, response, &remote)
	if remote.URL != "10.0.0.20:7401" || remote.ReservationID != created.ID || remote.ID == "" {
		t.Fatalf("remote session=%#v", remote)
	}
	replayed := environment.request(t, http.MethodPost, path, input, serviceToken, "remote-session-key-01")
	assertStatus(t, replayed, http.StatusCreated)
	var replayedRemote reservation.RemoteSessionView
	decodeData(t, replayed, &replayedRemote)
	if replayedRemote.ID != remote.ID || controller.remoteCalls != 1 {
		t.Fatalf("replayed=%#v remote calls=%d", replayedRemote, controller.remoteCalls)
	}
	wrongOwner := map[string]any{
		"owner_type": "test_run", "owner_id": "attempt_000000000002", "ttl_seconds": 30,
	}
	assertStatus(t, environment.request(t, http.MethodPost, path, wrongOwner, serviceToken, "remote-session-key-02"), http.StatusForbidden)

	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_sessions
		SET connection_metadata=jsonb_set(connection_metadata,'{stf_remote_session,expires_at}',to_jsonb((clock_timestamp()-interval '1 second')::text))
		WHERE reservation_id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	reaped, err := environment.reservations.ReapRemoteSessionOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reaped.ID != remote.ID || controller.disconnectCalls != 1 {
		t.Fatalf("reaped=%#v disconnect calls=%d", reaped, controller.disconnectCalls)
	}
	var hasRemote bool
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT connection_metadata ? 'stf_remote_session'
		FROM device_sessions WHERE reservation_id=$1`, created.ID).Scan(&hasRemote); err != nil {
		t.Fatal(err)
	}
	if hasRemote {
		t.Fatal("expired remote session metadata was not cleared")
	}
}

func createAndActivateReservation(
	t *testing.T,
	environment *managementEnvironment,
	controller *fakeSTFController,
	key string,
) reservation.View {
	t.Helper()
	body := map[string]any{
		"pool_id": "pool_000000000000001", "owner_type": "test_run",
		"owner_id": "attempt_000000000001", "lease_seconds": 600,
		"requested_capabilities": map[string]any{"platformName": "Android", "apiLevel": 34},
	}
	createdResponse := environment.request(t, http.MethodPost, "/api/v1/device-reservations", body, serviceToken, key)
	assertStatus(t, createdResponse, http.StatusCreated)
	var created reservation.View
	decodeData(t, createdResponse, &created)
	if _, err := scheduler.New(environment.db, nil, nil, controller).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	return created
}

func TestAgentHealthEventAPIQuarantinesAfterThreshold(t *testing.T) {
	environment := newManagementEnvironment(t)
	seedReservationDevice(t, environment)
	path := "/internal/v1/devices/device_0000000000001/health-events"
	body := map[string]any{
		"source": "appium", "event_type": "appium_unhealthy", "severity": "error",
		"reason": "Appium status endpoint is unhealthy", "observed_at": "2026-08-03T12:00:00Z",
		"payload": map[string]any{"status_code": 503, "authorization": "Bearer health-event-secret",
			"message": "upstream password=health-password"},
	}
	assertStatus(t, environment.request(t, http.MethodPost, path, body, "", ""), http.StatusUnauthorized)
	assertStatus(t, environment.request(t, http.MethodPost, path, body, serviceToken, ""), http.StatusForbidden)
	for index := 0; index < 3; index++ {
		assertStatus(t, environment.request(t, http.MethodPost, path, body, agentToken, ""), http.StatusCreated)
	}
	var lifecycle, health string
	var failures int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT lifecycle_status,health_status,consecutive_failures
        FROM devices WHERE id='device_0000000000001'`).Scan(&lifecycle, &health, &failures); err != nil {
		t.Fatal(err)
	}
	if lifecycle != "quarantined" || health != "unhealthy" || failures != 3 {
		t.Fatalf("device lifecycle=%s health=%s failures=%d", lifecycle, health, failures)
	}
	var leaked int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_health_events
		WHERE payload::text LIKE '%health-event-secret%' OR payload::text LIKE '%health-password%'`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("health event secret rows=%d", leaked)
	}
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

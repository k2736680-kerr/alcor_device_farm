package iossession

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	stfadapter "github.com/Ad-Quanta/alcor-device-farm/internal/adapters/stf"
	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
)

const (
	testAgentToken    = "agent-token-ios-session-fence-0001"
	testHostID        = "ios_host_000000000001"
	testDeviceID      = "ios_device_000000001"
	testPoolID        = "ios_pool_000000000001"
	testReservation   = "ios_reservation_000001"
	testDeviceSession = "ios_session_0000000001"
	testOwnerID       = "ios_owner_00000000001"
	testUDID          = "00008110-IOS-UDID-0001"
)

func TestGrantLifecycleUsesOneReservationAndOneUDID(t *testing.T) {
	db := openIOSSessionTestDatabase(t)
	seedActiveIOSReservation(t, db, "http://127.0.0.1:4810", false)
	service := New(db, testAgentToken, fixedGrantGenerator(1))

	grant, err := service.Issue(context.Background(), audit.Service("ios-integration"), testReservation,
		"issue_ios_grant_0001", GrantInput{OwnerType: "test_run", OwnerID: testOwnerID, TTLSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if grant.UDID != testUDID || grant.HostID != testHostID || grant.SessionGrant == "" {
		t.Fatalf("unexpected grant: %#v", grant)
	}
	var storedHash string
	if err := db.Pool().QueryRow(context.Background(), `SELECT session_grant_hash FROM device_sessions WHERE id=$1`,
		testDeviceSession).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == grant.SessionGrant || len(storedHash) != 64 {
		t.Fatalf("plaintext Grant was persisted or hash is invalid: %q", storedHash)
	}

	request := validSessionRequest(testUDID)
	if _, err := service.Consume(context.Background(), ConsumeInput{HostID: "ios_host_000000000002",
		SessionGrant: grant.SessionGrant, Request: request}, "consume_wrong_host_0001"); !errors.Is(err, ErrRoutingMismatch) {
		t.Fatalf("cross-Host consume error=%v", err)
	}
	invalidRequest := json.RawMessage(`{"capabilities":{"alwaysMatch":{"platformName":"iOS","appium:udid":"` + testUDID + `","df:udids":"` + testUDID + `","filterByHost":"any"},"firstMatch":[{}]}}`)
	if _, err := service.Consume(context.Background(), ConsumeInput{HostID: testHostID,
		SessionGrant: grant.SessionGrant, Request: invalidRequest}, "consume_selector_0001"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("selector consume error=%v", err)
	}
	consumed, err := service.Consume(context.Background(), ConsumeInput{HostID: testHostID,
		SessionGrant: grant.SessionGrant, Request: request}, "consume_ios_grant_0001")
	if err != nil {
		t.Fatal(err)
	}
	if consumed.DeviceID != testDeviceID || consumed.UpstreamEndpoint != "http://127.0.0.1:4723" ||
		!bytes.Contains(consumed.Request, []byte(`"df:udids":"`+testUDID+`"`)) {
		t.Fatalf("unexpected pinned consume response: %#v", consumed)
	}
	if _, err := service.Consume(context.Background(), ConsumeInput{HostID: testHostID,
		SessionGrant: grant.SessionGrant, Request: request}, "consume_replay_0001"); !errors.Is(err, ErrGrantConsumed) {
		t.Fatalf("Grant replay error=%v", err)
	}

	binding := BindingInput{HostID: testHostID, SessionGrant: grant.SessionGrant, AppiumSessionID: "appium-session-0001"}
	if err := service.Bind(context.Background(), binding, "bind_ios_session_0001"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authorize(context.Background(), AuthorizationInput{HostID: testHostID,
		SessionGrant: grant.SessionGrant, AppiumSessionID: "other-session"}); !errors.Is(err, ErrRoutingMismatch) {
		t.Fatalf("wrong Session authorization error=%v", err)
	}
	authorized, err := service.Authorize(context.Background(), AuthorizationInput{HostID: testHostID,
		SessionGrant: grant.SessionGrant, AppiumSessionID: binding.AppiumSessionID})
	if err != nil || authorized.UpstreamEndpoint != consumed.UpstreamEndpoint {
		t.Fatalf("authorize=%#v error=%v", authorized, err)
	}
	if err := service.Close(context.Background(), binding, "close_ios_session_0001"); err != nil {
		t.Fatal(err)
	}
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_sessions WHERE id=$1
		AND appium_session_id='appium-session-0001' AND appium_session_ended_at IS NOT NULL`, testDeviceSession, 1)
}

func TestGrantExpiryAndProviderBusyDrift(t *testing.T) {
	db := openIOSSessionTestDatabase(t)
	seedActiveIOSReservation(t, db, "http://127.0.0.1:4810", false)
	service := New(db, testAgentToken, fixedGrantGenerator(2))
	grant, err := service.Issue(context.Background(), audit.Service("ios-integration"), testReservation,
		"issue_expiring_grant_0001", GrantInput{OwnerType: "test_run", OwnerID: testOwnerID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_sessions
		SET session_grant_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, testDeviceSession); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Consume(context.Background(), ConsumeInput{HostID: testHostID,
		SessionGrant: grant.SessionGrant, Request: validSessionRequest(testUDID)}, "consume_expired_0001"); !errors.Is(err, ErrGrantExpired) {
		t.Fatalf("expired Grant error=%v", err)
	}

	seedActiveIOSReservation(t, db, "http://127.0.0.1:4810", true)
	service = New(db, testAgentToken, fixedGrantGenerator(3))
	if _, err := service.Issue(context.Background(), audit.Service("ios-integration"), testReservation,
		"issue_busy_grant_0001", GrantInput{OwnerType: "test_run", OwnerID: testOwnerID}); !errors.Is(err, ErrProviderBusy) {
		t.Fatalf("provider-busy issue error=%v", err)
	}
	assertIOSSessionCount(t, db, `SELECT count(*) FROM devices WHERE id=$1
		AND lifecycle_status='quarantined' AND health_status='degraded'`, testDeviceID, 1)
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_health_events WHERE device_id=$1
		AND source='session_fence' AND event_type='ios_provider_busy_on_grant'`, testDeviceID, 1)
}

func TestRecentSessionCleanupBusyIsRetryableWithoutQuarantine(t *testing.T) {
	db := openIOSSessionTestDatabase(t)
	seedActiveIOSReservation(t, db, "http://127.0.0.1:4810", false)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO device_reservations
			(id,client_id,pool_id,device_id,owner_type,owner_id,requested_capabilities,lease_seconds,status,
			idempotency_key,starts_at,expires_at,released_at)
			VALUES ('ios_recent_reservation01','service',$1,$2,'test_run','ios_recent_owner_00001','{}',600,
			'released','ios-recent-reservation-key',clock_timestamp()-interval '2 minutes',
			clock_timestamp()-interval '1 minute',clock_timestamp())`, []any{testPoolID, testDeviceID}},
		{`INSERT INTO device_sessions
			(id,reservation_id,device_id,status,connection_metadata,started_at,ended_at,
			appium_session_id,appium_session_started_at,appium_session_ended_at)
			VALUES ('ios_recent_session00001','ios_recent_reservation01',$1,'closed','{}',
			clock_timestamp()-interval '1 minute',clock_timestamp(),'recent-appium-session',
			clock_timestamp()-interval '1 minute',clock_timestamp())`, []any{testDeviceID}},
		{`UPDATE devices SET capabilities=jsonb_set(capabilities,'{providerBusy}','true'::jsonb) WHERE id=$1`, []any{testDeviceID}},
	}
	for _, statement := range statements {
		if _, err := db.Pool().Exec(context.Background(), statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	service := New(db, testAgentToken, fixedGrantGenerator(9))
	if _, err := service.Issue(context.Background(), audit.Service("ios-integration"), testReservation,
		"issue_recent_cleanup_0001", GrantInput{OwnerType: "test_run", OwnerID: testOwnerID}); !errors.Is(err, ErrProviderBusyConverging) {
		t.Fatalf("清理收敛期没有返回可重试错误：%v", err)
	}
	assertIOSSessionCount(t, db, `SELECT count(*) FROM devices WHERE id=$1
		AND lifecycle_status='busy' AND health_status='healthy'`, testDeviceID, 1)
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_health_events WHERE device_id=$1
		AND event_type='ios_provider_busy_on_grant'`, testDeviceID, 0)
}

func TestReconcilerAuditsUnreservedDriftAsDevice(t *testing.T) {
	db := openIOSSessionTestDatabase(t)
	seedActiveIOSReservation(t, db, "http://127.0.0.1:4810", true)
	for _, statement := range []string{"DELETE FROM device_sessions WHERE device_id=$1", "DELETE FROM device_reservations WHERE device_id=$1",
		"UPDATE devices SET lifecycle_status='ready' WHERE id=$1"} {
		if _, err := db.Pool().Exec(context.Background(), statement, testDeviceID); err != nil {
			t.Fatal(err)
		}
	}
	service := New(db, testAgentToken)
	if err := service.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_audit_events WHERE resource_type='device'
		AND resource_id=$1 AND action='quarantine_ios_session_drift'`, testDeviceID, 1)
}

func TestReconcilerAllowsColdWDAStartupBeforeBinding(t *testing.T) {
	db := openIOSSessionTestDatabase(t)
	seedActiveIOSReservation(t, db, "http://127.0.0.1:4810", false)
	service := New(db, testAgentToken, fixedGrantGenerator(8))
	grant, err := service.Issue(context.Background(), audit.Service("ios-integration"), testReservation,
		"issue_cold_wda_grant_0001", GrantInput{OwnerType: "test_run", OwnerID: testOwnerID, TTLSeconds: 120})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Consume(context.Background(), ConsumeInput{HostID: testHostID,
		SessionGrant: grant.SessionGrant, Request: validSessionRequest(testUDID)}, "consume_cold_wda_0001"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE devices SET
		capabilities=jsonb_set(capabilities,'{providerBusy}','true'::jsonb) WHERE id=$1`, testDeviceID); err != nil {
		t.Fatal(err)
	}
	if err := service.ReconcileOnce(context.Background()); !errors.Is(err, ErrNothingToReconcile) {
		t.Fatalf("正常的 WDA 冷启动被过早判定为漂移：%v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_sessions SET
		session_grant_consumed_at=clock_timestamp()-interval '6 minutes' WHERE id=$1`, testDeviceSession); err != nil {
		t.Fatal(err)
	}
	if err := service.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("超过启动宽限期的未绑定 Session 没有被隔离：%v", err)
	}
	assertIOSSessionCount(t, db, `SELECT count(*) FROM devices WHERE id=$1
		AND lifecycle_status='quarantined' AND health_status='degraded'`, testDeviceID, 1)
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_health_events WHERE device_id=$1
		AND reason='IOS_PROVIDER_BUSY_WITHOUT_BOUND_SESSION'`, testDeviceID, 1)
}

func TestReservationReleaseClosesIOSSessionBeforeReleasing(t *testing.T) {
	var cleanupCalls atomic.Int32
	fence := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete || request.URL.Path != "/internal/v1/ios-session-fence/sessions/appium-session-release" ||
			request.Header.Get("Authorization") != "Bearer "+testAgentToken || request.Header.Get("X-Device-Farm-Host-Id") != testHostID {
			http.Error(writer, "unexpected cleanup request", http.StatusBadRequest)
			return
		}
		cleanupCalls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer fence.Close()

	db := openIOSSessionTestDatabase(t)
	seedActiveIOSReservation(t, db, fence.URL, false)
	iosService := New(db, testAgentToken, fixedGrantGenerator(4))
	bindTestSession(t, iosService, "appium-session-release")
	stf := &countingSTFController{}
	reservationService := reservation.NewService(db, nil, stf)
	reservationService.SetIOSSessionController(iosService)
	view, err := reservationService.Release(context.Background(), audit.Service("ios-integration"),
		"release_ios_session_0001", testReservation, "request_ios_release_0001", reservation.ReleaseInput{Reason: "iOS test complete"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "released" || cleanupCalls.Load() != 1 || stf.releases.Load() != 0 {
		t.Fatalf("release=%s cleanup=%d STF releases=%d", view.Status, cleanupCalls.Load(), stf.releases.Load())
	}
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_sessions WHERE reservation_id=$1
		AND status='closed' AND appium_session_ended_at IS NOT NULL`, testReservation, 1)
	if _, err := db.Pool().Exec(context.Background(), `UPDATE devices SET lifecycle_status='ready',health_status='healthy',
		capabilities=jsonb_set(capabilities,'{providerBusy}','true'::jsonb) WHERE id=$1`, testDeviceID); err != nil {
		t.Fatal(err)
	}
	if err := iosService.ReconcileOnce(context.Background()); !errors.Is(err, ErrNothingToReconcile) {
		t.Fatalf("recent cleanup heartbeat lag was treated as drift: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_sessions SET
		appium_session_started_at=clock_timestamp()-interval '1 minute',
		appium_session_ended_at=clock_timestamp()-interval '45 seconds' WHERE reservation_id=$1`, testReservation); err != nil {
		t.Fatal(err)
	}
	if err := iosService.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("stale providerBusy drift was not quarantined: %v", err)
	}
	assertIOSSessionCount(t, db, `SELECT count(*) FROM devices WHERE id=$1
		AND lifecycle_status='quarantined' AND health_status='degraded'`, testDeviceID, 1)
}

func TestReservationReaperClosesIOSSessionBeforeExpiry(t *testing.T) {
	var cleanupCalls atomic.Int32
	fence := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cleanupCalls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer fence.Close()

	db := openIOSSessionTestDatabase(t)
	seedActiveIOSReservation(t, db, fence.URL, false)
	iosService := New(db, testAgentToken, fixedGrantGenerator(7))
	bindTestSession(t, iosService, "appium-session-reaper")
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_reservations SET
		starts_at=clock_timestamp()-interval '12 minutes',expires_at=clock_timestamp()-interval '2 minutes'
		WHERE id=$1`, testReservation); err != nil {
		t.Fatal(err)
	}
	reservationService := reservation.NewService(db, nil)
	reservationService.SetIOSSessionController(iosService)
	view, err := reservationService.ReapOnce(context.Background(), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "expired" || cleanupCalls.Load() != 1 {
		t.Fatalf("reaped status=%s cleanup=%d", view.Status, cleanupCalls.Load())
	}
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_sessions WHERE reservation_id=$1
		AND status='closed' AND appium_session_ended_at IS NOT NULL`, testReservation, 1)
}

func TestReservationExpiryAndSessionReconcilerConvergeWithoutQuarantine(t *testing.T) {
	fence := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer fence.Close()

	db := openIOSSessionTestDatabase(t)
	seedActiveIOSReservation(t, db, fence.URL, false)
	iosService := New(db, testAgentToken, fixedGrantGenerator(8))
	bindTestSession(t, iosService, "appium-session-expiry-race")
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_sessions
		SET appium_session_started_at=clock_timestamp()-interval '2 minutes' WHERE id=$1`, testDeviceSession); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_reservations
		SET starts_at=clock_timestamp()-interval '3 minutes',
			expires_at=clock_timestamp()-interval '2 minutes' WHERE id=$1`, testReservation); err != nil {
		t.Fatal(err)
	}

	reservationService := reservation.NewService(db, nil)
	reservationService.SetIOSSessionController(iosService)
	start := make(chan struct{})
	errorsSeen := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		err := iosService.ReconcileOnce(context.Background())
		if !errors.Is(err, ErrNothingToReconcile) {
			errorsSeen <- err
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		_, err := reservationService.ReapOnce(context.Background(), 30*time.Second)
		if err != nil {
			errorsSeen <- err
		}
	}()
	close(start)
	workers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("expiry reconciliation failed: %v", err)
	}
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_reservations WHERE id=$1 AND status='expired'`, testReservation, 1)
	assertIOSSessionCount(t, db, `SELECT count(*) FROM devices WHERE id=$1 AND lifecycle_status='ready'
		AND health_status='healthy'`, testDeviceID, 1)
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_health_events WHERE device_id=$1
		AND reason='IOS_BOUND_SESSION_NOT_BUSY'`, testDeviceID, 0)
}

func TestCleanupFailureKeepsReservationActiveAndQuarantinesDevice(t *testing.T) {
	fence := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "injected failure", http.StatusInternalServerError)
	}))
	defer fence.Close()

	db := openIOSSessionTestDatabase(t)
	seedActiveIOSReservation(t, db, fence.URL, false)
	iosService := New(db, testAgentToken, fixedGrantGenerator(5))
	bindTestSession(t, iosService, "appium-session-failure")
	reservationService := reservation.NewService(db, nil)
	reservationService.SetIOSSessionController(iosService)
	_, err := reservationService.Release(context.Background(), audit.Service("ios-integration"),
		"release_ios_failure_0001", testReservation, "request_ios_failure_0001", reservation.ReleaseInput{Reason: "iOS test complete"})
	if !errors.Is(err, reservation.ErrIOSSessionCleanup) {
		t.Fatalf("cleanup failure error=%v", err)
	}
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_reservations WHERE id=$1 AND status='active'`, testReservation, 1)
	assertIOSSessionCount(t, db, `SELECT count(*) FROM devices WHERE id=$1 AND lifecycle_status='quarantined'`, testDeviceID, 1)
	assertIOSSessionCount(t, db, `SELECT count(*) FROM device_sessions WHERE id=$1 AND appium_session_ended_at IS NULL`, testDeviceSession, 1)
}

func TestDatabaseRejectsDuplicateGrantAndActiveAppiumSessionPerDevice(t *testing.T) {
	db := openIOSSessionTestDatabase(t)
	seedActiveIOSReservation(t, db, "http://127.0.0.1:4810", false)
	service := New(db, testAgentToken, fixedGrantGenerator(6))
	bindTestSession(t, service, "appium-session-unique")
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_reservations
		(id,client_id,pool_id,device_id,owner_type,owner_id,requested_capabilities,lease_seconds,status,
		idempotency_key,starts_at,expires_at,released_at)
		VALUES ('ios_reservation_closed01','service',$1,$2,'test_run','ios_owner_closed00001','{}',600,
		'released','ios-reservation-closed-key',clock_timestamp()-interval '2 minutes',
		clock_timestamp()-interval '1 minute',clock_timestamp())`, testPoolID, testDeviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_sessions
		(id,reservation_id,device_id,status,started_at,connection_metadata)
		VALUES ('ios_session_closed00001','ios_reservation_closed01',$1,'active',clock_timestamp(),'{}')`, testDeviceID); err != nil {
		t.Fatal(err)
	}
	var grantHash string
	if err := db.Pool().QueryRow(context.Background(), `SELECT session_grant_hash FROM device_sessions WHERE id=$1`,
		testDeviceSession).Scan(&grantHash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_sessions SET session_grant_hash=$2,
		session_grant_expires_at=clock_timestamp()+interval '1 minute' WHERE id=$1`, "ios_session_closed00001", grantHash); err == nil {
		t.Fatal("duplicate Session Grant hash was accepted")
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_sessions SET appium_session_id='appium-session-second',
		appium_session_started_at=clock_timestamp() WHERE id=$1`, "ios_session_closed00001"); err == nil {
		t.Fatal("second active Appium Session for one Device was accepted")
	}
}

func bindTestSession(t *testing.T, service *Service, appiumSessionID string) {
	t.Helper()
	grant, err := service.Issue(context.Background(), audit.Service("ios-integration"), testReservation,
		"issue_for_binding_0001", GrantInput{OwnerType: "test_run", OwnerID: testOwnerID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Consume(context.Background(), ConsumeInput{HostID: testHostID,
		SessionGrant: grant.SessionGrant, Request: validSessionRequest(testUDID)}, "consume_for_binding_0001"); err != nil {
		t.Fatal(err)
	}
	if err := service.Bind(context.Background(), BindingInput{HostID: testHostID, SessionGrant: grant.SessionGrant,
		AppiumSessionID: appiumSessionID}, "bind_for_cleanup_0001"); err != nil {
		t.Fatal(err)
	}
}

func validSessionRequest(udid string) json.RawMessage {
	return json.RawMessage(`{"capabilities":{"alwaysMatch":{"platformName":"iOS","appium:automationName":"XCUITest","appium:udid":"` +
		udid + `","df:udids":"` + udid + `"},"firstMatch":[{}]}}`)
}

func fixedGrantGenerator(value byte) TokenGenerator {
	return func() (string, error) {
		return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32)), nil
	}
}

func openIOSSessionTestDatabase(t *testing.T) *database.DB {
	t.Helper()
	databaseURL := os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEVICE_FARM_TEST_DATABASE_URL is not set")
	}
	db, err := database.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func seedActiveIOSReservation(t *testing.T, db *database.DB, fenceEndpoint string, providerBusy bool) {
	t.Helper()
	capabilities, err := json.Marshal(map[string]any{"session_fence_endpoint": fenceEndpoint})
	if err != nil {
		t.Fatal(err)
	}
	deviceCapabilities, err := json.Marshal(map[string]any{"platformName": "iOS", "providerBusy": providerBusy})
	if err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`TRUNCATE TABLE device_idempotency_records,device_audit_events,device_health_events,device_sessions,
			device_reservations,device_pool_devices,devices,device_pool_images,device_pools,
			device_host_commands,device_hosts,device_images RESTART IDENTITY CASCADE`, nil},
		{`INSERT INTO device_hosts (id,name,host_type,host_os,host_arch,capabilities,status,draining)
			VALUES ($1,'ios-session-macos-host','appium_device_farm_ios','macos','arm64',$2::jsonb,'online',false)`, []any{testHostID, capabilities}},
		{`INSERT INTO device_pools (id,name,platform,default_lease_seconds,max_lease_seconds,max_concurrency,status)
			VALUES ($1,'ios-session-pool','ios',600,3600,1,'active')`, []any{testPoolID}},
		{`INSERT INTO devices (id,host_id,platform,device_kind,provider_type,provider_ref,lifecycle_mode,
			serial,appium_endpoint,capabilities,lifecycle_status,health_status)
			VALUES ($1,$2,'ios','simulator','appium_device_farm_ios','ios-provider-session-1','rebuild',
			$3,'http://127.0.0.1:4723',$4::jsonb,'busy','healthy')`, []any{testDeviceID, testHostID, testUDID, deviceCapabilities}},
		{`INSERT INTO device_pool_devices (pool_id,device_id,enabled) VALUES ($1,$2,true)`, []any{testPoolID, testDeviceID}},
		{`INSERT INTO device_reservations (id,client_id,pool_id,device_id,owner_type,owner_id,
			requested_capabilities,lease_seconds,status,idempotency_key,starts_at,expires_at)
			VALUES ($1,'service',$2,$3,'test_run',$4,'{"platformName":"iOS"}'::jsonb,600,'active',
			'ios-session-reservation-key',clock_timestamp(),clock_timestamp()+interval '10 minutes')`, []any{testReservation, testPoolID, testDeviceID, testOwnerID}},
		{`INSERT INTO device_sessions (id,reservation_id,device_id,status,started_at,connection_metadata)
			VALUES ($1,$2,$3,'active',clock_timestamp(),jsonb_build_object('platform','ios','host_id',$4::text,
			'appium_endpoint','http://127.0.0.1:4723','appium_udid',$5::text))`, []any{testDeviceSession, testReservation, testDeviceID, testHostID, testUDID}},
	}
	for _, statement := range statements {
		if _, err := db.Pool().Exec(context.Background(), statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}

func assertIOSSessionCount(t *testing.T, db *database.DB, query, id string, want int) {
	t.Helper()
	var got int
	if err := db.Pool().QueryRow(context.Background(), query, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count=%d want=%d: %s", got, want, query)
	}
}

type countingSTFController struct{ releases atomic.Int32 }

func (controller *countingSTFController) Release(context.Context, string) error {
	controller.releases.Add(1)
	return nil
}
func (*countingSTFController) RemoteConnect(context.Context, string) (stfadapter.RemoteConnection, error) {
	return stfadapter.RemoteConnection{}, nil
}
func (*countingSTFController) RemoteDisconnect(context.Context, string) error { return nil }

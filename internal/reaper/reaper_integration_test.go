package reaper_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reaper"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/scheduler"
)

func TestExtensionUsesDatabaseLeaseAndIsIdempotent(t *testing.T) {
	db := openTestDatabase(t)
	service, active := seedActiveReservation(t, db, "extend")
	if active.ExpiresAt == nil {
		t.Fatal("active reservation has no expiry")
	}
	originalExpiry := *active.ExpiresAt
	extended, err := service.Extend(context.Background(), audit.Service("service"), "extension-key-0001", active.ID, reservation.ExtensionInput{AdditionalSeconds: 300})
	if err != nil {
		t.Fatal(err)
	}
	if extended.ExpiresAt == nil || !extended.ExpiresAt.Equal(originalExpiry.Add(300*time.Second)) {
		t.Fatalf("extended expiry=%v want=%v", extended.ExpiresAt, originalExpiry.Add(300*time.Second))
	}
	replayed, err := service.Extend(context.Background(), audit.Service("service"), "extension-key-0001", active.ID, reservation.ExtensionInput{AdditionalSeconds: 300})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ExpiresAt == nil || !replayed.ExpiresAt.Equal(*extended.ExpiresAt) {
		t.Fatalf("replay extended twice: first=%v replay=%v", extended.ExpiresAt, replayed.ExpiresAt)
	}
	if _, err := service.Extend(context.Background(), audit.Service("service"), "extension-key-0001", active.ID, reservation.ExtensionInput{AdditionalSeconds: 301}); !errors.Is(err, reservation.ErrConflict) {
		t.Fatalf("changed idempotent extension error=%v", err)
	}
	if _, err := service.Extend(context.Background(), audit.Service("service"), "extension-key-0002", active.ID, reservation.ExtensionInput{AdditionalSeconds: 1201}); !errors.Is(err, reservation.ErrInvalidArgument) {
		t.Fatalf("over maximum extension error=%v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_reservations
        SET starts_at=clock_timestamp()-interval '700 seconds',expires_at=clock_timestamp()-interval '1 second'
        WHERE id=$1`, active.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Extend(context.Background(), audit.Service("service"), "extension-key-0003", active.ID, reservation.ExtensionInput{AdditionalSeconds: 60}); !errors.Is(err, reservation.ErrConflict) {
		t.Fatalf("expired extension error=%v", err)
	}
}

func TestConcurrentReleaseClosesOnlyOnce(t *testing.T) {
	db := openTestDatabase(t)
	service, active := seedActiveReservation(t, db, "release")
	start := make(chan struct{})
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			<-start
			_, err := service.Release(context.Background(), audit.Service("service"), fmt.Sprintf("release-key-%08d", index), active.ID,
				fmt.Sprintf("request_release_%d", index), reservation.ReleaseInput{Reason: "test run completed"})
			results <- err
		}()
	}
	close(start)
	for index := 0; index < 2; index++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	assertStateCounts(t, db, active.ID, "released")
	assertCount(t, db, "SELECT count(*) FROM device_audit_events WHERE resource_id=$1", active.ID, 1)
	if _, err := service.Release(context.Background(), audit.Service("service"), "release-key-00000000", active.ID,
		"request_release_replay", reservation.ReleaseInput{Reason: "test run completed"}); err != nil {
		t.Fatal(err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_audit_events WHERE resource_id=$1", active.ID, 1)
}

func TestForceReleaseWritesReasonedAudit(t *testing.T) {
	db := openTestDatabase(t)
	service, active := seedActiveReservation(t, db, "force")
	value, err := service.Release(context.Background(), audit.Service("service"), "force-release-key", active.ID,
		"request_force_release", reservation.ReleaseInput{Reason: "operator stopped unsafe session", Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != "force_released" {
		t.Fatalf("force release status=%s", value.Status)
	}
	var action, reason string
	if err := db.Pool().QueryRow(context.Background(), `
        SELECT action,reason FROM device_audit_events WHERE resource_id=$1`, active.ID).Scan(&action, &reason); err != nil {
		t.Fatal(err)
	}
	if action != "force_release_device_reservation" || reason != "operator stopped unsafe session" {
		t.Fatalf("audit action=%s reason=%s", action, reason)
	}
}

func TestTwoReapersCloseExpiredReservationOnce(t *testing.T) {
	db := openTestDatabase(t)
	service, active := seedActiveReservation(t, db, "expired")
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_reservations
        SET starts_at=clock_timestamp()-interval '700 seconds',expires_at=clock_timestamp()-interval '31 seconds'
        WHERE id=$1`, active.ID); err != nil {
		t.Fatal(err)
	}
	first := reaper.New(service, 30*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	second := reaper.New(service, 30*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, worker := range []*reaper.Reaper{first, second} {
		worker := worker
		go func() {
			<-start
			_, err := worker.RunOnce(context.Background())
			results <- err
		}()
	}
	close(start)
	successes, empty := 0, 0
	for index := 0; index < 2; index++ {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, reservation.ErrNothingToReap):
			empty++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || empty != 1 {
		t.Fatalf("reaper successes=%d empty=%d", successes, empty)
	}
	assertStateCounts(t, db, active.ID, "expired")
	assertCount(t, db, "SELECT count(*) FROM device_audit_events WHERE resource_id=$1", active.ID, 1)
}

func TestReaperHonorsGracePeriod(t *testing.T) {
	db := openTestDatabase(t)
	service, active := seedActiveReservation(t, db, "grace")
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_reservations
        SET starts_at=clock_timestamp()-interval '700 seconds',expires_at=clock_timestamp()-interval '10 seconds'
        WHERE id=$1`, active.ID); err != nil {
		t.Fatal(err)
	}
	worker := reaper.New(service, 30*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := worker.RunOnce(context.Background()); !errors.Is(err, reservation.ErrNothingToReap) {
		t.Fatalf("reaper before grace error=%v", err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_reservations WHERE id=$1 AND status='active'", active.ID, 1)
}

func seedActiveReservation(t *testing.T, db *database.DB, suffix string) (*reservation.Service, reservation.View) {
	t.Helper()
	resetAndSeed(t, db)
	service := reservation.NewService(db, nil)
	value, err := service.Create(context.Background(), audit.Service("service"), "create-active-"+suffix, reservation.CreateInput{
		PoolID: "pool_000000000000001", OwnerType: "test_run", OwnerID: "attempt_000000000001",
		RequestedCapabilities: map[string]any{"platformName": "Android", "apiLevel": 34}, LeaseSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	deviceScheduler := scheduler.New(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := deviceScheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, err := service.Get(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	return service, active
}

func openTestDatabase(t *testing.T) *database.DB {
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

func resetAndSeed(t *testing.T, db *database.DB) {
	t.Helper()
	statements := []string{
		`TRUNCATE TABLE device_idempotency_records,device_audit_events,device_health_events,device_sessions,
            device_reservations,device_pool_devices,devices,device_pool_images,device_pools,
            device_host_commands,device_hosts,device_images RESTART IDENTITY CASCADE`,
		`INSERT INTO device_hosts (id,name,host_type,status,draining)
            VALUES ('host_000000000000001','lease-host','docker_emulator','online',false)`,
		`INSERT INTO device_images (id,name,docker_image,docker_digest,api_level,abi,resolution,status)
			VALUES ('image_00000000000001','lease-image','registry.example/alcor/android-emulator:api34','sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',34,'x86_64','1080x2400','ready')`,
		`INSERT INTO device_pools (id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
            VALUES ('pool_000000000000001','lease-pool',600,1200,1,'active')`,
		`INSERT INTO devices (id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,
            serial,adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status)
            VALUES ('device_0000000000001','host_000000000000001','image_00000000000001','emulator','mock',
            'mock-lease-device','rebuild','emulator-lease','127.0.0.1:5555','http://127.0.0.1:4723',
            '{"platformName":"Android","apiLevel":34}'::jsonb,'ready','healthy')`,
		`INSERT INTO device_pool_devices (pool_id,device_id,enabled)
            VALUES ('pool_000000000000001','device_0000000000001',true)`,
	}
	for _, statement := range statements {
		if _, err := db.Pool().Exec(context.Background(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

func assertStateCounts(t *testing.T, db *database.DB, reservationID, status string) {
	t.Helper()
	assertCount(t, db, "SELECT count(*) FROM device_reservations WHERE id=$1 AND status='"+status+"'", reservationID, 1)
	assertCount(t, db, "SELECT count(*) FROM device_sessions WHERE reservation_id=$1 AND status='closed' AND ended_at IS NOT NULL", reservationID, 1)
	// DF-038 keeps long-lived devices and their data volume after a reservation.
	// Release closes access only; factory reset remains an explicit rebuild action.
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='ready' AND health_status='healthy'", "", 1)
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='recycling'", "", 0)
}

func assertCount(t *testing.T, db *database.DB, query, argument string, want int) {
	t.Helper()
	var got int
	var err error
	if argument == "" {
		err = db.Pool().QueryRow(context.Background(), query).Scan(&got)
	} else {
		err = db.Pool().QueryRow(context.Background(), query, argument).Scan(&got)
	}
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count=%d want=%d: %s", got, want, query)
	}
}

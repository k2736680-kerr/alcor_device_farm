package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/jackc/pgx/v5"
)

func TestReservationIdempotencyReturnsSameResource(t *testing.T) {
	db := openTestDatabase(t)
	resetDatabase(t, db)
	seedPool(t, db)
	repository := ReservationRepository{}
	params := testReservationParams("reservation_0000000001")

	first, err := repository.CreatePending(context.Background(), db.Pool(), params)
	if err != nil {
		t.Fatal(err)
	}
	params.ID = "reservation_0000000002"
	second, err := repository.CreatePending(context.Background(), db.Pool(), params)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent IDs differ: %s != %s", first.ID, second.ID)
	}

	params.OwnerID = "owner_00000000000002"
	if _, err := repository.CreatePending(context.Background(), db.Pool(), params); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different payload error = %v", err)
	}
}

func TestConcurrentReservationIdempotencyReturnsOneResource(t *testing.T) {
	db := openTestDatabase(t)
	resetDatabase(t, db)
	seedPool(t, db)
	repository := ReservationRepository{}
	const workers = 12
	start := make(chan struct{})
	results := make(chan string, workers)
	errorsChannel := make(chan error, workers)

	for index := 0; index < workers; index++ {
		index := index
		go func() {
			<-start
			params := testReservationParams(fmt.Sprintf("reservation_%014d", index))
			record, err := repository.CreatePending(context.Background(), db.Pool(), params)
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- record.ID
		}()
	}
	close(start)

	var expectedID string
	for index := 0; index < workers; index++ {
		select {
		case err := <-errorsChannel:
			t.Fatal(err)
		case id := <-results:
			if expectedID == "" {
				expectedID = id
			}
			if id != expectedID {
				t.Fatalf("concurrent idempotency returned %s and %s", expectedID, id)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent idempotency timed out")
		}
	}

	var count int
	if err := db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM device_reservations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reservation count = %d, want 1", count)
	}
}

func TestConcurrentReservationLockClaimsOnlyOnce(t *testing.T) {
	db := openTestDatabase(t)
	resetDatabase(t, db)
	seedPool(t, db)
	repository := ReservationRepository{}
	if _, err := repository.CreatePending(context.Background(), db.Pool(), testReservationParams("reservation_0000000001")); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() {
			tx, err := db.Pool().Begin(context.Background())
			if err != nil {
				results <- err
				return
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			ready <- struct{}{}
			<-start
			record, err := repository.LockNextPending(context.Background(), tx)
			if err != nil {
				results <- err
				return
			}
			if err := repository.MarkFailed(context.Background(), tx, record.ID, "TEST_CLAIMED"); err != nil {
				results <- err
				return
			}
			time.Sleep(100 * time.Millisecond)
			results <- tx.Commit(context.Background())
		}()
	}
	<-ready
	<-ready
	close(start)

	successes, notFound := 0, 0
	for index := 0; index < 2; index++ {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrNotFound):
			notFound++
		default:
			t.Fatalf("claim error = %v", err)
		}
	}
	if successes != 1 || notFound != 1 {
		t.Fatalf("successes=%d notFound=%d, want 1/1", successes, notFound)
	}
}

func TestConcurrentCommandClaimLeasesOnlyOnce(t *testing.T) {
	db := openTestDatabase(t)
	resetDatabase(t, db)
	seedHost(t, db)
	repository := CommandRepository{}
	_, err := repository.Create(context.Background(), db.Pool(), CreateCommandParams{
		ID: "command_000000000001", HostID: "host_000000000000001", CommandType: "inspect",
		Payload: map[string]any{"device_id": "device_0000000000001"}, MaxAttempts: 3,
		IdempotencyKey: "command-idempotency-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			<-start
			_, err := repository.ClaimNext(
				context.Background(), db.Pool(), "host_000000000000001",
				fmt.Sprintf("lease_token_%016d", index), time.Minute,
			)
			results <- err
		}()
	}
	close(start)

	successes, notFound := 0, 0
	for index := 0; index < 2; index++ {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrNotFound):
			notFound++
		default:
			t.Fatalf("command claim error = %v", err)
		}
	}
	if successes != 1 || notFound != 1 {
		t.Fatalf("successes=%d notFound=%d, want 1/1", successes, notFound)
	}
}

func TestCommandIdempotencyReturnsSameResource(t *testing.T) {
	db := openTestDatabase(t)
	resetDatabase(t, db)
	seedHost(t, db)
	repository := CommandRepository{}
	params := CreateCommandParams{
		ID: "command_000000000001", HostID: "host_000000000000001", CommandType: "inspect",
		Payload: map[string]any{"device_id": "device_0000000000001"}, MaxAttempts: 3,
		IdempotencyKey: "command-idempotency-1",
	}
	first, err := repository.Create(context.Background(), db.Pool(), params)
	if err != nil {
		t.Fatal(err)
	}
	params.ID = "command_000000000002"
	second, err := repository.Create(context.Background(), db.Pool(), params)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent command IDs differ: %s != %s", first.ID, second.ID)
	}
	params.Payload = map[string]any{"device_id": "different_device_0001"}
	if _, err := repository.Create(context.Background(), db.Pool(), params); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different command payload error = %v", err)
	}
}

func TestTransactionRollbackLeavesNoPartialReservation(t *testing.T) {
	db := openTestDatabase(t)
	resetDatabase(t, db)
	seedDeviceGraph(t, db)
	reservationRepository := ReservationRepository{}
	deviceRepository := DeviceRepository{}
	rollbackError := errors.New("force rollback")

	err := db.WithinTx(context.Background(), func(tx pgx.Tx) error {
		if _, err := reservationRepository.CreatePending(context.Background(), tx, testReservationParams("reservation_0000000001")); err != nil {
			return err
		}
		if err := deviceRepository.UpdateLifecycle(
			context.Background(), tx, "device_0000000000001", domain.DeviceReady, domain.DeviceReserved,
		); err != nil {
			return err
		}
		return rollbackError
	})
	if !errors.Is(err, rollbackError) {
		t.Fatalf("WithinTx() error = %v", err)
	}

	var reservationCount int
	if err := db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM device_reservations").Scan(&reservationCount); err != nil {
		t.Fatal(err)
	}
	var lifecycle domain.DeviceLifecycleStatus
	if err := db.Pool().QueryRow(
		context.Background(), "SELECT lifecycle_status FROM devices WHERE id=$1", "device_0000000000001",
	).Scan(&lifecycle); err != nil {
		t.Fatal(err)
	}
	if reservationCount != 0 || lifecycle != domain.DeviceReady {
		t.Fatalf("rollback left reservationCount=%d lifecycle=%s", reservationCount, lifecycle)
	}
}

func TestDatabaseClock(t *testing.T) {
	db := openTestDatabase(t)
	before := time.Now().Add(-time.Second)
	now, err := database.ClockNow(context.Background(), db.Pool())
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().Add(time.Second)
	if now.Before(before) || now.After(after) {
		t.Fatalf("database clock %s is outside test window", now)
	}
}

func openTestDatabase(t *testing.T) *database.DB {
	t.Helper()
	databaseURL := os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEVICE_FARM_TEST_DATABASE_URL is not set")
	}
	db, err := database.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func resetDatabase(t *testing.T, db *database.DB) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(), `
        TRUNCATE TABLE
            device_audit_events, device_health_events, device_sessions,
            device_reservations, device_pool_devices, devices, device_pool_images,
            device_pools, device_host_commands, device_hosts, device_images
        RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset database: %v", err)
	}
}

func seedPool(t *testing.T, db *database.DB) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(), `
        INSERT INTO device_pools (id, name, default_lease_seconds, max_lease_seconds)
        VALUES ('pool_000000000000001', 'repository-test-pool', 1800, 7200)`)
	if err != nil {
		t.Fatalf("seed pool: %v", err)
	}
}

func seedHost(t *testing.T, db *database.DB) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(), `
        INSERT INTO device_hosts (id, name, host_type, status)
        VALUES ('host_000000000000001', 'repository-test-host', 'docker_emulator', 'online')`)
	if err != nil {
		t.Fatalf("seed host: %v", err)
	}
}

func seedDeviceGraph(t *testing.T, db *database.DB) {
	t.Helper()
	seedPool(t, db)
	seedHost(t, db)
	_, err := db.Pool().Exec(context.Background(), `
        INSERT INTO device_images (id, name, docker_digest, api_level, abi, resolution, status)
        VALUES (
            'image_00000000000001', 'repository-test-image',
            'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
            34, 'x86_64', '1080x2400', 'ready'
        );
        INSERT INTO devices (
            id, host_id, image_id, device_kind, provider_type, provider_ref,
            lifecycle_mode, serial, lifecycle_status, health_status
        ) VALUES (
            'device_0000000000001', 'host_000000000000001', 'image_00000000000001',
            'emulator', 'docker_emulator', 'repository-container-1', 'rebuild',
            'emulator-repository-1', 'ready', 'healthy'
        )`)
	if err != nil {
		t.Fatalf("seed device graph: %v", err)
	}
}

func testReservationParams(id string) CreateReservationParams {
	return CreateReservationParams{
		ID: id, ClientID: "repository-test-client", PoolID: "pool_000000000000001",
		OwnerType: "test_run", OwnerID: "owner_00000000000001",
		RequestedCapabilities: map[string]any{"apiLevel": 34, "platformName": "Android"},
		LeaseSeconds:          1800, IdempotencyKey: "reservation-idempotency-1",
	}
}

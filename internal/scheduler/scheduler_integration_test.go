package scheduler_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/scheduler"
)

type fakeClaimer struct {
	claim        func(context.Context, string, time.Duration) error
	releaseCalls atomic.Int64
}

func (claimer *fakeClaimer) Claim(ctx context.Context, serial string, ttl time.Duration) error {
	if claimer.claim == nil {
		return nil
	}
	return claimer.claim(ctx, serial, ttl)
}

func (claimer *fakeClaimer) Release(context.Context, string) error {
	claimer.releaseCalls.Add(1)
	return nil
}

type claimFailure struct{ retryable bool }

func (failure claimFailure) Error() string     { return "simulated STF claim failure" }
func (failure claimFailure) IsRetryable() bool { return failure.retryable }

func TestOneHundredConcurrentReservationsUseTwoDevicesWithoutDoubleAllocation(t *testing.T) {
	db := openTestDatabase(t)
	resetAndSeed(t, db, 2)
	reservationService := reservation.NewService(db, nil)
	deviceScheduler := scheduler.New(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	const requests = 100
	start := make(chan struct{})
	errorsChannel := make(chan error, requests)
	var waitGroup sync.WaitGroup
	for index := 0; index < requests; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, err := reservationService.Create(context.Background(), audit.Service("service"), fmt.Sprintf("reservation-key-%03d", index), reservation.CreateInput{
				PoolID: "pool_000000000000001", OwnerType: "test_run",
				OwnerID:               fmt.Sprintf("owner_%019d", index),
				RequestedCapabilities: map[string]any{"platformName": "Android", "apiLevel": 34},
				LeaseSeconds:          600,
			})
			if err != nil {
				errorsChannel <- err
				return
			}
			_, err = deviceScheduler.RunOnce(context.Background())
			if err != nil && !errors.Is(err, scheduler.ErrCapacityUnavailable) && !errors.Is(err, scheduler.ErrNoPendingReservation) {
				errorsChannel <- err
			}
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatal(err)
	}

	assertCount(t, db, "SELECT count(*) FROM device_reservations", 100)
	assertCount(t, db, "SELECT count(*) FROM device_reservations WHERE status='active'", 2)
	assertCount(t, db, "SELECT count(*) FROM device_reservations WHERE status='pending'", 98)
	assertCount(t, db, "SELECT count(DISTINCT device_id) FROM device_reservations WHERE status='active'", 2)
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='busy'", 2)
	assertCount(t, db, "SELECT count(*) FROM device_sessions WHERE status='active'", 2)
	assertCount(t, db, `SELECT count(*) FROM device_sessions s
        JOIN device_reservations r ON r.id=s.reservation_id AND r.device_id=s.device_id
        WHERE r.status='active'`, 2)
	assertCount(t, db, `SELECT count(*) FROM device_sessions
		WHERE connection_metadata->>'appium_udid'=connection_metadata->>'serial'`, 2)
}

func TestConcurrentIdempotencyCreatesOneReservation(t *testing.T) {
	db := openTestDatabase(t)
	resetAndSeed(t, db, 2)
	service := reservation.NewService(db, nil)
	const requests = 20
	start := make(chan struct{})
	ids := make(chan string, requests)
	errorsChannel := make(chan error, requests)
	for index := 0; index < requests; index++ {
		go func() {
			<-start
			value, err := service.Create(context.Background(), audit.Service("service"), "same-reservation-key", reservation.CreateInput{
				PoolID: "pool_000000000000001", OwnerType: "manual", OwnerID: "owner_00000000000001",
				RequestedCapabilities: map[string]any{"platformName": "Android"}, LeaseSeconds: 600,
			})
			if err != nil {
				errorsChannel <- err
				return
			}
			ids <- value.ID
		}()
	}
	close(start)
	var expected string
	for index := 0; index < requests; index++ {
		select {
		case err := <-errorsChannel:
			t.Fatal(err)
		case id := <-ids:
			if expected == "" {
				expected = id
			}
			if id != expected {
				t.Fatalf("idempotency returned IDs %s and %s", expected, id)
			}
		}
	}
	assertCount(t, db, "SELECT count(*) FROM device_reservations", 1)
}

func TestCapabilityMismatchRemainsPendingWithoutBlockingMatchedRequest(t *testing.T) {
	db := openTestDatabase(t)
	resetAndSeed(t, db, 2)
	service := reservation.NewService(db, nil)
	deviceScheduler := scheduler.New(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	unmatched, err := service.Create(context.Background(), audit.Service("service"), "unmatched-capability-key", reservation.CreateInput{
		PoolID: "pool_000000000000001", OwnerType: "run_attempt", OwnerID: "attempt_000000000001",
		RequestedCapabilities: map[string]any{"platformName": "Android", "apiLevel": 35}, LeaseSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	matched, err := service.Create(context.Background(), audit.Service("service"), "matched-capability-key", reservation.CreateInput{
		PoolID: "pool_000000000000001", OwnerType: "run_attempt", OwnerID: "attempt_000000000002",
		RequestedCapabilities: map[string]any{"platformName": "Android", "apiLevel": 34}, LeaseSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deviceScheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	storedUnmatched, err := service.Get(context.Background(), unmatched.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedMatched, err := service.Get(context.Background(), matched.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedUnmatched.Status != "pending" || storedUnmatched.DeviceID != nil {
		t.Fatalf("unmatched reservation=%#v", storedUnmatched)
	}
	if storedMatched.Status != "active" || storedMatched.DeviceID == nil {
		t.Fatalf("matched reservation=%#v", storedMatched)
	}
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='ready'", 1)
	assertCount(t, db, "SELECT count(*) FROM device_sessions", 1)
}

func TestConcurrentSchedulersRespectPoolMaximumBelowDeviceCount(t *testing.T) {
	db := openTestDatabase(t)
	resetAndSeed(t, db, 2)
	if _, err := db.Pool().Exec(context.Background(), "UPDATE device_pools SET max_concurrency=1 WHERE id='pool_000000000000001'"); err != nil {
		t.Fatal(err)
	}
	service := reservation.NewService(db, nil)
	deviceScheduler := scheduler.New(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for index := 0; index < 2; index++ {
		if _, err := service.Create(context.Background(), audit.Service("service"), fmt.Sprintf("pool-limit-key-%02d", index), reservation.CreateInput{
			PoolID: "pool_000000000000001", OwnerType: "manual", OwnerID: fmt.Sprintf("owner_%019d", index),
			RequestedCapabilities: map[string]any{"platformName": "Android"}, LeaseSeconds: 600,
		}); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() {
			<-start
			_, err := deviceScheduler.RunOnce(context.Background())
			results <- err
		}()
	}
	close(start)
	for index := 0; index < 2; index++ {
		err := <-results
		if err != nil && !errors.Is(err, scheduler.ErrCapacityUnavailable) {
			t.Fatal(err)
		}
	}
	assertCount(t, db, "SELECT count(*) FROM device_reservations WHERE status='active'", 1)
	assertCount(t, db, "SELECT count(*) FROM device_reservations WHERE status='pending'", 1)
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='busy'", 1)
}

func TestSTFClaimRunsBeforeReservationActivation(t *testing.T) {
	db := openTestDatabase(t)
	resetAndSeed(t, db, 1)
	service := reservation.NewService(db, nil)
	created, err := service.Create(context.Background(), audit.Service("service"), "stf-claim-success-key", reservation.CreateInput{
		PoolID: "pool_000000000000001", OwnerType: "run_attempt", OwnerID: "attempt_000000000101",
		RequestedCapabilities: map[string]any{"platformName": "Android"}, LeaseSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimer := &fakeClaimer{claim: func(_ context.Context, serial string, ttl time.Duration) error {
		if serial != "emulator-0" || ttl != 600*time.Second {
			t.Fatalf("claim serial=%q ttl=%s", serial, ttl)
		}
		var reservationStatus domain.ReservationStatus
		var deviceStatus domain.DeviceLifecycleStatus
		if err := db.Pool().QueryRow(context.Background(), `SELECT r.status,d.lifecycle_status
			FROM device_reservations r JOIN devices d ON d.id=r.device_id WHERE r.id=$1`, created.ID).Scan(
			&reservationStatus, &deviceStatus,
		); err != nil {
			t.Fatal(err)
		}
		if reservationStatus != domain.ReservationPending || deviceStatus != domain.DeviceReserved {
			t.Fatalf("claim observed reservation=%s device=%s", reservationStatus, deviceStatus)
		}
		return nil
	}}
	assignment, err := scheduler.New(db, nil, nil, claimer).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if assignment.Reservation.Status != domain.ReservationActive {
		t.Fatalf("reservation status=%s", assignment.Reservation.Status)
	}
	assertCount(t, db, "SELECT count(*) FROM device_sessions WHERE status='active'", 1)
}

func TestRetryableSTFClaimFailureRestoresDeviceAndKeepsReservationPending(t *testing.T) {
	testSTFClaimFailureCompensation(t, true, domain.ReservationPending)
}

func TestTerminalSTFClaimFailureRestoresDeviceAndFailsReservation(t *testing.T) {
	testSTFClaimFailureCompensation(t, false, domain.ReservationFailed)
}

func TestSTFClaimIsReleasedWhenSessionIDGenerationFails(t *testing.T) {
	db := openTestDatabase(t)
	resetAndSeed(t, db, 1)
	service := reservation.NewService(db, nil)
	created, err := service.Create(context.Background(), audit.Service("service"), "stf-session-id-failure", reservation.CreateInput{
		PoolID: "pool_000000000000001", OwnerType: "manual", OwnerID: "owner_00000000000101",
		RequestedCapabilities: map[string]any{"platformName": "Android"}, LeaseSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimer := &fakeClaimer{}
	generationError := errors.New("ID generator unavailable")
	_, err = scheduler.New(db, func() (string, error) { return "", generationError }, nil, claimer).RunOnce(context.Background())
	if !errors.Is(err, generationError) {
		t.Fatalf("RunOnce() error=%v", err)
	}
	if claimer.releaseCalls.Load() != 1 {
		t.Fatalf("release calls=%d", claimer.releaseCalls.Load())
	}
	stored, err := service.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.ReservationFailed || stored.DeviceID != nil {
		t.Fatalf("reservation=%#v", stored)
	}
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='ready'", 1)
	assertCount(t, db, "SELECT count(*) FROM device_sessions", 0)
}

func testSTFClaimFailureCompensation(t *testing.T, retryable bool, want domain.ReservationStatus) {
	t.Helper()
	db := openTestDatabase(t)
	resetAndSeed(t, db, 1)
	service := reservation.NewService(db, nil)
	created, err := service.Create(context.Background(), audit.Service("service"), fmt.Sprintf("stf-claim-failure-%t", retryable), reservation.CreateInput{
		PoolID: "pool_000000000000001", OwnerType: "test_run", OwnerID: "owner_00000000000102",
		RequestedCapabilities: map[string]any{"platformName": "Android"}, LeaseSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimer := &fakeClaimer{claim: func(context.Context, string, time.Duration) error {
		return claimFailure{retryable: retryable}
	}}
	_, err = scheduler.New(db, nil, nil, claimer).RunOnce(context.Background())
	if !errors.Is(err, scheduler.ErrSTFClaimFailed) {
		t.Fatalf("RunOnce() error=%v", err)
	}
	stored, err := service.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != want || stored.DeviceID != nil || stored.FailureCode == nil || *stored.FailureCode != "STF_CLAIM_FAILED" {
		t.Fatalf("reservation=%#v", stored)
	}
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='ready'", 1)
	assertCount(t, db, "SELECT count(*) FROM device_sessions", 0)
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

func resetAndSeed(t *testing.T, db *database.DB, devices int) {
	t.Helper()
	statements := []string{`TRUNCATE TABLE
        device_idempotency_records,device_audit_events,device_health_events,device_sessions,
        device_reservations,device_pool_devices,devices,device_pool_images,device_pools,
        device_host_commands,device_hosts,device_images RESTART IDENTITY CASCADE`,
		`
        INSERT INTO device_hosts (id,name,host_type,status,draining)
        VALUES ('host_000000000000001','scheduler-host','docker_emulator','online',false)`,
		`
        INSERT INTO device_images (id,name,docker_image,docker_digest,api_level,abi,resolution,status)
        VALUES ('image_00000000000001','scheduler-image','registry.example/alcor/android-emulator:api34','sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',34,'x86_64','1080x2400','ready')`,
		`
        INSERT INTO device_pools (id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
        VALUES ('pool_000000000000001','scheduler-pool',600,3600,2,'active')`}
	for _, statement := range statements {
		if _, err := db.Pool().Exec(context.Background(), statement); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < devices; index++ {
		deviceID := fmt.Sprintf("device_%019d", index)
		_, err := db.Pool().Exec(context.Background(), `
            INSERT INTO devices (
                id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,
                serial,adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status
            ) VALUES ($1,'host_000000000000001','image_00000000000001','emulator','mock',$2,
                'rebuild',$3,$4,$5,$6,'ready','healthy');
            `, deviceID,
			fmt.Sprintf("mock-scheduler-%d", index), fmt.Sprintf("emulator-%d", index),
			fmt.Sprintf("127.0.0.1:%d", 5555+index*2), fmt.Sprintf("http://127.0.0.1:%d", 4723+index),
			`{"platformName":"Android","apiLevel":34,"abi":"x86_64"}`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool().Exec(context.Background(), `
            INSERT INTO device_pool_devices (pool_id,device_id,enabled)
            VALUES ('pool_000000000000001',$1,true)`, deviceID); err != nil {
			t.Fatal(err)
		}
	}
}

func assertCount(t *testing.T, db *database.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.Pool().QueryRow(context.Background(), query).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("query count=%d want=%d: %s", got, want, query)
	}
}

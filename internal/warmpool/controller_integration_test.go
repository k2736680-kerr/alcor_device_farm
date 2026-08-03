package warmpool_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/warmpool"
)

func TestConcurrentControllersCreateConfiguredTargetWithoutOverbuilding(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 2, 2, 2)
	generator := sequentialGenerator()
	controllers := []*warmpool.Controller{warmpool.New(db, generator, nil), warmpool.New(db, generator, nil)}
	start := make(chan struct{})
	errorsChannel := make(chan error, len(controllers))
	var group sync.WaitGroup
	for _, controller := range controllers {
		controller := controller
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := controller.RunOnce(context.Background())
			errorsChannel <- err
		}()
	}
	close(start)
	group.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertCount(t, db, "SELECT count(*) FROM devices", 2)
	assertCount(t, db, "SELECT count(*) FROM device_pool_devices WHERE enabled", 2)
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create' AND status='pending'", 2)
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='create'
		AND payload->>'docker_digest'='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'`, 2)
	assertCount(t, db, "SELECT count(*) FROM devices WHERE provider_type='docker_emulator' AND lifecycle_status='provisioning'", 2)
	result, err := controllers[0].RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 0 {
		t.Fatalf("second reconciliation result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices", 2)
}

func TestControllerRespectsHostCapacityImageStatusAndSafeScaleDown(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "draft", 2, 2, 1)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	if result, err := controller.RunOnce(context.Background()); err != nil || result.DevicesCreated != 0 {
		t.Fatalf("draft image result=%+v error=%v", result, err)
	}
	if _, err := db.Pool().Exec(context.Background(), "UPDATE device_images SET status='ready'"); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 || result.CapacityMisses != 1 {
		t.Fatalf("capacity result=%+v error=%v", result, err)
	}
	if _, err := db.Pool().Exec(context.Background(), "UPDATE device_pool_images SET min_ready=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices", 1)
}

func TestFailedCreateIsQuarantinedAndBackoffPreventsCommandStorm(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 2, 2, 2)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	if _, err := controller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET
		status='failed',error_code='KVM_UNAVAILABLE',completed_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE id=(SELECT id FROM device_host_commands ORDER BY id LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DevicesFailed != 1 || result.BackoffSkips != 1 || result.DevicesCreated != 0 {
		t.Fatalf("backoff result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='quarantined' AND health_status='unhealthy'", 1)
	assertCount(t, db, "SELECT count(*) FROM device_health_events WHERE event_type='warm_pool_create_failed'", 1)
	assertCount(t, db, "SELECT count(*) FROM device_host_commands", 2)
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET created_at=clock_timestamp()-interval '20 minutes',
		completed_at=clock_timestamp()-interval '10 minutes',updated_at=clock_timestamp() WHERE status='failed'`); err != nil {
		t.Fatal(err)
	}
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 {
		t.Fatalf("retry result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands", 3)
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status<>'quarantined'", 2)
}

func TestImageValidationCommandGatesWarmPoolCreation(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "validating", 2, 2, 2)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.ValidationsQueued != 1 || result.DevicesCreated != 0 {
		t.Fatalf("validation queue result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='validate_image' AND status='pending'", 1)
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='succeeded',
		result='{"digest_verified":true,"ready":true}',completed_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE command_type='validate_image'`); err != nil {
		t.Fatal(err)
	}
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.ValidationsCompleted != 1 || result.DevicesCreated != 2 {
		t.Fatalf("validation completion result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_images WHERE status='ready' AND validation_error IS NULL", 1)
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 2)
}

func TestFailedImageValidationDoesNotCreateEmulator(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "validating", 2, 2, 2)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	if _, err := controller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='failed',
		error_code='IMAGE_DIGEST_MISMATCH',completed_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE command_type='validate_image'`); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.ValidationsFailed != 1 || result.DevicesCreated != 0 {
		t.Fatalf("failed validation result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_images WHERE status='failed' AND validation_error='IMAGE_DIGEST_MISMATCH'", 1)
	assertCount(t, db, "SELECT count(*) FROM devices", 0)
}

func openTestDatabase(t *testing.T) *database.DB {
	t.Helper()
	url := os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("DEVICE_FARM_TEST_DATABASE_URL is not set")
	}
	db, err := database.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func seedWarmPool(t *testing.T, db *database.DB, imageStatus string, minReady, maxInstances, hostSlots int) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE
		device_idempotency_records,device_audit_events,device_health_events,device_sessions,
		device_reservations,device_pool_devices,devices,device_pool_images,device_pools,
		device_host_commands,device_hosts,device_images RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_images
		(id,name,docker_digest,api_level,abi,resolution,resource_config,status)
		VALUES('image_00000000000001','warm-image','sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',34,'x86_64','1080x1920','{"ramMb":4096}',$1)`, imageStatus); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_hosts(id,name,host_type,capacity,status)
		VALUES('host_000000000000001','warm-host','docker_emulator',jsonb_build_object('device_slots',$1::int),'online')`, hostSlots); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_pools(id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
		VALUES('pool_000000000000001','default-android',600,3600,2,'active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_pool_images(pool_id,image_id,min_ready,max_instances,enabled)
		VALUES('pool_000000000000001','image_00000000000001',$1,$2,true)`, minReady, maxInstances); err != nil {
		t.Fatal(err)
	}
}

func sequentialGenerator() func() (string, error) {
	var mutex sync.Mutex
	value := 0
	return func() (string, error) {
		mutex.Lock()
		defer mutex.Unlock()
		value++
		return fmt.Sprintf("generated_%016d", value), nil
	}
}

func assertCount(t *testing.T, db *database.DB, query string, expected int) {
	t.Helper()
	var actual int
	if err := db.Pool().QueryRow(context.Background(), query).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("query %q count=%d want=%d", query, actual, expected)
	}
}

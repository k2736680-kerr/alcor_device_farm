package warmpool_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/capacity"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/scheduler"
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
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='create'
		AND payload->>'docker_image'='registry.example/alcor/android-emulator:api34'`, 2)
	assertCount(t, db, "SELECT count(*) FROM devices WHERE provider_type='docker_emulator' AND lifecycle_status='provisioning'", 2)
	result, err := controllers[0].RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 0 {
		t.Fatalf("second reconciliation result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices", 2)
}

func TestProvisionPhoneCreatesOneCommandAndRaisesPoolTarget(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 2)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	input := warmpool.ProvisionInput{
		PoolID: "pool_000000000000001", ImageID: "image_00000000000001", HardwareProfileID: "pixel_9",
		RuntimeProfile: runtimeprofile.Default(), IdempotencyKey: "provision-phone-test-key",
	}
	first, err := controller.Provision(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := controller.Provision(context.Background(), input)
	if err != nil || second != first {
		t.Fatalf("idempotent provision=%+v first=%+v err=%v", second, first, err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='provisioning'", 1)
	assertCount(t, db, "SELECT count(*) FROM device_pool_devices WHERE enabled", 1)
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create' AND payload->'capabilities'->>'hardware_profile_id'='pixel_9'", 1)
	assertCount(t, db, "SELECT total_target FROM device_pools WHERE id='pool_000000000000001'", 2)
}

func TestCatalogProvisioningReusesCachedAndroidVersionAcrossRuntimeProfiles(t *testing.T) {
	db := openTestDatabase(t)
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE device_provisioning_jobs,device_image_preparations,android_system_image_catalog CASCADE`); err != nil {
		t.Fatal(err)
	}
	seedWarmPool(t, db, "ready", 1, 1, 2)
	ctx := context.Background()
	if _, err := db.Pool().Exec(ctx, `INSERT INTO device_host_commands(id,host_id,command_type,payload,idempotency_key)
		VALUES('build_0000000000001','host_000000000000001','prepare_android_image','{}','cached-build-command-key');
		INSERT INTO android_system_image_catalog
		(id,package_name,api_level,image_type,abi,revision)
		VALUES('catalog_000000000001','system-images;android-34;google_apis;x86_64',34,'google_apis','x86_64','7');
		INSERT INTO device_image_preparations(id,catalog_id,host_id,build_command_id,client_id,idempotency_key,catalog_revision,runtime_profile,image_id,status)
		VALUES('preparation_0000001','catalog_000000000001','host_000000000000001','build_0000000000001','test','cached-image-key','7',
		'{}','image_00000000000001','cached')`); err != nil {
		t.Fatal(err)
	}
	controller := warmpool.New(db, sequentialGenerator(), nil)
	requested := runtimeprofile.Default()
	requested.ContainerMemoryMB = 8192
	requested.GuestMemoryMB = 6144
	requested.DataDiskMB = 8192
	job, created, err := controller.CreateCatalogProvisioning(ctx, warmpool.CatalogProvisionInput{
		ClientID: "test", IdempotencyKey: "catalog-runtime-difference", PoolID: "pool_000000000000001",
		CatalogID: "catalog_000000000001", HardwareProfileID: "pixel_9", RuntimeProfile: requested,
	})
	if err != nil || !created {
		t.Fatalf("create job=%+v created=%t error=%v", job, created, err)
	}
	cached, err := controller.AttachCachedPreparation(ctx, job.ID)
	if err != nil || !cached {
		t.Fatalf("attach cached=%t error=%v", cached, err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_provisioning_jobs
		WHERE preparation_id='preparation_0000001'`, 1)
}

func TestCatalogProvisioningReportsCapacityShortfallAndResumesAfterRecovery(t *testing.T) {
	tests := []struct {
		name          string
		capacity      map[string]any
		shortfallKey  string
		messagePrefix string
	}{
		{
			name: "内存不足",
			capacity: map[string]any{"resource_model": "dynamic_v1", "cpu_cores": 16, "memory_total_mb": 8192,
				"memory_available_mb": 8192, "disk_total_mb": 100000, "disk_available_mb": 80000},
			shortfallKey: "memory_mb", messagePrefix: "宿主机内存不足",
		},
		{
			name: "磁盘不足",
			capacity: map[string]any{"resource_model": "dynamic_v1", "cpu_cores": 16, "memory_total_mb": 32768,
				"memory_available_mb": 30000, "disk_total_mb": 100000, "disk_available_mb": 5000},
			shortfallKey: "disk_mb", messagePrefix: "宿主机磁盘不足",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := openTestDatabase(t)
			seedWarmPool(t, db, "ready", 0, 2, 2)
			ctx := context.Background()
			if _, err := db.Pool().Exec(ctx, `TRUNCATE TABLE device_provisioning_jobs,device_image_preparations,android_system_image_catalog CASCADE`); err != nil {
				t.Fatal(err)
			}
			test.capacity["collected_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			if _, err := db.Pool().Exec(ctx, `UPDATE device_hosts SET capacity=$1::jsonb,last_heartbeat_at=clock_timestamp()`, test.capacity); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Pool().Exec(ctx, `INSERT INTO device_host_commands(id,host_id,command_type,payload,idempotency_key)
				VALUES('build_capacity_test','host_000000000000001','prepare_android_image','{}','build-capacity-test-key');
				INSERT INTO android_system_image_catalog(id,package_name,api_level,image_type,abi,revision)
				VALUES('catalog_capacity_01','system-images;android-34;google_apis;x86_64',34,'google_apis','x86_64','7');
				INSERT INTO device_image_preparations(id,catalog_id,host_id,build_command_id,client_id,idempotency_key,catalog_revision,runtime_profile,image_id,status)
				VALUES('preparation_capacity_01','catalog_capacity_01','host_000000000000001','build_capacity_test','test','capacity-image-key','7',
				'{}','image_00000000000001','cached')`); err != nil {
				t.Fatal(err)
			}
			controller := warmpool.New(db, sequentialGenerator(), nil)
			profile := runtimeprofile.Default()
			profile.ContainerCPUCores = 2
			profile.ContainerMemoryMB = 8192
			profile.GuestMemoryMB = 6144
			profile.DataDiskMB = 8192
			profile.ImageDiskMB = 0
			job, created, err := controller.CreateCatalogProvisioning(ctx, warmpool.CatalogProvisionInput{
				ClientID: "test", IdempotencyKey: "capacity-feedback-key", PoolID: "pool_000000000000001",
				CatalogID: "catalog_capacity_01", HardwareProfileID: "pixel_9", RuntimeProfile: profile,
			})
			if err != nil || !created {
				t.Fatalf("create job=%+v created=%t error=%v", job, created, err)
			}
			if attached, err := controller.AttachCachedPreparation(ctx, job.ID); err != nil || !attached {
				t.Fatalf("attach cached=%t error=%v", attached, err)
			}
			if _, err := controller.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			waiting, err := controller.GetCatalogProvisioning(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if waiting.Status != "waiting_capacity" || waiting.ErrorCode != "DEVICE_CAPACITY_UNAVAILABLE" || waiting.CapacityResult == nil || waiting.CapacityResult.Shortfall[test.shortfallKey] <= 0 {
				t.Fatalf("waiting job=%+v", waiting)
			}
			if message := capacity.ChineseMessage(*waiting.CapacityResult); !strings.HasPrefix(message, test.messagePrefix) {
				t.Fatalf("capacity message=%q", message)
			}
			assertCount(t, db, "SELECT count(*) FROM devices", 0)
			assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 0)

			recovered := map[string]any{"resource_model": "dynamic_v1", "cpu_cores": 16, "memory_total_mb": 32768,
				"memory_available_mb": 30000, "disk_total_mb": 100000, "disk_available_mb": 80000,
				"collected_at": time.Now().UTC().Format(time.RFC3339Nano)}
			if _, err := db.Pool().Exec(ctx, `UPDATE device_hosts SET capacity=$1::jsonb,last_heartbeat_at=clock_timestamp()`, recovered); err != nil {
				t.Fatal(err)
			}
			if _, err := controller.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			resumed, err := controller.GetCatalogProvisioning(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if resumed.Status != "creating_emulator" || resumed.DeviceID == "" || resumed.CapacityResult != nil || resumed.ErrorCode != "" {
				t.Fatalf("resumed job=%+v", resumed)
			}
			assertCount(t, db, "SELECT count(*) FROM devices", 1)
			assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 1)
		})
	}
}

func TestPoolUsesOnlyDefaultImageAndSwitchDoesNotReimageExistingDevice(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 2)
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_images
		(id,name,docker_image,docker_digest,api_level,abi,resolution,resource_config,status)
		VALUES('image_00000000000002','android-16','registry.example/alcor/android-emulator:api36',
		'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',36,'x86_64','1080x2400','{}','ready');
		INSERT INTO device_pool_images(pool_id,image_id,min_ready,max_instances,enabled)
		VALUES('pool_000000000000001','image_00000000000002',1,1,true)`); err != nil {
		t.Fatal(err)
	}
	controller := warmpool.New(db, sequentialGenerator(), nil)
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 {
		t.Fatalf("multi-image result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='create'
		AND payload->>'image_id'='image_00000000000001'
		AND payload->>'docker_image'='registry.example/alcor/android-emulator:api34'`, 1)
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='create'
		AND payload->>'image_id'='image_00000000000002'
		AND payload->>'docker_image'='registry.example/alcor/android-emulator:api36'
		AND payload->>'docker_digest'='sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'`, 0)
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_pools SET
		default_image_id='image_00000000000002',total_target=2,min_ready=2,max_concurrency=2
		WHERE id='pool_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 {
		t.Fatalf("default image switch result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='create'
		AND payload->>'image_id'='image_00000000000001'`, 1)
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='create'
		AND payload->>'image_id'='image_00000000000002'`, 1)
}

func TestPendingReservationCreatesDefaultImageWhenMinimumReadyIsZero(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 0, 2, 2)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	if result, err := controller.RunOnce(context.Background()); err != nil || result.DevicesCreated != 0 {
		t.Fatalf("zero warm target result=%+v error=%v", result, err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_reservations
		(id,client_id,pool_id,owner_type,owner_id,requested_capabilities,lease_seconds,status,idempotency_key)
		VALUES('reservation_jit_000001','service','pool_000000000000001','manual','jit-user',
		'{"platformName":"Android","apiLevel":34}',600,'pending','jit-reservation-0001')`); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 {
		t.Fatalf("pending demand result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='create'
		AND payload->>'image_id'='image_00000000000001'`, 1)
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 0 {
		t.Fatalf("pending demand duplicate result=%+v error=%v", result, err)
	}
}

func TestControllerAdjustsToLargerConfiguredTargetWithoutCodeChanges(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 2, 2, 4)
	controller := warmpool.New(db, sequentialGenerator(), nil)

	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 2 {
		t.Fatalf("initial target result=%+v error=%v", result, err)
	}

	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_pools SET max_concurrency=4,total_target=4,min_ready=4;
		UPDATE device_pool_images SET min_ready=4,max_instances=4`); err != nil {
		t.Fatal(err)
	}
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 2 {
		t.Fatalf("larger target result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices", 4)
	assertCount(t, db, "SELECT count(*) FROM device_pool_devices WHERE enabled", 4)
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 4)

	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_pools SET max_concurrency=3,total_target=3,min_ready=3;
		UPDATE device_pool_images SET min_ready=3,max_instances=3`); err != nil {
		t.Fatal(err)
	}
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 0 || result.DeletesQueued != 0 {
		t.Fatalf("smaller target result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices", 4)
	assertCount(t, db, "SELECT count(*) FROM device_pool_devices WHERE enabled", 4)
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='delete'", 0)
}

func TestControllerScaleDownDeletesOldestIdleDevicesAndKeepsNewest(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 3)
	seedReadyDevices(t, db, 3)
	controller := warmpool.New(db, sequentialGenerator(), nil)

	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DeletesQueued != 2 || result.DevicesCreated != 0 {
		t.Fatalf("scale down queue result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='delete'
		AND payload->>'operation_source'='warm_pool_scale_down'`, 2)
	assertCount(t, db, `SELECT count(*) FROM device_pool_devices pd JOIN devices d ON d.id=pd.device_id
		WHERE pd.enabled AND d.id='scale_device_00000003'`, 1)
	assertCount(t, db, `SELECT count(*) FROM devices WHERE id IN ('scale_device_00000001','scale_device_00000002')
		AND lifecycle_status='stopped'`, 2)

	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='succeeded',
		result=jsonb_build_object('provider_ref',payload->>'provider_ref','deleted',true),
		completed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE command_type='delete'`); err != nil {
		t.Fatal(err)
	}
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.DeletesCompleted != 2 || result.DeletesQueued != 0 {
		t.Fatalf("scale down completion result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM devices WHERE id IN ('scale_device_00000001','scale_device_00000002')
		AND lifecycle_status='deleted' AND adb_endpoint IS NULL AND appium_endpoint IS NULL`, 2)
	assertCount(t, db, `SELECT count(*) FROM devices WHERE id='scale_device_00000003'
		AND lifecycle_status='ready' AND health_status='healthy'`, 1)
}

func TestControllerScaleDownProtectsActiveDeviceAndDeletesAnotherIdleDevice(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 2)
	seedReadyDevices(t, db, 2)
	if _, err := db.Pool().Exec(context.Background(), `UPDATE devices SET lifecycle_status='busy'
		WHERE id='scale_device_00000001';
		INSERT INTO device_reservations(id,client_id,pool_id,device_id,owner_type,owner_id,lease_seconds,status,
		idempotency_key,starts_at,expires_at)
		VALUES('scale_reservation_0001','service','pool_000000000000001','scale_device_00000001','test_run',
		'scale_owner_00000001',600,'active','scale-active-reservation',clock_timestamp(),clock_timestamp()+interval '10 minutes')`); err != nil {
		t.Fatal(err)
	}
	controller := warmpool.New(db, sequentialGenerator(), nil)
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DeletesQueued != 1 {
		t.Fatalf("active oldest result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='delete'
		AND payload->>'device_id'='scale_device_00000002'`, 1)
	assertCount(t, db, `SELECT count(*) FROM devices WHERE id='scale_device_00000001'
		AND lifecycle_status='busy'`, 1)
	assertCount(t, db, `SELECT count(*) FROM device_pool_devices WHERE device_id='scale_device_00000001'
		AND enabled`, 1)
}

func TestConcurrentControllersQueueOneDeletePerExcessDevice(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 3)
	seedReadyDevices(t, db, 3)
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
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='delete'`, 2)
	assertCount(t, db, `SELECT count(DISTINCT payload->>'device_id') FROM device_host_commands
		WHERE command_type='delete'`, 2)
}

func TestFailedScaleDownDeleteQuarantinesWithoutCreatingReplacement(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 2)
	seedReadyDevices(t, db, 2)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	if result, err := controller.RunOnce(context.Background()); err != nil || result.DeletesQueued != 1 {
		t.Fatalf("delete queue result=%+v error=%v", result, err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='failed',
		error_code='EMULATOR_DELETE_FAILED',completed_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE command_type='delete'`); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DeletesFailed != 1 || result.DevicesCreated != 0 {
		t.Fatalf("delete failure result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM devices WHERE id='scale_device_00000001'
		AND lifecycle_status='quarantined' AND health_status='unhealthy'`, 1)
	assertCount(t, db, `SELECT count(*) FROM device_health_events WHERE device_id='scale_device_00000001'
		AND event_type='warm_pool_scale_down_failed'`, 1)
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 0)
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
	if _, err := db.Pool().Exec(context.Background(), "UPDATE device_pools SET min_ready=0; UPDATE device_pool_images SET min_ready=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices", 1)
}

func TestControllerUsesRequestedRuntimeProfileAndActualHostResources(t *testing.T) {
	tests := []struct {
		name          string
		memoryMB      int
		guestMemoryMB int
		wantCreated   int
		wantMisses    int
	}{
		{name: "4GB profile fits three", memoryMB: 4096, guestMemoryMB: 3072, wantCreated: 3},
		{name: "8GB profile fits one", memoryMB: 8192, guestMemoryMB: 6144, wantCreated: 1, wantMisses: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := openTestDatabase(t)
			seedWarmPool(t, db, "ready", 3, 3, 99)
			profile := map[string]any{"container_cpu_cores": 2, "container_memory_mb": test.memoryMB,
				"guest_cpu_cores": 2, "guest_memory_mb": test.guestMemoryMB, "data_disk_mb": 4096,
				"image_disk_mb": 8000, "width": 1080, "height": 2400, "density_dpi": 420, "vm_heap_mb": 512, "graphics": "software"}
			capacity := map[string]any{"resource_model": "dynamic_v1", "cpu_cores": 8, "memory_total_mb": 16000,
				"memory_available_mb": 14000, "disk_total_mb": 100000, "disk_available_mb": 50000,
				"collected_at": time.Now().UTC().Format(time.RFC3339Nano)}
			if _, err := db.Pool().Exec(context.Background(), `UPDATE device_images SET resource_config=$1::jsonb`, profile); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Pool().Exec(context.Background(), `UPDATE device_hosts SET capacity=$1::jsonb,
				last_heartbeat_at=clock_timestamp(),updated_at=clock_timestamp()`, capacity); err != nil {
				t.Fatal(err)
			}
			result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
			if err != nil || result.DevicesCreated != test.wantCreated || result.CapacityMisses != test.wantMisses {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			assertCount(t, db, fmt.Sprintf(`SELECT count(*) FROM device_host_commands WHERE command_type='create'
				AND (payload->'runtime_profile'->>'container_memory_mb')::int=%d`, test.memoryMB), test.wantCreated)
		})
	}
}

func TestSuccessfulCreateResultRestoresReadyStateAfterServerRestart(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 1)
	firstController := warmpool.New(db, sequentialGenerator(), nil)
	if result, err := firstController.RunOnce(context.Background()); err != nil || result.DevicesCreated != 1 {
		t.Fatalf("create result=%+v error=%v", result, err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='succeeded',
		result='{"generation":1,"connection":{"serial":"10.0.0.20:31000","adb_endpoint":"10.0.0.20:31000",
		"appium_endpoint":"http://10.0.0.20:32000","appium_udid":"emulator-5554"},
		"health":{"online":true,"adb_online":true,"boot_completed":true,"appium_healthy":true}}',
		completed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE command_type='create'`); err != nil {
		t.Fatal(err)
	}
	// A new controller instance represents a Server restart. It must continue
	// from the persisted command result without relying on an in-memory worker.
	restartedController := warmpool.New(db, sequentialGenerator(), nil)
	result, err := restartedController.RunOnce(context.Background())
	if err != nil || result.DevicesReady != 1 || result.DevicesCreated != 0 {
		t.Fatalf("restart result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM devices WHERE lifecycle_status='ready' AND health_status='unhealthy'
		AND health_reason='STF readiness stabilization is in progress'
		AND consecutive_failures=0 AND capabilities->>'appiumUdid'='emulator-5554'`, 1)
}

func TestHistoricalCreateResultDoesNotCompleteActiveManagementRebuild(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 1)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	if _, err := controller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='succeeded',
		result='{"generation":1,"connection":{"serial":"10.0.0.20:31000","adb_endpoint":"10.0.0.20:31000",
		"appium_endpoint":"http://10.0.0.20:32000","appium_udid":"emulator-5554"},
		"health":{"online":true,"adb_online":true,"boot_completed":true,"appium_healthy":true}}',
		completed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE command_type='create'`); err != nil {
		t.Fatal(err)
	}
	if result, err := controller.RunOnce(context.Background()); err != nil || result.DevicesReady != 1 {
		t.Fatalf("initial completion result=%+v error=%v", result, err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE devices SET
		lifecycle_status='provisioning',health_status='unknown',health_reason='management rebuild queued';
		INSERT INTO device_host_commands(id,host_id,command_type,payload,status,max_attempts,idempotency_key)
		VALUES('management_rebuild_01','host_000000000000001','rebuild',
		'{"device_id":"generated_0000000000000001","operation_source":"management"}','pending',3,'management-rebuild-active')`); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DevicesReady != 0 || result.DevicesCreated != 0 {
		t.Fatalf("active rebuild result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='provisioning' AND health_status='unknown'", 1)
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

func TestQuarantinedEmulatorDiscoveredByLatestHeartbeatStillOccupiesPoolSlot(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 1)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	if result, err := controller.RunOnce(context.Background()); err != nil || result.DevicesCreated != 1 {
		t.Fatalf("initial create result=%+v error=%v", result, err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='succeeded',
		result='{"generation":1,"connection":{"serial":"10.0.0.20:31000","adb_endpoint":"10.0.0.20:31000",
		"appium_endpoint":"http://10.0.0.20:32000","appium_udid":"emulator-5554"},
		"health":{"online":true,"adb_online":true,"boot_completed":true,"appium_healthy":true}}',
		completed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE command_type='create'`); err != nil {
		t.Fatal(err)
	}
	if result, err := controller.RunOnce(context.Background()); err != nil || result.DevicesReady != 1 {
		t.Fatalf("create completion result=%+v error=%v", result, err)
	}
	if _, err := db.Pool().Exec(context.Background(), `WITH observed AS (SELECT clock_timestamp() AS at)
		UPDATE device_hosts h SET last_heartbeat_at=observed.at,used_capacity='{"device_slots":1}',updated_at=observed.at
		FROM observed;
		UPDATE devices SET lifecycle_status='quarantined',health_status='unhealthy',
			health_reason='device is not visible through STF',last_seen_at=(SELECT last_heartbeat_at FROM device_hosts),
			updated_at=clock_timestamp()`); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 0 || result.CapacityMisses != 0 {
		t.Fatalf("present quarantined result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices", 1)

	// A fresh Agent heartbeat that omits the device proves the Provider resource
	// is gone. The quarantined audit record remains, while one replacement is allowed.
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_hosts SET
		last_heartbeat_at=last_heartbeat_at+interval '1 second',used_capacity='{"device_slots":0}',
		updated_at=clock_timestamp()`); err != nil {
		t.Fatal(err)
	}
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 {
		t.Fatalf("missing quarantined replacement result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices", 2)
	assertCount(t, db, "SELECT count(*) FROM devices WHERE lifecycle_status='quarantined'", 1)
}

func TestLatestAgentUsedCapacityBlocksUnknownProviderOverbuild(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 1)
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_hosts SET
		last_heartbeat_at=clock_timestamp(),used_capacity='{"device_slots":1}',updated_at=clock_timestamp()`); err != nil {
		t.Fatal(err)
	}
	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 0 || result.CapacityMisses != 1 {
		t.Fatalf("reported provider capacity result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM devices", 0)
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
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='validate_image'
		AND payload->>'docker_image'='registry.example/alcor/android-emulator:api34'`, 1)
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

func TestReleasedEmulatorQueuesOneRebuildAndReturnsReadyOnlyAfterCleanSnapshot(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 0, 2, 2)
	seedRecyclingDevice(t, db)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.RebuildsQueued != 1 {
		t.Fatalf("queue result=%+v error=%v", result, err)
	}
	if result, err = controller.RunOnce(context.Background()); err != nil || result.RebuildsQueued != 0 {
		t.Fatalf("idempotent result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='rebuild'
		AND payload->>'reservation_id'='reservation_00000001'
		AND payload->>'docker_image'='registry.example/alcor/android-emulator:api34'
		AND payload->>'docker_digest'='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'`, 1)
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='succeeded',
		result='{"generation":2,"connection":{"serial":"10.0.0.20:31001","adb_endpoint":"10.0.0.20:31001",
		"appium_endpoint":"http://10.0.0.20:32001","appium_udid":"emulator-5554"},
		"health":{"online":true,"adb_online":true,"boot_completed":true,"appium_healthy":true}}',
		completed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE command_type='rebuild'`); err != nil {
		t.Fatal(err)
	}
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.RebuildsCompleted != 1 {
		t.Fatalf("complete result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM devices WHERE id='device_0000000000001'
		AND lifecycle_status='ready' AND health_status='unhealthy'
		AND health_reason='STF readiness stabilization is in progress' AND consecutive_failures=0
		AND serial='10.0.0.20:31001' AND capabilities->>'appiumUdid'='emulator-5554'`, 1)
	reservationService := reservation.NewService(db, nil)
	if _, err := reservationService.Create(context.Background(), audit.Service("test-worker"), "recycle-stabilization-reservation", reservation.CreateInput{
		PoolID: "pool_000000000000001", OwnerType: "run_attempt", OwnerID: "attempt_000000000991",
		RequestedCapabilities: map[string]any{"platformName": "Android"}, LeaseSeconds: 600,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.New(db, nil, nil).RunOnce(context.Background()); !errors.Is(err, scheduler.ErrCapacityUnavailable) {
		t.Fatalf("scheduler during recycle stabilization error=%v", err)
	}
}

func TestFailedRecycleRebuildQuarantinesDevice(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 0, 2, 2)
	seedRecyclingDevice(t, db)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	if _, err := controller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='timed_out',
		error_code='DEVICE_BOOT_TIMEOUT',completed_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE command_type='rebuild'`); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.RebuildsFailed != 1 {
		t.Fatalf("failure result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM devices WHERE id='device_0000000000001'
		AND lifecycle_status='quarantined' AND health_status='unhealthy'
		AND health_reason LIKE 'DEVICE_BOOT_TIMEOUT:%'`, 1)
	assertCount(t, db, `SELECT count(*) FROM device_health_events WHERE device_id='device_0000000000001'
		AND event_type='device_rebuild_failed' AND payload->>'error_code'='DEVICE_BOOT_TIMEOUT'`, 1)
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
		(id,name,docker_image,docker_digest,api_level,abi,resolution,resource_config,status)
		VALUES('image_00000000000001','warm-image','registry.example/alcor/android-emulator:api34','sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',34,'x86_64','1080x1920','{"ramMb":4096}',$1)`, imageStatus); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_hosts(id,name,host_type,capacity,status)
		VALUES('host_000000000000001','warm-host','docker_emulator',jsonb_build_object('device_slots',$1::int),'online')`, hostSlots); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_pools
		(id,name,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,default_image_id,status)
		VALUES('pool_000000000000001','default-android',600,3600,LEAST(2,$2),$2,$1,'image_00000000000001','active')`,
		minReady, maxInstances); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_pool_images(pool_id,image_id,min_ready,max_instances,enabled)
		VALUES('pool_000000000000001','image_00000000000001',$1,$2,true)`, minReady, maxInstances); err != nil {
		t.Fatal(err)
	}
}

func seedRecyclingDevice(t *testing.T, db *database.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO devices(id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,
			adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status)
		VALUES('device_0000000000001','host_000000000000001','image_00000000000001','emulator','docker_emulator',
			'emulator-device-1','rebuild','10.0.0.20:31000','10.0.0.20:31000','http://10.0.0.20:32000',
			'{"platformName":"Android","appiumUdid":"emulator-5554"}','recycling','healthy')`,
		`INSERT INTO device_pool_devices(pool_id,device_id,enabled)
		VALUES('pool_000000000000001','device_0000000000001',true)`,
		`INSERT INTO device_reservations(id,client_id,pool_id,device_id,owner_type,owner_id,lease_seconds,status,
			idempotency_key,starts_at,expires_at,released_at)
		VALUES('reservation_00000001','service','pool_000000000000001','device_0000000000001','test_run',
			'owner_00000000000001',600,'released','recycle-reservation-key',clock_timestamp()-interval '2 minutes',
			clock_timestamp()+interval '8 minutes',clock_timestamp()-interval '1 minute')`,
	}
	for _, statement := range statements {
		if _, err := db.Pool().Exec(context.Background(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

func seedReadyDevices(t *testing.T, db *database.DB, count int) {
	t.Helper()
	for index := 1; index <= count; index++ {
		id := fmt.Sprintf("scale_device_%08d", index)
		providerRef := fmt.Sprintf("scale-emulator-%d", index)
		serial := fmt.Sprintf("10.0.0.20:%d", 31000+index)
		appium := fmt.Sprintf("http://10.0.0.20:%d", 32000+index)
		createdOffset := count - index + 1
		if _, err := db.Pool().Exec(context.Background(), `INSERT INTO devices
			(id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,adb_endpoint,
			appium_endpoint,capabilities,lifecycle_status,health_status,created_at,updated_at)
			VALUES($1,'host_000000000000001','image_00000000000001','emulator','docker_emulator',$2,
			'rebuild',$3,$3,$4,'{"platformName":"Android","appiumUdid":"emulator-5554"}','ready','healthy',
			clock_timestamp()-make_interval(hours=>$5),clock_timestamp()-make_interval(hours=>$5))`,
			id, providerRef, serial, appium, createdOffset); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_pool_devices(pool_id,device_id,enabled)
			VALUES('pool_000000000000001',$1,true)`, id); err != nil {
			t.Fatal(err)
		}
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

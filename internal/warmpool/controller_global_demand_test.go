package warmpool_test

import (
	"context"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/warmpool"
)

// TestGlobalDemandCreatesOneDevicePerReservationAcrossPools 锁定 ADR-0043 修订的
// 核心修复：一个待分配预约只能建一台设备，即使有多台池/宿主。
//
// 缺陷背景：此前全局 pending 需求被放进每池 reconcile，每个池都独立看到同一条
// pending 预约、各自 lockHostCapacity 挑一台宿主，于是 1 条预约会重复建 2+ 台，
// 并且可能"明明是 54 的预约、却建到别的 IP"。
//
// 场景：2 台宿主、2 个池（共享同一镜像），1 条 pending 预约。修前 RunOnce 会建
// 2 台（每池一台），修后只建 1 台。
func TestGlobalDemandCreatesOneDevicePerReservationAcrossPools(t *testing.T) {
	db := openTestDatabase(t)
	seedTwoPoolTwoHost(t, db)

	controller := warmpool.New(db, sequentialGenerator(), nil)

	// 空闲无 pending：不建任何设备。
	if result, err := controller.RunOnce(context.Background()); err != nil || result.DevicesCreated != 0 {
		t.Fatalf("idle result=%+v error=%v", result, err)
	}

	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_reservations
		(id,client_id,pool_id,owner_type,owner_id,requested_capabilities,lease_seconds,status,idempotency_key)
		VALUES('reservation_global_demand_0001','service','pool_a_000000000001','manual','global-user',
		'{"platformName":"Android","apiLevel":34}',600,'pending','global-demand-reservation-0001')`); err != nil {
		t.Fatal(err)
	}

	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 {
		t.Fatalf("跨池全局需求应只建 1 台，实际 result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM devices WHERE lifecycle_status<>'deleted'`, 1)
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='create'`, 1)

	// 第二轮：provisioning 设备已在建、预约仍 pending，不得再建（跨 RunOnce 去重）。
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 0 {
		t.Fatalf("已有一台在建时不得重复建机，result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM devices WHERE lifecycle_status<>'deleted'`, 1)
}

// seedTwoPoolTwoHost 建 2 台宿主 + 2 个池，共享同一镜像，均 total_target=0/min_ready=0。
func seedTwoPoolTwoHost(t *testing.T, db *database.DB) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE
		device_idempotency_records,device_audit_events,device_health_events,device_sessions,
		device_reservations,device_pool_devices,devices,device_pool_images,device_pools,
		device_host_commands,device_hosts,device_images,device_host_image_states RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO device_images
			(id,name,docker_image,docker_digest,api_level,abi,resolution,resource_config,status)
			VALUES('image_00000000000001','shared-image','registry.example/alcor/android-emulator:api34',
			'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',34,'x86_64','1080x1920',
			'{"ramMb":4096}','ready')`,
		`INSERT INTO device_hosts(id,name,host_type,capacity,status)
			VALUES('host_a_000000000001','host-a','docker_emulator',jsonb_build_object('device_slots',2),'online'),
			      ('host_b_000000000001','host-b','docker_emulator',jsonb_build_object('device_slots',2),'online')`,
		`INSERT INTO device_pools
			(id,name,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,default_image_id,status)
			VALUES('pool_a_000000000001','pool-a',600,3600,8,0,0,'image_00000000000001','active'),
			      ('pool_b_000000000001','pool-b',600,3600,8,0,0,'image_00000000000001','active')`,
		`INSERT INTO device_pool_images(pool_id,image_id,min_ready,max_instances,enabled)
			VALUES('pool_a_000000000001','image_00000000000001',0,8,true),
			      ('pool_b_000000000001','image_00000000000001',0,8,true)`,
	}
	for _, statement := range statements {
		if _, err := db.Pool().Exec(context.Background(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

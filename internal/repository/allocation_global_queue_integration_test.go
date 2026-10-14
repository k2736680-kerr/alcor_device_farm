package repository

import (
	"context"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/jackc/pgx/v5"
)

// 队列必须是**全局一条**，不按 pool_id 切分（ADR-0043）。
//
// 场景：预约 A 落在池 X（该池里没有可用设备），而**池 Y** 里有一台满足能力的
// ready 设备。池与宿主机一对一，池在分配链路里只是机器的一个名字，所以 A
// 应当能拿到池 Y 的那台设备。
//
// 这是本改造的核心回归：修前 `LockNextAllocatablePending` 与
// `LockMatchingDevice` 都用 `pd.pool_id = r.pool_id` 硬过滤，A 永远匹配不上，
// 只能排队到 PENDING_TIMEOUT —— 也就是"55 空闲却在干等 54"。
func TestAllocationCrossesPoolsWhenTheOtherPoolHasAMatchingDevice(t *testing.T) {
	db := openAllocationDB(t)
	ctx := context.Background()
	resetAllocationTables(t, db)

	// 两台宿主、两个池（池名不按地址命名，走 membership 回退路径）。
	exec(t, db, `INSERT INTO device_hosts (id,name,host_type,status,draining)
		VALUES ('host_allocation_aaaa','host-a','docker_emulator','online',false),
		       ('host_allocation_bbbb','host-b','docker_emulator','online',false)`)
	exec(t, db, `INSERT INTO device_images (id,name,docker_image,docker_digest,api_level,abi,resolution,status)
		VALUES ('image_allocation_aaaa','img-a','registry.example/alcor/emu:api34',
		        'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
		        34,'x86_64','1080x2400','ready')`)
	exec(t, db, `INSERT INTO device_pools (id,name,platform,default_lease_seconds,max_lease_seconds,
			max_concurrency,total_target,min_ready,status)
		VALUES ('pool_allocation_xxxx','pool-x','android',600,3600,8,0,0,'active'),
		       ('pool_allocation_yyyy','pool-y','android',600,3600,8,0,0,'active')`)

	// 池 Y 里有一台满足请求的 ready 设备；池 X 里没有设备。
	exec(t, db, `INSERT INTO devices (id,host_id,image_id,device_kind,provider_type,provider_ref,
			lifecycle_mode,serial,adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status)
		VALUES ('device_allocation_yyyy','host_allocation_bbbb','image_allocation_aaaa','emulator','mock','ref-y',
		        'rebuild','emu-y','127.0.0.1:5601','http://127.0.0.1:4741',
		        '{"platformName":"Android","apiLevel":34,"abi":"x86_64"}','ready','healthy')`)
	exec(t, db, `INSERT INTO device_pool_devices (pool_id,device_id,enabled)
		VALUES ('pool_allocation_yyyy','device_allocation_yyyy',true)`)

	// 预约落在池 X —— 该池里一台可用设备都没有。
	exec(t, db, `INSERT INTO device_reservations
			(id,client_id,pool_id,owner_type,owner_id,requested_capabilities,lease_seconds,status,
			 idempotency_key,created_at,updated_at)
		VALUES ('reservation_allocation_1111','client_allocation_0001','pool_allocation_xxxx','run_attempt',
		        'attempt_allocation_0001','{"platformName":"Android","apiLevel":34}',600,'pending',
		        'idem-allocation-1',clock_timestamp(),clock_timestamp())`)

	repo := ReservationRepository{}
	var locked ReservationRecord
	if err := db.WithinTx(ctx, func(tx pgx.Tx) error {
		record, err := repo.LockNextAllocatablePending(ctx, tx)
		if err != nil {
			return err
		}
		locked = record
		return nil
	}); err != nil {
		t.Fatalf("预约落在池 X、池 Y 有可用设备时，全局队列应当领取它（现状：%v）", err)
	}
	if locked.ID != "reservation_allocation_1111" {
		t.Fatalf("领到的预约应为 reservation_allocation_1111，实际 %s", locked.ID)
	}

	// 再确认真的能锁到池 Y 的那台设备（不按预约的 pool_id 过滤）。
	var device DeviceAssignment
	if err := db.WithinTx(ctx, func(tx pgx.Tx) error {
		assignment, err := repo.LockMatchingDevice(ctx, tx, locked.RequestedCapabilities)
		if err != nil {
			return err
		}
		device = assignment
		return nil
	}); err != nil {
		t.Fatalf("分配必须跨池拿到池 Y 的设备（现状：%v）", err)
	}
	if device.ID != "device_allocation_yyyy" {
		t.Fatalf("应分到 device_allocation_yyyy，实际 %s", device.ID)
	}
	if device.HostID != "host_allocation_bbbb" {
		t.Fatalf("设备应在 host_allocation_bbbb 上，实际 %s", device.HostID)
	}
}

// 反向保障：分配仍必须尊重能力，不能因为去掉了池过滤就乱发设备。
func TestAllocationStillRespectsCapabilitiesAfterPoolFilterRemoval(t *testing.T) {
	db := openAllocationDB(t)
	ctx := context.Background()
	resetAllocationTables(t, db)

	exec(t, db, `INSERT INTO device_hosts (id,name,host_type,status,draining)
		VALUES ('host_allocation_cccc','host-c','docker_emulator','online',false)`)
	exec(t, db, `INSERT INTO device_images (id,name,docker_image,docker_digest,api_level,abi,resolution,status)
		VALUES ('image_allocation_cccc','img-c','registry.example/alcor/emu:api30',
		        'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
		        30,'x86_64','1080x2400','ready')`)
	exec(t, db, `INSERT INTO device_pools (id,name,platform,default_lease_seconds,max_lease_seconds,
			max_concurrency,total_target,min_ready,status)
		VALUES ('pool_allocation_cccc','pool-c','android',600,3600,8,0,0,'active')`)
	// 池里只有一台 API 30 的设备。
	exec(t, db, `INSERT INTO devices (id,host_id,image_id,device_kind,provider_type,provider_ref,
			lifecycle_mode,serial,adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status)
		VALUES ('device_allocation_cccc','host_allocation_cccc','image_allocation_cccc','emulator','mock','ref-c',
		        'rebuild','emu-c','127.0.0.1:5603','http://127.0.0.1:4743',
		        '{"platformName":"Android","apiLevel":30,"abi":"x86_64"}','ready','healthy')`)
	exec(t, db, `INSERT INTO device_pool_devices (pool_id,device_id,enabled)
		VALUES ('pool_allocation_cccc','device_allocation_cccc',true)`)

	repo := ReservationRepository{}
	err := db.WithinTx(ctx, func(tx pgx.Tx) error {
		// 请求 API 35：池里那台 API 30 不满足，必须拒绝（不能因为去掉了池过滤就发放）。
		_, lockErr := repo.LockMatchingDevice(ctx, tx,
			[]byte(`{"platformName":"Android","apiLevel":35}`))
		return lockErr
	})
	if err == nil {
		t.Fatal("能力不匹配的设备绝不能被分配")
	}
}

func openAllocationDB(t *testing.T) *database.DB {
	t.Helper()
	db := openTestDatabase(t)
	return db
}

func resetAllocationTables(t *testing.T, db *database.DB) {
	t.Helper()
	exec(t, db, `TRUNCATE TABLE
		device_idempotency_records,device_audit_events,device_health_events,device_sessions,
		device_reservations,device_pool_devices,devices,device_pool_images,device_pools,
		device_host_commands,device_hosts,device_images,device_host_image_states
		RESTART IDENTITY CASCADE`)
}

func exec(t *testing.T, db *database.DB, statement string) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), statement); err != nil {
		t.Fatalf("seed 失败: %v\nSQL: %s", err, statement)
	}
}
package warmpool_test

import (
	"context"
	"os"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/warmpool"
)

// openAttributionDB 复用一个干净的测试库（与本包其它测试相同的环境变量）。
func openAttributionDB(t *testing.T) *database.DB {
	t.Helper()
	url := os.Getenv("DEVICE_TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
	}
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

type attributionFixture struct {
	poolA, poolAName   string
	poolB, poolBName   string
	hostA, hostB       string
	addressB           string
	imageID            string
}

// seedAttributionFixture 建两个"宿主-池一对一"的池：
//
//	pool A: android-10.0.30.54  <-> hostA(10.0.30.54)
//	pool B: android-10.0.30.55  <-> hostB(10.0.30.55)
//
// 池名按 ADR-0042 的既有约定 `<平台前缀>-<宿主address>`，归属推导就靠它。
func seedAttributionFixture(t *testing.T, db *database.DB) attributionFixture {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Pool().Exec(ctx, `TRUNCATE TABLE
		device_idempotency_records,device_audit_events,device_health_events,device_sessions,
		device_reservations,device_pool_devices,devices,device_pool_images,device_pools,
		device_host_commands,device_hosts,device_images,device_host_image_states
		RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	fixture := attributionFixture{
		poolA:     "pool_a_00000000000001",
		poolAName: "android-10.0.30.54",
		poolB:     "pool_b_00000000000002",
		poolBName: "android-10.0.30.55",
		hostA:     "host_a_00000000000001",
		hostB:     "host_b_00000000000002",
		addressB:  "10.0.30.55",
		imageID:   "image_00000000000001",
	}

	if _, err := db.Pool().Exec(ctx, `INSERT INTO device_images
		(id,name,docker_image,docker_digest,api_level,abi,resolution,resource_config,status)
		VALUES($1,'attribution-image','registry.example/alcor/android-emulator:api35',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		35,'x86_64','1080x1920','{"ramMb":5120}','ready')`, fixture.imageID); err != nil {
		t.Fatal(err)
	}

	// 两台宿主：hostA 是被预约的那台（容量会被压到 0），hostB 是空闲的那台。
	hosts := []struct{ id, name, address string }{
		{fixture.hostA, "安卓宿主机 - 10.0.30.54", "10.0.30.54"},
		{fixture.hostB, "安卓宿主机 - 10.0.30.55", fixture.addressB},
	}
	for _, host := range hosts {
		// capacity 必须带 resource_model='dynamic_v1'，否则 capacity.HostFromMap
		// 判定无效、直接跳过该宿主（这是本包容量判据的前提，见 internal/capacity）。
		if _, err := db.Pool().Exec(ctx, `INSERT INTO device_hosts
			(id,name,host_type,address,capacity,used_capacity,status,last_heartbeat_at,created_at,updated_at)
			VALUES($1,$2,'docker_emulator',$3,
			  jsonb_build_object('resource_model','dynamic_v1','cpu_cores',8,
			    'memory_total_mb',16384,'memory_available_mb',16000,
			    'disk_total_mb',102400,'disk_available_mb',102400,'device_slots',4,
			    'collected_at',to_char(clock_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
			  jsonb_build_object('cpu_cores',0,'memory_mb',0,'disk_mb',0,'device_slots',0),
			  'online',clock_timestamp(),clock_timestamp(),clock_timestamp())`,
			host.id, host.name, host.address); err != nil {
			t.Fatal(err)
		}
	}

	pools := []struct{ id, name string }{
		{fixture.poolA, fixture.poolAName},
		{fixture.poolB, fixture.poolBName},
	}
	for _, pool := range pools {
		if _, err := db.Pool().Exec(ctx, `INSERT INTO device_pools
			(id,name,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,
			 default_image_id,platform,status)
			VALUES($1,$2,600,3600,4,0,0,$3,'android','active')`,
			pool.id, pool.name, fixture.imageID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool().Exec(ctx, `INSERT INTO device_pool_images
			(pool_id,image_id,min_ready,max_instances,enabled)
			VALUES($1,$2,1,4,true)`, pool.id, fixture.imageID); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

// setHostUsedMemory 让某台宿主"装不下"（availableMB 设为很小），
// 用来制造"这台满了、那台空闲"的溢出场景。
//
// capacity.Evaluate 取 min(记账内存, 实时内存)：
//
//	accounted = memory_total_mb - 2048 - used.memory_mb
//	realtime  = memory_available_mb - 2048
//
// 所以要把 **memory_available_mb** 压小（它同时管住实时上限）。
// 显式 ::int 转换是必需的：$2 只出现在 jsonb_build_object 内部，
// Postgres 无法推断其类型（会报 could not determine data type of parameter $2）。
func setHostUsedMemory(t *testing.T, db *database.DB, hostID string, availableMB int) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_hosts
		SET capacity=jsonb_set(capacity,'{memory_available_mb}',to_jsonb($2::int)),
		    used_capacity=jsonb_build_object('cpu_cores',0,'memory_mb',$3::int,
		      'disk_mb',0,'device_slots',0),
		    updated_at=clock_timestamp()
		WHERE id=$1`, hostID, availableMB, maxInt(0, 16000-availableMB)); err != nil {
		t.Fatal(err)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// 设备归属的池必须等于它**实际运行**的那台宿主机对应的池（ADR-0042）。
//
// 场景：预约池 A，但 A 所属宿主容量不足、调度溢出到宿主 B。
// 期望：设备跑在 B 上，**归属也记在 B 对应的池**，而不是预约时请求的池 A。
//
// 这是本缺陷的直接回归：修前该断言失败（设备在 B 上、归属却仍是 A 池）。
func TestDevicePoolAttributionFollowsActualHostOnOverflow(t *testing.T) {
	db := openAttributionDB(t)
	fixture := seedAttributionFixture(t, db)

	// hostA 的可用内存压到远低于一台所需（2048 预留 + 5120 单台 = 7168）→ 装不下；
	// hostB 保持充足 → 应被选中。第二个参数是【可用内存 MB】。
	setHostUsedMemory(t, db, fixture.hostA, 1000)
	setHostUsedMemory(t, db, fixture.hostB, 16000)

	controller := warmpool.New(db, sequentialGenerator(), nil)
	if _, err := controller.Provision(context.Background(), warmpool.ProvisionInput{
		PoolID:            fixture.poolA,
		ImageID:           fixture.imageID,
		HardwareProfileID: "pixel_9",
		RuntimeProfile:    runtimeprofile.Default(),
		IdempotencyKey:    "attribution-overflow-0001",
	}); err != nil {
		t.Fatalf("Provision 失败: %v", err)
	}

	var address, poolName string
	if err := db.Pool().QueryRow(context.Background(), `
		SELECT h.address, COALESCE(p.name,'')
		FROM devices d
		JOIN device_hosts h ON h.id = d.host_id
		LEFT JOIN device_pool_devices pd ON pd.device_id = d.id
		LEFT JOIN device_pools p ON p.id = pd.pool_id
		WHERE d.lifecycle_status <> 'deleted'
		ORDER BY d.created_at DESC LIMIT 1`).Scan(&address, &poolName); err != nil {
		t.Fatalf("查询设备归属失败: %v", err)
	}

	if address != fixture.addressB {
		t.Fatalf("预期溢出到宿主 %s，实际 %s", fixture.addressB, address)
	}
	if poolName != fixture.poolBName {
		t.Fatalf("设备跑在 %s，归属池应为 %s，实际是 %s（归属未跟随实际宿主）",
			address, fixture.poolBName, poolName)
	}
}

// 宿主没有同名池时必须回退到调用方传入的池，不能因此建不出设备或丢掉归属。
func TestDevicePoolAttributionFallsBackWhenHostHasNoNamedPool(t *testing.T) {
	db := openAttributionDB(t)
	fixture := seedAttributionFixture(t, db)

	// 只有一台"没有同名池"的宿主有空闲（模拟新增宿主但还没建池）。
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_hosts
		(id,name,host_type,address,capacity,used_capacity,status,last_heartbeat_at,created_at,updated_at)
		VALUES('host_orphan_00000000001','安卓宿主机 - 10.0.30.199','docker_emulator','10.0.30.199',
		  jsonb_build_object('resource_model','dynamic_v1','cpu_cores',8,'memory_total_mb',16384,
		    'memory_available_mb',16000,'disk_total_mb',102400,'disk_available_mb',102400,'device_slots',4,
		    'collected_at',to_char(clock_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
		  jsonb_build_object('cpu_cores',0,'memory_mb',0,'disk_mb',0,'device_slots',0),
		  'online',clock_timestamp(),clock_timestamp(),clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	// 把两个有池的宿主都压到装不下，逼调度只能用这台无池宿主。
	setHostUsedMemory(t, db, fixture.hostA, 1000)
	setHostUsedMemory(t, db, fixture.hostB, 1000)

	controller := warmpool.New(db, sequentialGenerator(), nil)
	if _, err := controller.Provision(context.Background(), warmpool.ProvisionInput{
		PoolID:            fixture.poolA,
		ImageID:           fixture.imageID,
		HardwareProfileID: "pixel_9",
		RuntimeProfile:    runtimeprofile.Default(),
		IdempotencyKey:    "attribution-orphan-0001",
	}); err != nil {
		t.Fatalf("Provision 失败: %v", err)
	}

	var address, poolName string
	if err := db.Pool().QueryRow(context.Background(), `
		SELECT h.address, COALESCE(p.name,'')
		FROM devices d
		JOIN device_hosts h ON h.id = d.host_id
		LEFT JOIN device_pool_devices pd ON pd.device_id = d.id
		LEFT JOIN device_pools p ON p.id = pd.pool_id
		WHERE d.lifecycle_status <> 'deleted'
		ORDER BY d.created_at DESC LIMIT 1`).Scan(&address, &poolName); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if address != "10.0.30.199" {
		t.Fatalf("预期建在无池宿主 10.0.30.199，实际 %s", address)
	}
	if poolName != fixture.poolAName {
		t.Fatalf("宿主无同名池时应回退到请求池 %s，实际 %s", fixture.poolAName, poolName)
	}
}
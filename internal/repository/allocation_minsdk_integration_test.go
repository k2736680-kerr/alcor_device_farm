package repository

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 这一组锁住分配阶段（不只是选池阶段）必须遵守 androidMinSdkLevel 下限。
//
// 为什么单独测这层：下限键不在 device_schedulable_capabilities() 的白名单里，
// 送进去会被静默丢弃，于是分配查询只剩 `d.capabilities @> (...)` —— 只有
// platformName 这类精确匹配键生效。
//
// 真实故障形态（被测出来的）：
//   池内同时有 API 30 和 API 34 两台设备；提交 minSdk 34 的 APK。
//   - 选池阶段带了下限 -> 池因为「有 API 34 设备」被判可用，池被选中；
//   - 分配阶段没带下限 -> 查询按 created_at,id 排序取第一台，
//     若 API 30 那台排在前面，就会被分配出去 -> 装包必失败。
//
// 所以这里必须造**两台 API 不同的设备**，并断言分到的是 API 34 那台。

func openAllocationDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEVICE_FARM_TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(),
		`TRUNCATE device_reservations, device_pool_devices, devices, device_pools, device_hosts,
		 device_images, device_pool_images, device_host_image_states RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

// 与既有 seedSelectorHost 同形：draining 由 status 表达，
// 约束要求 draining = (status = 'draining')，不能单独置 true。
func seedAllocationHost(t *testing.T, db *pgxpool.Pool, hostID, status string) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `
		INSERT INTO device_hosts (id, name, host_type, status, draining)
		VALUES ($1, $1, 'docker_emulator', $2::varchar, $2::varchar = 'draining')`, hostID, status); err != nil {
		t.Fatalf("seed host: %v", err)
	}
}

// 与既有 seedSelectorPool 同形：default_lease_seconds / total_target 都是必填。
func seedAllocationPool(t *testing.T, db *pgxpool.Pool, poolID, platform string, maxConcurrency int) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `
		INSERT INTO device_pools (id, name, platform, status, default_lease_seconds, max_lease_seconds,
		                          total_target, max_concurrency)
		VALUES ($1, $1, $2, 'active', 1800, 7200, 1, $3)`, poolID, platform, maxConcurrency); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
}

// seedAllocationImage 造一个 ready 的 android 镜像：ck_devices_kind_image 要求
// android emulator 必须绑镜像，所以设备用例离不开它。
func seedAllocationImage(t *testing.T, db *pgxpool.Pool, imageID string, apiLevel int) {
	t.Helper()
	digest := "sha256:" + strings.Repeat("0123456789abcdef", 4)
	if _, err := db.Exec(context.Background(), `
		INSERT INTO device_images (id, name, docker_digest, docker_image, api_level, abi, resolution, status)
		VALUES ($1, $1, $2, $3, $4, 'x86_64', '1080x1920', 'ready')`,
		imageID, digest,
		"127.0.0.1:5001/alcor/android-emulator:api"+strconv.Itoa(apiLevel), apiLevel); err != nil {
		t.Fatalf("seed image: %v", err)
	}
}

// seedAllocationDevice 造一台可分配设备并加入池。
//
// orderSeconds 控制 created_at 相对当前时间的先后（负 = 更早）。
// 分配查询按 (created_at, id) 取第一台，所以「更低 API 的设备排在更早」
// 正是要构造的故障条件：没有下限时就会取到它。
//
// 注意 ck_devices_time 要求 updated_at >= created_at，两者必须用同一个基准，
// 不能用「未来 created_at + 当前 updated_at」。
func seedAllocationDevice(
	t *testing.T, db *pgxpool.Pool,
	deviceID, poolID, hostID, platform string, apiLevel, orderSeconds int,
) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `
		INSERT INTO devices (id, host_id, image_id, device_kind, provider_type, provider_ref, serial,
		                     lifecycle_mode, platform, lifecycle_status, health_status, capabilities,
		                     created_at, updated_at)
		SELECT $1, $2, 'image_00000000001', 'emulator', 'docker_emulator', $1, $1,
		       'clean', $3, 'ready', 'healthy', $4::jsonb,
		       base, base
		FROM (SELECT clock_timestamp() + ($5 || ' seconds')::interval AS base) AS t`,
		deviceID, hostID, platform,
		`{"platformName":"Android","apiLevel":`+strconv.Itoa(apiLevel)+`,"abi":"x86_64"}`,
		strconv.Itoa(orderSeconds)); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	if _, err := db.Exec(context.Background(), `
		INSERT INTO device_pool_devices (pool_id, device_id, enabled) VALUES ($1, $2, true)`,
		poolID, deviceID); err != nil {
		t.Fatalf("attach device to pool: %v", err)
	}
}

// seedPendingReservation 直接建一条 pending 预约（绕过 create 的选池），
// 以便单独验证「分配阶段」的行为。
func seedPendingReservation(t *testing.T, db *pgxpool.Pool, reservationID, poolID string, capabilities string) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `
		INSERT INTO device_reservations (id, client_id, pool_id, owner_type, owner_id,
		                                 requested_capabilities, lease_seconds, status, idempotency_key, starts_at, expires_at)
		VALUES ($1, 'test-client', $2, 'run_attempt', 'attempt_000000000001',
		        $3::jsonb, 600, 'pending', $1, clock_timestamp(), clock_timestamp() + interval '600 seconds')`,
		reservationID, poolID, capabilities); err != nil {
		t.Fatalf("seed reservation: %v", err)
	}
}

// 核心回归：分配阶段必须跳过低于 minSdk 的设备。
//
// 反向验证：去掉分配查询里的下限条件后，本用例会因为分到 API 30 设备而失败。
func TestAllocationSkipsDeviceBelowMinSdk(t *testing.T) {
	db := openAllocationDatabase(t)
	seedAllocationHost(t, db, "host_0000000000001", "online")
	seedAllocationImage(t, db, "image_00000000001", 34)
	seedAllocationPool(t, db, "pool_0000000000001", "android", 2)
	// 关键：低 API 设备排在前面（created_at 更早），高 API 设备排在后面。
	// 没有下限时，查询会按 (created_at, id) 取到第一台 —— 即 API 30 那台。
	seedAllocationDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001", "android", 30, -60)
	seedAllocationDevice(t, db, "device_00000000002", "pool_0000000000001", "host_0000000000001", "android", 34, -30)

	seedPendingReservation(t, db, "res_0000000000001", "pool_0000000000001",
		`{"platformName":"Android","androidMinSdkLevel":34}`)

	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	repo := ReservationRepository{}
	reservation, err := repo.LockNextAllocatablePending(ctx, tx)
	if err != nil {
		t.Fatalf("应当能锁到这条 pending 预约（池里有 API 34 设备）：%v", err)
	}

	device, err := repo.LockMatchingDevice(ctx, tx, reservation.PoolID, reservation.RequestedCapabilities)
	if err != nil {
		t.Fatalf("应当分到 API 34 的设备：%v", err)
	}
	if device.ID != "device_00000000002" {
		t.Fatalf("必须跳过低于 minSdk 的设备，期望 device_00000000002(API 34)，实际 %q", device.ID)
	}
}

// 下限为 0（没传 androidMinSdkLevel）时不得限制设备：保持既有行为。
func TestAllocationWithoutMinSdkDoesNotRestrict(t *testing.T) {
	db := openAllocationDatabase(t)
	seedAllocationHost(t, db, "host_0000000000001", "online")
	seedAllocationImage(t, db, "image_00000000001", 34)
	seedAllocationPool(t, db, "pool_0000000000001", "android", 2)
	seedAllocationDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001", "android", 30, -60)

	seedPendingReservation(t, db, "res_0000000000001", "pool_0000000000001", `{"platformName":"Android"}`)

	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	repo := ReservationRepository{}
	reservation, err := repo.LockNextAllocatablePending(ctx, tx)
	if err != nil {
		t.Fatalf("没有下限限制时应能锁到预约：%v", err)
	}
	device, err := repo.LockMatchingDevice(ctx, tx, reservation.PoolID, reservation.RequestedCapabilities)
	if err != nil {
		t.Fatalf("没有下限时 API 30 设备应当可用：%v", err)
	}
	if device.ID != "device_00000000001" {
		t.Fatalf("期望唯一的 API 30 设备，实际 %q", device.ID)
	}
}

// LockNextAllocatablePending 自身也必须带下限：池里只有低于下限的设备时，
// 这条预约不该被判为「可分派」——否则调度器会反复取到它却分配不出设备。
func TestPendingReservationNotAllocatableWhenOnlyDeviceBelowMinSdk(t *testing.T) {
	db := openAllocationDatabase(t)
	seedAllocationHost(t, db, "host_0000000000001", "online")
	seedAllocationImage(t, db, "image_00000000001", 34)
	seedAllocationPool(t, db, "pool_0000000000001", "android", 2)
	seedAllocationDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001", "android", 30, -60)

	seedPendingReservation(t, db, "res_0000000000001", "pool_0000000000001",
		`{"platformName":"Android","androidMinSdkLevel":34}`)

	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	repo := ReservationRepository{}
	_, err = repo.LockNextAllocatablePending(ctx, tx)
	if err == nil {
		t.Fatal("池内只有 API 30 设备，minSdk 34 的预约不该被判为可分派")
	}
	if !isNotFound(err) {
		t.Fatalf("应当是 ErrNotFound，实际 %v", err)
	}
}

func isNotFound(err error) bool {
	return err != nil && err.Error() == ErrNotFound.Error()
}

// minSDKLowerBoundValue 的取值规则：缺失/非法/非正数都视为不设下限。
func TestMinSDKLowerBoundValueParsing(t *testing.T) {
	cases := []struct {
		name      string
		requested map[string]any
		want      int
	}{
		{"缺失", map[string]any{"platformName": "Android"}, 0},
		{"int", map[string]any{"androidMinSdkLevel": 34}, 34},
		{"float64（来自 JSON 解码）", map[string]any{"androidMinSdkLevel": float64(34)}, 34},
		{"json.Number", map[string]any{"androidMinSdkLevel": json.Number("34")}, 34},
		{"字符串非法", map[string]any{"androidMinSdkLevel": "34"}, 0},
		{"零", map[string]any{"androidMinSdkLevel": 0}, 0},
		{"负数", map[string]any{"androidMinSdkLevel": -5}, 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := minSDKLowerBoundValue(testCase.requested); got != testCase.want {
				t.Fatalf("期望 %d，实际 %d", testCase.want, got)
			}
		})
	}
}
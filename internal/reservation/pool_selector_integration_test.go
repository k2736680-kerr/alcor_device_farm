package reservation

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
)

// 自动选池是「农场自己挑池」这条路径的核心，而且它的判定几乎全在 SQL 里
// （宿主是否在线、镜像是否 ready、宿主是否被证明缺镜像、能力是否匹配）。
// 所以这组测试必须打真库：编译通过完全不能说明 SQL 语义正确。

// testActor 返回一个合法的审计身份。选池本身不写审计，但 sign 需要合法的 actor。
func testActor() audit.Actor {
	return audit.Actor{Type: audit.ActorService, ID: "selector-test", ClientID: "selector-test"}
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func openSelectorDatabase(t *testing.T) *database.DB {
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
	resetSelectorDatabase(t, db)
	return db
}

func resetSelectorDatabase(t *testing.T, db *database.DB) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(), `
        TRUNCATE TABLE
            device_audit_events, device_health_events, device_sessions,
            device_reservations, device_pool_devices, devices, device_pool_images,
            device_pools, device_host_commands, device_hosts, device_images,
            device_host_image_states
        RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset database: %v", err)
	}
}

// seedSelectorHost 插入一台宿主。hostType 用 docker_emulator（能跑模拟器）。
//
// draining 由 status 表达：约束 ck_device_hosts_draining 要求
// draining = (status = 'draining')，不能单独把 draining 置 true。
func seedSelectorHost(t *testing.T, db *database.DB, id, status string) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(), `
		INSERT INTO device_hosts (id, name, host_type, status, draining)
		VALUES ($1, $1, 'docker_emulator', $2::varchar, $2::varchar = 'draining')`, id, status)
	if err != nil {
		t.Fatalf("seed host %s: %v", id, err)
	}
}

// seedSelectorImage 插入一个镜像。api_level 是能力匹配的关键。
//
// 约束 ck_device_images_runnable_reference 要求：
//
//	docker_image IS NOT NULL OR status IN ('draft','failed','disabled')
//
// 所以除了 draft/failed/disabled，任何状态都必须给 docker_image；且它不能以
// :latest 结尾，必须带显式 tag 或 digest。
func seedSelectorImage(t *testing.T, db *database.DB, id string, apiLevel int, status string) {
	t.Helper()
	digest := "sha256:" + strings.Repeat("0123456789abcdef", 4)
	var dockerImage any
	switch status {
	case "draft", "failed", "disabled":
		dockerImage = nil
	default:
		dockerImage = "127.0.0.1:5001/alcor/android-emulator:api" + strconv.Itoa(apiLevel)
	}
	_, err := db.Pool().Exec(context.Background(), `
		INSERT INTO device_images (id, name, docker_digest, docker_image, api_level, abi, resolution, status)
		VALUES ($1, $1, $2, $3, $4, 'x86_64', '1080x1920', $5)`,
		id, digest, dockerImage, apiLevel, status)
	if err != nil {
		t.Fatalf("seed image %s: %v", id, err)
	}
}

// seedSelectorPool 插入一个 active 池，可选绑定默认镜像。
func seedSelectorPool(t *testing.T, db *database.DB, id, platform, imageID string, maxConcurrency int) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(), `
		INSERT INTO device_pools (id, name, platform, status, default_lease_seconds, max_lease_seconds,
		                          total_target, max_concurrency, default_image_id)
		VALUES ($1, $1, $2, 'active', 1800, 7200, 1, $3, $4)`,
		id, platform, maxConcurrency, nullableString(imageID))
	if err != nil {
		t.Fatalf("seed pool %s: %v", id, err)
	}
	if imageID != "" {
		if _, err := db.Pool().Exec(context.Background(), `
			INSERT INTO device_pool_images (pool_id, image_id, min_ready, max_instances, enabled)
			VALUES ($1, $2, 0, 1, true)`, id, imageID); err != nil {
			t.Fatalf("link pool %s to image %s: %v", id, imageID, err)
		}
	}
}

// seedSelectorDevice 插入一台设备并加入池。capabilities 决定能力匹配。
//
// devices 表的必填列（lifecycle_mode / serial）以及
// ck_devices_kind_image（android emulator 必须绑镜像）都要满足。
// seedSelectorAndroidImage 为 android 设备准备好被引用的镜像。
//
// android emulator 必须绑一个镜像（ck_devices_kind_image），所以凡是要插入
// android 设备的用例都得先有 image_00000000001。
func seedSelectorAndroidImage(t *testing.T, db *database.DB, apiLevel int) {
	t.Helper()
	var exists bool
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM device_images WHERE id = 'image_00000000001')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		return
	}
	seedSelectorImage(t, db, "image_00000000001", apiLevel, "ready")
}

func seedSelectorDevice(
	t *testing.T,
	db *database.DB,
	deviceID, poolID, hostID, platform, lifecycle, health string,
	capabilitiesJSON string,
) {
	t.Helper()
	imageID := any(nil)
	deviceKind, providerType := "emulator", "docker_emulator"
	if platform == "ios" {
		// ios 只允许 simulator/physical，且 provider 必须是 appium_device_farm_ios，
		// 同时镜像必须为空（ck_devices_kind_image）。
		deviceKind, providerType = "simulator", "appium_device_farm_ios"
	} else {
		imageID = "image_00000000001"
	}
	_, err := db.Pool().Exec(context.Background(), `
		INSERT INTO devices (id, host_id, image_id, device_kind, provider_type, provider_ref, serial,
		                     lifecycle_mode, platform, lifecycle_status, health_status, capabilities)
		VALUES ($1, $2, $3, $4, $5, $1, $1,
		        'clean', $6, $7, $8, $9::jsonb)`,
		deviceID, hostID, imageID, deviceKind, providerType, platform, lifecycle, health, capabilitiesJSON)
	if err != nil {
		t.Fatalf("seed device %s: %v", deviceID, err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO device_pool_devices (pool_id, device_id, enabled) VALUES ($1, $2, true)`,
		poolID, deviceID); err != nil {
		t.Fatalf("add device %s to pool %s: %v", deviceID, poolID, err)
	}
}

// seedObservedImageUnavailable 记录「这台宿主被证明没有这个镜像」。
func seedObservedImageUnavailable(t *testing.T, db *database.DB, hostID, imageID string) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(), `
		INSERT INTO device_host_image_states (host_id, image_id, available, error_code, observed_at)
		VALUES ($1, $2, false, 'IMAGE_DIGEST_MISMATCH', clock_timestamp())
		ON CONFLICT (host_id, image_id) DO UPDATE SET available = false, observed_at = clock_timestamp()`,
		hostID, imageID)
	if err != nil {
		t.Fatalf("observe image unavailable: %v", err)
	}
}

func newSelectorService(db *database.DB) *Service {
	return &Service{db: db}
}

func androidCapabilities(apiLevel int) map[string]any {
	return map[string]any{"platformName": "Android", "apiLevel": apiLevel}
}

func selectedPoolID(t *testing.T, db *database.DB, requested map[string]any) string {
	t.Helper()
	service := newSelectorService(db)
	poolID, err := service.selectPoolForRequest(context.Background(), testActor(), requested)
	if err != nil {
		t.Fatalf("selectPoolForRequest: %v", err)
	}
	return poolID
}

// A 路径：池里有可直接分配的、满足请求的活跃设备 -> 必须被选中。
func TestFarmSelectsPoolWithLiveMatchingDevice(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	got := selectedPoolID(t, db, androidCapabilities(34))
	if got != "pool_0000000000001" {
		t.Fatalf("应选中唯一可用池，实际 %q", got)
	}
}

// 设备 apiLevel 不满足请求 -> 不能选中（农场匹配是精确包含，选了必然 pending）。
func TestFarmRejectsPoolWhoseOnlyDeviceDoesNotMatchCapabilities(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 30)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":30}`)

	service := newSelectorService(db)
	_, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34))
	if err == nil {
		t.Fatal("设备 API 30 不满足请求 API 34，应当报「没有可用池」而不是选中它")
	}
	if !errors.Is(err, ErrPoolUnavailable) {
		t.Fatalf("错误应当是 ErrPoolUnavailable 语义，实际 %v", err)
	}
}

// 宿主离线时的设备不算可分配：必须排除。
func TestFarmRejectsPoolWhoseDeviceHostIsOffline(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "offline")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34)); err == nil {
		t.Fatal("设备挂在离线宿主上，不应被选中")
	}
}

// draining 宿主同理：农场调度器不会从中分配设备。
func TestFarmRejectsPoolWhoseDeviceHostIsDraining(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "draining")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34)); err == nil {
		t.Fatal("设备所在宿主正在 draining，不应被选中")
	}
}

// 池里没有设备，但配置了 ready 且 apiLevel 匹配的默认镜像 + 在线宿主
// -> 能按需拉起，必须被选中（否则运行会全挤向有活跃设备的池）。
func TestFarmSelectsOnDemandPoolWithReadyMatchingImage(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorImage(t, db, "image_00000000001", 34, "ready")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 4)

	got := selectedPoolID(t, db, androidCapabilities(34))
	if got != "pool_0000000000001" {
		t.Fatalf("有 ready 且匹配的镜像时应能按需拉起，实际 %q", got)
	}
}

// 默认镜像 apiLevel 不匹配 -> 排除。这是「min_sdk 34 的 APK 被路由到 API 30 池」
// 这类故障在农场侧的最后一道防线。
func TestFarmRejectsOnDemandPoolWhoseImageAPIDoesNotMatch(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorImage(t, db, "image_00000000001", 30, "ready")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 4)

	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34)); err == nil {
		t.Fatal("镜像 API 30 不满足请求 API 34，不应被选中")
	}
}

// 镜像不是 ready（例如还在 validating）-> 排除。
func TestFarmRejectsOnDemandPoolWhoseImageIsNotReady(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorImage(t, db, "image_00000000001", 34, "validating")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 4)

	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34)); err == nil {
		t.Fatal("镜像不是 ready，不应被选中")
	}
}

// 所有在线宿主都被证明缺该镜像 -> 排除。
//
// 这是本次故障的原始现场：镜像只存在于一台已掉线的宿主的本地仓库里，池看着
// active、配置也齐全，但 create 注定 IMAGE_DIGEST_MISMATCH。
func TestFarmRejectsPoolWhenEveryOnlineHostLacksTheImage(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorImage(t, db, "image_00000000001", 34, "ready")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 4)
	seedObservedImageUnavailable(t, db, "host_0000000000001", "image_00000000001")

	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34)); err == nil {
		t.Fatal("唯一在线宿主已被证明缺该镜像，不应被选中：create 只会失败")
	}
}

// 有一台在线宿主被证明缺镜像，但另一台可用 -> 仍应选中（按宿主判断，不是按池）。
func TestFarmSelectsPoolWhenAnotherOnlineHostHasTheImage(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorHost(t, db, "host_0000000000002", "online")
	seedSelectorImage(t, db, "image_00000000001", 34, "ready")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 4)
	// 只把 host1 证明为缺镜像，host2 没有负向记录（乐观可调度）。
	seedObservedImageUnavailable(t, db, "host_0000000000001", "image_00000000001")

	got := selectedPoolID(t, db, androidCapabilities(34))
	if got != "pool_0000000000001" {
		t.Fatalf("还有一台在线宿主可用时应选中该池，实际 %q", got)
	}
}

// 平台不匹配的池不能被选中。
func TestFarmRejectsPoolOfAnotherPlatform(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorPool(t, db, "pool_0000000000001", "ios", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"ios", "ready", "healthy", `{"platformName":"iOS"}`)

	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34)); err == nil {
		t.Fatal("iOS 池不应服务 Android 请求")
	}
}

// 只有平台要求（没有具体能力）时，有可用设备的池应当被选中。
func TestFarmSelectsPoolForPlatformOnlyRequest(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	got := selectedPoolID(t, db, map[string]any{"platformName": "Android"})
	if got != "pool_0000000000001" {
		t.Fatalf("只要平台时应选中可用池，实际 %q", got)
	}
}

// 两个池都可服务时必须轮转，不能永远选同一个。
//
// 否则运行会全挤向一个池，到 max_concurrency 后干等，另一个池的容量闲置。
func TestFarmRotatesAcrossSelectablePools(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	for _, spec := range []struct{ pool, device string }{
		{"pool_0000000000001", "device_00000000001"},
		{"pool_0000000000002", "device_00000000002"},
	} {
		seedSelectorPool(t, db, spec.pool, "android", "", 2)
		seedSelectorDevice(t, db, spec.device, spec.pool, "host_0000000000001",
			"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)
	}
	// 游标是进程内的，先清掉，避免被其它用例影响。
	poolSelectionMu.Lock()
	delete(poolSelectionCursor, "android")
	poolSelectionMu.Unlock()

	seen := map[string]int{}
	service := newSelectorService(db)
	for range 4 {
		poolID, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34))
		if err != nil {
			t.Fatalf("selectPoolForRequest: %v", err)
		}
		seen[poolID]++
	}
	if len(seen) != 2 {
		t.Fatalf("两个池都应被选中过，实际只选中 %v", seen)
	}
	if seen["pool_0000000000001"] != 2 || seen["pool_0000000000002"] != 2 {
		t.Fatalf("两个池权重相同，应当各选中 2 次，实际 %v", seen)
	}
}

// 加权轮转：容量 3 : 1 时，4 次调用应按 3:1 分布。
func TestFarmRotationRespectsPoolCapacityWeight(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 3)
	seedSelectorPool(t, db, "pool_0000000000002", "android", "", 1)
	seedSelectorAndroidImage(t, db, 34)
	for _, spec := range []struct{ pool, device string }{
		{"pool_0000000000001", "device_00000000001"},
		{"pool_0000000000002", "device_00000000002"},
	} {
		seedSelectorDevice(t, db, spec.device, spec.pool, "host_0000000000001",
			"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)
	}
	poolSelectionMu.Lock()
	delete(poolSelectionCursor, "android")
	poolSelectionMu.Unlock()

	seen := map[string]int{}
	service := newSelectorService(db)
	for range 4 {
		poolID, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34))
		if err != nil {
			t.Fatalf("selectPoolForRequest: %v", err)
		}
		seen[poolID]++
	}
	if seen["pool_0000000000001"] != 3 || seen["pool_0000000000002"] != 1 {
		t.Fatalf("按 max_concurrency 3:1 加权，期望 3/1，实际 %v", seen)
	}
}

// 池里有设备，但设备不是 ready（例如还在 booting）-> 走分支 A 不成立。
//
// 这里同时锁住两件事：
//   - 分支 A 对设备状态的要求（ready + healthy），不能只看生命周期存在；
//   - 这台设备存在的事实必须让分支 B 不成立 —— 否则会「因为池里没设备」而按
//     默认镜像按需拉起，可这个池明明已经被一台 booting 设备占着，农场不会
//     再为它额外拉一台（fresh_vm_per_run 之外的容量语义）。
//
// 该池既没有默认镜像，也没有可分配设备，所以正确结果是「选不出来」。
func TestFarmRejectsPoolWhoseOnlyDeviceIsNotReady(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "booting", "healthy", `{"platformName":"Android","apiLevel":34}`)

	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34)); err == nil {
		t.Fatal("池内唯一设备还在 booting：不应被选中（不能按 ready 之外的设备分配）")
	}
}

// 设备是 ready 但健康度不是 healthy -> 同样不能分配。
func TestFarmRejectsPoolWhoseOnlyDeviceIsUnhealthy(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "unhealthy", `{"platformName":"Android","apiLevel":34}`)

	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34)); err == nil {
		t.Fatal("设备健康度是 unhealthy：不应被选中")
	}
}

// 池里有设备（非 deleted/stopped），但一台都不能分配；同时池配了默认镜像。
//
// 分支 B 必须**不成立**：唯一的宿主是离线的，没有任何在线宿主能拉起这个池的镜像。
// 这正是 fillPoolProvisionability 里 candidates==0 的那一种，面板也会显示不可供给。
func TestFarmDoesNotTreatPoolWithUnallocatableDevicesAsEmpty(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "offline")
	seedSelectorImage(t, db, "image_00000000001", 34, "ready")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 4)
	// 设备存在（非 deleted），但它挂在离线宿主上，分支 A 不成立。
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	service := newSelectorService(db)
	poolID, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34))
	// 唯一宿主离线 -> 默认镜像没有任何在线宿主可拉起，所以 B 也不成立。
	if err == nil {
		t.Fatalf("池内设备全不可分配、且没有在线宿主能拉起镜像，不应选中 %q", poolID)
	}
}

// 池里有一台挂在**离线**宿主上的设备时，自动选池必须仍然成功 ——
// 只要另有一台在线宿主真的能拉起这个池的镜像。
//
// 这条以前写的是相反的断言（「池内已有设备记录就不该按需拉起」），
// 依据是「池不是空池就别扩容」。那个依据是错的，而且和生产现场冲突：
//
//   - 池「能不能供出设备」的权威定义在 management/postgres/store.go 的
//     fillPoolProvisionability：provisionable = 有在线、非 draining、
//     且没有被证明缺该镜像的 docker_emulator 宿主。它**不看**池里有没有设备记录。
//   - 按旧断言，一台掉线宿主上的僵尸设备就能让整个池永远无法被自动选中，
//     而面板上这个池明明显示「可供给」。两处口径必须一致。
//   - 生产上真正踩到的也是这一类：设备还在池成员表里，但已经不会再被使用。
//
// 所以断言反过来：这里池可以服务，应该被选中。
func TestFarmSelectsPoolWhenAnOnlineHostCanStillBuildItsImage(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "offline")
	seedSelectorHost(t, db, "host_0000000000002", "online")
	seedSelectorImage(t, db, "image_00000000001", 34, "ready")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 4)
	// 这台设备挂在离线宿主上、且没有被任何预约持有：不是「被占用」，
	// 只是暂时不可分配。池本身仍然可以由 host2 按需拉起。
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	service := newSelectorService(db)
	poolID, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34))
	if err != nil {
		t.Fatalf("另有一台在线宿主能拉起该池镜像时，池应可选，实际 err=%v", err)
	}
	if poolID != "pool_0000000000001" {
		t.Fatalf("应选中 pool_0000000000001，实际 %q", poolID)
	}
}
// 跨仓库契约：Alcor 真实发出的预约载荷必须能被农场原样接受并选池。
//
// 这不是重复上面的用例，而是锁住两个仓库之间的**接口形状**。Alcor 侧
// （android_executor.go / androidReservationCapabilities）在省略 device_pool_id 时
// 会发这样一份 requested_capabilities：
//
//	{"platformName":"Android","androidMinSdkLevel":34}
//
// 农场必须：(1) 接受这个 payload，而不是报「参数无效」或「不支持的 key」；
// (2) 把它当作下限语义选池。任何一侧单独改 key 名或类型，这条会失败 ——
// 而两侧各自的单测都不会失败，这正是最容易被漏掉的集成风险。
func TestFarmAcceptsAlcorDelegatedReservationPayload(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	// 两个池：一个只有 API 30 的设备（装不上 minSdk 34 的 APK），
	// 一个只有 API 34 的设备。农场必须选后者。
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":30}`)
	seedSelectorPool(t, db, "pool_0000000000002", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000002", "pool_0000000000002", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	// 这份 body 与 Alcor 发往 /api/v1/reservations 的形状一致：省略 pool_id。
	body := `{
		"owner_type":"run_attempt",
		"owner_id":"attempt_000000000001",
		"lease_seconds":600,
		"requested_capabilities":{"platformName":"Android","androidMinSdkLevel":34}
	}`
	input, err := decodeCreateInput(t, body)
	if err != nil {
		t.Fatalf("农场不应拒绝 Alcor 的载荷：%v", err)
	}
	if input.PoolIDProvided() {
		t.Fatal("该载荷不带 pool_id，应当被记为「未提供」以触发自动选池")
	}
	service := NewService(db, nil)
	view, createErr := service.Create(context.Background(), testActor(), "idem-key-alcor-contract-01", input)
	if createErr != nil {
		t.Fatalf("农场应当接受 Alcor 的委派载荷并成功建预约：%v", createErr)
	}
	if view.PoolID != "pool_0000000000002" {
		t.Fatalf("minSdk=34 应选中 API 34 的池 pool_0000000000002，实际 %q", view.PoolID)
	}
}

// androidMinSdkLevel 是下限语义：设备 API >= 它就能用，不是「正好等于」。
//
// 这条最容易被写错成精确匹配 —— 那样 minSdk=21 的 APK 会变成「只要 API 21 的设备」，
// 把 API 34 的设备全排除，运行反而找不到设备。
func TestFarmTreatsMinSdkAsLowerBoundNotExactMatch(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	// 设备是 API 34，要求「至少 API 21」——必须能选中。
	got := selectedPoolID(t, db, map[string]any{
		"platformName": "Android", androidMinSdkLevelKey: 21,
	})
	if got != "pool_0000000000001" {
		t.Fatalf("API 34 的设备满足「至少 API 21」，应被选中，实际 %q", got)
	}
}

// 下限高于设备 API -> 排除。
func TestFarmRejectsDeviceBelowMinSdk(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 30)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":30}`)

	service := newSelectorService(db)
	_, err := service.selectPoolForRequest(context.Background(), testActor(), map[string]any{
		"platformName": "Android", androidMinSdkLevelKey: 34,
	})
	if err == nil {
		t.Fatal("设备 API 30 低于 minSdk 34，不应被选中")
	}
}

// 按需拉起这条路也要遵守下限：镜像 API 必须 >= minSdk。
func TestFarmRejectsOnDemandImageBelowMinSdk(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorImage(t, db, "image_00000000001", 30, "ready")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 4)

	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), map[string]any{
		"platformName": "Android", androidMinSdkLevelKey: 34,
	}); err == nil {
		t.Fatal("镜像 API 30 低于 minSdk 34，不应被选中")
	}
}

// 按需拉起且镜像 API 高于下限 -> 应当选中。
func TestFarmAcceptsOnDemandImageAboveMinSdk(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorImage(t, db, "image_00000000001", 34, "ready")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 4)

	got := selectedPoolID(t, db, map[string]any{
		"platformName": "Android", androidMinSdkLevelKey: 21,
	})
	if got != "pool_0000000000001" {
		t.Fatalf("镜像 API 34 满足「至少 API 21」，应被选中，实际 %q", got)
	}
}

// minSdk 不能进 @> 精确匹配：设备能力里没有 androidMinSdkLevel 这个键，
// 若被一起送进 device_schedulable_capabilities()，任何设备都匹配不上。
func TestFarmKeepsMinSdkOutOfExactCapabilityMatch(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	got := selectedPoolID(t, db, map[string]any{
		"platformName": "Android", "apiLevel": 34, androidMinSdkLevelKey: 34,
	})
	if got != "pool_0000000000001" {
		t.Fatalf("minSdk 不该参与精确匹配，实际 %q", got)
	}
}

// 请求里混入农场不认的键不能影响选池，也不能让 SQL 出错。
//
// 真实场景：调用方顺手带上 maxConcurrency（它不是设备能力，不在调度白名单里）。
func TestFarmIgnoresNonSchedulableCapabilityKeys(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	requested := map[string]any{
		"platformName": "Android", "apiLevel": 34, "maxConcurrency": 4,
		"_device_farm_target_device_id": "device_00000000001",
	}
	got := selectedPoolID(t, db, requested)
	if got != "pool_0000000000001" {
		t.Fatalf("应选中可用池，实际 %q", got)
	}
}

// 没有任何池能服务时，错误必须明确可辨（ErrPoolUnavailable 语义），
// 而不是静默返回空 ID 让上层挑一个注定失败的池。
func TestFarmReturnsTypedErrorWhenNothingIsSelectable(t *testing.T) {
	db := openSelectorDatabase(t)
	service := newSelectorService(db)
	_, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34))
	if err == nil {
		t.Fatal("没有任何池时应返回错误")
	}
	if !errors.Is(err, ErrPoolUnavailable) {
		t.Fatalf("错误应满足 ErrPoolUnavailable，实际 %v", err)
	}
}

// 平台匹配但池不是 active -> 排除。
func TestFarmRejectsInactivePool(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)
	if _, err := db.Pool().Exec(context.Background(),
		`UPDATE device_pools SET status='disabled' WHERE id='pool_0000000000001'`); err != nil {
		t.Fatal(err)
	}
	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34)); err == nil {
		t.Fatal("非 active 池不应被选中")
	}
}

// 端到端：省略 pool_id 时，create() 必须把自动选出的池落到预约记录上。
//
// 为什么必须有这个用例：其余选池用例（含 TestServiceAutoSelectsPoolWhenPoolIDOmitted）
// 都是直接调 selectPoolForRequest，只证明「选池函数选得对」，不证明 create() 用了它。
// 反向验证发现：把 create() 里的 input.PoolID = selectedPoolID 改成丢弃结果，
// 那些用例照样全绿。这个盲区会让「pool_id 可选」在真实链路上静默失效 ——
// 选池算出来了，预约却落在别的池（或空池）上。
//
// 所以这里必须走完整 create()，并且断言返回的 View.PoolID 就是选中的那个池。
func TestFarmCreatePersistsAutoSelectedPool(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	// 两个同为 android 的可用池，容量不同。省略 pool_id 时 create() 必须选中其中一个，
	// 并把它的 ID 写进预约 —— 而不是把 PoolID 留空。
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 1)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)
	seedSelectorPool(t, db, "pool_0000000000002", "android", "", 3)
	seedSelectorDevice(t, db, "device_00000000002", "pool_0000000000002", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	input, err := decodeCreateInput(t, `{"owner_type":"run_attempt","owner_id":"attempt_000000000001","lease_seconds":600,"requested_capabilities":{"platformName":"Android","apiLevel":34}}`)
	if err != nil {
		t.Fatal(err)
	}
	if input.PoolIDProvided() {
		t.Fatal("省略 pool_id 不该被记为「已提供」")
	}
	service := NewService(db, nil)
	view, createErr := service.Create(context.Background(), testActor(), "idem-key-autoselect-01", input)
	if createErr != nil {
		t.Fatalf("省略 pool_id 时 create 应当成功（由农场自选池），实际 %v", createErr)
	}
	if view.PoolID == "" {
		t.Fatal("create() 没有把自动选出的池落到预约上：PoolID 为空")
	}
	if view.PoolID != "pool_0000000000001" && view.PoolID != "pool_0000000000002" {
		t.Fatalf("预约落在了一个不可用的池上：%q", view.PoolID)
	}
	// 库里那一行也必须是同一个池 —— 防止只在返回值上贴标签、没写库。
	var stored string
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT pool_id FROM device_reservations WHERE id = $1`, view.ID).Scan(&stored); err != nil {
		t.Fatalf("读取预约行：%v", err)
	}
	if stored != view.PoolID {
		t.Fatalf("返回值 PoolID=%q 与库里 pool_id=%q 不一致", view.PoolID, stored)
	}
}

// 池里只剩一台「租约刚过期、正等回收」的设备时，自动选池必须仍然成功。
//
// 现场（生产 2026-10-08，精确到毫秒）：
//
//	前一次预约 94b0f825  expires_at  = 07:24:12.807
//	新请求              发起于        = 07:24:12.255   ← 早 551 毫秒
//	94b0f825            released_at  = 07:24:43.012   ← 晚 31 秒
//	那台设备被删掉                     = 07:25:40
//
// 那一刻池里只有这一台设备，它还挂在池成员表里、lifecycle 也还没变成 deleted，
// 但它的预约已经终结（过期），不会再被任何人使用。旧判定把这种设备当成
// 「池还占着」，于是 A（要有 ready 设备）和 B（要池内没有非 deleted 设备）
// 两个分支都不成立，自动选池直接返回 DEVICE_POOL_UNAVAILABLE —— 一次本来马上
// 就能扩容的请求被判死。整整 88 秒的窗口内都会这样。
//
// 这条用例锁住修复后的语义：正被回收的设备不阻止按需拉起。
func TestFarmStillSelectsPoolWhileExpiredLeaseIsBeingReclaimed(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	// 池必须显式绑默认镜像，才能走「按需拉起」这条路。
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 1)

	// 那台正在被回收的设备：池成员还在，lifecycle 也不是 deleted，
	// 但它的预约已经过期终结（released_at 已写，reaper 还没删设备）。
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "busy", "healthy", `{"platformName":"Android","apiLevel":34}`)
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO device_reservations
			(id, client_id, pool_id, device_id, owner_type, owner_id, requested_capabilities,
			 lease_seconds, status, idempotency_key, starts_at, expires_at, released_at)
		VALUES ('reservation_expired0000001', 'device_farm_server', 'pool_0000000000001',
		        'device_00000000001', 'run_attempt', 'attempt_000000000001', '{}'::jsonb,
		        900, 'expired', 'idem-expired-lease-01',
		        clock_timestamp() - interval '16 minutes', clock_timestamp() - interval '1 second',
		        clock_timestamp() - interval '1 second')`); err != nil {
		t.Fatalf("seed 已过期预约：%v", err)
	}

	got := selectedPoolID(t, db, androidCapabilities(34))
	if got != "pool_0000000000001" {
		t.Fatalf("租约已过期、设备待回收时，池仍应可选（能按需拉起），实际 %q", got)
	}
}

// 反向：设备被一条**仍然有效**的预约持有时，池确实满，不能被当成「可扩容」。
//
// 没有这条，把上面那个 NOT EXISTS 写成恒真也能过 —— 那样池会被反复选中，
// 每次都在等待回收的设备后面排队，等于把「选不出来」换成「看起来选出来了」。
func TestFarmRejectsPoolHeldByLiveReservation(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 1)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "busy", "healthy", `{"platformName":"Android","apiLevel":34}`)
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO device_reservations
			(id, client_id, pool_id, device_id, owner_type, owner_id, requested_capabilities,
			 lease_seconds, status, idempotency_key, starts_at, expires_at)
		VALUES ('reservation_active00000001', 'device_farm_server', 'pool_0000000000001',
		        'device_00000000001', 'run_attempt', 'attempt_000000000002', '{}'::jsonb,
		        900, 'active', 'idem-live-lease-0001',
		        clock_timestamp() - interval '1 minute', clock_timestamp() + interval '14 minutes')`); err != nil {
		t.Fatalf("seed 有效预约：%v", err)
	}

	service := newSelectorService(db)
	if _, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34)); err == nil {
		t.Fatal("设备被有效预约持有时，池不应被选为「可扩容」")
	}
}

// 生产现场那一刻的准确状态：预约还是 active，但 expires_at 已经过去，
// reaper 还没走到（有 30 秒 grace）。这种「死租约」同样不该阻止扩容。
//
// 与上一条的区别：上一条 expires_at 在未来（真的还在用），这一条已经过期。
// 少了这条，「active 一律算占用」的写法也能让另外两条通过，
// 但生产上真正踩到的正是这个 active+已过期 的组合。
func TestFarmStillSelectsPoolWhileActiveLeaseAlreadyExpired(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 1)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "busy", "healthy", `{"platformName":"Android","apiLevel":34}`)
	// status 仍是 active（reaper 有 grace 期），但 expires_at 已经过去 551 毫秒以上。
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO device_reservations
			(id, client_id, pool_id, device_id, owner_type, owner_id, requested_capabilities,
			 lease_seconds, status, idempotency_key, starts_at, expires_at)
		VALUES ('reservation_deadlease00001', 'device_farm_server', 'pool_0000000000001',
		        'device_00000000001', 'run_attempt', 'attempt_000000000003', '{}'::jsonb,
		        900, 'active', 'idem-dead-lease-0001',
		        clock_timestamp() - interval '16 minutes', clock_timestamp() - interval '1 second')`); err != nil {
		t.Fatalf("seed 死租约：%v", err)
	}

	got := selectedPoolID(t, db, androidCapabilities(34))
	if got != "pool_0000000000001" {
		t.Fatalf("active 但租约已过期（等 reaper 回收）时，池仍应可选，实际 %q", got)
	}
}
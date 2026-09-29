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
// 分支 B 必须**不成立**：池里已经有设备记录，说明这个池不是「还没建过虚拟机」
// 的空池，不该再按默认镜像走「按需拉起」这条路。否则会把运行送进一个已经被
// 不可用设备占住的池，预约等不到设备。
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
	// 分支 B 有条件豁免掉「池里有设备」的情况，但这个池的设备全在离线宿主上，
	// 默认镜像的在线宿主列表也是空的（唯一宿主离线），所以任何分支都不成立。
	if err == nil {
		t.Fatalf("池内设备全不可分配、且没有在线宿主能拉起镜像，不应选中 %q", poolID)
	}
}

// 明确验证「池内已有设备」会阻断按需拉起这条路：另一台在线宿主存在（镜像可拉起），
// 但池里已经有一台不可分配的设备时，不能把它当空池去按需拉起。
func TestFarmEmptyPoolBranchRequiresNoExistingDevices(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "offline")
	seedSelectorHost(t, db, "host_0000000000002", "online")
	seedSelectorImage(t, db, "image_00000000001", 34, "ready")
	seedSelectorPool(t, db, "pool_0000000000001", "android", "image_00000000001", 4)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	service := newSelectorService(db)
	poolID, err := service.selectPoolForRequest(context.Background(), testActor(), androidCapabilities(34))
	if err == nil {
		t.Fatalf("池内已有设备（虽在离线宿主上），不该按「空池按需拉起」选中 %q", poolID)
	}
}
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
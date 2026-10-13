package warmpool_test

import (
	"context"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/poolhost"
	"github.com/Ad-Quanta/alcor-device-farm/internal/warmpool"
)

// 串台回归：池名里带宿主地址时，建机只能落在该地址对应的宿主上。
//
// 背景（正式服实测复现）：池 android-10.0.30.54 的预约，设备却建到 55 上；
// 反向也有一次。根因是宿主选择 SQL 完全不含池的限定，
// 任何在线安卓宿主都是任何池的候选。修复方式是从池名推导目标宿主地址
// （复用矩阵要求不新增表/字段），并只在该宿主上选。
func TestWarmPoolScopesCreationToPoolHostAddress(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 4)

	// 三台宿主：只有 10.0.30.54 是池名指向的那台。host id 顺序刻意让「错的那台」
	// 排在前面 —— 修复前会选中 host...002（10.0.30.55），修复后必须选 host...003。
	seedExtraHost(t, db, "host_000000000000002", "安卓宿主机 - 10.0.30.55", "10.0.30.55")
	seedExtraHost(t, db, "host_000000000000003", "安卓宿主机 - 10.0.30.54", "10.0.30.54")
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_hosts
		SET address='10.0.30.171' WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_pools
		SET name='android-10.0.30.54' WHERE id='pool_000000000000001'`); err != nil {
		t.Fatal(err)
	}

	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}

	var hostID string
	if err := db.Pool().QueryRow(context.Background(), `SELECT host_id FROM device_host_commands
		WHERE command_type='create'`).Scan(&hostID); err != nil {
		t.Fatal(err)
	}
	if hostID != "host_000000000000003" {
		t.Fatalf("池 android-10.0.30.54 的建机必须落在 10.0.30.54 上，实际 host_id=%s（串台未修复）", hostID)
	}
}

// 回退语义：池名里没有地址线索时，行为与修复前完全一致（全农场任选）。
// 既有的 default-android 这类池、以及所有测试夹具都依赖这个回退。
func TestWarmPoolFallsBackWhenPoolNameHasNoAddress(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 4)
	seedExtraHost(t, db, "host_000000000000002", "安卓宿主机 - 10.0.30.55", "10.0.30.55")
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_hosts
		SET address='10.0.30.171' WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	// 池名保持 default-android（无 IP）。
	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 {
		t.Fatalf("无地址线索的池必须回退到全农场任选，result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 1)
}

// 池名指向的宿主离线时，不得"就近"落到别的宿主 —— 宁可排队等它回来。
// 这是串台危害最大的形态：以为在 54 上跑，其实跑在 55。
func TestWarmPoolQueuesInsteadOfUsingAnotherHostWhenScopedHostOffline(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 4)
	seedExtraHost(t, db, "host_000000000000002", "安卓宿主机 - 10.0.30.55", "10.0.30.55")
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_hosts
		SET address='10.0.30.54', status='offline' WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_pools
		SET name='android-10.0.30.54' WHERE id='pool_000000000000001'`); err != nil {
		t.Fatal(err)
	}

	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.DevicesCreated != 0 {
		t.Fatalf("目标宿主离线时必须排队等待，不得改落到其他宿主；实际 DevicesCreated=%d", result.DevicesCreated)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 0)
}

// 池名里的地址**在农场里找不到对应宿主**时，不能把整个池饿死。
//
// 这是回退语义的边界：改名、写错 IP、或那台宿主已被删掉时，
// 池名里的地址匹配不到任何 device_hosts.address。
// 此时必须回退到「全农场任选」，而不是让这个池永远无法建机。
func TestWarmPoolFallsBackWhenPoolAddressMatchesNoHost(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 4)
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_hosts
		SET address='10.0.30.55' WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	// 池名指向一台**不存在**的宿主。
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_pools
		SET name='android-10.0.30.199' WHERE id='pool_000000000000001'`); err != nil {
		t.Fatal(err)
	}

	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 {
		t.Fatalf("池名地址匹配不到任何宿主时必须回退到全农场任选，"+
			"否则该池会被永久饿死；result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 1)
}

// 收窄后，"目标宿主缺镜像"必须报镜像原因（可操作），且**不得**悄悄改用别的宿主。
//
// 这是收窄与镜像门禁的交叉点，也是最能说明"宁可排队也不串台"的用例：
// 池 android-10.0.30.54 的目标宿主 54 上没有该镜像，而 55 上**有**。
// 修复前会建到 55（串台且掩盖了 54 缺镜像的事实）；
// 修复后必须一条都不建，并给出镜像原因。
func TestWarmPoolReportsImageReasonAndNeverUsesOtherHostWhenScopedHostLacksImage(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 4)
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_hosts
		SET address='10.0.30.54' WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	// 另一台宿主：地址是 55，且**已有该镜像的成功校验证据**。
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_hosts(id,name,host_type,address,capacity,status)
		VALUES('host_000000000000002','安卓宿主机 - 10.0.30.55','docker_emulator','10.0.30.55',
		jsonb_build_object('device_slots',4),'online')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_host_commands
		(id,host_id,command_type,payload,status,attempts,max_attempts,idempotency_key,completed_at)
		VALUES('cmd_validate_55_ok','host_000000000000002','validate_image',
		'{"image_id":"image_00000000000001"}','succeeded',1,3,'validate-55-ok',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	// 目标宿主 54 明确**没有**该镜像。
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_host_image_states
		(host_id,image_id,available,error_code,observed_at)
		VALUES('host_000000000000001','image_00000000000001',false,'IMAGE_DIGEST_MISMATCH',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_pools
		SET name='android-10.0.30.54' WHERE id='pool_000000000000001'`); err != nil {
		t.Fatal(err)
	}

	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.DevicesCreated != 0 {
		t.Fatalf("目标宿主缺镜像时不得改用别的宿主，实际 DevicesCreated=%d", result.DevicesCreated)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 0)
	// 必须给出镜像原因，而不是资源原因（资源原因会让运维去清内存，方向错误）。
	assertCount(t, db, `SELECT count(*) FROM device_host_commands
		WHERE command_type='create' AND host_id='host_000000000000002'`, 0)
}

// 池名里的地址必须按整段 IPv4 解析：不能把 `android-16-api36` 这种
// 版本号、或更长数字串的前缀误当地址。
func TestAddressFromPoolNameParsing(t *testing.T) {
	cases := []struct {
		name     string
		wantAddr string
	}{
		{"android-10.0.30.54", "10.0.30.54"},
		{"android-10.0.30.171", "10.0.30.171"},
		{"iOS - 10.0.33.68", "10.0.33.68"},
		{"default-android", ""},
		{"android-16-api36", ""},
		{"warm-host", ""},
		{"pool-1.2.3.4.5", ""},
		{"pool-999.1.1.1", ""},
		{"android-10.0.30.54-extra", "10.0.30.54"},
		{"android-10.0.30.540", ""},
		{"android-10.0.30.5.4", ""},
	}
	for _, testCase := range cases {
		if got := poolhost.AddressFromPoolName(testCase.name); got != testCase.wantAddr {
			t.Errorf("AddressFromPoolName(%q)=%q，期望 %q", testCase.name, got, testCase.wantAddr)
		}
	}
}

func seedExtraHost(t *testing.T, db *database.DB, id, name, address string) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_hosts(id,name,host_type,address,capacity,status)
		VALUES($1,$2,'docker_emulator',$3,jsonb_build_object('device_slots',4),'online')`,
		id, name, address); err != nil {
		t.Fatal(err)
	}
}
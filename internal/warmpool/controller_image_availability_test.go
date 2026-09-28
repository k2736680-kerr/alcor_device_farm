package warmpool_test

import (
	"context"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/warmpool"
)

// seedHostImageState 登记一条「这台宿主上有没有该池镜像」的观测。
func seedHostImageState(t *testing.T, db *database.DB, available bool, observedAt time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_host_image_states
		(host_id,image_id,available,observed_at)
		VALUES('host_000000000000001','image_00000000000001',$1,$2)`, available, observedAt); err != nil {
		t.Fatal(err)
	}
}

// 镜像引用走宿主本地仓库，且创建时只校验不拉取，所以「这台宿主上没有该池镜像」
// 是能被证明的事实。证明过之后调度就不该再往它上面排队注定失败的 create ——
// 这正是正式服上 30 秒一轮 create/delete 空转的来源。
func TestWarmPoolSkipsHostThatProvedItLacksPoolImage(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 1)
	seedHostImageState(t, db, false, time.Now())
	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 0 || result.CapacityMisses != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 0)
}

// 负向记录刻意不过期：宿主「没有这个镜像」不会自己变好，让它过期只会把注定失败
// 的调度放回去重来一遍。这个回归点来自正式服实际故障：池 android-10.0.30.55 被
// 正确排除 30 分钟后记录过期、又被放回自动调度，把一条真实运行卡到 PENDING_TIMEOUT。
func TestWarmPoolKeepsNegativeImageStateForever(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 1)
	// 远超任何 TTL 的一条陈旧负向记录，仍然必须挡住调度。
	seedHostImageState(t, db, false, time.Now().Add(-72*time.Hour))
	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 0 || result.CapacityMisses != 1 {
		t.Fatalf("陈旧的负向记录仍然必须挡住调度，result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 0)
}

// 运维重新验证镜像成功之后，该宿主的负向记录必须被清掉，池才会重新可用 —— 这是
// 负向记录不随时间失效之后，让宿主恢复的唯一正道。
func TestWarmPoolClearsNegativeImageStateAfterSuccessfulValidation(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "validating", 1, 1, 1)
	seedHostImageState(t, db, false, time.Now())
	controller := warmpool.New(db, sequentialGenerator(), nil)
	// 第一轮：把 validate_image 命令派发出去（校验路径必须忽略负向记录，否则死锁）。
	if _, err := controller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='validate_image'", 1)
	// 宿主回报校验成功。
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET
		status='succeeded', result='{"digest_verified":true,"ready":true}'::jsonb,
		completed_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE command_type='validate_image'`); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_image_states
		WHERE host_id='host_000000000000001' AND image_id='image_00000000000001'
		AND available=false`, 0)
}

// 正向证据（宿主机上确实有镜像）不能反过来挡住调度。
func TestWarmPoolKeepsHostWithPositiveImageEvidence(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 1)
	seedHostImageState(t, db, true, time.Now())
	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 1)
}

// 失败的 create 要自动沉淀成「这台宿主没有这个镜像」的证据，不需要人工登记。
func TestWarmPoolLearnsMissingImageFromFailedCreate(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 0, 1, 1)
	seedReadyDevices(t, db, 1)
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_host_commands
		(id,host_id,command_type,payload,status,error_code,attempts,max_attempts,idempotency_key,completed_at)
		VALUES('cmd_failed_create_01','host_000000000000001','create',
		'{"device_id":"scale_device_00000001","image_id":"image_00000000000001"}','failed',
		'IMAGE_DIGEST_MISMATCH',1,3,'failed-create-01',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	if _, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_image_states
		WHERE host_id='host_000000000000001' AND image_id='image_00000000000001'
		AND available=false AND error_code='IMAGE_DIGEST_MISMATCH'`, 1)
}

// 一次成功的 create 是正向证据，要能覆盖掉更早的失败记录，避免把已经修好的
// 宿主一直挡在调度之外。
func TestWarmPoolSuccessClearsEarlierMissingImageEvidence(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 0, 1, 1)
	seedReadyDevices(t, db, 1)
	seedHostImageState(t, db, false, time.Now().Add(-2*time.Minute))
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_host_commands
		(id,host_id,command_type,payload,status,attempts,max_attempts,idempotency_key,completed_at)
		VALUES('cmd_ok_create_01','host_000000000000001','create',
		'{"device_id":"scale_device_00000001","image_id":"image_00000000000001"}','succeeded',
		1,3,'ok-create-01',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	if _, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_image_states
		WHERE host_id='host_000000000000001' AND image_id='image_00000000000001'
		AND available=true AND error_code IS NULL`, 1)
}

// 从没试过的 (宿主, 镜像) 必须保持可调度，否则全新镜像/全新池第一次就永远
// 起不来。
func TestWarmPoolKeepsHostWithoutAnyImageObservation(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 1)
	assertCount(t, db, "SELECT count(*) FROM device_host_image_states", 0)
	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 || result.CapacityMisses != 0 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
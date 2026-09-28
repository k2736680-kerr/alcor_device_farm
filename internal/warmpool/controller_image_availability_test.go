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

// 观测会过期：宿主后来装上镜像之后，不需要人工清记录就能重新参与调度。
func TestWarmPoolRetriesHostOnceImageStateExpires(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 1)
	seedHostImageState(t, db, false, time.Now().Add(-31*time.Minute))
	result, err := warmpool.New(db, sequentialGenerator(), nil).RunOnce(context.Background())
	if err != nil || result.DevicesCreated != 1 || result.CapacityMisses != 0 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	assertCount(t, db, "SELECT count(*) FROM device_host_commands WHERE command_type='create'", 1)
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
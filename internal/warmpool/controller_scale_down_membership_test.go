package warmpool_test

import (
	"context"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/warmpool"
)

// TestScaleDownKeepsPoolMembershipUntilDeleteCompletes 锁定释放链路的孤儿泄漏修复：
// queueScaleDown 排入 delete 命令时不得预先取消 membership，否则 delete 失败后
// 设备变成没有池负责的孤儿（所有清理路径都要求 pd.enabled）。
// 修复语义：物理删除确认（reconcileScaleDownDeletes 看到 deleted=true）前，
// pd.enabled 保持 true；确认后才统一关闭。
func TestScaleDownKeepsPoolMembershipUntilDeleteCompletes(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 0, 1, 2)
	if _, err := db.Pool().Exec(context.Background(),
		`UPDATE device_pools SET total_target=0,min_ready=0 WHERE id='pool_000000000000001';
		 UPDATE device_pool_images SET min_ready=0 WHERE pool_id='pool_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	seedReadyDevices(t, db, 1)
	controller := warmpool.New(db, sequentialGenerator(), nil)

	// target=0、现有 1 台 → 排一条 scale-down delete。
	if result, err := controller.RunOnce(context.Background()); err != nil || result.DeletesQueued != 1 {
		t.Fatalf("scale down queue result=%+v error=%v", result, err)
	}
	// 排命令后：membership 必须仍在（物理删除还没发生）。
	assertCount(t, db, `SELECT count(*) FROM device_pool_devices pd
		JOIN devices d ON d.id=pd.device_id
		WHERE pd.enabled AND d.lifecycle_status='stopped'`, 1)

	// delete 失败并结算：membership 必须仍保留，设备隔离后仍可被清理路径找到。
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='failed',
		error_code='EMULATOR_DELETE_FAILED',completed_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE command_type='delete' AND status='pending'`); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DeletesFailed != 1 {
		t.Fatalf("delete failure result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM device_pool_devices pd
		JOIN devices d ON d.id=pd.device_id
		WHERE pd.enabled AND d.lifecycle_status='quarantined' AND d.health_status='unhealthy'`, 1)

	// 失败命令不得永久阻挡：本轮或下一轮必须重新排入一条 quarantined 清理 delete。
	if result.DeletesQueued == 0 {
		if result, err = controller.RunOnce(context.Background()); err != nil || result.DeletesQueued != 1 {
			t.Fatalf("quarantined retry result=%+v error=%v", result, err)
		}
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_commands
		WHERE command_type='delete' AND status='pending'
		AND payload->>'operation_source'='warm_pool_quarantined_cleanup'`, 1)

	// 清理成功：deleted=true 后 membership 才关闭，设备 deleted。
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='succeeded',
		result='{"deleted":true}',completed_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE command_type='delete' AND status='pending'`); err != nil {
		t.Fatal(err)
	}
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.DeletesCompleted != 1 {
		t.Fatalf("cleanup completion result=%+v error=%v", result, err)
	}
	assertCount(t, db, `SELECT count(*) FROM devices WHERE id='scale_device_00000001'
		AND lifecycle_status='deleted'`, 1)
	assertCount(t, db, `SELECT count(*) FROM device_pool_devices pd
		WHERE pd.device_id='scale_device_00000001' AND pd.enabled`, 0)
}

// TestFailedDeleteCommandDoesNotBlockRetryForever 锁定清理查询重试语义：
// 历史 failed/timed_out delete 命令不得永久阻挡新的清理命令；pending/leased 才阻止。
func TestFailedDeleteCommandDoesNotBlockRetryForever(t *testing.T) {
	db := openTestDatabase(t)
	seedWarmPool(t, db, "ready", 1, 1, 1)
	controller := warmpool.New(db, sequentialGenerator(), nil)
	if result, err := controller.RunOnce(context.Background()); err != nil || result.DevicesCreated != 1 {
		t.Fatalf("initial create result=%+v error=%v", result, err)
	}
	// create 成功 → ready。
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
	// 设备变 quarantined/unhealthy → 排清理 delete。
	if _, err := db.Pool().Exec(context.Background(), `UPDATE devices SET lifecycle_status='quarantined',
		health_status='unhealthy',health_reason='stf invisible',last_seen_at=clock_timestamp(),updated_at=clock_timestamp()`); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunOnce(context.Background())
	if err != nil || result.DeletesQueued != 1 {
		t.Fatalf("quarantined cleanup queue result=%+v error=%v", result, err)
	}
	// 这条 delete 失败：结算后必须能（同轮或下一轮）重新排入新的清理 delete。
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_commands SET status='failed',
		error_code='EMULATOR_DELETE_FAILED',completed_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE command_type='delete' AND status='pending'`); err != nil {
		t.Fatal(err)
	}
	result, err = controller.RunOnce(context.Background())
	if err != nil || result.DeletesFailed != 1 {
		t.Fatalf("cleanup settle result=%+v error=%v", result, err)
	}
	if result.DeletesQueued == 0 {
		if result, err = controller.RunOnce(context.Background()); err != nil || result.DeletesQueued != 1 {
			t.Fatalf("cleanup retry must be re-queued after failure, result=%+v error=%v", result, err)
		}
	}
	assertCount(t, db, `SELECT count(*) FROM device_host_commands WHERE command_type='delete'
		AND status='pending' AND payload->>'operation_source'='warm_pool_quarantined_cleanup'`, 1)
}

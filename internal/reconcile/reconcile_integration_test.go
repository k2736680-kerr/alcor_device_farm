package reconcile_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	managementpostgres "github.com/Ad-Quanta/alcor-device-farm/internal/management/postgres"
	providermock "github.com/Ad-Quanta/alcor-device-farm/internal/providers/mock"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reconcile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/scheduler"
)

type fixedVisibility struct {
	visible bool
	err     error
}

type sequenceVisibility struct {
	calls    int
	failures int
}

func (value *sequenceVisibility) Visible(context.Context, string) (bool, error) {
	value.calls++
	return value.calls > value.failures, nil
}

func (value fixedVisibility) Visible(context.Context, string) (bool, error) {
	return value.visible, value.err
}

func TestProviderMissingQuarantinesReadyDatabaseDevice(t *testing.T) {
	environment := newEnvironment(t, false)
	service := reconcile.New(environment.db, environment.provider, nil, 3, 0, 0, testLogger())
	result, err := service.RunOnce(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if result.DevicesQuarantined != 1 {
		t.Fatalf("result=%+v", result)
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 1)
	assertEvent(t, environment.db, "provider_device_missing")
}

func TestStaleDrainingHostKeepsOperatorDrainIntent(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_hosts
		SET status='draining',draining=true,last_heartbeat_at=clock_timestamp()-interval '31 seconds'
		WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, environment.provider, nil, 3, 0, 0, testLogger())
	result, err := service.RunOnce(context.Background(), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.HostsMarkedOffline != 0 {
		t.Fatalf("result=%+v", result)
	}
	var hostStatus string
	var draining bool
	if err := environment.db.Pool().QueryRow(context.Background(), "SELECT status,draining FROM device_hosts WHERE id='host_000000000000001'").Scan(&hostStatus, &draining); err != nil {
		t.Fatal(err)
	}
	if hostStatus != "draining" || !draining {
		t.Fatalf("host status=%s draining=%v", hostStatus, draining)
	}
	var schedulable int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM devices d
        JOIN device_pool_devices pd ON pd.device_id=d.id
        JOIN device_hosts h ON h.id=d.host_id
        WHERE pd.pool_id='pool_000000000000001' AND d.lifecycle_status='ready'
          AND d.health_status='healthy' AND h.status='online' AND NOT h.draining`).Scan(&schedulable); err != nil {
		t.Fatal(err)
	}
	if schedulable != 0 {
		t.Fatalf("schedulable devices=%d", schedulable)
	}
}

func TestRecoveredHostAndSTFVisibilityClearStaleFailureCounter(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_hosts
		SET last_heartbeat_at=clock_timestamp()-interval '31 seconds' WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, nil, fixedVisibility{visible: true}, 3, 0, 0, testLogger())
	if _, err := service.RunOnce(context.Background(), 30*time.Second); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "degraded", 1)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_hosts
		SET status='online',last_heartbeat_at=clock_timestamp() WHERE id='host_000000000000001';
		UPDATE devices SET health_status='healthy',health_reason=NULL WHERE id='device_0000000000001'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunOnce(context.Background(), 30*time.Second); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "healthy", 0)
	assertEvent(t, environment.db, "health_recovered")
}

func TestRecentlyOfflineHostDoesNotMutateDeviceDuringRecoveryGrace(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_hosts
		SET status='offline',last_heartbeat_at=clock_timestamp()-interval '31 seconds'
		WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, nil, nil, 2, 0, time.Minute, testLogger())
	result, err := service.RunOnce(context.Background(), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.EventsRecorded != 0 || result.DevicesQuarantined != 0 {
		t.Fatalf("recovery grace result=%+v", result)
	}
	assertDevice(t, environment.db, "ready", "healthy", 0)
	var events int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_health_events
		WHERE device_id='device_0000000000001'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("recovery grace health events=%d", events)
	}
}

func TestRepeatedAppiumFailureQuarantinesAndManualRecoveryResetsCounter(t *testing.T) {
	environment := newEnvironment(t, true)
	environment.provider.SetScenario(providermock.Scenario{AppiumUnhealthy: true})
	service := reconcile.New(environment.db, environment.provider, nil, 2, 0, 0, testLogger())
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "unhealthy", 1)
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 2)

	environment.provider.SetScenario(providermock.Scenario{})
	if _, err := environment.management.UnquarantineDeviceAudited(context.Background(), "device_0000000000001",
		"operator confirmed rebuild path", audit.Service("test-operator"), "request-reconcile-recovery"); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "provisioning", "unknown", 0)
}

func TestSTFInvisibleConvergesToQuarantineAndSameDeviceAutoRecovers(t *testing.T) {
	environment := newEnvironment(t, true)
	service := reconcile.New(environment.db, environment.provider, fixedVisibility{visible: false}, 2, 0, 0, testLogger())
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 2)
	assertEvent(t, environment.db, "stf_not_visible")

	healthyService := reconcile.New(environment.db, environment.provider, fixedVisibility{visible: true}, 2, 0, 0, testLogger())
	if _, err := healthyService.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "healthy", 0)
	assertEvent(t, environment.db, "health_recovered")
}

func TestIOSDeviceDoesNotUseAndroidProviderOrSTFHealthChain(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `
		DELETE FROM device_pool_devices WHERE device_id='device_0000000000001';
		UPDATE device_pools SET platform='ios' WHERE id='pool_000000000000001';
		UPDATE device_hosts SET host_type='appium_device_farm_ios',host_os='macos',host_arch='arm64'
		WHERE id='host_000000000000001';
		UPDATE devices SET platform='ios',image_id=NULL,device_kind='simulator',
			provider_type='appium_device_farm_ios',provider_ref='SIM-IOS-1',serial='SIM-IOS-1',
			capabilities='{"componentHealth":{"transport":"passed","automation":"passed","router":"passed"}}',
			lifecycle_status='ready',health_status='healthy',health_reason=NULL,consecutive_failures=0
		WHERE id='device_0000000000001';
		INSERT INTO device_pool_devices(pool_id,device_id,enabled)
		VALUES('pool_000000000000001','device_0000000000001',true)`); err != nil {
		t.Fatal(err)
	}
	visibility := &sequenceVisibility{failures: 10}
	service := reconcile.New(environment.db, environment.provider, visibility, 2, 0, 0, testLogger())
	result, err := service.RunOnce(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if visibility.calls != 0 || result.EventsRecorded != 0 || result.DevicesQuarantined != 0 {
		t.Fatalf("iOS reconcile result=%+v STF calls=%d", result, visibility.calls)
	}
	assertDevice(t, environment.db, "ready", "healthy", 0)
}

func TestIOSSharedAutomationStabilizationDoesNotQuarantineSimulator(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `
		DELETE FROM device_pool_devices WHERE device_id='device_0000000000001';
		UPDATE device_pools SET platform='ios' WHERE id='pool_000000000000001';
		UPDATE device_hosts SET host_type='appium_device_farm_ios',host_os='macos',host_arch='arm64'
		WHERE id='host_000000000000001';
		UPDATE devices SET platform='ios',image_id=NULL,device_kind='simulator',provider_type='appium_device_farm_ios',
			provider_ref='SIM-IOS-1',serial='SIM-IOS-1',lifecycle_status='ready',health_status='degraded',
			health_reason='agent heartbeat reported degraded',consecutive_failures=0
		WHERE id='device_0000000000001';
		INSERT INTO device_pool_devices(pool_id,device_id,enabled)
		VALUES('pool_000000000000001','device_0000000000001',true)`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, nil, nil, 3, 0, 0, testLogger())
	for range 5 {
		result, err := service.RunOnce(context.Background(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if result.DevicesQuarantined != 0 {
			t.Fatalf("iOS stabilization quarantined device: %+v", result)
		}
	}
	assertDevice(t, environment.db, "ready", "degraded", 0)
	assertEvent(t, environment.db, "ios_automation_stabilizing")
	var events int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_health_events
		WHERE device_id='device_0000000000001' AND event_type='ios_automation_stabilizing'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("deduplicated iOS stabilization events=%d", events)
	}
}

func TestSTFVisibilityGraceDoesNotConsumeFailureBudgetAfterProvisioning(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `INSERT INTO device_host_commands
		(id,host_id,command_type,payload,status,attempts,max_attempts,idempotency_key,result,completed_at)
		VALUES('command_000000000009','host_000000000000001','rebuild',
		'{"device_id":"device_0000000000001"}','succeeded',1,3,'recent-rebuild',
		'{}',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, nil, fixedVisibility{visible: false}, 2, 30*time.Second, 0, testLogger())
	result, err := service.RunOnce(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if result.EventsRecorded != 1 || result.DevicesQuarantined != 0 {
		t.Fatalf("grace reconcile result=%+v", result)
	}
	assertDevice(t, environment.db, "ready", "unhealthy", 0)

	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_host_commands
		SET created_at=created_at-interval '31 seconds',completed_at=completed_at-interval '31 seconds'
		WHERE id='command_000000000009'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "unhealthy", 1)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_health_events
		SET observed_at=observed_at-interval '31 seconds',created_at=created_at-interval '31 seconds'
		WHERE device_id='device_0000000000001' AND event_type='stf_not_visible'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 2)
}

func TestSTFReadinessStabilizationBlocksSchedulingUntilGraceExpires(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE devices
		SET lifecycle_status='provisioning',health_status='unknown',health_reason=NULL
		WHERE id='device_0000000000001'`); err != nil {
		t.Fatal(err)
	}
	commandService := hostcommand.New(environment.db)
	createdCommand, err := commandService.Create(context.Background(), "host_000000000000001", "rebuild", map[string]any{
		"operation_source": "management", "device_id": "device_0000000000001",
		"provider_ref": "mock-reconcile-device", "operation_state": "provisioning",
	}, 3, "recent-visible-rebuild")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := commandService.Claim(context.Background(), "host_000000000000001", hostcommand.ClaimInput{LeaseSeconds: 30, MaxCommands: 1})
	if err != nil || len(claimed) != 1 || claimed[0].LeaseToken == nil {
		t.Fatalf("claimed command=%#v error=%v", claimed, err)
	}
	if _, err := commandService.Complete(context.Background(), createdCommand.ID, hostcommand.CompletionInput{
		LeaseToken: *claimed[0].LeaseToken, Attempt: claimed[0].Attempt, Status: "succeeded",
		Result: map[string]any{
			"generation": 2,
			"connection": map[string]any{"serial": "10.0.0.1:31000", "adb_endpoint": "10.0.0.1:31000",
				"appium_endpoint": "http://10.0.0.1:32000", "appium_udid": "emulator-5554"},
			"health": map[string]any{"online": true, "adb_online": true, "boot_completed": true, "appium_healthy": true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "unhealthy", 0)

	reservationService := reservation.NewService(environment.db, nil)
	created, err := reservationService.Create(context.Background(), audit.Service("test-worker"), "stf-stabilization-reservation", reservation.CreateInput{
		PoolID: "pool_000000000000001", OwnerType: "run_attempt", OwnerID: "attempt_000000000901",
		RequestedCapabilities: map[string]any{"platformName": "Android"}, LeaseSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	deviceScheduler := scheduler.New(environment.db, nil, testLogger())
	if _, err := deviceScheduler.RunOnce(context.Background()); !errors.Is(err, scheduler.ErrCapacityUnavailable) {
		t.Fatalf("scheduler during stabilization error=%v", err)
	}
	stored, err := reservationService.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "pending" || stored.DeviceID != nil {
		t.Fatalf("reservation during stabilization=%#v", stored)
	}
	service := reconcile.New(environment.db, nil, fixedVisibility{visible: true}, 2, 30*time.Second, 0, testLogger())
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "unhealthy", 0)
	assertEvent(t, environment.db, "stf_stabilizing")

	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_host_commands
		SET created_at=created_at-interval '31 seconds',completed_at=completed_at-interval '31 seconds'
		WHERE id=$1`, createdCommand.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "healthy", 0)
	assertEvent(t, environment.db, "health_recovered")
	assignment, err := deviceScheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if assignment.Reservation.ID != created.ID || assignment.Reservation.Status != "active" {
		t.Fatalf("assignment after stabilization=%#v", assignment)
	}
}

func TestRecoveredSTFVisibilityClearsReconcilerFailureBeforeQuarantine(t *testing.T) {
	environment := newEnvironment(t, true)
	visibility := &sequenceVisibility{failures: 1}
	service := reconcile.New(environment.db, nil, visibility, 3, 0, 0, testLogger())
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "unhealthy", 1)
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "healthy", 0)
	assertEvent(t, environment.db, "health_recovered")
}

func TestRecoveredSTFVisibilityClearsFailuresEvenAfterCountThresholdDuringGrace(t *testing.T) {
	environment := newEnvironment(t, true)
	visibility := &sequenceVisibility{failures: 3}
	service := reconcile.New(environment.db, nil, visibility, 2, 30*time.Second, 0, testLogger())
	for range 3 {
		if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	assertDevice(t, environment.db, "ready", "unhealthy", 3)
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "healthy", 0)
	assertEvent(t, environment.db, "health_recovered")
}

func TestPersistentSTFVisibilityFailureQuarantinesAfterGrace(t *testing.T) {
	environment := newEnvironment(t, true)
	service := reconcile.New(environment.db, nil, fixedVisibility{visible: false}, 2, 30*time.Second, 0, testLogger())
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "ready", "unhealthy", 1)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_health_events
		SET observed_at=observed_at-interval '31 seconds',created_at=created_at-interval '31 seconds'
		WHERE device_id='device_0000000000001' AND event_type='stf_not_visible'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 2)
}

func TestHealthReportValidationAndMissingDevice(t *testing.T) {
	environment := newEnvironment(t, true)
	service := reconcile.New(environment.db, environment.provider, nil, 3, 0, 0, testLogger())
	if _, err := service.Report(context.Background(), "bad", reconcile.EventInput{}); !errors.Is(err, reconcile.ErrInvalidArgument) {
		t.Fatalf("invalid event error=%v", err)
	}
	_, err := service.Report(context.Background(), "missing_00000000001", reconcile.EventInput{
		Source: "agent", EventType: "adb_offline", Severity: "error", Reason: "ADB is offline", ObservedAt: time.Now().UTC(),
	})
	if !errors.Is(err, reconcile.ErrNotFound) {
		t.Fatalf("missing device error=%v", err)
	}
}

func TestAgentReportedUnhealthyQuarantinesWithoutServerProviderAccess(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE devices SET health_status='unhealthy'
		WHERE id='device_0000000000001'`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, nil, nil, 2, 0, 0, testLogger())
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 2)
}

func TestPersistentSystemQuarantineQueuesOnlyNonDestructiveRestart(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE devices SET health_status='unhealthy'
		WHERE id='device_0000000000001'`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, nil, nil, 2, 0, 0, testLogger())
	for range 2 {
		if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 2)
	result, err := service.RunOnce(context.Background(), time.Hour)
	if err != nil || result.RestartsQueued != 1 {
		t.Fatalf("self-healing result=%+v error=%v", result, err)
	}
	assertDevice(t, environment.db, "provisioning", "unknown", 2)
	var restartCommands, destructiveCommands int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT
		count(*) FILTER (WHERE command_type='restart' AND payload->>'operation_source'='self_healing'),
		count(*) FILTER (WHERE command_type IN ('delete','rebuild','create'))
		FROM device_host_commands WHERE payload->>'device_id'='device_0000000000001'`).Scan(&restartCommands, &destructiveCommands); err != nil {
		t.Fatal(err)
	}
	if restartCommands != 1 || destructiveCommands != 0 {
		t.Fatalf("restart commands=%d destructive commands=%d", restartCommands, destructiveCommands)
	}
	if result, err = service.RunOnce(context.Background(), time.Hour); err != nil || result.RestartsQueued != 0 {
		t.Fatalf("duplicate self-healing result=%+v error=%v", result, err)
	}
}

func TestIdleBootingUnhealthyDeviceQueuesOnlyNonDestructiveRestart(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE devices SET
		lifecycle_status='booting',health_status='unhealthy' WHERE id='device_0000000000001'`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, nil, nil, 2, 0, 0, testLogger())
	for range 2 {
		if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 2)
	result, err := service.RunOnce(context.Background(), time.Hour)
	if err != nil || result.RestartsQueued != 1 {
		t.Fatalf("self-healing result=%+v error=%v", result, err)
	}
	var providerRef string
	var restartCommands, destructiveCommands int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT provider_ref FROM devices
		WHERE id='device_0000000000001'`).Scan(&providerRef); err != nil {
		t.Fatal(err)
	}
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT
		count(*) FILTER (WHERE command_type='restart' AND payload->>'operation_source'='self_healing'),
		count(*) FILTER (WHERE command_type IN ('delete','rebuild','create'))
		FROM device_host_commands WHERE payload->>'device_id'='device_0000000000001'`).Scan(&restartCommands, &destructiveCommands); err != nil {
		t.Fatal(err)
	}
	if providerRef == "" || restartCommands != 1 || destructiveCommands != 0 {
		t.Fatalf("provider_ref=%q restart commands=%d destructive commands=%d", providerRef, restartCommands, destructiveCommands)
	}
	if result, err = service.RunOnce(context.Background(), time.Hour); err != nil || result.RestartsQueued != 0 {
		t.Fatalf("duplicate self-healing result=%+v error=%v", result, err)
	}
}

func TestAutomaticHostMaintenanceUsesRecoveryGraceBeforeQuarantine(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_hosts SET status='maintenance',
		capabilities=capabilities || jsonb_build_object('host_readiness_auto_maintenance',true,
		'host_readiness_failure_started_at',clock_timestamp()) WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, nil, nil, 2, 0, time.Minute, testLogger())
	for range 3 {
		result, err := service.RunOnce(context.Background(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if result.DevicesQuarantined != 0 {
			t.Fatalf("恢复宽限期内发生隔离：%+v", result)
		}
	}
	assertDevice(t, environment.db, "ready", "degraded", 0)

	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_hosts SET
		capabilities=jsonb_set(capabilities,'{host_readiness_failure_started_at}',to_jsonb(clock_timestamp()-interval '2 minutes'))
		WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	assertDevice(t, environment.db, "quarantined", "degraded", 2)
}

func TestInFlightCreateOrRebuildIsNotQuarantinedWhileBooting(t *testing.T) {
	environment := newEnvironment(t, false)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE devices SET
		lifecycle_status='booting',health_status='unhealthy',provider_ref='missing-provider-device'
		WHERE id='device_0000000000001';
		INSERT INTO device_host_commands(id,host_id,command_type,payload,status,max_attempts,idempotency_key)
		VALUES('command_000000000001','host_000000000000001','rebuild',
		'{"device_id":"device_0000000000001","provider_ref":"missing-provider-device"}','pending',3,'rebuild-in-flight')`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, environment.provider, fixedVisibility{visible: false}, 2, 0, 0, testLogger())
	for range 3 {
		result, err := service.RunOnce(context.Background(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if result.DevicesChecked != 0 || result.EventsRecorded != 0 || result.DevicesQuarantined != 0 {
			t.Fatalf("in-flight reconcile result=%+v", result)
		}
	}
	assertDevice(t, environment.db, "booting", "unhealthy", 0)
}

func TestInFlightDeleteIsNotQuarantinedAfterProviderResourceDisappears(t *testing.T) {
	environment := newEnvironment(t, false)
	if _, err := environment.db.Pool().Exec(context.Background(), `INSERT INTO device_host_commands
		(id,host_id,command_type,payload,status,max_attempts,idempotency_key)
		VALUES('command_000000000001','host_000000000000001','delete',
		'{"device_id":"device_0000000000001","provider_ref":"missing-provider-device"}',
		'pending',3,'delete-in-flight')`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, environment.provider, fixedVisibility{visible: false}, 1, 0, 0, testLogger())
	for range 3 {
		result, err := service.RunOnce(context.Background(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if result.DevicesChecked != 0 || result.EventsRecorded != 0 || result.DevicesQuarantined != 0 {
			t.Fatalf("删除中的 reconcile 结果=%+v", result)
		}
	}
	assertDevice(t, environment.db, "ready", "healthy", 0)
}

type environment struct {
	db         *database.DB
	provider   *providermock.Provider
	management *management.Service
}

func newEnvironment(t *testing.T, createProviderDevice bool) environment {
	t.Helper()
	databaseURL := os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEVICE_FARM_TEST_DATABASE_URL is not set")
	}
	db, err := database.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	statements := []string{
		`TRUNCATE TABLE device_idempotency_records,device_audit_events,device_health_events,device_sessions,
            device_reservations,device_pool_devices,devices,device_pool_images,device_pools,
            device_host_commands,device_hosts,device_images RESTART IDENTITY CASCADE`,
		`INSERT INTO device_hosts (id,name,host_type,status,draining,last_heartbeat_at)
            VALUES ('host_000000000000001','reconcile-host','docker_emulator','online',false,clock_timestamp())`,
		`INSERT INTO device_images (id,name,docker_image,docker_digest,api_level,abi,resolution,status)
			VALUES ('image_00000000000001','reconcile-image','registry.example/alcor/android-emulator:api34','sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',34,'x86_64','1080x2400','ready')`,
		`INSERT INTO device_pools (id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
            VALUES ('pool_000000000000001','reconcile-pool',600,1200,1,'active')`,
	}
	for _, statement := range statements {
		if _, err := db.Pool().Exec(context.Background(), statement); err != nil {
			t.Fatal(err)
		}
	}
	provider := providermock.New(providermock.Config{})
	store := managementpostgres.New(db)
	managementService := management.NewService(store, provider, nil)
	if createProviderDevice {
		if _, err := managementService.ProvisionMockDevice(context.Background(), management.ProvisionMockDeviceInput{
			ID: "device_0000000000001", HostID: "host_000000000000001", ImageID: "image_00000000000001",
			ProviderRef: "mock-reconcile-device", Capabilities: map[string]any{"platformName": "Android"},
		}); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := db.Pool().Exec(context.Background(), `INSERT INTO devices
            (id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,
             capabilities,lifecycle_status,health_status)
            VALUES ('device_0000000000001','host_000000000000001','image_00000000000001','emulator',
            'mock','missing-provider-device','rebuild','emulator-missing','{}','ready','healthy')`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_pool_devices (pool_id,device_id,enabled)
        VALUES ('pool_000000000000001','device_0000000000001',true)`); err != nil {
		t.Fatal(err)
	}
	return environment{db: db, provider: provider, management: managementService}
}

func assertDevice(t *testing.T, db *database.DB, lifecycle, health string, failures int) {
	t.Helper()
	var gotLifecycle, gotHealth string
	var gotFailures int
	if err := db.Pool().QueryRow(context.Background(), `SELECT lifecycle_status,health_status,consecutive_failures
        FROM devices WHERE id='device_0000000000001'`).Scan(&gotLifecycle, &gotHealth, &gotFailures); err != nil {
		t.Fatal(err)
	}
	if gotLifecycle != lifecycle || gotHealth != health || gotFailures != failures {
		t.Fatalf("device lifecycle=%s health=%s failures=%d", gotLifecycle, gotHealth, gotFailures)
	}
}

func assertEvent(t *testing.T, db *database.DB, eventType string) {
	t.Helper()
	var count int
	if err := db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM device_health_events WHERE event_type=$1", eventType).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Fatalf("event %s count=%d", eventType, count)
	}
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

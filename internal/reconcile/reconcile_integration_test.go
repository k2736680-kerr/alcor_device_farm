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
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	managementpostgres "github.com/Ad-Quanta/alcor-device-farm/internal/management/postgres"
	providermock "github.com/Ad-Quanta/alcor-device-farm/internal/providers/mock"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reconcile"
)

type fixedVisibility struct {
	visible bool
	err     error
}

func (value fixedVisibility) Visible(context.Context, string) (bool, error) {
	return value.visible, value.err
}

func TestProviderMissingQuarantinesReadyDatabaseDevice(t *testing.T) {
	environment := newEnvironment(t, false)
	service := reconcile.New(environment.db, environment.provider, nil, 3, testLogger())
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

func TestStaleHostGoesOfflineAndDeviceStopsScheduling(t *testing.T) {
	environment := newEnvironment(t, true)
	if _, err := environment.db.Pool().Exec(context.Background(), `UPDATE device_hosts
        SET last_heartbeat_at=clock_timestamp()-interval '31 seconds' WHERE id='host_000000000000001'`); err != nil {
		t.Fatal(err)
	}
	service := reconcile.New(environment.db, environment.provider, nil, 3, testLogger())
	result, err := service.RunOnce(context.Background(), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.HostsMarkedOffline != 1 {
		t.Fatalf("result=%+v", result)
	}
	var hostStatus string
	if err := environment.db.Pool().QueryRow(context.Background(), "SELECT status FROM device_hosts WHERE id='host_000000000000001'").Scan(&hostStatus); err != nil {
		t.Fatal(err)
	}
	if hostStatus != "offline" {
		t.Fatalf("host status=%s", hostStatus)
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

func TestRepeatedAppiumFailureQuarantinesAndManualRecoveryResetsCounter(t *testing.T) {
	environment := newEnvironment(t, true)
	environment.provider.SetScenario(providermock.Scenario{AppiumUnhealthy: true})
	service := reconcile.New(environment.db, environment.provider, nil, 2, testLogger())
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

func TestSTFInvisibleConvergesToQuarantineButHealthyDoesNotAutoRecover(t *testing.T) {
	environment := newEnvironment(t, true)
	service := reconcile.New(environment.db, environment.provider, fixedVisibility{visible: false}, 2, testLogger())
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 2)
	assertEvent(t, environment.db, "stf_not_visible")

	healthyService := reconcile.New(environment.db, environment.provider, fixedVisibility{visible: true}, 2, testLogger())
	if _, err := healthyService.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 2)
}

func TestHealthReportValidationAndMissingDevice(t *testing.T) {
	environment := newEnvironment(t, true)
	service := reconcile.New(environment.db, environment.provider, nil, 3, testLogger())
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
	service := reconcile.New(environment.db, nil, nil, 2, testLogger())
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunOnce(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertDevice(t, environment.db, "quarantined", "unhealthy", 2)
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
	service := reconcile.New(environment.db, environment.provider, fixedVisibility{visible: false}, 2, testLogger())
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

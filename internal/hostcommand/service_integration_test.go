package hostcommand_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
)

func TestHeartbeatBringsOfflineHostOnline(t *testing.T) {
	db := openTestDatabase(t)
	seedHost(t, db)
	seedDevice(t, db, "device_0000000000001", "host_000000000000001", "container-1", "pending-container-1", nil, nil, "provisioning", "unknown")
	service := hostcommand.New(db)
	result, err := service.Heartbeat(context.Background(), "host_000000000000001", hostcommand.HeartbeatInput{
		AgentTime: time.Now().UTC(), Capacity: map[string]any{"cpu": 8, "device_slots": 2},
		Devices: []hostcommand.DiscoveredDevice{{ProviderRef: "container-1", Serial: "emulator-5554", LifecycleStatus: "ready", HealthStatus: "healthy",
			Connection: map[string]any{"adb_endpoint": "10.0.0.8:31000", "appium_endpoint": "http://10.0.0.8:4723", "appium_udid": "emulator-5554"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "online" || result.Devices != 1 || result.ReceivedAt.IsZero() {
		t.Fatalf("heartbeat result=%+v", result)
	}
	var status, lifecycle, health, serial string
	var adbEndpoint, appiumEndpoint *string
	var heartbeat *time.Time
	if err := db.Pool().QueryRow(context.Background(), "SELECT status,last_heartbeat_at FROM device_hosts WHERE id=$1", result.HostID).Scan(&status, &heartbeat); err != nil {
		t.Fatal(err)
	}
	if status != "online" || heartbeat == nil {
		t.Fatalf("host status=%s heartbeat=%v", status, heartbeat)
	}
	var lastSeen *time.Time
	var appiumUDID string
	if err := db.Pool().QueryRow(context.Background(), `SELECT lifecycle_status,health_status,serial,adb_endpoint,appium_endpoint,
		COALESCE(capabilities->>'appiumUdid',''),last_seen_at FROM devices WHERE id='device_0000000000001'`).
		Scan(&lifecycle, &health, &serial, &adbEndpoint, &appiumEndpoint, &appiumUDID, &lastSeen); err != nil {
		t.Fatal(err)
	}
	if lifecycle != "ready" || health != "healthy" || serial != "emulator-5554" || adbEndpoint == nil || *adbEndpoint != "10.0.0.8:31000" ||
		appiumEndpoint == nil || *appiumEndpoint != "http://10.0.0.8:4723" || appiumUDID != "emulator-5554" || lastSeen == nil {
		t.Fatalf("device lifecycle=%s health=%s serial=%s adb=%v appium=%v appium_udid=%s last_seen=%v", lifecycle, health, serial, adbEndpoint, appiumEndpoint, appiumUDID, lastSeen)
	}
}

func TestHeartbeatDoesNotMaskSTFUnhealthyState(t *testing.T) {
	db := openTestDatabase(t)
	seedHost(t, db)
	seedDevice(t, db, "device_0000000000001", "host_000000000000001", "container-1", "emulator-5554",
		nil, nil, "ready", "unhealthy")
	if _, err := db.Pool().Exec(context.Background(), `UPDATE devices SET
		health_reason='device is not visible through STF',consecutive_failures=2
		WHERE id='device_0000000000001'`); err != nil {
		t.Fatal(err)
	}
	_, err := hostcommand.New(db).Heartbeat(context.Background(), "host_000000000000001", hostcommand.HeartbeatInput{
		AgentTime: time.Now().UTC(), Capacity: map[string]any{"device_slots": 1},
		Devices: []hostcommand.DiscoveredDevice{{ProviderRef: "container-1", Serial: "emulator-5554",
			LifecycleStatus: "ready", HealthStatus: "healthy", Connection: map[string]any{
				"adb_endpoint": "10.0.0.8:31000", "appium_endpoint": "http://10.0.0.8:4723", "appium_udid": "emulator-5554"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var health, reason string
	var failures int
	if err := db.Pool().QueryRow(context.Background(), `SELECT health_status,health_reason,consecutive_failures
		FROM devices WHERE id='device_0000000000001'`).Scan(&health, &reason, &failures); err != nil {
		t.Fatal(err)
	}
	if health != "unhealthy" || reason != "device is not visible through STF" || failures != 2 {
		t.Fatalf("health=%s reason=%q failures=%d", health, reason, failures)
	}
}

func TestStoppedHeartbeatMayOmitConnectionAndPreservesLastKnownEndpoints(t *testing.T) {
	db := openTestDatabase(t)
	seedHost(t, db)
	adbEndpoint, appiumEndpoint := "10.0.0.8:31000", "http://10.0.0.8:4723"
	seedDevice(t, db, "device_0000000000001", "host_000000000000001", "container-1", "emulator-5554",
		&adbEndpoint, &appiumEndpoint, "ready", "healthy")
	result, err := hostcommand.New(db).Heartbeat(context.Background(), "host_000000000000001", hostcommand.HeartbeatInput{
		AgentTime: time.Now().UTC(), Capacity: map[string]any{"device_slots": 1},
		Devices: []hostcommand.DiscoveredDevice{{ProviderRef: "container-1", LifecycleStatus: "stopped", HealthStatus: "unknown",
			Connection: map[string]any{"adb_endpoint": "", "appium_endpoint": "", "appium_udid": ""}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "online" || result.Devices != 1 {
		t.Fatalf("heartbeat result=%+v", result)
	}
	var lifecycle, health, serial string
	var actualADB, actualAppium *string
	var lastSeen *time.Time
	if err := db.Pool().QueryRow(context.Background(), `SELECT lifecycle_status,health_status,serial,
		adb_endpoint,appium_endpoint,last_seen_at FROM devices WHERE id='device_0000000000001'`).Scan(
		&lifecycle, &health, &serial, &actualADB, &actualAppium, &lastSeen); err != nil {
		t.Fatal(err)
	}
	if lifecycle != "stopped" || health != "unknown" || serial != "emulator-5554" ||
		actualADB == nil || *actualADB != adbEndpoint || actualAppium == nil || *actualAppium != appiumEndpoint || lastSeen == nil {
		t.Fatalf("lifecycle=%s health=%s serial=%s adb=%v appium=%v last_seen=%v",
			lifecycle, health, serial, actualADB, actualAppium, lastSeen)
	}
}

func TestHeartbeatPreservesProvisioningStateDuringInFlightRebuild(t *testing.T) {
	db := openTestDatabase(t)
	seedHost(t, db)
	seedDevice(t, db, "device_0000000000001", "host_000000000000001", "container-1", "old-serial",
		nil, nil, "provisioning", "unknown")
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_host_commands
		(id,host_id,command_type,payload,status,max_attempts,idempotency_key)
		VALUES('command_000000000001','host_000000000000001','rebuild',
		'{"device_id":"device_0000000000001","provider_ref":"container-1"}','pending',3,'heartbeat-rebuild-in-flight')`); err != nil {
		t.Fatal(err)
	}
	_, err := hostcommand.New(db).Heartbeat(context.Background(), "host_000000000000001", hostcommand.HeartbeatInput{
		AgentTime: time.Now().UTC(), Capacity: map[string]any{"device_slots": 1},
		Devices: []hostcommand.DiscoveredDevice{{ProviderRef: "container-1", Serial: "new-serial",
			LifecycleStatus: "ready", HealthStatus: "healthy", Connection: map[string]any{
				"adb_endpoint": "10.0.0.8:31000", "appium_endpoint": "http://10.0.0.8:4723", "appium_udid": "emulator-5554"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var lifecycle, health, serial string
	var lastSeen *time.Time
	if err := db.Pool().QueryRow(context.Background(), `SELECT lifecycle_status,health_status,serial,last_seen_at
		FROM devices WHERE id='device_0000000000001'`).Scan(&lifecycle, &health, &serial, &lastSeen); err != nil {
		t.Fatal(err)
	}
	if lifecycle != "provisioning" || health != "unknown" || serial != "new-serial" || lastSeen == nil {
		t.Fatalf("lifecycle=%s health=%s serial=%s last_seen=%v", lifecycle, health, serial, lastSeen)
	}
}

func TestHeartbeatMayReuseConnectionIdentityFromQuarantinedPoolExitRecord(t *testing.T) {
	db := openTestDatabase(t)
	seedHost(t, db)
	seedDevice(t, db, "device_0000000000001", "host_000000000000001", "container-active", "old-active-serial",
		nil, nil, "booting", "unknown")
	staleADB, staleAppium := "10.0.0.8:31000", "http://10.0.0.8:4723"
	seedDevice(t, db, "device_0000000000002", "host_000000000000001", "container-retired", "reused-serial",
		&staleADB, &staleAppium, "quarantined", "unhealthy")
	_, err := hostcommand.New(db).Heartbeat(context.Background(), "host_000000000000001", hostcommand.HeartbeatInput{
		AgentTime: time.Now().UTC(), Capacity: map[string]any{"device_slots": 1},
		Devices: []hostcommand.DiscoveredDevice{{ProviderRef: "container-active", Serial: "reused-serial",
			LifecycleStatus: "ready", HealthStatus: "healthy", Connection: map[string]any{
				"adb_endpoint": staleADB, "appium_endpoint": staleAppium, "appium_udid": "emulator-5554"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var lifecycle, health, serial string
	if err := db.Pool().QueryRow(context.Background(), `SELECT lifecycle_status,health_status,serial
		FROM devices WHERE id='device_0000000000001'`).Scan(&lifecycle, &health, &serial); err != nil {
		t.Fatal(err)
	}
	if lifecycle != "ready" || health != "healthy" || serial != "reused-serial" {
		t.Fatalf("lifecycle=%s health=%s serial=%s", lifecycle, health, serial)
	}
	var count int
	if err := db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM devices WHERE serial='reused-serial'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("devices sharing retired connection identity=%d", count)
	}
}

func TestHeartbeatDoesNotCreateOrCrossBindDiscoveredDevice(t *testing.T) {
	db := openTestDatabase(t)
	seedHost(t, db)
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_hosts
        (id,name,host_type,status) VALUES ('host_000000000000002','other-command-host','docker_emulator','offline')`); err != nil {
		t.Fatal(err)
	}
	seedDevice(t, db, "device_0000000000002", "host_000000000000002", "container-other", "other-serial", nil, nil, "provisioning", "unknown")
	service := hostcommand.New(db)
	_, err := service.Heartbeat(context.Background(), "host_000000000000001", hostcommand.HeartbeatInput{
		AgentTime: time.Now().UTC(), Capacity: map[string]any{"device_slots": 2},
		Devices: []hostcommand.DiscoveredDevice{
			{ProviderRef: "container-unregistered", Serial: "unregistered-serial", LifecycleStatus: "booting", HealthStatus: "unknown"},
			{ProviderRef: "container-other", Serial: "forged-cross-host-serial", LifecycleStatus: "ready", HealthStatus: "healthy"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM devices").Scan(&count); err != nil {
		t.Fatal(err)
	}
	var serial string
	if err := db.Pool().QueryRow(context.Background(), "SELECT serial FROM devices WHERE id='device_0000000000002'").Scan(&serial); err != nil {
		t.Fatal(err)
	}
	if count != 1 || serial != "other-serial" {
		t.Fatalf("device count=%d cross-host serial=%s", count, serial)
	}
}

func TestHeartbeatPreservesReservationAndTerminalLifecycleTruth(t *testing.T) {
	for _, lifecycle := range []string{"reserved", "busy", "recycling", "quarantined", "deleted"} {
		t.Run(lifecycle, func(t *testing.T) {
			db := openTestDatabase(t)
			seedHost(t, db)
			seedDevice(t, db, "device_0000000000001", "host_000000000000001", "container-1", "old-serial", nil, nil, lifecycle, "unknown")
			if lifecycle == "quarantined" || lifecycle == "deleted" {
				if _, err := db.Pool().Exec(context.Background(), `UPDATE devices SET health_reason='manual state must be preserved'
                    WHERE id='device_0000000000001'`); err != nil {
					t.Fatal(err)
				}
			}
			incomingLifecycle, incomingHealth := "stopped", "unknown"
			if lifecycle == "quarantined" || lifecycle == "deleted" {
				incomingLifecycle, incomingHealth = "ready", "healthy"
			}
			_, err := hostcommand.New(db).Heartbeat(context.Background(), "host_000000000000001", hostcommand.HeartbeatInput{
				AgentTime: time.Now().UTC(), Capacity: map[string]any{"device_slots": 1},
				Devices: []hostcommand.DiscoveredDevice{{ProviderRef: "container-1", Serial: "new-serial", LifecycleStatus: incomingLifecycle,
					HealthStatus: incomingHealth, Connection: map[string]any{"adb_endpoint": "10.0.0.8:32000"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var actualLifecycle, actualHealth, serial string
			var adbEndpoint, healthReason *string
			var lastSeen *time.Time
			if err := db.Pool().QueryRow(context.Background(), `SELECT lifecycle_status,health_status,serial,adb_endpoint,health_reason,last_seen_at
                    FROM devices WHERE id='device_0000000000001'`).Scan(&actualLifecycle, &actualHealth, &serial, &adbEndpoint, &healthReason, &lastSeen); err != nil {
				t.Fatal(err)
			}
			if actualLifecycle != lifecycle || actualHealth != "unknown" || serial != "new-serial" || adbEndpoint == nil || lastSeen == nil {
				t.Fatalf("lifecycle=%s health=%s serial=%s adb=%v last_seen=%v", actualLifecycle, actualHealth, serial, adbEndpoint, lastSeen)
			}
			if (lifecycle == "quarantined" || lifecycle == "deleted") && (healthReason == nil || *healthReason != "manual state must be preserved") {
				t.Fatalf("health reason=%v", healthReason)
			}
		})
	}
}

func TestHeartbeatIdentityConflictsRollbackEntireTransaction(t *testing.T) {
	for _, field := range []string{"serial", "adb_endpoint", "appium_endpoint"} {
		t.Run(field, func(t *testing.T) {
			db := openTestDatabase(t)
			seedHost(t, db)
			firstADB, firstAppium := "10.0.0.8:31000", "http://10.0.0.8:4723"
			secondADB, secondAppium := "10.0.0.8:31001", "http://10.0.0.8:4724"
			seedDevice(t, db, "device_0000000000001", "host_000000000000001", "container-1", "serial-1", &firstADB, &firstAppium, "booting", "unknown")
			seedDevice(t, db, "device_0000000000002", "host_000000000000001", "container-2", "serial-2", &secondADB, &secondAppium, "ready", "healthy")
			discovered := hostcommand.DiscoveredDevice{ProviderRef: "container-1", Serial: "new-serial", LifecycleStatus: "ready", HealthStatus: "healthy",
				Connection: map[string]any{"adb_endpoint": "10.0.0.8:32000", "appium_endpoint": "http://10.0.0.8:4823"}}
			switch field {
			case "serial":
				discovered.Serial = "serial-2"
			case "adb_endpoint":
				discovered.Connection["adb_endpoint"] = secondADB
			case "appium_endpoint":
				discovered.Connection["appium_endpoint"] = secondAppium
			}
			_, err := hostcommand.New(db).Heartbeat(context.Background(), "host_000000000000001", hostcommand.HeartbeatInput{
				AgentTime: time.Now().UTC(), Capacity: map[string]any{"device_slots": 2}, Devices: []hostcommand.DiscoveredDevice{discovered},
			})
			if !errors.Is(err, hostcommand.ErrDeviceIdentityConflict) {
				t.Fatalf("heartbeat error=%v", err)
			}
			var hostStatus, lifecycle, health, serial string
			if err := db.Pool().QueryRow(context.Background(), "SELECT status FROM device_hosts WHERE id='host_000000000000001'").Scan(&hostStatus); err != nil {
				t.Fatal(err)
			}
			if err := db.Pool().QueryRow(context.Background(), `SELECT lifecycle_status,health_status,serial FROM devices
                    WHERE id='device_0000000000001'`).Scan(&lifecycle, &health, &serial); err != nil {
				t.Fatal(err)
			}
			if hostStatus != "offline" || lifecycle != "booting" || health != "unknown" || serial != "serial-1" {
				t.Fatalf("host=%s lifecycle=%s health=%s serial=%s", hostStatus, lifecycle, health, serial)
			}
		})
	}
}

func TestConcurrentClaimsLeaseOneCommandOnce(t *testing.T) {
	db := openTestDatabase(t)
	seedHost(t, db)
	service := hostcommand.New(db)
	if _, err := service.Create(context.Background(), "host_000000000000001", "inspect", map[string]any{"device_id": "device_0000000000001"}, 3, "claim-once-key"); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan []hostcommand.Command, 2)
	errorsChannel := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() {
			<-start
			values, err := service.Claim(context.Background(), "host_000000000000001", hostcommand.ClaimInput{LeaseSeconds: 30, MaxCommands: 1})
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- values
		}()
	}
	close(start)
	claimed := 0
	for index := 0; index < 2; index++ {
		select {
		case err := <-errorsChannel:
			t.Fatal(err)
		case values := <-results:
			claimed += len(values)
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed commands=%d", claimed)
	}
}

func TestExpiredLeaseReclaimsAndRejectsOldCompletion(t *testing.T) {
	db := openTestDatabase(t)
	seedHost(t, db)
	service := hostcommand.New(db)
	created, err := service.Create(context.Background(), "host_000000000000001", "restart", map[string]any{"device_id": "device_0000000000001"}, 2, "lease-retry-key")
	if err != nil {
		t.Fatal(err)
	}
	first := claimOne(t, service)
	if _, err := db.Pool().Exec(context.Background(), "UPDATE device_host_commands SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", created.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.RecoverExpiredOnce(context.Background())
	if err != nil || recovered.Status != "pending" {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	second := claimOne(t, service)
	if second.Attempt != 2 || first.LeaseToken == nil || second.LeaseToken == nil || *first.LeaseToken == *second.LeaseToken {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if _, err := service.Complete(context.Background(), created.ID, hostcommand.CompletionInput{
		LeaseToken: *first.LeaseToken, Attempt: first.Attempt, Status: "succeeded",
	}); !errors.Is(err, hostcommand.ErrConflict) {
		t.Fatalf("old completion error=%v", err)
	}
	completed, err := service.Complete(context.Background(), created.ID, hostcommand.CompletionInput{
		LeaseToken: *second.LeaseToken, Attempt: second.Attempt, Status: "succeeded", Result: map[string]any{"ok": true},
	})
	if err != nil || completed.Status != "succeeded" || completed.CompletedAt == nil {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
}

func TestExpiredFinalAttemptTimesOut(t *testing.T) {
	db := openTestDatabase(t)
	seedHost(t, db)
	service := hostcommand.New(db)
	created, err := service.Create(context.Background(), "host_000000000000001", "delete", map[string]any{"provider_ref": "container-1"}, 1, "final-attempt-key")
	if err != nil {
		t.Fatal(err)
	}
	claimOne(t, service)
	if _, err := db.Pool().Exec(context.Background(), "UPDATE device_host_commands SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", created.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.RecoverExpiredOnce(context.Background())
	if err != nil || recovered.Status != "timed_out" || recovered.ErrorCode == nil || recovered.CompletedAt == nil {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	values, err := service.Claim(context.Background(), "host_000000000000001", hostcommand.ClaimInput{LeaseSeconds: 30, MaxCommands: 1})
	if err != nil || len(values) != 0 {
		t.Fatalf("claim after timeout values=%v err=%v", values, err)
	}
}

func TestRetryableCompletionReturnsCommandToPendingUntilSuccess(t *testing.T) {
	db := openTestDatabase(t)
	seedHost(t, db)
	service := hostcommand.New(db)
	created, err := service.Create(context.Background(), "host_000000000000001", "delete",
		map[string]any{"provider_ref": "container-1"}, 2, "reported-retry-key")
	if err != nil {
		t.Fatal(err)
	}
	first := claimOne(t, service)
	retried, err := service.Complete(context.Background(), created.ID, hostcommand.CompletionInput{
		LeaseToken: *first.LeaseToken, Attempt: first.Attempt, Status: "failed",
		Error: &hostcommand.CompletionError{Code: "EMULATOR_DELETE_FAILED", Message: "temporary cleanup failure", Retryable: true},
	})
	if err != nil || retried.Status != "pending" || retried.CompletedAt != nil || retried.ErrorCode == nil {
		t.Fatalf("retried=%+v err=%v", retried, err)
	}
	second := claimOne(t, service)
	if second.Attempt != 2 {
		t.Fatalf("second attempt=%d", second.Attempt)
	}
	completed, err := service.Complete(context.Background(), created.ID, hostcommand.CompletionInput{
		LeaseToken: *second.LeaseToken, Attempt: second.Attempt, Status: "succeeded", Result: map[string]any{"deleted": true},
	})
	if err != nil || completed.Status != "succeeded" || completed.CompletedAt == nil || completed.ErrorCode != nil {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
}

func claimOne(t *testing.T, service *hostcommand.Service) hostcommand.Command {
	t.Helper()
	values, err := service.Claim(context.Background(), "host_000000000000001", hostcommand.ClaimInput{LeaseSeconds: 30, MaxCommands: 1})
	if err != nil || len(values) != 1 {
		t.Fatalf("claim values=%v err=%v", values, err)
	}
	return values[0]
}

func openTestDatabase(t *testing.T) *database.DB {
	t.Helper()
	url := os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
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

func seedHost(t *testing.T, db *database.DB) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE
        device_idempotency_records,device_audit_events,device_health_events,device_sessions,
        device_reservations,device_pool_devices,devices,device_pool_images,device_pools,
        device_host_commands,device_hosts,device_images RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_hosts
        (id,name,host_type,status) VALUES ('host_000000000000001','command-host','docker_emulator','offline')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_images
		(id,name,docker_image,docker_digest,api_level,abi,resolution,status)
		VALUES ('image_00000000000001','command-image','registry.example/alcor/android-emulator:api34','sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',34,'x86_64','1080x1920','ready')`); err != nil {
		t.Fatal(err)
	}
}

func seedDevice(t *testing.T, db *database.DB, id, hostID, providerRef, serial string, adbEndpoint, appiumEndpoint *string, lifecycle, health string) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO devices
        (id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,adb_endpoint,appium_endpoint,lifecycle_status,health_status)
        VALUES($1,$2,'image_00000000000001','emulator','docker_emulator',$3,'rebuild',$4,$5,$6,$7,$8)`,
		id, hostID, providerRef, serial, adbEndpoint, appiumEndpoint, lifecycle, health); err != nil {
		t.Fatal(err)
	}
}

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
	service := hostcommand.New(db)
	result, err := service.Heartbeat(context.Background(), "host_000000000000001", hostcommand.HeartbeatInput{
		AgentTime: time.Now().UTC(), Capacity: map[string]any{"cpu": 8, "device_slots": 2},
		Devices: []hostcommand.DiscoveredDevice{{ProviderRef: "container-1", Serial: "emulator-5554", LifecycleStatus: "ready", HealthStatus: "healthy"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "online" || result.Devices != 1 || result.ReceivedAt.IsZero() {
		t.Fatalf("heartbeat result=%+v", result)
	}
	var status string
	var heartbeat *time.Time
	if err := db.Pool().QueryRow(context.Background(), "SELECT status,last_heartbeat_at FROM device_hosts WHERE id=$1", result.HostID).Scan(&status, &heartbeat); err != nil {
		t.Fatal(err)
	}
	if status != "online" || heartbeat == nil {
		t.Fatalf("host status=%s heartbeat=%v", status, heartbeat)
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
}

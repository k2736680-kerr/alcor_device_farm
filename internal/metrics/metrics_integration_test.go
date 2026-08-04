package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
)

func TestDatabaseMetricsExposeDeviceSchedulerAgentAndReservationState(t *testing.T) {
	databaseURL := os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEVICE_FARM_TEST_DATABASE_URL is not set")
	}
	db, err := database.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE
		device_idempotency_records,device_audit_events,device_health_events,device_sessions,device_reservations,
		device_pool_devices,devices,device_pool_images,device_pools,device_host_commands,device_hosts,device_images
		RESTART IDENTITY CASCADE;
		INSERT INTO device_images (id,name,docker_image,docker_digest,api_level,abi,resolution,status)
		VALUES ('image_metrics_0000001','metrics-image','registry.example/alcor/android-emulator:api34','sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',34,'x86_64','1080x2400','ready');
		INSERT INTO device_hosts (id,name,host_type,status,last_heartbeat_at)
		VALUES ('host_metrics_00000001','metrics-host','docker_emulator','online',clock_timestamp());
		INSERT INTO device_host_commands (id,host_id,command_type,status,idempotency_key)
		VALUES ('command_metrics_00001','host_metrics_00000001','inspect','pending','metrics-command-key');
		INSERT INTO device_pools (id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
		VALUES ('pool_metrics_00000001','metrics-pool',600,3600,2,'active');
		INSERT INTO devices (id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,lifecycle_status,health_status)
		VALUES ('device_metrics_000001','host_metrics_00000001','image_metrics_0000001','emulator','docker_emulator',
		'metrics-container','rebuild','metrics-serial','ready','healthy');
		INSERT INTO device_reservations (id,client_id,pool_id,owner_type,owner_id,lease_seconds,status,idempotency_key)
		VALUES ('reservation_metrics_01','metrics-client','pool_metrics_00000001','test_run','attempt_metrics_00001',600,'pending','metrics-reservation-key');
		INSERT INTO device_health_events (id,device_id,source,event_type,severity,reason,observed_at)
		VALUES ('event_metrics_0000001','device_metrics_000001','agent','agent_reported','error','metrics fixture',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	New(db).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status=%d body=%s", response.Code, response.Body.String())
	}
	for _, want := range []string{
		"device_farm_database_ready 1",
		`device_farm_devices{health_status="healthy",lifecycle_status="ready"} 1`,
		`device_farm_reservations{status="pending"} 1`,
		`device_farm_agents{status="online"} 1`,
		`device_farm_host_commands{status="pending"} 1`,
		`device_farm_health_events_total{severity="error"} 1`,
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("metrics do not contain %q:\n%s", want, response.Body.String())
		}
	}
}

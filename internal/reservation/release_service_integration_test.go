package reservation

import (
	"context"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/jackc/pgx/v5"
)

func TestReleaseUnhealthyBusyDeviceQuarantinesAndClosesReservation(t *testing.T) {
	db := openSelectorDatabase(t)
	ctx := context.Background()
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO device_hosts(id,name,address,host_type,status,draining)
		VALUES('host_release_unhealthy_01','release-host','10.0.0.41','docker_emulator','online',false);
		INSERT INTO device_images(id,name,docker_image,docker_digest,api_level,abi,resolution,status)
		VALUES('image_release_unhealthy_01','release-image','registry.example/android:34',
		'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',34,'x86_64','1080x1920','ready');
		INSERT INTO device_pools(id,name,platform,status,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,default_image_id,fresh_vm_per_run)
		VALUES('pool_release_unhealthy_01','android-10.0.0.41','android','active',600,3600,2,0,0,'image_release_unhealthy_01',false);
		INSERT INTO device_pool_images(pool_id,image_id,min_ready,max_instances,enabled)
		VALUES('pool_release_unhealthy_01','image_release_unhealthy_01',0,2,true);
		INSERT INTO devices(id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,platform,serial,lifecycle_status,health_status,capabilities)
		VALUES('device_release_unhealthy_01','host_release_unhealthy_01','image_release_unhealthy_01','emulator','docker_emulator','release-emulator-01','clean','android','release-serial-01','busy','unhealthy','{"platformName":"Android","apiLevel":34}');
		INSERT INTO device_pool_devices(pool_id,device_id,enabled)
		VALUES('pool_release_unhealthy_01','device_release_unhealthy_01',true);
		INSERT INTO device_reservations(id,client_id,pool_id,device_id,owner_type,owner_id,requested_capabilities,lease_seconds,status,idempotency_key,starts_at,expires_at)
		VALUES('reservation_release_unhealthy_01','release-client','pool_release_unhealthy_01','device_release_unhealthy_01','manual','release-owner-01','{"platformName":"Android","apiLevel":34}',600,'active','release-unhealthy-key',clock_timestamp(),clock_timestamp()+interval '10 minutes');
		INSERT INTO device_sessions(id,reservation_id,device_id,status,started_at,connection_metadata)
		VALUES('session_release_unhealthy_01','reservation_release_unhealthy_01','device_release_unhealthy_01','active',clock_timestamp(),'{}');
	`); err != nil {
		t.Fatal(err)
	}

	service := NewService(db, nil)
	view, err := service.Release(ctx, audit.Actor{Type: audit.ActorService, ID: "release-client", ClientID: "release-client"},
		"release-unhealthy-idempotency", "reservation_release_unhealthy_01", "release-unhealthy-request",
		ReleaseInput{Reason: "run completed"})
	if err != nil {
		t.Fatalf("unhealthy release must complete: %v", err)
	}
	if view.Status != "released" {
		t.Fatalf("reservation status=%q, want released", view.Status)
	}
	var lifecycle, health, reservationStatus, sessionStatus string
	if err := db.Pool().QueryRow(ctx, `SELECT lifecycle_status,health_status FROM devices WHERE id='device_release_unhealthy_01'`).Scan(&lifecycle, &health); err != nil {
		t.Fatal(err)
	}
	if lifecycle != "quarantined" || health != "unhealthy" {
		t.Fatalf("device state=%s/%s, want quarantined/unhealthy", lifecycle, health)
	}
	if err := db.Pool().QueryRow(ctx, `SELECT r.status,s.status FROM device_reservations r JOIN device_sessions s ON s.reservation_id=r.id WHERE r.id='reservation_release_unhealthy_01'`).Scan(&reservationStatus, &sessionStatus); err != nil {
		t.Fatal(err)
	}
	if reservationStatus != "released" || sessionStatus != "closed" {
		t.Fatalf("reservation/session=%s/%s, want released/closed", reservationStatus, sessionStatus)
	}
}

func TestReleaseContinuesWhenPoolIsDisabled(t *testing.T) {
	db := openSelectorDatabase(t)
	ctx := context.Background()
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO device_hosts(id,name,address,host_type,status,draining)
		VALUES('host_release_disabled_01','release-host-disabled','10.0.0.42','docker_emulator','online',false);
		INSERT INTO device_images(id,name,docker_image,docker_digest,api_level,abi,resolution,status)
		VALUES('image_release_disabled_01','release-image-disabled','registry.example/android:34',
		'sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789',34,'x86_64','1080x1920','ready');
		INSERT INTO device_pools(id,name,platform,status,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,default_image_id,fresh_vm_per_run)
		VALUES('pool_release_disabled_01','android-10.0.0.42','android','disabled',600,3600,2,0,0,'image_release_disabled_01',false);
		INSERT INTO device_pool_images(pool_id,image_id,min_ready,max_instances,enabled)
		VALUES('pool_release_disabled_01','image_release_disabled_01',0,2,true);
		INSERT INTO devices(id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,platform,serial,lifecycle_status,health_status,capabilities)
		VALUES('device_release_disabled_01','host_release_disabled_01','image_release_disabled_01','emulator','docker_emulator','release-emulator-02','clean','android','release-serial-02','busy','healthy','{"platformName":"Android","apiLevel":34}');
		INSERT INTO device_pool_devices(pool_id,device_id,enabled)
		VALUES('pool_release_disabled_01','device_release_disabled_01',true);
		INSERT INTO device_reservations(id,client_id,pool_id,device_id,owner_type,owner_id,requested_capabilities,lease_seconds,status,idempotency_key,starts_at,expires_at)
		VALUES('reservation_release_disabled_01','release-client','pool_release_disabled_01','device_release_disabled_01','manual','release-owner-02','{"platformName":"Android","apiLevel":34}',600,'active','release-disabled-key',clock_timestamp(),clock_timestamp()+interval '10 minutes');
		INSERT INTO device_sessions(id,reservation_id,device_id,status,started_at,connection_metadata)
		VALUES('session_release_disabled_01','reservation_release_disabled_01','device_release_disabled_01','active',clock_timestamp(),'{}');
	`); err != nil {
		t.Fatal(err)
	}

	service := NewService(db, nil)
	view, err := service.Release(ctx, audit.Actor{Type: audit.ActorService, ID: "release-client", ClientID: "release-client"},
		"release-disabled-idempotency", "reservation_release_disabled_01", "release-disabled-request",
		ReleaseInput{Reason: "pool disabled cleanup"})
	if err != nil {
		t.Fatalf("release must not depend on active pool: %v", err)
	}
	if view.Status != "released" {
		t.Fatalf("reservation status=%q, want released", view.Status)
	}
	var lifecycle, status string
	if err := db.Pool().QueryRow(ctx, `SELECT d.lifecycle_status,r.status FROM devices d JOIN device_reservations r ON r.device_id=d.id WHERE d.id='device_release_disabled_01'`).Scan(&lifecycle, &status); err != nil {
		t.Fatal(err)
	}
	if lifecycle != "ready" || status != "released" {
		t.Fatalf("device/reservation=%s/%s, want ready/released", lifecycle, status)
	}
}

func TestPoolPolicyCountsActiveReservationsByDeviceHost(t *testing.T) {
	db := openSelectorDatabase(t)
	ctx := context.Background()
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO device_hosts(id,name,address,host_type,status,draining)
		VALUES('host_policy_count_0001','policy-host-a','10.0.0.51','docker_emulator','online',false),
		      ('host_policy_count_0002','policy-host-b','10.0.0.52','docker_emulator','online',false);
		INSERT INTO device_images(id,name,docker_image,docker_digest,api_level,abi,resolution,status)
		VALUES('image_policy_count_0001','policy-image','registry.example/android:34',
		'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',34,'x86_64','1080x1920','ready');
		INSERT INTO device_pools(id,name,platform,status,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,default_image_id,fresh_vm_per_run)
		VALUES('pool_policy_count_0001','android-10.0.0.51','android','active',600,3600,4,0,0,'image_policy_count_0001',false),
		      ('pool_policy_count_0002','android-10.0.0.52','android','active',600,3600,4,0,0,'image_policy_count_0001',false);
		INSERT INTO device_pool_images(pool_id,image_id,min_ready,max_instances,enabled)
		VALUES('pool_policy_count_0001','image_policy_count_0001',0,4,true),
		      ('pool_policy_count_0002','image_policy_count_0001',0,4,true);
		INSERT INTO devices(id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,platform,serial,lifecycle_status,health_status,capabilities)
		VALUES('device_policy_count_0001','host_policy_count_0001','image_policy_count_0001','emulator','docker_emulator','policy-emulator-01','clean','android','policy-serial-01','busy','healthy','{"platformName":"Android","apiLevel":34}'),
		      ('device_policy_count_0002','host_policy_count_0001','image_policy_count_0001','emulator','docker_emulator','policy-emulator-02','clean','android','policy-serial-02','busy','healthy','{"platformName":"Android","apiLevel":34}');
		INSERT INTO device_pool_devices(pool_id,device_id,enabled)
		VALUES('pool_policy_count_0001','device_policy_count_0001',true),
		      ('pool_policy_count_0001','device_policy_count_0002',true);
		INSERT INTO device_reservations(id,client_id,pool_id,device_id,owner_type,owner_id,requested_capabilities,lease_seconds,status,idempotency_key,starts_at,expires_at)
		VALUES('reservation_policy_count_0001','policy-client','pool_policy_count_0001','device_policy_count_0001','manual','policy-owner-01','{"platformName":"Android","apiLevel":34}',600,'active','policy-count-key-01',clock_timestamp(),clock_timestamp()+interval '10 minutes'),
		      ('reservation_policy_count_0002','policy-client','pool_policy_count_0002','device_policy_count_0002','manual','policy-owner-02','{"platformName":"Android","apiLevel":34}',600,'active','policy-count-key-02',clock_timestamp(),clock_timestamp()+interval '10 minutes');
	`); err != nil {
		t.Fatal(err)
	}

	var policy repository.PoolPolicy
	if err := db.WithinTx(ctx, func(tx pgx.Tx) error {
		var err error
		policy, err = (repository.ReservationRepository{}).LockPoolPolicyForHost(ctx, tx, "host_policy_count_0001", "device_policy_count_0001")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if policy.ActiveReservations != 2 {
		t.Fatalf("active reservations on host=%d, want 2", policy.ActiveReservations)
	}
}

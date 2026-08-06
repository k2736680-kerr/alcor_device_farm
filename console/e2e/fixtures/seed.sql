-- DF-027/DF-028 E0 fixture: one ready device in a single-device pool.
-- The local runner rejects non-loopback and non-test database targets before
-- executing this destructive reset.
TRUNCATE TABLE
  device_console_sessions,
  device_audit_events,
  device_health_events,
  device_sessions,
  device_reservations,
  device_idempotency_records,
  device_pool_devices,
  devices,
  device_pool_images,
  device_pools,
  device_host_commands,
  device_hosts,
  device_images
RESTART IDENTITY CASCADE;

INSERT INTO device_images (id,name,docker_image,docker_digest,api_level,abi,resolution,resource_config,status)
VALUES ('image_e2e0000000000001','e2e-image','registry.example/alcor/android-emulator:api34',
        'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',34,'x86_64','1080x2400','{}','ready');

INSERT INTO device_hosts (id,name,host_type,address,capabilities,capacity,used_capacity,status,last_heartbeat_at)
VALUES ('host_e2e00000000000001','e2e-host','docker_emulator','10.0.0.10','{}','{"device_slots":2}','{}','online',NOW());

INSERT INTO device_pools (id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
VALUES ('pool_e2e00000000000001','e2e-pool',1800,7200,1,'active');

INSERT INTO device_pool_images (pool_id,image_id,min_ready,max_instances,enabled)
VALUES ('pool_e2e00000000000001','image_e2e0000000000001',0,1,true);

INSERT INTO devices (id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,stf_serial,capabilities,lifecycle_status,health_status,consecutive_failures)
VALUES ('device_e2e000000000001','host_e2e00000000000001','image_e2e0000000000001','emulator','docker_emulator','emulator-5554','rebuild','emulator-5554','emulator-5554','{}','ready','healthy',0);

INSERT INTO device_pool_devices (pool_id,device_id,enabled)
VALUES ('pool_e2e00000000000001','device_e2e000000000001',true);

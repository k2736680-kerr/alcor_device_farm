INSERT INTO device_images (id, name, docker_digest, api_level, abi, resolution, status)
VALUES (
    'img_0000000000000001',
    'android-14-test',
    'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
    34,
    'x86_64',
    '1080x2400',
    'ready'
);

INSERT INTO device_hosts (id, name, host_type, status)
VALUES ('host_000000000000001', 'df004-host', 'docker_emulator', 'online');

INSERT INTO device_pools (id, name, default_lease_seconds, max_lease_seconds, max_concurrency)
VALUES ('pool_000000000000001', 'df004-pool', 1800, 7200, 2);

INSERT INTO devices (
    id, host_id, image_id, device_kind, provider_type, provider_ref,
    lifecycle_mode, serial, lifecycle_status, health_status
)
VALUES (
    'dev_0000000000000001', 'host_000000000000001', 'img_0000000000000001',
    'emulator', 'docker_emulator', 'container-df004-1', 'rebuild',
    'emulator-5554', 'ready', 'healthy'
);

DO $test$
DECLARE
    rejected boolean := false;
BEGIN
    BEGIN
        INSERT INTO devices (
            id, host_id, image_id, device_kind, provider_type, provider_ref,
            lifecycle_mode, serial, lifecycle_status, health_status
        )
        VALUES (
            'dev_0000000000000002', 'host_000000000000001', 'img_0000000000000001',
            'emulator', 'docker_emulator', 'container-df004-2', 'rebuild',
            'emulator-5554', 'ready', 'healthy'
        );
    EXCEPTION WHEN unique_violation THEN
        rejected := true;
    END;
    IF NOT rejected THEN
        RAISE EXCEPTION 'duplicate device serial was accepted';
    END IF;
END
$test$;

INSERT INTO device_reservations (
    id, client_id, pool_id, owner_type, owner_id, lease_seconds, status, idempotency_key
)
VALUES (
    'res_0000000000000001', 'df004-client', 'pool_000000000000001',
    'test_run', 'owner_00000000000001', 1800, 'pending', 'df004-idempotency-1'
);

DO $test$
DECLARE
    rejected boolean := false;
BEGIN
    BEGIN
        INSERT INTO device_reservations (
            id, client_id, pool_id, owner_type, owner_id, lease_seconds, status, idempotency_key
        )
        VALUES (
            'res_0000000000000002', 'df004-client', 'pool_000000000000001',
            'test_run', 'owner_00000000000002', 1800, 'pending', 'df004-idempotency-1'
        );
    EXCEPTION WHEN unique_violation THEN
        rejected := true;
    END;
    IF NOT rejected THEN
        RAISE EXCEPTION 'duplicate client idempotency key was accepted';
    END IF;
END
$test$;

INSERT INTO device_reservations (
    id, client_id, pool_id, device_id, owner_type, owner_id, lease_seconds,
    status, idempotency_key, starts_at, expires_at
)
VALUES (
    'res_0000000000000003', 'df004-client-a', 'pool_000000000000001',
    'dev_0000000000000001', 'test_run', 'owner_00000000000003', 1800,
    'active', 'df004-idempotency-3', clock_timestamp(), clock_timestamp() + interval '30 minutes'
);

DO $test$
DECLARE
    rejected boolean := false;
BEGIN
    BEGIN
        INSERT INTO device_reservations (
            id, client_id, pool_id, device_id, owner_type, owner_id, lease_seconds,
            status, idempotency_key, starts_at, expires_at
        )
        VALUES (
            'res_0000000000000004', 'df004-client-b', 'pool_000000000000001',
            'dev_0000000000000001', 'test_run', 'owner_00000000000004', 1800,
            'active', 'df004-idempotency-4', clock_timestamp(), clock_timestamp() + interval '30 minutes'
        );
    EXCEPTION WHEN unique_violation THEN
        rejected := true;
    END;
    IF NOT rejected THEN
        RAISE EXCEPTION 'second active reservation for one device was accepted';
    END IF;
END
$test$;

DO $test$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_tables
        WHERE schemaname = 'public'
          AND tablename IN ('eval_tasks', 'eval_results', 'runs', 'run_attempts', 'run_results', 'artifacts')
    ) THEN
        RAISE EXCEPTION 'business-domain table exists in device database';
    END IF;
END
$test$;

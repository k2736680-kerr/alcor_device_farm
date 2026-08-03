CREATE TABLE device_images (
    id                  varchar(64) PRIMARY KEY,
    name                varchar(128) NOT NULL UNIQUE,
    docker_digest       varchar(80) NOT NULL UNIQUE,
    api_level           integer NOT NULL,
    abi                 varchar(32) NOT NULL,
    resolution          varchar(32) NOT NULL,
    resource_config     jsonb NOT NULL DEFAULT '{}'::jsonb,
    status              varchar(24) NOT NULL DEFAULT 'draft',
    validation_error    text,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ck_device_images_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_device_images_digest CHECK (docker_digest ~ '^sha256:[a-fA-F0-9]{64}$'),
    CONSTRAINT ck_device_images_api_level CHECK (api_level >= 21),
    CONSTRAINT ck_device_images_abi CHECK (abi IN ('x86_64', 'arm64-v8a')),
    CONSTRAINT ck_device_images_resolution CHECK (resolution ~ '^[0-9]+x[0-9]+$'),
    CONSTRAINT ck_device_images_status CHECK (status IN ('draft', 'validating', 'ready', 'failed', 'disabled')),
    CONSTRAINT ck_device_images_time CHECK (updated_at >= created_at)
);

CREATE TABLE device_hosts (
    id                  varchar(64) PRIMARY KEY,
    name                varchar(128) NOT NULL UNIQUE,
    host_type           varchar(32) NOT NULL,
    address             varchar(255),
    capabilities        jsonb NOT NULL DEFAULT '{}'::jsonb,
    capacity            jsonb NOT NULL DEFAULT '{}'::jsonb,
    used_capacity       jsonb NOT NULL DEFAULT '{}'::jsonb,
    status              varchar(24) NOT NULL DEFAULT 'offline',
    draining            boolean NOT NULL DEFAULT false,
    last_heartbeat_at   timestamptz,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ck_device_hosts_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_device_hosts_type CHECK (host_type IN ('docker_emulator', 'usb_android', 'hybrid')),
    CONSTRAINT ck_device_hosts_status CHECK (status IN ('online', 'offline', 'draining', 'maintenance')),
    CONSTRAINT ck_device_hosts_draining CHECK (draining = (status = 'draining')),
    CONSTRAINT ck_device_hosts_time CHECK (updated_at >= created_at)
);

CREATE TABLE device_host_commands (
    id                  varchar(64) PRIMARY KEY,
    host_id             varchar(64) NOT NULL REFERENCES device_hosts(id) ON DELETE RESTRICT,
    command_type        varchar(24) NOT NULL,
    payload             jsonb NOT NULL DEFAULT '{}'::jsonb,
    status              varchar(24) NOT NULL DEFAULT 'pending',
    lease_token         varchar(128),
    lease_expires_at    timestamptz,
    attempts            integer NOT NULL DEFAULT 0,
    max_attempts        integer NOT NULL DEFAULT 3,
    idempotency_key     varchar(128) NOT NULL,
    result              jsonb,
    error_code          varchar(64),
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at        timestamptz,
    CONSTRAINT uq_device_host_commands_idempotency UNIQUE (host_id, idempotency_key),
    CONSTRAINT ck_device_host_commands_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_device_host_commands_type CHECK (command_type IN ('create', 'start', 'stop', 'restart', 'rebuild', 'delete', 'inspect')),
    CONSTRAINT ck_device_host_commands_status CHECK (status IN ('pending', 'leased', 'succeeded', 'failed', 'timed_out', 'canceled')),
    CONSTRAINT ck_device_host_commands_attempts CHECK (attempts >= 0 AND max_attempts > 0 AND attempts <= max_attempts),
    CONSTRAINT ck_device_host_commands_lease CHECK (
        (status = 'leased' AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR status <> 'leased'
    ),
    CONSTRAINT ck_device_host_commands_completion CHECK (
        (status IN ('succeeded', 'failed', 'timed_out', 'canceled') AND completed_at IS NOT NULL)
        OR status IN ('pending', 'leased')
    ),
    CONSTRAINT ck_device_host_commands_time CHECK (updated_at >= created_at AND (completed_at IS NULL OR completed_at >= created_at))
);

CREATE INDEX ix_device_host_commands_claim
    ON device_host_commands (host_id, created_at, id)
    WHERE status = 'pending';
CREATE INDEX ix_device_host_commands_lease_expiry
    ON device_host_commands (lease_expires_at)
    WHERE status = 'leased';

CREATE TABLE device_pools (
    id                      varchar(64) PRIMARY KEY,
    name                    varchar(128) NOT NULL UNIQUE,
    default_lease_seconds   integer NOT NULL,
    max_lease_seconds       integer NOT NULL,
    max_concurrency         integer NOT NULL DEFAULT 1,
    status                  varchar(24) NOT NULL DEFAULT 'active',
    created_at              timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at              timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ck_device_pools_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_device_pools_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT ck_device_pools_lease CHECK (default_lease_seconds >= 60 AND max_lease_seconds >= default_lease_seconds),
    CONSTRAINT ck_device_pools_concurrency CHECK (max_concurrency > 0),
    CONSTRAINT ck_device_pools_time CHECK (updated_at >= created_at)
);

CREATE TABLE device_pool_images (
    pool_id             varchar(64) NOT NULL REFERENCES device_pools(id) ON DELETE CASCADE,
    image_id            varchar(64) NOT NULL REFERENCES device_images(id) ON DELETE RESTRICT,
    min_ready           integer NOT NULL DEFAULT 0,
    max_instances       integer NOT NULL,
    enabled             boolean NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (pool_id, image_id),
    CONSTRAINT ck_device_pool_images_capacity CHECK (min_ready >= 0 AND max_instances > 0 AND max_instances >= min_ready),
    CONSTRAINT ck_device_pool_images_time CHECK (updated_at >= created_at)
);

CREATE TABLE devices (
    id                  varchar(64) PRIMARY KEY,
    host_id             varchar(64) NOT NULL REFERENCES device_hosts(id) ON DELETE RESTRICT,
    image_id            varchar(64) REFERENCES device_images(id) ON DELETE RESTRICT,
    device_kind         varchar(16) NOT NULL,
    provider_type       varchar(32) NOT NULL,
    provider_ref        varchar(255) NOT NULL,
    lifecycle_mode      varchar(24) NOT NULL,
    serial              varchar(255) NOT NULL UNIQUE,
    stf_serial          varchar(255),
    adb_endpoint        varchar(255),
    appium_endpoint     varchar(255),
    capabilities        jsonb NOT NULL DEFAULT '{}'::jsonb,
    lifecycle_status    varchar(24) NOT NULL DEFAULT 'provisioning',
    health_status       varchar(24) NOT NULL DEFAULT 'unknown',
    health_reason       text,
    consecutive_failures integer NOT NULL DEFAULT 0,
    last_seen_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT uq_devices_provider_ref UNIQUE (provider_type, provider_ref),
    CONSTRAINT ck_devices_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_devices_kind CHECK (device_kind IN ('emulator', 'physical')),
    CONSTRAINT ck_devices_provider CHECK (provider_type IN ('mock', 'docker_emulator', 'usb_android')),
    CONSTRAINT ck_devices_lifecycle_mode CHECK (lifecycle_mode IN ('rebuild', 'clean', 'factory_reset')),
    CONSTRAINT ck_devices_lifecycle_status CHECK (lifecycle_status IN ('provisioning', 'booting', 'ready', 'reserved', 'busy', 'recycling', 'stopped', 'quarantined', 'deleted')),
    CONSTRAINT ck_devices_health_status CHECK (health_status IN ('unknown', 'healthy', 'degraded', 'unhealthy')),
    CONSTRAINT ck_devices_failures CHECK (consecutive_failures >= 0),
    CONSTRAINT ck_devices_kind_image CHECK ((device_kind = 'emulator' AND image_id IS NOT NULL) OR device_kind = 'physical'),
    CONSTRAINT ck_devices_time CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX uq_devices_stf_serial ON devices (stf_serial) WHERE stf_serial IS NOT NULL;
CREATE UNIQUE INDEX uq_devices_adb_endpoint ON devices (adb_endpoint) WHERE adb_endpoint IS NOT NULL;
CREATE UNIQUE INDEX uq_devices_appium_endpoint ON devices (appium_endpoint) WHERE appium_endpoint IS NOT NULL;
CREATE INDEX ix_devices_schedulable ON devices (lifecycle_status, health_status, host_id)
    WHERE lifecycle_status = 'ready' AND health_status = 'healthy';

CREATE TABLE device_pool_devices (
    pool_id             varchar(64) NOT NULL REFERENCES device_pools(id) ON DELETE CASCADE,
    device_id           varchar(64) NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
    enabled             boolean NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (pool_id, device_id),
    CONSTRAINT ck_device_pool_devices_time CHECK (updated_at >= created_at)
);

CREATE TABLE device_reservations (
    id                      varchar(64) PRIMARY KEY,
    client_id               varchar(128) NOT NULL,
    pool_id                 varchar(64) NOT NULL REFERENCES device_pools(id) ON DELETE RESTRICT,
    device_id               varchar(64) REFERENCES devices(id) ON DELETE RESTRICT,
    owner_type              varchar(24) NOT NULL,
    owner_id                varchar(64) NOT NULL,
    requested_capabilities  jsonb NOT NULL DEFAULT '{}'::jsonb,
    lease_seconds           integer NOT NULL,
    status                  varchar(24) NOT NULL DEFAULT 'pending',
    idempotency_key         varchar(128) NOT NULL,
    starts_at               timestamptz,
    expires_at              timestamptz,
    released_at             timestamptz,
    failure_code            varchar(64),
    failure_message         text,
    created_at              timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at              timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT uq_device_reservations_idempotency UNIQUE (client_id, idempotency_key),
    CONSTRAINT ck_device_reservations_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_device_reservations_owner_type CHECK (owner_type IN ('run_attempt', 'manual', 'test_run')),
    CONSTRAINT ck_device_reservations_owner_id CHECK (owner_id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_device_reservations_status CHECK (status IN ('pending', 'active', 'released', 'failed', 'expired', 'force_released')),
    CONSTRAINT ck_device_reservations_lease CHECK (lease_seconds >= 60),
    CONSTRAINT ck_device_reservations_active CHECK (
        (status = 'active' AND device_id IS NOT NULL AND starts_at IS NOT NULL AND expires_at > starts_at AND released_at IS NULL)
        OR status <> 'active'
    ),
    CONSTRAINT ck_device_reservations_terminal CHECK (
        (status IN ('released', 'expired', 'force_released') AND device_id IS NOT NULL AND released_at IS NOT NULL)
        OR status IN ('pending', 'active', 'failed')
    ),
    CONSTRAINT ck_device_reservations_time CHECK (
        updated_at >= created_at
        AND (expires_at IS NULL OR (starts_at IS NOT NULL AND expires_at > starts_at))
        AND (released_at IS NULL OR starts_at IS NULL OR released_at >= starts_at)
    )
);

CREATE UNIQUE INDEX uq_device_reservations_active_device
    ON device_reservations (device_id)
    WHERE status = 'active';
CREATE INDEX ix_device_reservations_schedule
    ON device_reservations (created_at, id)
    WHERE status = 'pending';
CREATE INDEX ix_device_reservations_expiry
    ON device_reservations (expires_at)
    WHERE status = 'active';
CREATE INDEX ix_device_reservations_owner
    ON device_reservations (owner_type, owner_id, created_at DESC);

CREATE TABLE device_sessions (
    id                  varchar(64) PRIMARY KEY,
    reservation_id      varchar(64) NOT NULL UNIQUE REFERENCES device_reservations(id) ON DELETE RESTRICT,
    device_id           varchar(64) NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
    status              varchar(24) NOT NULL DEFAULT 'starting',
    started_at          timestamptz,
    ended_at            timestamptz,
    connection_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ck_device_sessions_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_device_sessions_status CHECK (status IN ('starting', 'active', 'closing', 'closed', 'failed')),
    CONSTRAINT ck_device_sessions_active CHECK ((status = 'active' AND started_at IS NOT NULL AND ended_at IS NULL) OR status <> 'active'),
    CONSTRAINT ck_device_sessions_terminal CHECK ((status IN ('closed', 'failed') AND ended_at IS NOT NULL) OR status IN ('starting', 'active', 'closing')),
    CONSTRAINT ck_device_sessions_time CHECK (updated_at >= created_at AND (ended_at IS NULL OR started_at IS NULL OR ended_at >= started_at))
);

CREATE TABLE device_health_events (
    id              varchar(64) PRIMARY KEY,
    device_id       varchar(64) NOT NULL REFERENCES devices(id) ON DELETE RESTRICT,
    source          varchar(24) NOT NULL,
    event_type      varchar(64) NOT NULL,
    severity        varchar(16) NOT NULL,
    reason          text NOT NULL,
    payload         jsonb NOT NULL DEFAULT '{}'::jsonb,
    observed_at     timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ck_device_health_events_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_device_health_events_source CHECK (source IN ('agent', 'provider', 'adb', 'appium', 'stf', 'reconciler')),
    CONSTRAINT ck_device_health_events_severity CHECK (severity IN ('info', 'warning', 'error', 'critical'))
);

CREATE INDEX ix_device_health_events_device_time
    ON device_health_events (device_id, observed_at DESC, id);

CREATE TABLE device_audit_events (
    id              varchar(64) PRIMARY KEY,
    actor_type      varchar(24) NOT NULL,
    actor_id        varchar(128) NOT NULL,
    action          varchar(64) NOT NULL,
    resource_type   varchar(64) NOT NULL,
    resource_id     varchar(64) NOT NULL,
    request_id      varchar(128) NOT NULL,
    reason          text,
    summary         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ck_device_audit_events_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_device_audit_events_actor_type CHECK (actor_type IN ('service', 'agent', 'system'))
);

CREATE INDEX ix_device_audit_events_resource_time
    ON device_audit_events (resource_type, resource_id, created_at DESC, id);
CREATE INDEX ix_device_audit_events_request
    ON device_audit_events (request_id);

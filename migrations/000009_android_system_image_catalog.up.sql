CREATE TABLE android_system_image_catalog (
    id              varchar(64) PRIMARY KEY,
    package_name    varchar(255) NOT NULL UNIQUE,
    api_level       integer NOT NULL,
    image_type      varchar(64) NOT NULL,
    abi             varchar(32) NOT NULL,
    channel         integer NOT NULL DEFAULT 0,
    revision        varchar(64) NOT NULL,
    source_updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_seen_at    timestamptz NOT NULL DEFAULT clock_timestamp(),
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ck_android_system_image_catalog_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_android_system_image_catalog_api CHECK (api_level BETWEEN 33 AND 36),
    CONSTRAINT ck_android_system_image_catalog_type CHECK (image_type IN ('google_apis', 'google_play')),
    CONSTRAINT ck_android_system_image_catalog_abi CHECK (abi = 'x86_64'),
    CONSTRAINT ck_android_system_image_catalog_channel CHECK (channel = 0),
    CONSTRAINT ck_android_system_image_catalog_package CHECK (package_name ~ '^system-images;android-(33|34|35|36);(google_apis|google_play);x86_64$'),
    CONSTRAINT ck_android_system_image_catalog_time CHECK (updated_at >= created_at)
);

CREATE TABLE device_image_preparations (
    id                  varchar(64) PRIMARY KEY,
    catalog_id          varchar(64) NOT NULL REFERENCES android_system_image_catalog(id) ON DELETE RESTRICT,
    host_id             varchar(64) NOT NULL REFERENCES device_hosts(id) ON DELETE RESTRICT,
    build_command_id    varchar(64) NOT NULL UNIQUE REFERENCES device_host_commands(id) ON DELETE RESTRICT,
    validation_command_id varchar(64) UNIQUE REFERENCES device_host_commands(id) ON DELETE RESTRICT,
    client_id           varchar(128) NOT NULL,
    idempotency_key     varchar(128) NOT NULL,
    catalog_revision    varchar(64) NOT NULL,
    runtime_profile     jsonb NOT NULL,
    status              varchar(24) NOT NULL DEFAULT 'queued',
    docker_image        varchar(512),
    docker_digest       varchar(80),
    image_disk_mb       bigint,
    image_id            varchar(64) REFERENCES device_images(id) ON DELETE RESTRICT,
    error_code          varchar(64),
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ck_device_image_preparations_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT uq_device_image_preparations_idempotency UNIQUE (client_id, idempotency_key),
    CONSTRAINT ck_device_image_preparations_profile CHECK (jsonb_typeof(runtime_profile) = 'object'),
    CONSTRAINT ck_device_image_preparations_status CHECK (status IN ('queued', 'building', 'validating', 'cached', 'failed')),
    CONSTRAINT ck_device_image_preparations_build_result CHECK (
        (docker_image IS NULL AND docker_digest IS NULL AND image_disk_mb IS NULL)
        OR (docker_image IS NOT NULL AND docker_digest ~ '^sha256:[a-f0-9]{64}$' AND image_disk_mb > 0)
    ),
    CONSTRAINT ck_device_image_preparations_validation CHECK (
        validation_command_id IS NULL OR (docker_image IS NOT NULL AND status IN ('validating', 'cached', 'failed'))
    ),
    CONSTRAINT ck_device_image_preparations_cached CHECK (
        status <> 'cached' OR image_id IS NOT NULL
    ),
    CONSTRAINT ck_device_image_preparations_time CHECK (updated_at >= created_at)
);

CREATE INDEX ix_android_system_image_catalog_selector
    ON android_system_image_catalog (api_level, image_type, abi);
CREATE INDEX ix_device_image_preparations_catalog_created
    ON device_image_preparations (catalog_id, created_at DESC);

-- A Docker digest identifies the shared immutable image layer, not the
-- emulator's CPU, memory, disk or display profile.  The latter remain part
-- of a Device Image runtime selection, so one digest may back several
-- independently validated profiles while Host capacity still charges the
-- shared layer only once.
ALTER TABLE device_images DROP CONSTRAINT IF EXISTS device_images_docker_digest_key;
CREATE UNIQUE INDEX uq_device_images_digest_runtime_profile
    ON device_images (docker_digest, md5(resource_config::text));

ALTER TABLE device_host_commands DROP CONSTRAINT ck_device_host_commands_type;
ALTER TABLE device_host_commands ADD CONSTRAINT ck_device_host_commands_type
    CHECK (command_type IN ('create', 'start', 'stop', 'restart', 'rebuild', 'delete', 'inspect', 'validate_image', 'sync_android_catalog', 'prepare_android_image'));

COMMENT ON TABLE android_system_image_catalog IS 'Stable Android SDK System Image candidates reported by a controlled Build Agent; never Docker images.';
COMMENT ON TABLE device_image_preparations IS 'Audited on-demand build and validation requests. A device_images row is created only after real validation succeeds.';

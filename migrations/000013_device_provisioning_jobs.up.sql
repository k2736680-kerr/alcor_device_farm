CREATE TABLE device_provisioning_jobs (
    id                   varchar(64) PRIMARY KEY,
    client_id            varchar(128) NOT NULL,
    idempotency_key      varchar(128) NOT NULL,
    request_hash         varchar(128) NOT NULL,
    pool_id              varchar(64) NOT NULL REFERENCES device_pools(id) ON DELETE RESTRICT,
    catalog_id           varchar(64) NOT NULL REFERENCES android_system_image_catalog(id) ON DELETE RESTRICT,
    hardware_profile_id  varchar(64) NOT NULL,
    runtime_profile      jsonb NOT NULL,
    preparation_id       varchar(64) NULL REFERENCES device_image_preparations(id) ON DELETE RESTRICT,
    image_id             varchar(64) NULL REFERENCES device_images(id) ON DELETE RESTRICT,
    device_id            varchar(64) NULL REFERENCES devices(id) ON DELETE RESTRICT,
    command_id           varchar(64) NULL REFERENCES device_host_commands(id) ON DELETE RESTRICT,
    status               varchar(32) NOT NULL DEFAULT 'preparing_image',
    error_stage          varchar(64) NULL,
    error_code           varchar(128) NULL,
    created_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ck_device_provisioning_jobs_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT uq_device_provisioning_jobs_idempotency UNIQUE (client_id,idempotency_key),
    CONSTRAINT ck_device_provisioning_jobs_profile CHECK (jsonb_typeof(runtime_profile)='object'),
    CONSTRAINT ck_device_provisioning_jobs_status CHECK (status IN ('preparing_image','creating_emulator','adb_check','stf_registration','appium_check','ready','failed')),
    CONSTRAINT ck_device_provisioning_jobs_time CHECK (updated_at >= created_at)
);

CREATE INDEX ix_device_provisioning_jobs_active ON device_provisioning_jobs(status,updated_at)
    WHERE status NOT IN ('ready','failed');

COMMENT ON TABLE device_provisioning_jobs IS 'DF-038 durable browser-independent provisioning orchestration; it only queues existing Device Farm Agent commands.';

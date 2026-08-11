-- Upgrade the short-lived DF-035 preview schema in place. Fresh databases
-- already receive this shape from 000009; every operation below is guarded so
-- the same ordered migration set works for both paths.
DO $migration$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='device_image_preparations' AND column_name='command_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='device_image_preparations' AND column_name='build_command_id'
    ) THEN
        ALTER TABLE device_image_preparations RENAME COLUMN command_id TO build_command_id;
    END IF;
END $migration$;

ALTER TABLE device_image_preparations
    ADD COLUMN IF NOT EXISTS validation_command_id varchar(64),
    ADD COLUMN IF NOT EXISTS docker_image varchar(512),
    ADD COLUMN IF NOT EXISTS docker_digest varchar(80),
    ADD COLUMN IF NOT EXISTS image_disk_mb bigint;

DO $migration$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='device_image_preparations_validation_command_id_key') THEN
        ALTER TABLE device_image_preparations
            ADD CONSTRAINT device_image_preparations_validation_command_id_key UNIQUE (validation_command_id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='device_image_preparations_validation_command_id_fkey') THEN
        ALTER TABLE device_image_preparations
            ADD CONSTRAINT device_image_preparations_validation_command_id_fkey
            FOREIGN KEY (validation_command_id) REFERENCES device_host_commands(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='ck_device_image_preparations_build_result') THEN
        ALTER TABLE device_image_preparations ADD CONSTRAINT ck_device_image_preparations_build_result CHECK (
            (docker_image IS NULL AND docker_digest IS NULL AND image_disk_mb IS NULL)
            OR (docker_image IS NOT NULL AND docker_digest ~ '^sha256:[a-f0-9]{64}$' AND image_disk_mb > 0)
        );
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='ck_device_image_preparations_validation') THEN
        ALTER TABLE device_image_preparations ADD CONSTRAINT ck_device_image_preparations_validation CHECK (
            validation_command_id IS NULL OR (docker_image IS NOT NULL AND status IN ('validating', 'cached', 'failed'))
        );
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='ck_device_image_preparations_cached') THEN
        ALTER TABLE device_image_preparations ADD CONSTRAINT ck_device_image_preparations_cached CHECK (
            status <> 'cached' OR image_id IS NOT NULL
        );
    END IF;
END $migration$;

ALTER TABLE android_system_image_catalog
    DROP CONSTRAINT IF EXISTS ck_android_system_image_catalog_api,
    DROP CONSTRAINT IF EXISTS ck_android_system_image_catalog_abi,
    DROP CONSTRAINT IF EXISTS ck_android_system_image_catalog_package;
ALTER TABLE android_system_image_catalog
    ADD CONSTRAINT ck_android_system_image_catalog_api CHECK (api_level BETWEEN 33 AND 36),
    ADD CONSTRAINT ck_android_system_image_catalog_abi CHECK (abi = 'x86_64'),
    ADD CONSTRAINT ck_android_system_image_catalog_package
        CHECK (package_name ~ '^system-images;android-(33|34|35|36);(google_apis|google_play);x86_64$');

COMMENT ON TABLE device_image_preparations IS 'Audited on-demand build and validation requests. A device_images row is created only after real validation succeeds.';

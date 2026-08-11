ALTER TABLE android_system_image_catalog
    DROP CONSTRAINT IF EXISTS ck_android_system_image_catalog_api,
    DROP CONSTRAINT IF EXISTS ck_android_system_image_catalog_abi,
    DROP CONSTRAINT IF EXISTS ck_android_system_image_catalog_package;
ALTER TABLE android_system_image_catalog
    ADD CONSTRAINT ck_android_system_image_catalog_api CHECK (api_level >= 21),
    ADD CONSTRAINT ck_android_system_image_catalog_abi CHECK (abi IN ('x86_64', 'arm64-v8a')),
    ADD CONSTRAINT ck_android_system_image_catalog_package
        CHECK (package_name ~ '^system-images;android-[0-9]+;(google_apis|google_play);(x86_64|arm64-v8a)$');

ALTER TABLE device_image_preparations
    DROP CONSTRAINT IF EXISTS ck_device_image_preparations_cached,
    DROP CONSTRAINT IF EXISTS ck_device_image_preparations_validation,
    DROP CONSTRAINT IF EXISTS ck_device_image_preparations_build_result,
    DROP CONSTRAINT IF EXISTS device_image_preparations_validation_command_id_fkey,
    DROP CONSTRAINT IF EXISTS device_image_preparations_validation_command_id_key,
    DROP COLUMN IF EXISTS validation_command_id,
    DROP COLUMN IF EXISTS docker_image,
    DROP COLUMN IF EXISTS docker_digest,
    DROP COLUMN IF EXISTS image_disk_mb;

DO $migration$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='device_image_preparations' AND column_name='build_command_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='device_image_preparations' AND column_name='command_id'
    ) THEN
        ALTER TABLE device_image_preparations RENAME COLUMN build_command_id TO command_id;
    END IF;
END $migration$;

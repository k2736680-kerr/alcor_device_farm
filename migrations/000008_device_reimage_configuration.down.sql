DROP INDEX IF EXISTS ix_devices_reimage_pending;

ALTER TABLE devices
    DROP CONSTRAINT IF EXISTS ck_devices_reimage_pending,
    DROP CONSTRAINT IF EXISTS ck_devices_reimage_status,
    DROP CONSTRAINT IF EXISTS ck_devices_pending_runtime_profile,
    DROP CONSTRAINT IF EXISTS ck_devices_runtime_profile_override,
    DROP CONSTRAINT IF EXISTS fk_devices_pending_image,
    DROP COLUMN IF EXISTS reimage_error,
    DROP COLUMN IF EXISTS reimage_status,
    DROP COLUMN IF EXISTS pending_runtime_profile,
    DROP COLUMN IF EXISTS pending_image_id,
    DROP COLUMN IF EXISTS runtime_profile_override;

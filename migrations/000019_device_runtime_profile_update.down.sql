DROP INDEX IF EXISTS ix_devices_runtime_profile_update_pending;

UPDATE devices
SET pending_runtime_profile = NULL
WHERE runtime_profile_update_status = 'pending';

ALTER TABLE devices
    DROP CONSTRAINT IF EXISTS ck_devices_runtime_profile_update_pending,
    DROP CONSTRAINT IF EXISTS ck_devices_runtime_profile_update_status,
    DROP CONSTRAINT IF EXISTS ck_devices_reimage_pending,
    DROP COLUMN IF EXISTS runtime_profile_update_error,
    DROP COLUMN IF EXISTS runtime_profile_update_status,
    ADD CONSTRAINT ck_devices_reimage_pending
        CHECK (
            (reimage_status = 'pending' AND pending_image_id IS NOT NULL AND pending_runtime_profile IS NOT NULL)
            OR reimage_status <> 'pending'
        );

COMMENT ON COLUMN devices.pending_runtime_profile IS 'Resolved target profile while an asynchronous reimage is pending.';

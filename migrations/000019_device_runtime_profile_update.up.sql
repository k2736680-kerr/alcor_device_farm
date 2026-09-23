ALTER TABLE devices
    ADD COLUMN runtime_profile_update_status varchar(24) NOT NULL DEFAULT 'idle',
    ADD COLUMN runtime_profile_update_error text;

ALTER TABLE devices
    DROP CONSTRAINT ck_devices_reimage_pending,
    ADD CONSTRAINT ck_devices_reimage_pending
        CHECK (
            (reimage_status = 'pending'
                AND runtime_profile_update_status <> 'pending'
                AND pending_image_id IS NOT NULL
                AND pending_runtime_profile IS NOT NULL)
            OR reimage_status <> 'pending'
        ),
    ADD CONSTRAINT ck_devices_runtime_profile_update_status
        CHECK (runtime_profile_update_status IN ('idle', 'pending', 'failed')),
    ADD CONSTRAINT ck_devices_runtime_profile_update_pending
        CHECK (
            (runtime_profile_update_status = 'pending'
                AND reimage_status <> 'pending'
                AND pending_image_id IS NULL
                AND pending_runtime_profile IS NOT NULL)
            OR runtime_profile_update_status <> 'pending'
        );

CREATE INDEX ix_devices_runtime_profile_update_pending
    ON devices (host_id, updated_at, id)
    WHERE runtime_profile_update_status = 'pending';

COMMENT ON COLUMN devices.pending_runtime_profile IS 'Resolved target profile while an asynchronous reimage or runtime profile update is pending.';
COMMENT ON COLUMN devices.runtime_profile_update_status IS 'Visible non-destructive CPU and memory update state retained across Console refreshes.';

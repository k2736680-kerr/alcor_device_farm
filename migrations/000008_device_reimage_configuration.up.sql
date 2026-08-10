ALTER TABLE devices
    ADD COLUMN runtime_profile_override jsonb,
    ADD COLUMN pending_image_id varchar(64),
    ADD COLUMN pending_runtime_profile jsonb,
    ADD COLUMN reimage_status varchar(24) NOT NULL DEFAULT 'idle',
    ADD COLUMN reimage_error text;

ALTER TABLE devices
    ADD CONSTRAINT fk_devices_pending_image
        FOREIGN KEY (pending_image_id) REFERENCES device_images(id) ON DELETE RESTRICT,
    ADD CONSTRAINT ck_devices_runtime_profile_override
        CHECK (runtime_profile_override IS NULL OR jsonb_typeof(runtime_profile_override) = 'object'),
    ADD CONSTRAINT ck_devices_pending_runtime_profile
        CHECK (pending_runtime_profile IS NULL OR jsonb_typeof(pending_runtime_profile) = 'object'),
    ADD CONSTRAINT ck_devices_reimage_status
        CHECK (reimage_status IN ('idle', 'pending', 'failed')),
    ADD CONSTRAINT ck_devices_reimage_pending
        CHECK (
            (reimage_status = 'pending' AND pending_image_id IS NOT NULL AND pending_runtime_profile IS NOT NULL)
            OR reimage_status <> 'pending'
        );

CREATE INDEX ix_devices_reimage_pending
    ON devices (host_id, updated_at, id)
    WHERE reimage_status = 'pending';

COMMENT ON COLUMN devices.runtime_profile_override IS 'Optional per-device profile; NULL inherits the current Image profile.';
COMMENT ON COLUMN devices.pending_image_id IS 'Target Image while an asynchronous reimage is pending.';
COMMENT ON COLUMN devices.pending_runtime_profile IS 'Resolved target profile while an asynchronous reimage is pending.';
COMMENT ON COLUMN devices.reimage_status IS 'Visible reimage state retained across Console refreshes.';

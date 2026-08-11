-- DF-038: a pool expands from a selected long-lived base device.  A pool may
-- deliberately be empty after its last device has been deleted.
ALTER TABLE device_pools
    ADD COLUMN base_device_id varchar(64) NULL REFERENCES devices(id) ON DELETE RESTRICT;

CREATE INDEX idx_device_pools_base_device ON device_pools(base_device_id) WHERE base_device_id IS NOT NULL;

ALTER TABLE device_pools
    DROP CONSTRAINT IF EXISTS ck_device_pools_concurrency,
    DROP CONSTRAINT IF EXISTS ck_device_pools_total_target,
    DROP CONSTRAINT IF EXISTS ck_device_pools_min_ready,
    ADD CONSTRAINT ck_device_pools_concurrency CHECK (max_concurrency > 0),
    ADD CONSTRAINT ck_device_pools_total_target CHECK (total_target >= 0),
    ADD CONSTRAINT ck_device_pools_min_ready CHECK (min_ready >= 0 AND min_ready <= total_target);

-- Preserve existing behaviour after an upgrade only when a member is already
-- a healthy Phone emulator with a complete hardware profile. Other pools keep
-- their legacy default-image fallback until an administrator selects a base.
UPDATE device_pools p
SET base_device_id = candidate.device_id
FROM (
    SELECT p2.id AS pool_id, (
        SELECT pd.device_id
        FROM device_pool_devices pd
        JOIN devices d ON d.id = pd.device_id
        WHERE pd.pool_id = p2.id
          AND pd.enabled
          AND d.device_kind = 'emulator'
          AND d.provider_type = 'docker_emulator'
          AND d.lifecycle_status = 'ready'
          AND d.health_status = 'healthy'
          AND d.capabilities ? 'hardware_profile_id'
        ORDER BY d.updated_at DESC, d.id DESC
        LIMIT 1
    ) AS device_id
    FROM device_pools p2
    WHERE p2.base_device_id IS NULL
) candidate
WHERE p.id = candidate.pool_id
  AND candidate.device_id IS NOT NULL;

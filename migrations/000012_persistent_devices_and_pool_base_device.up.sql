-- DF-038: a pool expands from a selected long-lived base device.  A pool may
-- deliberately be empty after its last device has been deleted.
ALTER TABLE device_pools
    ADD COLUMN base_device_id varchar(64) NULL REFERENCES devices(id) ON DELETE RESTRICT;

CREATE INDEX idx_device_pools_base_device ON device_pools(base_device_id) WHERE base_device_id IS NOT NULL;

ALTER TABLE device_pools
    DROP CONSTRAINT IF EXISTS ck_device_pools_concurrency,
    DROP CONSTRAINT IF EXISTS ck_device_pools_total_target,
    DROP CONSTRAINT IF EXISTS ck_device_pools_min_ready,
    ADD CONSTRAINT ck_device_pools_concurrency CHECK (max_concurrency >= 0 AND max_concurrency <= total_target),
    ADD CONSTRAINT ck_device_pools_total_target CHECK (total_target >= 0),
    ADD CONSTRAINT ck_device_pools_min_ready CHECK (min_ready >= 0 AND min_ready <= total_target);

-- Preserve existing behaviour after an upgrade: use the newest usable member
-- as the initial base where one is available. Administrators can change it.
UPDATE device_pools p
SET base_device_id = candidate.device_id
FROM LATERAL (
    SELECT pd.device_id
    FROM device_pool_devices pd
    JOIN devices d ON d.id = pd.device_id
    WHERE pd.pool_id = p.id
      AND pd.enabled
      AND d.device_kind = 'emulator'
      AND d.provider_type = 'docker_emulator'
      AND d.lifecycle_status IN ('ready','reserved','busy')
    ORDER BY d.updated_at DESC, d.id DESC
    LIMIT 1
) candidate
WHERE p.base_device_id IS NULL;

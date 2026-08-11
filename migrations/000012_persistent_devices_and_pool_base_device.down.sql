UPDATE device_pools
SET total_target=GREATEST(total_target, 1),
    min_ready=GREATEST(min_ready, 1),
    max_concurrency=GREATEST(max_concurrency, 1);

ALTER TABLE device_pools
    DROP CONSTRAINT IF EXISTS ck_device_pools_concurrency,
    DROP CONSTRAINT IF EXISTS ck_device_pools_total_target,
    DROP CONSTRAINT IF EXISTS ck_device_pools_min_ready,
    ADD CONSTRAINT ck_device_pools_concurrency CHECK (max_concurrency > 0),
    ADD CONSTRAINT ck_device_pools_total_target CHECK (total_target > 0),
    ADD CONSTRAINT ck_device_pools_min_ready CHECK (min_ready >= 0 AND min_ready <= total_target),
    DROP COLUMN IF EXISTS base_device_id;

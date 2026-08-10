DROP INDEX IF EXISTS ix_device_pools_default_image;

ALTER TABLE device_pools
    DROP CONSTRAINT IF EXISTS fk_device_pools_default_image,
    DROP CONSTRAINT IF EXISTS ck_device_pools_min_ready,
    DROP CONSTRAINT IF EXISTS ck_device_pools_total_target,
    DROP COLUMN IF EXISTS default_image_id,
    DROP COLUMN IF EXISTS min_ready,
    DROP COLUMN IF EXISTS total_target;

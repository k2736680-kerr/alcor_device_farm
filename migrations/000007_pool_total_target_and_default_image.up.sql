ALTER TABLE device_pools
    ADD COLUMN total_target integer NOT NULL DEFAULT 1,
    ADD COLUMN min_ready integer NOT NULL DEFAULT 0,
    ADD COLUMN default_image_id varchar(64);

WITH existing_targets AS (
    SELECT
        p.id,
        GREATEST(1, COALESCE(sum(pi.max_instances) FILTER (WHERE pi.enabled), p.max_concurrency, 1)::integer) AS total_target,
        GREATEST(0, COALESCE(sum(pi.min_ready) FILTER (WHERE pi.enabled), 0)::integer) AS min_ready,
        (
            SELECT selected.image_id
            FROM device_pool_images selected
            JOIN device_images image ON image.id = selected.image_id
            WHERE selected.pool_id = p.id AND selected.enabled AND image.status = 'ready'
            ORDER BY image.api_level DESC, selected.created_at, selected.image_id
            LIMIT 1
        ) AS default_image_id
    FROM device_pools p
    LEFT JOIN device_pool_images pi ON pi.pool_id = p.id
    GROUP BY p.id
)
UPDATE device_pools p
SET total_target = targets.total_target,
    min_ready = LEAST(targets.min_ready, targets.total_target),
    max_concurrency = LEAST(p.max_concurrency, targets.total_target),
    default_image_id = targets.default_image_id
FROM existing_targets targets
WHERE targets.id = p.id;

ALTER TABLE device_pools
    ADD CONSTRAINT ck_device_pools_total_target CHECK (total_target > 0),
    ADD CONSTRAINT ck_device_pools_min_ready CHECK (min_ready >= 0 AND min_ready <= total_target),
    ADD CONSTRAINT fk_device_pools_default_image
        FOREIGN KEY (default_image_id) REFERENCES device_images(id) ON DELETE RESTRICT;

CREATE INDEX ix_device_pools_default_image ON device_pools(default_image_id);

COMMENT ON COLUMN device_pools.total_target IS 'Pool-wide emulator target; never summed per Image.';
COMMENT ON COLUMN device_pools.min_ready IS 'Minimum ready capacity objective within total_target.';
COMMENT ON COLUMN device_pools.default_image_id IS 'Image used only for automatic additions; changing it does not reimage existing devices.';

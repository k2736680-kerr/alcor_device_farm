-- Emulator images are referenced as `127.0.0.1:5001/alcor/android-emulator:<tag>`,
-- i.e. a HOST-LOCAL registry. The same tag can therefore resolve to different
-- content on different hosts, and `device_images.docker_digest` pins exactly one
-- of those contents. The host verifies that digest at create time
-- (providers/docker VerifyImageDigest, which only InspectImage and never pulls),
-- so a create can only succeed on a host that already has the pinned content.
--
-- The control plane did not model that: host selection only looked at online
-- status and free capacity, so a pool whose image lives on exactly one host was
-- happily scheduled onto a different host. The create then failed with
-- IMAGE_DIGEST_MISMATCH, the placeholder device was cleaned up, and the warm pool
-- retried on the next tick -- an endless create/delete churn that also stranded
-- real Alcor runs on a pool that could never be served.
--
-- This table records what we have actually observed about one (host, image)
-- pair, so scheduling can avoid repeating a failure it has already proven:
--   available = true   the host demonstrably has the pinned image
--   available = false  the host demonstrably does not (IMAGE_DIGEST_MISMATCH /
--                      IMAGE_NOT_FOUND); a row is only respected while fresh, so
--                      a host that later gains the image is retried automatically
-- Absence of a row means "never tried", which stays optimistically schedulable so
-- brand-new images and pools still provision on first use.
CREATE TABLE IF NOT EXISTS device_host_image_states (
    host_id     text        NOT NULL REFERENCES device_hosts(id) ON DELETE CASCADE,
    image_id    text        NOT NULL REFERENCES device_images(id) ON DELETE CASCADE,
    available   boolean     NOT NULL,
    error_code  text,
    observed_at timestamptz NOT NULL,
    PRIMARY KEY (host_id, image_id)
);

CREATE INDEX IF NOT EXISTS ix_device_host_image_states_image
    ON device_host_image_states (image_id, available, observed_at DESC);

COMMENT ON TABLE device_host_image_states IS 'Per (host, image) observed availability of a pinned emulator image digest. Learned from real create/validate outcomes; absence of a row means never tried and stays optimistically schedulable.';
COMMENT ON COLUMN device_host_image_states.available IS 'True when the host demonstrably holds the image digest pinned by device_images.docker_digest; false when a create or validate proved it does not.';
COMMENT ON COLUMN device_host_image_states.error_code IS 'Host error code that proved unavailability, e.g. IMAGE_DIGEST_MISMATCH or IMAGE_NOT_FOUND.';
COMMENT ON COLUMN device_host_image_states.observed_at IS 'When the observation was made. Stale rows are ignored so a host that later gains the image is retried without operator action.';
ALTER TABLE device_images
    DROP CONSTRAINT IF EXISTS ck_device_images_runnable_reference,
    DROP CONSTRAINT IF EXISTS ck_device_images_runtime_reference,
    DROP COLUMN IF EXISTS docker_image;

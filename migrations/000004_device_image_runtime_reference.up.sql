ALTER TABLE device_images
    ADD COLUMN docker_image varchar(512);

UPDATE device_images
SET status = CASE WHEN status = 'disabled' THEN 'disabled' ELSE 'draft' END,
    validation_error = 'IMAGE_REFERENCE_REQUIRED',
    updated_at = clock_timestamp()
WHERE docker_image IS NULL;

ALTER TABLE device_images
    ADD CONSTRAINT ck_device_images_runtime_reference
        CHECK (
            docker_image IS NULL OR (
                docker_image ~ '^[A-Za-z0-9._:/@-]+$'
                AND docker_image !~* ':latest$'
                AND (
                    docker_image ~ '@sha256:[a-fA-F0-9]{64}$'
                    OR (docker_image !~ '@' AND docker_image ~ ':[A-Za-z0-9._-]+$')
                )
            )
        ),
    ADD CONSTRAINT ck_device_images_runnable_reference
        CHECK (docker_image IS NOT NULL OR status IN ('draft', 'failed', 'disabled'));

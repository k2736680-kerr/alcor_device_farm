ALTER TABLE device_host_commands DROP CONSTRAINT ck_device_host_commands_type;
ALTER TABLE device_host_commands ADD CONSTRAINT ck_device_host_commands_type
    CHECK (command_type IN ('create', 'start', 'stop', 'restart', 'rebuild', 'delete', 'inspect', 'validate_image'));

DROP TABLE IF EXISTS device_image_preparations;
DROP TABLE IF EXISTS android_system_image_catalog;

DROP INDEX IF EXISTS uq_device_images_digest_runtime_profile;
ALTER TABLE device_images ADD CONSTRAINT device_images_docker_digest_key UNIQUE (docker_digest);

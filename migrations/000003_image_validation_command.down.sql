ALTER TABLE device_host_commands
    DROP CONSTRAINT ck_device_host_commands_type;

ALTER TABLE device_host_commands
    ADD CONSTRAINT ck_device_host_commands_type
    CHECK (command_type IN ('create', 'start', 'stop', 'restart', 'rebuild', 'delete', 'inspect'));

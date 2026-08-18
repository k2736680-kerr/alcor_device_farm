ALTER TABLE device_host_commands
    DROP CONSTRAINT ck_device_host_commands_type;

ALTER TABLE device_host_commands
    ADD CONSTRAINT ck_device_host_commands_type
        CHECK (command_type IN (
            'create', 'start', 'stop', 'restart', 'rebuild', 'delete', 'inspect',
            'validate_image', 'sync_android_catalog', 'prepare_android_image'
        ));

COMMENT ON CONSTRAINT ck_device_host_commands_type ON device_host_commands IS
    'Host Agent 受控命令类型；iOS Simulator 远控复用 Session Fence，不新增宿主机窗口命令。';

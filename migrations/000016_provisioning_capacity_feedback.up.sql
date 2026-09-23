ALTER TABLE device_provisioning_jobs
    DROP CONSTRAINT ck_device_provisioning_jobs_status;

ALTER TABLE device_provisioning_jobs
    ADD COLUMN capacity_result jsonb NULL,
    ADD CONSTRAINT ck_device_provisioning_jobs_capacity_result
        CHECK (capacity_result IS NULL OR jsonb_typeof(capacity_result)='object'),
    ADD CONSTRAINT ck_device_provisioning_jobs_status
        CHECK (status IN ('preparing_image','waiting_capacity','creating_emulator','adb_check','stf_registration','appium_check','ready','failed'));

COMMENT ON COLUMN device_provisioning_jobs.capacity_result IS
    'Structured Host capacity shortfall shown by the Device Farm Console while the durable job remains retryable.';

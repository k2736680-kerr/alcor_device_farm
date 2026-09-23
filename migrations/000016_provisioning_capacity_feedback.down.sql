ALTER TABLE device_provisioning_jobs
    DROP CONSTRAINT ck_device_provisioning_jobs_status,
    DROP CONSTRAINT ck_device_provisioning_jobs_capacity_result,
    DROP COLUMN capacity_result;

ALTER TABLE device_provisioning_jobs
    ADD CONSTRAINT ck_device_provisioning_jobs_status
        CHECK (status IN ('preparing_image','creating_emulator','adb_check','stf_registration','appium_check','ready','failed'));

ALTER TABLE devices DROP CONSTRAINT devices_serial_key;

DROP INDEX uq_devices_stf_serial;
DROP INDEX uq_devices_adb_endpoint;
DROP INDEX uq_devices_appium_endpoint;

CREATE UNIQUE INDEX uq_devices_active_serial
    ON devices (serial)
    WHERE lifecycle_status NOT IN ('quarantined', 'deleted');
CREATE UNIQUE INDEX uq_devices_active_stf_serial
    ON devices (stf_serial)
    WHERE stf_serial IS NOT NULL AND lifecycle_status NOT IN ('quarantined', 'deleted');
CREATE UNIQUE INDEX uq_devices_active_adb_endpoint
    ON devices (adb_endpoint)
    WHERE adb_endpoint IS NOT NULL AND lifecycle_status NOT IN ('quarantined', 'deleted');
CREATE UNIQUE INDEX uq_devices_active_appium_endpoint
    ON devices (appium_endpoint)
    WHERE appium_endpoint IS NOT NULL AND lifecycle_status NOT IN ('quarantined', 'deleted');

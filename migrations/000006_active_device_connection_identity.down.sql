DROP INDEX uq_devices_active_appium_endpoint;
DROP INDEX uq_devices_active_adb_endpoint;
DROP INDEX uq_devices_active_stf_serial;
DROP INDEX uq_devices_active_serial;

ALTER TABLE devices ADD CONSTRAINT devices_serial_key UNIQUE (serial);
CREATE UNIQUE INDEX uq_devices_stf_serial ON devices (stf_serial) WHERE stf_serial IS NOT NULL;
CREATE UNIQUE INDEX uq_devices_adb_endpoint ON devices (adb_endpoint) WHERE adb_endpoint IS NOT NULL;
CREATE UNIQUE INDEX uq_devices_appium_endpoint ON devices (appium_endpoint) WHERE appium_endpoint IS NOT NULL;

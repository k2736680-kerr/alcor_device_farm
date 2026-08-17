-- A downgrade cannot represent iOS or non-default Host metadata. Refuse it
-- instead of silently rewriting those rows as Android/Linux.
DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM devices WHERE platform <> 'android')
        OR EXISTS (SELECT 1 FROM device_pools WHERE platform <> 'android')
        OR EXISTS (SELECT 1 FROM device_hosts WHERE host_os <> 'linux' OR host_arch <> 'unknown' OR host_type = 'appium_device_farm_ios') THEN
        RAISE EXCEPTION 'cannot downgrade platform-neutral device domain while non-legacy platform data exists';
    END IF;

    IF EXISTS (
        SELECT appium_endpoint
        FROM devices
        WHERE appium_endpoint IS NOT NULL AND lifecycle_status NOT IN ('quarantined', 'deleted')
        GROUP BY appium_endpoint HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot downgrade while active devices share an Appium endpoint';
    END IF;
END
$guard$;

DROP FUNCTION device_schedulable_capabilities(jsonb);

DROP TRIGGER trg_devices_platform ON devices;
DROP FUNCTION prevent_device_platform_mismatch();
DROP TRIGGER trg_device_pools_platform ON device_pools;
DROP FUNCTION prevent_pool_platform_mismatch();
DROP TRIGGER trg_device_pool_devices_platform ON device_pool_devices;
DROP FUNCTION enforce_device_pool_platform_match();

DROP INDEX ix_devices_appium_endpoint;
DROP INDEX uq_devices_active_android_appium_endpoint;
CREATE UNIQUE INDEX uq_devices_active_appium_endpoint
    ON devices (appium_endpoint)
    WHERE appium_endpoint IS NOT NULL AND lifecycle_status NOT IN ('quarantined', 'deleted');

ALTER TABLE devices
    DROP CONSTRAINT ck_devices_kind_image,
    DROP CONSTRAINT ck_devices_platform_provider,
    DROP CONSTRAINT ck_devices_platform_kind,
    DROP CONSTRAINT ck_devices_provider,
    DROP CONSTRAINT ck_devices_kind,
    DROP CONSTRAINT ck_devices_platform,
    DROP COLUMN platform,
    ADD CONSTRAINT ck_devices_kind CHECK (device_kind IN ('emulator', 'physical')),
    ADD CONSTRAINT ck_devices_provider CHECK (provider_type IN ('mock', 'docker_emulator', 'usb_android')),
    ADD CONSTRAINT ck_devices_kind_image CHECK ((device_kind = 'emulator' AND image_id IS NOT NULL) OR device_kind = 'physical');

ALTER TABLE device_pools
    DROP CONSTRAINT ck_device_pools_platform,
    DROP COLUMN platform;

ALTER TABLE device_hosts
    DROP CONSTRAINT ck_device_hosts_type,
    DROP CONSTRAINT ck_device_hosts_ios_macos,
    DROP CONSTRAINT ck_device_hosts_arch,
    DROP CONSTRAINT ck_device_hosts_os,
    DROP COLUMN host_arch,
    DROP COLUMN host_os,
    ADD CONSTRAINT ck_device_hosts_type CHECK (host_type IN ('docker_emulator', 'usb_android', 'hybrid'));

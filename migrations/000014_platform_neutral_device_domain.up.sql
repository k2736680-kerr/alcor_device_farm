-- DF-040: make the device domain explicit about Android and iOS while
-- preserving every existing row as Android.
ALTER TABLE device_hosts
    ADD COLUMN host_os varchar(16) NOT NULL DEFAULT 'linux',
    ADD COLUMN host_arch varchar(32) NOT NULL DEFAULT 'unknown',
    ADD CONSTRAINT ck_device_hosts_os CHECK (host_os IN ('linux', 'macos', 'windows')),
    ADD CONSTRAINT ck_device_hosts_arch CHECK (host_arch ~ '^[A-Za-z0-9_.-]{1,32}$');

ALTER TABLE device_hosts
    DROP CONSTRAINT ck_device_hosts_type,
    ADD CONSTRAINT ck_device_hosts_type CHECK (host_type IN ('docker_emulator', 'usb_android', 'hybrid', 'appium_device_farm_ios')),
    ADD CONSTRAINT ck_device_hosts_ios_macos CHECK (host_type <> 'appium_device_farm_ios' OR host_os = 'macos');

ALTER TABLE device_pools
    ADD COLUMN platform varchar(16) NOT NULL DEFAULT 'android',
    ADD CONSTRAINT ck_device_pools_platform CHECK (platform IN ('android', 'ios'));

ALTER TABLE devices
    ADD COLUMN platform varchar(16) NOT NULL DEFAULT 'android';

ALTER TABLE devices
    DROP CONSTRAINT ck_devices_kind,
    DROP CONSTRAINT ck_devices_provider,
    DROP CONSTRAINT ck_devices_kind_image,
    ADD CONSTRAINT ck_devices_platform CHECK (platform IN ('android', 'ios')),
    ADD CONSTRAINT ck_devices_kind CHECK (device_kind IN ('emulator', 'simulator', 'physical')),
    ADD CONSTRAINT ck_devices_provider CHECK (provider_type IN ('mock', 'docker_emulator', 'usb_android', 'appium_device_farm_ios')),
    ADD CONSTRAINT ck_devices_platform_kind CHECK (
        (platform = 'android' AND device_kind IN ('emulator', 'physical'))
        OR (platform = 'ios' AND device_kind IN ('simulator', 'physical'))
    ),
    ADD CONSTRAINT ck_devices_platform_provider CHECK (
        provider_type = 'mock'
        OR (platform = 'android' AND device_kind = 'emulator' AND provider_type = 'docker_emulator')
        OR (platform = 'android' AND device_kind = 'physical' AND provider_type = 'usb_android')
        OR (platform = 'ios' AND device_kind IN ('simulator', 'physical') AND provider_type = 'appium_device_farm_ios')
    ),
    ADD CONSTRAINT ck_devices_kind_image CHECK (
        (platform = 'android' AND device_kind = 'emulator' AND image_id IS NOT NULL)
        OR (platform = 'android' AND device_kind = 'physical' AND image_id IS NULL)
        OR (platform = 'ios' AND device_kind IN ('simulator', 'physical') AND image_id IS NULL)
    );

-- One Appium Device Farm Node on a macOS Host routes sessions for many iOS
-- devices. Endpoint identity therefore cannot be a Device uniqueness key.
DROP INDEX uq_devices_active_appium_endpoint;
CREATE UNIQUE INDEX uq_devices_active_android_appium_endpoint
    ON devices (appium_endpoint)
    WHERE platform = 'android' AND appium_endpoint IS NOT NULL
      AND lifecycle_status NOT IN ('quarantined', 'deleted');
CREATE INDEX ix_devices_appium_endpoint
    ON devices (appium_endpoint)
    WHERE appium_endpoint IS NOT NULL AND lifecycle_status NOT IN ('quarantined', 'deleted');

CREATE OR REPLACE FUNCTION enforce_device_pool_platform_match()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
DECLARE
    pool_platform varchar(16);
    device_platform varchar(16);
BEGIN
    SELECT platform INTO pool_platform FROM device_pools WHERE id = NEW.pool_id;
    SELECT platform INTO device_platform FROM devices WHERE id = NEW.device_id;
    IF pool_platform IS DISTINCT FROM device_platform THEN
        RAISE EXCEPTION 'device pool platform % does not match device platform %', pool_platform, device_platform
            USING ERRCODE = '23514', CONSTRAINT = 'ck_device_pool_devices_platform';
    END IF;
    RETURN NEW;
END
$function$;

CREATE TRIGGER trg_device_pool_devices_platform
BEFORE INSERT OR UPDATE OF pool_id, device_id ON device_pool_devices
FOR EACH ROW EXECUTE FUNCTION enforce_device_pool_platform_match();

CREATE OR REPLACE FUNCTION prevent_pool_platform_mismatch()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    IF NEW.platform <> OLD.platform AND EXISTS (
        SELECT 1 FROM device_pool_devices membership
        JOIN devices device ON device.id = membership.device_id
        WHERE membership.pool_id = NEW.id AND device.platform <> NEW.platform
    ) THEN
        RAISE EXCEPTION 'device pool platform conflicts with an existing device membership'
            USING ERRCODE = '23514', CONSTRAINT = 'ck_device_pool_devices_platform';
    END IF;
    RETURN NEW;
END
$function$;

CREATE TRIGGER trg_device_pools_platform
BEFORE UPDATE OF platform ON device_pools
FOR EACH ROW EXECUTE FUNCTION prevent_pool_platform_mismatch();

CREATE OR REPLACE FUNCTION prevent_device_platform_mismatch()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    IF NEW.platform <> OLD.platform AND EXISTS (
        SELECT 1 FROM device_pool_devices membership
        JOIN device_pools pool ON pool.id = membership.pool_id
        WHERE membership.device_id = NEW.id AND pool.platform <> NEW.platform
    ) THEN
        RAISE EXCEPTION 'device platform conflicts with an existing pool membership'
            USING ERRCODE = '23514', CONSTRAINT = 'ck_device_pool_devices_platform';
    END IF;
    RETURN NEW;
END
$function$;

CREATE TRIGGER trg_devices_platform
BEFORE UPDATE OF platform ON devices
FOR EACH ROW EXECUTE FUNCTION prevent_device_platform_mismatch();

COMMENT ON COLUMN device_hosts.host_os IS 'Normalized Host operating system: linux, macos, or windows.';
COMMENT ON COLUMN device_hosts.host_arch IS 'Normalized Host architecture reported by the Host Agent.';
COMMENT ON COLUMN device_pools.platform IS 'Single platform owned by this Pool; mixed Android/iOS membership is forbidden.';
COMMENT ON COLUMN devices.platform IS 'Normalized Device platform: android or ios.';

-- Only registered, non-secret capability keys participate in scheduling.
-- Unknown keys remain in the Reservation as non-authoritative extensions.
CREATE OR REPLACE FUNCTION device_schedulable_capabilities(requested jsonb)
RETURNS jsonb
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $function$
    SELECT COALESCE(jsonb_object_agg(capability.key, capability.value), '{}'::jsonb)
    FROM jsonb_each(COALESCE(requested, '{}'::jsonb)) AS capability
    WHERE capability.key IN (
        'platformVersion', 'automationName', 'deviceClass', 'realDevice', 'model',
        'apiLevel', 'abi', 'resolution', 'hardware_profile_id'
    )
$function$;

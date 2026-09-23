ALTER TABLE device_health_events DROP CONSTRAINT ck_device_health_events_source;
ALTER TABLE device_health_events ADD CONSTRAINT ck_device_health_events_source
    CHECK (source IN ('agent', 'provider', 'adb', 'appium', 'stf', 'reconciler'));

DROP INDEX IF EXISTS uq_device_sessions_active_appium_device;
DROP INDEX IF EXISTS uq_device_sessions_grant_hash;

ALTER TABLE device_sessions
    DROP CONSTRAINT IF EXISTS ck_device_sessions_appium_binding,
    DROP CONSTRAINT IF EXISTS ck_device_sessions_grant,
    DROP COLUMN IF EXISTS appium_session_ended_at,
    DROP COLUMN IF EXISTS appium_session_started_at,
    DROP COLUMN IF EXISTS appium_session_id,
    DROP COLUMN IF EXISTS session_grant_consumed_at,
    DROP COLUMN IF EXISTS session_grant_expires_at,
    DROP COLUMN IF EXISTS session_grant_hash;

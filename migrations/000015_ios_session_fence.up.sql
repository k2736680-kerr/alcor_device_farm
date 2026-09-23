-- DF-042: bind one-time iOS Session Grants and the upstream Appium Session
-- to the existing Reservation-owned Device Session.
ALTER TABLE device_sessions
    ADD COLUMN session_grant_hash char(64),
    ADD COLUMN session_grant_expires_at timestamptz,
    ADD COLUMN session_grant_consumed_at timestamptz,
    ADD COLUMN appium_session_id varchar(128),
    ADD COLUMN appium_session_started_at timestamptz,
    ADD COLUMN appium_session_ended_at timestamptz,
    ADD CONSTRAINT ck_device_sessions_grant CHECK (
        (session_grant_hash IS NULL AND session_grant_expires_at IS NULL AND session_grant_consumed_at IS NULL)
        OR (session_grant_hash ~ '^[a-f0-9]{64}$' AND session_grant_expires_at IS NOT NULL
            AND (session_grant_consumed_at IS NULL OR session_grant_consumed_at <= updated_at))
    ),
    ADD CONSTRAINT ck_device_sessions_appium_binding CHECK (
        (appium_session_id IS NULL AND appium_session_started_at IS NULL AND appium_session_ended_at IS NULL)
        OR (appium_session_id IS NOT NULL AND length(appium_session_id) BETWEEN 1 AND 128
            AND appium_session_started_at IS NOT NULL
            AND (appium_session_ended_at IS NULL OR appium_session_ended_at >= appium_session_started_at))
    );

CREATE UNIQUE INDEX uq_device_sessions_grant_hash
    ON device_sessions(session_grant_hash)
    WHERE session_grant_hash IS NOT NULL;

CREATE UNIQUE INDEX uq_device_sessions_active_appium_device
    ON device_sessions(device_id)
    WHERE appium_session_id IS NOT NULL AND appium_session_ended_at IS NULL
      AND status IN ('starting','active','closing');

ALTER TABLE device_health_events DROP CONSTRAINT ck_device_health_events_source;
ALTER TABLE device_health_events ADD CONSTRAINT ck_device_health_events_source
    CHECK (source IN ('agent', 'provider', 'adb', 'appium', 'stf', 'reconciler', 'session_fence'));

COMMENT ON COLUMN device_sessions.session_grant_hash IS 'SHA-256 of the one-time iOS Session Grant; plaintext is never persisted.';
COMMENT ON COLUMN device_sessions.appium_session_id IS 'Upstream Appium Session ID bound by the Host Session Fence.';

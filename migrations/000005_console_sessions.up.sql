ALTER TABLE device_audit_events
    DROP CONSTRAINT ck_device_audit_events_actor_type,
    ADD CONSTRAINT ck_device_audit_events_actor_type
        CHECK (actor_type IN ('service', 'agent', 'system', 'console'));

ALTER TABLE device_reservations
    DROP CONSTRAINT ck_device_reservations_owner_id,
    ADD CONSTRAINT ck_device_reservations_owner_id CHECK (
        (owner_type = 'manual' AND owner_id ~ '^[A-Za-z0-9_.@-]{1,64}$')
        OR (owner_type <> 'manual' AND owner_id ~ '^[A-Za-z0-9_-]{16,64}$')
    );

CREATE TABLE device_console_sessions (
    id              varchar(64) PRIMARY KEY,
    token_hash      bytea NOT NULL UNIQUE,
    user_id         varchar(64) NOT NULL,
    display_name    varchar(128) NOT NULL,
    role            varchar(16) NOT NULL,
    csrf_hash       bytea NOT NULL,
    source_address  inet NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_seen_at    timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at      timestamptz NOT NULL,
    revoked_at      timestamptz,
    CONSTRAINT ck_device_console_sessions_id CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$'),
    CONSTRAINT ck_device_console_sessions_user_id CHECK (user_id ~ '^[A-Za-z0-9_.@-]{1,64}$'),
    CONSTRAINT ck_device_console_sessions_role CHECK (role IN ('viewer', 'operator', 'admin')),
    CONSTRAINT ck_device_console_sessions_token_hash CHECK (octet_length(token_hash) = 32),
    CONSTRAINT ck_device_console_sessions_csrf_hash CHECK (octet_length(csrf_hash) = 32),
    CONSTRAINT ck_device_console_sessions_time CHECK (
        last_seen_at >= created_at
        AND expires_at > created_at
        AND (revoked_at IS NULL OR revoked_at >= created_at)
    )
);

CREATE INDEX ix_device_console_sessions_user
    ON device_console_sessions (user_id, created_at DESC);
CREATE INDEX ix_device_console_sessions_expiry
    ON device_console_sessions (expires_at)
    WHERE revoked_at IS NULL;

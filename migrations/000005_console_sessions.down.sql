DROP TABLE IF EXISTS device_console_sessions;

ALTER TABLE device_reservations
    DROP CONSTRAINT ck_device_reservations_owner_id,
    ADD CONSTRAINT ck_device_reservations_owner_id CHECK (owner_id ~ '^[A-Za-z0-9_-]{16,64}$');

DELETE FROM device_audit_events WHERE actor_type = 'console';
ALTER TABLE device_audit_events
    DROP CONSTRAINT ck_device_audit_events_actor_type,
    ADD CONSTRAINT ck_device_audit_events_actor_type
        CHECK (actor_type IN ('service', 'agent', 'system'));

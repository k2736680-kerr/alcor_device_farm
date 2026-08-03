CREATE TABLE device_idempotency_records (
    client_id       varchar(128) NOT NULL,
    scope           varchar(128) NOT NULL,
    idempotency_key varchar(128) NOT NULL,
    request_hash    char(64) NOT NULL,
    resource_type   varchar(64) NOT NULL,
    resource_id     varchar(64) NOT NULL,
    response_status integer NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at      timestamptz,
    PRIMARY KEY (client_id, scope, idempotency_key),
    CONSTRAINT ck_device_idempotency_hash CHECK (request_hash ~ '^[a-f0-9]{64}$'),
    CONSTRAINT ck_device_idempotency_status CHECK (response_status BETWEEN 200 AND 299),
    CONSTRAINT ck_device_idempotency_expiry CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE INDEX ix_device_idempotency_records_expiry
    ON device_idempotency_records (expires_at)
    WHERE expires_at IS NOT NULL;

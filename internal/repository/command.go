package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/jackc/pgx/v5"
)

type CreateCommandParams struct {
	ID             string
	HostID         string
	CommandType    string
	Payload        map[string]any
	MaxAttempts    int
	IdempotencyKey string
}

type CommandRecord struct {
	ID             string
	HostID         string
	CommandType    string
	Payload        json.RawMessage
	Status         domain.CommandStatus
	LeaseToken     *string
	LeaseExpiresAt *time.Time
	Attempts       int
	MaxAttempts    int
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Result         json.RawMessage
	ErrorCode      *string
	CompletedAt    *time.Time
}

func (CommandRepository) Complete(
	ctx context.Context,
	querier database.Querier,
	id, leaseToken string,
	attempt int,
	status domain.CommandStatus,
	result map[string]any,
	errorCode *string,
	retryable bool,
) (CommandRecord, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return CommandRecord{}, err
	}
	record, err := scanCommand(querier.QueryRow(ctx, `UPDATE device_host_commands SET
		status=CASE WHEN $7 AND $4::varchar='failed' AND attempts < max_attempts THEN 'pending' ELSE $4::varchar END,
		lease_token=NULL,lease_expires_at=NULL,result=$5,error_code=$6,
		completed_at=CASE WHEN $7 AND $4::varchar='failed' AND attempts < max_attempts THEN NULL ELSE clock_timestamp() END,
		updated_at=clock_timestamp()
		WHERE id=$1 AND status='leased' AND lease_token=$2 AND attempts=$3
		  AND lease_expires_at >= clock_timestamp()
        RETURNING id,host_id,command_type,payload,status,lease_token,lease_expires_at,
                  attempts,max_attempts,idempotency_key,created_at,updated_at,result,error_code,completed_at`,
		id, leaseToken, attempt, status, encoded, errorCode, retryable))
	if errors.Is(err, pgx.ErrNoRows) {
		return CommandRecord{}, ErrLeaseConflict
	}
	if err != nil {
		return CommandRecord{}, fmt.Errorf("complete host command: %w", err)
	}
	return record, nil
}

// RecoverExpiredLeases returns retryable commands to pending and closes a
// command as timed_out after its final allowed attempt. The database clock is
// authoritative and SKIP LOCKED allows multiple server instances.
func (CommandRepository) RecoverExpiredLease(ctx context.Context, querier database.Querier) (CommandRecord, error) {
	record, err := scanCommand(querier.QueryRow(ctx, `WITH candidate AS (
        SELECT id FROM device_host_commands
        WHERE status='leased' AND lease_expires_at < clock_timestamp()
        ORDER BY lease_expires_at,id FOR UPDATE SKIP LOCKED LIMIT 1
    )
    UPDATE device_host_commands c SET
        status=CASE WHEN c.attempts < c.max_attempts THEN 'pending' ELSE 'timed_out' END,
        lease_token=NULL,lease_expires_at=NULL,
        error_code=CASE WHEN c.attempts < c.max_attempts THEN NULL ELSE 'COMMAND_ATTEMPTS_EXHAUSTED' END,
        completed_at=CASE WHEN c.attempts < c.max_attempts THEN NULL ELSE clock_timestamp() END,
        updated_at=clock_timestamp()
    FROM candidate WHERE c.id=candidate.id
    RETURNING c.id,c.host_id,c.command_type,c.payload,c.status,c.lease_token,c.lease_expires_at,
              c.attempts,c.max_attempts,c.idempotency_key,c.created_at,c.updated_at,c.result,c.error_code,c.completed_at`))
	if errors.Is(err, pgx.ErrNoRows) {
		return CommandRecord{}, ErrNotFound
	}
	if err != nil {
		return CommandRecord{}, fmt.Errorf("recover expired command lease: %w", err)
	}
	return record, nil
}

func (CommandRepository) Get(ctx context.Context, querier database.Querier, id string) (CommandRecord, error) {
	record, err := scanCommand(querier.QueryRow(ctx, `SELECT
        id,host_id,command_type,payload,status,lease_token,lease_expires_at,
        attempts,max_attempts,idempotency_key,created_at,updated_at,result,error_code,completed_at
        FROM device_host_commands WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return CommandRecord{}, ErrNotFound
	}
	return record, err
}

type CommandRepository struct{}

func (CommandRepository) Create(ctx context.Context, querier database.Querier, params CreateCommandParams) (CommandRecord, error) {
	payload, err := json.Marshal(params.Payload)
	if err != nil {
		return CommandRecord{}, fmt.Errorf("encode command payload: %w", err)
	}
	record, err := scanCommand(querier.QueryRow(ctx, `
        INSERT INTO device_host_commands (
            id, host_id, command_type, payload, max_attempts, idempotency_key
        ) VALUES ($1, $2, $3, $4, $5, $6)
        ON CONFLICT (host_id, idempotency_key) DO UPDATE
        SET idempotency_key = EXCLUDED.idempotency_key
		RETURNING id, host_id, command_type, payload, status, lease_token,
		          lease_expires_at, attempts, max_attempts, idempotency_key,
		          created_at, updated_at,result,error_code,completed_at`,
		params.ID, params.HostID, params.CommandType, payload, params.MaxAttempts, params.IdempotencyKey,
	))
	if err != nil {
		return CommandRecord{}, fmt.Errorf("create host command: %w", err)
	}
	if record.HostID != params.HostID || record.CommandType != params.CommandType ||
		record.MaxAttempts != params.MaxAttempts || record.IdempotencyKey != params.IdempotencyKey ||
		!jsonEquivalent(record.Payload, payload) {
		return CommandRecord{}, ErrIdempotencyConflict
	}
	return record, nil
}

func (CommandRepository) ClaimNext(
	ctx context.Context,
	querier database.Querier,
	hostID string,
	leaseToken string,
	leaseDuration time.Duration,
) (CommandRecord, error) {
	record, err := scanCommand(querier.QueryRow(ctx, `
        WITH candidate AS (
            SELECT id
            FROM device_host_commands
            WHERE host_id = $1 AND status = 'pending' AND attempts < max_attempts
            ORDER BY created_at, id
            FOR UPDATE SKIP LOCKED
            LIMIT 1
        )
        UPDATE device_host_commands AS command
        SET status = 'leased',
            lease_token = $2,
            lease_expires_at = clock_timestamp() + ($3 * interval '1 millisecond'),
            attempts = attempts + 1,
            updated_at = clock_timestamp()
        FROM candidate
        WHERE command.id = candidate.id
		RETURNING command.id, command.host_id, command.command_type, command.payload,
		          command.status, command.lease_token, command.lease_expires_at,
		          command.attempts, command.max_attempts, command.idempotency_key,
		          command.created_at, command.updated_at,command.result,command.error_code,command.completed_at`,
		hostID, leaseToken, leaseDuration.Milliseconds(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return CommandRecord{}, ErrNotFound
	}
	if err != nil {
		return CommandRecord{}, fmt.Errorf("claim next host command: %w", err)
	}
	return record, nil
}

func scanCommand(row pgx.Row) (CommandRecord, error) {
	var record CommandRecord
	err := row.Scan(
		&record.ID, &record.HostID, &record.CommandType, &record.Payload,
		&record.Status, &record.LeaseToken, &record.LeaseExpiresAt,
		&record.Attempts, &record.MaxAttempts, &record.IdempotencyKey,
		&record.CreatedAt, &record.UpdatedAt, &record.Result, &record.ErrorCode, &record.CompletedAt,
	)
	return record, err
}

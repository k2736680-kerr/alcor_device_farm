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
                  created_at, updated_at`,
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
                  command.created_at, command.updated_at`,
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
		&record.CreatedAt, &record.UpdatedAt,
	)
	return record, err
}

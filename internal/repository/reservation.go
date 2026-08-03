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

type CreateReservationParams struct {
	ID                    string
	ClientID              string
	PoolID                string
	OwnerType             string
	OwnerID               string
	RequestedCapabilities map[string]any
	LeaseSeconds          int
	IdempotencyKey        string
}

type ReservationRecord struct {
	ID                    string
	ClientID              string
	PoolID                string
	DeviceID              *string
	OwnerType             string
	OwnerID               string
	RequestedCapabilities json.RawMessage
	LeaseSeconds          int
	Status                domain.ReservationStatus
	IdempotencyKey        string
	StartsAt              *time.Time
	ExpiresAt             *time.Time
	ReleasedAt            *time.Time
	FailureCode           *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type ReservationRepository struct{}

func (ReservationRepository) CreatePending(ctx context.Context, querier database.Querier, params CreateReservationParams) (ReservationRecord, error) {
	capabilities, err := json.Marshal(params.RequestedCapabilities)
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("encode requested capabilities: %w", err)
	}
	record, err := scanReservation(querier.QueryRow(ctx, `
        INSERT INTO device_reservations (
            id, client_id, pool_id, owner_type, owner_id, requested_capabilities,
            lease_seconds, status, idempotency_key
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', $8)
        ON CONFLICT (client_id, idempotency_key) DO UPDATE
        SET idempotency_key = EXCLUDED.idempotency_key
        RETURNING id, client_id, pool_id, device_id, owner_type, owner_id,
                  requested_capabilities, lease_seconds, status, idempotency_key,
                  starts_at, expires_at, released_at, failure_code, created_at, updated_at`,
		params.ID, params.ClientID, params.PoolID, params.OwnerType, params.OwnerID,
		capabilities, params.LeaseSeconds, params.IdempotencyKey,
	))
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("create pending reservation: %w", err)
	}
	if !sameReservationRequest(record, params, capabilities) {
		return ReservationRecord{}, ErrIdempotencyConflict
	}
	return record, nil
}

func (ReservationRepository) LockNextPending(ctx context.Context, tx pgx.Tx) (ReservationRecord, error) {
	record, err := scanReservation(tx.QueryRow(ctx, `
        SELECT id, client_id, pool_id, device_id, owner_type, owner_id,
               requested_capabilities, lease_seconds, status, idempotency_key,
               starts_at, expires_at, released_at, failure_code, created_at, updated_at
        FROM device_reservations
        WHERE status = 'pending'
        ORDER BY created_at, id
        FOR UPDATE SKIP LOCKED
        LIMIT 1`))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("lock next pending reservation: %w", err)
	}
	return record, nil
}

func (ReservationRepository) MarkFailed(ctx context.Context, querier database.Querier, id, failureCode string) error {
	result, err := querier.Exec(ctx, `
        UPDATE device_reservations
        SET status = 'failed', failure_code = $2, updated_at = clock_timestamp()
        WHERE id = $1 AND status = 'pending'`, id, failureCode)
	if err != nil {
		return fmt.Errorf("mark reservation failed: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func scanReservation(row pgx.Row) (ReservationRecord, error) {
	var record ReservationRecord
	err := row.Scan(
		&record.ID, &record.ClientID, &record.PoolID, &record.DeviceID,
		&record.OwnerType, &record.OwnerID, &record.RequestedCapabilities,
		&record.LeaseSeconds, &record.Status, &record.IdempotencyKey,
		&record.StartsAt, &record.ExpiresAt, &record.ReleasedAt,
		&record.FailureCode, &record.CreatedAt, &record.UpdatedAt,
	)
	return record, err
}

func sameReservationRequest(record ReservationRecord, params CreateReservationParams, encodedCapabilities []byte) bool {
	if record.ClientID != params.ClientID || record.PoolID != params.PoolID ||
		record.OwnerType != params.OwnerType || record.OwnerID != params.OwnerID ||
		record.LeaseSeconds != params.LeaseSeconds || record.IdempotencyKey != params.IdempotencyKey {
		return false
	}
	return jsonEquivalent(record.RequestedCapabilities, encodedCapabilities)
}

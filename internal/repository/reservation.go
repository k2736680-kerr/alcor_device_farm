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

type ReservationFilter struct {
	OwnerType string
	OwnerID   string
}

type PoolPolicy struct {
	Status             string
	MaxLeaseSeconds    int
	MaxConcurrency     int
	ActiveReservations int
}

type DeviceAssignment struct {
	ID             string
	Lifecycle      domain.DeviceLifecycleStatus
	Health         domain.HealthStatus
	Serial         string
	ADBEndpoint    *string
	AppiumEndpoint *string
}

type SessionRecord struct {
	ID                 string               `json:"id"`
	ReservationID      string               `json:"reservation_id"`
	DeviceID           string               `json:"device_id"`
	Status             domain.SessionStatus `json:"status"`
	StartedAt          *time.Time           `json:"started_at,omitempty"`
	ConnectionMetadata json.RawMessage      `json:"connection_metadata"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
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

// LockNextAllocatablePending keeps FIFO ordering among reservations that have
// a matching device right now. An unmatched old request therefore stays
// pending without blocking a later request that the farm can satisfy.
func (ReservationRepository) LockNextAllocatablePending(ctx context.Context, tx pgx.Tx) (ReservationRecord, error) {
	record, err := scanReservation(tx.QueryRow(ctx, `
        SELECT r.id, r.client_id, r.pool_id, r.device_id, r.owner_type, r.owner_id,
               r.requested_capabilities, r.lease_seconds, r.status, r.idempotency_key,
               r.starts_at, r.expires_at, r.released_at, r.failure_code, r.created_at, r.updated_at
        FROM device_reservations r
        WHERE r.status = 'pending'
          AND EXISTS (
              SELECT 1
              FROM devices d
              JOIN device_pool_devices pd ON pd.device_id = d.id
              JOIN device_hosts h ON h.id = d.host_id
              JOIN device_pools p ON p.id = pd.pool_id
              WHERE pd.pool_id = r.pool_id AND pd.enabled AND p.status = 'active'
                AND h.status = 'online' AND NOT h.draining
                AND d.lifecycle_status = 'ready' AND d.health_status = 'healthy'
                AND d.capabilities @> r.requested_capabilities
          )
        ORDER BY r.created_at, r.id
        FOR UPDATE OF r SKIP LOCKED
        LIMIT 1`))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("lock next allocatable reservation: %w", err)
	}
	return record, nil
}

func (ReservationRepository) HasPending(ctx context.Context, querier database.Querier) (bool, error) {
	var exists bool
	if err := querier.QueryRow(ctx, `SELECT EXISTS(
        SELECT 1 FROM device_reservations WHERE status = 'pending'
    )`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check pending reservations: %w", err)
	}
	return exists, nil
}

func (ReservationRepository) Get(ctx context.Context, querier database.Querier, id string) (ReservationRecord, error) {
	record, err := scanReservation(querier.QueryRow(ctx, `
        SELECT id, client_id, pool_id, device_id, owner_type, owner_id,
               requested_capabilities, lease_seconds, status, idempotency_key,
               starts_at, expires_at, released_at, failure_code, created_at, updated_at
        FROM device_reservations WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("get reservation: %w", err)
	}
	return record, nil
}

func (ReservationRepository) List(ctx context.Context, querier database.Querier, filter ReservationFilter) ([]ReservationRecord, error) {
	rows, err := querier.Query(ctx, `
        SELECT id, client_id, pool_id, device_id, owner_type, owner_id,
               requested_capabilities, lease_seconds, status, idempotency_key,
               starts_at, expires_at, released_at, failure_code, created_at, updated_at
        FROM device_reservations
        WHERE ($1 = '' OR owner_type = $1) AND ($2 = '' OR owner_id = $2)
        ORDER BY created_at DESC, id DESC`, filter.OwnerType, filter.OwnerID)
	if err != nil {
		return nil, fmt.Errorf("list reservations: %w", err)
	}
	defer rows.Close()
	result := make([]ReservationRecord, 0)
	for rows.Next() {
		record, scanErr := scanReservation(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan reservation: %w", scanErr)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reservations: %w", err)
	}
	return result, nil
}

func (ReservationRepository) GetPoolPolicy(ctx context.Context, querier database.Querier, poolID string) (PoolPolicy, error) {
	var policy PoolPolicy
	err := querier.QueryRow(ctx, `
        SELECT status, max_lease_seconds, max_concurrency,
               (SELECT count(*) FROM device_reservations WHERE pool_id = $1 AND status = 'active')
        FROM device_pools WHERE id = $1`, poolID).Scan(
		&policy.Status, &policy.MaxLeaseSeconds, &policy.MaxConcurrency, &policy.ActiveReservations,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PoolPolicy{}, ErrNotFound
	}
	if err != nil {
		return PoolPolicy{}, fmt.Errorf("get pool policy: %w", err)
	}
	return policy, nil
}

func (ReservationRepository) LockPoolPolicy(ctx context.Context, tx pgx.Tx, poolID string) (PoolPolicy, error) {
	var policy PoolPolicy
	err := tx.QueryRow(ctx, `
		SELECT status, max_lease_seconds, max_concurrency
		FROM device_pools WHERE id = $1 FOR UPDATE`, poolID).Scan(
		&policy.Status, &policy.MaxLeaseSeconds, &policy.MaxConcurrency,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PoolPolicy{}, ErrNotFound
	}
	if err != nil {
		return PoolPolicy{}, fmt.Errorf("lock pool policy: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM device_reservations
		WHERE pool_id = $1 AND status = 'active'`, poolID).Scan(&policy.ActiveReservations); err != nil {
		return PoolPolicy{}, fmt.Errorf("count active pool reservations: %w", err)
	}
	return policy, nil
}

func (ReservationRepository) LockMatchingDevice(ctx context.Context, tx pgx.Tx, poolID string, capabilities json.RawMessage) (DeviceAssignment, error) {
	if len(capabilities) == 0 {
		capabilities = json.RawMessage(`{}`)
	}
	var device DeviceAssignment
	err := tx.QueryRow(ctx, `
        SELECT d.id, d.lifecycle_status, d.health_status, d.serial,
               d.adb_endpoint, d.appium_endpoint
        FROM devices d
        JOIN device_pool_devices pd ON pd.device_id = d.id
        JOIN device_hosts h ON h.id = d.host_id
        WHERE pd.pool_id = $1 AND pd.enabled
          AND h.status = 'online' AND NOT h.draining
          AND d.lifecycle_status = 'ready' AND d.health_status = 'healthy'
          AND d.capabilities @> $2::jsonb
        ORDER BY d.created_at, d.id
        FOR UPDATE OF d SKIP LOCKED
        LIMIT 1`, poolID, capabilities).Scan(
		&device.ID, &device.Lifecycle, &device.Health, &device.Serial,
		&device.ADBEndpoint, &device.AppiumEndpoint,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeviceAssignment{}, ErrCapacityUnavailable
	}
	if err != nil {
		return DeviceAssignment{}, fmt.Errorf("lock matching device: %w", err)
	}
	return device, nil
}

func (ReservationRepository) Activate(
	ctx context.Context,
	tx pgx.Tx,
	reservation ReservationRecord,
	device DeviceAssignment,
	sessionID string,
) (ReservationRecord, SessionRecord, error) {
	now, err := database.ClockNow(ctx, tx)
	if err != nil {
		return ReservationRecord{}, SessionRecord{}, err
	}
	if _, err := tx.Exec(ctx, `
        UPDATE devices SET lifecycle_status = 'reserved', updated_at = $2
        WHERE id = $1 AND lifecycle_status = 'ready'`, device.ID, now); err != nil {
		return ReservationRecord{}, SessionRecord{}, fmt.Errorf("reserve device: %w", err)
	}
	result, err := tx.Exec(ctx, `
        UPDATE devices SET lifecycle_status = 'busy', updated_at = $2
        WHERE id = $1 AND lifecycle_status = 'reserved'`, device.ID, now)
	if err != nil {
		return ReservationRecord{}, SessionRecord{}, fmt.Errorf("mark device busy: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ReservationRecord{}, SessionRecord{}, ErrCapacityUnavailable
	}

	active, err := scanReservation(tx.QueryRow(ctx, `
        UPDATE device_reservations
		SET device_id = $2, status = 'active', starts_at = $3::timestamptz,
		    expires_at = $3::timestamptz + make_interval(secs => lease_seconds), updated_at = $3::timestamptz
        WHERE id = $1 AND status = 'pending'
        RETURNING id, client_id, pool_id, device_id, owner_type, owner_id,
                  requested_capabilities, lease_seconds, status, idempotency_key,
                  starts_at, expires_at, released_at, failure_code, created_at, updated_at`,
		reservation.ID, device.ID, now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, SessionRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, SessionRecord{}, fmt.Errorf("activate reservation: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"serial": device.Serial, "adb_endpoint": device.ADBEndpoint, "appium_endpoint": device.AppiumEndpoint,
	})
	if err != nil {
		return ReservationRecord{}, SessionRecord{}, fmt.Errorf("encode session metadata: %w", err)
	}
	var session SessionRecord
	err = tx.QueryRow(ctx, `
        INSERT INTO device_sessions (
            id, reservation_id, device_id, status, started_at, connection_metadata, created_at, updated_at
        ) VALUES ($1, $2, $3, 'active', $4, $5, $4, $4)
        RETURNING id, reservation_id, device_id, status, started_at,
                  connection_metadata, created_at, updated_at`,
		sessionID, reservation.ID, device.ID, now, metadata,
	).Scan(&session.ID, &session.ReservationID, &session.DeviceID, &session.Status,
		&session.StartedAt, &session.ConnectionMetadata, &session.CreatedAt, &session.UpdatedAt)
	if err != nil {
		return ReservationRecord{}, SessionRecord{}, fmt.Errorf("create device session: %w", err)
	}
	return active, session, nil
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

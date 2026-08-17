package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
	"github.com/Ad-Quanta/alcor-device-farm/internal/sensitive"
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
	Platform           string
	MaxLeaseSeconds    int
	MaxConcurrency     int
	ActiveReservations int
}

type DeviceAssignment struct {
	ID             string
	HostID         string
	Platform       string
	Lifecycle      domain.DeviceLifecycleStatus
	Health         domain.HealthStatus
	Serial         string
	ADBEndpoint    *string
	AppiumEndpoint *string
	AppiumUDID     string
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

type ExpiredRemoteSession struct {
	ID            string
	ReservationID string
	DeviceID      string
	Serial        string
	ExpiresAt     time.Time
}

type OperationParams struct {
	ClientID       string
	Scope          string
	Key            string
	RequestHash    string
	ResourceType   string
	ResourceID     string
	ResponseStatus int
}

type ReservationRepository struct{}

const TargetDeviceCapability = "_device_farm_target_device_id"

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
        WHERE r.status = 'pending' AND r.device_id IS NULL
          AND (r.failure_code IS NULL OR r.updated_at <= clock_timestamp() - interval '5 seconds')
          AND EXISTS (
              SELECT 1
              FROM devices d
              JOIN device_pool_devices pd ON pd.device_id = d.id
              JOIN device_hosts h ON h.id = d.host_id
              JOIN device_pools p ON p.id = pd.pool_id
              WHERE pd.pool_id = r.pool_id AND pd.enabled AND p.status = 'active'
				AND p.platform = d.platform
                AND h.status = 'online' AND NOT h.draining
                AND d.lifecycle_status = 'ready' AND d.health_status = 'healthy'
                AND (NOT r.requested_capabilities ? '`+TargetDeviceCapability+`'
                     OR d.id = r.requested_capabilities->>'`+TargetDeviceCapability+`')
				AND (NOT r.requested_capabilities ? 'platformName'
				     OR lower(r.requested_capabilities->>'platformName') = p.platform)
				AND d.capabilities @> device_schedulable_capabilities(r.requested_capabilities)
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

func (ReservationRepository) ReserveForClaim(
	ctx context.Context,
	tx pgx.Tx,
	reservationID, deviceID string,
	reservedAt time.Time,
) error {
	result, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status='reserved',updated_at=$2
		WHERE id=$1 AND lifecycle_status='ready' AND health_status='healthy'`, deviceID, reservedAt)
	if err != nil {
		return fmt.Errorf("reserve device for STF claim: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrCapacityUnavailable
	}
	result, err = tx.Exec(ctx, `UPDATE device_reservations SET device_id=$2,failure_code=NULL,updated_at=$3
		WHERE id=$1 AND status='pending' AND device_id IS NULL`, reservationID, deviceID, reservedAt)
	if err != nil {
		return fmt.Errorf("mark reservation STF claim in progress: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (ReservationRepository) CompensateClaim(
	ctx context.Context,
	tx pgx.Tx,
	reservationID, deviceID, failureCode string,
	terminal bool,
	compensatedAt time.Time,
) error {
	result, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status='recycling',updated_at=$2
		WHERE id=$1 AND lifecycle_status='reserved'`, deviceID, compensatedAt)
	if err != nil {
		return fmt.Errorf("recycle device after STF claim failure: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	result, err = tx.Exec(ctx, `UPDATE devices SET lifecycle_status='ready',updated_at=$2
		WHERE id=$1 AND lifecycle_status='recycling' AND health_status='healthy'`, deviceID, compensatedAt)
	if err != nil {
		return fmt.Errorf("restore device after STF claim failure: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	status := domain.ReservationPending
	if terminal {
		status = domain.ReservationFailed
	}
	result, err = tx.Exec(ctx, `UPDATE device_reservations
		SET device_id=NULL,status=$3,failure_code=$4,updated_at=$5
		WHERE id=$1 AND status='pending' AND device_id=$2`, reservationID, deviceID, status, failureCode, compensatedAt)
	if err != nil {
		return fmt.Errorf("compensate reservation STF claim failure: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
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

func (ReservationRepository) LockByID(ctx context.Context, tx pgx.Tx, id string) (ReservationRecord, error) {
	record, err := scanReservation(tx.QueryRow(ctx, `
        SELECT id, client_id, pool_id, device_id, owner_type, owner_id,
               requested_capabilities, lease_seconds, status, idempotency_key,
               starts_at, expires_at, released_at, failure_code, created_at, updated_at
        FROM device_reservations WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("lock reservation: %w", err)
	}
	return record, nil
}

func (ReservationRepository) LockNextExpired(ctx context.Context, tx pgx.Tx, graceSeconds int) (ReservationRecord, error) {
	record, err := scanReservation(tx.QueryRow(ctx, `
        SELECT id, client_id, pool_id, device_id, owner_type, owner_id,
               requested_capabilities, lease_seconds, status, idempotency_key,
               starts_at, expires_at, released_at, failure_code, created_at, updated_at
        FROM device_reservations
        WHERE status = 'active'
          AND expires_at + make_interval(secs => $1) <= clock_timestamp()
        ORDER BY expires_at, id
        FOR UPDATE SKIP LOCKED
        LIMIT 1`, graceSeconds))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("lock expired reservation: %w", err)
	}
	return record, nil
}

func (ReservationRepository) FindNextExpired(ctx context.Context, querier database.Querier, graceSeconds int) (ReservationRecord, error) {
	record, err := scanReservation(querier.QueryRow(ctx, `
        SELECT id, client_id, pool_id, device_id, owner_type, owner_id,
               requested_capabilities, lease_seconds, status, idempotency_key,
               starts_at, expires_at, released_at, failure_code, created_at, updated_at
        FROM device_reservations
        WHERE status = 'active'
          AND expires_at + make_interval(secs => $1) <= clock_timestamp()
        ORDER BY expires_at, id
        LIMIT 1`, graceSeconds))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("find expired reservation: %w", err)
	}
	return record, nil
}

func (ReservationRepository) BeginOperation(ctx context.Context, tx pgx.Tx, params OperationParams) (bool, error) {
	command, err := tx.Exec(ctx, `
        INSERT INTO device_idempotency_records
            (client_id,scope,idempotency_key,request_hash,resource_type,resource_id,response_status)
        VALUES($1,$2,$3,$4,$5,$6,$7)
        ON CONFLICT(client_id,scope,idempotency_key) DO NOTHING`,
		params.ClientID, params.Scope, params.Key, params.RequestHash,
		params.ResourceType, params.ResourceID, params.ResponseStatus)
	if err != nil {
		return false, fmt.Errorf("record reservation operation: %w", err)
	}
	if command.RowsAffected() == 1 {
		return false, nil
	}
	var requestHash, resourceType, resourceID string
	if err := tx.QueryRow(ctx, `
        SELECT request_hash,resource_type,resource_id
        FROM device_idempotency_records
        WHERE client_id=$1 AND scope=$2 AND idempotency_key=$3`,
		params.ClientID, params.Scope, params.Key,
	).Scan(&requestHash, &resourceType, &resourceID); err != nil {
		return false, fmt.Errorf("load reservation operation: %w", err)
	}
	if requestHash != params.RequestHash || resourceType != params.ResourceType || resourceID != params.ResourceID {
		return false, ErrIdempotencyConflict
	}
	return true, nil
}

func (ReservationRepository) CheckOperation(ctx context.Context, querier database.Querier, params OperationParams) (bool, error) {
	var requestHash, resourceType, resourceID string
	err := querier.QueryRow(ctx, `
		SELECT request_hash,resource_type,resource_id
		FROM device_idempotency_records
		WHERE client_id=$1 AND scope=$2 AND idempotency_key=$3`,
		params.ClientID, params.Scope, params.Key,
	).Scan(&requestHash, &resourceType, &resourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check reservation operation: %w", err)
	}
	if requestHash != params.RequestHash || resourceType != params.ResourceType || resourceID != params.ResourceID {
		return false, ErrIdempotencyConflict
	}
	return true, nil
}

func (ReservationRepository) Extend(ctx context.Context, tx pgx.Tx, id string, expiresAt time.Time) (ReservationRecord, error) {
	record, err := scanReservation(tx.QueryRow(ctx, `
        UPDATE device_reservations
        SET expires_at=$2::timestamptz, updated_at=clock_timestamp()
        WHERE id=$1 AND status='active'
        RETURNING id, client_id, pool_id, device_id, owner_type, owner_id,
                  requested_capabilities, lease_seconds, status, idempotency_key,
                  starts_at, expires_at, released_at, failure_code, created_at, updated_at`, id, expiresAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("extend reservation: %w", err)
	}
	return record, nil
}

func (ReservationRepository) CancelPending(
	ctx context.Context,
	tx pgx.Tx,
	id, failureCode string,
	canceledAt time.Time,
) (ReservationRecord, error) {
	record, err := scanReservation(tx.QueryRow(ctx, `
		UPDATE device_reservations
		SET status='failed',failure_code=$2,updated_at=$3::timestamptz
		WHERE id=$1 AND status='pending' AND device_id IS NULL
		RETURNING id,client_id,pool_id,device_id,owner_type,owner_id,
		          requested_capabilities,lease_seconds,status,idempotency_key,
		          starts_at,expires_at,released_at,failure_code,created_at,updated_at`,
		id, failureCode, canceledAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("cancel pending reservation: %w", err)
	}
	return record, nil
}

func (ReservationRepository) LockSession(ctx context.Context, tx pgx.Tx, reservationID string) (SessionRecord, error) {
	var session SessionRecord
	err := tx.QueryRow(ctx, `
        SELECT id,reservation_id,device_id,status,started_at,connection_metadata,created_at,updated_at
        FROM device_sessions WHERE reservation_id=$1 FOR UPDATE`, reservationID).Scan(
		&session.ID, &session.ReservationID, &session.DeviceID, &session.Status,
		&session.StartedAt, &session.ConnectionMetadata, &session.CreatedAt, &session.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, ErrNotFound
	}
	if err != nil {
		return SessionRecord{}, fmt.Errorf("lock device session: %w", err)
	}
	return session, nil
}

func (ReservationRepository) GetSession(ctx context.Context, querier database.Querier, reservationID string) (SessionRecord, error) {
	var session SessionRecord
	err := querier.QueryRow(ctx, `
        SELECT id,reservation_id,device_id,status,started_at,connection_metadata,created_at,updated_at
        FROM device_sessions WHERE reservation_id=$1`, reservationID).Scan(
		&session.ID, &session.ReservationID, &session.DeviceID, &session.Status,
		&session.StartedAt, &session.ConnectionMetadata, &session.CreatedAt, &session.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, ErrNotFound
	}
	if err != nil {
		return SessionRecord{}, fmt.Errorf("get device session: %w", err)
	}
	return session, nil
}

func (ReservationRepository) LockDevice(ctx context.Context, tx pgx.Tx, id string) (DeviceAssignment, error) {
	var device DeviceAssignment
	err := tx.QueryRow(ctx, `
		SELECT id,host_id,platform,lifecycle_status,health_status,serial,adb_endpoint,appium_endpoint,
		       COALESCE(capabilities->>'appiumUdid',serial)
		FROM devices WHERE id=$1 FOR UPDATE`, id).Scan(
		&device.ID, &device.HostID, &device.Platform, &device.Lifecycle, &device.Health, &device.Serial,
		&device.ADBEndpoint, &device.AppiumEndpoint, &device.AppiumUDID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeviceAssignment{}, ErrNotFound
	}
	if err != nil {
		return DeviceAssignment{}, fmt.Errorf("lock reserved device: %w", err)
	}
	return device, nil
}

func (ReservationRepository) GetDevice(ctx context.Context, querier database.Querier, id string) (DeviceAssignment, error) {
	var device DeviceAssignment
	err := querier.QueryRow(ctx, `
		SELECT id,host_id,platform,lifecycle_status,health_status,serial,adb_endpoint,appium_endpoint,
		       COALESCE(capabilities->>'appiumUdid',serial)
		FROM devices WHERE id=$1`, id).Scan(
		&device.ID, &device.HostID, &device.Platform, &device.Lifecycle, &device.Health, &device.Serial,
		&device.ADBEndpoint, &device.AppiumEndpoint, &device.AppiumUDID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeviceAssignment{}, ErrNotFound
	}
	if err != nil {
		return DeviceAssignment{}, fmt.Errorf("get reserved device: %w", err)
	}
	return device, nil
}

func (ReservationRepository) SetRemoteSession(
	ctx context.Context,
	tx pgx.Tx,
	reservationID string,
	metadata json.RawMessage,
) (SessionRecord, error) {
	var session SessionRecord
	err := tx.QueryRow(ctx, `
		UPDATE device_sessions
		SET connection_metadata=jsonb_set(connection_metadata,'{stf_remote_session}',$2::jsonb,true),
		    updated_at=clock_timestamp()
		WHERE reservation_id=$1 AND status='active'
		RETURNING id,reservation_id,device_id,status,started_at,connection_metadata,created_at,updated_at`,
		reservationID, metadata,
	).Scan(&session.ID, &session.ReservationID, &session.DeviceID, &session.Status,
		&session.StartedAt, &session.ConnectionMetadata, &session.CreatedAt, &session.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, ErrNotFound
	}
	if err != nil {
		return SessionRecord{}, fmt.Errorf("store STF remote session: %w", err)
	}
	return session, nil
}

func (ReservationRepository) FindNextExpiredRemoteSession(ctx context.Context, querier database.Querier) (ExpiredRemoteSession, error) {
	var remote ExpiredRemoteSession
	err := querier.QueryRow(ctx, `
		SELECT s.connection_metadata->'stf_remote_session'->>'id',s.reservation_id,s.device_id,d.serial,
		       (s.connection_metadata->'stf_remote_session'->>'expires_at')::timestamptz
		FROM device_sessions s
		JOIN device_reservations r ON r.id=s.reservation_id
		JOIN devices d ON d.id=s.device_id
		WHERE s.status='active' AND r.status='active'
		  AND s.connection_metadata ? 'stf_remote_session'
		  AND (s.connection_metadata->'stf_remote_session'->>'expires_at')::timestamptz <= clock_timestamp()
		ORDER BY (s.connection_metadata->'stf_remote_session'->>'expires_at')::timestamptz,s.id
		LIMIT 1`).Scan(&remote.ID, &remote.ReservationID, &remote.DeviceID, &remote.Serial, &remote.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExpiredRemoteSession{}, ErrNotFound
	}
	if err != nil {
		return ExpiredRemoteSession{}, fmt.Errorf("find expired STF remote session: %w", err)
	}
	return remote, nil
}

func (ReservationRepository) ClearRemoteSession(ctx context.Context, tx pgx.Tx, reservationID, remoteID string) error {
	result, err := tx.Exec(ctx, `
		UPDATE device_sessions
		SET connection_metadata=connection_metadata-'stf_remote_session',updated_at=clock_timestamp()
		WHERE reservation_id=$1 AND status='active'
		  AND connection_metadata->'stf_remote_session'->>'id'=$2`, reservationID, remoteID)
	if err != nil {
		return fmt.Errorf("clear STF remote session: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (ReservationRepository) CloseActive(
	ctx context.Context,
	tx pgx.Tx,
	reservationID, deviceID string,
	terminal domain.ReservationStatus,
	closedAt time.Time,
) (ReservationRecord, error) {
	if _, err := tx.Exec(ctx, `
        UPDATE device_sessions SET status='closing',updated_at=$2::timestamptz
        WHERE reservation_id=$1 AND status='active'`, reservationID, closedAt); err != nil {
		return ReservationRecord{}, fmt.Errorf("close device session: %w", err)
	}
	if _, err := tx.Exec(ctx, `
        UPDATE device_sessions SET status='closed',ended_at=$2::timestamptz,updated_at=$2::timestamptz
        WHERE reservation_id=$1 AND status='closing'`, reservationID, closedAt); err != nil {
		return ReservationRecord{}, fmt.Errorf("finish device session: %w", err)
	}
	result, err := tx.Exec(ctx, `
		UPDATE devices SET
			-- DF-038: reservations only release access. They must not erase the
			-- administrator-managed emulator data or queue a factory rebuild.
			lifecycle_status=CASE WHEN lifecycle_status='quarantined' THEN lifecycle_status ELSE 'ready' END,
			updated_at=$2::timestamptz
		WHERE id=$1 AND lifecycle_status IN ('busy','reserved','quarantined')`, deviceID, closedAt)
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("release retained device: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ReservationRecord{}, ErrNotFound
	}
	record, err := scanReservation(tx.QueryRow(ctx, `
        UPDATE device_reservations
        SET status=$2,released_at=$3::timestamptz,updated_at=$3::timestamptz
        WHERE id=$1 AND status='active'
        RETURNING id, client_id, pool_id, device_id, owner_type, owner_id,
                  requested_capabilities, lease_seconds, status, idempotency_key,
                  starts_at, expires_at, released_at, failure_code, created_at, updated_at`,
		reservationID, terminal, closedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("close reservation: %w", err)
	}
	return record, nil
}

func (ReservationRepository) InsertAudit(
	ctx context.Context,
	tx pgx.Tx,
	id, actorType, actorID, action, resourceID, requestID, reason string,
) error {
	reason = sensitive.RedactText(reason)
	_, err := tx.Exec(ctx, `
        INSERT INTO device_audit_events
            (id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
        VALUES($1,$2,$3,$4,'device_reservation',$5,$6,$7,'{}'::jsonb)`,
		id, actorType, actorID, action, resourceID, requestID, reason)
	if err != nil {
		return fmt.Errorf("insert reservation audit event: %w", err)
	}
	return nil
}

// List returns one page of reservations plus the number of rows the filter
// matches. The window is applied by the database so a caller listing page 1
// never reads the rest of the table.
func (ReservationRepository) List(
	ctx context.Context,
	querier database.Querier,
	filter ReservationFilter,
	page paging.Page,
) ([]ReservationRecord, int, error) {
	var total int
	if err := querier.QueryRow(ctx, `
        SELECT count(*) FROM device_reservations
        WHERE ($1 = '' OR owner_type = $1) AND ($2 = '' OR owner_id = $2)`,
		filter.OwnerType, filter.OwnerID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count reservations: %w", err)
	}
	rows, err := querier.Query(ctx, `
        SELECT id, client_id, pool_id, device_id, owner_type, owner_id,
               requested_capabilities, lease_seconds, status, idempotency_key,
               starts_at, expires_at, released_at, failure_code, created_at, updated_at
        FROM device_reservations
        WHERE ($1 = '' OR owner_type = $1) AND ($2 = '' OR owner_id = $2)
        ORDER BY created_at DESC, id DESC
        LIMIT $3 OFFSET $4`, filter.OwnerType, filter.OwnerID, page.Limit(), page.Offset())
	if err != nil {
		return nil, 0, fmt.Errorf("list reservations: %w", err)
	}
	defer rows.Close()
	result := make([]ReservationRecord, 0)
	for rows.Next() {
		record, scanErr := scanReservation(rows)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("scan reservation: %w", scanErr)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate reservations: %w", err)
	}
	return result, total, nil
}

func (ReservationRepository) GetPoolPolicy(ctx context.Context, querier database.Querier, poolID string) (PoolPolicy, error) {
	var policy PoolPolicy
	err := querier.QueryRow(ctx, `
		SELECT status, platform, max_lease_seconds, max_concurrency,
               (SELECT count(*) FROM device_reservations WHERE pool_id = $1 AND status = 'active')
        FROM device_pools WHERE id = $1`, poolID).Scan(
		&policy.Status, &policy.Platform, &policy.MaxLeaseSeconds, &policy.MaxConcurrency, &policy.ActiveReservations,
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
		SELECT status, platform, max_lease_seconds, max_concurrency
		FROM device_pools WHERE id = $1 FOR UPDATE`, poolID).Scan(
		&policy.Status, &policy.Platform, &policy.MaxLeaseSeconds, &policy.MaxConcurrency,
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
		SELECT d.id, d.host_id, d.platform, d.lifecycle_status, d.health_status, d.serial,
		       d.adb_endpoint, d.appium_endpoint, COALESCE(d.capabilities->>'appiumUdid',d.serial)
        FROM devices d
        JOIN device_pool_devices pd ON pd.device_id = d.id
        JOIN device_hosts h ON h.id = d.host_id
        JOIN device_pools p ON p.id = pd.pool_id
        WHERE pd.pool_id = $1 AND pd.enabled
          AND p.platform = d.platform
          AND h.status = 'online' AND NOT h.draining
          AND d.lifecycle_status = 'ready' AND d.health_status = 'healthy'
          AND (NOT $2::jsonb ? '`+TargetDeviceCapability+`'
               OR d.id = $2::jsonb->>'`+TargetDeviceCapability+`')
		  AND (NOT $2::jsonb ? 'platformName' OR lower($2::jsonb->>'platformName') = p.platform)
		  AND d.capabilities @> device_schedulable_capabilities($2::jsonb)
        ORDER BY d.created_at, d.id
        FOR UPDATE OF d SKIP LOCKED
        LIMIT 1`, poolID, capabilities).Scan(
		&device.ID, &device.HostID, &device.Platform, &device.Lifecycle, &device.Health, &device.Serial,
		&device.ADBEndpoint, &device.AppiumEndpoint, &device.AppiumUDID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeviceAssignment{}, ErrCapacityUnavailable
	}
	if err != nil {
		return DeviceAssignment{}, fmt.Errorf("lock matching device: %w", err)
	}
	return device, nil
}

func (ReservationRepository) FindActivePoolForDevice(ctx context.Context, querier database.Querier, deviceID string) (string, error) {
	var poolID string
	err := querier.QueryRow(ctx, `
		SELECT p.id
		FROM devices d
		JOIN device_hosts h ON h.id=d.host_id
		JOIN device_pool_devices pd ON pd.device_id=d.id AND pd.enabled
		JOIN device_pools p ON p.id=pd.pool_id AND p.status='active'
		WHERE d.id=$1 AND d.lifecycle_status='ready' AND d.health_status='healthy'
		  AND p.platform=d.platform
		  AND h.status='online' AND NOT h.draining
		ORDER BY p.created_at,p.id
		LIMIT 1`, deviceID).Scan(&poolID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrCapacityUnavailable
	}
	if err != nil {
		return "", fmt.Errorf("find active pool for device: %w", err)
	}
	return poolID, nil
}

func (ReservationRepository) LockTargetDevice(ctx context.Context, tx pgx.Tx, deviceID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, deviceID); err != nil {
		return fmt.Errorf("lock target device reservation: %w", err)
	}
	return nil
}

func (ReservationRepository) FindOpenTargeted(
	ctx context.Context,
	querier database.Querier,
	ownerID, deviceID string,
) (ReservationRecord, error) {
	record, err := scanReservation(querier.QueryRow(ctx, `
		SELECT id,client_id,pool_id,device_id,owner_type,owner_id,
		       requested_capabilities,lease_seconds,status,idempotency_key,
		       starts_at,expires_at,released_at,failure_code,created_at,updated_at
		FROM device_reservations
		WHERE owner_type='manual' AND ($1='' OR owner_id=$1) AND status IN ('pending','active')
		  AND requested_capabilities->>'`+TargetDeviceCapability+`'=$2
		ORDER BY created_at DESC,id DESC
		LIMIT 1`, ownerID, deviceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("find open targeted reservation: %w", err)
	}
	return record, nil
}

func (ReservationRepository) KeepAliveTargeted(
	ctx context.Context,
	querier database.Querier,
	reservationID, ownerID, deviceID string,
	lease time.Duration,
) (ReservationRecord, error) {
	seconds := int64(lease / time.Second)
	record, err := scanReservation(querier.QueryRow(ctx, `
		UPDATE device_reservations r
		SET expires_at=LEAST(
			clock_timestamp()+make_interval(secs=>$4),
			clock_timestamp()+make_interval(secs=>p.max_lease_seconds)
		), updated_at=clock_timestamp()
		FROM device_pools p
		WHERE r.id=$1 AND r.pool_id=p.id AND r.status='active'
		  AND r.expires_at>clock_timestamp()
		  AND r.owner_type='manual' AND r.owner_id=$2
		  AND r.requested_capabilities->>'`+TargetDeviceCapability+`'=$3
		RETURNING r.id,r.client_id,r.pool_id,r.device_id,r.owner_type,r.owner_id,
		          r.requested_capabilities,r.lease_seconds,r.status,r.idempotency_key,
		          r.starts_at,r.expires_at,r.released_at,r.failure_code,r.created_at,r.updated_at`,
		reservationID, ownerID, deviceID, seconds))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationRecord{}, ErrNotFound
	}
	if err != nil {
		return ReservationRecord{}, fmt.Errorf("keep targeted reservation alive: %w", err)
	}
	return record, nil
}

func (ReservationRepository) ActivateClaimed(
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
		WHERE id = $1 AND status = 'pending' AND device_id=$2
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
		"host_id": device.HostID, "platform": device.Platform, "serial": device.Serial,
		"adb_endpoint": device.ADBEndpoint, "appium_endpoint": device.AppiumEndpoint,
		"appium_udid": device.AppiumUDID,
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

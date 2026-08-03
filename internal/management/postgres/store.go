package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/Ad-Quanta/alcor-device-farm/internal/sensitive"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Store struct{ db *database.DB }

func New(db *database.DB) *Store { return &Store{db: db} }

func (store *Store) CreateImage(ctx context.Context, meta management.Idempotency, image management.Image) (management.Image, error) {
	return createIdempotent(ctx, store.db, meta,
		func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO device_images
                (id,name,docker_digest,api_level,abi,resolution,resource_config,status)
                VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, image.ID, image.Name, image.DockerDigest,
				image.APILevel, image.ABI, image.Resolution, mustJSON(image.ResourceConfig), image.Status)
			return err
		},
		func(query database.Querier, id string) (management.Image, error) { return getImage(ctx, query, id) },
	)
}

func (store *Store) ListImages(ctx context.Context) ([]management.Image, error) {
	rows, err := store.db.Pool().Query(ctx, imageSelect+` ORDER BY created_at,id`)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	result := make([]management.Image, 0)
	for rows.Next() {
		value, err := scanImage(rows)
		if err != nil {
			return nil, normalize(err)
		}
		result = append(result, value)
	}
	return result, normalize(rows.Err())
}

func (store *Store) GetImage(ctx context.Context, id string) (management.Image, error) {
	return getImage(ctx, store.db.Pool(), id)
}

func (store *Store) UpdateImage(ctx context.Context, image management.Image, expected domain.ImageStatus) (management.Image, error) {
	value, err := scanImage(store.db.Pool().QueryRow(ctx, `UPDATE device_images SET
		name=$2,docker_digest=$3,api_level=$4,abi=$5,resolution=$6,resource_config=$7,status=$8,validation_error=$9,updated_at=clock_timestamp()
		WHERE id=$1 AND status=$10
		RETURNING id,name,docker_digest,api_level,abi,resolution,resource_config,status,validation_error,created_at,updated_at`,
		image.ID, image.Name, image.DockerDigest, image.APILevel, image.ABI, image.Resolution,
		mustJSON(image.ResourceConfig), image.Status, image.ValidationError, expected))
	return value, rowError(err)
}

func (store *Store) CreateHost(ctx context.Context, meta management.Idempotency, host management.Host) (management.Host, error) {
	return createIdempotent(ctx, store.db, meta,
		func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO device_hosts
                (id,name,host_type,address,capabilities,capacity,used_capacity,status,draining)
                VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9)`, host.ID, host.Name, host.HostType,
				host.Address, mustJSON(host.Capabilities), mustJSON(host.Capacity), mustJSON(host.UsedCapacity), host.Status, host.Draining)
			return err
		},
		func(query database.Querier, id string) (management.Host, error) { return getHost(ctx, query, id) },
	)
}

func (store *Store) ListHosts(ctx context.Context) ([]management.Host, error) {
	rows, err := store.db.Pool().Query(ctx, hostSelect+` ORDER BY created_at,id`)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	result := make([]management.Host, 0)
	for rows.Next() {
		value, err := scanHost(rows)
		if err != nil {
			return nil, normalize(err)
		}
		result = append(result, value)
	}
	return result, normalize(rows.Err())
}

func (store *Store) GetHost(ctx context.Context, id string) (management.Host, error) {
	return getHost(ctx, store.db.Pool(), id)
}

func (store *Store) UpdateHost(ctx context.Context, host management.Host, expected domain.HostStatus) (management.Host, error) {
	value, err := scanHost(store.db.Pool().QueryRow(ctx, `UPDATE device_hosts SET
        name=$2,host_type=$3,address=NULLIF($4,''),capabilities=$5,capacity=$6,used_capacity=$7,
        status=$8,draining=$9,updated_at=clock_timestamp()
        WHERE id=$1 AND status=$10
        RETURNING id,name,host_type,COALESCE(address,''),capabilities,capacity,used_capacity,status,draining,last_heartbeat_at,created_at,updated_at`,
		host.ID, host.Name, host.HostType, host.Address, mustJSON(host.Capabilities), mustJSON(host.Capacity),
		mustJSON(host.UsedCapacity), host.Status, host.Draining, expected))
	return value, rowError(err)
}

func (store *Store) CreatePool(ctx context.Context, meta management.Idempotency, pool management.Pool) (management.Pool, error) {
	return createIdempotent(ctx, store.db, meta,
		func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO device_pools
                (id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
                VALUES ($1,$2,$3,$4,$5,$6)`, pool.ID, pool.Name, pool.DefaultLeaseSeconds,
				pool.MaxLeaseSeconds, pool.MaxConcurrency, pool.Status)
			return err
		},
		func(query database.Querier, id string) (management.Pool, error) { return getPool(ctx, query, id) },
	)
}

func (store *Store) ListPools(ctx context.Context) ([]management.Pool, error) {
	rows, err := store.db.Pool().Query(ctx, poolSelect+` ORDER BY created_at,id`)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	result := make([]management.Pool, 0)
	for rows.Next() {
		value, err := scanPool(rows)
		if err != nil {
			return nil, normalize(err)
		}
		result = append(result, value)
	}
	return result, normalize(rows.Err())
}

func (store *Store) GetPool(ctx context.Context, id string) (management.Pool, error) {
	return getPool(ctx, store.db.Pool(), id)
}

func (store *Store) UpdatePool(ctx context.Context, pool management.Pool, expected domain.PoolStatus) (management.Pool, error) {
	value, err := scanPool(store.db.Pool().QueryRow(ctx, `UPDATE device_pools SET
        name=$2,default_lease_seconds=$3,max_lease_seconds=$4,max_concurrency=$5,status=$6,updated_at=clock_timestamp()
        WHERE id=$1 AND status=$7
        RETURNING id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status,created_at,updated_at`,
		pool.ID, pool.Name, pool.DefaultLeaseSeconds, pool.MaxLeaseSeconds, pool.MaxConcurrency, pool.Status, expected))
	return value, rowError(err)
}

func (store *Store) ListPoolImages(ctx context.Context, poolID string) ([]management.PoolImage, error) {
	rows, err := store.db.Pool().Query(ctx, poolImageSelect+` WHERE pool_id=$1 ORDER BY created_at,image_id`, poolID)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	values := make([]management.PoolImage, 0)
	for rows.Next() {
		value, err := scanPoolImage(rows)
		if err != nil {
			return nil, normalize(err)
		}
		values = append(values, value)
	}
	return values, normalize(rows.Err())
}

func (store *Store) SetPoolImage(ctx context.Context, value management.PoolImage) (management.PoolImage, error) {
	result, err := scanPoolImage(store.db.Pool().QueryRow(ctx, `INSERT INTO device_pool_images
		(pool_id,image_id,min_ready,max_instances,enabled) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(pool_id,image_id) DO UPDATE SET min_ready=EXCLUDED.min_ready,
		max_instances=EXCLUDED.max_instances,enabled=EXCLUDED.enabled,updated_at=clock_timestamp()
		RETURNING pool_id,image_id,min_ready,max_instances,enabled,created_at,updated_at`,
		value.PoolID, value.ImageID, value.MinReady, value.MaxInstances, value.Enabled))
	return result, rowError(err)
}

func (store *Store) DisablePoolImage(ctx context.Context, poolID, imageID string) (management.PoolImage, error) {
	value, err := scanPoolImage(store.db.Pool().QueryRow(ctx, `UPDATE device_pool_images SET enabled=false,updated_at=clock_timestamp()
		WHERE pool_id=$1 AND image_id=$2
		RETURNING pool_id,image_id,min_ready,max_instances,enabled,created_at,updated_at`, poolID, imageID))
	return value, rowError(err)
}

func (store *Store) AddDeviceToPool(ctx context.Context, poolID, deviceID string) error {
	_, err := store.db.Pool().Exec(ctx, `INSERT INTO device_pool_devices(pool_id,device_id,enabled)
        VALUES($1,$2,true) ON CONFLICT(pool_id,device_id) DO UPDATE SET enabled=true,updated_at=clock_timestamp()`, poolID, deviceID)
	return normalize(err)
}

func (store *Store) RemoveDeviceFromPool(ctx context.Context, poolID, deviceID string) error {
	result, err := store.db.Pool().Exec(ctx, `UPDATE device_pool_devices SET enabled=false,updated_at=clock_timestamp()
        WHERE pool_id=$1 AND device_id=$2 AND enabled=true`, poolID, deviceID)
	if err != nil {
		return normalize(err)
	}
	if result.RowsAffected() != 1 {
		return management.ErrNotFound
	}
	return nil
}

func (store *Store) CreateDevice(ctx context.Context, device management.Device) (management.Device, error) {
	value, err := scanDevice(store.db.Pool().QueryRow(ctx, `INSERT INTO devices
        (id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,stf_serial,
         adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status,health_reason,consecutive_failures)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
        RETURNING id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,stf_serial,
                  adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status,health_reason,
                  consecutive_failures,created_at,updated_at`,
		device.ID, device.HostID, device.ImageID, device.DeviceKind, device.ProviderType, device.ProviderRef,
		device.LifecycleMode, device.Serial, device.STFSerial, device.ADBEndpoint, device.AppiumEndpoint,
		mustJSON(device.Capabilities), device.LifecycleStatus, device.HealthStatus, device.HealthReason, device.ConsecutiveFailures))
	return value, rowError(err)
}

func (store *Store) ListDevices(ctx context.Context) ([]management.Device, error) {
	rows, err := store.db.Pool().Query(ctx, deviceSelect+` ORDER BY created_at,id`)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	return scanDevices(rows)
}

func (store *Store) ListSchedulableDevices(ctx context.Context, poolID string) ([]management.Device, error) {
	rows, err := store.db.Pool().Query(ctx, deviceSelect+`
        JOIN device_pool_devices membership ON membership.device_id=devices.id AND membership.enabled=true
        JOIN device_pools pool ON pool.id=membership.pool_id AND pool.status='active'
        JOIN device_hosts host ON host.id=devices.host_id AND host.status='online' AND host.draining=false
        WHERE membership.pool_id=$1 AND devices.lifecycle_status='ready' AND devices.health_status='healthy'
        ORDER BY devices.created_at,devices.id`, poolID)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	return scanDevices(rows)
}

func (store *Store) GetDevice(ctx context.Context, id string) (management.Device, error) {
	value, err := scanDevice(store.db.Pool().QueryRow(ctx, deviceSelect+` WHERE devices.id=$1`, id))
	return value, rowError(err)
}

func (store *Store) UpdateDeviceState(ctx context.Context, device management.Device, oldLifecycle domain.DeviceLifecycleStatus, oldHealth domain.HealthStatus, audit management.DeviceAudit) (management.Device, error) {
	var value management.Device
	err := store.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var err error
		value, err = scanDevice(tx.QueryRow(ctx, `UPDATE devices SET
			lifecycle_status=$2::varchar,health_status=$3::varchar,health_reason=$4,
			consecutive_failures=CASE WHEN $2::varchar='provisioning' AND $3::varchar='unknown' THEN 0 ELSE consecutive_failures END,
			updated_at=clock_timestamp()
			WHERE id=$1 AND lifecycle_status=$5 AND health_status=$6
			RETURNING id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,stf_serial,
				adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status,health_reason,
				consecutive_failures,created_at,updated_at`,
			device.ID, device.LifecycleStatus, device.HealthStatus, device.HealthReason, oldLifecycle, oldHealth))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO device_audit_events
			(id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
			VALUES($1,$2,$3,$4,'device',$5,$6,$7,'{}'::jsonb)`, audit.ID, audit.ActorType, audit.ActorID,
			audit.Action, device.ID, audit.RequestID, sensitive.RedactText(audit.Reason))
		return err
	})
	return value, rowError(err)
}

func (store *Store) ReplayDeviceOperation(ctx context.Context, deviceID, hostID, key, commandType, requestHash string) (management.Device, bool, error) {
	var existingType string
	var existingRaw []byte
	err := store.db.Pool().QueryRow(ctx, `SELECT command_type,payload FROM device_host_commands
		WHERE host_id=$1 AND idempotency_key=$2`, hostID, key).Scan(&existingType, &existingRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return management.Device{}, false, nil
	}
	if err != nil {
		return management.Device{}, false, normalize(err)
	}
	var payload map[string]any
	if json.Unmarshal(existingRaw, &payload) != nil || existingType != commandType ||
		payload["device_id"] != deviceID || payload["request_hash"] != requestHash {
		return management.Device{}, false, management.ErrConflict
	}
	value, err := store.GetDevice(ctx, deviceID)
	return value, true, err
}

func (store *Store) QueueDeviceOperation(ctx context.Context, operation management.DeviceOperation) (management.Device, error) {
	if operation.CommandID == "" || operation.CommandType == "" || operation.IdempotencyKey == "" ||
		operation.MaxAttempts < 1 || operation.Device.ID == "" || operation.Device.HostID == "" {
		return management.Device{}, management.ErrInvalidArgument
	}
	var value management.Device
	err := store.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var existingType string
		var existingRaw []byte
		err := tx.QueryRow(ctx, `SELECT command_type,payload FROM device_host_commands
			WHERE host_id=$1 AND idempotency_key=$2`, operation.Device.HostID, operation.IdempotencyKey).
			Scan(&existingType, &existingRaw)
		if err == nil {
			var existingPayload map[string]any
			if json.Unmarshal(existingRaw, &existingPayload) != nil || existingType != operation.CommandType ||
				existingPayload["request_hash"] != operation.Payload["request_hash"] {
				return management.ErrConflict
			}
			value, err = scanDevice(tx.QueryRow(ctx, deviceSelect+` WHERE devices.id=$1`, operation.Device.ID))
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		record, err := (repository.CommandRepository{}).Create(ctx, tx, repository.CreateCommandParams{
			ID: operation.CommandID, HostID: operation.Device.HostID, CommandType: operation.CommandType,
			Payload: sensitive.RedactMap(operation.Payload), MaxAttempts: operation.MaxAttempts,
			IdempotencyKey: operation.IdempotencyKey,
		})
		if errors.Is(err, repository.ErrIdempotencyConflict) {
			return management.ErrConflict
		}
		if err != nil {
			return err
		}
		if record.ID != operation.CommandID {
			value, err = scanDevice(tx.QueryRow(ctx, deviceSelect+` WHERE devices.id=$1`, operation.Device.ID))
			return err
		}
		value, err = scanDevice(tx.QueryRow(ctx, `UPDATE devices SET
			lifecycle_status=$2::varchar,health_status=$3::varchar,health_reason=$4,updated_at=clock_timestamp()
			WHERE id=$1 AND host_id=$5 AND provider_ref=$6 AND lifecycle_status=$7 AND health_status=$8
			RETURNING id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,stf_serial,
				adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status,health_reason,
				consecutive_failures,created_at,updated_at`,
			operation.Device.ID, operation.Device.LifecycleStatus, operation.Device.HealthStatus,
			operation.Device.HealthReason, operation.Device.HostID, operation.Device.ProviderRef,
			operation.ExpectedLifecycle, operation.ExpectedHealth))
		if errors.Is(err, pgx.ErrNoRows) {
			return management.ErrConflict
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO device_audit_events
			(id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
			VALUES($1,$2,$3,$4,'device',$5,$6,$7,jsonb_build_object('command_id',$8::text,'command_type',$9::text))`,
			operation.Audit.ID, operation.Audit.ActorType, operation.Audit.ActorID, operation.Audit.Action,
			operation.Device.ID, operation.Audit.RequestID, sensitive.RedactText(operation.Audit.Reason),
			record.ID, record.CommandType)
		return err
	})
	return value, normalize(err)
}

const imageSelect = `SELECT id,name,docker_digest,api_level,abi,resolution,resource_config,status,validation_error,created_at,updated_at FROM device_images`
const hostSelect = `SELECT id,name,host_type,COALESCE(address,''),capabilities,capacity,used_capacity,status,draining,last_heartbeat_at,created_at,updated_at FROM device_hosts`
const poolSelect = `SELECT id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status,created_at,updated_at FROM device_pools`
const poolImageSelect = `SELECT pool_id,image_id,min_ready,max_instances,enabled,created_at,updated_at FROM device_pool_images`
const deviceSelect = `SELECT devices.id,devices.host_id,devices.image_id,devices.device_kind,devices.provider_type,devices.provider_ref,
    devices.lifecycle_mode,devices.serial,devices.stf_serial,devices.adb_endpoint,devices.appium_endpoint,devices.capabilities,
    devices.lifecycle_status,devices.health_status,devices.health_reason,devices.consecutive_failures,devices.created_at,devices.updated_at FROM devices`

func getImage(ctx context.Context, query database.Querier, id string) (management.Image, error) {
	value, err := scanImage(query.QueryRow(ctx, imageSelect+` WHERE id=$1`, id))
	return value, rowError(err)
}
func getHost(ctx context.Context, query database.Querier, id string) (management.Host, error) {
	value, err := scanHost(query.QueryRow(ctx, hostSelect+` WHERE id=$1`, id))
	return value, rowError(err)
}
func getPool(ctx context.Context, query database.Querier, id string) (management.Pool, error) {
	value, err := scanPool(query.QueryRow(ctx, poolSelect+` WHERE id=$1`, id))
	return value, rowError(err)
}

type rowScanner interface{ Scan(...any) error }

func scanImage(row rowScanner) (management.Image, error) {
	var v management.Image
	var raw []byte
	err := row.Scan(&v.ID, &v.Name, &v.DockerDigest, &v.APILevel, &v.ABI, &v.Resolution, &raw, &v.Status, &v.ValidationError, &v.CreatedAt, &v.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(raw, &v.ResourceConfig)
	}
	return v, err
}
func scanHost(row rowScanner) (management.Host, error) {
	var v management.Host
	var a, b, c []byte
	err := row.Scan(&v.ID, &v.Name, &v.HostType, &v.Address, &a, &b, &c, &v.Status, &v.Draining, &v.LastHeartbeatAt, &v.CreatedAt, &v.UpdatedAt)
	if err == nil {
		err = unmarshalMaps([][]byte{a, b, c}, []*map[string]any{&v.Capabilities, &v.Capacity, &v.UsedCapacity})
	}
	return v, err
}
func scanPool(row rowScanner) (management.Pool, error) {
	var v management.Pool
	err := row.Scan(&v.ID, &v.Name, &v.DefaultLeaseSeconds, &v.MaxLeaseSeconds, &v.MaxConcurrency, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanPoolImage(row rowScanner) (management.PoolImage, error) {
	var v management.PoolImage
	err := row.Scan(&v.PoolID, &v.ImageID, &v.MinReady, &v.MaxInstances, &v.Enabled, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanDevice(row rowScanner) (management.Device, error) {
	var v management.Device
	var raw []byte
	err := row.Scan(&v.ID, &v.HostID, &v.ImageID, &v.DeviceKind, &v.ProviderType, &v.ProviderRef, &v.LifecycleMode, &v.Serial, &v.STFSerial, &v.ADBEndpoint, &v.AppiumEndpoint, &raw, &v.LifecycleStatus, &v.HealthStatus, &v.HealthReason, &v.ConsecutiveFailures, &v.CreatedAt, &v.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(raw, &v.Capabilities)
	}
	return v, err
}

func scanDevices(rows pgx.Rows) ([]management.Device, error) {
	result := make([]management.Device, 0)
	for rows.Next() {
		value, err := scanDevice(rows)
		if err != nil {
			return nil, normalize(err)
		}
		result = append(result, value)
	}
	return result, normalize(rows.Err())
}
func unmarshalMaps(raw [][]byte, targets []*map[string]any) error {
	for i := range raw {
		if err := json.Unmarshal(raw[i], targets[i]); err != nil {
			return err
		}
	}
	return nil
}
func mustJSON(value any) []byte {
	content, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return content
}

func createIdempotent[T any](ctx context.Context, db *database.DB, meta management.Idempotency, insert func(pgx.Tx) error, load func(database.Querier, string) (T, error)) (T, error) {
	var result T
	err := db.WithinTx(ctx, func(tx pgx.Tx) error {
		command, err := tx.Exec(ctx, `INSERT INTO device_idempotency_records
            (client_id,scope,idempotency_key,request_hash,resource_type,resource_id,response_status)
            VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(client_id,scope,idempotency_key) DO NOTHING`,
			meta.ClientID, meta.Scope, meta.Key, meta.RequestHash, meta.ResourceType, meta.ResourceID, meta.ResponseStatus)
		if err != nil {
			return err
		}
		resourceID := meta.ResourceID
		if command.RowsAffected() == 0 {
			var hash, resourceType string
			if err := tx.QueryRow(ctx, `SELECT request_hash,resource_type,resource_id FROM device_idempotency_records WHERE client_id=$1 AND scope=$2 AND idempotency_key=$3`, meta.ClientID, meta.Scope, meta.Key).Scan(&hash, &resourceType, &resourceID); err != nil {
				return err
			}
			if hash != meta.RequestHash || resourceType != meta.ResourceType {
				return management.ErrConflict
			}
		} else if err := insert(tx); err != nil {
			return err
		}
		result, err = load(tx, resourceID)
		return err
	})
	return result, normalize(err)
}

func rowError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return management.ErrNotFound
	}
	return normalize(err)
}
func normalize(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", management.ErrConflict, pgErr.ConstraintName)
		case "23503":
			return fmt.Errorf("%w: foreign key", management.ErrNotFound)
		case "23514", "23502", "22P02":
			return fmt.Errorf("%w: database constraint", management.ErrInvalidArgument)
		}
	}
	return err
}

var _ management.Store = (*Store)(nil)

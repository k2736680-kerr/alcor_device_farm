package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/capacity"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
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
				(id,name,docker_image,docker_digest,api_level,abi,resolution,resource_config,status,validation_error)
				VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10)`, image.ID, image.Name, image.DockerImage, image.DockerDigest,
				image.APILevel, image.ABI, image.Resolution, mustJSON(image.ResourceConfig), image.Status, image.ValidationError)
			return err
		},
		func(query database.Querier, id string) (management.Image, error) { return getImage(ctx, query, id) },
	)
}

func (store *Store) ListImages(ctx context.Context, page paging.Page, status *domain.ImageStatus) ([]management.Image, int, error) {
	where, args := "", []any{}
	if status != nil {
		where, args = ` WHERE status=$1`, append(args, *status)
	}
	var total int
	if err := store.db.Pool().QueryRow(ctx, `SELECT count(*) FROM device_images`+where, args...).Scan(&total); err != nil {
		return nil, 0, normalize(err)
	}
	args = append(args, page.Limit(), page.Offset())
	limitIndex := len(args) - 1
	rows, err := store.db.Pool().Query(ctx, imageSelect+where+fmt.Sprintf(
		` ORDER BY created_at,id LIMIT $%d OFFSET $%d`, limitIndex, limitIndex+1), args...)
	if err != nil {
		return nil, 0, normalize(err)
	}
	defer rows.Close()
	result := make([]management.Image, 0)
	for rows.Next() {
		value, err := scanImage(rows)
		if err != nil {
			return nil, 0, normalize(err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, normalize(err)
	}
	return result, total, nil
}

func (store *Store) GetImage(ctx context.Context, id string) (management.Image, error) {
	return getImage(ctx, store.db.Pool(), id)
}

func (store *Store) UpdateImage(ctx context.Context, image management.Image, expected domain.ImageStatus) (management.Image, error) {
	value, err := scanImage(store.db.Pool().QueryRow(ctx, `UPDATE device_images SET
		name=$2,docker_image=NULLIF($3,''),docker_digest=$4,api_level=$5,abi=$6,resolution=$7,resource_config=$8,status=$9,validation_error=$10,updated_at=clock_timestamp()
		WHERE id=$1 AND status=$11
		RETURNING id,name,COALESCE(docker_image,''),docker_digest,api_level,abi,resolution,resource_config,status,validation_error,created_at,updated_at`,
		image.ID, image.Name, image.DockerImage, image.DockerDigest, image.APILevel, image.ABI, image.Resolution,
		mustJSON(image.ResourceConfig), image.Status, image.ValidationError, expected))
	return value, rowError(err)
}

func (store *Store) RetireImage(
	ctx context.Context,
	image management.Image,
	expected domain.ImageStatus,
	audit management.DeviceAudit,
) (management.Image, error) {
	var value management.Image
	err := store.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var current domain.ImageStatus
		if err := tx.QueryRow(ctx, `SELECT status FROM device_images WHERE id=$1 FOR UPDATE`, image.ID).Scan(&current); err != nil {
			return rowError(err)
		}
		if current != expected {
			return management.ErrConflict
		}
		var defaultReferences, activeDevices int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM device_pools WHERE default_image_id=$1`, image.ID).Scan(&defaultReferences); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM devices WHERE image_id=$1 AND lifecycle_status<>'deleted'`, image.ID).Scan(&activeDevices); err != nil {
			return err
		}
		if defaultReferences > 0 || activeDevices > 0 {
			return management.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE device_pool_images SET enabled=false,updated_at=clock_timestamp() WHERE image_id=$1`, image.ID); err != nil {
			return err
		}
		var err error
		value, err = scanImage(tx.QueryRow(ctx, `UPDATE device_images SET status=$2,updated_at=clock_timestamp()
			WHERE id=$1 AND status=$3
			RETURNING id,name,COALESCE(docker_image,''),docker_digest,api_level,abi,resolution,resource_config,status,validation_error,created_at,updated_at`,
			image.ID, image.Status, expected))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO device_audit_events
			(id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
			VALUES($1,$2,$3,$4,'device_image',$5,$6,$7,jsonb_build_object(
			'previous_status',$8::text,'status',$9::text,'pool_links_disabled',true,'history_preserved',true))`,
			audit.ID, audit.ActorType, audit.ActorID, audit.Action, image.ID, audit.RequestID,
			sensitive.RedactText(audit.Reason), expected, image.Status)
		return err
	})
	return value, normalize(err)
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

func (store *Store) ListHosts(ctx context.Context, page paging.Page) ([]management.Host, int, error) {
	total, err := store.count(ctx, `SELECT count(*) FROM device_hosts`)
	if err != nil {
		return nil, 0, err
	}
	rows, err := store.db.Pool().Query(ctx,
		hostSelect+` ORDER BY created_at,id LIMIT $1 OFFSET $2`, page.Limit(), page.Offset())
	if err != nil {
		return nil, 0, normalize(err)
	}
	defer rows.Close()
	result := make([]management.Host, 0)
	for rows.Next() {
		value, err := scanHost(rows)
		if err != nil {
			return nil, 0, normalize(err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, normalize(err)
	}
	return result, total, nil
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
				(id,name,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,default_image_id,base_device_id,status)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, pool.ID, pool.Name, pool.DefaultLeaseSeconds,
				pool.MaxLeaseSeconds, pool.MaxConcurrency, pool.TotalTarget, pool.MinReady, pool.DefaultImageID, pool.BaseDeviceID, pool.Status)
			return err
		},
		func(query database.Querier, id string) (management.Pool, error) { return getPool(ctx, query, id) },
	)
}

func (store *Store) ListPools(ctx context.Context, page paging.Page) ([]management.Pool, int, error) {
	total, err := store.count(ctx, `SELECT count(*) FROM device_pools`)
	if err != nil {
		return nil, 0, err
	}
	rows, err := store.db.Pool().Query(ctx,
		poolSelect+` ORDER BY created_at,id LIMIT $1 OFFSET $2`, page.Limit(), page.Offset())
	if err != nil {
		return nil, 0, normalize(err)
	}
	defer rows.Close()
	result := make([]management.Pool, 0)
	for rows.Next() {
		value, err := scanPool(rows)
		if err != nil {
			return nil, 0, normalize(err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, normalize(err)
	}
	return result, total, nil
}

func (store *Store) GetPool(ctx context.Context, id string) (management.Pool, error) {
	return getPool(ctx, store.db.Pool(), id)
}

func (store *Store) UpdatePool(ctx context.Context, pool management.Pool, expected domain.PoolStatus, audit management.DeviceAudit) (management.Pool, error) {
	var value management.Pool
	err := store.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var err error
		value, err = scanPool(tx.QueryRow(ctx, `UPDATE device_pools SET
			name=$2,default_lease_seconds=$3,max_lease_seconds=$4,max_concurrency=$5,total_target=$6,
			min_ready=$7,default_image_id=$8,base_device_id=$9,status=$10,updated_at=clock_timestamp()
			WHERE id=$1 AND status=$11
			RETURNING id,name,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,
			default_image_id,base_device_id,status,created_at,updated_at`, pool.ID, pool.Name, pool.DefaultLeaseSeconds,
			pool.MaxLeaseSeconds, pool.MaxConcurrency, pool.TotalTarget, pool.MinReady, pool.DefaultImageID, pool.BaseDeviceID, pool.Status, expected))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO device_audit_events
			(id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
			VALUES($1,$2,$3,$4,'device_pool',$5,$6,$7,jsonb_build_object(
			'total_target',$8::int,'min_ready',$9::int,'max_concurrency',$10::int,'default_image_id',$11::text))`,
			audit.ID, audit.ActorType, audit.ActorID, audit.Action, pool.ID, audit.RequestID,
			sensitive.RedactText(audit.Reason), pool.TotalTarget, pool.MinReady, pool.MaxConcurrency, pool.DefaultImageID)
		return err
	})
	return value, normalize(err)
}

func (store *Store) ListPoolImages(ctx context.Context, poolID string, page paging.Page) ([]management.PoolImage, int, error) {
	total, err := store.count(ctx, `SELECT count(*) FROM device_pool_images WHERE pool_id=$1`, poolID)
	if err != nil {
		return nil, 0, err
	}
	rows, err := store.db.Pool().Query(ctx,
		poolImageSelect+` WHERE pool_id=$1 ORDER BY created_at,image_id LIMIT $2 OFFSET $3`,
		poolID, page.Limit(), page.Offset())
	if err != nil {
		return nil, 0, normalize(err)
	}
	defer rows.Close()
	values := make([]management.PoolImage, 0)
	for rows.Next() {
		value, err := scanPoolImage(rows)
		if err != nil {
			return nil, 0, normalize(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, normalize(err)
	}
	return values, total, nil
}

func (store *Store) GetPoolImage(ctx context.Context, poolID, imageID string) (management.PoolImage, error) {
	value, err := scanPoolImage(store.db.Pool().QueryRow(ctx,
		poolImageSelect+` WHERE pool_id=$1 AND image_id=$2`, poolID, imageID))
	return value, rowError(err)
}

func (store *Store) SetPoolImage(ctx context.Context, value management.PoolImage, audit management.DeviceAudit) (management.PoolImage, error) {
	var result management.PoolImage
	err := store.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var currentMax int
		lookupErr := tx.QueryRow(ctx, `SELECT max_instances FROM device_pool_images
			WHERE pool_id=$1 AND image_id=$2 FOR UPDATE`, value.PoolID, value.ImageID).Scan(&currentMax)
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			return lookupErr
		}
		if lookupErr == nil && value.MaxInstances < currentMax && !audit.DestructiveApproved {
			return management.ErrInvalidArgument
		}
		var err error
		result, err = scanPoolImage(tx.QueryRow(ctx, `INSERT INTO device_pool_images
			(pool_id,image_id,min_ready,max_instances,enabled) VALUES($1,$2,$3,$4,$5)
			ON CONFLICT(pool_id,image_id) DO UPDATE SET min_ready=EXCLUDED.min_ready,
			max_instances=EXCLUDED.max_instances,enabled=EXCLUDED.enabled,updated_at=clock_timestamp()
			RETURNING pool_id,image_id,min_ready,max_instances,enabled,created_at,updated_at`,
			value.PoolID, value.ImageID, value.MinReady, value.MaxInstances, value.Enabled))
		if err != nil {
			return err
		}
		var target int
		if err := tx.QueryRow(ctx, `UPDATE device_pools SET
			default_image_id=COALESCE(default_image_id,$2),
			total_target=CASE WHEN default_image_id IS NULL OR default_image_id=$2 THEN $3 ELSE total_target END,
			min_ready=CASE WHEN default_image_id IS NULL OR default_image_id=$2 THEN $4 ELSE min_ready END,
			max_concurrency=CASE WHEN default_image_id IS NULL OR default_image_id=$2 THEN $3 ELSE max_concurrency END,
			updated_at=clock_timestamp() WHERE id=$1 RETURNING total_target`, value.PoolID, value.ImageID,
			value.MaxInstances, value.MinReady).Scan(&target); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO device_audit_events
			(id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
			VALUES($1,$2,$3,$4,'device_pool_image',$5,$6,$7,
			jsonb_build_object('pool_id',$8::text,'min_ready',$9::int,'max_instances',$10::int,
			'enabled',$11::boolean,'pool_total_target',$12::int,'host_capacity_policy','dynamic'))`, audit.ID, audit.ActorType, audit.ActorID,
			audit.Action, value.ImageID, audit.RequestID, sensitive.RedactText(audit.Reason), value.PoolID,
			value.MinReady, value.MaxInstances, value.Enabled, target)
		return err
	})
	return result, normalize(err)
}

func (store *Store) DisablePoolImage(ctx context.Context, poolID, imageID string) (management.PoolImage, error) {
	value, err := scanPoolImage(store.db.Pool().QueryRow(ctx, `UPDATE device_pool_images SET enabled=false,updated_at=clock_timestamp()
		WHERE pool_id=$1 AND image_id=$2
		RETURNING pool_id,image_id,min_ready,max_instances,enabled,created_at,updated_at`, poolID, imageID))
	return value, rowError(err)
}

func (store *Store) SelectPoolDefaultImage(
	ctx context.Context,
	poolID, imageID string,
	audit management.DeviceAudit,
) (management.Pool, error) {
	var value management.Pool
	err := store.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var totalTarget int
		if err := tx.QueryRow(ctx, `SELECT total_target FROM device_pools WHERE id=$1 FOR UPDATE`, poolID).Scan(&totalTarget); err != nil {
			return rowError(err)
		}
		var imageStatus domain.ImageStatus
		if err := tx.QueryRow(ctx, `SELECT status FROM device_images WHERE id=$1 FOR UPDATE`, imageID).Scan(&imageStatus); err != nil {
			return rowError(err)
		}
		if imageStatus != domain.ImageReady {
			return management.ErrImageUnavailable
		}
		if _, err := tx.Exec(ctx, `INSERT INTO device_pool_images(pool_id,image_id,min_ready,max_instances,enabled)
			VALUES($1,$2,$3,$3,true) ON CONFLICT(pool_id,image_id) DO UPDATE SET
			min_ready=EXCLUDED.min_ready,max_instances=EXCLUDED.max_instances,enabled=true,updated_at=clock_timestamp()`,
			poolID, imageID, totalTarget); err != nil {
			return err
		}
		var err error
		value, err = scanPool(tx.QueryRow(ctx, `UPDATE device_pools SET default_image_id=$2,updated_at=clock_timestamp()
			WHERE id=$1 RETURNING id,name,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,
			default_image_id,status,created_at,updated_at`, poolID, imageID))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO device_audit_events
			(id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
			VALUES($1,$2,$3,$4,'device_pool',$5,$6,$7,jsonb_build_object(
			'default_image_id',$8::text,'pool_image_enabled',true,'existing_devices_reimaged',false))`,
			audit.ID, audit.ActorType, audit.ActorID, audit.Action, poolID, audit.RequestID,
			sensitive.RedactText(audit.Reason), imageID)
		return err
	})
	return value, normalize(err)
}

func (store *Store) SetPoolBaseDevice(ctx context.Context, poolID, deviceID string, audit management.DeviceAudit) (management.Pool, error) {
	var value management.Pool
	err := store.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var enabled bool
		var lifecycle domain.DeviceLifecycleStatus
		var health domain.HealthStatus
		if err := tx.QueryRow(ctx, `SELECT pd.enabled,d.lifecycle_status,d.health_status FROM device_pool_devices pd
			JOIN devices d ON d.id=pd.device_id WHERE pd.pool_id=$1 AND pd.device_id=$2 FOR UPDATE`, poolID, deviceID).Scan(&enabled, &lifecycle, &health); err != nil {
			return err
		}
		if !enabled || lifecycle != domain.DeviceReady || health != domain.HealthHealthy {
			return management.ErrInvalidArgument
		}
		var err error
		value, err = scanPool(tx.QueryRow(ctx, `UPDATE device_pools SET base_device_id=$2,updated_at=clock_timestamp()
			WHERE id=$1 RETURNING id,name,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,default_image_id,base_device_id,status,created_at,updated_at`, poolID, deviceID))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO device_audit_events
			(id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
			VALUES($1,$2,$3,$4,'device_pool',$5,$6,$7,jsonb_build_object('base_device_id',$8::text))`,
			audit.ID, audit.ActorType, audit.ActorID, audit.Action, poolID, audit.RequestID, sensitive.RedactText(audit.Reason), deviceID)
		return err
	})
	return value, normalize(err)
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
	_, err := store.db.Pool().Exec(ctx, `INSERT INTO devices
        (id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,stf_serial,
         adb_endpoint,appium_endpoint,capabilities,lifecycle_status,health_status,health_reason,consecutive_failures)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		`,
		device.ID, device.HostID, device.ImageID, device.DeviceKind, device.ProviderType, device.ProviderRef,
		device.LifecycleMode, device.Serial, device.STFSerial, device.ADBEndpoint, device.AppiumEndpoint,
		mustJSON(device.Capabilities), device.LifecycleStatus, device.HealthStatus, device.HealthReason, device.ConsecutiveFailures)
	if err != nil {
		return management.Device{}, rowError(err)
	}
	return store.GetDevice(ctx, device.ID)
}

func (store *Store) ListDevices(ctx context.Context, page paging.Page, filter management.DeviceFilter) ([]management.Device, int, error) {
	conditions := make([]string, 0, 3)
	arguments := make([]any, 0, 5)
	if filter.PoolID != "" {
		arguments = append(arguments, filter.PoolID)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (SELECT 1 FROM device_pool_devices membership
			WHERE membership.device_id=devices.id AND membership.pool_id=$%d AND membership.enabled)`, len(arguments)))
	}
	if filter.LifecycleStatus != "" {
		arguments = append(arguments, filter.LifecycleStatus)
		conditions = append(conditions, fmt.Sprintf("devices.lifecycle_status=$%d", len(arguments)))
	}
	if filter.HealthStatus != "" {
		arguments = append(arguments, filter.HealthStatus)
		conditions = append(conditions, fmt.Sprintf("devices.health_status=$%d", len(arguments)))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int
	err := store.db.Pool().QueryRow(ctx, `SELECT count(*) FROM devices`+where, arguments...).Scan(&total)
	if err != nil {
		return nil, 0, normalize(err)
	}
	arguments = append(arguments, page.Limit(), page.Offset())
	rows, err := store.db.Pool().Query(ctx, deviceSelect+where+fmt.Sprintf(
		` ORDER BY devices.created_at,devices.id LIMIT $%d OFFSET $%d`, len(arguments)-1, len(arguments)), arguments...)
	if err != nil {
		return nil, 0, normalize(err)
	}
	defer rows.Close()
	values, err := scanDevices(rows)
	if err != nil {
		return nil, 0, err
	}
	return values, total, nil
}

// count runs the total query separately from the page query. Keeping it out of
// the page SELECT avoids a window function over every matching row and keeps
// the two statements independently indexable.
func (store *Store) count(ctx context.Context, query string, args ...any) (int, error) {
	var total int
	if err := store.db.Pool().QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, normalize(err)
	}
	return total, nil
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

func (store *Store) IsDevicePoolBase(ctx context.Context, deviceID string) (bool, error) {
	var exists bool
	err := store.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_pools WHERE base_device_id=$1)`, deviceID).Scan(&exists)
	return exists, normalize(err)
}

func (store *Store) UpdateDeviceState(ctx context.Context, device management.Device, oldLifecycle domain.DeviceLifecycleStatus, oldHealth domain.HealthStatus, audit management.DeviceAudit) (management.Device, error) {
	var value management.Device
	err := store.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var err error
		result, updateErr := tx.Exec(ctx, `UPDATE devices SET
			lifecycle_status=$2::varchar,health_status=$3::varchar,health_reason=$4,
			consecutive_failures=CASE WHEN $2::varchar='provisioning' AND $3::varchar='unknown' THEN 0 ELSE consecutive_failures END,
			updated_at=clock_timestamp()
			WHERE id=$1 AND lifecycle_status=$5 AND health_status=$6`,
			device.ID, device.LifecycleStatus, device.HealthStatus, device.HealthReason, oldLifecycle, oldHealth)
		if updateErr != nil {
			return updateErr
		}
		if result.RowsAffected() != 1 {
			return management.ErrConflict
		}
		value, err = scanDevice(tx.QueryRow(ctx, deviceSelect+` WHERE devices.id=$1`, device.ID))
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
		var lockedDeviceID string
		if err := tx.QueryRow(ctx, `SELECT id FROM devices
			WHERE id=$1 AND host_id=$2 AND provider_ref=$3 AND lifecycle_status=$4 AND health_status=$5
			FOR UPDATE`, operation.Device.ID, operation.Device.HostID, operation.Device.ProviderRef,
			operation.ExpectedLifecycle, operation.ExpectedHealth).Scan(&lockedDeviceID); errors.Is(err, pgx.ErrNoRows) {
			return management.ErrConflict
		} else if err != nil {
			return err
		}
		if operation.RequireNoActiveCommand {
			var active bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM device_host_commands
				WHERE payload->>'device_id'=$1 AND status IN ('pending','leased'))`,
				operation.Device.ID).Scan(&active); err != nil {
				return err
			}
			if active {
				return management.ErrConflict
			}
		}
		if operation.RequireNoActiveReservation {
			var active bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM device_reservations
				WHERE device_id=$1 AND status IN ('pending','active'))`, operation.Device.ID).Scan(&active); err != nil {
				return err
			}
			if active {
				return management.ErrConflict
			}
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
		result, updateErr := tx.Exec(ctx, `UPDATE devices SET
			lifecycle_status=$2::varchar,health_status=$3::varchar,health_reason=$4,updated_at=clock_timestamp()
			WHERE id=$1 AND host_id=$5 AND provider_ref=$6 AND lifecycle_status=$7 AND health_status=$8`,
			operation.Device.ID, operation.Device.LifecycleStatus, operation.Device.HealthStatus,
			operation.Device.HealthReason, operation.Device.HostID, operation.Device.ProviderRef,
			operation.ExpectedLifecycle, operation.ExpectedHealth)
		if updateErr != nil {
			return updateErr
		}
		if result.RowsAffected() != 1 {
			return management.ErrConflict
		}
		if operation.Reimage {
			if _, err := tx.Exec(ctx, `UPDATE devices SET pending_image_id=$2,pending_runtime_profile=$3,
				reimage_status='pending',reimage_error=NULL,updated_at=clock_timestamp() WHERE id=$1`,
				operation.Device.ID, operation.PendingImageID, mustJSON(operation.PendingRuntimeProfile)); err != nil {
				return err
			}
		}
		value, err = scanDevice(tx.QueryRow(ctx, deviceSelect+` WHERE devices.id=$1`, operation.Device.ID))
		if err != nil {
			return err
		}
		if operation.DisableMemberships {
			if _, err := tx.Exec(ctx, `UPDATE device_pool_devices SET enabled=false,updated_at=clock_timestamp()
				WHERE device_id=$1 AND enabled`, operation.Device.ID); err != nil {
				return err
			}
		}
		if operation.ReducePoolTargets {
			// Deleting a device is an explicit capacity decision. Lower the pool
			// target in the same transaction so the reconciler never replaces it.
			if _, err := tx.Exec(ctx, `UPDATE device_pools p SET
				total_target=GREATEST(0,p.total_target-1),
				min_ready=LEAST(p.min_ready,GREATEST(0,p.total_target-1)),
				max_concurrency=LEAST(p.max_concurrency,GREATEST(0,p.total_target-1)),
				updated_at=clock_timestamp()
				WHERE EXISTS (SELECT 1 FROM device_pool_devices pd WHERE pd.pool_id=p.id AND pd.device_id=$1)`, operation.Device.ID); err != nil {
				return err
			}
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

func (store *Store) CheckDeviceReimageCapacity(ctx context.Context, device management.Device, current, target runtimeprofile.Profile, imageID string) (capacity.Result, error) {
	var capacityRaw, usedRaw, pendingRaw []byte
	var status domain.HostStatus
	var draining, imageCached bool
	var lastHeartbeat *time.Time
	err := store.db.Pool().QueryRow(ctx, `SELECT h.capacity,h.used_capacity,h.status,h.draining,h.last_heartbeat_at,
		COALESCE((SELECT jsonb_agg(c.payload) FROM device_host_commands c
			WHERE c.host_id=h.id AND c.status IN ('pending','leased')
			AND c.command_type IN ('create','rebuild','validate_image')
			AND COALESCE(c.payload->>'device_id','')<>$2),'[]'::jsonb),
		EXISTS (SELECT 1 FROM devices cached WHERE cached.host_id=h.id AND cached.image_id=$3
			AND cached.lifecycle_status<>'deleted') OR EXISTS (SELECT 1 FROM device_host_commands cached_command
			WHERE cached_command.host_id=h.id AND cached_command.command_type='validate_image'
			AND cached_command.status='succeeded' AND cached_command.payload->>'image_id'=$3)
		FROM device_hosts h WHERE h.id=$1`, device.HostID, device.ID, imageID).
		Scan(&capacityRaw, &usedRaw, &status, &draining, &lastHeartbeat, &pendingRaw, &imageCached)
	if err != nil {
		return capacity.Result{}, rowError(err)
	}
	if status != domain.HostOnline || draining || lastHeartbeat == nil || time.Since(*lastHeartbeat) > 30*time.Second {
		return capacity.Result{}, management.ErrHostUnavailable
	}
	var capacityMap, usedMap map[string]any
	var pending []map[string]any
	if json.Unmarshal(capacityRaw, &capacityMap) != nil || json.Unmarshal(usedRaw, &usedMap) != nil || json.Unmarshal(pendingRaw, &pending) != nil {
		return capacity.Result{}, management.ErrInvalidArgument
	}
	host, dynamic := capacity.HostFromMap(capacityMap)
	if !dynamic {
		return capacity.Result{Fits: true, Additional: 1}, nil
	}
	if host.CollectedAt.IsZero() || time.Since(host.CollectedAt) > 30*time.Second {
		return capacity.Result{}, management.ErrHostUnavailable
	}
	existing := capacity.Allocation{
		CPUCores: number(usedMap["cpu_cores"]), MemoryMB: int64(number(usedMap["memory_mb"])),
		DiskMB: int64(number(usedMap["data_disk_mb"])), Slots: int(number(usedMap["device_slots"])),
	}
	existing.CPUCores = maxFloat(0, existing.CPUCores-current.ContainerCPUCores)
	existing.MemoryMB = maxInt64(0, existing.MemoryMB-current.ContainerMemoryMB)
	existing.DiskMB = maxInt64(0, existing.DiskMB-current.DataDiskMB)
	existing.Slots = maxInt(0, existing.Slots-1)
	host.MemoryAvailableMB = minInt64(host.MemoryTotalMB, host.MemoryAvailableMB+current.ContainerMemoryMB)
	host.DiskAvailableMB = minInt64(host.DiskTotalMB, host.DiskAvailableMB+current.DataDiskMB)
	pendingAllocation := capacity.Allocation{}
	for _, payload := range pending {
		profile, parseErr := runtimeprofile.Parse(mapValue(payload, "runtime_profile"))
		if parseErr != nil {
			return capacity.Result{}, management.ErrInvalidArgument
		}
		pendingAllocation.CPUCores += profile.ContainerCPUCores
		pendingAllocation.MemoryMB += profile.ContainerMemoryMB
		pendingAllocation.DiskMB += profile.DataDiskMB + profile.ImageDiskMB
		pendingAllocation.Slots++
	}
	return capacity.Evaluate(host, existing, pendingAllocation, target, imageCached), nil
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int64:
		return float64(typed)
	case int:
		return float64(typed)
	default:
		return 0
	}
}
func mapValue(values map[string]any, key string) map[string]any {
	value, _ := values[key].(map[string]any)
	return value
}
func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

const imageSelect = `SELECT id,name,COALESCE(docker_image,''),docker_digest,api_level,abi,resolution,resource_config,status,validation_error,created_at,updated_at FROM device_images`
const hostSelect = `SELECT id,name,host_type,COALESCE(address,''),capabilities,capacity,used_capacity,status,draining,last_heartbeat_at,created_at,updated_at FROM device_hosts`
const poolSelect = `SELECT id,name,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,default_image_id,base_device_id,status,created_at,updated_at FROM device_pools`
const poolImageSelect = `SELECT pool_id,image_id,min_ready,max_instances,enabled,created_at,updated_at FROM device_pool_images`
const deviceSelect = `SELECT devices.id,devices.host_id,devices.image_id,
    (SELECT pool.id FROM device_pool_devices membership JOIN device_pools pool ON pool.id=membership.pool_id
        WHERE membership.device_id=devices.id AND membership.enabled ORDER BY pool.created_at,pool.id LIMIT 1),
    (SELECT pool.name FROM device_pool_devices membership JOIN device_pools pool ON pool.id=membership.pool_id
        WHERE membership.device_id=devices.id AND membership.enabled ORDER BY pool.created_at,pool.id LIMIT 1),
    EXISTS(SELECT 1 FROM device_pools pool WHERE pool.base_device_id=devices.id),
    devices.device_kind,devices.provider_type,devices.provider_ref,
    devices.lifecycle_mode,devices.serial,devices.stf_serial,devices.adb_endpoint,devices.appium_endpoint,devices.capabilities,
    devices.runtime_profile_override,COALESCE(devices.runtime_profile_override,device_image.resource_config,'{}'::jsonb),
    devices.pending_image_id,devices.pending_runtime_profile,devices.reimage_status,devices.reimage_error,
    devices.lifecycle_status,devices.health_status,devices.health_reason,devices.consecutive_failures,devices.created_at,devices.updated_at
    FROM devices LEFT JOIN device_images device_image ON device_image.id=devices.image_id`

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
	err := row.Scan(&v.ID, &v.Name, &v.DockerImage, &v.DockerDigest, &v.APILevel, &v.ABI, &v.Resolution, &raw, &v.Status, &v.ValidationError, &v.CreatedAt, &v.UpdatedAt)
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
	err := row.Scan(&v.ID, &v.Name, &v.DefaultLeaseSeconds, &v.MaxLeaseSeconds, &v.MaxConcurrency,
		&v.TotalTarget, &v.MinReady, &v.DefaultImageID, &v.BaseDeviceID, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanPoolImage(row rowScanner) (management.PoolImage, error) {
	var v management.PoolImage
	err := row.Scan(&v.PoolID, &v.ImageID, &v.MinReady, &v.MaxInstances, &v.Enabled, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanDevice(row rowScanner) (management.Device, error) {
	var v management.Device
	var capabilities, override, effective, pending []byte
	err := row.Scan(&v.ID, &v.HostID, &v.ImageID, &v.PoolID, &v.PoolName, &v.IsPoolBase, &v.DeviceKind, &v.ProviderType, &v.ProviderRef, &v.LifecycleMode, &v.Serial, &v.STFSerial, &v.ADBEndpoint, &v.AppiumEndpoint,
		&capabilities, &override, &effective, &v.PendingImageID, &pending, &v.ReimageStatus, &v.ReimageError,
		&v.LifecycleStatus, &v.HealthStatus, &v.HealthReason, &v.ConsecutiveFailures, &v.CreatedAt, &v.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(capabilities, &v.Capabilities)
	}
	if err == nil && len(override) > 0 {
		err = json.Unmarshal(override, &v.RuntimeProfileOverride)
	}
	if err == nil {
		err = json.Unmarshal(effective, &v.EffectiveRuntimeProfile)
	}
	if err == nil && len(pending) > 0 {
		err = json.Unmarshal(pending, &v.PendingRuntimeProfile)
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

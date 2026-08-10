package warmpool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/capacity"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
	"github.com/jackc/pgx/v5"
)

var ErrNoCapacity = errors.New("no eligible Docker emulator host capacity")

// A quarantined emulator still occupies a slot when the latest Agent heartbeat
// discovered its Provider resource. A later heartbeat that no longer reports
// the device advances host.last_heartbeat_at without advancing last_seen_at,
// allowing the warm pool to replace the missing resource.
const slotOccupyingDevicePredicate = `(d.lifecycle_status NOT IN ('quarantined','deleted') OR
	(d.lifecycle_status='quarantined' AND d.last_seen_at IS NOT NULL AND
	 h.last_heartbeat_at IS NOT NULL AND d.last_seen_at >= h.last_heartbeat_at))`

type Result struct {
	Configurations       int
	ValidationsQueued    int
	ValidationsCompleted int
	ValidationsFailed    int
	RebuildsQueued       int
	RebuildsCompleted    int
	RebuildsFailed       int
	DevicesCreated       int
	DevicesReady         int
	DevicesFailed        int
	DeletesQueued        int
	DeletesCompleted     int
	DeletesFailed        int
	CapacityMisses       int
	BackoffSkips         int
}

type Controller struct {
	db     *database.DB
	newID  func() (string, error)
	logger *slog.Logger
}

func New(db *database.DB, generator func() (string, error), logger *slog.Logger) *Controller {
	if generator == nil {
		generator = identifier.New
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{db: db, newID: generator, logger: logger}
}

func (controller *Controller) RunOnce(ctx context.Context) (Result, error) {
	if controller == nil || controller.db == nil {
		return Result{}, errors.New("warm pool database is not configured")
	}
	result, err := controller.reconcileRecyclingDevices(ctx)
	if err != nil {
		return result, err
	}
	deletes, err := controller.reconcileScaleDownDeletes(ctx)
	if err != nil {
		return result, err
	}
	result.DeletesCompleted += deletes.DeletesCompleted
	result.DeletesFailed += deletes.DeletesFailed
	validations, err := controller.reconcileImageValidations(ctx)
	if err != nil {
		return result, err
	}
	result.ValidationsQueued += validations.ValidationsQueued
	result.ValidationsCompleted += validations.ValidationsCompleted
	result.ValidationsFailed += validations.ValidationsFailed
	rows, err := controller.db.Pool().Query(ctx, `SELECT pi.pool_id,pi.image_id
		FROM device_pool_images pi
		JOIN device_pools p ON p.id=pi.pool_id AND p.status='active'
		JOIN device_images i ON i.id=pi.image_id AND i.status='ready'
		WHERE pi.enabled ORDER BY pi.pool_id,pi.image_id`)
	if err != nil {
		return Result{}, err
	}
	var keys [][2]string
	for rows.Next() {
		var key [2]string
		if err := rows.Scan(&key[0], &key[1]); err != nil {
			rows.Close()
			return Result{}, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, err
	}
	rows.Close()
	result.Configurations = len(keys)
	for _, key := range keys {
		partial, err := controller.reconcile(ctx, key[0], key[1])
		if err != nil {
			return result, err
		}
		result.DevicesCreated += partial.DevicesCreated
		result.DevicesReady += partial.DevicesReady
		result.DevicesFailed += partial.DevicesFailed
		result.DeletesQueued += partial.DeletesQueued
		result.CapacityMisses += partial.CapacityMisses
		result.BackoffSkips += partial.BackoffSkips
	}
	return result, nil
}

type scaleDownDevice struct {
	ID           string
	HostID       string
	ProviderRef  string
	Lifecycle    domain.DeviceLifecycleStatus
	Health       domain.HealthStatus
	HasActiveUse bool
	HasCommand   bool
	Shared       bool
}

func (controller *Controller) reconcileScaleDownDeletes(ctx context.Context) (Result, error) {
	rows, err := controller.db.Pool().Query(ctx, `SELECT DISTINCT payload->>'device_id'
		FROM device_host_commands WHERE command_type='delete'
		AND payload->>'operation_source'='warm_pool_scale_down'
		AND status IN ('succeeded','failed','timed_out','canceled')
		AND COALESCE(payload->>'scale_down_reconciled','false')<>'true'
		ORDER BY payload->>'device_id'`)
	if err != nil {
		return Result{}, err
	}
	var deviceIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return Result{}, err
		}
		deviceIDs = append(deviceIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, err
	}
	rows.Close()
	result := Result{}
	for _, deviceID := range deviceIDs {
		err := controller.db.WithinTx(ctx, func(tx pgx.Tx) error {
			var commandID string
			var status domain.CommandStatus
			var commandResult []byte
			var errorCode *string
			if err := tx.QueryRow(ctx, `SELECT id,status,result,error_code FROM device_host_commands
				WHERE command_type='delete' AND payload->>'operation_source'='warm_pool_scale_down'
				AND payload->>'device_id'=$1 AND COALESCE(payload->>'scale_down_reconciled','false')<>'true'
				ORDER BY created_at DESC,id DESC LIMIT 1 FOR UPDATE`, deviceID).
				Scan(&commandID, &status, &commandResult, &errorCode); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				return err
			}
			if status == domain.CommandPending || status == domain.CommandLeased {
				return nil
			}
			var lifecycle domain.DeviceLifecycleStatus
			var health domain.HealthStatus
			if err := tx.QueryRow(ctx, `SELECT lifecycle_status,health_status FROM devices WHERE id=$1 FOR UPDATE`, deviceID).
				Scan(&lifecycle, &health); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				return err
			}
			now, err := database.ClockNow(ctx, tx)
			if err != nil {
				return err
			}
			deleted := false
			if status == domain.CommandSucceeded {
				var value struct {
					Deleted bool `json:"deleted"`
				}
				deleted = json.Unmarshal(commandResult, &value) == nil && value.Deleted
			}
			eventType, severity, reason := "warm_pool_scale_down_completed", "info", "automatic scale down removed emulator resources"
			if deleted {
				aggregate, err := domain.RestoreDevice(deviceID, lifecycle, health)
				if err != nil {
					return err
				}
				if aggregate.Lifecycle() != domain.DeviceDeleted {
					if err := aggregate.Transition(domain.DeviceDeleted, reason, now); err != nil {
						return err
					}
				}
				if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status='deleted',health_reason=$2,
					adb_endpoint=NULL,appium_endpoint=NULL,stf_serial=NULL,updated_at=$3 WHERE id=$1`, deviceID, reason, now); err != nil {
					return err
				}
				result.DeletesCompleted++
			} else {
				code := "EMULATOR_DELETE_FAILED"
				if errorCode != nil && *errorCode != "" {
					code = *errorCode
				}
				reason = code + ": automatic scale down could not remove emulator resources"
				eventType, severity = "warm_pool_scale_down_failed", "error"
				aggregate, err := domain.RestoreDevice(deviceID, lifecycle, health)
				if err != nil {
					return err
				}
				if aggregate.Health() != domain.HealthUnhealthy {
					if err := aggregate.UpdateHealth(domain.HealthUnhealthy, reason, now); err != nil {
						return err
					}
				}
				if aggregate.Lifecycle() != domain.DeviceQuarantined {
					if err := aggregate.Transition(domain.DeviceQuarantined, reason, now); err != nil {
						return err
					}
				}
				if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_status=$3,
					health_reason=$4,consecutive_failures=consecutive_failures+1,updated_at=$5 WHERE id=$1`,
					deviceID, aggregate.Lifecycle(), aggregate.Health(), reason, now); err != nil {
					return err
				}
				result.DeletesFailed++
			}
			eventID, err := controller.newID()
			if err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string]any{"command_id": commandID})
			if _, err := tx.Exec(ctx, `INSERT INTO device_health_events
				(id,device_id,source,event_type,severity,reason,payload,observed_at)
				VALUES($1,$2,'reconciler',$3,$4,$5,$6::jsonb,$7)`, eventID, deviceID,
				eventType, severity, reason, payload, now); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE device_host_commands SET
				payload=jsonb_set(payload,'{scale_down_reconciled}','true'::jsonb,true),updated_at=clock_timestamp()
				WHERE id=$1`, commandID)
			return err
		})
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func (controller *Controller) queueScaleDown(
	ctx context.Context,
	tx pgx.Tx,
	poolID, imageID string,
	target, excess int,
) (int, error) {
	queued := 0
	for queued < excess {
		var current scaleDownDevice
		err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT d.id,d.host_id,d.provider_ref,d.lifecycle_status,d.health_status,
			EXISTS (SELECT 1 FROM device_reservations r WHERE r.device_id=d.id AND r.status='active'),
			EXISTS (SELECT 1 FROM device_host_commands c WHERE c.payload->>'device_id'=d.id AND c.status IN ('pending','leased')),
			EXISTS (SELECT 1 FROM device_pool_devices other WHERE other.device_id=d.id AND other.enabled AND other.pool_id<>$1)
			FROM device_pool_devices pd JOIN devices d ON d.id=pd.device_id
			JOIN device_hosts h ON h.id=d.host_id
			WHERE pd.pool_id=$1 AND pd.enabled AND d.image_id=$2 AND d.device_kind='emulator'
			AND d.provider_type='docker_emulator' AND %s
			ORDER BY d.created_at,d.id FOR UPDATE OF d,pd SKIP LOCKED LIMIT 1`, slotOccupyingDevicePredicate),
			poolID, imageID).Scan(&current.ID, &current.HostID, &current.ProviderRef, &current.Lifecycle,
			&current.Health, &current.HasActiveUse, &current.HasCommand, &current.Shared)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return queued, err
		}
		if current.HasActiveUse || current.HasCommand || current.Shared ||
			(current.Lifecycle != domain.DeviceReady && current.Lifecycle != domain.DeviceStopped && current.Lifecycle != domain.DeviceQuarantined) {
			break
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return queued, err
		}
		nextLifecycle := current.Lifecycle
		if current.Lifecycle == domain.DeviceReady {
			aggregate, err := domain.RestoreDevice(current.ID, current.Lifecycle, current.Health)
			if err != nil {
				return queued, err
			}
			if err := aggregate.Transition(domain.DeviceStopped, "automatic scale down queued", now); err != nil {
				return queued, err
			}
			nextLifecycle = aggregate.Lifecycle()
		}
		commandID, err := controller.newID()
		if err != nil {
			return queued, err
		}
		payload, err := json.Marshal(map[string]any{"operation_source": "warm_pool_scale_down",
			"device_id": current.ID, "pool_id": poolID, "image_id": imageID, "provider_ref": current.ProviderRef,
			"target_instances": target})
		if err != nil {
			return queued, err
		}
		hash := sha256.Sum256([]byte(poolID + "\x00" + imageID + "\x00" + current.ID + "\x00" + fmt.Sprint(target)))
		if _, err := tx.Exec(ctx, `INSERT INTO device_host_commands
			(id,host_id,command_type,payload,status,max_attempts,idempotency_key)
			VALUES($1,$2,'delete',$3::jsonb,'pending',3,$4)`, commandID, current.HostID, payload,
			"scale-down-"+hex.EncodeToString(hash[:16])); err != nil {
			return queued, err
		}
		if _, err := tx.Exec(ctx, `UPDATE device_pool_devices SET enabled=false,updated_at=$3
			WHERE pool_id=$1 AND device_id=$2 AND enabled`, poolID, current.ID, now); err != nil {
			return queued, err
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_reason='automatic scale down queued',updated_at=$3
			WHERE id=$1 AND lifecycle_status=$4`, current.ID, nextLifecycle, now, current.Lifecycle); err != nil {
			return queued, err
		}
		auditID, err := controller.newID()
		if err != nil {
			return queued, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO device_audit_events
			(id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
			VALUES($1,'system','system','scale_down_device','device',$2,$3,
			'automatic fixed target scale down',jsonb_build_object('command_id',$4::text,'pool_id',$5::text,
			'image_id',$6::text,'target_instances',$7::int))`, auditID, current.ID, "scale-down-"+commandID,
			commandID, poolID, imageID, target); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

type recyclingDevice struct {
	ID             string
	HostID         string
	ImageID        string
	DockerImage    string
	DockerDigest   string
	ProviderRef    string
	ReservationID  string
	Capabilities   map[string]any
	RuntimeProfile runtimeprofile.Profile
	Lifecycle      domain.DeviceLifecycleStatus
	Health         domain.HealthStatus
	HostOnline     bool
}

type rebuildResult struct {
	Generation int `json:"generation"`
	Connection struct {
		Serial         string `json:"serial"`
		ADBEndpoint    string `json:"adb_endpoint"`
		AppiumEndpoint string `json:"appium_endpoint"`
		AppiumUDID     string `json:"appium_udid"`
	} `json:"connection"`
	Health struct {
		Online        bool `json:"online"`
		ADBOnline     bool `json:"adb_online"`
		BootCompleted bool `json:"boot_completed"`
		AppiumHealthy bool `json:"appium_healthy"`
	} `json:"health"`
}

func (controller *Controller) reconcileRecyclingDevices(ctx context.Context) (Result, error) {
	rows, err := controller.db.Pool().Query(ctx, `SELECT d.id FROM devices d
		WHERE d.device_kind='emulator' AND d.lifecycle_mode='rebuild' AND d.lifecycle_status='recycling'
		ORDER BY d.updated_at,d.id`)
	if err != nil {
		return Result{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return Result{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, err
	}
	rows.Close()
	result := Result{}
	for _, id := range ids {
		err := controller.db.WithinTx(ctx, func(tx pgx.Tx) error {
			current, err := lockRecyclingDevice(ctx, tx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			var status domain.CommandStatus
			var commandResult []byte
			var errorCode *string
			err = tx.QueryRow(ctx, `SELECT status,result,error_code FROM device_host_commands
				WHERE command_type='rebuild' AND payload->>'device_id'=$1 AND payload->>'reservation_id'=$2
				ORDER BY created_at DESC,id DESC LIMIT 1`, current.ID, current.ReservationID).
				Scan(&status, &commandResult, &errorCode)
			if errors.Is(err, pgx.ErrNoRows) {
				if !current.HostOnline {
					return nil
				}
				if err := controller.queueRecycleRebuild(ctx, tx, current); err != nil {
					return err
				}
				result.RebuildsQueued++
				return nil
			}
			if err != nil {
				return err
			}
			switch status {
			case domain.CommandPending, domain.CommandLeased:
				return nil
			case domain.CommandSucceeded:
				var value rebuildResult
				if json.Unmarshal(commandResult, &value) != nil || !validRebuildResult(value) {
					if err := controller.failRecycle(ctx, tx, current, "REBUILD_RESULT_INVALID", "rebuild command returned an incomplete readiness snapshot"); err != nil {
						return err
					}
					result.RebuildsFailed++
					return nil
				}
				if err := controller.completeRecycle(ctx, tx, current, value); err != nil {
					return err
				}
				result.RebuildsCompleted++
			default:
				code := "REBUILD_FAILED"
				if errorCode != nil && *errorCode != "" {
					code = *errorCode
				}
				if err := controller.failRecycle(ctx, tx, current, code, "rebuild command exhausted retries"); err != nil {
					return err
				}
				result.RebuildsFailed++
			}
			return nil
		})
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func lockRecyclingDevice(ctx context.Context, tx pgx.Tx, id string) (recyclingDevice, error) {
	var value recyclingDevice
	var capabilities, resourceConfig []byte
	err := tx.QueryRow(ctx, `SELECT d.id,d.host_id,d.image_id,i.docker_image,i.docker_digest,d.provider_ref,r.id,d.capabilities,i.resource_config,d.lifecycle_status,d.health_status,
		(h.status='online' AND NOT h.draining)
		FROM devices d JOIN device_hosts h ON h.id=d.host_id JOIN device_images i ON i.id=d.image_id
		JOIN LATERAL (SELECT id FROM device_reservations WHERE device_id=d.id
			AND status IN ('released','expired','force_released') ORDER BY COALESCE(released_at,updated_at) DESC,id DESC LIMIT 1) r ON true
		WHERE d.id=$1 AND d.device_kind='emulator' AND d.lifecycle_mode='rebuild' AND d.lifecycle_status='recycling'
		FOR UPDATE OF d`, id).Scan(&value.ID, &value.HostID, &value.ImageID, &value.DockerImage, &value.DockerDigest, &value.ProviderRef, &value.ReservationID,
		&capabilities, &resourceConfig, &value.Lifecycle, &value.Health, &value.HostOnline)
	if err == nil {
		err = json.Unmarshal(capabilities, &value.Capabilities)
	}
	if err == nil {
		var resources map[string]any
		if err = json.Unmarshal(resourceConfig, &resources); err == nil {
			value.RuntimeProfile, err = runtimeprofile.Parse(resources)
		}
	}
	return value, err
}

func (controller *Controller) queueRecycleRebuild(ctx context.Context, tx pgx.Tx, device recyclingDevice) error {
	commandID, err := controller.newID()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"device_id": device.ID, "host_id": device.HostID, "image_id": device.ImageID,
		"docker_image": device.DockerImage, "docker_digest": device.DockerDigest,
		"provider_ref": device.ProviderRef, "reservation_id": device.ReservationID, "capabilities": device.Capabilities,
		"runtime_profile": device.RuntimeProfile.Map()})
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(device.ID + "\x00" + device.ReservationID))
	_, err = tx.Exec(ctx, `INSERT INTO device_host_commands
		(id,host_id,command_type,payload,status,max_attempts,idempotency_key)
		VALUES($1,$2,'rebuild',$3::jsonb,'pending',3,$4)
		ON CONFLICT(host_id,idempotency_key) DO NOTHING`, commandID, device.HostID, payload, "recycle-"+hex.EncodeToString(hash[:16]))
	return err
}

func validRebuildResult(value rebuildResult) bool {
	return value.Generation > 0 && value.Connection.Serial != "" && value.Connection.ADBEndpoint != "" &&
		value.Connection.AppiumEndpoint != "" && value.Connection.AppiumUDID != "" && value.Health.Online &&
		value.Health.ADBOnline && value.Health.BootCompleted && value.Health.AppiumHealthy
}

func (controller *Controller) completeRecycle(ctx context.Context, tx pgx.Tx, current recyclingDevice, value rebuildResult) error {
	now, err := database.ClockNow(ctx, tx)
	if err != nil {
		return err
	}
	aggregate, err := domain.RestoreDevice(current.ID, current.Lifecycle, current.Health)
	if err != nil {
		return err
	}
	if aggregate.Health() != domain.HealthHealthy {
		if err := aggregate.UpdateHealth(domain.HealthHealthy, "rebuild readiness checks passed", now); err != nil {
			return err
		}
	}
	if err := aggregate.Transition(domain.DeviceReady, "rebuild removed previous run data and passed readiness checks", now); err != nil {
		return err
	}
	if err := aggregate.UpdateHealth(domain.HealthUnhealthy, domain.STFReadinessStabilizationReason, now); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE devices SET serial=$2,adb_endpoint=$3,appium_endpoint=$4,
		capabilities=jsonb_set(capabilities,'{appiumUdid}',to_jsonb($5::text),true),lifecycle_status=$6,health_status=$7,
		health_reason=$8,consecutive_failures=0,last_seen_at=$9,updated_at=$9 WHERE id=$1 AND lifecycle_status='recycling'`,
		current.ID, value.Connection.Serial, value.Connection.ADBEndpoint, value.Connection.AppiumEndpoint,
		value.Connection.AppiumUDID, aggregate.Lifecycle(), aggregate.Health(), domain.STFReadinessStabilizationReason, now)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("recycling device state changed concurrently")
	}
	return nil
}

func (controller *Controller) failRecycle(ctx context.Context, tx pgx.Tx, current recyclingDevice, code, reason string) error {
	now, err := database.ClockNow(ctx, tx)
	if err != nil {
		return err
	}
	aggregate, err := domain.RestoreDevice(current.ID, current.Lifecycle, current.Health)
	if err != nil {
		return err
	}
	if aggregate.Health() != domain.HealthUnhealthy {
		if err := aggregate.UpdateHealth(domain.HealthUnhealthy, reason, now); err != nil {
			return err
		}
	}
	if err := aggregate.Transition(domain.DeviceQuarantined, reason, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_status=$3,health_reason=$4,
		consecutive_failures=consecutive_failures+1,updated_at=$5 WHERE id=$1 AND lifecycle_status='recycling'`,
		current.ID, aggregate.Lifecycle(), aggregate.Health(), code+": "+reason, now); err != nil {
		return err
	}
	eventID, err := controller.newID()
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"error_code": code, "reservation_id": current.ReservationID})
	_, err = tx.Exec(ctx, `INSERT INTO device_health_events
		(id,device_id,source,event_type,severity,reason,payload,observed_at)
		VALUES($1,$2,'reconciler','device_rebuild_failed','error',$3,$4::jsonb,$5)`, eventID, current.ID, reason, payload, now)
	return err
}

func (controller *Controller) reconcileImageValidations(ctx context.Context) (Result, error) {
	rows, err := controller.db.Pool().Query(ctx, `SELECT id FROM device_images WHERE status='validating' ORDER BY created_at,id`)
	if err != nil {
		return Result{}, err
	}
	var imageIDs []string
	for rows.Next() {
		var imageID string
		if err := rows.Scan(&imageID); err != nil {
			rows.Close()
			return Result{}, err
		}
		imageIDs = append(imageIDs, imageID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, err
	}
	rows.Close()
	result := Result{}
	for _, imageID := range imageIDs {
		partial, err := controller.reconcileImageValidation(ctx, imageID)
		if err != nil {
			return result, err
		}
		result.ValidationsQueued += partial.ValidationsQueued
		result.ValidationsCompleted += partial.ValidationsCompleted
		result.ValidationsFailed += partial.ValidationsFailed
	}
	return result, nil
}

func (controller *Controller) reconcileImageValidation(ctx context.Context, imageID string) (Result, error) {
	result := Result{}
	err := controller.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var status domain.ImageStatus
		var runtimeImage, digest, abi, resolution string
		var apiLevel int
		var resourceConfig []byte
		var requestedAt time.Time
		if err := tx.QueryRow(ctx, `SELECT status,docker_image,docker_digest,api_level,abi,resolution,resource_config,updated_at
			FROM device_images WHERE id=$1 FOR UPDATE`, imageID).
			Scan(&status, &runtimeImage, &digest, &apiLevel, &abi, &resolution, &resourceConfig, &requestedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		if status != domain.ImageValidating {
			return nil
		}
		var commandStatus domain.CommandStatus
		var commandResult []byte
		var errorCode *string
		err := tx.QueryRow(ctx, `SELECT status,result,error_code FROM device_host_commands
			WHERE command_type='validate_image' AND payload->>'image_id'=$1 AND created_at >= $2
			ORDER BY created_at DESC,id DESC LIMIT 1`, imageID, requestedAt).
			Scan(&commandStatus, &commandResult, &errorCode)
		if err == nil {
			switch commandStatus {
			case domain.CommandPending, domain.CommandLeased:
				return nil
			case domain.CommandSucceeded:
				var value struct {
					DigestVerified bool `json:"digest_verified"`
					Ready          bool `json:"ready"`
				}
				if json.Unmarshal(commandResult, &value) == nil && value.DigestVerified && value.Ready {
					if err := transitionImage(ctx, tx, imageID, status, domain.ImageReady, nil); err != nil {
						return err
					}
					result.ValidationsCompleted++
					return nil
				}
				reason := "image validation returned an incomplete result"
				if err := transitionImage(ctx, tx, imageID, status, domain.ImageFailed, &reason); err != nil {
					return err
				}
				result.ValidationsFailed++
				return nil
			case domain.CommandFailed, domain.CommandTimedOut, domain.CommandCanceled:
				reason := "IMAGE_VALIDATION_FAILED"
				if errorCode != nil && *errorCode != "" {
					reason = *errorCode
				}
				if err := transitionImage(ctx, tx, imageID, status, domain.ImageFailed, &reason); err != nil {
					return err
				}
				result.ValidationsFailed++
				return nil
			}
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var resources map[string]any
		if err := json.Unmarshal(resourceConfig, &resources); err != nil {
			return err
		}
		profile, err := runtimeprofile.Parse(resources)
		if err != nil {
			return fmt.Errorf("invalid image runtime profile: %w", err)
		}
		hostID, err := lockHostCapacity(ctx, tx, imageID, profile)
		if errors.Is(err, ErrNoCapacity) {
			return nil
		}
		if err != nil {
			return err
		}
		commandID, err := controller.newID()
		if err != nil {
			return err
		}
		capabilities := map[string]any{"platformName": "Android", "apiLevel": apiLevel, "abi": abi, "resolution": resolution}
		for key, value := range resources {
			capabilities[key] = value
		}
		payload, err := json.Marshal(map[string]any{"image_id": imageID, "device_id": "validation-" + commandID,
			"provider_ref": "validation-" + commandID, "docker_image": runtimeImage, "docker_digest": digest,
			"capabilities": capabilities, "runtime_profile": profile.Map()})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO device_host_commands
			(id,host_id,command_type,payload,status,max_attempts,idempotency_key)
			VALUES($1,$2,'validate_image',$3::jsonb,'pending',3,$4)`, commandID, hostID, payload, "validate-"+commandID); err != nil {
			return err
		}
		result.ValidationsQueued++
		return nil
	})
	return result, err
}

func transitionImage(ctx context.Context, tx pgx.Tx, imageID string, from, to domain.ImageStatus, validationError *string) error {
	aggregate, err := domain.RestoreImage(imageID, from)
	if err != nil {
		return err
	}
	now, err := database.ClockNow(ctx, tx)
	if err != nil {
		return err
	}
	if err := aggregate.Transition(to, "image validation command completed", now); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE device_images SET status=$2,validation_error=$3,updated_at=$4 WHERE id=$1 AND status=$5`,
		imageID, aggregate.Status(), validationError, now, from)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("image validation state changed concurrently")
	}
	return nil
}

func (controller *Controller) reconcile(ctx context.Context, poolID, imageID string) (Result, error) {
	result := Result{}
	err := controller.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var minReady, maxInstances int
		var apiLevel int
		var runtimeImage, digest, abi, resolution string
		var resourceConfig []byte
		if err := tx.QueryRow(ctx, `SELECT pi.min_ready,pi.max_instances,i.docker_image,i.docker_digest,i.api_level,i.abi,i.resolution,i.resource_config
			FROM device_pool_images pi JOIN device_pools p ON p.id=pi.pool_id
			JOIN device_images i ON i.id=pi.image_id
			WHERE pi.pool_id=$1 AND pi.image_id=$2 AND pi.enabled AND p.status='active' AND i.status='ready'
			FOR UPDATE OF pi`, poolID, imageID).Scan(&minReady, &maxInstances, &runtimeImage, &digest, &apiLevel, &abi, &resolution, &resourceConfig); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		ready, invalid, err := controller.completeSuccessfulCreates(ctx, tx, poolID, imageID)
		if err != nil {
			return err
		}
		result.DevicesReady = ready
		result.DevicesFailed = invalid
		failed, err := controller.quarantineFailedCreates(ctx, tx, poolID, imageID)
		if err != nil {
			return err
		}
		result.DevicesFailed += failed
		backoff, err := creationBackoff(ctx, tx, poolID, imageID)
		if err != nil {
			return err
		}
		var activeInstances, readyOrCreating int
		if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT
			count(*) FILTER (WHERE %s),
			count(*) FILTER (WHERE d.lifecycle_status IN ('provisioning','booting','ready'))
			FROM device_pool_devices pd JOIN devices d ON d.id=pd.device_id
			JOIN device_hosts h ON h.id=d.host_id
			WHERE pd.pool_id=$1 AND pd.enabled AND d.image_id=$2 AND d.device_kind='emulator' AND d.provider_type='docker_emulator'`,
			slotOccupyingDevicePredicate), poolID, imageID).Scan(&activeInstances, &readyOrCreating); err != nil {
			return err
		}
		if activeInstances > maxInstances {
			queued, err := controller.queueScaleDown(ctx, tx, poolID, imageID, maxInstances, activeInstances-maxInstances)
			if err != nil {
				return err
			}
			result.DeletesQueued += queued
			return nil
		}
		missing := min(minReady-readyOrCreating, maxInstances-activeInstances)
		if missing <= 0 {
			return nil
		}
		if backoff {
			result.BackoffSkips++
			return nil
		}
		capabilities := map[string]any{"platformName": "Android", "apiLevel": apiLevel, "abi": abi, "resolution": resolution}
		var resources map[string]any
		if json.Unmarshal(resourceConfig, &resources) == nil {
			for key, value := range resources {
				capabilities[key] = value
			}
		}
		profile, err := runtimeprofile.Parse(resources)
		if err != nil {
			return fmt.Errorf("invalid image runtime profile: %w", err)
		}
		for range missing {
			hostID, err := lockHostCapacity(ctx, tx, imageID, profile)
			if errors.Is(err, ErrNoCapacity) {
				result.CapacityMisses++
				break
			}
			if err != nil {
				return err
			}
			if err := controller.createDeviceCommand(ctx, tx, poolID, imageID, runtimeImage, digest, hostID, capabilities, profile); err != nil {
				return err
			}
			result.DevicesCreated++
		}
		return nil
	})
	return result, err
}

func (controller *Controller) completeSuccessfulCreates(ctx context.Context, tx pgx.Tx, poolID, imageID string) (int, int, error) {
	rows, err := tx.Query(ctx, `SELECT d.id,d.lifecycle_status,d.health_status,c.result
		FROM devices d JOIN device_pool_devices pd ON pd.device_id=d.id AND pd.enabled
		JOIN LATERAL (SELECT result FROM device_host_commands WHERE command_type='create'
			AND payload->>'device_id'=d.id AND status='succeeded' ORDER BY completed_at DESC,id DESC LIMIT 1) c ON true
		WHERE pd.pool_id=$1 AND d.image_id=$2 AND d.lifecycle_status IN ('provisioning','booting')
		AND NOT EXISTS (SELECT 1 FROM device_host_commands active_rebuild
			WHERE active_rebuild.payload->>'device_id'=d.id AND active_rebuild.command_type='rebuild'
			AND active_rebuild.status IN ('pending','leased'))
		FOR UPDATE OF d`, poolID, imageID)
	if err != nil {
		return 0, 0, err
	}
	type successfulCreate struct {
		id        string
		lifecycle domain.DeviceLifecycleStatus
		health    domain.HealthStatus
		result    []byte
	}
	var values []successfulCreate
	for rows.Next() {
		var value successfulCreate
		if err := rows.Scan(&value.id, &value.lifecycle, &value.health, &value.result); err != nil {
			rows.Close()
			return 0, 0, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, err
	}
	rows.Close()
	completed, invalid := 0, 0
	for _, current := range values {
		var snapshot rebuildResult
		if json.Unmarshal(current.result, &snapshot) != nil || !validRebuildResult(snapshot) {
			if err := controller.quarantineCreateResult(ctx, tx, current.id, current.lifecycle, current.health); err != nil {
				return completed, invalid, err
			}
			invalid++
			continue
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return completed, invalid, err
		}
		aggregate, err := domain.RestoreDevice(current.id, current.lifecycle, current.health)
		if err != nil {
			return completed, invalid, err
		}
		if aggregate.Health() != domain.HealthHealthy {
			if err := aggregate.UpdateHealth(domain.HealthHealthy, "create readiness checks passed", now); err != nil {
				return completed, invalid, err
			}
		}
		if aggregate.Lifecycle() == domain.DeviceProvisioning {
			if err := aggregate.Transition(domain.DeviceBooting, "emulator started", now); err != nil {
				return completed, invalid, err
			}
		}
		if err := aggregate.Transition(domain.DeviceReady, "create readiness checks passed", now); err != nil {
			return completed, invalid, err
		}
		if err := aggregate.UpdateHealth(domain.HealthUnhealthy, domain.STFReadinessStabilizationReason, now); err != nil {
			return completed, invalid, err
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET serial=$2,adb_endpoint=$3,appium_endpoint=$4,
			capabilities=jsonb_set(capabilities,'{appiumUdid}',to_jsonb($5::text),true),lifecycle_status=$6,health_status=$7,
			health_reason=$8,consecutive_failures=0,last_seen_at=$9,updated_at=$9 WHERE id=$1`, current.id,
			snapshot.Connection.Serial, snapshot.Connection.ADBEndpoint, snapshot.Connection.AppiumEndpoint,
			snapshot.Connection.AppiumUDID, aggregate.Lifecycle(), aggregate.Health(), domain.STFReadinessStabilizationReason, now); err != nil {
			return completed, invalid, err
		}
		completed++
	}
	return completed, invalid, nil
}

func (controller *Controller) quarantineCreateResult(
	ctx context.Context,
	tx pgx.Tx,
	deviceID string,
	lifecycle domain.DeviceLifecycleStatus,
	health domain.HealthStatus,
) error {
	now, err := database.ClockNow(ctx, tx)
	if err != nil {
		return err
	}
	aggregate, err := domain.RestoreDevice(deviceID, lifecycle, health)
	if err != nil {
		return err
	}
	if aggregate.Health() != domain.HealthUnhealthy {
		if err := aggregate.UpdateHealth(domain.HealthUnhealthy, "create command returned an incomplete readiness snapshot", now); err != nil {
			return err
		}
	}
	if err := aggregate.Transition(domain.DeviceQuarantined, "create command returned an incomplete readiness snapshot", now); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_status=$3,
		health_reason='CREATE_RESULT_INVALID',consecutive_failures=consecutive_failures+1,updated_at=$4 WHERE id=$1`,
		deviceID, aggregate.Lifecycle(), aggregate.Health(), now)
	return err
}

func (controller *Controller) createDeviceCommand(ctx context.Context, tx pgx.Tx, poolID, imageID, runtimeImage, digest, hostID string, capabilities map[string]any, profile runtimeprofile.Profile) error {
	deviceID, err := controller.newID()
	if err != nil {
		return err
	}
	commandID, err := controller.newID()
	if err != nil {
		return err
	}
	providerRef := "emulator-" + deviceID
	serial := "pending-" + deviceID
	encodedCapabilities, err := json.Marshal(capabilities)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO devices
		(id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,capabilities,lifecycle_status,health_status)
		VALUES($1,$2,$3,'emulator','docker_emulator',$4,'rebuild',$5,$6::jsonb,'provisioning','unknown')`,
		deviceID, hostID, imageID, providerRef, serial, encodedCapabilities); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO device_pool_devices(pool_id,device_id,enabled) VALUES($1,$2,true)`, poolID, deviceID); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"device_id": deviceID, "image_id": imageID, "provider_ref": providerRef,
		"docker_image": runtimeImage, "docker_digest": digest, "capabilities": capabilities, "runtime_profile": profile.Map()})
	if err != nil {
		return err
	}
	keyHash := sha256.Sum256([]byte(poolID + "\x00" + imageID + "\x00" + deviceID))
	idempotencyKey := "warm-" + hex.EncodeToString(keyHash[:16])
	_, err = tx.Exec(ctx, `INSERT INTO device_host_commands
		(id,host_id,command_type,payload,status,max_attempts,idempotency_key)
		VALUES($1,$2,'create',$3::jsonb,'pending',3,$4)`, commandID, hostID, payload, idempotencyKey)
	return err
}

func lockHostCapacity(ctx context.Context, tx pgx.Tx, imageID string, requested runtimeprofile.Profile) (string, error) {
	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT h.id,h.capacity,h.used_capacity,h.last_heartbeat_at,
		COALESCE((SELECT jsonb_agg(jsonb_build_object('profile',d.capabilities,'image_id',d.image_id))
			FROM devices d WHERE d.host_id=h.id AND %s),'[]'::jsonb),
		COALESCE((SELECT jsonb_agg(c.payload) FROM device_host_commands c WHERE c.host_id=h.id
			AND c.command_type='validate_image' AND c.status IN ('pending','leased')),'[]'::jsonb),
		EXISTS (SELECT 1 FROM device_host_commands c WHERE c.host_id=h.id AND c.command_type='validate_image'
			AND c.status='succeeded' AND c.payload->>'image_id'=$1)
		FROM device_hosts h WHERE h.status='online' AND NOT h.draining AND h.host_type IN ('docker_emulator','hybrid')
		ORDER BY h.id FOR UPDATE OF h SKIP LOCKED`, slotOccupyingDevicePredicate), imageID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var hostID string
		var capacityJSON, usedJSON, deviceJSON, pendingJSON []byte
		var lastHeartbeat *time.Time
		var imageCached bool
		if err := rows.Scan(&hostID, &capacityJSON, &usedJSON, &lastHeartbeat, &deviceJSON, &pendingJSON, &imageCached); err != nil {
			return "", err
		}
		var capacityMap, usedMap map[string]any
		var devices []struct {
			Profile map[string]any `json:"profile"`
			ImageID string         `json:"image_id"`
		}
		var pending []map[string]any
		if json.Unmarshal(capacityJSON, &capacityMap) != nil || json.Unmarshal(usedJSON, &usedMap) != nil ||
			json.Unmarshal(deviceJSON, &devices) != nil || json.Unmarshal(pendingJSON, &pending) != nil {
			continue
		}
		host, dynamic := capacity.HostFromMap(capacityMap)
		if !dynamic {
			limit := jsonInt(capacityMap, "device_slots")
			used := max(len(devices), jsonInt(usedMap, "device_slots")) + len(pending)
			if limit > used {
				return hostID, nil
			}
			continue
		}
		if lastHeartbeat == nil || time.Since(*lastHeartbeat) > 30*time.Second || host.CollectedAt.IsZero() || time.Since(host.CollectedAt) > 30*time.Second {
			continue
		}
		existing := capacity.Allocation{Slots: len(devices)}
		valid := true
		for _, device := range devices {
			profile, parseErr := runtimeprofile.Parse(device.Profile)
			if parseErr != nil {
				valid = false
				break
			}
			existing.CPUCores += profile.ContainerCPUCores
			existing.MemoryMB += profile.ContainerMemoryMB
			if device.ImageID == imageID {
				imageCached = true
			}
		}
		if !valid {
			continue
		}
		pendingAllocation := capacity.Allocation{}
		for _, payload := range pending {
			profile, parseErr := runtimeprofile.Parse(mapValue(payload, "runtime_profile"))
			if parseErr != nil {
				valid = false
				break
			}
			pendingAllocation.CPUCores += profile.ContainerCPUCores
			pendingAllocation.MemoryMB += profile.ContainerMemoryMB
			pendingAllocation.DiskMB += profile.DataDiskMB + profile.ImageDiskMB
			pendingAllocation.Slots++
		}
		if valid && capacity.Evaluate(host, existing, pendingAllocation, requested, imageCached).Fits {
			return hostID, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", ErrNoCapacity
}

func jsonInt(values map[string]any, key string) int {
	if value, ok := values[key].(float64); ok {
		return int(value)
	}
	return 0
}

func mapValue(values map[string]any, key string) map[string]any {
	if result, ok := values[key].(map[string]any); ok {
		return result
	}
	return nil
}

func (controller *Controller) quarantineFailedCreates(ctx context.Context, tx pgx.Tx, poolID, imageID string) (int, error) {
	rows, err := tx.Query(ctx, `SELECT d.id,d.lifecycle_status,d.health_status
		FROM devices d JOIN device_pool_devices pd ON pd.device_id=d.id AND pd.enabled
		WHERE pd.pool_id=$1 AND d.image_id=$2 AND d.lifecycle_status='provisioning'
		AND EXISTS (SELECT 1 FROM device_host_commands c WHERE c.command_type='create'
			AND c.payload->>'device_id'=d.id AND c.status IN ('failed','timed_out'))
		FOR UPDATE OF d`, poolID, imageID)
	if err != nil {
		return 0, err
	}
	type failedDevice struct {
		id        string
		lifecycle domain.DeviceLifecycleStatus
		health    domain.HealthStatus
	}
	var devices []failedDevice
	for rows.Next() {
		var device failedDevice
		if err := rows.Scan(&device.id, &device.lifecycle, &device.health); err != nil {
			rows.Close()
			return 0, err
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, current := range devices {
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return 0, err
		}
		aggregate, err := domain.RestoreDevice(current.id, current.lifecycle, current.health)
		if err != nil {
			return 0, err
		}
		if err := aggregate.Transition(domain.DeviceQuarantined, "emulator create command exhausted retries", now); err != nil {
			return 0, err
		}
		if aggregate.Health() != domain.HealthUnhealthy {
			if err := aggregate.UpdateHealth(domain.HealthUnhealthy, "emulator create command exhausted retries", now); err != nil {
				return 0, err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_status=$3,
			health_reason='emulator create command exhausted retries',consecutive_failures=consecutive_failures+1,updated_at=$4 WHERE id=$1`,
			current.id, aggregate.Lifecycle(), aggregate.Health(), now); err != nil {
			return 0, err
		}
		eventID, err := controller.newID()
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO device_health_events
			(id,device_id,source,event_type,severity,reason,payload,observed_at)
			VALUES($1,$2,'reconciler','warm_pool_create_failed','error','emulator create command exhausted retries','{}',$3)`,
			eventID, current.id, now); err != nil {
			return 0, err
		}
	}
	return len(devices), nil
}

func creationBackoff(ctx context.Context, tx pgx.Tx, poolID, imageID string) (bool, error) {
	var failures int
	var lastFailure *time.Time
	err := tx.QueryRow(ctx, `SELECT count(*),max(c.completed_at) FROM device_host_commands c
		JOIN devices d ON d.id=c.payload->>'device_id'
		JOIN device_pool_devices pd ON pd.device_id=d.id AND pd.enabled
		WHERE pd.pool_id=$1 AND d.image_id=$2 AND c.command_type='create'
		AND c.status IN ('failed','timed_out') AND c.completed_at>clock_timestamp()-interval '1 hour'`, poolID, imageID).
		Scan(&failures, &lastFailure)
	if err != nil || failures == 0 || lastFailure == nil {
		return false, err
	}
	now, err := database.ClockNow(ctx, tx)
	if err != nil {
		return false, err
	}
	exponent := min(failures-1, 4)
	delay := 30 * time.Second * time.Duration(1<<exponent)
	return now.Before(lastFailure.Add(delay)), nil
}

func (controller *Controller) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		result, err := controller.RunOnce(ctx)
		if err != nil {
			controller.logger.Error("warm pool reconciliation failed", "error", err)
		} else if result.ValidationsQueued > 0 || result.ValidationsCompleted > 0 || result.ValidationsFailed > 0 ||
			result.DevicesCreated > 0 || result.DevicesFailed > 0 || result.CapacityMisses > 0 {
			controller.logger.Info("warm pool reconciled", "validations_queued", result.ValidationsQueued,
				"validations_completed", result.ValidationsCompleted, "validations_failed", result.ValidationsFailed,
				"created", result.DevicesCreated, "failed", result.DevicesFailed,
				"capacity_misses", result.CapacityMisses, "backoff_skips", result.BackoffSkips)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

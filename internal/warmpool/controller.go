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

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
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
		result.CapacityMisses += partial.CapacityMisses
		result.BackoffSkips += partial.BackoffSkips
	}
	return result, nil
}

type recyclingDevice struct {
	ID            string
	HostID        string
	ImageID       string
	DockerImage   string
	DockerDigest  string
	ProviderRef   string
	ReservationID string
	Capabilities  map[string]any
	Lifecycle     domain.DeviceLifecycleStatus
	Health        domain.HealthStatus
	HostOnline    bool
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
	var capabilities []byte
	err := tx.QueryRow(ctx, `SELECT d.id,d.host_id,d.image_id,i.docker_image,i.docker_digest,d.provider_ref,r.id,d.capabilities,d.lifecycle_status,d.health_status,
		(h.status='online' AND NOT h.draining)
		FROM devices d JOIN device_hosts h ON h.id=d.host_id JOIN device_images i ON i.id=d.image_id
		JOIN LATERAL (SELECT id FROM device_reservations WHERE device_id=d.id
			AND status IN ('released','expired','force_released') ORDER BY COALESCE(released_at,updated_at) DESC,id DESC LIMIT 1) r ON true
		WHERE d.id=$1 AND d.device_kind='emulator' AND d.lifecycle_mode='rebuild' AND d.lifecycle_status='recycling'
		FOR UPDATE OF d`, id).Scan(&value.ID, &value.HostID, &value.ImageID, &value.DockerImage, &value.DockerDigest, &value.ProviderRef, &value.ReservationID,
		&capabilities, &value.Lifecycle, &value.Health, &value.HostOnline)
	if err == nil {
		err = json.Unmarshal(capabilities, &value.Capabilities)
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
		"provider_ref": device.ProviderRef, "reservation_id": device.ReservationID, "capabilities": device.Capabilities})
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
	result, err := tx.Exec(ctx, `UPDATE devices SET serial=$2,adb_endpoint=$3,appium_endpoint=$4,
		capabilities=jsonb_set(capabilities,'{appiumUdid}',to_jsonb($5::text),true),lifecycle_status=$6,health_status=$7,
		health_reason=NULL,consecutive_failures=0,last_seen_at=$8,updated_at=$8 WHERE id=$1 AND lifecycle_status='recycling'`,
		current.ID, value.Connection.Serial, value.Connection.ADBEndpoint, value.Connection.AppiumEndpoint,
		value.Connection.AppiumUDID, aggregate.Lifecycle(), aggregate.Health(), now)
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
		hostID, err := lockHostCapacity(ctx, tx)
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
		var resources map[string]any
		if json.Unmarshal(resourceConfig, &resources) == nil {
			for key, value := range resources {
				capabilities[key] = value
			}
		}
		payload, err := json.Marshal(map[string]any{"image_id": imageID, "device_id": "validation-" + commandID,
			"provider_ref": "validation-" + commandID, "docker_image": runtimeImage, "docker_digest": digest, "capabilities": capabilities})
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
		for range missing {
			hostID, err := lockHostCapacity(ctx, tx)
			if errors.Is(err, ErrNoCapacity) {
				result.CapacityMisses++
				break
			}
			if err != nil {
				return err
			}
			if err := controller.createDeviceCommand(ctx, tx, poolID, imageID, runtimeImage, digest, hostID, capabilities); err != nil {
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
		if _, err := tx.Exec(ctx, `UPDATE devices SET serial=$2,adb_endpoint=$3,appium_endpoint=$4,
			capabilities=jsonb_set(capabilities,'{appiumUdid}',to_jsonb($5::text),true),lifecycle_status=$6,health_status=$7,
			health_reason=NULL,consecutive_failures=0,last_seen_at=$8,updated_at=$8 WHERE id=$1`, current.id,
			snapshot.Connection.Serial, snapshot.Connection.ADBEndpoint, snapshot.Connection.AppiumEndpoint,
			snapshot.Connection.AppiumUDID, aggregate.Lifecycle(), aggregate.Health(), now); err != nil {
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

func (controller *Controller) createDeviceCommand(ctx context.Context, tx pgx.Tx, poolID, imageID, runtimeImage, digest, hostID string, capabilities map[string]any) error {
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
		"docker_image": runtimeImage, "docker_digest": digest, "capabilities": capabilities})
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

func lockHostCapacity(ctx context.Context, tx pgx.Tx) (string, error) {
	var hostID string
	err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT h.id FROM device_hosts h
		WHERE h.status='online' AND NOT h.draining AND h.host_type IN ('docker_emulator','hybrid')
		AND CASE WHEN h.capacity->>'device_slots' ~ '^[0-9]+$' THEN (h.capacity->>'device_slots')::int ELSE 0 END >
			(GREATEST(
				(SELECT count(*) FROM devices d WHERE d.host_id=h.id AND %s),
				CASE WHEN h.used_capacity->>'device_slots' ~ '^[0-9]+$' THEN (h.used_capacity->>'device_slots')::int ELSE 0 END
			) +
			 (SELECT count(*) FROM device_host_commands c WHERE c.host_id=h.id AND c.command_type='validate_image' AND c.status IN ('pending','leased')))
		ORDER BY GREATEST(
			(SELECT count(*) FROM devices d WHERE d.host_id=h.id AND %s),
			CASE WHEN h.used_capacity->>'device_slots' ~ '^[0-9]+$' THEN (h.used_capacity->>'device_slots')::int ELSE 0 END
		),h.id
		FOR UPDATE OF h SKIP LOCKED LIMIT 1`, slotOccupyingDevicePredicate, slotOccupyingDevicePredicate)).Scan(&hostID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoCapacity
	}
	return hostID, err
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

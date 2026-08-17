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
	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
	"github.com/Ad-Quanta/alcor-device-farm/internal/phoneprofile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
	"github.com/jackc/pgx/v5"
)

var ErrNoCapacity = errors.New("没有符合条件且容量充足的 Docker 模拟器宿主机")

type CapacityUnavailableError struct {
	Result capacity.Result
}

func (value *CapacityUnavailableError) Error() string { return capacity.ChineseMessage(value.Result) }
func (value *CapacityUnavailableError) Unwrap() error { return ErrNoCapacity }

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

// ProvisionInput is the user-selected Phone configuration. The browser only
// reaches this service; the resulting Docker work remains an Agent command.
type ProvisionInput struct {
	PoolID            string
	ImageID           string
	HardwareProfileID string
	RuntimeProfile    runtimeprofile.Profile
	IdempotencyKey    string
}

type Provisioning struct {
	ID             string           `json:"id,omitempty"`
	DeviceID       string           `json:"device_id"`
	CommandID      string           `json:"command_id"`
	HostID         string           `json:"host_id"`
	Status         string           `json:"status"`
	ErrorStage     string           `json:"error_stage,omitempty"`
	ErrorCode      string           `json:"error_code,omitempty"`
	CapacityResult *capacity.Result `json:"capacity_result,omitempty"`
}

// CatalogProvisionInput is the durable, browser-independent version of a
// create request. Image preparation is deliberately not performed here: only
// the Image Catalog service may queue its fixed Build Agent command types.
type CatalogProvisionInput struct {
	ClientID          string
	IdempotencyKey    string
	PoolID            string
	CatalogID         string
	HardwareProfileID string
	RuntimeProfile    runtimeprofile.Profile
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

func (controller *Controller) CreateCatalogProvisioning(ctx context.Context, input CatalogProvisionInput) (Provisioning, bool, error) {
	if controller == nil || controller.db == nil || input.ClientID == "" || len(input.IdempotencyKey) < 8 || input.PoolID == "" || input.CatalogID == "" || input.HardwareProfileID == "" {
		return Provisioning{}, false, errors.New("invalid device provisioning request")
	}
	if _, ok := phoneprofile.Find(input.HardwareProfileID); !ok || input.RuntimeProfile.Validate() != nil {
		return Provisioning{}, false, errors.New("invalid Phone hardware profile or runtime profile")
	}
	requestJSON, err := json.Marshal(map[string]any{"pool_id": input.PoolID, "catalog_id": input.CatalogID, "hardware_profile_id": input.HardwareProfileID, "runtime_profile": input.RuntimeProfile.Map()})
	if err != nil {
		return Provisioning{}, false, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(requestJSON))
	jobID, err := controller.newID()
	if err != nil {
		return Provisioning{}, false, err
	}
	var output Provisioning
	created := false
	err = controller.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var poolStatus domain.PoolStatus
		if err := tx.QueryRow(ctx, `SELECT status FROM device_pools WHERE id=$1 AND platform='android' FOR UPDATE`, input.PoolID).Scan(&poolStatus); err != nil {
			return err
		}
		if poolStatus != domain.PoolActive {
			return errors.New("device pool is not active")
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM android_system_image_catalog WHERE id=$1`, input.CatalogID).Scan(new(string)); err != nil {
			return err
		}
		var existingHash string
		err := tx.QueryRow(ctx, `SELECT id,request_hash,status,COALESCE(device_id,''),COALESCE(command_id,''),COALESCE(error_stage,''),COALESCE(error_code,''),capacity_result
			FROM device_provisioning_jobs WHERE client_id=$1 AND idempotency_key=$2 FOR UPDATE`, input.ClientID, input.IdempotencyKey).
			Scan(&output.ID, &existingHash, &output.Status, &output.DeviceID, &output.CommandID, &output.ErrorStage, &output.ErrorCode, &output.CapacityResult)
		if err == nil {
			if existingHash != hash {
				return errors.New("device provisioning idempotency key conflicts with another request")
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO device_provisioning_jobs(id,client_id,idempotency_key,request_hash,pool_id,catalog_id,hardware_profile_id,runtime_profile)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, jobID, input.ClientID, input.IdempotencyKey, hash, input.PoolID, input.CatalogID, input.HardwareProfileID, input.RuntimeProfile.Map())
		if err != nil {
			return err
		}
		output = Provisioning{ID: jobID, Status: "preparing_image"}
		created = true
		return nil
	})
	return output, created, err
}

func (controller *Controller) AttachPreparation(ctx context.Context, jobID, preparationID string) error {
	if controller == nil || controller.db == nil || jobID == "" || preparationID == "" {
		return errors.New("invalid provisioning preparation")
	}
	result, err := controller.db.Pool().Exec(ctx, `UPDATE device_provisioning_jobs SET preparation_id=$2,updated_at=clock_timestamp()
		WHERE id=$1 AND status='preparing_image' AND preparation_id IS NULL`, jobID, preparationID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("device provisioning state changed")
	}
	return nil
}

// AttachCachedPreparation reuses any verified Image for the selected immutable
// Android catalog entry. The runtime profile belongs to the Device being
// created, not to its SDK package: requiring an exact match would download the
// same Android version whenever an administrator changes CPU, memory, display,
// or data-disk settings in the creation wizard.
func (controller *Controller) AttachCachedPreparation(ctx context.Context, jobID string) (bool, error) {
	result, err := controller.db.Pool().Exec(ctx, `UPDATE device_provisioning_jobs j SET preparation_id=(
		SELECT p.id FROM device_image_preparations p WHERE p.catalog_id=j.catalog_id
		AND p.status='cached' AND p.image_id IS NOT NULL ORDER BY p.updated_at DESC,p.id DESC LIMIT 1),updated_at=clock_timestamp()
		WHERE j.id=$1 AND j.status='preparing_image' AND j.preparation_id IS NULL AND EXISTS (
		SELECT 1 FROM device_image_preparations p WHERE p.catalog_id=j.catalog_id
		AND p.status='cached' AND p.image_id IS NOT NULL)`, jobID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

// FailCatalogProvisioning records a synchronous scheduling error so refreshes
// never leave a job that appears to be preparing forever.
func (controller *Controller) FailCatalogProvisioning(ctx context.Context, jobID, stage, code string) error {
	if controller == nil || controller.db == nil || jobID == "" {
		return errors.New("invalid device provisioning job")
	}
	_, err := controller.db.Pool().Exec(ctx, `UPDATE device_provisioning_jobs
		SET status='failed',error_stage=$2,error_code=$3,updated_at=clock_timestamp()
		WHERE id=$1 AND status NOT IN ('ready','failed')`, jobID, stage, code)
	return err
}

func (controller *Controller) GetCatalogProvisioning(ctx context.Context, id string) (Provisioning, error) {
	var output Provisioning
	err := controller.db.Pool().QueryRow(ctx, `SELECT id,COALESCE(device_id,''),COALESCE(command_id,''),status,COALESCE(error_stage,''),COALESCE(error_code,''),capacity_result
		FROM device_provisioning_jobs WHERE id=$1`, id).Scan(&output.ID, &output.DeviceID, &output.CommandID, &output.Status, &output.ErrorStage, &output.ErrorCode, &output.CapacityResult)
	return output, err
}

// ListCatalogProvisionings exposes durable creation state for a refreshed
// Console page. The SQL window keeps completed job history bounded.
func (controller *Controller) ListCatalogProvisionings(ctx context.Context, page paging.Page) (paging.Result[Provisioning], error) {
	if controller == nil || controller.db == nil {
		return paging.Result[Provisioning]{}, errors.New("warm pool database is not configured")
	}
	var total int
	if err := controller.db.Pool().QueryRow(ctx, `SELECT count(*) FROM device_provisioning_jobs`).Scan(&total); err != nil {
		return paging.Result[Provisioning]{}, err
	}
	rows, err := controller.db.Pool().Query(ctx, `SELECT id,COALESCE(device_id,''),COALESCE(command_id,''),status,COALESCE(error_stage,''),COALESCE(error_code,''),capacity_result
		FROM device_provisioning_jobs ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2`, page.Limit(), page.Offset())
	if err != nil {
		return paging.Result[Provisioning]{}, err
	}
	defer rows.Close()
	items := []Provisioning{}
	for rows.Next() {
		var item Provisioning
		if err := rows.Scan(&item.ID, &item.DeviceID, &item.CommandID, &item.Status, &item.ErrorStage, &item.ErrorCode, &item.CapacityResult); err != nil {
			return paging.Result[Provisioning]{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return paging.Result[Provisioning]{}, err
	}
	return paging.NewResult(items, page, total), nil
}

// Provision creates the persistent Device, its Pool membership and the Agent
// create command atomically. Warm-pool reconciliation retains ownership of the
// later readiness transition, so the normal ADB/STF/Appium gate is unchanged.
func (controller *Controller) Provision(ctx context.Context, input ProvisionInput) (Provisioning, error) {
	if controller == nil || controller.db == nil || input.PoolID == "" || input.ImageID == "" || len(input.IdempotencyKey) < 8 {
		return Provisioning{}, errors.New("invalid device provisioning request")
	}
	if err := input.RuntimeProfile.Validate(); err != nil {
		return Provisioning{}, err
	}
	profile, ok := phoneprofile.Find(input.HardwareProfileID)
	if !ok {
		return Provisioning{}, errors.New("unknown Phone hardware profile")
	}
	result := Provisioning{Status: "provisioning"}
	err := controller.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var existingDeviceID, existingCommandID, existingHostID string
		err := tx.QueryRow(ctx, `SELECT payload->>'device_id',id,host_id FROM device_host_commands
			WHERE command_type='create' AND payload->>'provisioning_key'=$1 ORDER BY created_at DESC LIMIT 1`, input.IdempotencyKey).
			Scan(&existingDeviceID, &existingCommandID, &existingHostID)
		if err == nil {
			result.ID, result.DeviceID, result.CommandID, result.HostID = existingCommandID, existingDeviceID, existingCommandID, existingHostID
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var status domain.PoolStatus
		if err := tx.QueryRow(ctx, `SELECT status FROM device_pools WHERE id=$1 AND platform='android' FOR UPDATE`, input.PoolID).Scan(&status); err != nil {
			return err
		}
		// The Pool row serializes concurrent create submissions. Recheck after
		// acquiring it so two identical browser retries cannot both add target.
		err = tx.QueryRow(ctx, `SELECT payload->>'device_id',id,host_id FROM device_host_commands
			WHERE command_type='create' AND payload->>'provisioning_key'=$1 ORDER BY created_at DESC LIMIT 1`, input.IdempotencyKey).
			Scan(&existingDeviceID, &existingCommandID, &existingHostID)
		if err == nil {
			result.ID, result.DeviceID, result.CommandID, result.HostID = existingCommandID, existingDeviceID, existingCommandID, existingHostID
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if status != domain.PoolActive {
			return errors.New("device pool is not active")
		}
		var runtimeImage, digest string
		var api int
		var abi string
		if err := tx.QueryRow(ctx, `SELECT docker_image,docker_digest,api_level,abi FROM device_images WHERE id=$1 AND status='ready' FOR UPDATE`, input.ImageID).
			Scan(&runtimeImage, &digest, &api, &abi); err != nil {
			return err
		}
		hostID, err := lockHostCapacity(ctx, tx, input.ImageID, input.RuntimeProfile)
		if err != nil {
			return err
		}
		capabilities := map[string]any{
			"platformName": "Android", "apiLevel": api, "abi": abi,
			"resolution":          fmt.Sprintf("%dx%d", input.RuntimeProfile.Width, input.RuntimeProfile.Height),
			"hardware_profile_id": profile.ID, "hardware_profile_name": profile.Name,
			"avd_device": profile.Name,
		}
		for key, value := range input.RuntimeProfile.Map() {
			capabilities[key] = value
		}
		deviceID, commandID, err := controller.createDeviceCommand(ctx, tx, input.PoolID, input.ImageID, runtimeImage, digest, hostID, capabilities, input.RuntimeProfile, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE device_pools SET total_target=total_target+1,
			base_device_id=COALESCE(base_device_id,$2),updated_at=clock_timestamp() WHERE id=$1`, input.PoolID, deviceID); err != nil {
			return err
		}
		result.ID, result.DeviceID, result.CommandID, result.HostID = commandID, deviceID, commandID, hostID
		return nil
	})
	return result, err
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
	if err := controller.reconcileCatalogProvisioningJobs(ctx); err != nil {
		return result, err
	}
	rows, err := controller.db.Pool().Query(ctx, `SELECT p.id,COALESCE(b.image_id,p.default_image_id)
		FROM device_pools p
		LEFT JOIN devices b ON b.id=p.base_device_id AND b.lifecycle_status<>'deleted'
		JOIN device_pool_images pi ON pi.pool_id=p.id AND pi.image_id=COALESCE(b.image_id,p.default_image_id) AND pi.enabled
		JOIN device_images i ON i.id=COALESCE(b.image_id,p.default_image_id) AND i.status='ready'
		WHERE p.status='active' AND p.platform='android' ORDER BY p.id`)
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

func (controller *Controller) reconcileCatalogProvisioningJobs(ctx context.Context) error {
	rows, err := controller.db.Pool().Query(ctx, `SELECT j.id,j.pool_id,j.hardware_profile_id,j.runtime_profile,
		COALESCE(j.preparation_id,''),COALESCE(j.image_id,''),COALESCE(j.device_id,''),j.status,
		COALESCE(p.status,''),COALESCE(p.image_id,''),COALESCE(p.error_code,'')
		FROM device_provisioning_jobs j
		LEFT JOIN device_image_preparations p ON p.id=j.preparation_id
		WHERE j.status NOT IN ('ready','failed') ORDER BY j.created_at,j.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type job struct {
		id, pool, hardware, preparation, image, device, status, preparationStatus, preparedImage, errorCode string
		profile                                                                                             map[string]any
	}
	jobs := []job{}
	for rows.Next() {
		var item job
		var profile []byte
		if err := rows.Scan(&item.id, &item.pool, &item.hardware, &profile, &item.preparation, &item.image, &item.device, &item.status, &item.preparationStatus, &item.preparedImage, &item.errorCode); err != nil {
			return err
		}
		if err := json.Unmarshal(profile, &item.profile); err != nil {
			return err
		}
		jobs = append(jobs, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range jobs {
		if item.preparation == "" {
			continue
		} // API will attach the catalog job immediately after it is queued.
		if item.preparationStatus == "failed" {
			_, err := controller.db.Pool().Exec(ctx, `UPDATE device_provisioning_jobs SET status='failed',error_stage='prepare_system_image',error_code=$2,updated_at=clock_timestamp() WHERE id=$1 AND status<>'failed'`, item.id, item.errorCode)
			if err != nil {
				return err
			}
			continue
		}
		if item.preparationStatus != "cached" || item.preparedImage == "" {
			continue
		}
		if item.device == "" {
			profile, err := runtimeprofile.Parse(item.profile)
			if err != nil {
				_, updateErr := controller.db.Pool().Exec(ctx, `UPDATE device_provisioning_jobs SET status='failed',error_stage='runtime_profile',error_code='INVALID_RUNTIME_PROFILE',updated_at=clock_timestamp() WHERE id=$1`, item.id)
				if updateErr != nil {
					return updateErr
				}
				continue
			}
			created, err := controller.Provision(ctx, ProvisionInput{PoolID: item.pool, ImageID: item.preparedImage, HardwareProfileID: item.hardware, RuntimeProfile: profile, IdempotencyKey: "catalog-provision-" + item.id})
			if err != nil {
				var capacityError *CapacityUnavailableError
				if errors.As(err, &capacityError) {
					resultJSON, marshalErr := json.Marshal(capacityError.Result)
					if marshalErr != nil {
						return marshalErr
					}
					_, updateErr := controller.db.Pool().Exec(ctx, `UPDATE device_provisioning_jobs
						SET status='waiting_capacity',error_stage='host_capacity',error_code='DEVICE_CAPACITY_UNAVAILABLE',capacity_result=$2::jsonb,updated_at=clock_timestamp()
						WHERE id=$1 AND device_id IS NULL`, item.id, resultJSON)
					if updateErr != nil {
						return updateErr
					}
				}
				continue
			} // Capacity/build-agent recovery is retried by the durable job.
			_, err = controller.db.Pool().Exec(ctx, `UPDATE device_provisioning_jobs SET image_id=$2,device_id=$3,command_id=$4,status='creating_emulator',error_stage=NULL,error_code=NULL,capacity_result=NULL,updated_at=clock_timestamp() WHERE id=$1`, item.id, item.preparedImage, created.DeviceID, created.CommandID)
			if err != nil {
				return err
			}
			continue
		}
		var lifecycle domain.DeviceLifecycleStatus
		var health domain.HealthStatus
		err := controller.db.Pool().QueryRow(ctx, `SELECT lifecycle_status,health_status FROM devices WHERE id=$1`, item.device).Scan(&lifecycle, &health)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		status, stage := "creating_emulator", ""
		if lifecycle == domain.DeviceReady && health == domain.HealthHealthy {
			status = "ready"
		}
		if lifecycle == domain.DeviceQuarantined {
			status, stage = "failed", "device_readiness"
		}
		if lifecycle == domain.DeviceBooting {
			status = "adb_check"
		}
		_, err = controller.db.Pool().Exec(ctx, `UPDATE device_provisioning_jobs SET status=$2,error_stage=CASE WHEN $3='' THEN NULL ELSE $3 END,updated_at=clock_timestamp() WHERE id=$1`, item.id, status, stage)
		if err != nil {
			return err
		}
	}
	return nil
}

type scaleDownDevice struct {
	ID           string
	HostID       string
	ImageID      string
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
	poolID string,
	target, excess int,
) (int, error) {
	queued := 0
	for queued < excess {
		var current scaleDownDevice
		err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT d.id,d.host_id,d.image_id,d.provider_ref,d.lifecycle_status,d.health_status,
			EXISTS (SELECT 1 FROM device_reservations r WHERE r.device_id=d.id AND r.status='active'),
			EXISTS (SELECT 1 FROM device_host_commands c WHERE c.payload->>'device_id'=d.id AND c.status IN ('pending','leased')),
			EXISTS (SELECT 1 FROM device_pool_devices other WHERE other.device_id=d.id AND other.enabled AND other.pool_id<>$1)
			FROM device_pool_devices pd JOIN devices d ON d.id=pd.device_id
			JOIN device_hosts h ON h.id=d.host_id
			WHERE pd.pool_id=$1 AND pd.enabled AND d.device_kind='emulator'
			AND d.provider_type='docker_emulator' AND %s
			AND d.lifecycle_status IN ('ready','stopped','quarantined')
			AND NOT EXISTS (SELECT 1 FROM device_reservations active_use
				WHERE active_use.device_id=d.id AND active_use.status='active')
			AND NOT EXISTS (SELECT 1 FROM device_host_commands active_command
				WHERE active_command.payload->>'device_id'=d.id AND active_command.status IN ('pending','leased'))
			AND NOT EXISTS (SELECT 1 FROM device_pool_devices shared_membership
				WHERE shared_membership.device_id=d.id AND shared_membership.enabled AND shared_membership.pool_id<>$1)
			ORDER BY d.created_at,d.id FOR UPDATE OF d,pd SKIP LOCKED LIMIT 1`, slotOccupyingDevicePredicate),
			poolID).Scan(&current.ID, &current.HostID, &current.ImageID, &current.ProviderRef, &current.Lifecycle,
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
			"device_id": current.ID, "pool_id": poolID, "image_id": current.ImageID, "provider_ref": current.ProviderRef,
			"target_instances": target})
		if err != nil {
			return queued, err
		}
		hash := sha256.Sum256([]byte(poolID + "\x00" + current.ImageID + "\x00" + current.ID + "\x00" + fmt.Sprint(target)))
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
			commandID, poolID, current.ImageID, target); err != nil {
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
		var minReady, totalTarget int
		var apiLevel int
		var runtimeImage, digest, abi, resolution string
		var baseCapabilities, baseRuntimeProfile []byte
		if err := tx.QueryRow(ctx, `SELECT p.min_ready,p.total_target,i.docker_image,i.docker_digest,i.api_level,i.abi,i.resolution,
			COALESCE(b.capabilities,jsonb_build_object('platformName','Android','apiLevel',i.api_level,'abi',i.abi,'resolution',i.resolution)),
			COALESCE(b.runtime_profile_override,i.resource_config,'{}'::jsonb)
			FROM device_pools p LEFT JOIN devices b ON b.id=p.base_device_id AND b.lifecycle_status<>'deleted'
			JOIN device_pool_images pi ON pi.pool_id=p.id AND pi.image_id=COALESCE(b.image_id,p.default_image_id)
			JOIN device_images i ON i.id=COALESCE(b.image_id,p.default_image_id)
			WHERE p.id=$1 AND p.platform='android' AND COALESCE(b.image_id,p.default_image_id)=$2 AND pi.enabled AND p.status='active' AND i.status='ready'
			FOR UPDATE OF p`, poolID, imageID).Scan(&minReady, &totalTarget, &runtimeImage, &digest, &apiLevel, &abi, &resolution, &baseCapabilities, &baseRuntimeProfile); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		capabilities := map[string]any{}
		if err := json.Unmarshal(baseCapabilities, &capabilities); err != nil {
			return fmt.Errorf("invalid base device capabilities: %w", err)
		}
		for key, value := range map[string]any{"platformName": "Android", "apiLevel": apiLevel, "abi": abi, "resolution": resolution} {
			if _, exists := capabilities[key]; !exists {
				capabilities[key] = value
			}
		}
		capabilitiesJSON, err := json.Marshal(capabilities)
		if err != nil {
			return err
		}
		ready, invalid, err := controller.completeSuccessfulCreates(ctx, tx, poolID)
		if err != nil {
			return err
		}
		result.DevicesReady = ready
		result.DevicesFailed = invalid
		failed, err := controller.quarantineFailedCreates(ctx, tx, poolID)
		if err != nil {
			return err
		}
		result.DevicesFailed += failed
		backoff, err := creationBackoff(ctx, tx, poolID)
		if err != nil {
			return err
		}
		var activeInstances, readyOrCreating, defaultReadyOrCreating int
		if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT
			count(*) FILTER (WHERE %s),
			count(*) FILTER (WHERE d.lifecycle_status IN ('provisioning','booting','ready')),
			count(*) FILTER (WHERE d.image_id=$2 AND d.lifecycle_status IN ('provisioning','booting','ready'))
			FROM device_pool_devices pd JOIN devices d ON d.id=pd.device_id
			JOIN device_hosts h ON h.id=d.host_id
			WHERE pd.pool_id=$1 AND pd.enabled AND d.device_kind='emulator' AND d.provider_type='docker_emulator'`,
			slotOccupyingDevicePredicate), poolID, imageID).Scan(&activeInstances, &readyOrCreating, &defaultReadyOrCreating); err != nil {
			return err
		}
		if activeInstances > totalTarget {
			queued, err := controller.queueScaleDown(ctx, tx, poolID, totalTarget, activeInstances-totalTarget)
			if err != nil {
				return err
			}
			result.DeletesQueued += queued
			return nil
		}
		var pendingDemand int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM device_reservations
			WHERE pool_id=$1 AND status='pending'
			AND NOT requested_capabilities ? '_device_farm_target_device_id'
			AND $2::jsonb @> device_schedulable_capabilities(requested_capabilities)
			AND (NOT requested_capabilities ? 'platformName' OR lower(requested_capabilities->>'platformName')='android')`, poolID, capabilitiesJSON).Scan(&pendingDemand); err != nil {
			return err
		}
		missing := min(max(minReady-readyOrCreating, pendingDemand-defaultReadyOrCreating), totalTarget-activeInstances)
		if missing <= 0 {
			return nil
		}
		if backoff {
			result.BackoffSkips++
			return nil
		}
		var baseProfile map[string]any
		if err := json.Unmarshal(baseRuntimeProfile, &baseProfile); err != nil {
			return fmt.Errorf("decode base device runtime profile: %w", err)
		}
		profile, err := runtimeprofile.Parse(baseProfile)
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
			if _, _, err := controller.createDeviceCommand(ctx, tx, poolID, imageID, runtimeImage, digest, hostID, capabilities, profile, ""); err != nil {
				return err
			}
			result.DevicesCreated++
		}
		return nil
	})
	return result, err
}

func (controller *Controller) completeSuccessfulCreates(ctx context.Context, tx pgx.Tx, poolID string) (int, int, error) {
	rows, err := tx.Query(ctx, `SELECT d.id,d.lifecycle_status,d.health_status,c.result
		FROM devices d JOIN device_pool_devices pd ON pd.device_id=d.id AND pd.enabled
		JOIN LATERAL (SELECT result FROM device_host_commands WHERE command_type='create'
			AND payload->>'device_id'=d.id AND status='succeeded' ORDER BY completed_at DESC,id DESC LIMIT 1) c ON true
		WHERE pd.pool_id=$1 AND d.lifecycle_status IN ('provisioning','booting')
		AND NOT EXISTS (SELECT 1 FROM device_host_commands active_rebuild
			WHERE active_rebuild.payload->>'device_id'=d.id AND active_rebuild.command_type='rebuild'
			AND active_rebuild.status IN ('pending','leased'))
		FOR UPDATE OF d`, poolID)
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

func (controller *Controller) createDeviceCommand(ctx context.Context, tx pgx.Tx, poolID, imageID, runtimeImage, digest, hostID string, capabilities map[string]any, profile runtimeprofile.Profile, provisioningKey string) (string, string, error) {
	deviceID, err := controller.newID()
	if err != nil {
		return "", "", err
	}
	commandID, err := controller.newID()
	if err != nil {
		return "", "", err
	}
	providerRef := "emulator-" + deviceID
	serial := "pending-" + deviceID
	encodedCapabilities, err := json.Marshal(capabilities)
	if err != nil {
		return "", "", err
	}
	encodedProfile, err := json.Marshal(profile.Map())
	if err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO devices
		(id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,capabilities,runtime_profile_override,lifecycle_status,health_status)
		VALUES($1,$2,$3,'emulator','docker_emulator',$4,'clean',$5,$6::jsonb,$7::jsonb,'provisioning','unknown')`,
		deviceID, hostID, imageID, providerRef, serial, encodedCapabilities, encodedProfile); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO device_pool_devices(pool_id,device_id,enabled) VALUES($1,$2,true)`, poolID, deviceID); err != nil {
		return "", "", err
	}
	payloadMap := map[string]any{"device_id": deviceID, "image_id": imageID, "provider_ref": providerRef,
		"docker_image": runtimeImage, "docker_digest": digest, "capabilities": capabilities, "runtime_profile": profile.Map()}
	if provisioningKey != "" {
		payloadMap["provisioning_key"] = provisioningKey
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return "", "", err
	}
	keyHash := sha256.Sum256([]byte(poolID + "\x00" + imageID + "\x00" + deviceID))
	idempotencyKey := "warm-" + hex.EncodeToString(keyHash[:16])
	if provisioningKey != "" {
		provisioningHash := sha256.Sum256([]byte(provisioningKey))
		idempotencyKey = "provision-" + hex.EncodeToString(provisioningHash[:16])
	}
	_, err = tx.Exec(ctx, `INSERT INTO device_host_commands
		(id,host_id,command_type,payload,status,max_attempts,idempotency_key)
		VALUES($1,$2,'create',$3::jsonb,'pending',3,$4)`, commandID, hostID, payload, idempotencyKey)
	return deviceID, commandID, err
}

func lockHostCapacity(ctx context.Context, tx pgx.Tx, imageID string, requested runtimeprofile.Profile) (string, error) {
	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT h.id,h.capacity,h.used_capacity,h.last_heartbeat_at,
		COALESCE((SELECT jsonb_agg(jsonb_build_object('profile',COALESCE(d.runtime_profile_override,d.capabilities),'image_id',d.image_id))
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
	var bestResult *capacity.Result
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
			result := capacity.Result{Limiting: "device_slots", Shortfall: map[string]int64{"device_slots": 1}}
			bestResult = betterCapacityResult(bestResult, result)
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
		if valid {
			result := capacity.Evaluate(host, existing, pendingAllocation, requested, imageCached)
			if result.Fits {
				return hostID, nil
			}
			bestResult = betterCapacityResult(bestResult, result)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if bestResult != nil {
		return "", &CapacityUnavailableError{Result: *bestResult}
	}
	return "", &CapacityUnavailableError{Result: capacity.Result{}}
}

func betterCapacityResult(current *capacity.Result, candidate capacity.Result) *capacity.Result {
	if current == nil {
		value := candidate
		return &value
	}
	currentKinds, candidateKinds := len(current.Shortfall), len(candidate.Shortfall)
	currentTotal, candidateTotal := int64(0), int64(0)
	for _, value := range current.Shortfall {
		currentTotal += value
	}
	for _, value := range candidate.Shortfall {
		candidateTotal += value
	}
	if candidateKinds < currentKinds || (candidateKinds == currentKinds && candidateTotal < currentTotal) {
		value := candidate
		return &value
	}
	return current
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

func (controller *Controller) quarantineFailedCreates(ctx context.Context, tx pgx.Tx, poolID string) (int, error) {
	rows, err := tx.Query(ctx, `SELECT d.id,d.lifecycle_status,d.health_status
		FROM devices d JOIN device_pool_devices pd ON pd.device_id=d.id AND pd.enabled
		WHERE pd.pool_id=$1 AND d.lifecycle_status='provisioning'
		AND EXISTS (SELECT 1 FROM device_host_commands c WHERE c.command_type='create'
			AND c.payload->>'device_id'=d.id AND c.status IN ('failed','timed_out'))
		FOR UPDATE OF d`, poolID)
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

func creationBackoff(ctx context.Context, tx pgx.Tx, poolID string) (bool, error) {
	var failures int
	var lastFailure *time.Time
	err := tx.QueryRow(ctx, `SELECT count(*),max(c.completed_at) FROM device_host_commands c
		JOIN devices d ON d.id=c.payload->>'device_id'
		JOIN device_pool_devices pd ON pd.device_id=d.id AND pd.enabled
		WHERE pd.pool_id=$1 AND c.command_type='create'
		AND c.status IN ('failed','timed_out') AND c.completed_at>clock_timestamp()-interval '1 hour'`, poolID).
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

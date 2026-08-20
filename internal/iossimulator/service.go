package iossimulator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/appiumdevicefarm"
	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/capacity"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/Ad-Quanta/alcor-device-farm/internal/sensitive"
	"github.com/jackc/pgx/v5"
)

const (
	minimumAvailableMemoryMB = int64(4096)
	minimumAvailableDiskMB   = int64(16384)
)

var (
	ErrInvalidArgument = errors.New("iOS Simulator 创建参数无效")
	ErrNotFound        = errors.New("未找到指定的 iOS Simulator 宿主机或设备池")
	ErrConflict        = errors.New("iOS Simulator 创建请求与当前资源状态冲突")
	ErrCapacity        = errors.New("宿主机资源不足，暂时不能创建新的 iOS Simulator")
	ErrTargetSatisfied = errors.New("iOS 设备池已经达到目标数量")
)

type CapacityError struct{ Result capacity.Result }

func (value *CapacityError) Error() string { return capacity.ChineseMessage(value.Result) }
func (value *CapacityError) Unwrap() error { return ErrCapacity }

type Catalog struct {
	HostID      string                                 `json:"host_id"`
	Runtimes    []appiumdevicefarm.SimulatorRuntime    `json:"runtimes"`
	DeviceTypes []appiumdevicefarm.SimulatorDeviceType `json:"device_types"`
}

type CreateInput struct {
	HostID       string `json:"host_id"`
	PoolID       string `json:"pool_id"`
	RuntimeID    string `json:"runtime_id"`
	DeviceTypeID string `json:"device_type_id"`
	DisplayName  string `json:"display_name,omitempty"`
	Reason       string `json:"reason"`
}

type Operation struct {
	DeviceID  string `json:"device_id"`
	CommandID string `json:"command_id"`
	HostID    string `json:"host_id"`
	PoolID    string `json:"pool_id"`
	Status    string `json:"status"`
}

type Service struct {
	db    *database.DB
	newID func() (string, error)
}

func New(db *database.DB, generator func() (string, error)) *Service {
	if generator == nil {
		generator = identifier.New
	}
	return &Service{db: db, newID: generator}
}

func (service *Service) Catalog(ctx context.Context, hostID string) (Catalog, error) {
	if service == nil || service.db == nil || strings.TrimSpace(hostID) == "" {
		return Catalog{}, ErrInvalidArgument
	}
	var capabilities []byte
	var status, hostOS, hostType string
	var draining bool
	var heartbeat *time.Time
	err := service.db.Pool().QueryRow(ctx, `SELECT capabilities,status,draining,host_os,host_type,last_heartbeat_at FROM device_hosts WHERE id=$1`, hostID).
		Scan(&capabilities, &status, &draining, &hostOS, &hostType, &heartbeat)
	if errors.Is(err, pgx.ErrNoRows) {
		return Catalog{}, ErrNotFound
	}
	if err != nil {
		return Catalog{}, err
	}
	if status != "online" || draining || hostOS != "macos" || hostType != "appium_device_farm_ios" || heartbeat == nil || time.Since(*heartbeat) > 45*time.Second {
		return Catalog{}, ErrConflict
	}
	catalog, err := catalogFromCapabilities(capabilities)
	if err != nil || len(catalog.Runtimes) == 0 || len(catalog.DeviceTypes) == 0 {
		return Catalog{}, ErrConflict
	}
	catalog.HostID = hostID
	return catalog, nil
}

func (service *Service) Create(ctx context.Context, actor audit.Actor, requestID, idempotencyKey string, input CreateInput) (Operation, error) {
	return service.create(ctx, actor, requestID, idempotencyKey, input, true)
}

func (service *Service) create(ctx context.Context, actor audit.Actor, requestID, idempotencyKey string, input CreateInput, increaseTarget bool) (Operation, error) {
	if service == nil || service.db == nil || !actor.Valid() || len(strings.TrimSpace(idempotencyKey)) < 8 ||
		strings.TrimSpace(requestID) == "" || strings.TrimSpace(input.HostID) == "" || strings.TrimSpace(input.PoolID) == "" ||
		strings.TrimSpace(input.RuntimeID) == "" || strings.TrimSpace(input.DeviceTypeID) == "" || strings.TrimSpace(input.Reason) == "" ||
		utf8.RuneCountInString(input.RuntimeID) > 255 || utf8.RuneCountInString(input.DeviceTypeID) > 255 ||
		utf8.RuneCountInString(strings.TrimSpace(input.DisplayName)) > 128 || utf8.RuneCountInString(strings.TrimSpace(input.Reason)) > 500 {
		return Operation{}, ErrInvalidArgument
	}
	requestBytes, err := json.Marshal(map[string]string{"host_id": input.HostID, "pool_id": input.PoolID,
		"runtime_id": input.RuntimeID, "device_type_id": input.DeviceTypeID, "display_name": strings.TrimSpace(input.DisplayName)})
	if err != nil {
		return Operation{}, err
	}
	requestDigest := sha256.Sum256(requestBytes)
	requestHash := hex.EncodeToString(requestDigest[:])
	commandKeyHash := sha256.Sum256([]byte(actor.ClientID + "|" + strings.TrimSpace(idempotencyKey)))
	commandKey := "ios-create-" + hex.EncodeToString(commandKeyHash[:16])
	deviceID, err := service.newID()
	if err != nil {
		return Operation{}, err
	}
	commandID, err := service.newID()
	if err != nil {
		return Operation{}, err
	}
	auditID, err := service.newID()
	if err != nil {
		return Operation{}, err
	}
	result := Operation{}
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, commandKey); err != nil {
			return err
		}
		var existingDevice, existingCommand, existingHost, existingPool, existingHash string
		existingErr := tx.QueryRow(ctx, `SELECT payload->>'device_id',id,host_id,payload->>'pool_id',payload->>'request_hash'
			FROM device_host_commands WHERE idempotency_key=$1 ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, commandKey).
			Scan(&existingDevice, &existingCommand, &existingHost, &existingPool, &existingHash)
		if existingErr == nil {
			if existingHash != requestHash {
				return ErrConflict
			}
			result = Operation{DeviceID: existingDevice, CommandID: existingCommand, HostID: existingHost, PoolID: existingPool, Status: "creating_simulator"}
			return nil
		}
		if !errors.Is(existingErr, pgx.ErrNoRows) {
			return existingErr
		}

		var capabilities, capacity, usedCapacity []byte
		var hostStatus, hostOS, hostType string
		var draining bool
		var heartbeat *time.Time
		if err := tx.QueryRow(ctx, `SELECT capabilities,capacity,used_capacity,status,draining,host_os,host_type,last_heartbeat_at
			FROM device_hosts WHERE id=$1 FOR UPDATE`, input.HostID).
			Scan(&capabilities, &capacity, &usedCapacity, &hostStatus, &draining, &hostOS, &hostType, &heartbeat); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if hostStatus != "online" || draining || hostOS != "macos" || hostType != "appium_device_farm_ios" || heartbeat == nil || time.Since(*heartbeat) > 45*time.Second {
			return ErrConflict
		}
		catalog, err := catalogFromCapabilities(capabilities)
		if err != nil || !catalogHasRuntime(catalog, input.RuntimeID) || !catalogHasDeviceType(catalog, input.DeviceTypeID) ||
			!catalogSupportsDeviceType(catalog, input.RuntimeID, input.DeviceTypeID) {
			return ErrInvalidArgument
		}
		var poolStatus, platform string
		var totalTarget int
		if err := tx.QueryRow(ctx, `SELECT status,platform,total_target FROM device_pools WHERE id=$1 FOR UPDATE`, input.PoolID).Scan(&poolStatus, &platform, &totalTarget); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if poolStatus != "active" || platform != "ios" {
			return ErrConflict
		}
		if !increaseTarget {
			var current int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM device_pool_devices pd
				JOIN devices d ON d.id=pd.device_id
				WHERE pd.pool_id=$1 AND pd.enabled AND d.platform='ios' AND d.device_kind='simulator'
				AND d.provider_type='appium_device_farm_ios' AND d.lifecycle_status<>'deleted'`, input.PoolID).Scan(&current); err != nil {
				return err
			}
			if current >= totalTarget {
				return ErrTargetSatisfied
			}
		}
		var registeredSlots, pendingSlots int64
		if err := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE lifecycle_status IN ('provisioning','booting'))
			FROM devices WHERE host_id=$1 AND lifecycle_status NOT IN ('deleted','quarantined')`, input.HostID).
			Scan(&registeredSlots, &pendingSlots); err != nil {
			return err
		}
		capacityResult := evaluateHostCapacity(capacity, usedCapacity, registeredSlots, pendingSlots)
		if !capacityResult.Fits {
			return &CapacityError{Result: capacityResult}
		}
		displayName := strings.TrimSpace(input.DisplayName)
		if displayName == "" {
			displayName = catalogDeviceTypeName(catalog, input.DeviceTypeID)
		}
		deviceCapabilities := map[string]any{"platformName": "iOS", "automationName": "XCUITest", "deviceClass": "phone", "realDevice": false,
			"runtimeId": input.RuntimeID, "deviceTypeId": input.DeviceTypeID, "model": catalogDeviceTypeName(catalog, input.DeviceTypeID), "deviceName": displayName}
		placeholder := "pending:" + deviceID
		if _, err := tx.Exec(ctx, `INSERT INTO devices(id,host_id,platform,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,
			capabilities,lifecycle_status,health_status) VALUES($1,$2,'ios',NULL,'simulator','appium_device_farm_ios',$3,'rebuild',$3,$4::jsonb,'provisioning','unknown')`,
			deviceID, input.HostID, placeholder, deviceCapabilities); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO device_pool_devices(pool_id,device_id,enabled) VALUES($1,$2,true)`, input.PoolID, deviceID); err != nil {
			return err
		}
		payload := map[string]any{"operation_source": "management", "operation_state": "provisioning", "operation_kind": "ios_simulator_create",
			"device_id": deviceID, "host_id": input.HostID, "pool_id": input.PoolID, "provider_ref": placeholder,
			"platform": "ios", "device_kind": "simulator", "capabilities": deviceCapabilities, "request_hash": requestHash}
		command, err := (repository.CommandRepository{}).Create(ctx, tx, repository.CreateCommandParams{ID: commandID, HostID: input.HostID,
			CommandType: "create", Payload: payload, MaxAttempts: 3, IdempotencyKey: commandKey})
		if err != nil {
			return err
		}
		if increaseTarget {
			if _, err := tx.Exec(ctx, `UPDATE device_pools SET total_target=total_target+1,
				max_concurrency=GREATEST(max_concurrency,total_target+1),updated_at=clock_timestamp() WHERE id=$1`, input.PoolID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO device_audit_events(id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
			VALUES($1,$2,$3,'create_ios_simulator','device',$4,$5,$6,jsonb_build_object('host_id',$7::text,'pool_id',$8::text,'runtime_id',$9::text,'device_type_id',$10::text,'command_id',$11::text))`,
			auditID, actor.Type, actor.ID, deviceID, requestID, sensitive.RedactText(input.Reason), input.HostID, input.PoolID, input.RuntimeID, input.DeviceTypeID, command.ID); err != nil {
			return err
		}
		result = Operation{DeviceID: deviceID, CommandID: command.ID, HostID: input.HostID, PoolID: input.PoolID, Status: "creating_simulator"}
		return nil
	})
	return result, err
}

// ReconcileScaleUp fills active iOS pools to their configured total target.
// It reuses a selected, healthy Simulator only as an immutable Host/Runtime/
// device-type template; CoreSimulator data is never cloned.
func (service *Service) ReconcileScaleUp(ctx context.Context) (created, capacityMisses int, err error) {
	if service == nil || service.db == nil {
		return 0, 0, errors.New("iOS 设备池伸缩服务尚未配置")
	}
	rows, err := service.db.Pool().Query(ctx, `SELECT id FROM device_pools
		WHERE platform='ios' AND status='active' AND base_device_id IS NOT NULL
		AND total_target>(SELECT count(*) FROM device_pool_devices pd JOIN devices d ON d.id=pd.device_id
			WHERE pd.pool_id=device_pools.id AND pd.enabled AND d.platform='ios'
			AND d.device_kind='simulator' AND d.provider_type='appium_device_farm_ios'
			AND d.lifecycle_status<>'deleted')
		ORDER BY id`)
	if err != nil {
		return 0, 0, err
	}
	var poolIDs []string
	for rows.Next() {
		var poolID string
		if err := rows.Scan(&poolID); err != nil {
			rows.Close()
			return created, capacityMisses, err
		}
		poolIDs = append(poolIDs, poolID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return created, capacityMisses, err
	}
	rows.Close()

	for _, poolID := range poolIDs {
		for {
			input, templateErr := service.scaleTemplate(ctx, poolID)
			if errors.Is(templateErr, ErrConflict) || errors.Is(templateErr, ErrNotFound) {
				break
			}
			if templateErr != nil {
				return created, capacityMisses, templateErr
			}
			keyID, keyErr := service.newID()
			if keyErr != nil {
				return created, capacityMisses, keyErr
			}
			requestID, requestErr := service.newID()
			if requestErr != nil {
				return created, capacityMisses, requestErr
			}
			_, createErr := service.create(ctx, audit.System(), "ios-pool-scale-"+requestID,
				"ios-pool-scale-"+keyID, input, false)
			switch {
			case createErr == nil:
				created++
			case errors.Is(createErr, ErrTargetSatisfied):
				break
			case errors.Is(createErr, ErrCapacity):
				capacityMisses++
				break
			case errors.Is(createErr, ErrConflict), errors.Is(createErr, ErrInvalidArgument), errors.Is(createErr, ErrNotFound):
				break
			default:
				return created, capacityMisses, createErr
			}
			if createErr != nil {
				break
			}
		}
	}
	return created, capacityMisses, nil
}

func (service *Service) scaleTemplate(ctx context.Context, poolID string) (CreateInput, error) {
	var hostID string
	var capabilities []byte
	var lifecycle, health, platform, kind, provider string
	err := service.db.Pool().QueryRow(ctx, `SELECT d.host_id,d.capabilities,d.lifecycle_status,d.health_status,
		d.platform,d.device_kind,d.provider_type
		FROM device_pools p
		JOIN device_pool_devices pd ON pd.pool_id=p.id AND pd.device_id=p.base_device_id AND pd.enabled
		JOIN devices d ON d.id=pd.device_id
		WHERE p.id=$1 AND p.platform='ios' AND p.status='active'`, poolID).
		Scan(&hostID, &capabilities, &lifecycle, &health, &platform, &kind, &provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return CreateInput{}, ErrNotFound
	}
	if err != nil {
		return CreateInput{}, err
	}
	if platform != "ios" || kind != "simulator" || provider != "appium_device_farm_ios" ||
		health != "healthy" || lifecycle == "deleted" || lifecycle == "quarantined" {
		return CreateInput{}, ErrConflict
	}
	var values map[string]any
	if json.Unmarshal(capabilities, &values) != nil {
		return CreateInput{}, ErrConflict
	}
	runtimeID, _ := values["runtimeId"].(string)
	deviceTypeID, _ := values["deviceTypeId"].(string)
	displayName, _ := values["model"].(string)
	if strings.TrimSpace(runtimeID) == "" || strings.TrimSpace(deviceTypeID) == "" {
		return CreateInput{}, ErrConflict
	}
	return CreateInput{HostID: hostID, PoolID: poolID, RuntimeID: runtimeID,
		DeviceTypeID: deviceTypeID, DisplayName: displayName, Reason: "按设备池目标自动扩容 iOS 模拟器"}, nil
}

func catalogFromCapabilities(raw []byte) (Catalog, error) {
	var envelope struct {
		Catalog appiumdevicefarm.SimulatorCatalog `json:"ios_simulator_catalog"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return Catalog{}, ErrConflict
	}
	return Catalog{Runtimes: envelope.Catalog.Runtimes, DeviceTypes: envelope.Catalog.DeviceTypes}, nil
}

func catalogHasRuntime(catalog Catalog, id string) bool {
	for _, item := range catalog.Runtimes {
		if item.ID == id {
			return true
		}
	}
	return false
}

func catalogHasDeviceType(catalog Catalog, id string) bool {
	for _, item := range catalog.DeviceTypes {
		if item.ID == id {
			return true
		}
	}
	return false
}

func catalogSupportsDeviceType(catalog Catalog, runtimeID, deviceTypeID string) bool {
	for _, runtime := range catalog.Runtimes {
		if runtime.ID != runtimeID {
			continue
		}
		for _, supportedID := range runtime.DeviceTypeIDs {
			if supportedID == deviceTypeID {
				return true
			}
		}
		return false
	}
	return false
}

func catalogDeviceTypeName(catalog Catalog, id string) string {
	for _, item := range catalog.DeviceTypes {
		if item.ID == id {
			return item.Name
		}
	}
	return "iPhone"
}

func evaluateHostCapacity(capacityRaw, usedRaw []byte, registeredSlots, pendingSlots int64) capacity.Result {
	var capacity, used map[string]any
	if json.Unmarshal(capacityRaw, &capacity) != nil || json.Unmarshal(usedRaw, &used) != nil {
		return capacityResult(0, 0, 0, 0, registeredSlots, pendingSlots)
	}
	slots := int64Number(capacity["device_slots"])
	usedSlots := int64Number(used["device_slots"])
	if registeredSlots > usedSlots {
		usedSlots = registeredSlots
	}
	memory := int64Number(capacity["memory_available_mb"])
	disk := int64Number(capacity["disk_available_mb"])
	return capacityResult(int64Number(capacity["cpu_cores"]), memory, disk, slots, usedSlots, pendingSlots)
}

func capacityResult(cpuCores, memoryAvailableMB, diskAvailableMB, slots, usedSlots, pendingSlots int64) capacity.Result {
	requiredMemoryMB := minimumAvailableMemoryMB * (pendingSlots + 1)
	requiredDiskMB := minimumAvailableDiskMB * (pendingSlots + 1)
	result := capacity.Result{
		Fits:              true,
		Additional:        1,
		AvailableCPU:      float64(cpuCores),
		AvailableMemoryMB: memoryAvailableMB,
		AvailableDiskMB:   diskAvailableMB,
		Shortfall:         map[string]int64{},
	}
	if memoryAvailableMB < requiredMemoryMB {
		result.Fits = false
		result.Limiting = "memory"
		result.Shortfall["memory_mb"] = requiredMemoryMB - memoryAvailableMB
	}
	if diskAvailableMB < requiredDiskMB {
		result.Fits = false
		if result.Limiting == "" {
			result.Limiting = "disk"
		}
		result.Shortfall["disk_mb"] = requiredDiskMB - diskAvailableMB
	}
	if slots > 0 && slots <= usedSlots {
		result.Fits = false
		if result.Limiting == "" {
			result.Limiting = "device_slots"
		}
		result.Shortfall["device_slots"] = 1
	}
	if result.Fits {
		result.Shortfall = nil
		return result
	}
	result.Additional = 0
	return result
}

func int64Number(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		result, _ := typed.Int64()
		return result
	default:
		return 0
	}
}

package hostcommand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/imagecatalog"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/sensitive"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var errorCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)
var hostArchPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)

const iosInventoryMissingGrace = 30 * time.Second

var (
	ErrInvalidArgument        = errors.New("宿主机命令参数无效")
	ErrNotFound               = errors.New("未找到宿主机命令资源")
	ErrConflict               = errors.New("宿主机命令发生冲突")
	ErrDeviceIdentityConflict = errors.New("发现的设备身份发生冲突")
	ErrNoCommand              = errors.New("当前没有可领取的宿主机命令")
)

type DiscoveredDevice struct {
	ProviderRef     string            `json:"provider_ref"`
	Serial          string            `json:"serial"`
	Platform        string            `json:"platform,omitempty"`
	DeviceKind      string            `json:"device_kind,omitempty"`
	ProviderType    string            `json:"provider_type,omitempty"`
	LifecycleStatus string            `json:"lifecycle_status"`
	HealthStatus    string            `json:"health_status"`
	Connection      map[string]any    `json:"connection,omitempty"`
	Capabilities    map[string]any    `json:"capabilities,omitempty"`
	Components      map[string]string `json:"components,omitempty"`
	RuntimeProfile  map[string]any    `json:"runtime_profile,omitempty"`
}

type HeartbeatInput struct {
	AgentTime   time.Time          `json:"agent_time"`
	Capacity    map[string]any     `json:"capacity"`
	Environment map[string]any     `json:"environment,omitempty"`
	Devices     []DiscoveredDevice `json:"devices"`
}

type HeartbeatResult struct {
	HostID     string    `json:"host_id"`
	Status     string    `json:"status"`
	ReceivedAt time.Time `json:"received_at"`
	Devices    int       `json:"devices"`
}

type ClaimInput struct {
	LeaseSeconds int `json:"lease_seconds"`
	MaxCommands  int `json:"max_commands"`
	WaitSeconds  int `json:"wait_seconds,omitempty"`
}

type CompletionError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	Retryable bool           `json:"retryable"`
}

type CompletionInput struct {
	LeaseToken string           `json:"lease_token"`
	Attempt    int              `json:"attempt"`
	Status     string           `json:"status"`
	Result     map[string]any   `json:"result,omitempty"`
	Error      *CompletionError `json:"error,omitempty"`
}

type LeaseExtensionInput struct {
	LeaseToken   string `json:"lease_token"`
	Attempt      int    `json:"attempt"`
	LeaseSeconds int    `json:"lease_seconds"`
}

type Command struct {
	ID             string               `json:"id"`
	HostID         string               `json:"host_id"`
	CommandType    string               `json:"command_type"`
	Payload        map[string]any       `json:"payload"`
	Status         domain.CommandStatus `json:"status"`
	LeaseToken     *string              `json:"lease_token,omitempty"`
	LeaseExpiresAt *time.Time           `json:"lease_expires_at,omitempty"`
	Attempt        int                  `json:"attempt"`
	MaxAttempts    int                  `json:"max_attempts"`
	Result         map[string]any       `json:"result,omitempty"`
	ErrorCode      *string              `json:"error_code,omitempty"`
	CompletedAt    *time.Time           `json:"completed_at,omitempty"`
}

type Service struct {
	db    *database.DB
	repo  repository.CommandRepository
	newID func() (string, error)
}

func New(db *database.DB) *Service {
	return &Service{db: db, repo: repository.CommandRepository{}, newID: identifier.New}
}

func (service *Service) Create(ctx context.Context, hostID, commandType string, payload map[string]any, maxAttempts int, key string) (Command, error) {
	if service == nil || service.db == nil || len(hostID) < 16 || !validCommandType(commandType) || maxAttempts < 1 || len(key) < 8 {
		return Command{}, ErrInvalidArgument
	}
	id, err := service.newID()
	if err != nil {
		return Command{}, err
	}
	record, err := service.repo.Create(ctx, service.db.Pool(), repository.CreateCommandParams{
		ID: id, HostID: hostID, CommandType: commandType, Payload: sensitive.RedactMap(payload),
		MaxAttempts: maxAttempts, IdempotencyKey: key,
	})
	if err != nil {
		return Command{}, translate(err)
	}
	return toCommand(record)
}

func (service *Service) Heartbeat(ctx context.Context, hostID string, input HeartbeatInput) (HeartbeatResult, error) {
	if service == nil || service.db == nil || len(hostID) < 16 || input.AgentTime.IsZero() || input.Capacity == nil {
		return HeartbeatResult{}, ErrInvalidArgument
	}
	seenRefs, seenSerials := map[string]bool{}, map[string]bool{}
	for index := range input.Devices {
		device := &input.Devices[index]
		device.ProviderRef = strings.TrimSpace(device.ProviderRef)
		device.Serial = strings.TrimSpace(device.Serial)
		device.Platform = strings.ToLower(strings.TrimSpace(device.Platform))
		device.DeviceKind = strings.ToLower(strings.TrimSpace(device.DeviceKind))
		device.ProviderType = strings.ToLower(strings.TrimSpace(device.ProviderType))
		if device.ProviderRef == "" || seenRefs[device.ProviderRef] ||
			!validDiscoveredLifecycle(device.LifecycleStatus) || !validDiscoveredHealth(device.HealthStatus) ||
			(device.LifecycleStatus == string(domain.DeviceReady) && device.HealthStatus != string(domain.HealthHealthy)) {
			return HeartbeatResult{}, ErrInvalidArgument
		}
		if (device.Platform != "" && device.Platform != "android" && device.Platform != "ios") ||
			(device.DeviceKind != "" && device.DeviceKind != "emulator" && device.DeviceKind != "simulator" && device.DeviceKind != "physical") ||
			(device.ProviderType != "" && device.ProviderType != "mock" && device.ProviderType != "docker_emulator" && device.ProviderType != "usb_android" && device.ProviderType != "appium_device_farm_ios") ||
			!validComponents(device.Components) {
			return HeartbeatResult{}, ErrInvalidArgument
		}
		if device.Serial == "" && device.LifecycleStatus != string(domain.DeviceStopped) {
			return HeartbeatResult{}, ErrInvalidArgument
		}
		if device.Serial != "" && seenSerials[device.Serial] {
			return HeartbeatResult{}, ErrInvalidArgument
		}
		if _, _, _, err := discoveredConnection(device.Connection); err != nil {
			return HeartbeatResult{}, ErrInvalidArgument
		}
		if len(device.RuntimeProfile) > 0 {
			if _, err := runtimeprofile.Parse(device.RuntimeProfile); err != nil {
				return HeartbeatResult{}, ErrInvalidArgument
			}
		}
		seenRefs[device.ProviderRef] = true
		if device.Serial != "" {
			seenSerials[device.Serial] = true
		}
	}
	readiness, err := reportedHostReadiness(input.Environment)
	if err != nil {
		return HeartbeatResult{}, ErrInvalidArgument
	}
	inventoryComplete, err := reportedInventoryComplete(input.Environment)
	if err != nil {
		return HeartbeatResult{}, ErrInvalidArgument
	}
	hostOS, hostArch, err := reportedHostIdentity(input.Environment)
	if err != nil {
		return HeartbeatResult{}, ErrInvalidArgument
	}
	sessionFenceEndpoint, err := reportedSessionFenceEndpoint(input.Environment, hostOS)
	if err != nil {
		return HeartbeatResult{}, ErrInvalidArgument
	}
	if sessionFenceEndpoint != nil {
		input.Environment["session_fence_endpoint"] = *sessionFenceEndpoint
	}
	capacity, err := json.Marshal(sensitive.RedactMap(input.Capacity))
	if err != nil {
		return HeartbeatResult{}, ErrInvalidArgument
	}
	environment, err := json.Marshal(sensitive.RedactMap(input.Environment))
	if err != nil {
		return HeartbeatResult{}, ErrInvalidArgument
	}
	used := map[string]any{"device_slots": 0, "cpu_cores": float64(0), "memory_mb": int64(0), "data_disk_mb": int64(0)}
	for _, device := range input.Devices {
		if discoveredCountsAsUsed(device) {
			used["device_slots"] = used["device_slots"].(int) + 1
		}
		if len(device.RuntimeProfile) == 0 {
			continue
		}
		profile, _ := runtimeprofile.Parse(device.RuntimeProfile)
		used["cpu_cores"] = used["cpu_cores"].(float64) + profile.ContainerCPUCores
		used["memory_mb"] = used["memory_mb"].(int64) + profile.ContainerMemoryMB
		used["data_disk_mb"] = used["data_disk_mb"].(int64) + profile.DataDiskMB
	}
	usedCapacity, _ := json.Marshal(used)
	var result HeartbeatResult
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var status domain.HostStatus
		var autoMaintenance bool
		var readinessFailureStartedAt *time.Time
		if err := tx.QueryRow(ctx, `SELECT status,COALESCE((capabilities->>'host_readiness_auto_maintenance')::boolean,false),
			NULLIF(capabilities->>'host_readiness_failure_started_at','')::timestamptz
			FROM device_hosts WHERE id=$1 FOR UPDATE`, hostID).Scan(&status, &autoMaintenance, &readinessFailureStartedAt); err != nil {
			return err
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		target := status
		if status != domain.HostDraining && readiness != nil {
			if !*readiness {
				if status != domain.HostMaintenance {
					target = domain.HostMaintenance
					autoMaintenance = true
				}
				if autoMaintenance && readinessFailureStartedAt == nil {
					startedAt := now
					readinessFailureStartedAt = &startedAt
				}
			} else if status == domain.HostOffline || status == domain.HostOnline || (status == domain.HostMaintenance && autoMaintenance) {
				target = domain.HostOnline
				autoMaintenance = false
				readinessFailureStartedAt = nil
			}
		} else if status == domain.HostOffline {
			target = domain.HostOnline
		}
		if target != status {
			host, err := domain.RestoreHost(hostID, status)
			if err != nil {
				return err
			}
			if err := host.Transition(target, "agent heartbeat readiness observation", now); err != nil {
				return err
			}
			target = host.Status()
		}
		if _, err := tx.Exec(ctx, `UPDATE device_hosts SET status=$2::varchar,draining=($2::varchar='draining'),
			capacity=CASE WHEN $3::jsonb->>'resource_model'='dynamic_v1' THEN $3::jsonb ELSE capacity || ($3::jsonb-'device_slots') END,
			capabilities=((capabilities || ($4::jsonb-'provider'-'host_os'-'host_arch'))-'host_readiness_failure_started_at') ||
				jsonb_build_object('host_readiness_auto_maintenance',$9::boolean) ||
				CASE WHEN $10::timestamptz IS NULL THEN '{}'::jsonb ELSE jsonb_build_object('host_readiness_failure_started_at',$10::timestamptz) END,
			used_capacity=$5,
			host_os=COALESCE($6,host_os),host_arch=COALESCE($7,host_arch),last_heartbeat_at=$8,updated_at=$8 WHERE id=$1`,
			hostID, target, capacity, environment, usedCapacity, hostOS, hostArch, now, autoMaintenance, readinessFailureStartedAt); err != nil {
			return err
		}
		for _, discovered := range input.Devices {
			if err := updateDiscoveredDevice(ctx, tx, hostID, discovered, now); err != nil {
				return err
			}
		}
		if inventoryComplete {
			if err := service.quarantineMissingIOSDevices(ctx, tx, hostID, seenRefs, now); err != nil {
				return err
			}
		}
		result = HeartbeatResult{HostID: hostID, Status: string(target), ReceivedAt: now, Devices: len(input.Devices)}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return HeartbeatResult{}, ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return HeartbeatResult{}, fmt.Errorf("%w: %s", ErrDeviceIdentityConflict, pgErr.ConstraintName)
	}
	return result, err
}

func reportedInventoryComplete(environment map[string]any) (bool, error) {
	value, exists := environment["provider_inventory_complete"]
	if !exists {
		// Older Agents did not declare whether an empty list was authoritative.
		// Failing open here prevents an upgrade from deleting every Simulator.
		return false, nil
	}
	complete, ok := value.(bool)
	if !ok {
		return false, ErrInvalidArgument
	}
	return complete, nil
}

func (service *Service) quarantineMissingIOSDevices(
	ctx context.Context,
	tx pgx.Tx,
	hostID string,
	seenRefs map[string]bool,
	now time.Time,
) error {
	rows, err := tx.Query(ctx, `SELECT d.id,d.provider_ref,d.lifecycle_status,d.health_status,
		COALESCE(d.last_seen_at,d.created_at)
		FROM devices d
		WHERE d.host_id=$1 AND d.platform='ios' AND d.device_kind='simulator'
		AND d.provider_type='appium_device_farm_ios'
		AND d.lifecycle_status NOT IN ('quarantined','deleted')
		AND NOT EXISTS (SELECT 1 FROM device_host_commands c WHERE c.payload->>'device_id'=d.id
			AND c.command_type IN ('create','rebuild','delete') AND c.status IN ('pending','leased'))
		ORDER BY d.id FOR UPDATE OF d`, hostID)
	if err != nil {
		return err
	}
	type missingCandidate struct {
		id, providerRef string
		lifecycle       domain.DeviceLifecycleStatus
		health          domain.HealthStatus
		lastSeen        time.Time
	}
	candidates := []missingCandidate{}
	for rows.Next() {
		var candidate missingCandidate
		if err := rows.Scan(&candidate.id, &candidate.providerRef, &candidate.lifecycle, &candidate.health, &candidate.lastSeen); err != nil {
			rows.Close()
			return err
		}
		if !seenRefs[candidate.providerRef] && now.Sub(candidate.lastSeen) >= iosInventoryMissingGrace {
			candidates = append(candidates, candidate)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, candidate := range candidates {
		reason := "IOS_PROVIDER_DEVICE_MISSING: complete inventory no longer contains the registered Simulator"
		aggregate, err := domain.RestoreDevice(candidate.id, candidate.lifecycle, candidate.health)
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
		if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_status=$3,health_reason=$4,
			consecutive_failures=consecutive_failures+1,updated_at=$5 WHERE id=$1`, candidate.id,
			aggregate.Lifecycle(), aggregate.Health(), reason, now); err != nil {
			return err
		}
		eventID, err := service.newID()
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"host_id": hostID, "inventory_complete": true})
		if _, err := tx.Exec(ctx, `INSERT INTO device_health_events
			(id,device_id,source,event_type,severity,reason,payload,observed_at)
			VALUES($1,$2,'agent','ios_provider_device_missing','critical',$3,$4::jsonb,$5)`,
			eventID, candidate.id, reason, payload, now); err != nil {
			return err
		}
	}
	return nil
}

func discoveredCountsAsUsed(device DiscoveredDevice) bool {
	if device.Platform != "ios" {
		return true
	}
	allowlisted, _ := device.Capabilities["allowlisted"].(bool)
	return allowlisted
}

type discoveredDeviceState struct {
	id                string
	lifecycle         domain.DeviceLifecycleStatus
	health            domain.HealthStatus
	healthReason      *string
	operationInFlight bool
	assignmentTarget  string
	platform          string
	deviceKind        string
	providerType      string
}

func updateDiscoveredDevice(ctx context.Context, tx pgx.Tx, hostID string, discovered DiscoveredDevice, now time.Time) error {
	var current discoveredDeviceState
	err := tx.QueryRow(ctx, `SELECT d.id,d.lifecycle_status,d.health_status,d.health_reason,
		EXISTS (SELECT 1 FROM device_host_commands c WHERE c.payload->>'device_id'=d.id
			AND c.command_type IN ('create','rebuild','delete') AND c.status IN ('pending','leased')),
		COALESCE((SELECT CASE WHEN r.status='active' THEN 'busy' ELSE 'reserved' END
			FROM device_reservations r WHERE r.device_id=d.id AND r.status IN ('pending','active')
			ORDER BY CASE WHEN r.status='active' THEN 0 ELSE 1 END,r.updated_at DESC,r.id LIMIT 1),''),
		d.platform,d.device_kind,d.provider_type
		FROM devices d WHERE d.host_id=$1 AND d.provider_ref=$2 FOR UPDATE OF d`, hostID, discovered.ProviderRef).
		Scan(&current.id, &current.lifecycle, &current.health, &current.healthReason, &current.operationInFlight, &current.assignmentTarget,
			&current.platform, &current.deviceKind, &current.providerType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if (discovered.Platform != "" && discovered.Platform != current.platform) ||
		(discovered.DeviceKind != "" && discovered.DeviceKind != current.deviceKind) ||
		(discovered.ProviderType != "" && discovered.ProviderType != current.providerType) {
		return ErrDeviceIdentityConflict
	}
	adbEndpoint, appiumEndpoint, appiumUDID, err := discoveredConnection(discovered.Connection)
	if err != nil {
		return err
	}
	aggregate, err := domain.RestoreDevice(current.id, current.lifecycle, current.health)
	if err != nil {
		return err
	}
	preserveSTFHealth := false
	recoveredHostOutage := current.lifecycle == domain.DeviceQuarantined && current.healthReason != nil &&
		*current.healthReason == domain.HostUnavailableReason && discovered.HealthStatus == string(domain.HealthHealthy) && !current.operationInFlight
	recoveredIOSAutomation := current.lifecycle == domain.DeviceQuarantined && current.platform == "ios" && current.healthReason != nil &&
		*current.healthReason == domain.AgentReportedUnhealthyReason && discovered.HealthStatus == string(domain.HealthHealthy) && !current.operationInFlight
	recoveredAutomatically := recoveredHostOutage || recoveredIOSAutomation
	if recoveredAutomatically {
		if err := recoverFromHostOutage(aggregate, current.assignmentTarget, now); err != nil {
			return err
		}
	}
	if (current.lifecycle != domain.DeviceQuarantined || recoveredAutomatically) && current.lifecycle != domain.DeviceDeleted && !current.operationInFlight {
		incomingHealth := domain.HealthStatus(discovered.HealthStatus)
		preserveSTFHealth = incomingHealth == domain.HealthHealthy && current.healthReason != nil &&
			domain.IsSTFFailureReason(*current.healthReason)
		if !preserveSTFHealth && aggregate.Health() != incomingHealth {
			if err := aggregate.UpdateHealth(incomingHealth, "agent heartbeat health observation", now); err != nil {
				return err
			}
		}
		if current.lifecycle != domain.DeviceReserved && current.lifecycle != domain.DeviceBusy && current.lifecycle != domain.DeviceRecycling {
			if err := applyDiscoveredLifecycle(aggregate, domain.DeviceLifecycleStatus(discovered.LifecycleStatus), now); err != nil {
				return err
			}
		}
	}
	healthReason := current.healthReason
	if (current.lifecycle != domain.DeviceQuarantined || recoveredAutomatically) && current.lifecycle != domain.DeviceDeleted &&
		!current.operationInFlight && !preserveSTFHealth {
		healthReason = nil
		if aggregate.Health() != domain.HealthHealthy {
			value := "agent heartbeat reported " + string(aggregate.Health())
			healthReason = &value
		}
	}
	discoveryCapabilities := sensitive.RedactMap(discovered.Capabilities)
	if len(discovered.Components) > 0 {
		componentValues := make(map[string]any, len(discovered.Components))
		for name, status := range discovered.Components {
			componentValues[name] = status
		}
		discoveryCapabilities["componentHealth"] = componentValues
	}
	capabilitiesJSON, err := json.Marshal(discoveryCapabilities)
	if err != nil {
		return ErrInvalidArgument
	}
	_, err = tx.Exec(ctx, `UPDATE devices SET serial=CASE WHEN $2='' THEN serial ELSE $2 END,
		adb_endpoint=COALESCE($3,adb_endpoint),appium_endpoint=COALESCE($4,appium_endpoint),
		capabilities=CASE WHEN $5::text IS NULL THEN (capabilities || $6::jsonb) ELSE jsonb_set((capabilities || $6::jsonb),'{appiumUdid}',to_jsonb($5::text),true) END,
		lifecycle_status=$7::varchar,health_status=$8::varchar,health_reason=$9,
		consecutive_failures=CASE WHEN $11::boolean THEN 0 ELSE consecutive_failures END,last_seen_at=$10,updated_at=$10
		WHERE id=$1`, current.id, discovered.Serial, adbEndpoint, appiumEndpoint, appiumUDID, capabilitiesJSON,
		aggregate.Lifecycle(), aggregate.Health(), healthReason, now, recoveredAutomatically)
	if err != nil {
		return err
	}
	// The first healthy managed Simulator is the pool's safe expansion
	// template. Existing pools are repaired by the next regular heartbeat;
	// an explicit administrator selection is never overwritten.
	if current.platform == "ios" && current.deviceKind == "simulator" && current.providerType == "appium_device_farm_ios" &&
		aggregate.Lifecycle() == domain.DeviceReady && aggregate.Health() == domain.HealthHealthy {
		_, err = tx.Exec(ctx, `UPDATE device_pools p SET base_device_id=$1,updated_at=$2
			WHERE p.platform='ios' AND p.status='active' AND p.base_device_id IS NULL
			AND EXISTS (SELECT 1 FROM device_pool_devices pd
				WHERE pd.pool_id=p.id AND pd.device_id=$1 AND pd.enabled)`, current.id, now)
	}
	return err
}

func reportedHostReadiness(environment map[string]any) (*bool, error) {
	value, exists := environment["host_readiness"]
	if !exists {
		return nil, nil
	}
	readiness, ok := value.(map[string]any)
	if !ok {
		return nil, ErrInvalidArgument
	}
	ready, ok := readiness["ready"].(bool)
	if !ok {
		return nil, ErrInvalidArgument
	}
	return &ready, nil
}

func reportedHostIdentity(environment map[string]any) (*string, *string, error) {
	var hostOS, hostArch *string
	if raw, exists := environment["host_os"]; exists {
		value, ok := raw.(string)
		value = strings.ToLower(strings.TrimSpace(value))
		if !ok || (value != "linux" && value != "macos" && value != "windows") {
			return nil, nil, ErrInvalidArgument
		}
		hostOS = &value
	}
	if raw, exists := environment["host_arch"]; exists {
		value, ok := raw.(string)
		value = strings.TrimSpace(value)
		if !ok || !hostArchPattern.MatchString(value) {
			return nil, nil, ErrInvalidArgument
		}
		hostArch = &value
	}
	return hostOS, hostArch, nil
}

func reportedSessionFenceEndpoint(environment map[string]any, hostOS *string) (*string, error) {
	raw, exists := environment["session_fence_endpoint"]
	if !exists {
		return nil, nil
	}
	value, ok := raw.(string)
	if !ok || strings.TrimSpace(value) == "" || hostOS == nil || *hostOS != "macos" {
		return nil, ErrInvalidArgument
	}
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, ErrInvalidArgument
	}
	hostname := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(hostname)
	loopback := hostname == "localhost" || (ip != nil && ip.IsLoopback())
	if parsed.Scheme != "https" && !loopback {
		return nil, ErrInvalidArgument
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	normalized := strings.TrimRight(parsed.String(), "/")
	return &normalized, nil
}

func validComponents(components map[string]string) bool {
	for name, status := range components {
		switch name {
		case providers.ProbeTransport, providers.ProbeOSReady, providers.ProbeAutomation, providers.ProbeRouter, providers.ProbeRemoteControl:
		default:
			return false
		}
		switch providers.ProbeStatus(status) {
		case providers.ProbePassed, providers.ProbeFailed, providers.ProbeUnknown, providers.ProbeUnsupported:
		default:
			return false
		}
	}
	return true
}

func recoverFromHostOutage(device *domain.Device, assignmentTarget string, now time.Time) error {
	if device.Health() != domain.HealthHealthy {
		if err := device.UpdateHealth(domain.HealthHealthy, "host and provider heartbeat recovered", now); err != nil {
			return err
		}
	}
	for _, target := range []domain.DeviceLifecycleStatus{domain.DeviceProvisioning, domain.DeviceBooting, domain.DeviceReady} {
		if err := device.Transition(target, "host and provider heartbeat recovered", now); err != nil {
			return err
		}
	}
	if assignmentTarget == string(domain.DeviceReserved) || assignmentTarget == string(domain.DeviceBusy) {
		if err := device.Transition(domain.DeviceReserved, "existing reservation restored after host recovery", now); err != nil {
			return err
		}
	}
	if assignmentTarget == string(domain.DeviceBusy) {
		if err := device.Transition(domain.DeviceBusy, "active session restored after host recovery", now); err != nil {
			return err
		}
	}
	return nil
}

func applyDiscoveredLifecycle(device *domain.Device, incoming domain.DeviceLifecycleStatus, now time.Time) error {
	if device.Lifecycle() == incoming {
		return nil
	}
	switch incoming {
	case domain.DeviceBooting:
		if device.Lifecycle() == domain.DeviceProvisioning || device.Lifecycle() == domain.DeviceStopped {
			return device.Transition(domain.DeviceBooting, "agent discovered booting device", now)
		}
	case domain.DeviceReady:
		if device.Lifecycle() == domain.DeviceProvisioning || device.Lifecycle() == domain.DeviceStopped {
			if err := device.Transition(domain.DeviceBooting, "agent discovered running device", now); err != nil {
				return err
			}
		}
		if device.Lifecycle() == domain.DeviceBooting {
			return device.Transition(domain.DeviceReady, "agent health checks passed", now)
		}
	case domain.DeviceStopped:
		if device.Lifecycle() == domain.DeviceBooting || device.Lifecycle() == domain.DeviceReady {
			return device.Transition(domain.DeviceStopped, "agent discovered stopped device", now)
		}
	}
	return nil
}

func discoveredConnection(connection map[string]any) (*string, *string, *string, error) {
	adb, err := optionalConnectionValue(connection, "adb_endpoint")
	if err != nil {
		return nil, nil, nil, err
	}
	appium, err := optionalConnectionValue(connection, "appium_endpoint")
	if err != nil {
		return nil, nil, nil, err
	}
	appiumUDID, err := optionalConnectionValue(connection, "appium_udid")
	return adb, appium, appiumUDID, err
}

func optionalConnectionValue(connection map[string]any, key string) (*string, error) {
	value, exists := connection[key]
	if !exists || value == nil {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, ErrInvalidArgument
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	return &text, nil
}

func validDiscoveredLifecycle(value string) bool {
	switch domain.DeviceLifecycleStatus(value) {
	case domain.DeviceBooting, domain.DeviceReady, domain.DeviceStopped:
		return true
	default:
		return false
	}
}

func validDiscoveredHealth(value string) bool {
	switch domain.HealthStatus(value) {
	case domain.HealthUnknown, domain.HealthHealthy, domain.HealthDegraded, domain.HealthUnhealthy:
		return true
	default:
		return false
	}
}

func (service *Service) Claim(ctx context.Context, hostID string, input ClaimInput) ([]Command, error) {
	if service == nil || service.db == nil || len(hostID) < 16 || input.LeaseSeconds < 5 || input.LeaseSeconds > 300 ||
		input.MaxCommands < 1 || input.MaxCommands > 20 || input.WaitSeconds < 0 || input.WaitSeconds > 30 {
		return nil, ErrInvalidArgument
	}
	deadline := time.Now().Add(time.Duration(input.WaitSeconds) * time.Second)
	for {
		commands := make([]Command, 0, input.MaxCommands)
		for len(commands) < input.MaxCommands {
			token, err := service.newID()
			if err != nil {
				return nil, err
			}
			record, err := service.repo.ClaimNext(ctx, service.db.Pool(), hostID, token, time.Duration(input.LeaseSeconds)*time.Second)
			if errors.Is(err, repository.ErrNotFound) {
				break
			}
			if err != nil {
				return nil, err
			}
			command, err := toCommand(record)
			if err != nil {
				return nil, err
			}
			commands = append(commands, command)
		}
		if len(commands) > 0 || input.WaitSeconds == 0 || time.Now().After(deadline) {
			return commands, nil
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (service *Service) Complete(ctx context.Context, id string, input CompletionInput) (Command, error) {
	if service == nil || service.db == nil || len(id) < 16 || len(input.LeaseToken) < 16 || input.Attempt < 1 ||
		(input.Status != "succeeded" && input.Status != "failed" && input.Status != "timed_out") {
		return Command{}, ErrInvalidArgument
	}
	target := domain.CommandStatus(input.Status)
	if target == domain.CommandFailed && input.Error != nil && input.Error.Retryable {
		target = domain.CommandPending
	}
	state, err := domain.RestoreCommand(id, domain.CommandLeased)
	if err != nil {
		return Command{}, err
	}
	if err := state.Transition(target, "agent command completion", time.Now().UTC()); err != nil {
		return Command{}, err
	}
	var errorCode *string
	if input.Error != nil && strings.TrimSpace(input.Error.Code) != "" {
		value := strings.TrimSpace(input.Error.Code)
		if !errorCodePattern.MatchString(value) {
			return Command{}, ErrInvalidArgument
		}
		errorCode = &value
	}
	retryable := input.Error != nil && input.Error.Retryable
	var record repository.CommandRecord
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var completionErr error
		record, completionErr = service.repo.Complete(ctx, tx, id, input.LeaseToken, input.Attempt,
			domain.CommandStatus(input.Status), sensitive.RedactMap(input.Result), errorCode, retryable)
		if completionErr != nil {
			return completionErr
		}
		return service.reconcileManagementOperation(ctx, tx, record)
	})
	if err != nil {
		return Command{}, translate(err)
	}
	return toCommand(record)
}

func (service *Service) Extend(ctx context.Context, id string, input LeaseExtensionInput) (Command, error) {
	if service == nil || service.db == nil || len(id) < 16 || len(input.LeaseToken) < 16 || input.Attempt < 1 || input.LeaseSeconds < 5 || input.LeaseSeconds > 300 {
		return Command{}, ErrInvalidArgument
	}
	record, err := service.repo.ExtendLease(ctx, service.db.Pool(), id, input.LeaseToken, input.Attempt, time.Duration(input.LeaseSeconds)*time.Second)
	if err != nil {
		return Command{}, translate(err)
	}
	return toCommand(record)
}

func (service *Service) RecoverExpiredOnce(ctx context.Context) (Command, error) {
	var record repository.CommandRecord
	err := service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var recoveryErr error
		record, recoveryErr = service.repo.RecoverExpiredLease(ctx, tx)
		if recoveryErr != nil {
			return recoveryErr
		}
		return service.reconcileManagementOperation(ctx, tx, record)
	})
	if errors.Is(err, repository.ErrNotFound) {
		return Command{}, ErrNoCommand
	}
	if err != nil {
		return Command{}, err
	}
	return toCommand(record)
}

type managementOperationResult struct {
	Platform         string `json:"platform"`
	State            string `json:"state"`
	Generation       int    `json:"generation"`
	ReimageApplied   bool   `json:"reimage_applied"`
	RollbackRestored bool   `json:"rollback_restored"`
	Connection       struct {
		Serial         string `json:"serial"`
		ProviderID     string `json:"provider_id"`
		ADBEndpoint    string `json:"adb_endpoint"`
		AppiumEndpoint string `json:"appium_endpoint"`
		AppiumUDID     string `json:"appium_udid"`
	} `json:"connection"`
	Health struct {
		Online        bool              `json:"online"`
		ADBOnline     bool              `json:"adb_online"`
		BootCompleted bool              `json:"boot_completed"`
		AppiumHealthy bool              `json:"appium_healthy"`
		Components    map[string]string `json:"components"`
	} `json:"health"`
}

func (service *Service) reconcileManagementOperation(ctx context.Context, tx pgx.Tx, record repository.CommandRecord) error {
	if err := imagecatalog.ReconcileCommand(ctx, tx, record, service.newID); err != nil {
		return err
	}
	if record.Status == domain.CommandPending || record.Status == domain.CommandLeased {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return err
	}
	if commandPayloadString(payload, "operation_source") != "management" {
		return nil
	}
	deviceID := commandPayloadString(payload, "device_id")
	providerRef := commandPayloadString(payload, "provider_ref")
	expectedState := domain.DeviceLifecycleStatus(commandPayloadString(payload, "operation_state"))
	if len(deviceID) < 16 || providerRef == "" || expectedState == "" {
		return ErrInvalidArgument
	}
	var lifecycle domain.DeviceLifecycleStatus
	var health domain.HealthStatus
	if err := tx.QueryRow(ctx, `SELECT lifecycle_status,health_status FROM devices
		WHERE id=$1 AND host_id=$2 AND provider_ref=$3 FOR UPDATE`, deviceID, record.HostID, providerRef).
		Scan(&lifecycle, &health); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if lifecycle != expectedState && !(record.CommandType == "rebuild" &&
		expectedState == domain.DeviceProvisioning && lifecycle == domain.DeviceBooting) {
		return nil
	}
	now, err := database.ClockNow(ctx, tx)
	if err != nil {
		return err
	}
	if record.CommandType == "delete" {
		return service.reconcileManagementDelete(ctx, tx, record, deviceID, lifecycle, health, now)
	}
	if commandPayloadString(payload, "operation_kind") == "reimage" {
		return service.reconcileManagementReimage(ctx, tx, record, payload, deviceID, lifecycle, health, now)
	}
	if record.CommandType == "stop" {
		return service.reconcileManagementStop(ctx, tx, record, deviceID, lifecycle, health, now)
	}
	code, reason := "", "设备管理命令执行完成："+record.CommandType
	var result managementOperationResult
	succeeded := record.Status == domain.CommandSucceeded && json.Unmarshal(record.Result, &result) == nil &&
		validManagementOperationResult(result)
	if !succeeded {
		code = "AGENT_COMMAND_FAILED"
		if record.ErrorCode != nil && *record.ErrorCode != "" {
			code = *record.ErrorCode
		} else if record.Status == domain.CommandSucceeded {
			code = "COMMAND_RESULT_INVALID"
		}
		reason = code + "：设备管理命令未产生健康设备（" + record.CommandType + "）"
	}
	aggregate, err := domain.RestoreDevice(deviceID, lifecycle, health)
	if err != nil {
		return err
	}
	if succeeded {
		if aggregate.Health() != domain.HealthHealthy {
			if err := aggregate.UpdateHealth(domain.HealthHealthy, reason, now); err != nil {
				return err
			}
		}
		if aggregate.Lifecycle() == domain.DeviceStopped || aggregate.Lifecycle() == domain.DeviceProvisioning {
			if err := aggregate.Transition(domain.DeviceBooting, reason, now); err != nil {
				return err
			}
		}
		if aggregate.Lifecycle() != domain.DeviceBooting {
			return nil
		}
		if err := aggregate.Transition(domain.DeviceReady, reason, now); err != nil {
			return err
		}
		var healthReason *string
		if (record.CommandType == "create" || record.CommandType == "rebuild") && !strings.EqualFold(result.Platform, "ios") {
			if err := aggregate.UpdateHealth(domain.HealthUnhealthy, domain.STFReadinessStabilizationReason, now); err != nil {
				return err
			}
			value := domain.STFReadinessStabilizationReason
			healthReason = &value
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET provider_ref=CASE WHEN $2<>'' THEN $2 ELSE provider_ref END,
			serial=$3,adb_endpoint=NULLIF($4,''),appium_endpoint=$5,
			capabilities=jsonb_set(capabilities,'{appiumUdid}',to_jsonb($6::text),true),
			lifecycle_status=$7,health_status=$8,health_reason=$9,consecutive_failures=0,
			last_seen_at=$10,updated_at=$10 WHERE id=$1 AND lifecycle_status=$11`,
			deviceID, result.Connection.ProviderID, result.Connection.Serial, result.Connection.ADBEndpoint, result.Connection.AppiumEndpoint,
			result.Connection.AppiumUDID, aggregate.Lifecycle(), aggregate.Health(), healthReason, now, lifecycle); err != nil {
			return err
		}
	} else {
		if aggregate.Health() != domain.HealthUnhealthy {
			if err := aggregate.UpdateHealth(domain.HealthUnhealthy, reason, now); err != nil {
				return err
			}
		}
		if err := aggregate.Transition(domain.DeviceQuarantined, reason, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_status=$3,health_reason=$4,
			consecutive_failures=consecutive_failures+1,updated_at=$5 WHERE id=$1 AND lifecycle_status=$6`,
			deviceID, aggregate.Lifecycle(), aggregate.Health(), reason, now, lifecycle); err != nil {
			return err
		}
	}
	eventID, err := service.newID()
	if err != nil {
		return err
	}
	severity, eventType := "info", "device_management_operation_succeeded"
	if !succeeded {
		severity, eventType = "error", "device_management_operation_failed"
	}
	eventPayload, err := json.Marshal(map[string]any{"command_id": record.ID, "command_type": record.CommandType, "error_code": code})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO device_health_events
		(id,device_id,source,event_type,severity,reason,payload,observed_at)
		VALUES($1,$2,'agent',$3,$4,$5,$6::jsonb,$7)`, eventID, deviceID, eventType, severity, reason, eventPayload, now)
	return err
}

func (service *Service) reconcileManagementReimage(ctx context.Context, tx pgx.Tx, record repository.CommandRecord,
	payload map[string]any, deviceID string, lifecycle domain.DeviceLifecycleStatus, health domain.HealthStatus, now time.Time) error {
	var result managementOperationResult
	resultValid := json.Unmarshal(record.Result, &result) == nil && validManagementOperationResult(result)
	applied := record.Status == domain.CommandSucceeded && resultValid && result.ReimageApplied
	restored := record.Status == domain.CommandFailed && resultValid && result.RollbackRestored
	code := ""
	if record.ErrorCode != nil {
		code = *record.ErrorCode
	}
	if code == "" && !applied {
		code = "REIMAGE_COMMAND_FAILED"
	}
	reason := "management reimage applied target image"
	if restored {
		reason = code + ": target failed; previous image was restored"
	}
	if !applied && !restored {
		reason = code + ": target and previous image restore did not produce a healthy device"
	}

	aggregate, err := domain.RestoreDevice(deviceID, lifecycle, health)
	if err != nil {
		return err
	}
	if applied || restored {
		if aggregate.Health() != domain.HealthHealthy {
			if err := aggregate.UpdateHealth(domain.HealthHealthy, reason, now); err != nil {
				return err
			}
		}
		if aggregate.Lifecycle() == domain.DeviceProvisioning {
			if err := aggregate.Transition(domain.DeviceBooting, reason, now); err != nil {
				return err
			}
		}
		if aggregate.Lifecycle() != domain.DeviceBooting {
			return nil
		}
		if err := aggregate.Transition(domain.DeviceReady, reason, now); err != nil {
			return err
		}
		if err := aggregate.UpdateHealth(domain.HealthUnhealthy, domain.STFReadinessStabilizationReason, now); err != nil {
			return err
		}
		if applied {
			targetProfile, marshalErr := json.Marshal(mapValue(payload, "runtime_profile"))
			if marshalErr != nil {
				return marshalErr
			}
			targetCapabilities := mapValue(payload, "capabilities")
			if targetCapabilities == nil {
				targetCapabilities = map[string]any{}
			}
			targetCapabilitiesJSON, marshalErr := json.Marshal(targetCapabilities)
			if marshalErr != nil {
				return marshalErr
			}
			if _, err := tx.Exec(ctx, `UPDATE devices SET image_id=$2,runtime_profile_override=$3,
				pending_image_id=NULL,pending_runtime_profile=NULL,reimage_status='idle',reimage_error=NULL,
				serial=$4,adb_endpoint=$5,appium_endpoint=$6,
				capabilities=jsonb_set(capabilities || $13::jsonb,'{appiumUdid}',to_jsonb($7::text),true),
				lifecycle_status=$8,health_status=$9,health_reason=$10,consecutive_failures=0,last_seen_at=$11,updated_at=$11
				WHERE id=$1 AND lifecycle_status=$12`, deviceID, commandPayloadString(payload, "image_id"), targetProfile,
				result.Connection.Serial, result.Connection.ADBEndpoint, result.Connection.AppiumEndpoint, result.Connection.AppiumUDID,
				aggregate.Lifecycle(), aggregate.Health(), domain.STFReadinessStabilizationReason, now, lifecycle, targetCapabilitiesJSON); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE devices SET pending_image_id=NULL,pending_runtime_profile=NULL,
				reimage_status='failed',reimage_error=$2,serial=$3,adb_endpoint=$4,appium_endpoint=$5,
				capabilities=jsonb_set(capabilities,'{appiumUdid}',to_jsonb($6::text),true),
				lifecycle_status=$7,health_status=$8,health_reason=$9,consecutive_failures=0,last_seen_at=$10,updated_at=$10
				WHERE id=$1 AND lifecycle_status=$11`, deviceID, reason, result.Connection.Serial,
				result.Connection.ADBEndpoint, result.Connection.AppiumEndpoint, result.Connection.AppiumUDID,
				aggregate.Lifecycle(), aggregate.Health(), domain.STFReadinessStabilizationReason, now, lifecycle); err != nil {
				return err
			}
		}
	} else {
		if aggregate.Health() != domain.HealthUnhealthy {
			if err := aggregate.UpdateHealth(domain.HealthUnhealthy, reason, now); err != nil {
				return err
			}
		}
		if err := aggregate.Transition(domain.DeviceQuarantined, reason, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET pending_image_id=NULL,pending_runtime_profile=NULL,
			reimage_status='failed',reimage_error=$2,lifecycle_status=$3,health_status=$4,health_reason=$2,
			consecutive_failures=consecutive_failures+1,updated_at=$5 WHERE id=$1 AND lifecycle_status=$6`,
			deviceID, reason, aggregate.Lifecycle(), aggregate.Health(), now, lifecycle); err != nil {
			return err
		}
	}
	eventID, err := service.newID()
	if err != nil {
		return err
	}
	severity, eventType := "info", "device_reimage_succeeded"
	if !applied {
		severity, eventType = "error", "device_reimage_failed"
	}
	eventPayload, _ := json.Marshal(map[string]any{"command_id": record.ID, "command_type": record.CommandType,
		"target_image_id": commandPayloadString(payload, "image_id"), "rollback_restored": restored, "error_code": code})
	_, err = tx.Exec(ctx, `INSERT INTO device_health_events
		(id,device_id,source,event_type,severity,reason,payload,observed_at)
		VALUES($1,$2,'agent',$3,$4,$5,$6::jsonb,$7)`, eventID, deviceID, eventType, severity, reason, eventPayload, now)
	return err
}

func (service *Service) reconcileManagementDelete(ctx context.Context, tx pgx.Tx, record repository.CommandRecord,
	deviceID string, lifecycle domain.DeviceLifecycleStatus, health domain.HealthStatus, now time.Time) error {
	var result struct {
		Deleted bool `json:"deleted"`
	}
	succeeded := record.Status == domain.CommandSucceeded && json.Unmarshal(record.Result, &result) == nil && result.Deleted
	code, reason := "", "management delete command removed provider resources"
	if !succeeded {
		code = "AGENT_COMMAND_FAILED"
		if record.ErrorCode != nil && *record.ErrorCode != "" {
			code = *record.ErrorCode
		} else if record.Status == domain.CommandSucceeded {
			code = "COMMAND_RESULT_INVALID"
		}
		reason = code + ": management delete command did not remove provider resources"
	}
	aggregate, err := domain.RestoreDevice(deviceID, lifecycle, health)
	if err != nil {
		return err
	}
	if succeeded {
		if err := aggregate.Transition(domain.DeviceDeleted, reason, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_reason=$3,
			stf_serial=NULL,adb_endpoint=NULL,appium_endpoint=NULL,updated_at=$4
			WHERE id=$1 AND lifecycle_status=$5`, deviceID, aggregate.Lifecycle(), reason, now, lifecycle); err != nil {
			return err
		}
	} else {
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
		if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_status=$3,health_reason=$4,
			consecutive_failures=consecutive_failures+1,updated_at=$5 WHERE id=$1 AND lifecycle_status=$6`,
			deviceID, aggregate.Lifecycle(), aggregate.Health(), reason, now, lifecycle); err != nil {
			return err
		}
	}
	eventID, err := service.newID()
	if err != nil {
		return err
	}
	severity, eventType := "info", "device_management_operation_succeeded"
	if !succeeded {
		severity, eventType = "error", "device_management_operation_failed"
	}
	eventPayload, err := json.Marshal(map[string]any{"command_id": record.ID, "command_type": record.CommandType, "error_code": code})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO device_health_events
		(id,device_id,source,event_type,severity,reason,payload,observed_at)
		VALUES($1,$2,'agent',$3,$4,$5,$6::jsonb,$7)`, eventID, deviceID, eventType, severity, reason, eventPayload, now)
	return err
}

func validManagementOperationResult(value managementOperationResult) bool {
	baseValid := value.Generation > 0 && value.Connection.Serial != "" && value.Connection.AppiumEndpoint != "" &&
		value.Connection.AppiumUDID != "" && value.Health.Online && value.Health.BootCompleted && value.Health.AppiumHealthy
	if strings.EqualFold(value.Platform, "ios") {
		return baseValid && value.State == string(providers.StateRunning) &&
			value.Health.Components[providers.ProbeTransport] == string(providers.ProbePassed) &&
			value.Health.Components[providers.ProbeOSReady] == string(providers.ProbePassed) &&
			value.Health.Components[providers.ProbeAutomation] == string(providers.ProbePassed) &&
			value.Health.Components[providers.ProbeRouter] == string(providers.ProbePassed)
	}
	return baseValid && value.Connection.ADBEndpoint != "" && value.Health.ADBOnline
}

func (service *Service) reconcileManagementStop(ctx context.Context, tx pgx.Tx, record repository.CommandRecord,
	deviceID string, lifecycle domain.DeviceLifecycleStatus, health domain.HealthStatus, now time.Time) error {
	var result managementOperationResult
	succeeded := record.Status == domain.CommandSucceeded && json.Unmarshal(record.Result, &result) == nil &&
		strings.EqualFold(result.Platform, "ios") && result.State == string(providers.StateStopped) &&
		result.Generation > 0 && result.Connection.Serial != "" && result.Connection.AppiumEndpoint != "" &&
		result.Connection.AppiumUDID != ""
	code, reason := "", "Simulator 已停止"
	if !succeeded {
		code = "AGENT_COMMAND_FAILED"
		if record.ErrorCode != nil && *record.ErrorCode != "" {
			code = *record.ErrorCode
		} else if record.Status == domain.CommandSucceeded {
			code = "COMMAND_RESULT_INVALID"
		}
		reason = code + "：停止 Simulator 失败"
	}

	aggregate, err := domain.RestoreDevice(deviceID, lifecycle, health)
	if err != nil {
		return err
	}
	if succeeded {
		if aggregate.Health() != domain.HealthUnknown {
			if err := aggregate.UpdateHealth(domain.HealthUnknown, reason, now); err != nil {
				return err
			}
		}
		if aggregate.Lifecycle() != domain.DeviceStopped {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET health_status=$2,health_reason=$3,
			consecutive_failures=0,last_seen_at=$4,updated_at=$4 WHERE id=$1 AND lifecycle_status=$5`,
			deviceID, aggregate.Health(), reason, now, lifecycle); err != nil {
			return err
		}
	} else {
		if aggregate.Health() != domain.HealthUnhealthy {
			if err := aggregate.UpdateHealth(domain.HealthUnhealthy, reason, now); err != nil {
				return err
			}
		}
		if err := aggregate.Transition(domain.DeviceQuarantined, reason, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_status=$3,health_reason=$4,
			consecutive_failures=consecutive_failures+1,updated_at=$5 WHERE id=$1 AND lifecycle_status=$6`,
			deviceID, aggregate.Lifecycle(), aggregate.Health(), reason, now, lifecycle); err != nil {
			return err
		}
	}

	eventID, err := service.newID()
	if err != nil {
		return err
	}
	severity, eventType := "info", "device_management_operation_succeeded"
	if !succeeded {
		severity, eventType = "error", "device_management_operation_failed"
	}
	eventPayload, err := json.Marshal(map[string]any{"command_id": record.ID, "command_type": record.CommandType, "error_code": code})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO device_health_events
		(id,device_id,source,event_type,severity,reason,payload,observed_at)
		VALUES($1,$2,'agent',$3,$4,$5,$6::jsonb,$7)`, eventID, deviceID, eventType, severity, reason, eventPayload, now)
	return err
}

func commandPayloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func mapValue(values map[string]any, key string) map[string]any {
	value, _ := values[key].(map[string]any)
	return value
}

func validCommandType(value string) bool {
	switch value {
	case "create", "start", "stop", "restart", "rebuild", "delete", "inspect", "validate_image", "sync_android_catalog", "prepare_android_image":
		return true
	default:
		return false
	}
}

func toCommand(record repository.CommandRecord) (Command, error) {
	payload, result := map[string]any{}, map[string]any(nil)
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return Command{}, err
	}
	if len(record.Result) > 0 {
		if err := json.Unmarshal(record.Result, &result); err != nil {
			return Command{}, err
		}
	}
	return Command{ID: record.ID, HostID: record.HostID, CommandType: record.CommandType,
		Payload: payload, Status: record.Status, LeaseToken: record.LeaseToken,
		LeaseExpiresAt: record.LeaseExpiresAt, Attempt: record.Attempts, MaxAttempts: record.MaxAttempts,
		Result: result, ErrorCode: record.ErrorCode, CompletedAt: record.CompletedAt}, nil
}

func translate(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, repository.ErrIdempotencyConflict), errors.Is(err, repository.ErrLeaseConflict):
		return ErrConflict
	default:
		return err
	}
}

func (service *Service) RunLeaseRecovery(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for {
			if _, err := service.RecoverExpiredOnce(ctx); err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

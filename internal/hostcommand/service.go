package hostcommand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/Ad-Quanta/alcor-device-farm/internal/sensitive"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var errorCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)

var (
	ErrInvalidArgument        = errors.New("invalid host command argument")
	ErrNotFound               = errors.New("host command resource not found")
	ErrConflict               = errors.New("host command conflict")
	ErrDeviceIdentityConflict = errors.New("discovered device identity conflict")
	ErrNoCommand              = errors.New("no host command available")
)

type DiscoveredDevice struct {
	ProviderRef     string         `json:"provider_ref"`
	Serial          string         `json:"serial"`
	LifecycleStatus string         `json:"lifecycle_status"`
	HealthStatus    string         `json:"health_status"`
	Connection      map[string]any `json:"connection,omitempty"`
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
		if device.ProviderRef == "" || seenRefs[device.ProviderRef] ||
			!validDiscoveredLifecycle(device.LifecycleStatus) || !validDiscoveredHealth(device.HealthStatus) ||
			(device.LifecycleStatus == string(domain.DeviceReady) && device.HealthStatus != string(domain.HealthHealthy)) {
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
		seenRefs[device.ProviderRef] = true
		if device.Serial != "" {
			seenSerials[device.Serial] = true
		}
	}
	capacity, err := json.Marshal(sensitive.RedactMap(input.Capacity))
	if err != nil {
		return HeartbeatResult{}, ErrInvalidArgument
	}
	usedCapacity, _ := json.Marshal(map[string]any{"device_slots": len(input.Devices)})
	var result HeartbeatResult
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var status domain.HostStatus
		if err := tx.QueryRow(ctx, "SELECT status FROM device_hosts WHERE id=$1 FOR UPDATE", hostID).Scan(&status); err != nil {
			return err
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		target := status
		if status == domain.HostOffline {
			host, err := domain.RestoreHost(hostID, status)
			if err != nil {
				return err
			}
			if err := host.Transition(domain.HostOnline, "agent heartbeat received", now); err != nil {
				return err
			}
			target = host.Status()
		}
		if _, err := tx.Exec(ctx, `UPDATE device_hosts SET status=$2::varchar,draining=($2::varchar='draining'),
            capacity=$3,used_capacity=$4,last_heartbeat_at=$5,updated_at=$5 WHERE id=$1`,
			hostID, target, capacity, usedCapacity, now); err != nil {
			return err
		}
		for _, discovered := range input.Devices {
			if err := updateDiscoveredDevice(ctx, tx, hostID, discovered, now); err != nil {
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

type discoveredDeviceState struct {
	id                string
	lifecycle         domain.DeviceLifecycleStatus
	health            domain.HealthStatus
	healthReason      *string
	operationInFlight bool
}

func updateDiscoveredDevice(ctx context.Context, tx pgx.Tx, hostID string, discovered DiscoveredDevice, now time.Time) error {
	var current discoveredDeviceState
	err := tx.QueryRow(ctx, `SELECT d.id,d.lifecycle_status,d.health_status,d.health_reason,
		EXISTS (SELECT 1 FROM device_host_commands c WHERE c.payload->>'device_id'=d.id
			AND c.command_type IN ('create','rebuild') AND c.status IN ('pending','leased'))
		FROM devices d WHERE d.host_id=$1 AND d.provider_ref=$2 FOR UPDATE OF d`, hostID, discovered.ProviderRef).
		Scan(&current.id, &current.lifecycle, &current.health, &current.healthReason, &current.operationInFlight)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	adbEndpoint, appiumEndpoint, appiumUDID, err := discoveredConnection(discovered.Connection)
	if err != nil {
		return err
	}
	aggregate, err := domain.RestoreDevice(current.id, current.lifecycle, current.health)
	if err != nil {
		return err
	}
	if current.lifecycle != domain.DeviceQuarantined && current.lifecycle != domain.DeviceDeleted && !current.operationInFlight {
		incomingHealth := domain.HealthStatus(discovered.HealthStatus)
		if aggregate.Health() != incomingHealth {
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
	if current.lifecycle != domain.DeviceQuarantined && current.lifecycle != domain.DeviceDeleted && !current.operationInFlight {
		healthReason = nil
		if aggregate.Health() != domain.HealthHealthy {
			value := "agent heartbeat reported " + string(aggregate.Health())
			healthReason = &value
		}
	}
	_, err = tx.Exec(ctx, `UPDATE devices SET serial=CASE WHEN $2='' THEN serial ELSE $2 END,
		adb_endpoint=COALESCE($3,adb_endpoint),appium_endpoint=COALESCE($4,appium_endpoint),
		capabilities=CASE WHEN $5::text IS NULL THEN capabilities ELSE jsonb_set(capabilities,'{appiumUdid}',to_jsonb($5::text),true) END,
		lifecycle_status=$6::varchar,health_status=$7::varchar,health_reason=$8,last_seen_at=$9,updated_at=$9
		WHERE id=$1`, current.id, discovered.Serial, adbEndpoint, appiumEndpoint, appiumUDID,
		aggregate.Lifecycle(), aggregate.Health(), healthReason, now)
	return err
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

func (service *Service) reconcileManagementOperation(ctx context.Context, tx pgx.Tx, record repository.CommandRecord) error {
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
	code, reason := "", "management "+record.CommandType+" command completed"
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
		reason = code + ": management " + record.CommandType + " command did not produce a healthy device"
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
		if _, err := tx.Exec(ctx, `UPDATE devices SET serial=$2,adb_endpoint=$3,appium_endpoint=$4,
			capabilities=jsonb_set(capabilities,'{appiumUdid}',to_jsonb($5::text),true),
			lifecycle_status=$6,health_status=$7,health_reason=NULL,consecutive_failures=0,
			last_seen_at=$8,updated_at=$8 WHERE id=$1 AND lifecycle_status=$9`,
			deviceID, result.Connection.Serial, result.Connection.ADBEndpoint, result.Connection.AppiumEndpoint,
			result.Connection.AppiumUDID, aggregate.Lifecycle(), aggregate.Health(), now, lifecycle); err != nil {
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

func validManagementOperationResult(value managementOperationResult) bool {
	return value.Generation > 0 && value.Connection.Serial != "" && value.Connection.ADBEndpoint != "" &&
		value.Connection.AppiumEndpoint != "" && value.Connection.AppiumUDID != "" && value.Health.Online &&
		value.Health.ADBOnline && value.Health.BootCompleted && value.Health.AppiumHealthy
}

func commandPayloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func validCommandType(value string) bool {
	switch value {
	case "create", "start", "stop", "restart", "rebuild", "delete", "inspect", "validate_image":
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

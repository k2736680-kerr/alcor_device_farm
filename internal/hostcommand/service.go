package hostcommand

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidArgument = errors.New("invalid host command argument")
	ErrNotFound        = errors.New("host command resource not found")
	ErrConflict        = errors.New("host command conflict")
	ErrNoCommand       = errors.New("no host command available")
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
		ID: id, HostID: hostID, CommandType: commandType, Payload: payload,
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
	for _, device := range input.Devices {
		if device.ProviderRef == "" || device.Serial == "" || seenRefs[device.ProviderRef] || seenSerials[device.Serial] {
			return HeartbeatResult{}, ErrInvalidArgument
		}
		seenRefs[device.ProviderRef], seenSerials[device.Serial] = true, true
	}
	capacity, err := json.Marshal(input.Capacity)
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
		result = HeartbeatResult{HostID: hostID, Status: string(target), ReceivedAt: now, Devices: len(input.Devices)}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return HeartbeatResult{}, ErrNotFound
	}
	return result, err
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
	state, err := domain.RestoreCommand(id, domain.CommandLeased)
	if err != nil {
		return Command{}, err
	}
	if err := state.Transition(target, "agent command completion", time.Now().UTC()); err != nil {
		return Command{}, err
	}
	var errorCode *string
	if input.Error != nil && strings.TrimSpace(input.Error.Code) != "" {
		value := input.Error.Code
		errorCode = &value
	}
	record, err := service.repo.Complete(ctx, service.db.Pool(), id, input.LeaseToken, input.Attempt, target, input.Result, errorCode)
	if err != nil {
		return Command{}, translate(err)
	}
	return toCommand(record)
}

func (service *Service) RecoverExpiredOnce(ctx context.Context) (Command, error) {
	record, err := service.repo.RecoverExpiredLease(ctx, service.db.Pool())
	if errors.Is(err, repository.ErrNotFound) {
		return Command{}, ErrNoCommand
	}
	if err != nil {
		return Command{}, err
	}
	return toCommand(record)
}

func validCommandType(value string) bool {
	switch value {
	case "create", "start", "stop", "restart", "rebuild", "delete", "inspect":
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

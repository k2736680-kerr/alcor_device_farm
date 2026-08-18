package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	"github.com/Ad-Quanta/alcor-device-farm/internal/sensitive"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidArgument = errors.New("健康事件参数无效")
	ErrNotFound        = errors.New("未找到指定设备")
)

type Visibility interface {
	Visible(context.Context, string) (bool, error)
}

type EventInput struct {
	Source               string         `json:"source"`
	EventType            string         `json:"event_type"`
	Severity             string         `json:"severity"`
	Reason               string         `json:"reason"`
	ObservedAt           time.Time      `json:"observed_at"`
	Payload              map[string]any `json:"payload,omitempty"`
	ForceQuarantine      bool           `json:"-"`
	SuppressQuarantine   bool           `json:"-"`
	SuppressFailureCount bool           `json:"-"`
}

type Event struct {
	ID         string         `json:"id"`
	DeviceID   string         `json:"device_id"`
	Source     string         `json:"source"`
	EventType  string         `json:"event_type"`
	Severity   string         `json:"severity"`
	Reason     string         `json:"reason"`
	Payload    map[string]any `json:"payload"`
	ObservedAt time.Time      `json:"observed_at"`
	CreatedAt  time.Time      `json:"created_at"`
}

type DeviceState struct {
	ID                  string
	HostID              string
	ProviderRef         string
	Serial              string
	Lifecycle           domain.DeviceLifecycleStatus
	Health              domain.HealthStatus
	HealthReason        string
	ConsecutiveFailures int
	HostStatus          domain.HostStatus
	OperationInFlight   bool
	LatestProvisionedAt *time.Time
	STFFailureStartedAt *time.Time
}

type Result struct {
	HostsMarkedOffline int `json:"hosts_marked_offline"`
	DevicesChecked     int `json:"devices_checked"`
	EventsRecorded     int `json:"events_recorded"`
	DevicesQuarantined int `json:"devices_quarantined"`
}

type Service struct {
	db               *database.DB
	provider         providers.Provider
	visibility       Visibility
	visibilityGrace  time.Duration
	failureThreshold int
	newID            func() (string, error)
	logger           *slog.Logger
}

func New(db *database.DB, provider providers.Provider, visibility Visibility, failureThreshold int, visibilityGrace time.Duration, logger *slog.Logger) *Service {
	if failureThreshold < 1 {
		failureThreshold = 3
	}
	if logger == nil {
		logger = slog.Default()
	}
	if visibilityGrace < 0 {
		visibilityGrace = 0
	}
	return &Service{db: db, provider: provider, visibility: visibility, visibilityGrace: visibilityGrace,
		failureThreshold: failureThreshold, newID: identifier.New, logger: logger}
}

func (service *Service) Report(ctx context.Context, deviceID string, input EventInput) (Event, error) {
	if service == nil || service.db == nil || !validEvent(deviceID, input) {
		return Event{}, ErrInvalidArgument
	}
	if input.Payload == nil {
		input.Payload = map[string]any{}
	}
	input.Reason = sensitive.RedactText(input.Reason)
	input.Payload = sensitive.RedactMap(input.Payload)
	id, err := service.newID()
	if err != nil {
		return Event{}, err
	}
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return Event{}, ErrInvalidArgument
	}
	var event Event
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		device, err := lockDevice(ctx, tx, deviceID)
		if err != nil {
			return err
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		aggregate, err := domain.RestoreDevice(device.ID, device.Lifecycle, device.Health)
		if err != nil {
			return err
		}
		targetHealth, failures := healthOutcome(device, input)
		if targetHealth != aggregate.Health() {
			if err := aggregate.UpdateHealth(targetHealth, input.Reason, now); err != nil {
				return err
			}
		}
		if (input.ForceQuarantine || (!input.SuppressQuarantine && failures >= service.failureThreshold)) &&
			aggregate.Lifecycle() != domain.DeviceQuarantined {
			if err := aggregate.Transition(domain.DeviceQuarantined, input.Reason, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET
			lifecycle_status=$2::varchar,health_status=$3::varchar,health_reason=$4,consecutive_failures=$5,
			last_seen_at=CASE WHEN $3::varchar='healthy' THEN $6::timestamptz ELSE last_seen_at END,updated_at=$6::timestamptz
            WHERE id=$1`, device.ID, aggregate.Lifecycle(), aggregate.Health(), input.Reason, failures, now); err != nil {
			return fmt.Errorf("update device health: %w", err)
		}
		err = tx.QueryRow(ctx, `INSERT INTO device_health_events
            (id,device_id,source,event_type,severity,reason,payload,observed_at)
            VALUES($1,$2,$3,$4,$5,$6,$7,$8)
            RETURNING created_at`, id, device.ID, input.Source, input.EventType,
			input.Severity, input.Reason, payload, input.ObservedAt).Scan(&event.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert device health event: %w", err)
		}
		event = Event{ID: id, DeviceID: device.ID, Source: input.Source, EventType: input.EventType,
			Severity: input.Severity, Reason: input.Reason, Payload: input.Payload,
			ObservedAt: input.ObservedAt, CreatedAt: event.CreatedAt}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return event, err
}

func (service *Service) RunOnce(ctx context.Context, hostTimeout time.Duration) (Result, error) {
	if service == nil || service.db == nil || hostTimeout <= 0 {
		return Result{}, ErrInvalidArgument
	}
	result := Result{}
	offlineCount, err := service.markStaleHostsOffline(ctx, hostTimeout)
	if err != nil {
		return Result{}, err
	}
	result.HostsMarkedOffline = offlineCount
	devices, err := listDevices(ctx, service.db.Pool())
	if err != nil {
		return Result{}, err
	}
	for _, device := range devices {
		if device.Lifecycle == domain.DeviceDeleted || device.Lifecycle == domain.DeviceQuarantined || device.Lifecycle == domain.DeviceRecycling {
			continue
		}
		if device.OperationInFlight && (device.Lifecycle == domain.DeviceProvisioning || device.Lifecycle == domain.DeviceBooting) {
			continue
		}
		result.DevicesChecked++
		input := EventInput{Source: "reconciler", ObservedAt: time.Now().UTC(), Payload: map[string]any{}}
		if device.HostStatus != domain.HostOnline {
			input.EventType, input.Severity, input.Reason = "host_unavailable", "warning", domain.HostUnavailableReason
		} else if service.visibility != nil && schedulableLifecycle(device.Lifecycle) &&
			service.withinVisibilityGrace(device, input.ObservedAt) {
			input.EventType, input.Severity, input.Reason = "stf_stabilizing", "error", domain.STFReadinessStabilizationReason
			input.SuppressFailureCount = true
			input.SuppressQuarantine = true
		} else if service.provider != nil {
			health, inspectErr := service.provider.InspectHealth(ctx, device.ProviderRef)
			switch {
			case inspectErr != nil && providers.ErrorCode(inspectErr) == "PROVIDER_DEVICE_NOT_FOUND":
				input.EventType, input.Severity, input.Reason, input.ForceQuarantine = "provider_device_missing", "critical", inspectErr.Error(), true
			case inspectErr != nil:
				input.EventType, input.Severity, input.Reason = "provider_health_failed", "error", inspectErr.Error()
			case !health.Ready():
				input.EventType, input.Severity, input.Reason = "provider_not_ready", "error", "provider device is not fully ready"
			default:
				input.EventType, input.Severity, input.Reason = "health_recovered", "info", "provider, ADB, boot and Appium checks passed"
				if service.visibility != nil {
					visible, visibilityErr := service.visibility.Visible(ctx, device.Serial)
					if visibilityErr != nil || !visible {
						input.EventType, input.Severity, input.Reason = "stf_not_visible", "error", "device is not visible through STF"
						if visibilityErr != nil {
							input.Reason = visibilityErr.Error()
						}
					}
				}
			}
		} else if service.visibility != nil && schedulableLifecycle(device.Lifecycle) &&
			(device.Health == domain.HealthHealthy || domain.IsSTFFailureReason(device.HealthReason)) {
			visible, visibilityErr := service.visibility.Visible(ctx, device.Serial)
			if visibilityErr == nil && visible {
				if device.Health == domain.HealthHealthy && device.ConsecutiveFailures == 0 {
					continue
				}
				input.EventType, input.Severity, input.Reason = "health_recovered", "info", "STF visibility recovered"
			} else {
				input.EventType, input.Severity, input.Reason = "stf_not_visible", "error", "device is not visible through STF"
				if visibilityErr != nil {
					input.Reason = visibilityErr.Error()
				}
			}
		} else if device.Health != domain.HealthHealthy && schedulableLifecycle(device.Lifecycle) {
			input.EventType, input.Severity, input.Reason = "agent_reported_unhealthy", "error", "agent heartbeat reported an assigned or schedulable device is not healthy"
		} else {
			continue
		}
		if input.EventType == "stf_not_visible" {
			if service.withinVisibilityGrace(device, input.ObservedAt) {
				input.SuppressFailureCount = true
				input.SuppressQuarantine = true
			} else if service.withinSTFOutageGrace(device, input.ObservedAt) {
				input.SuppressQuarantine = true
			}
		}
		before := device.Lifecycle
		if _, err := service.Report(ctx, device.ID, input); err != nil {
			return result, err
		}
		result.EventsRecorded++
		if before != domain.DeviceQuarantined {
			updated, loadErr := getDevice(ctx, service.db.Pool(), device.ID)
			if loadErr != nil {
				return result, loadErr
			}
			if updated.Lifecycle == domain.DeviceQuarantined {
				result.DevicesQuarantined++
			}
		}
	}
	return result, nil
}

func schedulableLifecycle(lifecycle domain.DeviceLifecycleStatus) bool {
	return lifecycle == domain.DeviceReady || lifecycle == domain.DeviceReserved || lifecycle == domain.DeviceBusy
}

func (service *Service) withinVisibilityGrace(device DeviceState, observedAt time.Time) bool {
	if service.visibilityGrace <= 0 || device.LatestProvisionedAt == nil {
		return false
	}
	age := observedAt.Sub(*device.LatestProvisionedAt)
	return age >= 0 && age < service.visibilityGrace
}

func (service *Service) withinSTFOutageGrace(device DeviceState, observedAt time.Time) bool {
	if service.visibilityGrace <= 0 {
		return false
	}
	startedAt := observedAt
	if device.STFFailureStartedAt != nil {
		startedAt = *device.STFFailureStartedAt
	}
	age := observedAt.Sub(startedAt)
	return age >= 0 && age < service.visibilityGrace
}

func (service *Service) markStaleHostsOffline(ctx context.Context, hostTimeout time.Duration) (int, error) {
	rows, err := service.db.Pool().Query(ctx, `SELECT id FROM device_hosts
        WHERE status IN ('online','draining')
          AND COALESCE(last_heartbeat_at,created_at) < clock_timestamp() - make_interval(secs => $1)
        ORDER BY id`, int(hostTimeout/time.Second))
	if err != nil {
		return 0, fmt.Errorf("list stale device hosts: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	count := 0
	for _, id := range ids {
		err := service.db.WithinTx(ctx, func(tx pgx.Tx) error {
			var status domain.HostStatus
			err := tx.QueryRow(ctx, `SELECT status FROM device_hosts
                WHERE id=$1 AND status IN ('online','draining')
                  AND COALESCE(last_heartbeat_at,created_at) < clock_timestamp() - make_interval(secs => $2)
                FOR UPDATE`, id, int(hostTimeout/time.Second)).Scan(&status)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			now, err := database.ClockNow(ctx, tx)
			if err != nil {
				return err
			}
			host, err := domain.RestoreHost(id, status)
			if err != nil {
				return err
			}
			if err := host.Transition(domain.HostOffline, "host heartbeat timed out", now); err != nil {
				return err
			}
			command, err := tx.Exec(ctx, `UPDATE device_hosts SET status='offline',draining=false,updated_at=$2
                WHERE id=$1 AND status=$3`, id, now, status)
			if err != nil {
				return err
			}
			count += int(command.RowsAffected())
			return nil
		})
		if err != nil {
			return count, fmt.Errorf("mark stale device host offline: %w", err)
		}
	}
	return count, nil
}

func (service *Service) Run(ctx context.Context, interval, hostTimeout time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := service.RunOnce(ctx, hostTimeout); err != nil && !errors.Is(err, context.Canceled) {
			service.logger.Error("device reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func validEvent(deviceID string, input EventInput) bool {
	if len(deviceID) < 16 || strings.TrimSpace(input.EventType) == "" || strings.TrimSpace(input.Reason) == "" || input.ObservedAt.IsZero() {
		return false
	}
	switch input.Source {
	case "agent", "provider", "adb", "appium", "stf", "reconciler":
	default:
		return false
	}
	switch input.Severity {
	case "info", "warning", "error", "critical":
		return true
	default:
		return false
	}
}

func healthOutcome(device DeviceState, input EventInput) (domain.HealthStatus, int) {
	failures := device.ConsecutiveFailures
	if !input.SuppressFailureCount {
		failures++
	}
	switch input.Severity {
	case "info":
		if input.EventType == "health_recovered" {
			return domain.HealthHealthy, 0
		}
		return device.Health, device.ConsecutiveFailures
	case "warning":
		return domain.HealthDegraded, failures
	default:
		return domain.HealthUnhealthy, failures
	}
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func listDevices(ctx context.Context, query queryer) ([]DeviceState, error) {
	rows, err := query.Query(ctx, `SELECT d.id,d.host_id,d.provider_ref,d.serial,d.lifecycle_status,
		d.health_status,COALESCE(d.health_reason,''),d.consecutive_failures,h.status,
		EXISTS (SELECT 1 FROM device_host_commands c
			WHERE c.payload->>'device_id'=d.id AND c.command_type IN ('create','rebuild')
			AND c.status IN ('pending','leased')),
		(SELECT max(c.completed_at) FROM device_host_commands c
			WHERE c.payload->>'device_id'=d.id AND c.command_type IN ('create','rebuild') AND c.status='succeeded'),
		(SELECT min(recent.observed_at) FROM (
			SELECT e.observed_at FROM device_health_events e
			WHERE e.device_id=d.id AND e.event_type='stf_not_visible'
			ORDER BY e.created_at DESC LIMIT d.consecutive_failures
		) recent)
		FROM devices d JOIN device_hosts h ON h.id=d.host_id ORDER BY d.created_at,d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DeviceState{}
	for rows.Next() {
		var device DeviceState
		if err := rows.Scan(&device.ID, &device.HostID, &device.ProviderRef, &device.Serial,
			&device.Lifecycle, &device.Health, &device.HealthReason, &device.ConsecutiveFailures, &device.HostStatus,
			&device.OperationInFlight, &device.LatestProvisionedAt, &device.STFFailureStartedAt); err != nil {
			return nil, err
		}
		result = append(result, device)
	}
	return result, rows.Err()
}

func lockDevice(ctx context.Context, tx pgx.Tx, id string) (DeviceState, error) {
	var device DeviceState
	err := tx.QueryRow(ctx, `SELECT d.id,d.host_id,d.provider_ref,d.serial,d.lifecycle_status,
        d.health_status,d.consecutive_failures,h.status
        FROM devices d JOIN device_hosts h ON h.id=d.host_id WHERE d.id=$1 FOR UPDATE OF d`, id).Scan(
		&device.ID, &device.HostID, &device.ProviderRef, &device.Serial,
		&device.Lifecycle, &device.Health, &device.ConsecutiveFailures, &device.HostStatus)
	return device, err
}

func getDevice(ctx context.Context, query queryer, id string) (DeviceState, error) {
	var device DeviceState
	err := query.QueryRow(ctx, `SELECT d.id,d.host_id,d.provider_ref,d.serial,d.lifecycle_status,
        d.health_status,d.consecutive_failures,h.status
        FROM devices d JOIN device_hosts h ON h.id=d.host_id WHERE d.id=$1`, id).Scan(
		&device.ID, &device.HostID, &device.ProviderRef, &device.Serial,
		&device.Lifecycle, &device.Health, &device.ConsecutiveFailures, &device.HostStatus)
	return device, err
}

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
	ID                   string
	HostID               string
	Platform             string
	ProviderRef          string
	Serial               string
	Lifecycle            domain.DeviceLifecycleStatus
	Health               domain.HealthStatus
	HealthReason         string
	AssignmentTarget     string
	ConsecutiveFailures  int
	HostStatus           domain.HostStatus
	OperationInFlight    bool
	DeletionInFlight     bool
	LatestProvisionedAt  *time.Time
	STFFailureStartedAt  *time.Time
	HostFailureStartedAt *time.Time
}

type Result struct {
	HostsMarkedOffline int `json:"hosts_marked_offline"`
	DevicesChecked     int `json:"devices_checked"`
	EventsRecorded     int `json:"events_recorded"`
	DevicesQuarantined int `json:"devices_quarantined"`
	DevicesRecovered   int `json:"devices_recovered"`
	RestartsQueued     int `json:"restarts_queued"`
}

type Service struct {
	db                *database.DB
	provider          providers.Provider
	visibility        Visibility
	visibilityGrace   time.Duration
	hostRecoveryGrace time.Duration
	failureThreshold  int
	newID             func() (string, error)
	logger            *slog.Logger
}

func New(db *database.DB, provider providers.Provider, visibility Visibility, failureThreshold int, visibilityGrace, hostRecoveryGrace time.Duration, logger *slog.Logger) *Service {
	if failureThreshold < 1 {
		failureThreshold = 3
	}
	if logger == nil {
		logger = slog.Default()
	}
	if visibilityGrace < 0 {
		visibilityGrace = 0
	}
	if hostRecoveryGrace < 0 {
		hostRecoveryGrace = 0
	}
	return &Service{db: db, provider: provider, visibility: visibility, visibilityGrace: visibilityGrace, hostRecoveryGrace: hostRecoveryGrace,
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
		if input.EventType == "health_recovered" && targetHealth == domain.HealthHealthy &&
			aggregate.Lifecycle() == domain.DeviceQuarantined && domain.IsSystemRecoverableHealthReason(device.HealthReason) {
			if err := recoverDeviceLifecycle(aggregate, device.AssignmentTarget, input.Reason, now); err != nil {
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
			return fmt.Errorf("更新设备健康状态：%w", err)
		}
		err = tx.QueryRow(ctx, `INSERT INTO device_health_events
            (id,device_id,source,event_type,severity,reason,payload,observed_at)
            VALUES($1,$2,$3,$4,$5,$6,$7,$8)
            RETURNING created_at`, id, device.ID, input.Source, input.EventType,
			input.Severity, input.Reason, payload, input.ObservedAt).Scan(&event.CreatedAt)
		if err != nil {
			return fmt.Errorf("写入设备健康事件：%w", err)
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
		if device.Lifecycle == domain.DeviceDeleted || device.Lifecycle == domain.DeviceRecycling {
			continue
		}
		// A host reboot can leave an Android emulator reported as stopped/unknown
		// even though its pool still requires the slot. This state is not
		// schedulable and used to be skipped forever, requiring an operator to
		// click Restart manually. Queue a non-destructive restart for managed
		// pool devices; an explicit administrator Stop remains respected.
		if device.Lifecycle == domain.DeviceStopped && device.Platform == "android" &&
			device.HostStatus == domain.HostOnline && !device.OperationInFlight && !device.DeletionInFlight {
			queued, err := service.queueUnexpectedStoppedRestart(ctx, device.ID)
			if err != nil {
				return result, err
			}
			if queued {
				result.RestartsQueued++
			}
			continue
		}
		if device.Lifecycle == domain.DeviceQuarantined {
			if device.Platform != "android" || !domain.IsSystemRecoverableHealthReason(device.HealthReason) {
				continue
			}
			if domain.IsSTFFailureReason(device.HealthReason) && service.visibility != nil {
				visible, visibilityErr := service.visibility.Visible(ctx, device.Serial)
				if visibilityErr == nil && visible {
					if _, err := service.Report(ctx, device.ID, EventInput{Source: "reconciler", EventType: "health_recovered",
						Severity: "info", Reason: "STF visibility recovered", ObservedAt: time.Now().UTC(), Payload: map[string]any{}}); err != nil {
						return result, err
					}
					result.DevicesChecked++
					result.EventsRecorded++
					result.DevicesRecovered++
					continue
				}
			}
			queued, err := service.queueSelfHealingRestart(ctx, device.ID)
			if err != nil {
				return result, err
			}
			if queued {
				result.RestartsQueued++
			}
			continue
		}
		if device.OperationInFlight && (device.Lifecycle == domain.DeviceProvisioning || device.Lifecycle == domain.DeviceBooting) {
			continue
		}
		// Provider 删除先移除宿主机资源，再回报 Host Command 成功。这个短窗口内
		// InspectHealth 必然返回不存在；若把它当漂移隔离，会抢先改变 lifecycle，
		// 导致成功的 delete completion 无法按 operation_state 收敛为 deleted。
		if device.DeletionInFlight {
			continue
		}
		result.DevicesChecked++
		usesAndroidHealthChain := device.Platform == "" || device.Platform == "android"
		input := EventInput{Source: "reconciler", ObservedAt: time.Now().UTC(), Payload: map[string]any{}}
		if device.HostStatus != domain.HostOnline {
			input.EventType, input.Severity, input.Reason = "host_unavailable", "warning", domain.HostUnavailableReason
			if service.withinHostRecoveryGrace(device, input.ObservedAt) {
				// A stale heartbeat can briefly mark the host offline while the server or
				// agent is restarting. The scheduler already excludes an offline host, so
				// keep the device's last verified health intact until the recovery grace
				// expires instead of manufacturing a device failure/event.
				if device.HostStatus == domain.HostOffline {
					continue
				}
				input.SuppressFailureCount = true
				input.SuppressQuarantine = true
			}
		} else if usesAndroidHealthChain && service.visibility != nil && schedulableLifecycle(device.Lifecycle) &&
			service.withinVisibilityGrace(device, input.ObservedAt) {
			input.EventType, input.Severity, input.Reason = "stf_stabilizing", "error", domain.STFReadinessStabilizationReason
			input.SuppressFailureCount = true
			input.SuppressQuarantine = true
		} else if usesAndroidHealthChain && service.provider != nil {
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
		} else if usesAndroidHealthChain && service.visibility != nil && schedulableLifecycle(device.Lifecycle) &&
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
			input.EventType, input.Severity, input.Reason = "agent_reported_unhealthy", "error", domain.AgentReportedUnhealthyReason
			// iOS Simulators share one Appium/Device Farm automation service on the Mac.
			// Creating another Simulator can briefly degrade that shared service for every
			// otherwise healthy device. Keep the observation, but do not spend an
			// individual device's quarantine budget; persistent shared failures are
			// handled by host readiness/maintenance instead.
			if device.Platform == "ios" {
				input.EventType, input.Severity = "ios_automation_stabilizing", "warning"
				input.SuppressFailureCount = true
				input.SuppressQuarantine = true
			}
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
		if input.SuppressFailureCount && input.SuppressQuarantine {
			var recentlyRecorded bool
			if err := service.db.Pool().QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM device_health_events
				WHERE device_id=$1 AND event_type=$2 AND reason=$3
				AND observed_at >= clock_timestamp()-interval '1 minute')`,
				device.ID, input.EventType, input.Reason).Scan(&recentlyRecorded); err != nil {
				return result, err
			}
			if recentlyRecorded {
				continue
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
	return lifecycle == domain.DeviceBooting || lifecycle == domain.DeviceReady ||
		lifecycle == domain.DeviceReserved || lifecycle == domain.DeviceBusy
}

func recoverDeviceLifecycle(device *domain.Device, assignmentTarget, reason string, now time.Time) error {
	for _, target := range []domain.DeviceLifecycleStatus{domain.DeviceProvisioning, domain.DeviceBooting, domain.DeviceReady} {
		if err := device.Transition(target, reason, now); err != nil {
			return err
		}
	}
	if assignmentTarget == string(domain.DeviceReserved) || assignmentTarget == string(domain.DeviceBusy) {
		if err := device.Transition(domain.DeviceReserved, reason, now); err != nil {
			return err
		}
	}
	if assignmentTarget == string(domain.DeviceBusy) {
		return device.Transition(domain.DeviceBusy, reason, now)
	}
	return nil
}

func (service *Service) queueSelfHealingRestart(ctx context.Context, deviceID string) (bool, error) {
	return service.queueRestart(ctx, deviceID, false)
}

func (service *Service) queueUnexpectedStoppedRestart(ctx context.Context, deviceID string) (bool, error) {
	return service.queueRestart(ctx, deviceID, true)
}

func (service *Service) queueRestart(ctx context.Context, deviceID string, allowStopped bool) (bool, error) {
	queued := false
	err := service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var hostID, providerRef, platform, healthReason, latestManagementAction string
		var lifecycle domain.DeviceLifecycleStatus
		var health domain.HealthStatus
		lifecyclePredicate := "d.lifecycle_status='quarantined'"
		if allowStopped {
			lifecyclePredicate = "d.lifecycle_status IN ('quarantined','stopped')"
		}
		err := tx.QueryRow(ctx, `SELECT d.host_id,d.provider_ref,d.platform,d.lifecycle_status,d.health_status,
			COALESCE(d.health_reason,''),COALESCE((SELECT c.command_type FROM device_host_commands c
				WHERE c.payload->>'device_id'=d.id AND c.payload->>'operation_source'='management'
				AND c.command_type IN ('stop','start','restart') AND c.status='succeeded'
				ORDER BY c.completed_at DESC NULLS LAST,c.id DESC LIMIT 1),'')
			FROM devices d JOIN device_hosts h ON h.id=d.host_id
			WHERE d.id=$1 AND `+lifecyclePredicate+` AND h.status='online' AND NOT h.draining
			AND EXISTS (SELECT 1 FROM device_pool_devices pd JOIN device_pools p ON p.id=pd.pool_id
				WHERE pd.device_id=d.id AND pd.enabled AND p.platform='android' AND p.status='active' AND p.total_target>0)
			AND NOT EXISTS (SELECT 1 FROM device_reservations r WHERE r.device_id=d.id AND r.status IN ('pending','active'))
			AND NOT EXISTS (SELECT 1 FROM device_sessions s WHERE s.device_id=d.id AND s.status IN ('starting','active','closing'))
			AND NOT EXISTS (SELECT 1 FROM device_host_commands c WHERE c.payload->>'device_id'=d.id AND c.status IN ('pending','leased'))
			FOR UPDATE OF d`, deviceID).Scan(&hostID, &providerRef, &platform, &lifecycle, &health, &healthReason, &latestManagementAction)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if platform != "android" {
			return nil
		}
		if lifecycle == domain.DeviceQuarantined && (healthReason != domain.AgentReportedUnhealthyReason && !domain.IsSTFFailureReason(healthReason)) {
			return nil
		}
		if lifecycle == domain.DeviceStopped && (!allowStopped || latestManagementAction == "stop") {
			return nil
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		var runtimeProfileJSON []byte
		if err := tx.QueryRow(ctx, `SELECT COALESCE(d.runtime_profile_override,i.resource_config,'{}'::jsonb)
			FROM devices d LEFT JOIN device_images i ON i.id=d.image_id WHERE d.id=$1`, deviceID).Scan(&runtimeProfileJSON); err != nil {
			return err
		}
		runtimeProfile := map[string]any{}
		if err := json.Unmarshal(runtimeProfileJSON, &runtimeProfile); err != nil {
			return err
		}
		aggregate, err := domain.RestoreDevice(deviceID, lifecycle, health)
		if err != nil {
			return err
		}
		reason := "系统健康检查持续失败，非破坏重启原设备"
		if lifecycle == domain.DeviceStopped {
			reason = "活动设备池中的 Android 模拟器意外停止，系统自动非破坏重启原设备"
		}
		if aggregate.Health() != domain.HealthUnknown {
			if err := aggregate.UpdateHealth(domain.HealthUnknown, reason, now); err != nil {
				return err
			}
		}
		targetLifecycle := domain.DeviceProvisioning
		if lifecycle == domain.DeviceStopped {
			targetLifecycle = domain.DeviceBooting
		}
		if err := aggregate.Transition(targetLifecycle, reason, now); err != nil {
			return err
		}
		commandID, err := service.newID()
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"operation_source": "self_healing", "operation_state": string(aggregate.Lifecycle()),
			"device_id": deviceID, "host_id": hostID, "provider_ref": providerRef, "previous_reason": healthReason,
			"runtime_profile": runtimeProfile})
		if _, err := tx.Exec(ctx, `INSERT INTO device_host_commands
			(id,host_id,command_type,payload,status,max_attempts,idempotency_key)
			VALUES($1,$2,'restart',$3::jsonb,'pending',3,$4)`, commandID, hostID, payload, "self-heal-restart-"+commandID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_status=$3,health_reason=$4,
			updated_at=$5 WHERE id=$1 AND lifecycle_status=$6`, deviceID, aggregate.Lifecycle(), aggregate.Health(), reason, now, lifecycle); err != nil {
			return err
		}
		eventID, err := service.newID()
		if err != nil {
			return err
		}
		eventPayload, _ := json.Marshal(map[string]any{"command_id": commandID, "previous_reason": healthReason})
		if _, err := tx.Exec(ctx, `INSERT INTO device_health_events
			(id,device_id,source,event_type,severity,reason,payload,observed_at)
			VALUES($1,$2,'reconciler','self_healing_restart_queued','warning',$3,$4::jsonb,$5)`,
			eventID, deviceID, reason, eventPayload, now); err != nil {
			return err
		}
		auditID, err := service.newID()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO device_audit_events
			(id,actor_type,actor_id,action,resource_type,resource_id,request_id,reason,summary)
			VALUES($1,'system','system','restart_device_self_healing','device',$2,$3,$4,
			jsonb_build_object('command_id',$5::text,'previous_reason',$6::text))`, auditID, deviceID,
			"self-heal-restart-"+commandID, reason, commandID, healthReason); err != nil {
			return err
		}
		queued = true
		return nil
	})
	return queued, err
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

func (service *Service) withinHostRecoveryGrace(device DeviceState, observedAt time.Time) bool {
	if service.hostRecoveryGrace <= 0 || device.HostFailureStartedAt == nil {
		return false
	}
	age := observedAt.Sub(*device.HostFailureStartedAt)
	return age >= 0 && age < service.hostRecoveryGrace
}

func (service *Service) markStaleHostsOffline(ctx context.Context, hostTimeout time.Duration) (int, error) {
	// draining 是运维人员控制的安全状态。心跳过期时仍保留排空意图；
	// 心跳年龄指标继续暴露故障，Host 在显式解除排空前始终不可调度。
	rows, err := service.db.Pool().Query(ctx, `SELECT id FROM device_hosts
        WHERE status='online'
          AND COALESCE(last_heartbeat_at,created_at) < clock_timestamp() - make_interval(secs => $1)
        ORDER BY id`, int(hostTimeout/time.Second))
	if err != nil {
		return 0, fmt.Errorf("查询心跳过期的设备宿主机：%w", err)
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
                WHERE id=$1 AND status='online'
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
			return count, fmt.Errorf("将心跳过期的设备宿主机标记为离线：%w", err)
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
			service.logger.Error("设备状态收敛失败", "error", err)
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
	rows, err := query.Query(ctx, `SELECT d.id,d.host_id,d.platform,d.provider_ref,d.serial,d.lifecycle_status,
		d.health_status,COALESCE(d.health_reason,''),
		COALESCE((SELECT CASE WHEN r.status='active' THEN 'busy' ELSE 'reserved' END
			FROM device_reservations r WHERE r.device_id=d.id AND r.status IN ('pending','active')
			ORDER BY CASE WHEN r.status='active' THEN 0 ELSE 1 END,r.updated_at DESC,r.id LIMIT 1),''),
		d.consecutive_failures,h.status,
		EXISTS (SELECT 1 FROM device_host_commands c
			WHERE c.payload->>'device_id'=d.id AND c.command_type IN ('create','rebuild')
			AND c.status IN ('pending','leased')),
		EXISTS (SELECT 1 FROM device_host_commands c
			WHERE c.payload->>'device_id'=d.id AND c.command_type='delete'
			AND c.status IN ('pending','leased')),
		(SELECT max(c.completed_at) FROM device_host_commands c
			WHERE c.payload->>'device_id'=d.id AND c.command_type IN ('create','rebuild') AND c.status='succeeded'),
		(SELECT min(recent.observed_at) FROM (
			SELECT e.observed_at FROM device_health_events e
			WHERE e.device_id=d.id AND e.event_type='stf_not_visible'
			ORDER BY e.created_at DESC LIMIT d.consecutive_failures
		) recent),
		CASE WHEN h.status<>'online' THEN COALESCE(
			NULLIF(h.capabilities->>'host_readiness_failure_started_at','')::timestamptz,
			h.last_heartbeat_at,h.updated_at)
		ELSE NULLIF(h.capabilities->>'host_readiness_failure_started_at','')::timestamptz END
		FROM devices d JOIN device_hosts h ON h.id=d.host_id ORDER BY d.created_at,d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DeviceState{}
	for rows.Next() {
		var device DeviceState
		if err := rows.Scan(&device.ID, &device.HostID, &device.Platform, &device.ProviderRef, &device.Serial,
			&device.Lifecycle, &device.Health, &device.HealthReason, &device.AssignmentTarget, &device.ConsecutiveFailures, &device.HostStatus,
			&device.OperationInFlight, &device.DeletionInFlight, &device.LatestProvisionedAt, &device.STFFailureStartedAt,
			&device.HostFailureStartedAt); err != nil {
			return nil, err
		}
		result = append(result, device)
	}
	return result, rows.Err()
}

func lockDevice(ctx context.Context, tx pgx.Tx, id string) (DeviceState, error) {
	var device DeviceState
	err := tx.QueryRow(ctx, `SELECT d.id,d.host_id,d.provider_ref,d.serial,d.lifecycle_status,
		d.health_status,COALESCE(d.health_reason,''),COALESCE((SELECT CASE WHEN r.status='active' THEN 'busy' ELSE 'reserved' END
			FROM device_reservations r WHERE r.device_id=d.id AND r.status IN ('pending','active')
			ORDER BY CASE WHEN r.status='active' THEN 0 ELSE 1 END,r.updated_at DESC,r.id LIMIT 1),''),
		d.consecutive_failures,h.status
		FROM devices d JOIN device_hosts h ON h.id=d.host_id WHERE d.id=$1 FOR UPDATE OF d`, id).Scan(
		&device.ID, &device.HostID, &device.ProviderRef, &device.Serial,
		&device.Lifecycle, &device.Health, &device.HealthReason, &device.AssignmentTarget, &device.ConsecutiveFailures, &device.HostStatus)
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

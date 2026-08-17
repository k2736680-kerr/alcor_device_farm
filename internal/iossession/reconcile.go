package iossession

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/jackc/pgx/v5"
)

var ErrNothingToReconcile = errors.New("no iOS Session drift to reconcile")

// XCUITest 首次启动 WDA 时可能需要现场编译。Session Grant 只限制开始消费的
// 时间；消费成功后必须给 Fence 足够时间完成 Appium Session 创建和绑定。
// Host Fence 默认命令超时为 270 秒，这里额外保留 30 秒的状态收敛余量。
const iosSessionBindingGraceSeconds = 300

func (service *Service) ReconcileOnce(ctx context.Context) error {
	if service == nil || service.db == nil {
		return ErrInvalidArgument
	}
	var deviceID, reservationID, reason string
	err := service.db.Pool().QueryRow(ctx, `WITH candidates AS (
		SELECT d.id AS device_id,COALESCE(r.id,'') AS reservation_id,CASE
			WHEN COALESCE((d.capabilities->>'providerBusy')::boolean,false) AND r.id IS NULL
				AND NOT EXISTS (
					SELECT 1 FROM device_sessions recent
					WHERE recent.device_id=d.id AND recent.appium_session_ended_at >= clock_timestamp()-interval '30 seconds'
				)
				THEN 'IOS_PROVIDER_BUSY_WITHOUT_RESERVATION'
			WHEN COALESCE((d.capabilities->>'providerBusy')::boolean,false) AND r.id IS NOT NULL
				AND s.appium_session_id IS NULL AND (s.session_grant_consumed_at IS NULL OR
					s.session_grant_consumed_at < clock_timestamp()-make_interval(secs => $1))
				THEN 'IOS_PROVIDER_BUSY_WITHOUT_BOUND_SESSION'
			WHEN NOT COALESCE((d.capabilities->>'providerBusy')::boolean,false) AND s.appium_session_id IS NOT NULL
				AND s.appium_session_ended_at IS NULL AND s.appium_session_started_at < clock_timestamp()-interval '15 seconds'
				THEN 'IOS_BOUND_SESSION_NOT_BUSY'
		END AS reason
		FROM devices d
		LEFT JOIN device_reservations r ON r.device_id=d.id AND r.status='active'
		LEFT JOIN device_sessions s ON s.reservation_id=r.id AND s.status='active'
		WHERE d.platform='ios' AND d.lifecycle_status NOT IN ('quarantined','deleted')
	)
	SELECT device_id,reservation_id,reason FROM candidates WHERE reason IS NOT NULL ORDER BY device_id LIMIT 1`,
		iosSessionBindingGraceSeconds,
	).Scan(&deviceID, &reservationID, &reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNothingToReconcile
	}
	if err != nil {
		return err
	}
	return service.quarantine(ctx, deviceID, reservationID, "ios_session_drift", reason,
		map[string]any{"drift_code": reason})
}

func (service *Service) quarantine(ctx context.Context, deviceID, reservationID, eventType, reason string, payload map[string]any) error {
	return service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var lifecycle domain.DeviceLifecycleStatus
		var health domain.HealthStatus
		if err := tx.QueryRow(ctx, `SELECT lifecycle_status,health_status FROM devices WHERE id=$1 FOR UPDATE`, deviceID).Scan(&lifecycle, &health); err != nil {
			return err
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		aggregate, err := domain.RestoreDevice(deviceID, lifecycle, health)
		if err != nil {
			return err
		}
		if aggregate.Health() != domain.HealthDegraded {
			if err := aggregate.UpdateHealth(domain.HealthDegraded, reason, now); err != nil {
				return err
			}
		}
		if aggregate.Lifecycle() != domain.DeviceQuarantined {
			if err := aggregate.Transition(domain.DeviceQuarantined, reason, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE devices SET lifecycle_status=$2,health_status=$3,health_reason=$4,
			consecutive_failures=consecutive_failures+1,updated_at=$5 WHERE id=$1`,
			deviceID, aggregate.Lifecycle(), aggregate.Health(), reason, now); err != nil {
			return err
		}
		eventID, err := service.newID()
		if err != nil {
			return err
		}
		encoded, _ := json.Marshal(payload)
		if _, err := tx.Exec(ctx, `INSERT INTO device_health_events
			(id,device_id,source,event_type,severity,reason,payload,observed_at)
			VALUES($1,$2,'session_fence',$3,'critical',$4,$5::jsonb,$6)`,
			eventID, deviceID, eventType, reason, encoded, now); err != nil {
			return err
		}
		actor := audit.System()
		actor.ID = "ios_session_reconciler"
		if reservationID == "" {
			return service.insertAuditResource(ctx, tx, actor, "quarantine_ios_session_drift", "device", deviceID,
				"ios_drift_"+eventID, map[string]any{"device_id": deviceID, "reason": reason})
		}
		return service.insertAudit(ctx, tx, actor, "quarantine_ios_session_drift", reservationID,
			"ios_drift_"+eventID, map[string]any{"device_id": deviceID, "reason": reason})
	})
}

func (service *Service) RunReconcile(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for {
			err := service.ReconcileOnce(ctx)
			if errors.Is(err, ErrNothingToReconcile) || errors.Is(err, context.Canceled) {
				break
			}
			if err != nil {
				logger.Error("iOS Session drift reconcile failed", "error", err)
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

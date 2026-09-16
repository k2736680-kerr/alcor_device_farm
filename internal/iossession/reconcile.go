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

var ErrNothingToReconcile = errors.New("当前没有需要收敛的 iOS 会话漂移")

// XCUITest 首次启动 WDA 时可能需要现场编译。Session Grant 只限制开始消费的
// 时间；消费成功后必须给 Fence 足够时间完成 Appium Session 创建和绑定。
// Host Fence 默认命令超时为 270 秒，这里额外保留 30 秒的状态收敛余量。
const (
	iosSessionBindingGraceSeconds            = 300
	iosSessionNotBusyReservationGraceSeconds = 30
)

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
				AND NOT EXISTS (
					-- 会话创建刚刚失败时，provider 侧可能残留瞬时 busy（Appium 会话
					-- 半途建立、WDA 已拉起）。server/网关重建窗口内的这类失败不应
					-- 立即隔离：给 90 秒自愈余量，超时后本条件自动失效、保护照常收敛。
					SELECT 1 FROM device_health_events failed
					WHERE failed.device_id=d.id AND failed.event_type='ios_session_create_failed'
					AND failed.observed_at >= clock_timestamp()-interval '90 seconds'
				)
				THEN 'IOS_PROVIDER_BUSY_WITHOUT_RESERVATION'
			WHEN COALESCE((d.capabilities->>'providerBusy')::boolean,false) AND r.id IS NOT NULL
				AND s.appium_session_id IS NULL AND (s.session_grant_consumed_at IS NULL OR
					s.session_grant_consumed_at < clock_timestamp()-make_interval(secs => $1))
				THEN 'IOS_PROVIDER_BUSY_WITHOUT_BOUND_SESSION'
			WHEN NOT COALESCE((d.capabilities->>'providerBusy')::boolean,false) AND s.appium_session_id IS NOT NULL
				AND s.appium_session_ended_at IS NULL AND s.appium_session_started_at < clock_timestamp()-interval '15 seconds'
				AND r.expires_at > clock_timestamp()+make_interval(secs => $2)
				THEN 'IOS_BOUND_SESSION_NOT_BUSY'
		END AS reason
		FROM devices d
		LEFT JOIN device_reservations r ON r.device_id=d.id AND r.status='active'
		LEFT JOIN device_sessions s ON s.reservation_id=r.id AND s.status='active'
		WHERE d.platform='ios' AND d.lifecycle_status NOT IN ('quarantined','deleted')
	)
	SELECT device_id,reservation_id,reason FROM candidates WHERE reason IS NOT NULL ORDER BY device_id LIMIT 1`,
		iosSessionBindingGraceSeconds, iosSessionNotBusyReservationGraceSeconds,
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
		// 隔离是幂等操作。设备已处于 quarantined 时不再重复写入健康事件、审计或
		// 累加连续失败计数；否则 fence 短暂不可达时，后台 Reaper 会每秒重试失败
		// 的 iOS 会话清理，单设备即可刷出每小时数千条 device_health_events。
		if lifecycle == domain.DeviceQuarantined {
			return nil
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
	// 节流：同一设备同一事件类型的健康事件/审计在窗口内只落一条。上面的幂等
	// 护栏依赖设备保持 quarantined，但自愈/稳定化流程可能在两次 Reaper 重试
	// 之间把设备恢复成 ready/healthy，护栏即被绕过——2026-09-14 曾单设备
	// 4 小时刷出 12352 条 ios_session_cleanup_failed（每秒一条）。状态更新
	// 与连续失败计数不受节流影响，控制台仍能看到最新的 health_reason。
	const quarantineEventThrottleSeconds = 300
	var recentlyReported bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_health_events
		WHERE device_id=$1 AND event_type=$2
		AND observed_at > clock_timestamp()-make_interval(secs => $3))`,
		deviceID, eventType, quarantineEventThrottleSeconds).Scan(&recentlyReported); err != nil {
		return err
	}
	if recentlyReported {
		return nil
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

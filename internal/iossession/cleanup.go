package iossession

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/jackc/pgx/v5"
)

func (service *Service) CloseForReservation(ctx context.Context, reservationID string) error {
	if service == nil || service.db == nil || !validIdentifier(reservationID) {
		return ErrInvalidArgument
	}
	var sessionID, deviceID, hostID, appiumSessionID, fenceEndpoint string
	err := service.db.Pool().QueryRow(ctx, `SELECT s.id,s.device_id,d.host_id,s.appium_session_id,
		COALESCE(h.capabilities->>'session_fence_endpoint','')
		FROM device_sessions s JOIN devices d ON d.id=s.device_id JOIN device_hosts h ON h.id=d.host_id
		WHERE s.reservation_id=$1 AND d.platform='ios' AND s.appium_session_id IS NOT NULL
		AND s.appium_session_ended_at IS NULL`, reservationID).Scan(&sessionID, &deviceID, &hostID, &appiumSessionID, &fenceEndpoint)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	fenceEndpoint, err = validateFenceEndpoint(fenceEndpoint)
	if err == nil && service.agentToken == "" {
		err = ErrHostUnavailable
	}
	if err == nil {
		endpoint := fenceEndpoint + "/internal/v1/ios-session-fence/sessions/" + url.PathEscape(appiumSessionID)
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
		if requestErr != nil {
			err = requestErr
		} else {
			request.Header.Set("Authorization", "Bearer "+service.agentToken)
			request.Header.Set("X-Device-Farm-Host-Id", hostID)
			response, callErr := service.httpClient.Do(request)
			if callErr != nil {
				err = callErr
			} else {
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
				response.Body.Close()
				if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
					err = fmt.Errorf("Fence cleanup returned status %d", response.StatusCode)
				}
			}
		}
	}
	if err != nil {
		reason := "IOS_SESSION_CLEANUP_FAILED: Host Fence could not close the upstream Appium Session"
		if quarantineErr := service.quarantine(ctx, deviceID, reservationID, "ios_session_cleanup_failed", reason,
			map[string]any{"host_id": hostID, "cleanup_failed": true}); quarantineErr != nil {
			return errors.Join(fmt.Errorf("%w: %v", ErrCleanupFailed, err), quarantineErr)
		}
		return fmt.Errorf("%w: %v", ErrCleanupFailed, err)
	}
	return service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `UPDATE device_sessions SET appium_session_ended_at=$3,updated_at=$3
			WHERE id=$1 AND appium_session_id=$2 AND appium_session_ended_at IS NULL`, sessionID, appiumSessionID, now)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return nil
		}
		actor := audit.System()
		actor.ID = "ios_session_reaper"
		return service.insertAudit(ctx, tx, actor, "cleanup_ios_appium_session", reservationID,
			"ios_cleanup_"+reservationID, map[string]any{"device_id": deviceID, "host_id": hostID, "appium_session_closed": true})
	})
}

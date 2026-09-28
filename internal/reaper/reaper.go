package reaper

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
)

// Policy carries the age bounds the Reaper enforces. Every field is a duration
// measured from a row's own timestamp, so the Reaper stays a pure sweep with no
// hidden clock of its own.
type Policy struct {
	// GracePeriod extends a reservation's own expiry before it is reaped, so a
	// briefly late release does not race the reaper.
	GracePeriod time.Duration
	// PendingTimeout bounds how long a reservation may wait for a device. A zero
	// value disables the sweep, which is how existing deployments that predate
	// this bound keep their previous behavior.
	PendingTimeout time.Duration
	// PendingClaimGrace bounds how long a reservation may hold a device while
	// still pending, covering the crash window between reserving a device and
	// activating the claim. Only consulted when PendingTimeout is set.
	PendingClaimGrace time.Duration
}

type Reaper struct {
	service *reservation.Service
	policy  Policy
	logger  *slog.Logger
}

func New(service *reservation.Service, policy Policy, logger *slog.Logger) *Reaper {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reaper{service: service, policy: policy, logger: logger}
}

func (reaper *Reaper) RunOnce(ctx context.Context) (reservation.View, error) {
	return reaper.service.ReapOnce(ctx, reaper.policy.GracePeriod)
}

// RunPendingOnce expires one reservation that waited too long for a device.
// It reports (reservation.ErrNothingToReap, nil) when nothing was eligible, and
// a nil error with a zero View when the sweep is disabled by configuration.
func (reaper *Reaper) RunPendingOnce(ctx context.Context) (reservation.View, error) {
	if reaper.policy.PendingTimeout <= 0 {
		return reservation.View{}, reservation.ErrNothingToReap
	}
	return reaper.service.ReapStalePendingOnce(
		ctx, reaper.policy.PendingTimeout, reaper.policy.PendingClaimGrace)
}

// RunOrphanedClaimOnce releases one reservation that stalled while already
// holding a device, restoring the device to a schedulable lifecycle.
func (reaper *Reaper) RunOrphanedClaimOnce(ctx context.Context) (reservation.View, error) {
	if reaper.policy.PendingTimeout <= 0 {
		return reservation.View{}, reservation.ErrNothingToReap
	}
	return reaper.service.ReapOrphanedClaimOnce(ctx, reaper.policy.PendingClaimGrace)
}

func (reaper *Reaper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for {
			value, err := reaper.RunOnce(ctx)
			if err == nil {
				reaper.logger.Info("expired device reservation reaped", "reservation_id", value.ID, "device_id", value.DeviceID)
				continue
			}
			if !errors.Is(err, reservation.ErrNothingToReap) && !errors.Is(err, context.Canceled) {
				reaper.logger.Error("device reservation reaper cycle failed", "error", err)
			}
			break
		}
		for {
			remote, err := reaper.service.ReapRemoteSessionOnce(ctx)
			if err == nil {
				reaper.logger.Info("expired STF remote session closed", "remote_session_id", remote.ID, "reservation_id", remote.ReservationID)
				continue
			}
			if !errors.Is(err, reservation.ErrNothingToReapRemote) && !errors.Is(err, context.Canceled) {
				reaper.logger.Error("STF remote session reaper cycle failed", "error", err)
			}
			break
		}
		// Drain stale pending requests before orphaned claims: a reservation
		// without a device is the common case and is cheap, while the orphan
		// sweep takes row locks on both the reservation and its device.
		for {
			value, err := reaper.RunPendingOnce(ctx)
			if err == nil {
				reaper.logger.Info("stale pending device reservation failed",
					"reservation_id", value.ID, "failure_code", value.FailureCode)
				continue
			}
			if !errors.Is(err, reservation.ErrNothingToReap) && !errors.Is(err, context.Canceled) {
				reaper.logger.Error("stale pending reservation sweep failed", "error", err)
			}
			break
		}
		for {
			value, err := reaper.RunOrphanedClaimOnce(ctx)
			if err == nil {
				reaper.logger.Info("orphaned device claim released",
					"reservation_id", value.ID, "device_id", value.DeviceID)
				continue
			}
			if !errors.Is(err, reservation.ErrNothingToReap) && !errors.Is(err, context.Canceled) {
				reaper.logger.Error("orphaned device claim sweep failed", "error", err)
			}
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
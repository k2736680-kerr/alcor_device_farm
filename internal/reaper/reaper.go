package reaper

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
)

type Reaper struct {
	service *reservation.Service
	grace   time.Duration
	logger  *slog.Logger
}

func New(service *reservation.Service, gracePeriod time.Duration, logger *slog.Logger) *Reaper {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reaper{service: service, grace: gracePeriod, logger: logger}
}

func (reaper *Reaper) RunOnce(ctx context.Context) (reservation.View, error) {
	return reaper.service.ReapOnce(ctx, reaper.grace)
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
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNoPendingReservation = errors.New("no pending reservation")
	ErrCapacityUnavailable  = errors.New("matching device capacity is unavailable")
)

type IDGenerator func() (string, error)

type Assignment struct {
	Reservation repository.ReservationRecord
	Session     repository.SessionRecord
}

type Scheduler struct {
	db     *database.DB
	repo   repository.ReservationRepository
	newID  IDGenerator
	logger *slog.Logger
}

func New(db *database.DB, generator IDGenerator, logger *slog.Logger) *Scheduler {
	if generator == nil {
		generator = identifier.New
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{db: db, repo: repository.ReservationRepository{}, newID: generator, logger: logger}
}

func (scheduler *Scheduler) RunOnce(ctx context.Context) (Assignment, error) {
	var assignment Assignment
	err := scheduler.db.WithinTx(ctx, func(tx pgx.Tx) error {
		reservation, err := scheduler.repo.LockNextAllocatablePending(ctx, tx)
		if errors.Is(err, repository.ErrNotFound) {
			hasPending, checkErr := scheduler.repo.HasPending(ctx, tx)
			if checkErr != nil {
				return checkErr
			}
			if hasPending {
				return ErrCapacityUnavailable
			}
			return ErrNoPendingReservation
		}
		if err != nil {
			return err
		}
		policy, err := scheduler.repo.LockPoolPolicy(ctx, tx, reservation.PoolID)
		if err != nil {
			return err
		}
		if policy.Status != "active" {
			return ErrCapacityUnavailable
		}
		if policy.ActiveReservations >= policy.MaxConcurrency {
			return ErrCapacityUnavailable
		}
		device, err := scheduler.repo.LockMatchingDevice(ctx, tx, reservation.PoolID, reservation.RequestedCapabilities)
		if errors.Is(err, repository.ErrCapacityUnavailable) {
			return ErrCapacityUnavailable
		}
		if err != nil {
			return err
		}

		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		reservationState, err := domain.RestoreReservation(reservation.ID, reservation.Status)
		if err != nil {
			return err
		}
		if err := reservationState.Transition(domain.ReservationActive, "scheduler allocated matching device", now); err != nil {
			return err
		}
		deviceState, err := domain.RestoreDevice(device.ID, device.Lifecycle, device.Health)
		if err != nil {
			return err
		}
		if err := deviceState.Transition(domain.DeviceReserved, "scheduler reserved device", now); err != nil {
			return err
		}
		if err := deviceState.Transition(domain.DeviceBusy, "device session started", now); err != nil {
			return err
		}
		sessionID, err := scheduler.newID()
		if err != nil {
			return fmt.Errorf("generate session ID: %w", err)
		}
		sessionState, err := domain.NewSession(sessionID)
		if err != nil {
			return err
		}
		if err := sessionState.Transition(domain.SessionActive, "scheduler started session", now); err != nil {
			return err
		}
		active, session, err := scheduler.repo.Activate(ctx, tx, reservation, device, sessionID)
		if err != nil {
			return err
		}
		assignment = Assignment{Reservation: active, Session: session}
		return nil
	})
	if err != nil {
		return Assignment{}, err
	}
	return assignment, nil
}

func (scheduler *Scheduler) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for {
			assignment, err := scheduler.RunOnce(ctx)
			if err == nil {
				scheduler.logger.Info("device reservation allocated", "reservation_id", assignment.Reservation.ID, "device_id", assignment.Reservation.DeviceID)
				continue
			}
			if !errors.Is(err, ErrNoPendingReservation) && !errors.Is(err, ErrCapacityUnavailable) && !errors.Is(err, context.Canceled) {
				scheduler.logger.Error("device scheduler cycle failed", "error", err)
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

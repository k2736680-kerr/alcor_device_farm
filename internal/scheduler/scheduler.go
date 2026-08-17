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
	ErrSTFClaimFailed       = errors.New("STF device claim failed")
)

type IDGenerator func() (string, error)

type Assignment struct {
	Reservation repository.ReservationRecord
	Session     repository.SessionRecord
}

type claimCandidate struct {
	reservation repository.ReservationRecord
	device      repository.DeviceAssignment
}

type DeviceClaimer interface {
	Claim(context.Context, string, time.Duration) error
	Release(context.Context, string) error
}

type Scheduler struct {
	db      *database.DB
	repo    repository.ReservationRepository
	newID   IDGenerator
	logger  *slog.Logger
	claimer DeviceClaimer
}

func New(db *database.DB, generator IDGenerator, logger *slog.Logger, claimers ...DeviceClaimer) *Scheduler {
	if generator == nil {
		generator = identifier.New
	}
	if logger == nil {
		logger = slog.Default()
	}
	var claimer DeviceClaimer
	if len(claimers) > 0 {
		claimer = claimers[0]
	}
	return &Scheduler{db: db, repo: repository.ReservationRepository{}, newID: generator, logger: logger, claimer: claimer}
}

func (scheduler *Scheduler) RunOnce(ctx context.Context) (Assignment, error) {
	var selected claimCandidate
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
		deviceState, err := domain.RestoreDevice(device.ID, device.Lifecycle, device.Health)
		if err != nil {
			return err
		}
		if err := deviceState.Transition(domain.DeviceReserved, "scheduler reserved device", now); err != nil {
			return err
		}
		if err := scheduler.repo.ReserveForClaim(ctx, tx, reservation.ID, device.ID, now); err != nil {
			return err
		}
		reservation.DeviceID = &device.ID
		selected = claimCandidate{reservation: reservation, device: device}
		return nil
	})
	if err != nil {
		return Assignment{}, err
	}
	if scheduler.claimer != nil && selected.device.Platform == "android" {
		if err := scheduler.claimer.Claim(ctx, selected.device.Serial, time.Duration(selected.reservation.LeaseSeconds)*time.Second); err != nil {
			terminal := !isRetryable(err)
			compensateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if compensateErr := scheduler.compensateClaim(compensateCtx, selected, "STF_CLAIM_FAILED", terminal); compensateErr != nil {
				return Assignment{}, errors.Join(fmt.Errorf("%w: %v", ErrSTFClaimFailed, err), compensateErr)
			}
			return Assignment{}, fmt.Errorf("%w: %v", ErrSTFClaimFailed, err)
		}
	}
	sessionID, err := scheduler.newID()
	if err != nil {
		scheduler.releaseAndCompensate(selected, "SESSION_ID_GENERATION_FAILED", true)
		return Assignment{}, fmt.Errorf("generate session ID: %w", err)
	}
	var assignment Assignment
	err = scheduler.db.WithinTx(ctx, func(tx pgx.Tx) error {
		reservation, err := scheduler.repo.LockByID(ctx, tx, selected.reservation.ID)
		if err != nil {
			return err
		}
		if reservation.Status != domain.ReservationPending || reservation.DeviceID == nil || *reservation.DeviceID != selected.device.ID {
			return repository.ErrNotFound
		}
		policy, err := scheduler.repo.LockPoolPolicy(ctx, tx, reservation.PoolID)
		if err != nil {
			return err
		}
		if policy.Status != "active" || policy.ActiveReservations >= policy.MaxConcurrency {
			return ErrCapacityUnavailable
		}
		device, err := scheduler.repo.LockDevice(ctx, tx, selected.device.ID)
		if err != nil {
			return err
		}
		reservationState, err := domain.RestoreReservation(reservation.ID, reservation.Status)
		if err != nil {
			return err
		}
		if err := reservationState.Transition(domain.ReservationActive, "device allocation claim succeeded", time.Now().UTC()); err != nil {
			return err
		}
		deviceState, err := domain.RestoreDevice(device.ID, device.Lifecycle, device.Health)
		if err != nil {
			return err
		}
		if err := deviceState.Transition(domain.DeviceBusy, "device session started after allocation claim", time.Now().UTC()); err != nil {
			return err
		}
		sessionState, err := domain.NewSession(sessionID)
		if err != nil {
			return err
		}
		if err := sessionState.Transition(domain.SessionActive, "scheduler started session", time.Now().UTC()); err != nil {
			return err
		}
		active, session, err := scheduler.repo.ActivateClaimed(ctx, tx, reservation, device, sessionID)
		if err != nil {
			return err
		}
		assignment = Assignment{Reservation: active, Session: session}
		return nil
	})
	if err != nil {
		scheduler.releaseAndCompensate(selected, "STF_CLAIM_COMPENSATED", false)
		return Assignment{}, err
	}
	return assignment, nil
}

func (scheduler *Scheduler) releaseAndCompensate(selected claimCandidate, failureCode string, terminal bool) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if scheduler.claimer != nil && selected.device.Platform == "android" {
		if err := scheduler.claimer.Release(cleanupCtx, selected.device.Serial); err != nil {
			scheduler.logger.Error("STF claim cleanup release failed", "reservation_id", selected.reservation.ID, "device_id", selected.device.ID, "error", err)
		}
	}
	if err := scheduler.compensateClaim(cleanupCtx, selected, failureCode, terminal); err != nil {
		scheduler.logger.Error("STF claim database compensation failed", "reservation_id", selected.reservation.ID, "device_id", selected.device.ID, "error", err)
	}
}

func (scheduler *Scheduler) compensateClaim(ctx context.Context, selected claimCandidate, failureCode string, terminal bool) error {
	return scheduler.db.WithinTx(ctx, func(tx pgx.Tx) error {
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		return scheduler.repo.CompensateClaim(ctx, tx, selected.reservation.ID, selected.device.ID, failureCode, terminal, now)
	})
}

func isRetryable(err error) bool {
	type retryable interface{ IsRetryable() bool }
	var value retryable
	return errors.As(err, &value) && value.IsRetryable()
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
			if !errors.Is(err, ErrNoPendingReservation) && !errors.Is(err, ErrCapacityUnavailable) &&
				!errors.Is(err, ErrSTFClaimFailed) && !errors.Is(err, context.Canceled) {
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

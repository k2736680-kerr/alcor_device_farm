package reservation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidArgument     = errors.New("invalid reservation argument")
	ErrNotFound            = errors.New("reservation not found")
	ErrConflict            = errors.New("reservation conflict")
	ErrPoolUnavailable     = errors.New("device pool is unavailable")
	ErrCapacityUnavailable = errors.New("matching device capacity is unavailable")
	ErrNothingToReap       = errors.New("no expired reservation to reap")
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type IDGenerator func() (string, error)

type CreateInput struct {
	PoolID                string         `json:"pool_id"`
	OwnerType             string         `json:"owner_type"`
	OwnerID               string         `json:"owner_id"`
	RequestedCapabilities map[string]any `json:"requested_capabilities,omitempty"`
	LeaseSeconds          int            `json:"lease_seconds"`
}

type Filter struct {
	OwnerType string
	OwnerID   string
}

type ExtensionInput struct {
	AdditionalSeconds int `json:"additional_seconds"`
}

type ReleaseInput struct {
	Reason string `json:"reason"`
	Force  bool   `json:"force,omitempty"`
}

type View struct {
	ID                    string                   `json:"id"`
	PoolID                string                   `json:"pool_id"`
	DeviceID              *string                  `json:"device_id,omitempty"`
	OwnerType             string                   `json:"owner_type"`
	OwnerID               string                   `json:"owner_id"`
	RequestedCapabilities map[string]any           `json:"requested_capabilities"`
	LeaseSeconds          int                      `json:"lease_seconds"`
	Status                domain.ReservationStatus `json:"status"`
	StartsAt              *time.Time               `json:"starts_at,omitempty"`
	ExpiresAt             *time.Time               `json:"expires_at,omitempty"`
	ReleasedAt            *time.Time               `json:"released_at,omitempty"`
	FailureCode           *string                  `json:"failure_code,omitempty"`
	CreatedAt             time.Time                `json:"created_at"`
	UpdatedAt             time.Time                `json:"updated_at"`
}

type Service struct {
	db    *database.DB
	repo  repository.ReservationRepository
	newID IDGenerator
}

func NewService(db *database.DB, generator IDGenerator) *Service {
	if generator == nil {
		generator = identifier.New
	}
	return &Service{db: db, repo: repository.ReservationRepository{}, newID: generator}
}

func (service *Service) Create(ctx context.Context, clientID, key string, input CreateInput) (View, error) {
	if service == nil || service.db == nil {
		return View{}, fmt.Errorf("%w: database is not configured", ErrPoolUnavailable)
	}
	if err := validateCreate(clientID, key, input); err != nil {
		return View{}, err
	}
	policy, err := service.repo.GetPoolPolicy(ctx, service.db.Pool(), input.PoolID)
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	if policy.Status != "active" {
		return View{}, ErrPoolUnavailable
	}
	if input.LeaseSeconds > policy.MaxLeaseSeconds {
		return View{}, fmt.Errorf("%w: lease_seconds exceeds pool maximum", ErrInvalidArgument)
	}
	if input.RequestedCapabilities == nil {
		input.RequestedCapabilities = map[string]any{}
	}
	id, err := service.newID()
	if err != nil {
		return View{}, fmt.Errorf("generate reservation ID: %w", err)
	}
	record, err := service.repo.CreatePending(ctx, service.db.Pool(), repository.CreateReservationParams{
		ID: id, ClientID: clientID, PoolID: input.PoolID, OwnerType: input.OwnerType,
		OwnerID: input.OwnerID, RequestedCapabilities: input.RequestedCapabilities,
		LeaseSeconds: input.LeaseSeconds, IdempotencyKey: key,
	})
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return toView(record)
}

func (service *Service) Get(ctx context.Context, id string) (View, error) {
	if service == nil || service.db == nil {
		return View{}, fmt.Errorf("%w: database is not configured", ErrPoolUnavailable)
	}
	if !identifierPattern.MatchString(id) {
		return View{}, ErrInvalidArgument
	}
	record, err := service.repo.Get(ctx, service.db.Pool(), id)
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return toView(record)
}

func (service *Service) List(ctx context.Context, filter Filter) ([]View, error) {
	if service == nil || service.db == nil {
		return nil, fmt.Errorf("%w: database is not configured", ErrPoolUnavailable)
	}
	if filter.OwnerType != "" && !validOwnerType(filter.OwnerType) {
		return nil, ErrInvalidArgument
	}
	if filter.OwnerID != "" && !identifierPattern.MatchString(filter.OwnerID) {
		return nil, ErrInvalidArgument
	}
	records, err := service.repo.List(ctx, service.db.Pool(), repository.ReservationFilter{
		OwnerType: filter.OwnerType, OwnerID: filter.OwnerID,
	})
	if err != nil {
		return nil, translateRepositoryError(err)
	}
	views := make([]View, 0, len(records))
	for _, record := range records {
		view, convertErr := toView(record)
		if convertErr != nil {
			return nil, convertErr
		}
		views = append(views, view)
	}
	return views, nil
}

func (service *Service) Extend(ctx context.Context, clientID, key, id string, input ExtensionInput) (View, error) {
	if service == nil || service.db == nil {
		return View{}, fmt.Errorf("%w: database is not configured", ErrPoolUnavailable)
	}
	if strings.TrimSpace(clientID) == "" || len(key) < 8 || len(key) > 128 ||
		!identifierPattern.MatchString(id) || input.AdditionalSeconds < 60 {
		return View{}, ErrInvalidArgument
	}
	hash, err := requestHash(map[string]any{"reservation_id": id, "additional_seconds": input.AdditionalSeconds})
	if err != nil {
		return View{}, err
	}
	var result repository.ReservationRecord
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		replay, err := service.repo.BeginOperation(ctx, tx, repository.OperationParams{
			ClientID: clientID, Scope: "extend_device_reservation", Key: key, RequestHash: hash,
			ResourceType: "device_reservation", ResourceID: id, ResponseStatus: 200,
		})
		if err != nil {
			return err
		}
		if replay {
			result, err = service.repo.Get(ctx, tx, id)
			return err
		}
		current, err := service.repo.LockByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Status != domain.ReservationActive || current.StartsAt == nil || current.ExpiresAt == nil {
			return ErrConflict
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		if !current.ExpiresAt.After(now) {
			return fmt.Errorf("%w: expired reservation cannot be extended", ErrConflict)
		}
		policy, err := service.repo.GetPoolPolicy(ctx, tx, current.PoolID)
		if err != nil {
			return err
		}
		maximumExpiry := current.StartsAt.Add(time.Duration(policy.MaxLeaseSeconds) * time.Second)
		remainingSeconds := int64(maximumExpiry.Sub(*current.ExpiresAt) / time.Second)
		if int64(input.AdditionalSeconds) > remainingSeconds {
			return fmt.Errorf("%w: extension exceeds pool maximum lease", ErrInvalidArgument)
		}
		result, err = service.repo.Extend(ctx, tx, id, current.ExpiresAt.Add(time.Duration(input.AdditionalSeconds)*time.Second))
		return err
	})
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return toView(result)
}

func (service *Service) Release(
	ctx context.Context,
	clientID, key, id, requestID string,
	input ReleaseInput,
) (View, error) {
	if service == nil || service.db == nil {
		return View{}, fmt.Errorf("%w: database is not configured", ErrPoolUnavailable)
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if strings.TrimSpace(clientID) == "" || len(key) < 8 || len(key) > 128 ||
		!identifierPattern.MatchString(id) || len(input.Reason) < 3 || len(input.Reason) > 500 || requestID == "" {
		return View{}, ErrInvalidArgument
	}
	hash, err := requestHash(map[string]any{"reservation_id": id, "reason": input.Reason, "force": input.Force})
	if err != nil {
		return View{}, err
	}
	var result repository.ReservationRecord
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		replay, err := service.repo.BeginOperation(ctx, tx, repository.OperationParams{
			ClientID: clientID, Scope: "release_device_reservation", Key: key, RequestHash: hash,
			ResourceType: "device_reservation", ResourceID: id, ResponseStatus: 200,
		})
		if err != nil {
			return err
		}
		if replay {
			result, err = service.repo.Get(ctx, tx, id)
			return err
		}
		current, err := service.repo.LockByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Status != domain.ReservationActive {
			if isClosedStatus(current.Status) {
				result = current
				return nil
			}
			return ErrConflict
		}
		terminal := domain.ReservationReleased
		action := "release_device_reservation"
		if input.Force {
			terminal = domain.ReservationForceReleased
			action = "force_release_device_reservation"
		}
		result, err = service.closeActiveLocked(ctx, tx, current, terminal, "service", clientID, action, requestID, input.Reason)
		return err
	})
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return toView(result)
}

func (service *Service) ReapOnce(ctx context.Context, gracePeriod time.Duration) (View, error) {
	if service == nil || service.db == nil || gracePeriod < 0 {
		return View{}, ErrInvalidArgument
	}
	graceSeconds := int(gracePeriod / time.Second)
	var result repository.ReservationRecord
	err := service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		current, err := service.repo.LockNextExpired(ctx, tx, graceSeconds)
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNothingToReap
		}
		if err != nil {
			return err
		}
		requestID := "reaper_" + current.ID
		result, err = service.closeActiveLocked(
			ctx, tx, current, domain.ReservationExpired, "system", "reservation_reaper",
			"expire_device_reservation", requestID, "reservation lease and grace period expired",
		)
		return err
	})
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return toView(result)
}

func (service *Service) closeActiveLocked(
	ctx context.Context,
	tx pgx.Tx,
	current repository.ReservationRecord,
	terminal domain.ReservationStatus,
	actorType, actorID, action, requestID, reason string,
) (repository.ReservationRecord, error) {
	if current.DeviceID == nil {
		return repository.ReservationRecord{}, ErrConflict
	}
	now, err := database.ClockNow(ctx, tx)
	if err != nil {
		return repository.ReservationRecord{}, err
	}
	reservationState, err := domain.RestoreReservation(current.ID, current.Status)
	if err != nil {
		return repository.ReservationRecord{}, err
	}
	if err := reservationState.Transition(terminal, reason, now); err != nil {
		return repository.ReservationRecord{}, err
	}
	session, err := service.repo.LockSession(ctx, tx, current.ID)
	if err != nil {
		return repository.ReservationRecord{}, err
	}
	sessionState, err := domain.RestoreSession(session.ID, session.Status)
	if err != nil {
		return repository.ReservationRecord{}, err
	}
	if err := sessionState.Transition(domain.SessionClosing, reason, now); err != nil {
		return repository.ReservationRecord{}, err
	}
	if err := sessionState.Transition(domain.SessionClosed, reason, now); err != nil {
		return repository.ReservationRecord{}, err
	}
	device, err := service.repo.LockDevice(ctx, tx, *current.DeviceID)
	if err != nil {
		return repository.ReservationRecord{}, err
	}
	deviceState, err := domain.RestoreDevice(device.ID, device.Lifecycle, device.Health)
	if err != nil {
		return repository.ReservationRecord{}, err
	}
	if err := deviceState.Transition(domain.DeviceRecycling, reason, now); err != nil {
		return repository.ReservationRecord{}, err
	}
	auditID, err := service.newID()
	if err != nil {
		return repository.ReservationRecord{}, err
	}
	closed, err := service.repo.CloseActive(ctx, tx, current.ID, *current.DeviceID, terminal, now)
	if err != nil {
		return repository.ReservationRecord{}, err
	}
	if err := service.repo.InsertAudit(ctx, tx, auditID, actorType, actorID, action, current.ID, requestID, reason); err != nil {
		return repository.ReservationRecord{}, err
	}
	return closed, nil
}

func validateCreate(clientID, key string, input CreateInput) error {
	if strings.TrimSpace(clientID) == "" || len(key) < 8 || len(key) > 128 ||
		!identifierPattern.MatchString(input.PoolID) || !identifierPattern.MatchString(input.OwnerID) ||
		!validOwnerType(input.OwnerType) || input.LeaseSeconds < 60 {
		return ErrInvalidArgument
	}
	if input.RequestedCapabilities == nil {
		input.RequestedCapabilities = map[string]any{}
	}
	if _, err := json.Marshal(input.RequestedCapabilities); err != nil {
		return fmt.Errorf("%w: requested_capabilities is not valid JSON", ErrInvalidArgument)
	}
	return nil
}

func validOwnerType(value string) bool {
	switch value {
	case "run_attempt", "manual", "test_run":
		return true
	default:
		return false
	}
}

func isClosedStatus(status domain.ReservationStatus) bool {
	switch status {
	case domain.ReservationReleased, domain.ReservationExpired, domain.ReservationForceReleased:
		return true
	default:
		return false
	}
}

func requestHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode idempotent request: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func toView(record repository.ReservationRecord) (View, error) {
	capabilities := map[string]any{}
	if len(record.RequestedCapabilities) > 0 {
		if err := json.Unmarshal(record.RequestedCapabilities, &capabilities); err != nil {
			return View{}, fmt.Errorf("decode requested capabilities: %w", err)
		}
	}
	return View{
		ID: record.ID, PoolID: record.PoolID, DeviceID: record.DeviceID,
		OwnerType: record.OwnerType, OwnerID: record.OwnerID,
		RequestedCapabilities: capabilities, LeaseSeconds: record.LeaseSeconds,
		Status: record.Status, StartsAt: record.StartsAt, ExpiresAt: record.ExpiresAt,
		ReleasedAt: record.ReleasedAt, FailureCode: record.FailureCode,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}, nil
}

func translateRepositoryError(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, repository.ErrIdempotencyConflict):
		return ErrConflict
	case errors.Is(err, repository.ErrPoolUnavailable):
		return ErrPoolUnavailable
	case errors.Is(err, repository.ErrCapacityUnavailable):
		return ErrCapacityUnavailable
	default:
		return err
	}
}

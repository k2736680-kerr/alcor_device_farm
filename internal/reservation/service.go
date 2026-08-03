package reservation

import (
	"context"
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
)

var (
	ErrInvalidArgument     = errors.New("invalid reservation argument")
	ErrNotFound            = errors.New("reservation not found")
	ErrConflict            = errors.New("reservation conflict")
	ErrPoolUnavailable     = errors.New("device pool is unavailable")
	ErrCapacityUnavailable = errors.New("matching device capacity is unavailable")
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

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

	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/stf"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/Ad-Quanta/alcor-device-farm/internal/sensitive"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidArgument     = errors.New("invalid reservation argument")
	ErrNotFound            = errors.New("reservation not found")
	ErrConflict            = errors.New("reservation conflict")
	ErrPoolUnavailable     = errors.New("device pool is unavailable")
	ErrCapacityUnavailable = errors.New("matching device capacity is unavailable")
	ErrNothingToReap       = errors.New("no expired reservation to reap")
	ErrNothingToReapRemote = errors.New("no expired STF remote session to reap")
	ErrForbidden           = errors.New("reservation access is forbidden")
	ErrSTFReleaseFailed    = errors.New("STF device release failed")
	ErrSTFRemoteFailed     = errors.New("STF remote connection failed")
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

type RemoteSessionInput struct {
	OwnerType  string `json:"owner_type"`
	OwnerID    string `json:"owner_id"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

type RemoteSessionView struct {
	ID            string    `json:"id"`
	ReservationID string    `json:"reservation_id"`
	URL           string    `json:"remote_connect_url,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type remoteSessionMetadata struct {
	ID             string    `json:"id"`
	URL            string    `json:"remote_connect_url"`
	ExpiresAt      time.Time `json:"expires_at"`
	OwnerType      string    `json:"owner_type"`
	OwnerID        string    `json:"owner_id"`
	ClientID       string    `json:"client_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	RequestHash    string    `json:"request_hash"`
}

type STFController interface {
	Release(context.Context, string) error
	RemoteConnect(context.Context, string) (stf.RemoteConnection, error)
	RemoteDisconnect(context.Context, string) error
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
	stf   STFController
}

func NewService(db *database.DB, generator IDGenerator, controllers ...STFController) *Service {
	if generator == nil {
		generator = identifier.New
	}
	var controller STFController
	if len(controllers) > 0 {
		controller = controllers[0]
	}
	return &Service{db: db, repo: repository.ReservationRepository{}, newID: generator, stf: controller}
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
		!identifierPattern.MatchString(id) || len(input.Reason) < 3 || len(input.Reason) > 500 || sensitive.Contains(input.Reason) || requestID == "" {
		return View{}, ErrInvalidArgument
	}
	hash, err := requestHash(map[string]any{"reservation_id": id, "reason": input.Reason, "force": input.Force})
	if err != nil {
		return View{}, err
	}
	operation := repository.OperationParams{
		ClientID: clientID, Scope: "release_device_reservation", Key: key, RequestHash: hash,
		ResourceType: "device_reservation", ResourceID: id, ResponseStatus: 200,
	}
	if _, err := service.repo.CheckOperation(ctx, service.db.Pool(), operation); err != nil {
		return View{}, translateRepositoryError(err)
	}
	current, err := service.repo.Get(ctx, service.db.Pool(), id)
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	if current.ClientID != clientID {
		return View{}, ErrForbidden
	}
	if current.Status == domain.ReservationActive {
		if err := service.releaseSTF(ctx, current); err != nil {
			auditErr := service.recordSTFFailure(context.Background(), current, "service", clientID, requestID,
				"stf_release_failed", "STF release failed; reservation remains active")
			return View{}, errors.Join(err, auditErr)
		}
	}
	var result repository.ReservationRecord
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		replay, err := service.repo.BeginOperation(ctx, tx, operation)
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
		if current.Status == domain.ReservationPending {
			if current.DeviceID != nil {
				return fmt.Errorf("%w: reservation claim is in progress", ErrConflict)
			}
			now, err := database.ClockNow(ctx, tx)
			if err != nil {
				return err
			}
			state, err := domain.RestoreReservation(current.ID, current.Status)
			if err != nil {
				return err
			}
			if err := state.Transition(domain.ReservationFailed, input.Reason, now); err != nil {
				return err
			}
			result, err = service.repo.CancelPending(ctx, tx, current.ID, "RESERVATION_CANCELED", now)
			if err != nil {
				return err
			}
			auditID, err := service.newID()
			if err != nil {
				return err
			}
			return service.repo.InsertAudit(ctx, tx, auditID, "service", clientID, "cancel_pending_device_reservation", current.ID, requestID, input.Reason)
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
	selected, err := service.repo.FindNextExpired(ctx, service.db.Pool(), graceSeconds)
	if errors.Is(err, repository.ErrNotFound) {
		return View{}, ErrNothingToReap
	}
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	requestID := "reaper_" + selected.ID
	if err := service.releaseSTF(ctx, selected); err != nil {
		auditErr := service.recordSTFFailure(context.Background(), selected, "system", "reservation_reaper", requestID,
			"stf_release_failed", "STF release failed during expiry; reservation remains active")
		return View{}, errors.Join(err, auditErr)
	}
	var result repository.ReservationRecord
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		current, err := service.repo.LockByID(ctx, tx, selected.ID)
		if err != nil {
			return err
		}
		if current.Status != domain.ReservationActive || current.ExpiresAt == nil {
			return ErrNothingToReap
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		if current.ExpiresAt.Add(gracePeriod).After(now) {
			return ErrNothingToReap
		}
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

func (service *Service) CreateRemoteSession(
	ctx context.Context,
	clientID, key, reservationID string,
	input RemoteSessionInput,
) (RemoteSessionView, error) {
	if service == nil || service.db == nil || service.stf == nil {
		return RemoteSessionView{}, fmt.Errorf("%w: STF is not configured", ErrSTFRemoteFailed)
	}
	if input.TTLSeconds == 0 {
		input.TTLSeconds = 300
	}
	if strings.TrimSpace(clientID) == "" || len(key) < 8 || len(key) > 128 ||
		!identifierPattern.MatchString(reservationID) || !validOwnerType(input.OwnerType) ||
		!identifierPattern.MatchString(input.OwnerID) || input.TTLSeconds < 30 || input.TTLSeconds > 3600 {
		return RemoteSessionView{}, ErrInvalidArgument
	}
	hash, err := requestHash(map[string]any{
		"reservation_id": reservationID, "owner_type": input.OwnerType,
		"owner_id": input.OwnerID, "ttl_seconds": input.TTLSeconds,
	})
	if err != nil {
		return RemoteSessionView{}, err
	}
	operation := repository.OperationParams{
		ClientID: clientID, Scope: "create_stf_remote_session", Key: key, RequestHash: hash,
		ResourceType: "device_reservation", ResourceID: reservationID, ResponseStatus: 201,
	}
	replay, err := service.repo.CheckOperation(ctx, service.db.Pool(), operation)
	if err != nil {
		return RemoteSessionView{}, translateRepositoryError(err)
	}
	if replay {
		return service.loadRemoteSession(ctx, reservationID, clientID, key, hash)
	}
	current, err := service.repo.Get(ctx, service.db.Pool(), reservationID)
	if err != nil {
		return RemoteSessionView{}, translateRepositoryError(err)
	}
	if current.ClientID != clientID || current.OwnerType != input.OwnerType || current.OwnerID != input.OwnerID {
		return RemoteSessionView{}, ErrForbidden
	}
	if current.Status != domain.ReservationActive || current.DeviceID == nil {
		return RemoteSessionView{}, ErrConflict
	}
	device, err := service.repo.GetDevice(ctx, service.db.Pool(), *current.DeviceID)
	if err != nil {
		return RemoteSessionView{}, translateRepositoryError(err)
	}
	remoteID, err := service.newID()
	if err != nil {
		return RemoteSessionView{}, fmt.Errorf("generate remote session ID: %w", err)
	}
	connection, err := service.stf.RemoteConnect(ctx, device.Serial)
	if err != nil {
		return RemoteSessionView{}, fmt.Errorf("%w: %w", ErrSTFRemoteFailed, err)
	}
	metadata := remoteSessionMetadata{
		ID: remoteID, URL: connection.URL, ExpiresAt: time.Now().UTC().Add(time.Duration(input.TTLSeconds) * time.Second),
		OwnerType: input.OwnerType, OwnerID: input.OwnerID, ClientID: clientID,
		IdempotencyKey: key, RequestHash: hash,
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		service.disconnectRemote(device.Serial)
		return RemoteSessionView{}, err
	}
	var stored remoteSessionMetadata
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		locked, err := service.repo.LockByID(ctx, tx, reservationID)
		if err != nil {
			return err
		}
		if locked.Status != domain.ReservationActive || locked.DeviceID == nil || *locked.DeviceID != device.ID ||
			locked.ClientID != clientID || locked.OwnerType != input.OwnerType || locked.OwnerID != input.OwnerID {
			return ErrConflict
		}
		replay, err := service.repo.BeginOperation(ctx, tx, operation)
		if err != nil {
			return err
		}
		if replay {
			session, err := service.repo.LockSession(ctx, tx, reservationID)
			if err != nil {
				return err
			}
			stored, err = decodeRemoteSession(session.ConnectionMetadata)
			return err
		}
		session, err := service.repo.SetRemoteSession(ctx, tx, reservationID, encoded)
		if err != nil {
			return err
		}
		stored, err = decodeRemoteSession(session.ConnectionMetadata)
		return err
	})
	if err != nil {
		service.disconnectRemote(device.Serial)
		return RemoteSessionView{}, translateRepositoryError(err)
	}
	return remoteSessionView(reservationID, stored), nil
}

func (service *Service) ReapRemoteSessionOnce(ctx context.Context) (RemoteSessionView, error) {
	if service == nil || service.db == nil || service.stf == nil {
		return RemoteSessionView{}, ErrNothingToReapRemote
	}
	remote, err := service.repo.FindNextExpiredRemoteSession(ctx, service.db.Pool())
	if errors.Is(err, repository.ErrNotFound) {
		return RemoteSessionView{}, ErrNothingToReapRemote
	}
	if err != nil {
		return RemoteSessionView{}, translateRepositoryError(err)
	}
	if err := service.stf.RemoteDisconnect(ctx, remote.Serial); err != nil {
		return RemoteSessionView{}, fmt.Errorf("%w: %w", ErrSTFRemoteFailed, err)
	}
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		return service.repo.ClearRemoteSession(ctx, tx, remote.ReservationID, remote.ID)
	})
	if errors.Is(err, repository.ErrNotFound) {
		return RemoteSessionView{}, ErrNothingToReapRemote
	}
	if err != nil {
		return RemoteSessionView{}, translateRepositoryError(err)
	}
	return RemoteSessionView{ID: remote.ID, ReservationID: remote.ReservationID, ExpiresAt: remote.ExpiresAt}, nil
}

func (service *Service) releaseSTF(ctx context.Context, current repository.ReservationRecord) error {
	if service.stf == nil || current.DeviceID == nil {
		return nil
	}
	device, err := service.repo.GetDevice(ctx, service.db.Pool(), *current.DeviceID)
	if err != nil {
		return translateRepositoryError(err)
	}
	if err := service.stf.Release(ctx, device.Serial); err != nil {
		return fmt.Errorf("%w: %w", ErrSTFReleaseFailed, err)
	}
	return nil
}

func (service *Service) disconnectRemote(serial string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = service.stf.RemoteDisconnect(ctx, serial)
}

func (service *Service) loadRemoteSession(ctx context.Context, reservationID, clientID, key, hash string) (RemoteSessionView, error) {
	session, err := service.repo.GetSession(ctx, service.db.Pool(), reservationID)
	if err != nil {
		return RemoteSessionView{}, translateRepositoryError(err)
	}
	metadata, err := decodeRemoteSession(session.ConnectionMetadata)
	if err != nil || metadata.ClientID != clientID || metadata.IdempotencyKey != key || metadata.RequestHash != hash {
		return RemoteSessionView{}, ErrConflict
	}
	if !metadata.ExpiresAt.After(time.Now().UTC()) {
		return RemoteSessionView{}, ErrConflict
	}
	return remoteSessionView(reservationID, metadata), nil
}

func decodeRemoteSession(connectionMetadata json.RawMessage) (remoteSessionMetadata, error) {
	var envelope struct {
		Remote remoteSessionMetadata `json:"stf_remote_session"`
	}
	if err := json.Unmarshal(connectionMetadata, &envelope); err != nil {
		return remoteSessionMetadata{}, fmt.Errorf("decode STF remote session: %w", err)
	}
	if envelope.Remote.ID == "" || envelope.Remote.URL == "" || envelope.Remote.ExpiresAt.IsZero() {
		return remoteSessionMetadata{}, ErrConflict
	}
	return envelope.Remote, nil
}

func remoteSessionView(reservationID string, metadata remoteSessionMetadata) RemoteSessionView {
	return RemoteSessionView{ID: metadata.ID, ReservationID: reservationID, URL: metadata.URL, ExpiresAt: metadata.ExpiresAt}
}

func (service *Service) recordSTFFailure(
	ctx context.Context,
	current repository.ReservationRecord,
	actorType, actorID, requestID, action, reason string,
) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		auditID, err := service.newID()
		if err != nil {
			return err
		}
		return service.repo.InsertAudit(ctx, tx, auditID, actorType, actorID, action, current.ID, requestID, reason)
	})
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
	if sensitive.ContainsMap(input.RequestedCapabilities) {
		return ErrInvalidArgument
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
	case domain.ReservationReleased, domain.ReservationExpired, domain.ReservationForceReleased, domain.ReservationFailed:
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

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
	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/Ad-Quanta/alcor-device-farm/internal/sensitive"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidArgument     = errors.New("预约参数无效")
	ErrNotFound            = errors.New("预约不存在")
	ErrConflict            = errors.New("预约状态冲突")
	ErrPoolUnavailable     = errors.New("设备池当前不可用")
	ErrCapacityUnavailable = errors.New("暂无满足条件的设备容量")
	ErrNothingToReap       = errors.New("没有需要回收的过期预约")
	ErrNothingToReapRemote = errors.New("没有需要回收的过期 STF 远控会话")
	ErrForbidden           = errors.New("无权访问该预约")
	ErrSTFReleaseFailed    = errors.New("STF 设备释放失败")
	ErrSTFRemoteFailed     = errors.New("STF 远程连接失败")
	ErrIOSSessionCleanup   = errors.New("iOS Appium 会话清理失败")
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type IDGenerator func() (string, error)

type CreateInput struct {
	PoolID                string         `json:"pool_id"`
	RequestedDeviceID     string         `json:"requested_device_id,omitempty"`
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

type IOSSessionController interface {
	CloseForReservation(context.Context, string) error
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
	ios   IOSSessionController
}

func (service *Service) SetIOSSessionController(controller IOSSessionController) {
	if service != nil {
		service.ios = controller
	}
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

func (service *Service) Create(ctx context.Context, actor audit.Actor, key string, input CreateInput) (View, error) {
	return service.create(ctx, actor, key, input, strings.TrimSpace(input.RequestedDeviceID))
}

func (service *Service) CreateForDevice(
	ctx context.Context,
	actor audit.Actor,
	key, deviceID string,
	leaseSeconds int,
) (View, error) {
	if service == nil || service.db == nil {
		return View{}, fmt.Errorf("%w：数据库未配置", ErrPoolUnavailable)
	}
	if !actor.Valid() || len(key) < 8 || len(key) > 128 || !validOwnerID("manual", actor.ID) ||
		!identifierPattern.MatchString(deviceID) || leaseSeconds < 60 {
		return View{}, ErrInvalidArgument
	}
	if existing, err := service.repo.FindOpenTargeted(ctx, service.db.Pool(), actor.ID, deviceID); err == nil {
		return toView(existing)
	} else if !errors.Is(err, repository.ErrNotFound) {
		return View{}, translateRepositoryError(err)
	}
	poolID, err := service.repo.FindActivePoolForDevice(ctx, service.db.Pool(), deviceID)
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return service.create(ctx, actor, key, CreateInput{
		PoolID: poolID, OwnerType: "manual", OwnerID: actor.ID,
		RequestedCapabilities: map[string]any{}, LeaseSeconds: leaseSeconds,
	}, deviceID)
}

func (service *Service) create(ctx context.Context, actor audit.Actor, key string, input CreateInput, targetDeviceID string) (View, error) {
	if service == nil || service.db == nil {
		return View{}, fmt.Errorf("%w：数据库未配置", ErrPoolUnavailable)
	}
	normalizedCapabilities, err := normalizeRequestedCapabilities(input.RequestedCapabilities)
	if err != nil {
		return View{}, err
	}
	input.RequestedCapabilities = normalizedCapabilities
	if err := validateCreate(actor, key, input); err != nil {
		return View{}, err
	}
	clientID := actor.ClientID
	policy, err := service.repo.GetPoolPolicy(ctx, service.db.Pool(), input.PoolID)
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	if policy.Status != "active" {
		return View{}, ErrPoolUnavailable
	}
	if platformName, exists := input.RequestedCapabilities["platformName"]; exists &&
		strings.ToLower(platformName.(string)) != policy.Platform {
		return View{}, fmt.Errorf("%w：platformName 与设备池平台不匹配", ErrInvalidArgument)
	}
	if input.LeaseSeconds > policy.MaxLeaseSeconds {
		return View{}, fmt.Errorf("%w：lease_seconds 超过设备池最大续约窗口", ErrInvalidArgument)
	}
	if input.RequestedCapabilities == nil {
		input.RequestedCapabilities = map[string]any{}
	}
	if targetDeviceID != "" {
		if !identifierPattern.MatchString(targetDeviceID) {
			return View{}, ErrInvalidArgument
		}
		if err := service.repo.ValidateTargetDeviceInPool(ctx, service.db.Pool(), input.PoolID, targetDeviceID); err != nil {
			return View{}, translateRepositoryError(err)
		}
		input.RequestedCapabilities[repository.TargetDeviceCapability] = targetDeviceID
	}
	id, err := service.newID()
	if err != nil {
		return View{}, fmt.Errorf("generate reservation ID: %w", err)
	}
	params := repository.CreateReservationParams{
		ID: id, ClientID: clientID, PoolID: input.PoolID, OwnerType: input.OwnerType,
		OwnerID: input.OwnerID, RequestedCapabilities: input.RequestedCapabilities,
		LeaseSeconds: input.LeaseSeconds, IdempotencyKey: key,
	}
	var record repository.ReservationRecord
	if targetDeviceID == "" {
		record, err = service.repo.CreatePending(ctx, service.db.Pool(), params)
	} else {
		err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
			if lockErr := service.repo.LockTargetDevice(ctx, tx, targetDeviceID); lockErr != nil {
				return lockErr
			}
			existing, findErr := service.repo.FindOpenTargeted(ctx, tx, "", targetDeviceID)
			switch {
			case findErr == nil && existing.OwnerID == actor.ID:
				record = existing
				return nil
			case findErr == nil:
				return ErrConflict
			case !errors.Is(findErr, repository.ErrNotFound):
				return findErr
			}
			created, createErr := service.repo.CreatePending(ctx, tx, params)
			if createErr != nil {
				return createErr
			}
			record = created
			return nil
		})
	}
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return toView(record)
}

func (service *Service) FindOpenForDevice(ctx context.Context, ownerID, deviceID string) (View, error) {
	if service == nil || service.db == nil || !validOwnerID("manual", ownerID) || !identifierPattern.MatchString(deviceID) {
		return View{}, ErrInvalidArgument
	}
	record, err := service.repo.FindOpenTargeted(ctx, service.db.Pool(), ownerID, deviceID)
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return toView(record)
}

func (service *Service) KeepAliveForDevice(
	ctx context.Context,
	reservationID, ownerID, deviceID string,
	lease time.Duration,
) (View, error) {
	if service == nil || service.db == nil || lease < time.Minute || !identifierPattern.MatchString(reservationID) ||
		!validOwnerID("manual", ownerID) || !identifierPattern.MatchString(deviceID) {
		return View{}, ErrInvalidArgument
	}
	record, err := service.repo.KeepAliveTargeted(ctx, service.db.Pool(), reservationID, ownerID, deviceID, lease)
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return toView(record)
}

func (service *Service) Get(ctx context.Context, id string) (View, error) {
	if service == nil || service.db == nil {
		return View{}, fmt.Errorf("%w：数据库未配置", ErrPoolUnavailable)
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

func (service *Service) List(ctx context.Context, filter Filter, page paging.Page) (paging.Result[View], error) {
	if service == nil || service.db == nil {
		return paging.Result[View]{}, fmt.Errorf("%w：数据库未配置", ErrPoolUnavailable)
	}
	if filter.OwnerType != "" && !validOwnerType(filter.OwnerType) {
		return paging.Result[View]{}, ErrInvalidArgument
	}
	if filter.OwnerID != "" && !validOwnerID(filter.OwnerType, filter.OwnerID) {
		return paging.Result[View]{}, ErrInvalidArgument
	}
	records, total, err := service.repo.List(ctx, service.db.Pool(), repository.ReservationFilter{
		OwnerType: filter.OwnerType, OwnerID: filter.OwnerID,
	}, page)
	if err != nil {
		return paging.Result[View]{}, translateRepositoryError(err)
	}
	views := make([]View, 0, len(records))
	for _, record := range records {
		view, convertErr := toView(record)
		if convertErr != nil {
			return paging.Result[View]{}, convertErr
		}
		views = append(views, view)
	}
	return paging.NewResult(views, page, total), nil
}

func (service *Service) Extend(ctx context.Context, actor audit.Actor, key, id string, input ExtensionInput) (View, error) {
	if service == nil || service.db == nil {
		return View{}, fmt.Errorf("%w：数据库未配置", ErrPoolUnavailable)
	}
	clientID := actor.ClientID
	if !actor.Valid() || len(key) < 8 || len(key) > 128 ||
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
			return fmt.Errorf("%w：预约已经过期，不能续约", ErrConflict)
		}
		policy, err := service.repo.GetPoolPolicy(ctx, tx, current.PoolID)
		if err != nil {
			return err
		}
		if input.AdditionalSeconds > policy.MaxLeaseSeconds {
			return fmt.Errorf("%w：单次续约秒数不能超过设备池最大续约窗口 %d 秒", ErrInvalidArgument, policy.MaxLeaseSeconds)
		}
		// max_lease_seconds is a rolling safety window, not a reservation lifetime
		// limit. A healthy worker may therefore keep a long-running reservation
		// alive indefinitely, while a crashed worker still stops extending it and
		// is collected by the Reaper after this bounded future window.
		maximumExpiry := now.Add(time.Duration(policy.MaxLeaseSeconds) * time.Second)
		nextExpiry := current.ExpiresAt.Add(time.Duration(input.AdditionalSeconds) * time.Second)
		if nextExpiry.After(maximumExpiry) {
			nextExpiry = maximumExpiry
		}
		result, err = service.repo.Extend(ctx, tx, id, nextExpiry)
		return err
	})
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return toView(result)
}

func (service *Service) Release(
	ctx context.Context,
	actor audit.Actor,
	key, id, requestID string,
	input ReleaseInput,
) (View, error) {
	if service == nil || service.db == nil {
		return View{}, fmt.Errorf("%w：数据库未配置", ErrPoolUnavailable)
	}
	clientID := actor.ClientID
	input.Reason = strings.TrimSpace(input.Reason)
	if !actor.Valid() || len(key) < 8 || len(key) > 128 ||
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
	// A manual reservation may be created through Alcor's trusted service
	// gateway on behalf of a Console user. In that case the persisted
	// idempotency client is "service", while the authenticated Console actor is
	// still the reservation owner. Treat that as an ordinary owner release.
	// Cross-owner releases remain limited to the explicit force path, whose API
	// authorization only permits Console administrators (or a trusted service).
	ownedByActor := current.ClientID == clientID ||
		(actor.Type == audit.ActorConsole && current.OwnerType == "manual" && current.OwnerID == actor.ID)
	if !ownedByActor && !input.Force {
		return View{}, ErrForbidden
	}
	if current.Status == domain.ReservationActive {
		if err := service.closeIOSSession(ctx, current); err != nil {
			return View{}, err
		}
		if err := service.releaseSTF(ctx, current); err != nil {
			auditErr := service.recordSTFFailure(context.Background(), current, actor, requestID,
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
				return fmt.Errorf("%w：预约设备仍在领取中", ErrConflict)
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
			return service.repo.InsertAudit(ctx, tx, auditID, actor.Type, actor.ID, "cancel_pending_device_reservation", current.ID, requestID, input.Reason)
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
		result, err = service.closeActiveLocked(ctx, tx, current, terminal, actor, action, requestID, input.Reason)
		return err
	})
	if err != nil {
		return View{}, translateRepositoryError(err)
	}
	return toView(result)
}

// reaperActor is the identity recorded when the background reaper expires a
// reservation. No request is involved, so the audit row must not borrow the
// identity of whoever created the reservation.
func reaperActor() audit.Actor {
	actor := audit.System()
	actor.ID = "reservation_reaper"
	return actor
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
	if err := service.closeIOSSession(ctx, selected); err != nil {
		return View{}, err
	}
	if err := service.releaseSTF(ctx, selected); err != nil {
		auditErr := service.recordSTFFailure(context.Background(), selected, reaperActor(), requestID,
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
			ctx, tx, current, domain.ReservationExpired, reaperActor(),
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
	actor audit.Actor,
	key, reservationID string,
	input RemoteSessionInput,
) (RemoteSessionView, error) {
	if service == nil || service.db == nil || service.stf == nil {
		return RemoteSessionView{}, fmt.Errorf("%w：STF 尚未配置", ErrSTFRemoteFailed)
	}
	clientID := actor.ClientID
	if input.TTLSeconds == 0 {
		input.TTLSeconds = 300
	}
	if !actor.Valid() || len(key) < 8 || len(key) > 128 ||
		!identifierPattern.MatchString(reservationID) || !validOwnerType(input.OwnerType) ||
		!validOwnerID(input.OwnerType, input.OwnerID) || input.TTLSeconds < 30 || input.TTLSeconds > 3600 {
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
	connection, err := service.stf.RemoteConnect(ctx, device.STFSerial)
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
		service.disconnectRemote(device.STFSerial)
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
		service.disconnectRemote(device.STFSerial)
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
	if device.Platform != "android" {
		return nil
	}
	if err := service.stf.Release(ctx, device.STFSerial); err != nil {
		return fmt.Errorf("%w: %w", ErrSTFReleaseFailed, err)
	}
	return nil
}

func (service *Service) closeIOSSession(ctx context.Context, current repository.ReservationRecord) error {
	if service.ios == nil || current.DeviceID == nil {
		return nil
	}
	if err := service.ios.CloseForReservation(ctx, current.ID); err != nil {
		return fmt.Errorf("%w: %w", ErrIOSSessionCleanup, err)
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
	actor audit.Actor,
	requestID, action, reason string,
) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		auditID, err := service.newID()
		if err != nil {
			return err
		}
		return service.repo.InsertAudit(ctx, tx, auditID, actor.Type, actor.ID, action, current.ID, requestID, reason)
	})
}

func (service *Service) closeActiveLocked(
	ctx context.Context,
	tx pgx.Tx,
	current repository.ReservationRecord,
	terminal domain.ReservationStatus,
	actor audit.Actor,
	action, requestID, reason string,
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
	// Reconciler/Host heartbeats may have already restored the device to ready
	// before the reservation is closed. Device transitions are intentionally
	// strict, so avoid an invalid ready -> ready transition and keep release and
	// Reaper retries idempotent.
	if lifecycle := deviceState.Lifecycle(); lifecycle != domain.DeviceQuarantined && lifecycle != domain.DeviceReady {
		if err := deviceState.Transition(domain.DeviceReady, "reservation released; device data retained", now); err != nil {
			return repository.ReservationRecord{}, err
		}
	}
	auditID, err := service.newID()
	if err != nil {
		return repository.ReservationRecord{}, err
	}
	closed, err := service.repo.CloseActive(ctx, tx, current.ID, *current.DeviceID, terminal, now)
	if err != nil {
		return repository.ReservationRecord{}, err
	}
	if err := service.repo.InsertAudit(ctx, tx, auditID, actor.Type, actor.ID, action, current.ID, requestID, reason); err != nil {
		return repository.ReservationRecord{}, err
	}
	return closed, nil
}

func validateCreate(actor audit.Actor, key string, input CreateInput) error {
	if !actor.Valid() || len(key) < 8 || len(key) > 128 ||
		!identifierPattern.MatchString(input.PoolID) || !validOwnerID(input.OwnerType, input.OwnerID) ||
		!validOwnerType(input.OwnerType) || input.LeaseSeconds < 60 {
		return ErrInvalidArgument
	}
	if input.RequestedDeviceID != "" && !identifierPattern.MatchString(input.RequestedDeviceID) {
		return ErrInvalidArgument
	}
	if input.RequestedCapabilities == nil {
		input.RequestedCapabilities = map[string]any{}
	}
	if sensitive.ContainsMap(input.RequestedCapabilities) {
		return ErrInvalidArgument
	}
	for key := range input.RequestedCapabilities {
		if strings.HasPrefix(key, "_device_farm_") {
			return fmt.Errorf("%w：requested_capabilities 包含系统保留字段", ErrInvalidArgument)
		}
	}
	if _, err := json.Marshal(input.RequestedCapabilities); err != nil {
		return fmt.Errorf("%w：requested_capabilities 不是有效的 JSON", ErrInvalidArgument)
	}
	return nil
}

func normalizeRequestedCapabilities(source map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	value, exists := result["platformName"]
	if !exists {
		return result, nil
	}
	platformName, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("%w：platformName 必须是 Android 或 iOS", ErrInvalidArgument)
	}
	switch strings.ToLower(strings.TrimSpace(platformName)) {
	case "android":
		result["platformName"] = "Android"
	case "ios":
		result["platformName"] = "iOS"
	default:
		return nil, fmt.Errorf("%w：platformName 必须是 Android 或 iOS", ErrInvalidArgument)
	}
	return result, nil
}

func validOwnerType(value string) bool {
	switch value {
	case "run_attempt", "manual", "test_run":
		return true
	default:
		return false
	}
}

func validOwnerID(ownerType, value string) bool {
	if ownerType == "manual" {
		if len(value) < 1 || len(value) > 64 {
			return false
		}
		for _, character := range value {
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') || strings.ContainsRune("_.@-", character) {
				continue
			}
			return false
		}
		return true
	}
	return identifierPattern.MatchString(value)
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
	delete(capabilities, repository.TargetDeviceCapability)
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

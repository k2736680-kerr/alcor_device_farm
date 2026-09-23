package iossession

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidArgument        = errors.New("iOS 会话参数无效")
	ErrNotFound               = errors.New("未找到 iOS 会话绑定")
	ErrForbidden              = errors.New("不允许执行当前 iOS 会话操作")
	ErrConflict               = errors.New("iOS 会话绑定发生冲突")
	ErrGrantExpired           = errors.New("iOS 会话授权已过期")
	ErrGrantConsumed          = errors.New("iOS 会话授权已被使用")
	ErrRoutingMismatch        = errors.New("iOS 会话路由与预约不匹配")
	ErrProviderBusy           = errors.New("Appium Device Farm 报告预约设备已被占用")
	ErrProviderBusyConverging = errors.New("Appium Device Farm 正在收敛上一会话的忙碌状态")
	ErrCleanupFailed          = errors.New("iOS Appium 会话清理失败")
	ErrHostUnavailable        = errors.New("iOS Session Fence 宿主机不可用")
)

var (
	appiumSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	failureCodePattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)
)

type TokenGenerator func() (string, error)

type GrantInput struct {
	OwnerType  string `json:"owner_type"`
	OwnerID    string `json:"owner_id"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

type GrantView struct {
	ReservationID string    `json:"reservation_id"`
	DeviceID      string    `json:"device_id"`
	HostID        string    `json:"host_id"`
	Platform      string    `json:"platform"`
	UDID          string    `json:"udid"`
	FenceEndpoint string    `json:"fence_endpoint"`
	SessionGrant  string    `json:"session_grant"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type ConsumeInput struct {
	HostID       string          `json:"host_id"`
	SessionGrant string          `json:"session_grant"`
	Request      json.RawMessage `json:"request"`
}

type ConsumeView struct {
	ReservationID    string          `json:"reservation_id"`
	DeviceID         string          `json:"device_id"`
	UpstreamEndpoint string          `json:"upstream_endpoint"`
	Request          json.RawMessage `json:"request"`
}

type BindingInput struct {
	HostID          string `json:"host_id"`
	SessionGrant    string `json:"session_grant"`
	AppiumSessionID string `json:"appium_session_id"`
}

type AuthorizationInput struct {
	HostID          string `json:"host_id"`
	SessionGrant    string `json:"session_grant"`
	AppiumSessionID string `json:"appium_session_id"`
}

type AuthorizationView struct {
	UpstreamEndpoint string `json:"upstream_endpoint"`
}

type FailureInput struct {
	HostID       string `json:"host_id"`
	SessionGrant string `json:"session_grant"`
	ErrorCode    string `json:"error_code"`
}

type Service struct {
	db         *database.DB
	newID      func() (string, error)
	newGrant   TokenGenerator
	agentToken string
	httpClient *http.Client
}

func New(db *database.DB, agentToken string, generators ...TokenGenerator) *Service {
	newGrant := randomGrant
	if len(generators) > 0 && generators[0] != nil {
		newGrant = generators[0]
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("iOS Session Fence 不允许重定向")
	}}
	return &Service{db: db, newID: identifier.New, newGrant: newGrant, agentToken: strings.TrimSpace(agentToken), httpClient: client}
}

func randomGrant() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (service *Service) Issue(ctx context.Context, actor audit.Actor, reservationID, requestID string, input GrantInput) (GrantView, error) {
	if service == nil || service.db == nil || !actor.Valid() || actor.Type != audit.ActorService ||
		!validIdentifier(reservationID) || strings.TrimSpace(requestID) == "" {
		return GrantView{}, ErrInvalidArgument
	}
	return service.issue(ctx, actor, reservationID, requestID, input)
}

func (service *Service) issue(ctx context.Context, actor audit.Actor, reservationID, requestID string, input GrantInput) (GrantView, error) {
	input.OwnerType, input.OwnerID = strings.TrimSpace(input.OwnerType), strings.TrimSpace(input.OwnerID)
	if input.TTLSeconds == 0 {
		input.TTLSeconds = 60
	}
	if input.TTLSeconds < 15 || input.TTLSeconds > 120 || input.OwnerType == "" || input.OwnerID == "" {
		return GrantView{}, ErrInvalidArgument
	}
	grant, err := service.newGrant()
	if err != nil {
		return GrantView{}, fmt.Errorf("生成 iOS 会话授权失败：%w", err)
	}
	if !validGrant(grant) {
		return GrantView{}, errors.New("生成 iOS 会话授权失败：生成器返回了无效的 256 位值")
	}
	grantHash := hashGrant(grant)
	var result GrantView
	var driftDeviceID string
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var clientID, ownerType, ownerID, reservationStatus string
		var reservationExpiry *time.Time
		var sessionID, sessionStatus, deviceID, hostID, platform, lifecycle, health string
		var snapshotHostID, appiumEndpoint, appiumUDID, fenceEndpoint, hostStatus string
		var providerBusy, recentCleanup bool
		var appiumSessionID *string
		if err := tx.QueryRow(ctx, `SELECT r.client_id,r.owner_type,r.owner_id,r.status,r.expires_at,
			s.id,s.status,s.appium_session_id,d.id,d.host_id,d.platform,d.lifecycle_status,d.health_status,
			COALESCE((d.capabilities->>'providerBusy')::boolean,false),h.status,
			EXISTS(SELECT 1 FROM device_sessions recent WHERE recent.device_id=d.id
				AND recent.appium_session_ended_at >= clock_timestamp()-interval '30 seconds'),
			COALESCE(h.capabilities->>'session_fence_endpoint',''),
			COALESCE(s.connection_metadata->>'host_id',''),COALESCE(s.connection_metadata->>'appium_endpoint',''),
			COALESCE(s.connection_metadata->>'appium_udid','')
			FROM device_reservations r JOIN device_sessions s ON s.reservation_id=r.id
			JOIN devices d ON d.id=s.device_id JOIN device_hosts h ON h.id=d.host_id
			WHERE r.id=$1 FOR UPDATE OF r,s,d,h`, reservationID).Scan(
			&clientID, &ownerType, &ownerID, &reservationStatus, &reservationExpiry,
			&sessionID, &sessionStatus, &appiumSessionID, &deviceID, &hostID, &platform, &lifecycle, &health,
			&providerBusy, &hostStatus, &recentCleanup, &fenceEndpoint, &snapshotHostID, &appiumEndpoint, &appiumUDID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		if clientID != actor.ClientID || ownerType != input.OwnerType || ownerID != input.OwnerID {
			return ErrForbidden
		}
		if reservationStatus != string(domain.ReservationActive) || reservationExpiry == nil || !reservationExpiry.After(now) ||
			sessionStatus != string(domain.SessionActive) || platform != "ios" || lifecycle != string(domain.DeviceBusy) ||
			health != string(domain.HealthHealthy) || hostStatus != string(domain.HostOnline) || snapshotHostID != hostID ||
			strings.TrimSpace(appiumEndpoint) == "" || strings.TrimSpace(appiumUDID) == "" {
			return ErrConflict
		}
		if providerBusy {
			if recentCleanup {
				return ErrProviderBusyConverging
			}
			driftDeviceID = deviceID
			return ErrProviderBusy
		}
		if appiumSessionID != nil {
			return fmt.Errorf("%w: Device Session already has an Appium Session", ErrConflict)
		}
		fenceEndpoint, err = validateFenceEndpoint(fenceEndpoint)
		if err != nil {
			return err
		}
		expiresAt := now.Add(time.Duration(input.TTLSeconds) * time.Second)
		if expiresAt.After(*reservationExpiry) {
			expiresAt = *reservationExpiry
		}
		if _, err := tx.Exec(ctx, `UPDATE device_sessions SET session_grant_hash=$2,
			session_grant_expires_at=$3,session_grant_consumed_at=NULL,updated_at=$4 WHERE id=$1`,
			sessionID, grantHash, expiresAt, now); err != nil {
			return err
		}
		if err := service.insertAudit(ctx, tx, actor, "issue_ios_session_grant", reservationID, requestID,
			map[string]any{"device_id": deviceID, "host_id": hostID, "expires_at": expiresAt}); err != nil {
			return err
		}
		result = GrantView{ReservationID: reservationID, DeviceID: deviceID, HostID: hostID, Platform: "ios",
			UDID: appiumUDID, FenceEndpoint: fenceEndpoint, SessionGrant: grant, ExpiresAt: expiresAt}
		return nil
	})
	if errors.Is(err, ErrProviderBusy) && driftDeviceID != "" {
		quarantineErr := service.quarantine(ctx, driftDeviceID, reservationID, "ios_provider_busy_on_grant",
			"IOS_PROVIDER_BUSY_WITHOUT_BOUND_SESSION",
			map[string]any{"drift_code": "IOS_PROVIDER_BUSY_WITHOUT_BOUND_SESSION"})
		if quarantineErr != nil {
			return GrantView{}, errors.Join(err, quarantineErr)
		}
	}
	return result, err
}

func (service *Service) Consume(ctx context.Context, input ConsumeInput, requestID string) (ConsumeView, error) {
	if service == nil || service.db == nil || !validIdentifier(input.HostID) || !validGrant(input.SessionGrant) || strings.TrimSpace(requestID) == "" {
		return ConsumeView{}, ErrInvalidArgument
	}
	hash := hashGrant(input.SessionGrant)
	var result ConsumeView
	var driftDeviceID, driftReservationID string
	err := service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		binding, err := service.lockBindingByGrant(ctx, tx, hash)
		if err != nil {
			return err
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		if binding.HostID != input.HostID || binding.SnapshotHostID != input.HostID {
			return ErrRoutingMismatch
		}
		if binding.GrantConsumedAt != nil {
			return ErrGrantConsumed
		}
		if binding.GrantExpiresAt == nil || !binding.GrantExpiresAt.After(now) {
			return ErrGrantExpired
		}
		if err := binding.available(now); err != nil {
			return err
		}
		if binding.ProviderBusy {
			if binding.RecentCleanup {
				return ErrProviderBusyConverging
			}
			driftDeviceID, driftReservationID = binding.DeviceID, binding.ReservationID
			return ErrProviderBusy
		}
		pinned, err := validateAndPinSessionRequest(input.Request, binding.AppiumUDID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE device_sessions SET session_grant_consumed_at=$2,updated_at=$2
			WHERE id=$1 AND session_grant_consumed_at IS NULL`, binding.SessionID, now); err != nil {
			return err
		}
		actor := audit.Actor{Type: audit.ActorAgent, ID: input.HostID, ClientID: audit.ActorAgent}
		if err := service.insertAudit(ctx, tx, actor, "consume_ios_session_grant", binding.ReservationID, requestID,
			map[string]any{"device_id": binding.DeviceID, "host_id": binding.HostID}); err != nil {
			return err
		}
		result = ConsumeView{ReservationID: binding.ReservationID, DeviceID: binding.DeviceID,
			UpstreamEndpoint: binding.AppiumEndpoint, Request: pinned}
		return nil
	})
	if errors.Is(err, ErrProviderBusy) && driftDeviceID != "" {
		quarantineErr := service.quarantine(ctx, driftDeviceID, driftReservationID, "ios_provider_busy_on_consume",
			"IOS_PROVIDER_BUSY_WITHOUT_BOUND_SESSION",
			map[string]any{"drift_code": "IOS_PROVIDER_BUSY_WITHOUT_BOUND_SESSION"})
		if quarantineErr != nil {
			return ConsumeView{}, errors.Join(err, quarantineErr)
		}
	}
	return result, err
}

func (service *Service) Bind(ctx context.Context, input BindingInput, requestID string) error {
	if service == nil || service.db == nil || !validIdentifier(input.HostID) || !validGrant(input.SessionGrant) ||
		!appiumSessionIDPattern.MatchString(strings.TrimSpace(input.AppiumSessionID)) || strings.TrimSpace(requestID) == "" {
		return ErrInvalidArgument
	}
	return service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		binding, err := service.lockBindingByGrant(ctx, tx, hashGrant(input.SessionGrant))
		if err != nil {
			return err
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		if binding.HostID != input.HostID || binding.SnapshotHostID != input.HostID || binding.GrantConsumedAt == nil {
			return ErrRoutingMismatch
		}
		if err := binding.available(now); err != nil {
			return err
		}
		if binding.AppiumSessionID != nil {
			if *binding.AppiumSessionID == input.AppiumSessionID && binding.AppiumSessionEndedAt == nil {
				return nil
			}
			return ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE device_sessions SET appium_session_id=$2,
			appium_session_started_at=$3,appium_session_ended_at=NULL,updated_at=$3 WHERE id=$1 AND appium_session_id IS NULL`,
			binding.SessionID, input.AppiumSessionID, now); err != nil {
			return err
		}
		actor := audit.Actor{Type: audit.ActorAgent, ID: input.HostID, ClientID: audit.ActorAgent}
		return service.insertAudit(ctx, tx, actor, "bind_ios_appium_session", binding.ReservationID, requestID,
			map[string]any{"device_id": binding.DeviceID, "host_id": binding.HostID, "appium_session_bound": true})
	})
}

func (service *Service) Authorize(ctx context.Context, input AuthorizationInput) (AuthorizationView, error) {
	if service == nil || service.db == nil || !validIdentifier(input.HostID) || !validGrant(input.SessionGrant) ||
		!appiumSessionIDPattern.MatchString(strings.TrimSpace(input.AppiumSessionID)) {
		return AuthorizationView{}, ErrInvalidArgument
	}
	binding, err := service.getBindingByGrant(ctx, hashGrant(input.SessionGrant))
	if err != nil {
		return AuthorizationView{}, err
	}
	if binding.HostID != input.HostID || binding.SnapshotHostID != input.HostID || binding.GrantConsumedAt == nil ||
		binding.AppiumSessionID == nil || *binding.AppiumSessionID != input.AppiumSessionID || binding.AppiumSessionEndedAt != nil {
		return AuthorizationView{}, ErrRoutingMismatch
	}
	now, err := database.ClockNow(ctx, service.db.Pool())
	if err != nil {
		return AuthorizationView{}, err
	}
	if err := binding.available(now); err != nil {
		return AuthorizationView{}, err
	}
	return AuthorizationView{UpstreamEndpoint: binding.AppiumEndpoint}, nil
}

func (service *Service) Close(ctx context.Context, input BindingInput, requestID string) error {
	if service == nil || service.db == nil || !validIdentifier(input.HostID) || !validGrant(input.SessionGrant) ||
		!appiumSessionIDPattern.MatchString(strings.TrimSpace(input.AppiumSessionID)) || strings.TrimSpace(requestID) == "" {
		return ErrInvalidArgument
	}
	return service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		binding, err := service.lockBindingByGrant(ctx, tx, hashGrant(input.SessionGrant))
		if err != nil {
			return err
		}
		if binding.HostID != input.HostID || binding.AppiumSessionID == nil || *binding.AppiumSessionID != input.AppiumSessionID {
			return ErrRoutingMismatch
		}
		if binding.AppiumSessionEndedAt != nil {
			return nil
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE device_sessions SET appium_session_ended_at=$2,updated_at=$2
			WHERE id=$1 AND appium_session_ended_at IS NULL`, binding.SessionID, now); err != nil {
			return err
		}
		actor := audit.Actor{Type: audit.ActorAgent, ID: input.HostID, ClientID: audit.ActorAgent}
		return service.insertAudit(ctx, tx, actor, "close_ios_appium_session", binding.ReservationID, requestID,
			map[string]any{"device_id": binding.DeviceID, "host_id": binding.HostID, "appium_session_closed": true})
	})
}

func (service *Service) RecordFailure(ctx context.Context, input FailureInput, requestID string) error {
	input.ErrorCode = strings.TrimSpace(input.ErrorCode)
	if service == nil || service.db == nil || !validIdentifier(input.HostID) || !validGrant(input.SessionGrant) ||
		!failureCodePattern.MatchString(input.ErrorCode) || strings.TrimSpace(requestID) == "" {
		return ErrInvalidArgument
	}
	return service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		binding, err := service.lockBindingByGrant(ctx, tx, hashGrant(input.SessionGrant))
		if err != nil {
			return err
		}
		if binding.HostID != input.HostID {
			return ErrRoutingMismatch
		}
		now, err := database.ClockNow(ctx, tx)
		if err != nil {
			return err
		}
		eventID, err := service.newID()
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"error_code": input.ErrorCode, "host_id": binding.HostID})
		if _, err := tx.Exec(ctx, `INSERT INTO device_health_events
			(id,device_id,source,event_type,severity,reason,payload,observed_at)
			VALUES($1,$2,'session_fence','ios_session_create_failed','warning',$3,$4::jsonb,$5)`,
			eventID, binding.DeviceID, "iOS Session Fence 创建上游 Appium Session 失败", payload, now); err != nil {
			return err
		}
		actor := audit.Actor{Type: audit.ActorAgent, ID: input.HostID, ClientID: audit.ActorAgent}
		return service.insertAudit(ctx, tx, actor, "fail_ios_appium_session", binding.ReservationID, requestID,
			map[string]any{"device_id": binding.DeviceID, "host_id": binding.HostID, "error_code": input.ErrorCode})
	})
}

type bindingRecord struct {
	SessionID              string
	ReservationID          string
	ReservationStatus      string
	ReservationExpiresAt   *time.Time
	DeviceID               string
	HostID                 string
	Platform               string
	DeviceLifecycle        string
	DeviceHealth           string
	ProviderBusy           bool
	RecentCleanup          bool
	HostStatus             string
	SnapshotHostID         string
	AppiumEndpoint         string
	AppiumUDID             string
	GrantExpiresAt         *time.Time
	GrantConsumedAt        *time.Time
	AppiumSessionID        *string
	AppiumSessionStartedAt *time.Time
	AppiumSessionEndedAt   *time.Time
}

func (service *Service) lockBindingByGrant(ctx context.Context, tx pgx.Tx, hash string) (bindingRecord, error) {
	return scanBinding(tx.QueryRow(ctx, bindingQuery+" FOR UPDATE OF s,r,d,h", hash))
}

func (service *Service) getBindingByGrant(ctx context.Context, hash string) (bindingRecord, error) {
	return scanBinding(service.db.Pool().QueryRow(ctx, bindingQuery, hash))
}

const bindingQuery = `SELECT s.id,r.id,r.status,r.expires_at,d.id,d.host_id,d.platform,d.lifecycle_status,d.health_status,
	COALESCE((d.capabilities->>'providerBusy')::boolean,false),h.status,
	EXISTS(SELECT 1 FROM device_sessions recent WHERE recent.device_id=d.id
		AND recent.appium_session_ended_at >= clock_timestamp()-interval '30 seconds'),
	COALESCE(s.connection_metadata->>'host_id',''),COALESCE(s.connection_metadata->>'appium_endpoint',''),
	COALESCE(s.connection_metadata->>'appium_udid',''),s.session_grant_expires_at,s.session_grant_consumed_at,
	s.appium_session_id,s.appium_session_started_at,s.appium_session_ended_at
	FROM device_sessions s JOIN device_reservations r ON r.id=s.reservation_id
	JOIN devices d ON d.id=s.device_id JOIN device_hosts h ON h.id=d.host_id WHERE s.session_grant_hash=$1`

func scanBinding(row pgx.Row) (bindingRecord, error) {
	var value bindingRecord
	err := row.Scan(&value.SessionID, &value.ReservationID, &value.ReservationStatus, &value.ReservationExpiresAt,
		&value.DeviceID, &value.HostID, &value.Platform, &value.DeviceLifecycle, &value.DeviceHealth,
		&value.ProviderBusy, &value.HostStatus, &value.RecentCleanup, &value.SnapshotHostID, &value.AppiumEndpoint, &value.AppiumUDID,
		&value.GrantExpiresAt, &value.GrantConsumedAt, &value.AppiumSessionID, &value.AppiumSessionStartedAt, &value.AppiumSessionEndedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return bindingRecord{}, ErrNotFound
	}
	return value, err
}

func (binding bindingRecord) available(now time.Time) error {
	if binding.Platform != "ios" || binding.ReservationStatus != string(domain.ReservationActive) ||
		binding.ReservationExpiresAt == nil || !binding.ReservationExpiresAt.After(now) ||
		binding.DeviceLifecycle != string(domain.DeviceBusy) || binding.DeviceHealth != string(domain.HealthHealthy) ||
		binding.HostStatus != string(domain.HostOnline) || strings.TrimSpace(binding.AppiumEndpoint) == "" || strings.TrimSpace(binding.AppiumUDID) == "" {
		return ErrConflict
	}
	return nil
}

func hashGrant(grant string) string {
	value := sha256.Sum256([]byte(grant))
	return hex.EncodeToString(value[:])
}

func validGrant(grant string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(grant))
	return err == nil && len(decoded) == 32
}

func validIdentifier(value string) bool {
	if len(value) < 16 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func validateFenceEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", ErrHostUnavailable
	}
	hostname := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(hostname)
	loopback := hostname == "localhost" || (ip != nil && ip.IsLoopback())
	if parsed.Scheme != "https" && !loopback {
		return "", fmt.Errorf("%w: non-loopback Fence Endpoint requires HTTPS", ErrHostUnavailable)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (service *Service) insertAudit(ctx context.Context, tx pgx.Tx, actor audit.Actor, action, reservationID, requestID string, summary map[string]any) error {
	return service.insertAuditResource(ctx, tx, actor, action, "device_reservation", reservationID, requestID, summary)
}

func (service *Service) insertAuditResource(ctx context.Context, tx pgx.Tx, actor audit.Actor, action, resourceType, resourceID, requestID string, summary map[string]any) error {
	id, err := service.newID()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO device_audit_events
		(id,actor_type,actor_id,action,resource_type,resource_id,request_id,summary)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`,
		id, actor.Type, actor.ID, action, resourceType, resourceID, requestID, encoded)
	return err
}

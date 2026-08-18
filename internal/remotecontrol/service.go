package remotecontrol

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
)

var (
	ErrUnavailable = errors.New("远程控制服务不可用")
	ErrNotFound    = errors.New("未找到远程控制连接")
	ErrConflict    = errors.New("远程控制操作发生冲突")
)

type Reservations interface {
	CreateForDevice(context.Context, audit.Actor, string, string, int) (reservation.View, error)
	FindOpenForDevice(context.Context, string, string) (reservation.View, error)
	KeepAliveForDevice(context.Context, string, string, string, time.Duration) (reservation.View, error)
	Release(context.Context, audit.Actor, string, string, string, reservation.ReleaseInput) (reservation.View, error)
}

type Devices interface {
	GetDevice(context.Context, string) (management.Device, error)
}

type Config struct {
	WebURL        string
	WebAuthSecret string
	WebUserName   string
	WebUserEmail  string
	WebTokenTTL   time.Duration
	Lease         time.Duration
	Heartbeat     time.Duration
	Now           func() time.Time
}

type View struct {
	DeviceID                string     `json:"device_id"`
	ReservationID           string     `json:"reservation_id,omitempty"`
	Status                  string     `json:"status"`
	URL                     string     `json:"url,omitempty"`
	ExpiresAt               *time.Time `json:"expires_at,omitempty"`
	HeartbeatIntervalSecond int        `json:"heartbeat_interval_seconds"`
}

type Service struct {
	reservations Reservations
	devices      Devices
	config       Config
	webURL       *url.URL
}

func New(reservations Reservations, devices Devices, cfg Config) (*Service, error) {
	if reservations == nil || devices == nil || len(cfg.WebAuthSecret) < 32 ||
		strings.TrimSpace(cfg.WebUserName) == "" || strings.TrimSpace(cfg.WebUserEmail) == "" || cfg.Lease < time.Minute ||
		cfg.Heartbeat <= 0 || cfg.Heartbeat >= cfg.Lease || cfg.WebTokenTTL <= 0 || cfg.WebTokenTTL > time.Minute {
		return nil, ErrUnavailable
	}
	parsed, err := url.Parse(strings.TrimSpace(cfg.WebURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: invalid STF web URL", ErrUnavailable)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/"
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{reservations: reservations, devices: devices, config: cfg, webURL: parsed}, nil
}

func ConfigFrom(app config.Config) Config {
	return Config{
		WebURL: app.STF.WebURL, WebAuthSecret: app.STF.WebAuthSecret,
		WebUserName: app.STF.WebUserName, WebUserEmail: app.STF.WebUserEmail,
		WebTokenTTL: app.STF.WebTokenTTL, Lease: app.Console.RemoteLease,
		Heartbeat: app.Console.RemoteHeartbeat,
	}
}

func (service *Service) Start(ctx context.Context, actor audit.Actor, key, deviceID string) (View, error) {
	if service == nil {
		return View{}, ErrUnavailable
	}
	created, err := service.reservations.CreateForDevice(ctx, actor, key, deviceID, int(service.config.Lease/time.Second))
	if err != nil {
		return View{}, translateReservationError(err)
	}
	return service.view(ctx, deviceID, created)
}

func (service *Service) Get(ctx context.Context, ownerID, deviceID string) (View, error) {
	if service == nil {
		return View{}, ErrUnavailable
	}
	current, err := service.reservations.FindOpenForDevice(ctx, ownerID, deviceID)
	if err != nil {
		return View{}, translateReservationError(err)
	}
	return service.view(ctx, deviceID, current)
}

func (service *Service) Heartbeat(ctx context.Context, actor audit.Actor, key, requestID, deviceID string) (View, error) {
	if service == nil {
		return View{}, ErrUnavailable
	}
	current, err := service.reservations.FindOpenForDevice(ctx, actor.ID, deviceID)
	if err != nil {
		return View{}, translateReservationError(err)
	}
	if current.Status != domain.ReservationActive {
		return service.view(ctx, deviceID, current)
	}
	if current.DeviceID == nil || *current.DeviceID != deviceID {
		return View{}, ErrConflict
	}
	kept, err := service.reservations.KeepAliveForDevice(ctx, current.ID, actor.ID, deviceID, service.config.Lease)
	if err != nil {
		return View{}, translateReservationError(err)
	}
	return service.viewWithoutURL(deviceID, kept), nil
}

func (service *Service) End(ctx context.Context, actor audit.Actor, key, requestID, deviceID string) (View, error) {
	if service == nil {
		return View{}, ErrUnavailable
	}
	current, err := service.reservations.FindOpenForDevice(ctx, actor.ID, deviceID)
	if errors.Is(err, reservation.ErrNotFound) {
		return service.endedView(deviceID, ""), nil
	}
	if err != nil {
		return View{}, translateReservationError(err)
	}
	return service.endReservation(ctx, actor, key, requestID, deviceID, current, "管理员结束远控")
}

func (service *Service) endReservation(
	ctx context.Context,
	actor audit.Actor,
	key, requestID, deviceID string,
	current reservation.View,
	reason string,
) (View, error) {
	closed, err := service.reservations.Release(ctx, actor, key, current.ID, requestID, reservation.ReleaseInput{Reason: reason})
	if err != nil {
		return View{}, translateReservationError(err)
	}
	return service.endedView(deviceID, closed.ID), nil
}

func (service *Service) view(ctx context.Context, deviceID string, current reservation.View) (View, error) {
	view := service.viewWithoutURL(deviceID, current)
	if current.Status != domain.ReservationActive {
		return view, nil
	}
	if current.DeviceID == nil || *current.DeviceID != deviceID {
		return View{}, ErrConflict
	}
	device, err := service.devices.GetDevice(ctx, deviceID)
	if err != nil {
		return View{}, fmt.Errorf("%w: load remote device: %v", ErrUnavailable, err)
	}
	token, err := service.signToken()
	if err != nil {
		return View{}, err
	}
	entry := *service.webURL
	query := entry.Query()
	query.Set("jwt", token)
	entry.RawQuery = query.Encode()
	entry.Fragment = "!/control/" + url.PathEscape(device.Serial)
	view.URL = entry.String()
	return view, nil
}

func (service *Service) viewWithoutURL(deviceID string, current reservation.View) View {
	status := "connecting"
	switch current.Status {
	case domain.ReservationActive:
		status = "connected"
	case domain.ReservationFailed:
		status = "failed"
	case domain.ReservationReleased, domain.ReservationExpired, domain.ReservationForceReleased:
		status = "ended"
	}
	return View{
		DeviceID: deviceID, ReservationID: current.ID, Status: status, ExpiresAt: current.ExpiresAt,
		HeartbeatIntervalSecond: int(service.config.Heartbeat / time.Second),
	}
}

func (service *Service) endedView(deviceID, reservationID string) View {
	return View{DeviceID: deviceID, ReservationID: reservationID, Status: "ended",
		HeartbeatIntervalSecond: int(service.config.Heartbeat / time.Second)}
}

func (service *Service) signToken() (string, error) {
	header, err := json.Marshal(map[string]any{"alg": "HS256", "exp": service.config.Now().Add(service.config.WebTokenTTL).UnixMilli()})
	if err != nil {
		return "", fmt.Errorf("sign STF web token: %w", err)
	}
	payload, err := json.Marshal(map[string]string{"name": service.config.WebUserName, "email": service.config.WebUserEmail})
	if err != nil {
		return "", fmt.Errorf("sign STF web token: %w", err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	unsigned := encode(header) + "." + encode(payload)
	mac := hmac.New(sha256.New, []byte(service.config.WebAuthSecret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + encode(mac.Sum(nil)), nil
}

func translateReservationError(err error) error {
	switch {
	case errors.Is(err, reservation.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, reservation.ErrConflict), errors.Is(err, reservation.ErrCapacityUnavailable), errors.Is(err, reservation.ErrPoolUnavailable):
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case errors.Is(err, reservation.ErrSTFReleaseFailed):
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	default:
		return err
	}
}

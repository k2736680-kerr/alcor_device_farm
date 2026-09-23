package remotecontrol

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/baguette"
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

type IOSRemote interface {
	EnsureBooted(context.Context, string) error
	EntryURL(string, string, string, string) (string, error)
}

type Config struct {
	STFWebURL        string
	STFWebAuthSecret string
	STFWebUserName   string
	STFWebUserEmail  string
	STFWebTokenTTL   time.Duration
	IOSEnabled       bool
	Lease            time.Duration
	Heartbeat        time.Duration
	Now              func() time.Time
	Logger           *slog.Logger
}

type View struct {
	DeviceID                string     `json:"device_id"`
	ReservationID           string     `json:"reservation_id,omitempty"`
	Status                  string     `json:"status"`
	Transport               string     `json:"transport"`
	URL                     string     `json:"url,omitempty"`
	ExpiresAt               *time.Time `json:"expires_at,omitempty"`
	HeartbeatIntervalSecond int        `json:"heartbeat_interval_seconds"`
}

type Service struct {
	reservations Reservations
	devices      Devices
	iosRemote    IOSRemote
	config       Config
	stfWebURL    *url.URL
	logger       *slog.Logger
}

const (
	TransportSTF      = "stf"
	TransportBaguette = "baguette"
)

func New(reservations Reservations, devices Devices, cfg Config, iosRemotes ...IOSRemote) (*Service, error) {
	if reservations == nil || devices == nil || cfg.Lease < time.Minute ||
		cfg.Heartbeat <= 0 || cfg.Heartbeat >= cfg.Lease {
		return nil, ErrUnavailable
	}
	var stfWebURL *url.URL
	if strings.TrimSpace(cfg.STFWebURL) != "" {
		if len(cfg.STFWebAuthSecret) < 32 || strings.TrimSpace(cfg.STFWebUserName) == "" ||
			strings.TrimSpace(cfg.STFWebUserEmail) == "" || cfg.STFWebTokenTTL <= 0 || cfg.STFWebTokenTTL > time.Minute {
			return nil, fmt.Errorf("%w: STF 远控配置无效", ErrUnavailable)
		}
		parsed, err := parseHTTPURL(cfg.STFWebURL)
		if err != nil {
			return nil, fmt.Errorf("%w: STF Web 地址无效", ErrUnavailable)
		}
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/"
		stfWebURL = parsed
	}
	var iosRemote IOSRemote
	if len(iosRemotes) > 0 {
		iosRemote = iosRemotes[0]
	}
	if cfg.IOSEnabled && iosRemote == nil {
		return nil, fmt.Errorf("%w: iOS 远控配置无效", ErrUnavailable)
	}
	if stfWebURL == nil && !cfg.IOSEnabled {
		return nil, ErrUnavailable
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{reservations: reservations, devices: devices, iosRemote: iosRemote,
		config: cfg, stfWebURL: stfWebURL, logger: cfg.Logger}, nil
}

func ConfigFrom(app config.Config) Config {
	return Config{
		STFWebURL: app.STF.WebURL, STFWebAuthSecret: app.STF.WebAuthSecret,
		STFWebUserName: app.STF.WebUserName, STFWebUserEmail: app.STF.WebUserEmail,
		STFWebTokenTTL: app.STF.WebTokenTTL, IOSEnabled: app.IOSRemote.Enabled,
		Lease: app.Console.RemoteLease, Heartbeat: app.Console.RemoteHeartbeat,
	}
}

func (service *Service) Start(ctx context.Context, actor audit.Actor, key, deviceID string) (View, error) {
	if service == nil {
		return View{}, ErrUnavailable
	}
	device, transport, err := service.remoteDevice(ctx, deviceID)
	if err != nil {
		return View{}, err
	}
	created, err := service.reservations.CreateForDevice(ctx, actor, key, deviceID, int(service.config.Lease/time.Second))
	if err != nil {
		return View{}, translateReservationError(err)
	}
	return service.view(ctx, device, transport, created)
}

func (service *Service) Get(ctx context.Context, ownerID, deviceID string) (View, error) {
	if service == nil {
		return View{}, ErrUnavailable
	}
	current, err := service.reservations.FindOpenForDevice(ctx, ownerID, deviceID)
	if err != nil {
		return View{}, translateReservationError(err)
	}
	device, transport, err := service.remoteDevice(ctx, deviceID)
	if err != nil {
		return View{}, err
	}
	return service.view(ctx, device, transport, current)
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
		transport, _ := service.transport(ctx, deviceID, false)
		return service.viewWithoutURL(deviceID, transport, current), nil
	}
	if current.DeviceID == nil || *current.DeviceID != deviceID {
		return View{}, ErrConflict
	}
	kept, err := service.reservations.KeepAliveForDevice(ctx, current.ID, actor.ID, deviceID, service.config.Lease)
	if err != nil {
		return View{}, translateReservationError(err)
	}
	transport, err := service.transport(ctx, deviceID, true)
	if err != nil {
		return View{}, err
	}
	return service.viewWithoutURL(deviceID, transport, kept), nil
}

func (service *Service) End(ctx context.Context, actor audit.Actor, key, requestID, deviceID string) (View, error) {
	if service == nil {
		return View{}, ErrUnavailable
	}
	current, err := service.reservations.FindOpenForDevice(ctx, actor.ID, deviceID)
	if errors.Is(err, reservation.ErrNotFound) {
		transport, _ := service.transport(ctx, deviceID, false)
		return service.endedView(deviceID, "", transport), nil
	}
	if err != nil {
		return View{}, translateReservationError(err)
	}
	transport, _ := service.transport(ctx, deviceID, false)
	return service.endReservation(ctx, actor, key, requestID, deviceID, transport, current, "管理员结束远控")
}

func (service *Service) endReservation(
	ctx context.Context,
	actor audit.Actor,
	key, requestID, deviceID string,
	transport string,
	current reservation.View,
	reason string,
) (View, error) {
	closed, err := service.reservations.Release(ctx, actor, key, current.ID, requestID, reservation.ReleaseInput{Reason: reason})
	if err != nil {
		return View{}, translateReservationError(err)
	}
	return service.endedView(deviceID, closed.ID, transport), nil
}

func (service *Service) view(ctx context.Context, device management.Device, transport string, current reservation.View) (View, error) {
	view := service.viewWithoutURL(device.ID, transport, current)
	if current.Status != domain.ReservationActive {
		return view, nil
	}
	if current.DeviceID == nil || *current.DeviceID != device.ID {
		return View{}, ErrConflict
	}
	switch transport {
	case TransportSTF:
		token, err := service.signSTFToken()
		if err != nil {
			return View{}, err
		}
		entry := *service.stfWebURL
		query := entry.Query()
		query.Set("jwt", token)
		entry.RawQuery = query.Encode()
		entry.Fragment = "!/control/" + url.PathEscape(stfSerial(device))
		view.URL = entry.String()
	case TransportBaguette:
		entry, err := service.iosRemote.EntryURL(device.ID, device.Serial, current.ID, current.OwnerID)
		if err != nil {
			return View{}, fmt.Errorf("%w: 无法签发 iOS 远控入口", ErrUnavailable)
		}
		view.URL = entry
	default:
		return View{}, ErrUnavailable
	}
	return view, nil
}

func stfSerial(device management.Device) string {
	if device.STFSerial != nil && strings.TrimSpace(*device.STFSerial) != "" {
		return strings.TrimSpace(*device.STFSerial)
	}
	return device.Serial
}

func (service *Service) viewWithoutURL(deviceID, transport string, current reservation.View) View {
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
		DeviceID: deviceID, ReservationID: current.ID, Status: status, Transport: transport, ExpiresAt: current.ExpiresAt,
		HeartbeatIntervalSecond: int(service.config.Heartbeat / time.Second),
	}
}

func (service *Service) endedView(deviceID, reservationID, transport string) View {
	return View{DeviceID: deviceID, ReservationID: reservationID, Status: "ended", Transport: transport,
		HeartbeatIntervalSecond: int(service.config.Heartbeat / time.Second)}
}

func (service *Service) signSTFToken() (string, error) {
	header, err := json.Marshal(map[string]any{"alg": "HS256", "exp": service.config.Now().Add(service.config.STFWebTokenTTL).UnixMilli()})
	if err != nil {
		return "", fmt.Errorf("sign STF web token: %w", err)
	}
	payload, err := json.Marshal(map[string]string{"name": service.config.STFWebUserName, "email": service.config.STFWebUserEmail})
	if err != nil {
		return "", fmt.Errorf("sign STF web token: %w", err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	unsigned := encode(header) + "." + encode(payload)
	mac := hmac.New(sha256.New, []byte(service.config.STFWebAuthSecret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + encode(mac.Sum(nil)), nil
}

func (service *Service) AuthorizeBaguette(ctx context.Context, ticket baguette.Ticket) error {
	if service == nil || service.iosRemote == nil {
		return ErrUnavailable
	}
	current, err := service.reservations.FindOpenForDevice(ctx, ticket.OwnerID, ticket.DeviceID)
	if err != nil || current.ID != ticket.ReservationID || current.Status != domain.ReservationActive ||
		current.DeviceID == nil || *current.DeviceID != ticket.DeviceID || current.ExpiresAt == nil ||
		!current.ExpiresAt.After(service.config.Now()) {
		return ErrNotFound
	}
	device, err := service.devices.GetDevice(ctx, ticket.DeviceID)
	if err != nil || device.Serial != ticket.UDID || strings.ToLower(device.Platform) != "ios" {
		return ErrNotFound
	}
	return nil
}

func (service *Service) remoteDevice(ctx context.Context, deviceID string) (management.Device, string, error) {
	device, err := service.devices.GetDevice(ctx, deviceID)
	if err != nil {
		return management.Device{}, "", fmt.Errorf("%w: 无法读取远控设备", ErrUnavailable)
	}
	transport, err := service.transportForDevice(device, true)
	if err == nil && transport == TransportBaguette {
		if bootErr := service.iosRemote.EnsureBooted(ctx, device.Serial); bootErr != nil {
			return management.Device{}, "", fmt.Errorf("%w: %v", ErrUnavailable, bootErr)
		}
	}
	return device, transport, err
}

func (service *Service) transport(ctx context.Context, deviceID string, requireConfigured bool) (string, error) {
	device, err := service.devices.GetDevice(ctx, deviceID)
	if err != nil {
		return "", fmt.Errorf("%w: 无法读取远控设备", ErrUnavailable)
	}
	return service.transportForDevice(device, requireConfigured)
}

func (service *Service) transportForDevice(device management.Device, requireConfigured bool) (string, error) {
	switch strings.ToLower(strings.TrimSpace(device.Platform)) {
	case "android":
		if requireConfigured && service.stfWebURL == nil {
			return "", fmt.Errorf("%w: Android STF 远控尚未配置", ErrUnavailable)
		}
		return TransportSTF, nil
	case "ios":
		if device.DeviceKind != "simulator" || device.ProviderType != "appium_device_farm_ios" {
			return "", fmt.Errorf("%w: 当前只支持 iOS Simulator 远程控制", ErrUnavailable)
		}
		if requireConfigured && (!service.config.IOSEnabled || service.iosRemote == nil) {
			return "", fmt.Errorf("%w: iOS 远程控制尚未配置", ErrUnavailable)
		}
		return TransportBaguette, nil
	default:
		return "", fmt.Errorf("%w: 当前设备平台不支持远程控制", ErrUnavailable)
	}
}

func parseHTTPURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrUnavailable
	}
	return parsed, nil
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

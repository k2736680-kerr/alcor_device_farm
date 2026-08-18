package remotecontrol

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/iossession"
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

type IOSSessions interface {
	IssueManual(context.Context, audit.Actor, string, string, iossession.GrantInput) (iossession.GrantView, error)
	RemoteBinding(context.Context, string, string, string) (iossession.RemoteBindingView, error)
}

type Config struct {
	STFWebURL          string
	STFWebAuthSecret   string
	STFWebUserName     string
	STFWebUserEmail    string
	STFWebTokenTTL     time.Duration
	IOSEnabled         bool
	IOSGatewaySecret   string
	IOSGatewayTokenTTL time.Duration
	AgentToken         string
	IOSCreateTimeout   time.Duration
	Lease              time.Duration
	Heartbeat          time.Duration
	Now                func() time.Time
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
	iosSessions  IOSSessions
	config       Config
	stfWebURL    *url.URL
	httpClient   *http.Client
	streamClient *http.Client
	iosStarts    sync.Map
}

const (
	TransportSTF    = "stf"
	TransportAppium = "appium"
)

type gatewayClaims struct {
	DeviceID      string `json:"device_id"`
	ReservationID string `json:"reservation_id"`
	OwnerID       string `json:"owner_id"`
	ExpiresAt     int64  `json:"expires_at"`
}

func New(reservations Reservations, devices Devices, cfg Config, iosSessionServices ...IOSSessions) (*Service, error) {
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
	var iosSessions IOSSessions
	if len(iosSessionServices) > 0 {
		iosSessions = iosSessionServices[0]
	}
	if cfg.IOSEnabled {
		if iosSessions == nil || len(cfg.IOSGatewaySecret) < 32 || len(strings.TrimSpace(cfg.AgentToken)) < 16 ||
			cfg.IOSGatewayTokenTTL <= 0 || cfg.IOSGatewayTokenTTL > time.Minute {
			return nil, fmt.Errorf("%w: iOS 远控配置无效", ErrUnavailable)
		}
		if cfg.IOSCreateTimeout <= 0 {
			cfg.IOSCreateTimeout = 2 * time.Minute
		}
	}
	if stfWebURL == nil && !cfg.IOSEnabled {
		return nil, ErrUnavailable
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	client := &http.Client{Timeout: cfg.IOSCreateTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("iOS 远控内部请求不允许重定向")
	}}
	streamClient := &http.Client{CheckRedirect: client.CheckRedirect}
	return &Service{reservations: reservations, devices: devices, iosSessions: iosSessions,
		config: cfg, stfWebURL: stfWebURL, httpClient: client, streamClient: streamClient}, nil
}

func ConfigFrom(app config.Config) Config {
	return Config{
		STFWebURL: app.STF.WebURL, STFWebAuthSecret: app.STF.WebAuthSecret,
		STFWebUserName: app.STF.WebUserName, STFWebUserEmail: app.STF.WebUserEmail,
		STFWebTokenTTL: app.STF.WebTokenTTL, IOSEnabled: app.IOSRemote.Enabled,
		IOSGatewaySecret: app.IOSRemote.GatewaySecret, IOSGatewayTokenTTL: app.IOSRemote.GatewayTokenTTL,
		AgentToken: app.Security.AgentToken, IOSCreateTimeout: 2 * time.Minute,
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
	if transport == TransportAppium {
		device, getErr := service.devices.GetDevice(ctx, deviceID)
		if getErr != nil {
			return View{}, fmt.Errorf("%w: 无法读取 iOS 远控设备", ErrUnavailable)
		}
		binding, bindingErr := service.iosSessions.RemoteBinding(ctx, actor.ID, kept.ID, deviceID)
		switch {
		case errors.Is(bindingErr, iossession.ErrNotFound):
			service.ensureIOSSession(device, kept)
			view := service.viewWithoutURL(deviceID, transport, kept)
			view.Status = "connecting"
			return view, nil
		case bindingErr != nil:
			return View{}, fmt.Errorf("%w: iOS 远控 Session 尚不可用", ErrUnavailable)
		case service.iosHealth(ctx, binding) != nil:
			return View{}, fmt.Errorf("%w: iOS 远控 Session 健康检查失败", ErrUnavailable)
		}
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
		entry.Fragment = "!/control/" + url.PathEscape(device.Serial)
		view.URL = entry.String()
	case TransportAppium:
		binding, err := service.iosSessions.RemoteBinding(ctx, current.OwnerID, current.ID, device.ID)
		if errors.Is(err, iossession.ErrNotFound) {
			view.Status = "connecting"
			service.ensureIOSSession(device, current)
			return view, nil
		}
		if err != nil {
			return View{}, fmt.Errorf("%w: iOS 远控 Session 尚不可用", ErrUnavailable)
		}
		_ = binding
		token, err := service.signGatewayToken(gatewayClaims{
			DeviceID: device.ID, ReservationID: current.ID, OwnerID: current.OwnerID,
			ExpiresAt: service.config.Now().Add(service.config.IOSGatewayTokenTTL).Unix(),
		})
		if err != nil {
			return View{}, err
		}
		view.URL = "/console/remote/ios/" + url.PathEscape(token) + "/control"
	default:
		return View{}, ErrUnavailable
	}
	return view, nil
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

func (service *Service) signGatewayToken(claims gatewayClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("签发 iOS 远控入口：%w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(service.config.IOSGatewaySecret))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (service *Service) ensureIOSSession(device management.Device, current reservation.View) {
	if service == nil || service.iosSessions == nil || current.Status != domain.ReservationActive ||
		current.DeviceID == nil || *current.DeviceID != device.ID {
		return
	}
	if _, loaded := service.iosStarts.LoadOrStore(current.ID, struct{}{}); loaded {
		return
	}
	go func() {
		defer service.iosStarts.Delete(current.ID)
		ctx, cancel := context.WithTimeout(context.Background(), service.config.IOSCreateTimeout)
		defer cancel()
		actor := audit.Console(current.OwnerID)
		grant, err := service.iosSessions.IssueManual(ctx, actor, current.ID,
			"ios_remote_grant_"+current.ID, iossession.GrantInput{
				OwnerType: "manual", OwnerID: current.OwnerID, TTLSeconds: 120,
			})
		if err == nil {
			err = service.createIOSSession(ctx, grant)
		}
		if err == nil {
			return
		}
		releaseContext, releaseCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer releaseCancel()
		_, _ = service.reservations.Release(releaseContext, actor,
			"ios-remote-create-failed-"+current.ID, current.ID, "ios_remote_create_failed_"+current.ID,
			reservation.ReleaseInput{Reason: "iOS 远控 Session 创建失败"})
	}()
}

func (service *Service) createIOSSession(ctx context.Context, grant iossession.GrantView) error {
	payload, err := json.Marshal(map[string]any{"capabilities": map[string]any{
		"alwaysMatch": map[string]any{
			"platformName": "iOS", "appium:automationName": "XCUITest",
			"appium:udid": grant.UDID, "df:udids": grant.UDID,
			"appium:noReset": true, "df:skipReport": true,
		},
		"firstMatch": []any{map[string]any{}},
	}})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(grant.FenceEndpoint, "/")+"/session", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Session-Grant "+grant.SessionGrant)
	request.Header.Set("Content-Type", "application/json")
	response, err := service.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var envelope struct {
		SessionID string `json:"sessionId"`
		Value     struct {
			SessionID string `json:"sessionId"`
		} `json:"value"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 || decoder.Decode(&envelope) != nil {
		return errors.New("iOS 远控 Appium Session 创建失败")
	}
	if envelope.Value.SessionID != "" {
		envelope.SessionID = envelope.Value.SessionID
	}
	if strings.TrimSpace(envelope.SessionID) == "" {
		return errors.New("iOS 远控 Appium Session 响应无效")
	}
	return nil
}

func (service *Service) iosBinding(ctx context.Context, claims gatewayClaims) (iossession.RemoteBindingView, error) {
	if service == nil || service.iosSessions == nil {
		return iossession.RemoteBindingView{}, ErrUnavailable
	}
	binding, err := service.iosSessions.RemoteBinding(ctx, claims.OwnerID, claims.ReservationID, claims.DeviceID)
	if err != nil {
		return iossession.RemoteBindingView{}, fmt.Errorf("%w: iOS 远控 Session 未绑定", ErrUnavailable)
	}
	return binding, nil
}

func (service *Service) iosFenceRequest(ctx context.Context, binding iossession.RemoteBindingView, method, operation string, body []byte) (*http.Response, error) {
	if method != http.MethodGet && method != http.MethodPost {
		return nil, ErrUnavailable
	}
	switch operation {
	case "stream", "frame", "health", "actions":
	default:
		return nil, ErrUnavailable
	}
	endpoint := strings.TrimRight(binding.FenceEndpoint, "/") + "/internal/v1/ios-remote/sessions/" +
		url.PathEscape(binding.AppiumSessionID) + "/" + operation
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+service.config.AgentToken)
	request.Header.Set("X-Device-Farm-Host-Id", binding.HostID)
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	client := service.httpClient
	if operation == "stream" {
		client = service.streamClient
	}
	return client.Do(request)
}

func (service *Service) iosHealth(ctx context.Context, binding iossession.RemoteBindingView) error {
	response, err := service.iosFenceRequest(ctx, binding, http.MethodGet, "health", nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("iOS Session Fence 健康状态码 %d", response.StatusCode)
	}
	return nil
}

func (service *Service) remoteDevice(ctx context.Context, deviceID string) (management.Device, string, error) {
	device, err := service.devices.GetDevice(ctx, deviceID)
	if err != nil {
		return management.Device{}, "", fmt.Errorf("%w: 无法读取远控设备", ErrUnavailable)
	}
	transport, err := service.transportForDevice(device, true)
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
		if requireConfigured && (!service.config.IOSEnabled || service.iosSessions == nil) {
			return "", fmt.Errorf("%w: iOS 远程控制尚未配置", ErrUnavailable)
		}
		return TransportAppium, nil
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

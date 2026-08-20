package baguette

import (
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
	"time"
)

const Version = "0.1.92"

var (
	ErrUnavailable  = errors.New("Baguette 远程控制服务不可用")
	ErrNotBooted    = errors.New("目标 iOS 模拟器尚未启动")
	ErrUnauthorized = errors.New("Baguette 远程控制入口无效")
)

type Config struct {
	UpstreamURL string
	PublicURL   string
	Secret      string
	TicketTTL   time.Duration
	HTTPClient  *http.Client
	Now         func() time.Time
}

type Client struct {
	upstream   *url.URL
	public     *url.URL
	secret     []byte
	ticketTTL  time.Duration
	httpClient *http.Client
	now        func() time.Time
}

type Ticket struct {
	DeviceID      string `json:"device_id"`
	UDID          string `json:"udid"`
	ReservationID string `json:"reservation_id"`
	OwnerID       string `json:"owner_id"`
	ExpiresAt     int64  `json:"expires_at"`
}

type simulator struct {
	UDID  string `json:"udid"`
	State string `json:"state"`
}

type inventory struct {
	Running   []simulator `json:"running"`
	Available []simulator `json:"available"`
}

func New(cfg Config) (*Client, error) {
	upstream, err := parseURL(cfg.UpstreamURL)
	if err != nil {
		return nil, fmt.Errorf("%w: 上游地址无效", ErrUnavailable)
	}
	public, err := parseURL(cfg.PublicURL)
	if err != nil || public.Path != "" && public.Path != "/" {
		return nil, fmt.Errorf("%w: 公开入口必须使用独立站点根地址", ErrUnavailable)
	}
	if len(cfg.Secret) < 32 || cfg.TicketTTL <= 0 || cfg.TicketTTL > time.Minute {
		return nil, fmt.Errorf("%w: 签名配置无效", ErrUnavailable)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	upstream.Path = strings.TrimRight(upstream.Path, "/")
	public.Path = strings.TrimRight(public.Path, "/")
	return &Client{upstream: upstream, public: public, secret: []byte(cfg.Secret),
		ticketTTL: cfg.TicketTTL, httpClient: cfg.HTTPClient, now: cfg.Now}, nil
}

func (client *Client) EnsureBooted(ctx context.Context, udid string) error {
	if client == nil || strings.TrimSpace(udid) == "" {
		return ErrUnavailable
	}
	endpoint := *client.upstream
	endpoint.Path += "/simulators.json"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("%w: 健康检查状态码 %d", ErrUnavailable, response.StatusCode)
	}
	var result inventory
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if decoder.Decode(&result) != nil {
		return fmt.Errorf("%w: 设备清单响应无效", ErrUnavailable)
	}
	for _, device := range result.Running {
		if device.UDID == udid && strings.EqualFold(device.State, "Booted") {
			return nil
		}
	}
	return ErrNotBooted
}

func (client *Client) EntryURL(deviceID, udid, reservationID, ownerID string) (string, error) {
	if client == nil || deviceID == "" || udid == "" || reservationID == "" || ownerID == "" {
		return "", ErrUnavailable
	}
	ticket := Ticket{DeviceID: deviceID, UDID: udid, ReservationID: reservationID,
		OwnerID: ownerID, ExpiresAt: client.now().Add(client.ticketTTL).Unix()}
	token, err := client.sign(ticket)
	if err != nil {
		return "", err
	}
	entry := *client.public
	entry.Path += "/entry/" + url.PathEscape(token)
	return entry.String(), nil
}

func (client *Client) sign(ticket Ticket) (string, error) {
	payload, err := json.Marshal(ticket)
	if err != nil {
		return "", fmt.Errorf("签发 Baguette 入口：%w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, client.secret)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (client *Client) verify(token string, checkExpiry bool) (Ticket, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Ticket{}, ErrUnauthorized
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Ticket{}, ErrUnauthorized
	}
	mac := hmac.New(sha256.New, client.secret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return Ticket{}, ErrUnauthorized
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Ticket{}, ErrUnauthorized
	}
	var ticket Ticket
	if json.Unmarshal(payload, &ticket) != nil || ticket.DeviceID == "" || ticket.UDID == "" ||
		ticket.ReservationID == "" || ticket.OwnerID == "" || checkExpiry && ticket.ExpiresAt < client.now().Unix() {
		return Ticket{}, ErrUnauthorized
	}
	return ticket, nil
}

func parseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrUnavailable
	}
	return parsed, nil
}

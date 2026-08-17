package appiumdevicefarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	PinnedVersion    = "12.0.1"
	maxResponseBytes = 4 << 20
)

type Config struct {
	Endpoint              string
	Timeout               time.Duration
	AllowUDIDs            []string
	HTTPClient            *http.Client
	XcrunBinary           string
	LifecyclePollInterval time.Duration
	CommandRunner         CommandRunner
}

type Client struct {
	endpoint              *url.URL
	httpClient            *http.Client
	allowUDIDs            map[string]struct{}
	xcrunBinary           string
	lifecyclePollInterval time.Duration
	commandRunner         CommandRunner
}

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, binary string, arguments ...string) ([]byte, error) {
	return exec.CommandContext(ctx, binary, arguments...).CombinedOutput()
}

type Device struct {
	UDID            string
	Name            string
	State           string
	Platform        string
	PlatformVersion string
	DeviceType      string
	ProductModel    string
	Busy            bool
	UserBlocked     bool
	RealDevice      bool
	Offline         bool
	Allowed         bool
}

type NodeHealth struct {
	AppiumReady   bool
	PluginReady   bool
	PluginVersion string
}

func (health NodeHealth) Ready() bool {
	return health.AppiumReady && health.PluginReady
}

func New(config Config) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(config.Endpoint))
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid Appium Device Farm endpoint")
	}
	hostname := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(hostname)
	if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("Appium Device Farm adapter only accepts a loopback Node endpoint")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("Appium Device Farm timeout must be positive")
	}
	httpClient := &http.Client{}
	if config.HTTPClient != nil {
		*httpClient = *config.HTTPClient
	}
	httpClient.Timeout = config.Timeout
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("Appium Device Farm redirects are not allowed")
	}
	allowUDIDs := make(map[string]struct{}, len(config.AllowUDIDs))
	for _, udid := range config.AllowUDIDs {
		if udid = strings.TrimSpace(udid); udid != "" {
			allowUDIDs[udid] = struct{}{}
		}
	}
	xcrunBinary := strings.TrimSpace(config.XcrunBinary)
	if xcrunBinary == "" {
		xcrunBinary = "xcrun"
	}
	pollInterval := config.LifecyclePollInterval
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	runner := config.CommandRunner
	if runner == nil {
		runner = execCommandRunner{}
	}
	return &Client{endpoint: parsed, httpClient: httpClient, allowUDIDs: allowUDIDs,
		xcrunBinary: xcrunBinary, lifecyclePollInterval: pollInterval, commandRunner: runner}, nil
}

func (client *Client) Inventory(ctx context.Context) ([]Device, error) {
	var payload []struct {
		UDID         string `json:"udid"`
		Name         string `json:"name"`
		State        string `json:"state"`
		SDK          string `json:"sdk"`
		Platform     string `json:"platform"`
		DeviceType   string `json:"deviceType"`
		ProductModel string `json:"productModel"`
		Busy         bool   `json:"busy"`
		UserBlocked  bool   `json:"userBlocked"`
		RealDevice   bool   `json:"realDevice"`
		Offline      bool   `json:"offline"`
	}
	if err := client.getJSON(ctx, "/device-farm/api/device/ios", &payload); err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(payload))
	seen := make(map[string]struct{}, len(payload))
	for _, item := range payload {
		item.UDID = strings.TrimSpace(item.UDID)
		if item.UDID == "" || strings.ToLower(strings.TrimSpace(item.Platform)) != "ios" {
			continue
		}
		if _, exists := seen[item.UDID]; exists {
			return nil, fmt.Errorf("Appium Device Farm 的 iOS 设备清单中存在重复 UDID")
		}
		seen[item.UDID] = struct{}{}
		_, allowed := client.allowUDIDs[item.UDID]
		deviceType := strings.ToLower(strings.TrimSpace(item.DeviceType))
		if deviceType == "real" || item.RealDevice {
			deviceType = "physical"
		} else {
			deviceType = "simulator"
		}
		devices = append(devices, Device{
			UDID: item.UDID, Name: strings.TrimSpace(item.Name), State: strings.TrimSpace(item.State),
			Platform: "ios", PlatformVersion: strings.TrimSpace(item.SDK), DeviceType: deviceType,
			ProductModel: strings.TrimSpace(item.ProductModel), Busy: item.Busy, UserBlocked: item.UserBlocked,
			RealDevice: item.RealDevice, Offline: item.Offline, Allowed: allowed,
		})
	}
	sort.Slice(devices, func(left, right int) bool { return devices[left].UDID < devices[right].UDID })
	return devices, nil
}

func (client *Client) Health(ctx context.Context) (NodeHealth, error) {
	var appium struct {
		Value struct {
			Ready *bool `json:"ready"`
		} `json:"value"`
	}
	if err := client.getJSON(ctx, "/status", &appium); err != nil {
		return NodeHealth{}, fmt.Errorf("read Appium status: %w", err)
	}
	var plugin struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := client.getJSON(ctx, "/device-farm/api/status", &plugin); err != nil {
		return NodeHealth{AppiumReady: appium.Value.Ready != nil && *appium.Value.Ready}, fmt.Errorf("read Device Farm status: %w", err)
	}
	return NodeHealth{
		AppiumReady:   appium.Value.Ready != nil && *appium.Value.Ready,
		PluginReady:   strings.EqualFold(strings.TrimSpace(plugin.Status), "ok"),
		PluginVersion: strings.TrimSpace(plugin.Version),
	}, nil
}

func (client *Client) getJSON(ctx context.Context, path string, target any) error {
	if client == nil || client.endpoint == nil || client.httpClient == nil {
		return errors.New("Appium Device Farm client is not configured")
	}
	endpoint := *client.endpoint
	endpoint.Path = path
	endpoint.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON response: %w", err)
	}
	return nil
}

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
	AllowedRuntimeIDs     []string
	AllowedDeviceTypeIDs  []string
	ManagedNamePrefix     string
	HTTPClient            *http.Client
	XcrunBinary           string
	LifecyclePollInterval time.Duration
	CommandRunner         CommandRunner
}

type Client struct {
	endpoint              *url.URL
	httpClient            *http.Client
	allowUDIDs            map[string]struct{}
	allowedRuntimeIDs     map[string]struct{}
	allowedDeviceTypeIDs  map[string]struct{}
	managedNamePrefix     string
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
	Managed         bool
	RouterPresent   bool
	RuntimeID       string
	DeviceTypeID    string
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
		return nil, errors.New("Appium Device Farm 地址无效")
	}
	hostname := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(hostname)
	if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("Appium Device Farm Adapter 只接受本机回环地址")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("Appium Device Farm 超时时间必须大于零")
	}
	httpClient := &http.Client{}
	if config.HTTPClient != nil {
		*httpClient = *config.HTTPClient
	}
	httpClient.Timeout = config.Timeout
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("Appium Device Farm 不允许重定向")
	}
	allowUDIDs := make(map[string]struct{}, len(config.AllowUDIDs))
	for _, udid := range config.AllowUDIDs {
		if udid = strings.TrimSpace(udid); udid != "" {
			allowUDIDs[udid] = struct{}{}
		}
	}
	allowedRuntimeIDs := normalizedSet(config.AllowedRuntimeIDs)
	allowedDeviceTypeIDs := normalizedSet(config.AllowedDeviceTypeIDs)
	managedNamePrefix := strings.TrimSpace(config.ManagedNamePrefix)
	if managedNamePrefix == "" {
		managedNamePrefix = "Alcor-DF-"
	}
	if strings.ContainsAny(managedNamePrefix, "\r\n\t/") || len(managedNamePrefix) > 32 {
		return nil, errors.New("动态 Simulator 名称前缀无效")
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
		allowedRuntimeIDs: allowedRuntimeIDs, allowedDeviceTypeIDs: allowedDeviceTypeIDs, managedNamePrefix: managedNamePrefix,
		xcrunBinary: xcrunBinary, lifecyclePollInterval: pollInterval, commandRunner: runner}, nil
}

func normalizedSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = struct{}{}
		}
	}
	return result
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
	seen := make(map[string]int, len(payload))
	for _, item := range payload {
		item.UDID = strings.TrimSpace(item.UDID)
		if item.UDID == "" || strings.ToLower(strings.TrimSpace(item.Platform)) != "ios" {
			continue
		}
		_, allowed := client.allowUDIDs[item.UDID]
		deviceType := strings.ToLower(strings.TrimSpace(item.DeviceType))
		if deviceType == "real" || item.RealDevice {
			deviceType = "physical"
		} else {
			deviceType = "simulator"
		}
		device := Device{
			UDID: item.UDID, Name: strings.TrimSpace(item.Name), State: strings.TrimSpace(item.State),
			Platform: "ios", PlatformVersion: strings.TrimSpace(item.SDK), DeviceType: deviceType,
			ProductModel: strings.TrimSpace(item.ProductModel), Busy: item.Busy, UserBlocked: item.UserBlocked,
			RealDevice: item.RealDevice, Offline: item.Offline, Allowed: allowed,
			Managed: strings.HasPrefix(strings.TrimSpace(item.Name), client.managedNamePrefix), RouterPresent: true,
		}
		if index, exists := seen[item.UDID]; exists {
			if !sameInventoryIdentity(devices[index], device) {
				return nil, fmt.Errorf("Appium Device Farm 的 iOS 设备清单中存在身份冲突的重复 UDID")
			}
			devices[index] = mergeInventoryDevice(devices[index], device)
			continue
		}
		seen[item.UDID] = len(devices)
		devices = append(devices, device)
	}
	sort.Slice(devices, func(left, right int) bool { return devices[left].UDID < devices[right].UDID })
	return devices, nil
}

func sameInventoryIdentity(left, right Device) bool {
	return left.DeviceType == right.DeviceType && left.RealDevice == right.RealDevice &&
		compatibleInventoryText(left.Name, right.Name) && compatibleInventoryText(left.PlatformVersion, right.PlatformVersion) &&
		compatibleInventoryText(left.ProductModel, right.ProductModel)
}

func compatibleInventoryText(left, right string) bool {
	return left == "" || right == "" || left == right
}

func mergeInventoryDevice(left, right Device) Device {
	if left.Name == "" {
		left.Name = right.Name
	}
	if left.PlatformVersion == "" {
		left.PlatformVersion = right.PlatformVersion
	}
	if left.ProductModel == "" {
		left.ProductModel = right.ProductModel
	}
	if strings.EqualFold(right.State, "Booted") {
		left.State = right.State
	}
	left.Busy = left.Busy || right.Busy
	left.UserBlocked = left.UserBlocked || right.UserBlocked
	left.Offline = left.Offline && right.Offline
	left.Allowed = left.Allowed || right.Allowed
	left.Managed = left.Managed || right.Managed
	left.RouterPresent = left.RouterPresent || right.RouterPresent
	return left
}

func (client *Client) Health(ctx context.Context) (NodeHealth, error) {
	var appium struct {
		Value struct {
			Ready *bool `json:"ready"`
		} `json:"value"`
	}
	if err := client.getJSON(ctx, "/status", &appium); err != nil {
		return NodeHealth{}, fmt.Errorf("读取 Appium 状态失败：%w", err)
	}
	var plugin struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := client.getJSON(ctx, "/device-farm/api/status", &plugin); err != nil {
		return NodeHealth{AppiumReady: appium.Value.Ready != nil && *appium.Value.Ready}, fmt.Errorf("读取 Device Farm 状态失败：%w", err)
	}
	return NodeHealth{
		AppiumReady:   appium.Value.Ready != nil && *appium.Value.Ready,
		PluginReady:   strings.EqualFold(strings.TrimSpace(plugin.Status), "ok"),
		PluginVersion: strings.TrimSpace(plugin.Version),
	}, nil
}

func (client *Client) getJSON(ctx context.Context, path string, target any) error {
	if client == nil || client.endpoint == nil || client.httpClient == nil {
		return errors.New("Appium Device Farm 客户端尚未配置")
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
		return fmt.Errorf("解析 JSON 响应失败：%w", err)
	}
	return nil
}

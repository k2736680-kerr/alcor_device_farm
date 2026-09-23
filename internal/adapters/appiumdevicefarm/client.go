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
	PinnedVersion                   = "12.0.1"
	maxResponseBytes                = 4 << 20
	defaultRegistrationRetryTimeout = 90 * time.Second
	defaultRegistrationRetryDelay   = 2 * time.Second
	defaultReadinessStableDuration  = 10 * time.Second
)

type Config struct {
	Endpoint                 string
	RegistrationEndpoints    []string
	Timeout                  time.Duration
	AllowUDIDs               []string
	AllowedRuntimeIDs        []string
	AllowedDeviceTypeIDs     []string
	ManagedNamePrefix        string
	HTTPClient               *http.Client
	XcrunBinary              string
	LifecyclePollInterval    time.Duration
	ReadinessStableDuration  time.Duration
	RegistrationRetryTimeout time.Duration
	RegistrationRetryDelay   time.Duration
	CommandRunner            CommandRunner
}

type Client struct {
	endpoint                 *url.URL
	registrationEndpoints    []*url.URL
	httpClient               *http.Client
	allowUDIDs               map[string]struct{}
	allowedRuntimeIDs        map[string]struct{}
	allowedDeviceTypeIDs     map[string]struct{}
	managedNamePrefix        string
	xcrunBinary              string
	lifecyclePollInterval    time.Duration
	readinessStableDuration  time.Duration
	registrationRetryTimeout time.Duration
	registrationRetryDelay   time.Duration
	commandRunner            CommandRunner
}

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, binary string, arguments ...string) ([]byte, error) {
	return exec.CommandContext(ctx, binary, arguments...).CombinedOutput()
}

type serializedCommandRunner struct {
	delegate CommandRunner
	simctl   chan struct{}
}

// NewSerializedCommandRunner serializes CoreSimulator mutations and inventory
// reads that share the same CoreSimulatorService. Waiting for the gate remains
// context-aware so a timed-out Host Command cannot become an invisible backlog.
// Non-simctl probes such as Appium doctor continue to run independently.
func NewSerializedCommandRunner(delegate CommandRunner) CommandRunner {
	if delegate == nil {
		delegate = execCommandRunner{}
	}
	return &serializedCommandRunner{delegate: delegate, simctl: make(chan struct{}, 1)}
}

func (runner *serializedCommandRunner) Run(ctx context.Context, binary string, arguments ...string) ([]byte, error) {
	if len(arguments) == 0 || arguments[0] != "simctl" {
		return runner.delegate.Run(ctx, binary, arguments...)
	}
	select {
	case runner.simctl <- struct{}{}:
		defer func() { <-runner.simctl }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return runner.delegate.Run(ctx, binary, arguments...)
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
	parsed, err := parseLoopbackEndpoint(config.Endpoint)
	if err != nil {
		return nil, errors.New("Appium Device Farm 地址无效")
	}
	registrationValues := config.RegistrationEndpoints
	if len(registrationValues) == 0 {
		registrationValues = []string{parsed.String()}
	}
	registrationEndpoints := make([]*url.URL, 0, len(registrationValues))
	seenRegistrationEndpoints := map[string]struct{}{}
	for _, value := range registrationValues {
		registrationEndpoint, registrationErr := parseLoopbackEndpoint(value)
		if registrationErr != nil {
			return nil, errors.New("Appium Device Farm 设备注销地址无效")
		}
		key := registrationEndpoint.String()
		if _, exists := seenRegistrationEndpoints[key]; exists {
			continue
		}
		seenRegistrationEndpoints[key] = struct{}{}
		registrationEndpoints = append(registrationEndpoints, registrationEndpoint)
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
	readinessStableDuration := config.ReadinessStableDuration
	if readinessStableDuration <= 0 {
		readinessStableDuration = defaultReadinessStableDuration
	}
	registrationRetryTimeout := config.RegistrationRetryTimeout
	if registrationRetryTimeout <= 0 {
		registrationRetryTimeout = defaultRegistrationRetryTimeout
	}
	registrationRetryDelay := config.RegistrationRetryDelay
	if registrationRetryDelay <= 0 {
		registrationRetryDelay = defaultRegistrationRetryDelay
	}
	runner := config.CommandRunner
	if runner == nil {
		runner = NewSerializedCommandRunner(nil)
	}
	return &Client{endpoint: parsed, registrationEndpoints: registrationEndpoints, httpClient: httpClient, allowUDIDs: allowUDIDs,
		allowedRuntimeIDs: allowedRuntimeIDs, allowedDeviceTypeIDs: allowedDeviceTypeIDs, managedNamePrefix: managedNamePrefix,
		xcrunBinary: xcrunBinary, lifecyclePollInterval: pollInterval, readinessStableDuration: readinessStableDuration,
		registrationRetryTimeout: registrationRetryTimeout, registrationRetryDelay: registrationRetryDelay, commandRunner: runner}, nil
}

func parseLoopbackEndpoint(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("地址无效")
	}
	hostname := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(hostname)
	if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("只接受本机回环地址")
	}
	return parsed, nil
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
	health, err := client.healthFrom(ctx, client.endpoint)
	if err != nil {
		return health, err
	}
	for _, endpoint := range client.registrationEndpoints {
		if endpoint.String() == client.endpoint.String() {
			continue
		}
		nodeHealth, nodeErr := client.healthFrom(ctx, endpoint)
		if nodeErr != nil {
			return health, fmt.Errorf("读取 Appium Device Farm Node 健康状态失败：%w", nodeErr)
		}
		if !nodeHealth.Ready() {
			return health, errors.New("Appium Device Farm Node 尚未就绪")
		}
		var inventory []json.RawMessage
		if inventoryErr := client.getJSONFrom(ctx, endpoint, "/device-farm/api/device/ios", &inventory); inventoryErr != nil {
			return health, fmt.Errorf("读取 Appium Device Farm Node 设备清单失败：%w", inventoryErr)
		}
	}
	return health, nil
}

func (client *Client) healthFrom(ctx context.Context, endpoint *url.URL) (NodeHealth, error) {
	var appium struct {
		Value struct {
			Ready *bool `json:"ready"`
		} `json:"value"`
	}
	if err := client.getJSONFrom(ctx, endpoint, "/status", &appium); err != nil {
		return NodeHealth{}, fmt.Errorf("读取 Appium 状态失败：%w", err)
	}
	var plugin struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := client.getJSONFrom(ctx, endpoint, "/device-farm/api/status", &plugin); err != nil {
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
	return client.getJSONFrom(ctx, client.endpoint, path, target)
}

func (client *Client) getJSONFrom(ctx context.Context, base *url.URL, path string, target any) error {
	endpoint := *base
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
		return fmt.Errorf("请求返回 HTTP 状态码 %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("解析 JSON 响应失败：%w", err)
	}
	return nil
}

func (client *Client) unregisterManagedSimulator(ctx context.Context, udid string) error {
	udid = strings.TrimSpace(udid)
	if client == nil || client.httpClient == nil || udid == "" || strings.HasPrefix(udid, "pending:") {
		return nil
	}
	for _, base := range client.registrationEndpoints {
		if err := client.unregisterManagedSimulatorAt(ctx, base, udid); err != nil {
			return err
		}
	}
	return nil
}

func (client *Client) unregisterManagedSimulatorBestEffort(ctx context.Context, udid string) {
	udid = strings.TrimSpace(udid)
	if client == nil || client.httpClient == nil || udid == "" || strings.HasPrefix(udid, "pending:") {
		return
	}
	for _, base := range client.registrationEndpoints {
		_ = client.unregisterManagedSimulatorOnce(ctx, base, udid)
	}
}

func (client *Client) unregisterManagedSimulatorAt(ctx context.Context, base *url.URL, udid string) error {
	retryCtx, cancel := context.WithTimeout(ctx, client.registrationRetryTimeout)
	defer cancel()
	var lastErr error
	for {
		if err := client.unregisterManagedSimulatorOnce(retryCtx, base, udid); err == nil {
			return nil
		} else {
			lastErr = err
		}
		timer := time.NewTimer(client.registrationRetryDelay)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("等待 Appium Device Farm 设备清单恢复超时：%w", lastErr)
		case <-timer.C:
		}
	}
}

func (client *Client) unregisterManagedSimulatorOnce(ctx context.Context, base *url.URL, udid string) error {
	var inventory []map[string]any
	if err := client.getJSONFrom(ctx, base, "/device-farm/api/device/ios", &inventory); err != nil {
		return err
	}
	matches := make([]map[string]any, 0, 1)
	for _, item := range inventory {
		itemUDID, _ := item["udid"].(string)
		name, _ := item["name"].(string)
		if strings.TrimSpace(itemUDID) == udid && strings.HasPrefix(strings.TrimSpace(name), client.managedNamePrefix) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	body, err := json.Marshal(matches)
	if err != nil {
		return err
	}
	endpoint := *base
	endpoint.Path = "/device-farm/api/register"
	endpoint.RawPath = ""
	endpoint.RawQuery = "type=remove"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	closeErr := response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("设备注销请求返回 HTTP 状态码 %d", response.StatusCode)
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

package dafit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type Config struct {
	ServerURL             string
	ServiceToken          string
	PoolID                string
	OwnerID               string
	LeaseSeconds          int
	RequestedCapabilities map[string]any
	DaFitDirectory        string
	ReportDirectory       string
	PythonExecutable      string
	ADBExecutable         string
	CaseID                string
	WaitTimeout           time.Duration
	PollInterval          time.Duration
	RunTimeout            time.Duration
	LeaseRenewInterval    time.Duration
}

type Result struct {
	ReservationID string `json:"reservation_id"`
	DeviceID      string `json:"device_id,omitempty"`
	ReportHTML    string `json:"report_html,omitempty"`
	ReportJSON    string `json:"report_json,omitempty"`
	ExitError     string `json:"exit_error,omitempty"`
}

type RunSpec struct {
	Directory        string
	ReportDirectory  string
	PythonExecutable string
	ADBExecutable    string
	ADBEndpoint      string
	AppiumUDID       string
	AppiumEndpoint   string
	CaseID           string
}

type Runner interface {
	Run(context.Context, RunSpec) error
}

type OSRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

func (runner OSRunner) Run(ctx context.Context, spec RunSpec) error {
	if strings.Contains(spec.ADBEndpoint, ":") {
		connect := exec.CommandContext(ctx, spec.ADBExecutable, "connect", spec.ADBEndpoint)
		connect.Dir, connect.Stdout, connect.Stderr = spec.Directory, runner.Stdout, runner.Stderr
		if err := connect.Run(); err != nil {
			return fmt.Errorf("连接已分配的 ADB 端点失败：%w", err)
		}
	}
	command := exec.CommandContext(ctx, spec.PythonExecutable, "tools/run_full.py", "--case", spec.CaseID)
	command.Dir, command.Stdout, command.Stderr = spec.Directory, runner.Stdout, runner.Stderr
	command.Env = childEnvironment(map[string]string{
		"DAFIT_RUN_MODE": "farm", "ANDROID_UDID": spec.AppiumUDID,
		"ANDROID_ADB_SERIAL": spec.ADBEndpoint, "APPIUM_SERVER": spec.AppiumEndpoint,
		"DAFIT_REPORT_DIR": spec.ReportDirectory,
	})
	if err := command.Run(); err != nil {
		return fmt.Errorf("DaFit 运行失败：%w", err)
	}
	return nil
}

func childEnvironment(overrides map[string]string) []string {
	blocked := map[string]struct{}{
		"DEVICE_FARM_SECURITY_SERVICE_TOKEN": {}, "DEVICE_FARM_DATABASE_URL": {}, "DEVICE_FARM_STF_API_TOKEN": {},
	}
	for key := range overrides {
		blocked[strings.ToUpper(key)] = struct{}{}
	}
	result := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key, _, found := strings.Cut(item, "=")
		if _, skip := blocked[strings.ToUpper(key)]; found && skip {
			continue
		}
		result = append(result, item)
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

type Harness struct {
	httpClient *http.Client
	runner     Runner
}

func New(client *http.Client, runner Runner) *Harness {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if runner == nil {
		runner = OSRunner{Stdout: os.Stdout, Stderr: os.Stderr}
	}
	return &Harness{httpClient: client, runner: runner}
}

func (harness *Harness) Run(ctx context.Context, config Config) (result Result, returnErr error) {
	if err := validate(config); err != nil {
		return Result{}, err
	}
	reservation, err := harness.createReservation(ctx, config)
	if err != nil {
		return Result{}, err
	}
	result.ReservationID = reservation.ID
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := harness.release(cleanupCtx, config, reservation.ID); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("释放预约 %s 失败：%w", reservation.ID, err))
		}
	}()

	waitCtx, cancelWait := context.WithTimeout(ctx, config.WaitTimeout)
	active, err := harness.waitActive(waitCtx, config, reservation.ID)
	cancelWait()
	if err != nil {
		return result, err
	}
	result.DeviceID = active.DeviceID
	device, err := harness.getDevice(ctx, config, active.DeviceID)
	if err != nil {
		return result, err
	}
	appiumUDID, _ := device.Capabilities["appiumUdid"].(string)
	if strings.TrimSpace(appiumUDID) == "" || device.ADBEndpoint == "" || device.AppiumEndpoint == "" {
		return result, errors.New("已激活设备的连接信息不完整")
	}
	var runCtx context.Context
	var cancelRun context.CancelFunc
	if config.RunTimeout > 0 {
		runCtx, cancelRun = context.WithTimeout(ctx, config.RunTimeout)
	} else {
		runCtx, cancelRun = context.WithCancel(ctx)
	}
	renewCtx, cancelRenew := context.WithCancel(runCtx)
	renewErrors := make(chan error, 1)
	renewStopped := make(chan struct{})
	go func() {
		defer close(renewStopped)
		if keepAliveErr := harness.keepAlive(renewCtx, config, reservation.ID); keepAliveErr != nil {
			renewErrors <- keepAliveErr
		}
	}()
	runDone := make(chan error, 1)
	go func() {
		runDone <- harness.runner.Run(runCtx, RunSpec{
			Directory: config.DaFitDirectory, ReportDirectory: config.ReportDirectory,
			PythonExecutable: config.PythonExecutable, ADBExecutable: config.ADBExecutable,
			ADBEndpoint: device.ADBEndpoint, AppiumUDID: appiumUDID,
			AppiumEndpoint: device.AppiumEndpoint, CaseID: config.CaseID,
		})
	}()
	var runErr error
	select {
	case runErr = <-runDone:
	case renewErr := <-renewErrors:
		runErr = fmt.Errorf("预约自动续约失败：%w", renewErr)
		cancelRun()
		if stoppedRunErr := <-runDone; stoppedRunErr != nil {
			runErr = errors.Join(runErr, stoppedRunErr)
		}
	case <-runCtx.Done():
		cancelRun()
		runErr = <-runDone
	}
	cancelRenew()
	<-renewStopped
	select {
	case renewErr := <-renewErrors:
		runErr = errors.Join(runErr, fmt.Errorf("预约自动续约失败：%w", renewErr))
	default:
	}
	cancelRun()
	result.ReportHTML = filepath.Join(config.ReportDirectory, "report.html")
	result.ReportJSON = filepath.Join(config.ReportDirectory, "report.json")
	var reportErr error
	for _, path := range []string{result.ReportHTML, result.ReportJSON} {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			reportErr = errors.Join(reportErr, fmt.Errorf("缺少预期的 DaFit 报告：%s", path))
		}
	}
	if runErr != nil {
		result.ExitError = runErr.Error()
	}
	return result, errors.Join(runErr, reportErr)
}

type reservationView struct {
	ID       string `json:"id"`
	DeviceID string `json:"device_id"`
	Status   string `json:"status"`
}

type deviceView struct {
	ID             string         `json:"id"`
	ADBEndpoint    string         `json:"adb_endpoint"`
	AppiumEndpoint string         `json:"appium_endpoint"`
	Capabilities   map[string]any `json:"capabilities"`
}

type apiEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
}

func (harness *Harness) createReservation(ctx context.Context, config Config) (reservationView, error) {
	payload := map[string]any{
		"pool_id": config.PoolID, "owner_type": "test_run", "owner_id": config.OwnerID,
		"lease_seconds": config.LeaseSeconds, "requested_capabilities": config.RequestedCapabilities,
	}
	var reservation reservationView
	err := harness.request(ctx, config, http.MethodPost, "/api/v1/device-reservations", "dafit-create-"+config.OwnerID, payload, &reservation)
	return reservation, err
}

func (harness *Harness) waitActive(ctx context.Context, config Config, id string) (reservationView, error) {
	ticker := time.NewTicker(config.PollInterval)
	defer ticker.Stop()
	for {
		var reservation reservationView
		if err := harness.request(ctx, config, http.MethodGet, "/api/v1/device-reservations/"+url.PathEscape(id), "", nil, &reservation); err != nil {
			return reservationView{}, err
		}
		switch reservation.Status {
		case "active":
			if reservation.DeviceID == "" {
				return reservationView{}, errors.New("已激活预约未包含设备 ID")
			}
			return reservation, nil
		case "failed", "released", "force_released", "expired":
			return reservationView{}, fmt.Errorf("预约已进入终态：%s", reservation.Status)
		}
		select {
		case <-ctx.Done():
			return reservationView{}, fmt.Errorf("等待预约激活失败：%w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (harness *Harness) getDevice(ctx context.Context, config Config, id string) (deviceView, error) {
	var device deviceView
	err := harness.request(ctx, config, http.MethodGet, "/api/v1/devices/"+url.PathEscape(id), "", nil, &device)
	return device, err
}

func (harness *Harness) keepAlive(ctx context.Context, config Config, id string) error {
	interval := config.LeaseRenewInterval
	if interval <= 0 {
		interval = time.Duration(config.LeaseSeconds) * time.Second / 3
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for counter := 1; ; counter++ {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			var extended reservationView
			key := fmt.Sprintf("dafit-extend-%s-%d", config.OwnerID, counter)
			if err := harness.request(ctx, config, http.MethodPost,
				"/api/v1/device-reservations/"+url.PathEscape(id)+"/extensions", key,
				map[string]any{"additional_seconds": config.LeaseSeconds}, &extended); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if extended.Status != "active" {
				return fmt.Errorf("续约后预约状态异常：%s", extended.Status)
			}
		}
	}
}

func (harness *Harness) release(ctx context.Context, config Config, id string) error {
	deadline := time.Now().Add(15 * time.Second)
	for {
		var released reservationView
		err := harness.request(ctx, config, http.MethodPost, "/api/v1/device-reservations/"+url.PathEscape(id)+"/releases",
			"dafit-release-"+config.OwnerID, map[string]any{"reason": "DaFit Harness 已运行结束或停止"}, &released)
		if err == nil || strings.Contains(err.Error(), "NOT_FOUND") {
			return nil
		}
		if !strings.Contains(err.Error(), "CONFLICT") || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (harness *Harness) request(ctx context.Context, config Config, method, path, key string, payload any, target any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(config.ServerURL, "/")+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+config.ServiceToken)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := harness.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var envelope apiEnvelope
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("解析设备农场响应失败：%w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Error != nil {
		if envelope.Error != nil {
			return fmt.Errorf("设备农场 %s：%s", envelope.Error.Code, envelope.Error.Message)
		}
		return fmt.Errorf("设备农场请求失败，HTTP 状态码：%d", response.StatusCode)
	}
	if target != nil {
		if err := json.Unmarshal(envelope.Data, target); err != nil {
			return fmt.Errorf("解析设备农场数据失败：%w", err)
		}
	}
	return nil
}

func validate(config Config) error {
	parsed, err := url.Parse(strings.TrimSpace(config.ServerURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("服务地址必须是无凭据、查询参数和片段的绝对 HTTP(S) 地址")
	}
	if config.ServiceToken == "" || !identifierPattern.MatchString(config.PoolID) || !identifierPattern.MatchString(config.OwnerID) {
		return errors.New("必须提供服务令牌、设备池 ID 和所有者 ID")
	}
	if config.LeaseSeconds < 60 || config.WaitTimeout <= 0 || config.PollInterval <= 0 || config.RunTimeout < 0 || config.LeaseRenewInterval < 0 {
		return errors.New("租期、等待超时和轮询间隔必须为正数，运行超时不能为负数")
	}
	for name, value := range map[string]string{"DaFit 目录": config.DaFitDirectory, "报告目录": config.ReportDirectory} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s 必须使用绝对路径", name)
		}
	}
	if config.PythonExecutable == "" || config.ADBExecutable == "" || config.CaseID == "" {
		return errors.New("必须提供 Python、ADB 和用例 ID")
	}
	return nil
}

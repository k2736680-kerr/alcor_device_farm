package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const envPrefix = "DEVICE_FARM_"

type Config struct {
	Server    ServerConfig    `yaml:"server" json:"server"`
	Database  DatabaseConfig  `yaml:"database" json:"-"`
	Log       LogConfig       `yaml:"log" json:"log"`
	Security  SecurityConfig  `yaml:"security" json:"-"`
	Lease     LeaseConfig     `yaml:"lease" json:"lease"`
	Reconcile ReconcileConfig `yaml:"reconcile" json:"reconcile"`
	WarmPool  WarmPoolConfig  `yaml:"warm_pool" json:"warm_pool"`
	STF       STFConfig       `yaml:"stf" json:"stf"`
	IOSRemote IOSRemoteConfig `yaml:"ios_remote_control" json:"ios_remote_control"`
	Console   ConsoleConfig   `yaml:"console" json:"console"`
}

type DatabaseConfig struct {
	URL string `yaml:"url" json:"-"`
}

type ServerConfig struct {
	Address         string        `yaml:"address" json:"address"`
	ReadTimeout     time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout" json:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout" json:"idle_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" json:"shutdown_timeout"`
}

type LogConfig struct {
	Level  string `yaml:"level" json:"level"`
	Format string `yaml:"format" json:"format"`
}

type SecurityConfig struct {
	ServiceToken         string `yaml:"service_token" json:"-"`
	ServicePreviousToken string `yaml:"service_previous_token" json:"-"`
	AgentToken           string `yaml:"agent_token" json:"-"`
	AgentPreviousToken   string `yaml:"agent_previous_token" json:"-"`
}

type LeaseConfig struct {
	SchedulerInterval time.Duration `yaml:"scheduler_interval" json:"scheduler_interval"`
	ReaperInterval    time.Duration `yaml:"reaper_interval" json:"reaper_interval"`
	GracePeriod       time.Duration `yaml:"grace_period" json:"grace_period"`
}

type ReconcileConfig struct {
	Interval           time.Duration `yaml:"interval" json:"interval"`
	HostTimeout        time.Duration `yaml:"host_timeout" json:"host_timeout"`
	HostRecoveryGrace  time.Duration `yaml:"host_recovery_grace" json:"host_recovery_grace"`
	STFVisibilityGrace time.Duration `yaml:"stf_visibility_grace" json:"stf_visibility_grace"`
	FailureThreshold   int           `yaml:"failure_threshold" json:"failure_threshold"`
}

type WarmPoolConfig struct {
	Interval time.Duration `yaml:"interval" json:"interval"`
}

type STFConfig struct {
	Enabled       bool          `yaml:"enabled" json:"enabled"`
	BaseURL       string        `yaml:"base_url" json:"base_url"`
	APIToken      string        `yaml:"api_token" json:"-"`
	Timeout       time.Duration `yaml:"timeout" json:"timeout"`
	Attempts      int           `yaml:"attempts" json:"attempts"`
	RetryDelay    time.Duration `yaml:"retry_delay" json:"retry_delay"`
	WebURL        string        `yaml:"web_url" json:"web_url,omitempty"`
	WebAuthSecret string        `yaml:"web_auth_secret" json:"-"`
	WebUserName   string        `yaml:"web_user_name" json:"-"`
	WebUserEmail  string        `yaml:"web_user_email" json:"-"`
	WebTokenTTL   time.Duration `yaml:"web_token_ttl" json:"web_token_ttl"`
}

type IOSRemoteConfig struct {
	Enabled         bool          `yaml:"enabled" json:"enabled"`
	BaguetteURL     string        `yaml:"baguette_url" json:"baguette_url,omitempty"`
	GatewayAddress  string        `yaml:"gateway_address" json:"gateway_address,omitempty"`
	PublicURL       string        `yaml:"public_url" json:"public_url,omitempty"`
	GatewaySecret   string        `yaml:"gateway_secret" json:"-"`
	GatewayTokenTTL time.Duration `yaml:"gateway_token_ttl" json:"gateway_token_ttl"`
}

type ConsoleConfig struct {
	Enabled             bool          `yaml:"enabled" json:"enabled"`
	UsersFile           string        `yaml:"users_file" json:"-"`
	DevelopmentInsecure bool          `yaml:"development_insecure" json:"development_insecure"`
	SessionMaxAge       time.Duration `yaml:"session_max_age" json:"session_max_age"`
	SessionIdleTimeout  time.Duration `yaml:"session_idle_timeout" json:"session_idle_timeout"`
	CleanupInterval     time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
	LoginWindow         time.Duration `yaml:"login_window" json:"login_window"`
	LoginMaxFailures    int           `yaml:"login_max_failures" json:"login_max_failures"`
	RemoteLease         time.Duration `yaml:"remote_lease" json:"remote_lease"`
	RemoteHeartbeat     time.Duration `yaml:"remote_heartbeat" json:"remote_heartbeat"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			Address:         "127.0.0.1:8080",
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     60 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Lease: LeaseConfig{
			SchedulerInterval: 250 * time.Millisecond,
			ReaperInterval:    time.Second,
			GracePeriod:       30 * time.Second,
		},
		Reconcile: ReconcileConfig{Interval: 2 * time.Second, HostTimeout: 30 * time.Second,
			HostRecoveryGrace: 90 * time.Second, STFVisibilityGrace: 30 * time.Second, FailureThreshold: 3},
		WarmPool: WarmPoolConfig{Interval: 30 * time.Second},
		STF: STFConfig{
			Timeout: 5 * time.Second, Attempts: 3, RetryDelay: 200 * time.Millisecond, WebTokenTTL: 30 * time.Second,
		},
		IOSRemote: IOSRemoteConfig{GatewayTokenTTL: 30 * time.Second},
		Console: ConsoleConfig{
			SessionMaxAge: 30 * 24 * time.Hour, SessionIdleTimeout: 30 * 24 * time.Hour,
			CleanupInterval: 10 * time.Minute, LoginWindow: 15 * time.Minute, LoginMaxFailures: 5,
			RemoteLease: 60 * time.Second, RemoteHeartbeat: 15 * time.Second,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	if strings.TrimSpace(path) != "" {
		content, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := decodeYAML(content, &cfg); err != nil {
			return Config{}, err
		}
	}

	if err := applyEnvironment(&cfg, os.LookupEnv); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func decodeYAML(content []byte, cfg *Config) error {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	if err := decoder.Decode(cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	if err := ensureSingleDocument(decoder); err != nil {
		return err
	}
	return nil
}

func ensureSingleDocument(decoder *yaml.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	return errors.New("decode config: multiple YAML documents are not supported")
}

func applyEnvironment(cfg *Config, lookup func(string) (string, bool)) error {
	stringValues := []struct {
		name   string
		target *string
	}{
		{"SERVER_ADDRESS", &cfg.Server.Address},
		{"DATABASE_URL", &cfg.Database.URL},
		{"LOG_LEVEL", &cfg.Log.Level},
		{"LOG_FORMAT", &cfg.Log.Format},
		{"SECURITY_SERVICE_TOKEN", &cfg.Security.ServiceToken},
		{"SECURITY_SERVICE_PREVIOUS_TOKEN", &cfg.Security.ServicePreviousToken},
		{"SECURITY_AGENT_TOKEN", &cfg.Security.AgentToken},
		{"SECURITY_AGENT_PREVIOUS_TOKEN", &cfg.Security.AgentPreviousToken},
		{"STF_BASE_URL", &cfg.STF.BaseURL},
		{"STF_API_TOKEN", &cfg.STF.APIToken},
		{"STF_WEB_URL", &cfg.STF.WebURL},
		{"STF_WEB_AUTH_SECRET", &cfg.STF.WebAuthSecret},
		{"STF_WEB_USER_NAME", &cfg.STF.WebUserName},
		{"STF_WEB_USER_EMAIL", &cfg.STF.WebUserEmail},
		{"IOS_REMOTE_CONTROL_BAGUETTE_URL", &cfg.IOSRemote.BaguetteURL},
		{"IOS_REMOTE_CONTROL_GATEWAY_ADDRESS", &cfg.IOSRemote.GatewayAddress},
		{"IOS_REMOTE_CONTROL_PUBLIC_URL", &cfg.IOSRemote.PublicURL},
		{"IOS_REMOTE_CONTROL_GATEWAY_SECRET", &cfg.IOSRemote.GatewaySecret},
		{"CONSOLE_USERS_FILE", &cfg.Console.UsersFile},
	}

	for _, item := range stringValues {
		if value, ok := lookup(envPrefix + item.name); ok {
			*item.target = value
		}
	}

	durations := []struct {
		name   string
		target *time.Duration
	}{
		{"SERVER_READ_TIMEOUT", &cfg.Server.ReadTimeout},
		{"SERVER_WRITE_TIMEOUT", &cfg.Server.WriteTimeout},
		{"SERVER_IDLE_TIMEOUT", &cfg.Server.IdleTimeout},
		{"SERVER_SHUTDOWN_TIMEOUT", &cfg.Server.ShutdownTimeout},
		{"LEASE_SCHEDULER_INTERVAL", &cfg.Lease.SchedulerInterval},
		{"LEASE_REAPER_INTERVAL", &cfg.Lease.ReaperInterval},
		{"LEASE_GRACE_PERIOD", &cfg.Lease.GracePeriod},
		{"RECONCILE_INTERVAL", &cfg.Reconcile.Interval},
		{"RECONCILE_HOST_TIMEOUT", &cfg.Reconcile.HostTimeout},
		{"RECONCILE_HOST_RECOVERY_GRACE", &cfg.Reconcile.HostRecoveryGrace},
		{"RECONCILE_STF_VISIBILITY_GRACE", &cfg.Reconcile.STFVisibilityGrace},
		{"WARM_POOL_INTERVAL", &cfg.WarmPool.Interval},
		{"STF_TIMEOUT", &cfg.STF.Timeout},
		{"STF_RETRY_DELAY", &cfg.STF.RetryDelay},
		{"STF_WEB_TOKEN_TTL", &cfg.STF.WebTokenTTL},
		{"IOS_REMOTE_CONTROL_GATEWAY_TOKEN_TTL", &cfg.IOSRemote.GatewayTokenTTL},
		{"CONSOLE_SESSION_MAX_AGE", &cfg.Console.SessionMaxAge},
		{"CONSOLE_SESSION_IDLE_TIMEOUT", &cfg.Console.SessionIdleTimeout},
		{"CONSOLE_CLEANUP_INTERVAL", &cfg.Console.CleanupInterval},
		{"CONSOLE_LOGIN_WINDOW", &cfg.Console.LoginWindow},
		{"CONSOLE_REMOTE_LEASE", &cfg.Console.RemoteLease},
		{"CONSOLE_REMOTE_HEARTBEAT", &cfg.Console.RemoteHeartbeat},
	}
	if value, ok := lookup(envPrefix + "STF_ENABLED"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse %sSTF_ENABLED: %w", envPrefix, err)
		}
		cfg.STF.Enabled = parsed
	}
	if value, ok := lookup(envPrefix + "IOS_REMOTE_CONTROL_ENABLED"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse %sIOS_REMOTE_CONTROL_ENABLED: %w", envPrefix, err)
		}
		cfg.IOSRemote.Enabled = parsed
	}
	if value, ok := lookup(envPrefix + "CONSOLE_ENABLED"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse %sCONSOLE_ENABLED: %w", envPrefix, err)
		}
		cfg.Console.Enabled = parsed
	}
	if value, ok := lookup(envPrefix + "CONSOLE_DEVELOPMENT_INSECURE"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse %sCONSOLE_DEVELOPMENT_INSECURE: %w", envPrefix, err)
		}
		cfg.Console.DevelopmentInsecure = parsed
	}
	if value, ok := lookup(envPrefix + "CONSOLE_LOGIN_MAX_FAILURES"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("parse %sCONSOLE_LOGIN_MAX_FAILURES: %w", envPrefix, err)
		}
		cfg.Console.LoginMaxFailures = parsed
	}
	if value, ok := lookup(envPrefix + "STF_ATTEMPTS"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("parse %sSTF_ATTEMPTS: %w", envPrefix, err)
		}
		cfg.STF.Attempts = parsed
	}
	if value, ok := lookup(envPrefix + "RECONCILE_FAILURE_THRESHOLD"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("parse %sRECONCILE_FAILURE_THRESHOLD: %w", envPrefix, err)
		}
		cfg.Reconcile.FailureThreshold = parsed
	}

	for _, item := range durations {
		value, ok := lookup(envPrefix + item.name)
		if !ok {
			continue
		}
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("parse %s%s: %w", envPrefix, item.name, err)
		}
		*item.target = parsed
	}

	return nil
}

func (cfg Config) Validate() error {
	var validationErrors []error

	if err := validateAddress(cfg.Server.Address); err != nil {
		validationErrors = append(validationErrors, err)
	}
	for name, value := range map[string]time.Duration{
		"server.read_timeout":                  cfg.Server.ReadTimeout,
		"server.write_timeout":                 cfg.Server.WriteTimeout,
		"server.idle_timeout":                  cfg.Server.IdleTimeout,
		"server.shutdown_timeout":              cfg.Server.ShutdownTimeout,
		"lease.scheduler_interval":             cfg.Lease.SchedulerInterval,
		"lease.reaper_interval":                cfg.Lease.ReaperInterval,
		"reconcile.interval":                   cfg.Reconcile.Interval,
		"reconcile.host_timeout":               cfg.Reconcile.HostTimeout,
		"reconcile.host_recovery_grace":        cfg.Reconcile.HostRecoveryGrace,
		"reconcile.stf_visibility_grace":       cfg.Reconcile.STFVisibilityGrace,
		"warm_pool.interval":                   cfg.WarmPool.Interval,
		"stf.timeout":                          cfg.STF.Timeout,
		"stf.retry_delay":                      cfg.STF.RetryDelay,
		"stf.web_token_ttl":                    cfg.STF.WebTokenTTL,
		"ios_remote_control.gateway_token_ttl": cfg.IOSRemote.GatewayTokenTTL,
		"console.session_max_age":              cfg.Console.SessionMaxAge,
		"console.session_idle_timeout":         cfg.Console.SessionIdleTimeout,
		"console.cleanup_interval":             cfg.Console.CleanupInterval,
		"console.login_window":                 cfg.Console.LoginWindow,
		"console.remote_lease":                 cfg.Console.RemoteLease,
		"console.remote_heartbeat":             cfg.Console.RemoteHeartbeat,
	} {
		if value <= 0 {
			validationErrors = append(validationErrors, fmt.Errorf("%s must be greater than zero", name))
		}
	}
	if cfg.Lease.GracePeriod < 0 {
		validationErrors = append(validationErrors, errors.New("lease.grace_period must not be negative"))
	}
	if cfg.Reconcile.FailureThreshold < 1 {
		validationErrors = append(validationErrors, errors.New("reconcile.failure_threshold must be greater than zero"))
	}
	if cfg.STF.Attempts < 1 || cfg.STF.Attempts > 5 {
		validationErrors = append(validationErrors, errors.New("stf.attempts must be between 1 and 5"))
	}
	if cfg.STF.Enabled {
		if strings.TrimSpace(cfg.STF.BaseURL) == "" {
			validationErrors = append(validationErrors, errors.New("stf.base_url is required when STF is enabled"))
		} else if err := validateSTFBaseURL(cfg.STF.BaseURL); err != nil {
			validationErrors = append(validationErrors, err)
		}
		if strings.TrimSpace(cfg.STF.APIToken) == "" {
			validationErrors = append(validationErrors, errors.New("stf.api_token is required when STF is enabled"))
		}
		webValues := []string{cfg.STF.WebURL, cfg.STF.WebAuthSecret, cfg.STF.WebUserName, cfg.STF.WebUserEmail}
		configured := 0
		for _, value := range webValues {
			if strings.TrimSpace(value) != "" {
				configured++
			}
		}
		if configured != 0 && configured != len(webValues) {
			validationErrors = append(validationErrors, errors.New("stf.web_url, web_auth_secret, web_user_name and web_user_email must be configured together"))
		}
		if strings.TrimSpace(cfg.STF.WebURL) != "" {
			if err := validateHTTPURL("stf.web_url", cfg.STF.WebURL); err != nil {
				validationErrors = append(validationErrors, err)
			}
			if !strings.Contains(cfg.STF.WebUserEmail, "@") {
				validationErrors = append(validationErrors, errors.New("stf.web_user_email must be an email address"))
			}
			if len(cfg.STF.WebAuthSecret) < 32 {
				validationErrors = append(validationErrors, errors.New("stf.web_auth_secret must contain at least 32 bytes"))
			}
			if cfg.STF.WebTokenTTL > time.Minute {
				validationErrors = append(validationErrors, errors.New("stf.web_token_ttl must not exceed 1 minute"))
			}
		}
	}
	if cfg.IOSRemote.Enabled {
		if err := validateLoopbackHTTPURL("ios_remote_control.baguette_url", cfg.IOSRemote.BaguetteURL); err != nil {
			validationErrors = append(validationErrors, err)
		}
		if err := validateNamedAddress("ios_remote_control.gateway_address", cfg.IOSRemote.GatewayAddress); err != nil {
			validationErrors = append(validationErrors, err)
		}
		if err := validateRootHTTPURL("ios_remote_control.public_url", cfg.IOSRemote.PublicURL); err != nil {
			validationErrors = append(validationErrors, err)
		}
		if strings.TrimSpace(cfg.IOSRemote.GatewayAddress) == strings.TrimSpace(cfg.Server.Address) {
			validationErrors = append(validationErrors, errors.New("ios_remote_control.gateway_address 不能与 server.address 相同"))
		}
		if len(cfg.IOSRemote.GatewaySecret) < 32 {
			validationErrors = append(validationErrors, errors.New("ios_remote_control.gateway_secret 必须至少包含 32 字节"))
		}
		if cfg.IOSRemote.GatewayTokenTTL > time.Minute {
			validationErrors = append(validationErrors, errors.New("ios_remote_control.gateway_token_ttl 不得超过 1 分钟"))
		}
	}
	if cfg.Console.Enabled {
		if strings.TrimSpace(cfg.Console.UsersFile) == "" {
			validationErrors = append(validationErrors, errors.New("console.users_file is required when Console is enabled"))
		}
		if strings.TrimSpace(cfg.Database.URL) == "" {
			validationErrors = append(validationErrors, errors.New("database.url is required when Console is enabled"))
		}
		if cfg.Console.LoginMaxFailures < 1 {
			validationErrors = append(validationErrors, errors.New("console.login_max_failures must be greater than zero"))
		}
		if cfg.Console.RemoteLease < time.Minute {
			validationErrors = append(validationErrors, errors.New("console.remote_lease must be at least 1 minute"))
		}
		if cfg.Console.RemoteHeartbeat <= 0 || cfg.Console.RemoteHeartbeat >= cfg.Console.RemoteLease {
			validationErrors = append(validationErrors, errors.New("console.remote_heartbeat must be greater than zero and shorter than remote_lease"))
		}
		if cfg.Console.DevelopmentInsecure && !isLoopbackAddress(cfg.Server.Address) {
			validationErrors = append(validationErrors, errors.New("console.development_insecure requires a loopback server.address"))
		}
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Log.Level)) {
	case "debug", "info", "warn", "error":
	default:
		validationErrors = append(validationErrors, errors.New("log.level must be one of debug, info, warn, error"))
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Log.Format)) {
	case "json", "text":
	default:
		validationErrors = append(validationErrors, errors.New("log.format must be one of json, text"))
	}

	tokens := map[string]string{
		"security.service_token": cfg.Security.ServiceToken, "security.service_previous_token": cfg.Security.ServicePreviousToken,
		"security.agent_token": cfg.Security.AgentToken, "security.agent_previous_token": cfg.Security.AgentPreviousToken,
	}
	seenTokens := map[string]string{}
	for name, token := range tokens {
		if token == "" {
			continue
		}
		if previous, exists := seenTokens[token]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("%s and %s must be different", previous, name))
			continue
		}
		seenTokens[token] = name
	}
	if cfg.Security.ServicePreviousToken != "" && cfg.Security.ServiceToken == "" {
		validationErrors = append(validationErrors, errors.New("security.service_previous_token requires security.service_token"))
	}
	if cfg.Security.AgentPreviousToken != "" && cfg.Security.AgentToken == "" {
		validationErrors = append(validationErrors, errors.New("security.agent_previous_token requires security.agent_token"))
	}

	if err := errors.Join(validationErrors...); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	return nil
}

func validateSTFBaseURL(value string) error {
	return validateHTTPURL("stf.base_url", value)
}

func validateHTTPURL(name, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL without user info, query or fragment", name)
	}
	return nil
}

func validateLoopbackHTTPURL(name, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s 必须是无用户信息、查询参数和片段的 HTTP(S) 绝对地址", name)
	}
	host := strings.TrimSpace(parsed.Hostname())
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("%s 必须使用回环主机地址", name)
	}
	return nil
}

func validateRootHTTPURL(name, value string) error {
	if err := validateHTTPURL(name, value); err != nil {
		return err
	}
	parsed, _ := url.Parse(strings.TrimSpace(value))
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("%s 必须使用独立站点根地址", name)
	}
	return nil
}

func validateAddress(address string) error {
	return validateNamedAddress("server.address", address)
}

func validateNamedAddress(name, address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("%s 必须使用 host:port 格式：%w", name, err)
	}
	if host == "" {
		return fmt.Errorf("%s 的主机不能为空", name)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("%s 的端口必须在 1 到 65535 之间", name)
	}
	return nil
}

// LogValue ensures that slog never reflects secret fields when a Config is
// logged with slog.Any.
func (cfg Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("server_address", cfg.Server.Address),
		slog.Duration("server_read_timeout", cfg.Server.ReadTimeout),
		slog.Duration("server_write_timeout", cfg.Server.WriteTimeout),
		slog.Duration("server_idle_timeout", cfg.Server.IdleTimeout),
		slog.Duration("server_shutdown_timeout", cfg.Server.ShutdownTimeout),
		slog.String("log_level", cfg.Log.Level),
		slog.String("log_format", cfg.Log.Format),
		slog.Duration("lease_scheduler_interval", cfg.Lease.SchedulerInterval),
		slog.Duration("lease_reaper_interval", cfg.Lease.ReaperInterval),
		slog.Duration("lease_grace_period", cfg.Lease.GracePeriod),
		slog.Duration("reconcile_interval", cfg.Reconcile.Interval),
		slog.Duration("reconcile_host_timeout", cfg.Reconcile.HostTimeout),
		slog.Duration("reconcile_host_recovery_grace", cfg.Reconcile.HostRecoveryGrace),
		slog.Duration("reconcile_stf_visibility_grace", cfg.Reconcile.STFVisibilityGrace),
		slog.Int("reconcile_failure_threshold", cfg.Reconcile.FailureThreshold),
		slog.Duration("warm_pool_interval", cfg.WarmPool.Interval),
		slog.Bool("stf_enabled", cfg.STF.Enabled),
		slog.String("stf_base_url", cfg.STF.BaseURL),
		slog.Duration("stf_timeout", cfg.STF.Timeout),
		slog.Int("stf_attempts", cfg.STF.Attempts),
		slog.Duration("stf_retry_delay", cfg.STF.RetryDelay),
		slog.String("stf_web_url", cfg.STF.WebURL),
		slog.Duration("stf_web_token_ttl", cfg.STF.WebTokenTTL),
		slog.Bool("ios_remote_control_enabled", cfg.IOSRemote.Enabled),
		slog.String("ios_remote_control_baguette_url", cfg.IOSRemote.BaguetteURL),
		slog.String("ios_remote_control_gateway_address", cfg.IOSRemote.GatewayAddress),
		slog.String("ios_remote_control_public_url", cfg.IOSRemote.PublicURL),
		slog.Duration("ios_remote_control_gateway_token_ttl", cfg.IOSRemote.GatewayTokenTTL),
		slog.Bool("console_enabled", cfg.Console.Enabled),
		slog.Bool("console_development_insecure", cfg.Console.DevelopmentInsecure),
		slog.Duration("console_session_max_age", cfg.Console.SessionMaxAge),
		slog.Duration("console_session_idle_timeout", cfg.Console.SessionIdleTimeout),
		slog.Duration("console_cleanup_interval", cfg.Console.CleanupInterval),
		slog.Duration("console_login_window", cfg.Console.LoginWindow),
		slog.Int("console_login_max_failures", cfg.Console.LoginMaxFailures),
		slog.Duration("console_remote_lease", cfg.Console.RemoteLease),
		slog.Duration("console_remote_heartbeat", cfg.Console.RemoteHeartbeat),
	)
}

func (cfg STFConfig) WebConfigured() bool {
	return strings.TrimSpace(cfg.WebURL) != "" && strings.TrimSpace(cfg.WebAuthSecret) != "" &&
		strings.TrimSpace(cfg.WebUserName) != "" && strings.TrimSpace(cfg.WebUserEmail) != ""
}

func (cfg IOSRemoteConfig) Configured() bool {
	return cfg.Enabled && strings.TrimSpace(cfg.BaguetteURL) != "" && strings.TrimSpace(cfg.GatewayAddress) != "" &&
		strings.TrimSpace(cfg.PublicURL) != "" && strings.TrimSpace(cfg.GatewaySecret) != ""
}

func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

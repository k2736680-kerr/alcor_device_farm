package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	ServiceToken string `yaml:"service_token" json:"-"`
	AgentToken   string `yaml:"agent_token" json:"-"`
}

type LeaseConfig struct {
	SchedulerInterval time.Duration `yaml:"scheduler_interval" json:"scheduler_interval"`
	ReaperInterval    time.Duration `yaml:"reaper_interval" json:"reaper_interval"`
	GracePeriod       time.Duration `yaml:"grace_period" json:"grace_period"`
}

type ReconcileConfig struct {
	Interval         time.Duration `yaml:"interval" json:"interval"`
	HostTimeout      time.Duration `yaml:"host_timeout" json:"host_timeout"`
	FailureThreshold int           `yaml:"failure_threshold" json:"failure_threshold"`
}

type WarmPoolConfig struct {
	Interval time.Duration `yaml:"interval" json:"interval"`
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
		Reconcile: ReconcileConfig{Interval: 2 * time.Second, HostTimeout: 30 * time.Second, FailureThreshold: 3},
		WarmPool:  WarmPoolConfig{Interval: 30 * time.Second},
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
		{"SECURITY_AGENT_TOKEN", &cfg.Security.AgentToken},
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
		{"WARM_POOL_INTERVAL", &cfg.WarmPool.Interval},
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
		"server.read_timeout":      cfg.Server.ReadTimeout,
		"server.write_timeout":     cfg.Server.WriteTimeout,
		"server.idle_timeout":      cfg.Server.IdleTimeout,
		"server.shutdown_timeout":  cfg.Server.ShutdownTimeout,
		"lease.scheduler_interval": cfg.Lease.SchedulerInterval,
		"lease.reaper_interval":    cfg.Lease.ReaperInterval,
		"reconcile.interval":       cfg.Reconcile.Interval,
		"reconcile.host_timeout":   cfg.Reconcile.HostTimeout,
		"warm_pool.interval":       cfg.WarmPool.Interval,
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

	if cfg.Security.ServiceToken != "" && cfg.Security.ServiceToken == cfg.Security.AgentToken {
		validationErrors = append(validationErrors, errors.New("security.service_token and security.agent_token must be different"))
	}

	if err := errors.Join(validationErrors...); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	return nil
}

func validateAddress(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("server.address must use host:port: %w", err)
	}
	if host == "" {
		return errors.New("server.address host must not be empty")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("server.address port must be between 1 and 65535")
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
		slog.Int("reconcile_failure_threshold", cfg.Reconcile.FailureThreshold),
		slog.Duration("warm_pool_interval", cfg.WarmPool.Interval),
	)
}

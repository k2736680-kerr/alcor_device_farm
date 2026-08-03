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
	Server   ServerConfig   `yaml:"server" json:"server"`
	Log      LogConfig      `yaml:"log" json:"log"`
	Security SecurityConfig `yaml:"security" json:"-"`
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
		"server.read_timeout":     cfg.Server.ReadTimeout,
		"server.write_timeout":    cfg.Server.WriteTimeout,
		"server.idle_timeout":     cfg.Server.IdleTimeout,
		"server.shutdown_timeout": cfg.Server.ShutdownTimeout,
	} {
		if value <= 0 {
			validationErrors = append(validationErrors, fmt.Errorf("%s must be greater than zero", name))
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
	)
}

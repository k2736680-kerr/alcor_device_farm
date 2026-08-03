package config

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadUsesDefaults(t *testing.T) {
	clearDeviceFarmEnvironment(t)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Address != "127.0.0.1:8080" {
		t.Fatalf("Address = %q", cfg.Server.Address)
	}
	if cfg.Log.Format != "json" {
		t.Fatalf("Log format = %q", cfg.Log.Format)
	}
}

func TestLoadYAMLAndEnvironmentOverride(t *testing.T) {
	clearDeviceFarmEnvironment(t)
	path := writeConfig(t, `
server:
  address: "127.0.0.1:18080"
  read_timeout: 3s
  write_timeout: 4s
  idle_timeout: 5s
  shutdown_timeout: 6s
log:
  level: debug
  format: text
security:
  service_token: yaml-secret
  agent_token: yaml-agent-secret
`)
	t.Setenv("DEVICE_FARM_SERVER_ADDRESS", "127.0.0.1:28080")
	t.Setenv("DEVICE_FARM_SERVER_READ_TIMEOUT", "7s")
	t.Setenv("DEVICE_FARM_SECURITY_SERVICE_TOKEN", "environment-secret")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Address != "127.0.0.1:28080" {
		t.Fatalf("Address = %q", cfg.Server.Address)
	}
	if cfg.Server.ReadTimeout != 7*time.Second {
		t.Fatalf("ReadTimeout = %v", cfg.Server.ReadTimeout)
	}
	if cfg.Security.ServiceToken != "environment-secret" {
		t.Fatal("environment did not override service token")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	clearDeviceFarmEnvironment(t)
	path := writeConfig(t, "server:\n  adress: 127.0.0.1:8080\n")

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "field adress not found") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestValidateRejectsInvalidValues(t *testing.T) {
	cfg := Default()
	cfg.Server.Address = ":0"
	cfg.Server.ReadTimeout = 0
	cfg.Log.Level = "verbose"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil")
	}
	for _, want := range []string{"host must not be empty", "read_timeout", "log.level"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Validate() error = %q, want %q", err, want)
		}
	}
}

func TestValidateRejectsAmbiguousSecurityTokens(t *testing.T) {
	cfg := Default()
	cfg.Security.ServiceToken = "same-token"
	cfg.Security.AgentToken = "same-token"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "must be different") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSecretsAreExcludedFromJSONAndSlogValue(t *testing.T) {
	cfg := Default()
	cfg.Security.ServiceToken = "service-token-value"
	cfg.Security.AgentToken = "agent-token-value"

	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	assertNoSecrets(t, string(encoded))

	value := cfg.LogValue()
	if value.Kind() != slog.KindGroup {
		t.Fatalf("LogValue() kind = %v", value.Kind())
	}
	var values []string
	for _, attr := range value.Group() {
		values = append(values, attr.String())
	}
	assertNoSecrets(t, strings.Join(values, " "))
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func clearDeviceFarmEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range os.Environ() {
		key, _, ok := strings.Cut(name, "=")
		if ok && strings.HasPrefix(key, envPrefix) {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatalf("Unsetenv(%q) error = %v", key, err)
			}
		}
	}
}

func assertNoSecrets(t *testing.T, value string) {
	t.Helper()
	for _, secret := range []string{"service-token-value", "agent-token-value"} {
		if strings.Contains(value, secret) {
			t.Fatalf("serialized value contains secret %q", secret)
		}
	}
}

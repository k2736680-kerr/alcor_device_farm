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
	if cfg.Lease.GracePeriod != 30*time.Second || cfg.Lease.ReaperInterval != time.Second {
		t.Fatalf("lease defaults = %+v", cfg.Lease)
	}
	if cfg.Reconcile.FailureThreshold != 3 || cfg.Reconcile.HostTimeout != 30*time.Second {
		t.Fatalf("reconcile defaults = %+v", cfg.Reconcile)
	}
	if cfg.WarmPool.Interval != 30*time.Second {
		t.Fatalf("warm pool defaults = %+v", cfg.WarmPool)
	}
	if cfg.STF.Enabled || cfg.STF.Attempts != 3 || cfg.STF.Timeout != 5*time.Second {
		t.Fatalf("STF defaults = %+v", cfg.STF)
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
database:
  url: postgres://yaml-database-secret
stf:
  enabled: true
  base_url: http://stf-yaml.local/stf
  api_token: yaml-stf-secret
  timeout: 8s
  attempts: 2
  retry_delay: 300ms
`)
	t.Setenv("DEVICE_FARM_SERVER_ADDRESS", "127.0.0.1:28080")
	t.Setenv("DEVICE_FARM_SERVER_READ_TIMEOUT", "7s")
	t.Setenv("DEVICE_FARM_SECURITY_SERVICE_TOKEN", "environment-secret")
	t.Setenv("DEVICE_FARM_DATABASE_URL", "postgres://environment-database-secret")
	t.Setenv("DEVICE_FARM_LEASE_GRACE_PERIOD", "45s")
	t.Setenv("DEVICE_FARM_RECONCILE_FAILURE_THRESHOLD", "5")
	t.Setenv("DEVICE_FARM_WARM_POOL_INTERVAL", "12s")
	t.Setenv("DEVICE_FARM_STF_BASE_URL", "http://stf-environment.local/base")
	t.Setenv("DEVICE_FARM_STF_API_TOKEN", "environment-stf-secret")
	t.Setenv("DEVICE_FARM_STF_ATTEMPTS", "4")

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
	if cfg.Database.URL != "postgres://environment-database-secret" {
		t.Fatal("environment did not override database URL")
	}
	if cfg.Lease.GracePeriod != 45*time.Second {
		t.Fatalf("GracePeriod = %v", cfg.Lease.GracePeriod)
	}
	if cfg.Reconcile.FailureThreshold != 5 {
		t.Fatalf("FailureThreshold = %d", cfg.Reconcile.FailureThreshold)
	}
	if cfg.WarmPool.Interval != 12*time.Second {
		t.Fatalf("WarmPool interval = %v", cfg.WarmPool.Interval)
	}
	if !cfg.STF.Enabled || cfg.STF.BaseURL != "http://stf-environment.local/base" ||
		cfg.STF.APIToken != "environment-stf-secret" || cfg.STF.Attempts != 4 {
		t.Fatalf("STF config = %+v", cfg.STF)
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
	cfg.WarmPool.Interval = 0
	cfg.Log.Level = "verbose"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil")
	}
	for _, want := range []string{"host must not be empty", "read_timeout", "warm_pool.interval", "log.level"} {
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

func TestValidateRequiresSTFEndpointAndTokenWhenEnabled(t *testing.T) {
	cfg := Default()
	cfg.STF.Enabled = true
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "stf.base_url") || !strings.Contains(err.Error(), "stf.api_token") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsSTFBaseURLWithCredentialsOrQuery(t *testing.T) {
	for _, value := range []string{"ftp://stf.internal", "http://user:secret@stf.internal", "http://stf.internal?token=secret"} {
		cfg := Default()
		cfg.STF.Enabled = true
		cfg.STF.BaseURL = value
		cfg.STF.APIToken = "secret"
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "stf.base_url") {
			t.Fatalf("Validate(%q) error=%v", value, err)
		}
	}
}

func TestSecretsAreExcludedFromJSONAndSlogValue(t *testing.T) {
	cfg := Default()
	cfg.Security.ServiceToken = "service-token-value"
	cfg.Security.AgentToken = "agent-token-value"
	cfg.Database.URL = "postgres://database-secret-value"
	cfg.STF.APIToken = "stf-token-value"

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
	for _, secret := range []string{"service-token-value", "agent-token-value", "database-secret-value", "stf-token-value"} {
		if strings.Contains(value, secret) {
			t.Fatalf("serialized value contains secret %q", secret)
		}
	}
}

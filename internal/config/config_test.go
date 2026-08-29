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
	if cfg.Reconcile.FailureThreshold != 3 || cfg.Reconcile.HostTimeout != 30*time.Second ||
		cfg.Reconcile.STFVisibilityGrace != 30*time.Second || cfg.Reconcile.HostRecoveryGrace != 90*time.Second {
		t.Fatalf("reconcile defaults = %+v", cfg.Reconcile)
	}
	if cfg.WarmPool.Interval != 30*time.Second {
		t.Fatalf("warm pool defaults = %+v", cfg.WarmPool)
	}
	if cfg.STF.Enabled || cfg.STF.Attempts != 3 || cfg.STF.Timeout != 5*time.Second {
		t.Fatalf("STF defaults = %+v", cfg.STF)
	}
	if cfg.Console.SessionMaxAge != 30*24*time.Hour || cfg.Console.SessionIdleTimeout != 30*24*time.Hour {
		t.Fatalf("Console session defaults = %+v", cfg.Console)
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
  service_previous_token: yaml-previous-secret
  agent_token: yaml-agent-secret
  agent_previous_token: yaml-agent-previous-secret
database:
  url: postgres://yaml-database-secret
stf:
  enabled: true
  base_url: http://stf-yaml.local/stf
  api_token: yaml-stf-secret
  timeout: 8s
  attempts: 2
  retry_delay: 300ms
  web_url: http://stf-web-yaml.local
  web_auth_secret: yaml-stf-web-secret-at-least-32-bytes
  web_user_name: Device Farm Admin
  web_user_email: admin@example.test
  web_token_ttl: 25s
ios_remote_control:
  enabled: true
  baguette_url: http://127.0.0.1:8421
  gateway_address: 0.0.0.0:8081
  public_url: http://device-farm.example.test:18081
  gateway_secret: yaml-ios-gateway-secret-at-least-32-bytes
  gateway_token_ttl: 25s
`)
	t.Setenv("DEVICE_FARM_SERVER_ADDRESS", "127.0.0.1:28080")
	t.Setenv("DEVICE_FARM_SERVER_READ_TIMEOUT", "7s")
	t.Setenv("DEVICE_FARM_SECURITY_SERVICE_TOKEN", "environment-secret")
	t.Setenv("DEVICE_FARM_SECURITY_SERVICE_PREVIOUS_TOKEN", "environment-previous-secret")
	t.Setenv("DEVICE_FARM_SECURITY_AGENT_PREVIOUS_TOKEN", "environment-agent-previous-secret")
	t.Setenv("DEVICE_FARM_DATABASE_URL", "postgres://environment-database-secret")
	t.Setenv("DEVICE_FARM_LEASE_GRACE_PERIOD", "45s")
	t.Setenv("DEVICE_FARM_RECONCILE_FAILURE_THRESHOLD", "5")
	t.Setenv("DEVICE_FARM_RECONCILE_STF_VISIBILITY_GRACE", "40s")
	t.Setenv("DEVICE_FARM_RECONCILE_HOST_RECOVERY_GRACE", "80s")
	t.Setenv("DEVICE_FARM_WARM_POOL_INTERVAL", "12s")
	t.Setenv("DEVICE_FARM_STF_BASE_URL", "http://stf-environment.local/base")
	t.Setenv("DEVICE_FARM_STF_API_TOKEN", "environment-stf-secret")
	t.Setenv("DEVICE_FARM_STF_WEB_URL", "http://stf-web-environment.local")
	t.Setenv("DEVICE_FARM_STF_WEB_AUTH_SECRET", "environment-stf-web-secret-32-bytes")
	t.Setenv("DEVICE_FARM_STF_WEB_USER_NAME", "Environment Admin")
	t.Setenv("DEVICE_FARM_STF_WEB_USER_EMAIL", "environment-admin@example.test")
	t.Setenv("DEVICE_FARM_STF_ATTEMPTS", "4")
	t.Setenv("DEVICE_FARM_IOS_REMOTE_CONTROL_ENABLED", "true")
	t.Setenv("DEVICE_FARM_IOS_REMOTE_CONTROL_BAGUETTE_URL", "http://127.0.0.1:4842")
	t.Setenv("DEVICE_FARM_IOS_REMOTE_CONTROL_GATEWAY_ADDRESS", "127.0.0.1:28081")
	t.Setenv("DEVICE_FARM_IOS_REMOTE_CONTROL_PUBLIC_URL", "http://gateway.example.test:28081")
	t.Setenv("DEVICE_FARM_IOS_REMOTE_CONTROL_GATEWAY_SECRET", "environment-ios-gateway-secret-at-least-32-bytes")
	t.Setenv("DEVICE_FARM_IOS_REMOTE_CONTROL_GATEWAY_TOKEN_TTL", "20s")

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
	if cfg.Security.ServiceToken != "environment-secret" || cfg.Security.ServicePreviousToken != "environment-previous-secret" ||
		cfg.Security.AgentPreviousToken != "environment-agent-previous-secret" {
		t.Fatalf("environment did not override rotation tokens: %+v", cfg.Security)
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
	if cfg.Reconcile.STFVisibilityGrace != 40*time.Second {
		t.Fatalf("STFVisibilityGrace = %v", cfg.Reconcile.STFVisibilityGrace)
	}
	if cfg.Reconcile.HostRecoveryGrace != 80*time.Second {
		t.Fatalf("HostRecoveryGrace = %v", cfg.Reconcile.HostRecoveryGrace)
	}
	if cfg.WarmPool.Interval != 12*time.Second {
		t.Fatalf("WarmPool interval = %v", cfg.WarmPool.Interval)
	}
	if !cfg.STF.Enabled || cfg.STF.BaseURL != "http://stf-environment.local/base" ||
		cfg.STF.APIToken != "environment-stf-secret" || cfg.STF.Attempts != 4 ||
		cfg.STF.WebURL != "http://stf-web-environment.local" || cfg.STF.WebUserEmail != "environment-admin@example.test" {
		t.Fatalf("STF config = %+v", cfg.STF)
	}
	if !cfg.IOSRemote.Enabled || cfg.IOSRemote.GatewayTokenTTL != 20*time.Second ||
		cfg.IOSRemote.BaguetteURL != "http://127.0.0.1:4842" || cfg.IOSRemote.GatewayAddress != "127.0.0.1:28081" ||
		cfg.IOSRemote.PublicURL != "http://gateway.example.test:28081" {
		t.Fatalf("iOS remote config = %+v", cfg.IOSRemote)
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
	for _, want := range []string{"主机不能为空", "read_timeout", "warm_pool.interval", "log.level"} {
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

func TestValidateRejectsPreviousTokenWithoutCurrentToken(t *testing.T) {
	cfg := Default()
	cfg.Security.AgentPreviousToken = "retired-agent-token"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "requires security.agent_token") {
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

func TestValidateRequiresIOSRemoteGatewaySecret(t *testing.T) {
	cfg := Default()
	cfg.IOSRemote.Enabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "ios_remote_control.gateway_secret") {
		t.Fatalf("Validate() error=%v", err)
	}
}

func TestValidateRequiresDedicatedIOSGatewayAddress(t *testing.T) {
	cfg := Default()
	cfg.IOSRemote = IOSRemoteConfig{Enabled: true, BaguetteURL: "http://127.0.0.1:8421",
		GatewayAddress: cfg.Server.Address, PublicURL: "http://gateway.example.test:18081",
		GatewaySecret: "ios-gateway-secret-at-least-32-bytes", GatewayTokenTTL: 30 * time.Second}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "不能与 server.address 相同") {
		t.Fatalf("Validate() error=%v", err)
	}
}

func TestSecretsAreExcludedFromJSONAndSlogValue(t *testing.T) {
	cfg := Default()
	cfg.Security.ServiceToken = "service-token-value"
	cfg.Security.ServicePreviousToken = "service-previous-token-value"
	cfg.Security.AgentToken = "agent-token-value"
	cfg.Security.AgentPreviousToken = "agent-previous-token-value"
	cfg.Database.URL = "postgres://database-secret-value"
	cfg.STF.APIToken = "stf-token-value"
	cfg.STF.WebAuthSecret = "stf-web-secret-value"
	cfg.IOSRemote.GatewaySecret = "ios-gateway-secret-value"

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
	for _, secret := range []string{"service-token-value", "service-previous-token-value", "agent-token-value", "agent-previous-token-value", "database-secret-value", "stf-token-value", "stf-web-secret-value", "ios-gateway-secret-value"} {
		if strings.Contains(value, secret) {
			t.Fatalf("serialized value contains secret %q", secret)
		}
	}
}

package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
)

func TestLoggerRedactsSensitiveAttributes(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(config.LogConfig{Level: "debug", Format: "json"}, &output)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	logger.Info(
		"request",
		"authorization", "Bearer top-secret",
		"database_dsn", "postgres://secret",
		"safe", "visible",
		slog.Group("nested", slog.String("agent_token", "agent-secret")),
	)

	got := output.String()
	for _, secret := range []string{"top-secret", "postgres://secret", "agent-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("log contains secret %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, redactedValue) || !strings.Contains(got, "visible") {
		t.Fatalf("log did not preserve safe value and redaction marker: %s", got)
	}
}

func TestLoggerUsesConfigLogValueWithoutSecrets(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(config.LogConfig{Level: "info", Format: "json"}, &output)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	cfg := config.Default()
	cfg.Security.ServiceToken = "service-secret"
	cfg.Security.AgentToken = "agent-secret"
	logger.Info("config loaded", "config", cfg)

	got := output.String()
	if strings.Contains(got, "service-secret") || strings.Contains(got, "agent-secret") {
		t.Fatalf("config log contains a secret: %s", got)
	}
	if !strings.Contains(got, "server_address") {
		t.Fatalf("config log does not contain safe fields: %s", got)
	}
}

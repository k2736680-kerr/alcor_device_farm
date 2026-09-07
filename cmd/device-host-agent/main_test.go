package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	providerdocker "github.com/Ad-Quanta/alcor-device-farm/internal/providers/docker"
)

type componentRunnerFunc func(context.Context) error

func (run componentRunnerFunc) Run(ctx context.Context) error { return run(ctx) }

func TestRunComponentsTreatsMissingIOSFenceAsNil(t *testing.T) {
	want := errors.New("runtime stopped")
	err := runComponents(context.Background(), componentRunnerFunc(func(context.Context) error { return want }), nil)
	if !errors.Is(err, want) {
		t.Fatalf("runComponents() error=%v, want %v", err, want)
	}
}

func TestAgentConcurrencyDefaultsToSingleSlotAndSupportsConfiguration(t *testing.T) {
	t.Setenv("DEVICE_FARM_AGENT_CONCURRENCY", "")
	if got := envInt("DEVICE_FARM_AGENT_CONCURRENCY", 1); got != 1 {
		t.Fatalf("default concurrency=%d, want 1", got)
	}
	t.Setenv("DEVICE_FARM_AGENT_CONCURRENCY", "3")
	if got := envInt("DEVICE_FARM_AGENT_CONCURRENCY", 1); got != 3 {
		t.Fatalf("configured concurrency=%d, want 3", got)
	}
}

func TestAgentLeaseAndCommandTimeoutSupportEnvironmentConfiguration(t *testing.T) {
	t.Setenv("DEVICE_FARM_AGENT_LEASE_SECONDS", "120")
	if got := envInt("DEVICE_FARM_AGENT_LEASE_SECONDS", 300); got != 120 {
		t.Fatalf("configured lease seconds=%d, want 120", got)
	}
	t.Setenv("DEVICE_FARM_AGENT_COMMAND_TIMEOUT", "30s")
	if got := envDuration("DEVICE_FARM_AGENT_COMMAND_TIMEOUT", 270*time.Second); got != 30*time.Second {
		t.Fatalf("configured command timeout=%v, want 30s", got)
	}
	t.Setenv("DEVICE_FARM_AGENT_COMMAND_TIMEOUT", "invalid")
	if got := envDuration("DEVICE_FARM_AGENT_COMMAND_TIMEOUT", 270*time.Second); got != 270*time.Second {
		t.Fatalf("invalid command timeout fallback=%v, want 270s", got)
	}
}

func TestBuildProviderRequiresExplicitProvider(t *testing.T) {
	provider, err := buildProvider(" ", providerdocker.Config{}, nil)
	if err == nil || provider != nil || !strings.Contains(err.Error(), "必须配置设备 Provider") {
		t.Fatalf("provider=%T error=%v", provider, err)
	}
}

func TestBuildProviderAcceptsExplicitMock(t *testing.T) {
	provider, err := buildProvider(" MOCK ", providerdocker.Config{}, nil)
	if err != nil || provider == nil {
		t.Fatalf("provider=%T error=%v", provider, err)
	}
}

func TestBuildProviderRejectsUnknownProvider(t *testing.T) {
	provider, err := buildProvider("unknown", providerdocker.Config{}, nil)
	if err == nil || provider != nil || !strings.Contains(err.Error(), "不支持的 Provider") {
		t.Fatalf("provider=%T error=%v", provider, err)
	}
}

func TestSplitCSVRemovesEmptyUDIDs(t *testing.T) {
	values := splitCSV(" SIM-1, ,SIM-2 ")
	if len(values) != 2 || values[0] != "SIM-1" || values[1] != "SIM-2" {
		t.Fatalf("values=%#v", values)
	}
}

func TestDockerEnvironmentEnablesAppiumAndDisablesBuiltInVNC(t *testing.T) {
	values := dockerEnvironment(" Pixel 9 ")
	if values["EMULATOR_DEVICE"] != "Pixel 9" || values["WEB_VNC"] != "false" || values["WEB_LOG"] != "false" || values["APPIUM"] != "true" || values["USER_BEHAVIOR_ANALYTICS"] != "false" {
		t.Fatalf("docker environment=%#v", values)
	}
	values = dockerEnvironment(" ")
	if _, exists := values["EMULATOR_DEVICE"]; exists {
		t.Fatalf("empty emulator device must not be injected: %#v", values)
	}
}

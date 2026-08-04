package main

import (
	"strings"
	"testing"

	providerdocker "github.com/Ad-Quanta/alcor-device-farm/internal/providers/docker"
)

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

func TestBuildProviderRequiresExplicitProvider(t *testing.T) {
	provider, err := buildProvider(" ", providerdocker.Config{})
	if err == nil || provider != nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("provider=%T error=%v", provider, err)
	}
}

func TestBuildProviderAcceptsExplicitMock(t *testing.T) {
	provider, err := buildProvider(" MOCK ", providerdocker.Config{})
	if err != nil || provider == nil {
		t.Fatalf("provider=%T error=%v", provider, err)
	}
}

func TestBuildProviderRejectsUnknownProvider(t *testing.T) {
	provider, err := buildProvider("unknown", providerdocker.Config{})
	if err == nil || provider != nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("provider=%T error=%v", provider, err)
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

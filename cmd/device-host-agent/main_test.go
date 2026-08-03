package main

import (
	"strings"
	"testing"

	providerdocker "github.com/Ad-Quanta/alcor-device-farm/internal/providers/docker"
)

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
	values := dockerEnvironment(" Samsung Galaxy S10 ")
	if values["EMULATOR_DEVICE"] != "Samsung Galaxy S10" || values["WEB_VNC"] != "false" || values["APPIUM"] != "true" {
		t.Fatalf("docker environment=%#v", values)
	}
	values = dockerEnvironment(" ")
	if _, exists := values["EMULATOR_DEVICE"]; exists {
		t.Fatalf("empty emulator device must not be injected: %#v", values)
	}
}

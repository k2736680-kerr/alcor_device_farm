package main

import "testing"

func TestDockerEnvironmentDisablesOutOfScopeServices(t *testing.T) {
	values := dockerEnvironment(" Samsung Galaxy S10 ")
	if values["EMULATOR_DEVICE"] != "Samsung Galaxy S10" || values["WEB_VNC"] != "false" || values["APPIUM"] != "false" {
		t.Fatalf("docker environment=%#v", values)
	}
	values = dockerEnvironment(" ")
	if _, exists := values["EMULATOR_DEVICE"]; exists {
		t.Fatalf("empty emulator device must not be injected: %#v", values)
	}
}

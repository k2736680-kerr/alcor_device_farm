package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSTFComposePinsImagesAndKeepsInfrastructurePrivate(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "stf", "compose.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(content)
	for _, required := range []string{
		"devicefarmer/stf:3.7.9", "rethinkdb:2.4.2", "--adb-host", "stf-adb",
		"devicefarmer/adb@sha256:a699fafbc63d8a145f816257b1cd366ea3c5f0aff657e3bb135309bf7da45759",
		"--allow-remote", "7400-7500", "STF_AUTH_SECRET", "internal: true",
	} {
		if !strings.Contains(raw, required) {
			t.Fatalf("STF compose is missing %q", required)
		}
	}
	for _, forbidden := range []string{"devicefarmer/stf:latest", "devicefarmer/adb:latest", "rethinkdb:latest", "/var/run/docker.sock", "STF_API_TOKEN", "privileged: true", "/dev/bus/usb"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("STF compose contains forbidden value %q", forbidden)
		}
	}

	var document struct {
		Services map[string]struct {
			Ports    []string `yaml:"ports"`
			Expose   []string `yaml:"expose"`
			Networks []string `yaml:"networks"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"rethinkdb", "stf-adb", "stf"} {
		if _, ok := document.Services[service]; !ok {
			t.Fatalf("missing STF service %q", service)
		}
	}
	if len(document.Services["rethinkdb"].Ports) != 0 || len(document.Services["stf-adb"].Ports) != 0 {
		t.Fatal("RethinkDB and ADB server must not publish host ports")
	}
	if strings.Join(document.Services["rethinkdb"].Networks, ",") != "stf-internal" ||
		!containsSTFNetwork(document.Services["stf-adb"].Networks, "stf-internal") ||
		!containsSTFNetwork(document.Services["stf-adb"].Networks, "stf-edge") {
		t.Fatalf("STF network boundary is invalid: rethinkdb=%v stf-adb=%v", document.Services["rethinkdb"].Networks, document.Services["stf-adb"].Networks)
	}
	if len(document.Services["stf"].Ports) != 3 {
		t.Fatalf("STF published ports=%v", document.Services["stf"].Ports)
	}
	for _, port := range document.Services["stf"].Ports {
		if !strings.Contains(port, "STF_BIND_ADDRESS:-127.0.0.1") {
			t.Fatalf("STF port is not private-by-default: %q", port)
		}
	}
}

func containsSTFNetwork(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestUSBOverlayLimitsPrivilegeToADBService(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "stf", "compose.usb.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(content)
	if !strings.Contains(raw, "stf-adb:") || !strings.Contains(raw, "/dev/bus/usb:/dev/bus/usb") || !strings.Contains(raw, "privileged: true") {
		t.Fatal("USB overlay does not grant the required ADB-only USB access")
	}
	if strings.Contains(raw, "rethinkdb:") || strings.Contains(raw, "\n  stf:\n") || strings.Contains(raw, "docker.sock") {
		t.Fatal("USB overlay broadens privileges beyond the ADB service")
	}
}

func TestSTFVerificationUsesSingleEmulatorAcceptanceProfile(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "verify-stf-deployment.sh")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(content)
	if !strings.Contains(raw, "at least one Emulator ADB endpoint") ||
		strings.Contains(raw, "at least two Emulator ADB endpoints") {
		t.Fatalf("STF verification is not aligned with ADR-0008:\n%s", raw)
	}
}

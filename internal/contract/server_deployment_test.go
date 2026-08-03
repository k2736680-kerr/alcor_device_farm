package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestServerComposeAndImageStayPrivateAndUnprivileged(t *testing.T) {
	composePath := filepath.Join("..", "..", "deploy", "server", "compose.yaml")
	content, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(content)
	for _, required := range []string{
		"DEVICE_FARM_BIND_ADDRESS:-127.0.0.1", "read_only: true", "no-new-privileges:true",
		"cap_drop:", "- ALL", "server.env", "/readyz",
	} {
		if !strings.Contains(raw, required) {
			t.Fatalf("Server compose is missing %q", required)
		}
	}
	for _, forbidden := range []string{"/var/run/docker.sock", "privileged: true", ":latest", "network_mode: host"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("Server compose contains forbidden value %q", forbidden)
		}
	}
	var document struct {
		Services map[string]struct {
			Ports []string `yaml:"ports"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	service, ok := document.Services["device-farm-server"]
	if !ok || len(service.Ports) != 1 || !strings.Contains(service.Ports[0], "127.0.0.1") {
		t.Fatalf("Server published ports=%v", service.Ports)
	}

	dockerfile, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	image := string(dockerfile)
	if !strings.Contains(image, "USER 65532:65532") || !strings.Contains(image, "CGO_ENABLED=0") ||
		!strings.Contains(image, "device-farm-server-entrypoint.sh") || strings.Contains(image, ":latest") {
		t.Fatalf("Dockerfile does not enforce the release runtime contract:\n%s", image)
	}
}

func TestServerSystemdAndScriptsFailClosed(t *testing.T) {
	unit, err := os.ReadFile(filepath.Join("..", "..", "deploy", "server", "alcor-device-farm-server.service"))
	if err != nil {
		t.Fatal(err)
	}
	rawUnit := string(unit)
	for _, required := range []string{
		"User=device-farm-server", "ExecStartPre=/opt/alcor-device-farm/bin/check-server-deployment.sh",
		"NoNewPrivileges=true", "ProtectSystem=strict", "ProtectKernelModules=true",
	} {
		if !strings.Contains(rawUnit, required) {
			t.Fatalf("Server systemd unit is missing %q", required)
		}
	}
	for _, forbidden := range []string{"SupplementaryGroups=docker", "SupplementaryGroups=kvm", "User=root"} {
		if strings.Contains(rawUnit, forbidden) {
			t.Fatalf("Server systemd unit contains forbidden value %q", forbidden)
		}
	}

	checkScript, err := os.ReadFile(filepath.Join("..", "..", "scripts", "check-server-deployment.sh"))
	if err != nil {
		t.Fatal(err)
	}
	rawCheck := string(checkScript)
	for _, required := range []string{
		"DEVICE_FARM_DATABASE_URL", "DEVICE_FARM_SECURITY_SERVICE_TOKEN", "DEVICE_FARM_SECURITY_AGENT_TOKEN", "--check-config",
	} {
		if !strings.Contains(rawCheck, required) {
			t.Fatalf("Server preflight is missing %q", required)
		}
	}

	backupScript, err := os.ReadFile(filepath.Join("..", "..", "scripts", "backup-device-farm.sh"))
	if err != nil {
		t.Fatal(err)
	}
	rawBackup := string(backupScript)
	if !strings.Contains(rawBackup, "--format=custom") || !strings.Contains(rawBackup, "chmod 0600") || strings.Contains(rawBackup, "echo $DEVICE_FARM_DATABASE_URL") {
		t.Fatal("backup script does not preserve the safe backup contract")
	}
}

func TestPrometheusRulesCoverCriticalDeviceFarmFailures(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "deploy", "monitoring", "prometheus-rules.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Groups []struct {
			Rules []struct {
				Alert string `yaml:"alert"`
				Expr  string `yaml:"expr"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	alerts := map[string]string{}
	for _, group := range document.Groups {
		for _, rule := range group.Rules {
			alerts[rule.Alert] = rule.Expr
		}
	}
	for _, required := range []string{
		"DeviceFarmServerDown", "DeviceFarmDatabaseNotReady", "DeviceFarmAgentHeartbeatStale",
		"DeviceFarmNoReadyDevice", "DeviceFarmReservationBacklog", "DeviceFarmRecyclingStuck",
		"DeviceFarmHTTP5xx", "DeviceFarmMetricCollectionError",
	} {
		if strings.TrimSpace(alerts[required]) == "" {
			t.Fatalf("Prometheus rules are missing %q", required)
		}
	}
}

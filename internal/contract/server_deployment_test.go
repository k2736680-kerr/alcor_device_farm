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
		"cap_drop:", "- ALL", "server.env", "/readyz", "./secrets:/run/secrets/device-farm:ro",
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
	if !ok || len(service.Ports) != 2 {
		t.Fatalf("Server published ports=%v", service.Ports)
	}
	for _, port := range service.Ports {
		if !strings.Contains(port, "DEVICE_FARM_BIND_ADDRESS:-127.0.0.1") {
			t.Fatalf("Server port is not private by default: %q", port)
		}
	}
	if !strings.Contains(service.Ports[0], ":8080:8080") || !strings.Contains(service.Ports[1], ":18081:8081") {
		t.Fatalf("Server published unexpected ports=%v", service.Ports)
	}

	dockerfile, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	image := string(dockerfile)
	if !strings.Contains(image, "USER 65532:65532") || !strings.Contains(image, "CGO_ENABLED=0") ||
		!strings.Contains(image, "console/pnpm-workspace.yaml") ||
		!strings.Contains(image, "pnpm --dir console build") ||
		!strings.Contains(image, "COPY --from=console-builder /src/internal/consoleui/dist") ||
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

	installer, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install-device-farm-server.sh"))
	if err != nil {
		t.Fatal(err)
	}
	rawInstaller := string(installer)
	for _, required := range []string{"pnpm_binary", "--dir console install --frozen-lockfile", "--dir console build", "-m 0750 -o root -g device-farm-server /etc/alcor-device-farm"} {
		if !strings.Contains(rawInstaller, required) {
			t.Fatalf("Server installer is missing %q", required)
		}
	}

	consoleCheck, err := os.ReadFile(filepath.Join("..", "..", "scripts", "verify-console-deployment.sh"))
	if err != nil {
		t.Fatal(err)
	}
	rawConsoleCheck := string(consoleCheck)
	for _, required := range []string{"DEVICE_FARM_CONSOLE_ORIGIN", "strict-transport-security", "content-security-policy", "cache-control", "UNAUTHORIZED"} {
		if !strings.Contains(strings.ToLower(rawConsoleCheck), strings.ToLower(required)) {
			t.Fatalf("Console deployment check is missing %q", required)
		}
	}

	nginx, err := os.ReadFile(filepath.Join("..", "..", "deploy", "server", "nginx-console.conf.example"))
	if err != nil {
		t.Fatal(err)
	}
	rawNginx := string(nginx)
	for _, required := range []string{"listen 443 ssl", "proxy_pass http://127.0.0.1:8080", "Strict-Transport-Security", "X-Forwarded-For $remote_addr", "return 301 https://"} {
		if !strings.Contains(rawNginx, required) {
			t.Fatalf("Nginx Console baseline is missing %q", required)
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

package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlPlanePredeploymentStaysIsolatedAndTLSOnly(t *testing.T) {
	root := filepath.Join("..", "..", "deploy", "control-plane")
	ignored, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignored), "cutover.env") {
		t.Fatal("control-plane cutover.env must stay outside Git")
	}

	content, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(content)
	for _, required := range []string{
		"alcor-device-farm-control-plane", "postgres:17.5-alpine", "./postgres-data:/var/lib/postgresql/data",
		"./backups:/var/backups/device-farm", "device-farm-gateway", "nginx:1.29.1-alpine",
		"./secrets/tls.crt", "./secrets/tls.key", "${DEVICE_FARM_HTTP_PORT:-18180}:8443",
		"${DEVICE_FARM_AGENT_HTTP_PORT:-18182}:8080",
		"device-farm-ios-tunnel", "network_mode: \"service:device-farm-server\"", "profiles: [\"ios\"]",
		"./secrets/ios-tunnel:/run/secrets/ios-tunnel:ro", "device-farm-ios-gateway",
		"${DEVICE_FARM_IOS_GATEWAY_PORT:-18181}:8443", "./secrets/ios-gateway-public-tls/fullchain.pem",
		"read_only: true", "no-new-privileges:true", "cap_drop:", "- ALL",
	} {
		if !strings.Contains(raw, required) {
			t.Fatalf("control-plane compose is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"5432:5432", "${DEVICE_FARM_HTTP_PORT:-18180}:8080", "${DEVICE_FARM_IOS_GATEWAY_PORT:-18181}:8081",
		"/var/run/docker.sock", "privileged: true", ":latest",
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("control-plane compose contains forbidden value %q", forbidden)
		}
	}

	nginx, err := os.ReadFile(filepath.Join(root, "gateway", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	rawNginx := string(nginx)
	for _, required := range []string{"listen 8443 ssl", "TLSv1.2 TLSv1.3", "proxy_pass http://device-farm-server:8080", "X-Forwarded-Proto https"} {
		if !strings.Contains(rawNginx, required) {
			t.Fatalf("control-plane TLS gateway is missing %q", required)
		}
	}

	iosNginx, err := os.ReadFile(filepath.Join(root, "ios-gateway", "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"listen 8443 ssl", "TLSv1.2 TLSv1.3", "proxy_pass http://device-farm-server:8081",
		"X-Forwarded-Proto https", "fullchain.pem", "private.key",
	} {
		if !strings.Contains(string(iosNginx), required) {
			t.Fatalf("control-plane iOS TLS gateway is missing %q", required)
		}
	}

	tunnelDockerfile, err := os.ReadFile(filepath.Join(root, "ios-tunnel", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"FROM alpine:3.22.1", "openssh-client", "adduser", "USER ios-tunnel:ios-tunnel"} {
		if !strings.Contains(string(tunnelDockerfile), required) {
			t.Fatalf("control-plane iOS tunnel image is missing %q", required)
		}
	}

	tunnelEntrypoint, err := os.ReadFile(filepath.Join(root, "ios-tunnel", "entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"StrictHostKeyChecking=yes", "-L 4811:127.0.0.1:4811", "-L 4842:127.0.0.1:8421"} {
		if !strings.Contains(string(tunnelEntrypoint), required) {
			t.Fatalf("control-plane iOS tunnel entrypoint is missing %q", required)
		}
	}
}

func TestCutoverReadinessCheckIsPortableAndReadOnly(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "scripts", "verify-control-plane-cutover-readiness.sh"))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(content)
	for _, required := range []string{
		"control plane origin must use HTTPS",
		"*[!A-Za-z0-9._-]*",
		"expected unauthenticated device API to return 401",
		"production backup not supplied; import remains a cutover-step",
	} {
		if !strings.Contains(raw, required) {
			t.Fatalf("cutover readiness check is missing %q", required)
		}
	}
	for _, forbidden := range []string{"docker compose down", "systemctl ", "sed -i", "DEVICE_FARM_CUTOVER_CONFIRM=1"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("cutover readiness check contains mutating command %q", forbidden)
		}
	}
}

func TestControlPlaneIOSPredeploymentCheckIsReadOnly(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "scripts", "verify-control-plane-ios-predeployment.sh"))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(content)
	for _, required := range []string{
		"iOS gateway origin must use HTTPS",
		"/^  device-farm-server:$/",
		"found && /^  [A-Za-z0-9_.-]+:$/ { exit }",
		"Baguette simulators.json reachable through the shared Server namespace",
		"expected unauthenticated iOS gateway $path to return 401",
		"iOS TLS gateway rejects unauthenticated requests",
		"no Agent, NPS, database or 171 service was changed",
	} {
		if !strings.Contains(raw, required) {
			t.Fatalf("iOS predeployment check is missing %q", required)
		}
	}
	for _, forbidden := range []string{"docker compose down", "systemctl ", "sed -i", "psql ", "ssh "} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("iOS predeployment check contains mutating command %q", forbidden)
		}
	}
}

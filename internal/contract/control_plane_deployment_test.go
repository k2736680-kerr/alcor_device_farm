package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlPlanePredeploymentStaysIsolatedAndTLSOnly(t *testing.T) {
	root := filepath.Join("..", "..", "deploy", "control-plane")
	content, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(content)
	for _, required := range []string{
		"alcor-device-farm-control-plane", "postgres:17.5-alpine", "./postgres-data:/var/lib/postgresql/data",
		"./backups:/var/backups/device-farm", "device-farm-gateway", "nginx:1.29.1-alpine",
		"./secrets/tls.crt", "./secrets/tls.key", "${DEVICE_FARM_HTTP_PORT:-18180}:8443",
		"read_only: true", "no-new-privileges:true", "cap_drop:", "- ALL",
	} {
		if !strings.Contains(raw, required) {
			t.Fatalf("control-plane compose is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"5432:5432", "${DEVICE_FARM_HTTP_PORT:-18180}:8080", "/var/run/docker.sock", "privileged: true", ":latest",
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
}

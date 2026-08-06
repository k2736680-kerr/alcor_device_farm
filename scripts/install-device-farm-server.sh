#!/usr/bin/env sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "run this installer with sudo or as root" >&2
  exit 1
fi
if [ "$(uname -s)" != "Linux" ]; then
  echo "device-farm-server systemd deployment requires Linux" >&2
  exit 1
fi

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_binary="${DEVICE_FARM_GO:-go}"
pnpm_binary="${DEVICE_FARM_PNPM:-pnpm}"
version="${DEVICE_FARM_VERSION:-dev}"
commit="${DEVICE_FARM_COMMIT:-$(git -C "$repository_root" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)}"
build_date="${DEVICE_FARM_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

"$go_binary" version >/dev/null
"$pnpm_binary" --version >/dev/null
if ! id device-farm-server >/dev/null 2>&1; then
  nologin_shell=$(command -v nologin || true)
  if [ -z "$nologin_shell" ]; then
    echo "nologin shell is required" >&2
    exit 1
  fi
  useradd --system --home-dir /nonexistent --shell "$nologin_shell" device-farm-server
fi

install -d -m 0755 /opt/alcor-device-farm/bin /opt/alcor-device-farm/migrations
temporary_binary=$(mktemp)
trap 'rm -f "$temporary_binary"' EXIT HUP INT TERM
(
  cd "$repository_root"
  "$pnpm_binary" --dir console install --frozen-lockfile
  "$pnpm_binary" --dir console build
  CGO_ENABLED=0 "$go_binary" build -trimpath \
    -ldflags "-s -w -X github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo.version=$version -X github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo.commit=$commit -X github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo.buildDate=$build_date" \
    -o "$temporary_binary" ./cmd/device-farm-server
)
install -m 0755 "$temporary_binary" /opt/alcor-device-farm/bin/device-farm-server
install -m 0755 "$repository_root/scripts/check-server-deployment.sh" /opt/alcor-device-farm/bin/check-server-deployment.sh
install -m 0644 "$repository_root"/migrations/*.sql /opt/alcor-device-farm/migrations/

install -d -m 0750 -o root -g device-farm-server /etc/alcor-device-farm
if [ ! -e /etc/alcor-device-farm/server.env ]; then
  install -m 0600 "$repository_root/deploy/server/server.env.example" /etc/alcor-device-farm/server.env
fi
if [ ! -e /etc/alcor-device-farm/server.yaml ]; then
  install -m 0640 -o root -g device-farm-server "$repository_root/config/config.example.yaml" /etc/alcor-device-farm/server.yaml
fi
install -m 0644 "$repository_root/deploy/server/alcor-device-farm-server.service" /etc/systemd/system/alcor-device-farm-server.service
systemctl daemon-reload

echo "Device Farm Server installed but not started."
echo "Configure /etc/alcor-device-farm/server.env, apply migrations, verify --check-config, then enable the service."

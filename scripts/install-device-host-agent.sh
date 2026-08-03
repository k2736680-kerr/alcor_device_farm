#!/usr/bin/env sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "run this installer with sudo or as root" >&2
  exit 1
fi
if [ "$(uname -s)" != "Linux" ]; then
  echo "device-host-agent Docker mode requires Linux" >&2
  exit 1
fi

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
kvm_device="${DEVICE_FARM_DOCKER_KVM_DEVICE:-/dev/kvm}"
docker_binary="${DEVICE_FARM_DOCKER_BINARY:-docker}"
go_binary="${DEVICE_FARM_GO:-go}"

if [ ! -c "$kvm_device" ] || [ ! -r "$kvm_device" ] || [ ! -w "$kvm_device" ]; then
  echo "$kvm_device must be a readable and writable KVM character device" >&2
  exit 1
fi
"$docker_binary" version >/dev/null
"$go_binary" version >/dev/null

if ! getent group docker >/dev/null; then
  echo "docker group does not exist; install Docker Engine first" >&2
  exit 1
fi
if ! getent group kvm >/dev/null; then
  echo "kvm group does not exist; configure KVM permissions first" >&2
  exit 1
fi
if ! id device-farm >/dev/null 2>&1; then
  nologin_shell=$(command -v nologin || true)
  if [ -z "$nologin_shell" ]; then
    echo "nologin shell is required" >&2
    exit 1
  fi
  useradd --system --home-dir /nonexistent --shell "$nologin_shell" device-farm
fi
usermod --append --groups docker,kvm device-farm

install -d -m 0755 /opt/alcor-device-farm/bin
temporary_binary=$(mktemp)
trap 'rm -f "$temporary_binary"' EXIT HUP INT TERM
(
  cd "$repository_root"
  CGO_ENABLED=0 "$go_binary" build -trimpath -o "$temporary_binary" ./cmd/device-host-agent
)
install -m 0755 "$temporary_binary" /opt/alcor-device-farm/bin/device-host-agent

install -d -m 0700 /etc/alcor-device-farm
if [ ! -e /etc/alcor-device-farm/host-agent.env ]; then
  install -m 0600 "$repository_root/deploy/docker-emulator/host-agent.env.example" /etc/alcor-device-farm/host-agent.env
fi
install -m 0644 "$repository_root/deploy/docker-emulator/alcor-device-host-agent.service" /etc/systemd/system/alcor-device-host-agent.service
systemctl daemon-reload

echo "Host Agent installed but not started."
echo "Edit /etc/alcor-device-farm/host-agent.env, run the Docker integration verification, then enable the service."

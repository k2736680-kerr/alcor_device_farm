#!/usr/bin/env sh
set -eu

if [ "$(uname -s)" != "Linux" ]; then
  echo "DF-014 requires a Linux host" >&2
  exit 1
fi

kvm_device="${DEVICE_FARM_DOCKER_KVM_DEVICE:-/dev/kvm}"
if [ ! -c "$kvm_device" ] || [ ! -r "$kvm_device" ] || [ ! -w "$kvm_device" ]; then
  echo "$kvm_device must be a readable and writable KVM character device" >&2
  exit 1
fi

: "${DEVICE_FARM_DOCKER_IMAGE:?set DEVICE_FARM_DOCKER_IMAGE to a fixed tag or digest}"
: "${DEVICE_FARM_DOCKER_ADVERTISE_HOST:?set DEVICE_FARM_DOCKER_ADVERTISE_HOST to the Host address reachable by ADB clients}"

case "$DEVICE_FARM_DOCKER_IMAGE" in
  *:latest|latest)
    echo "floating latest images are not allowed" >&2
    exit 1
    ;;
esac

docker_binary="${DEVICE_FARM_DOCKER_BINARY:-docker}"
go_binary="${DEVICE_FARM_GO:-go}"

"$docker_binary" version >/dev/null
if ! "$docker_binary" image inspect "$DEVICE_FARM_DOCKER_IMAGE" >/dev/null 2>&1; then
  "$docker_binary" pull "$DEVICE_FARM_DOCKER_IMAGE"
fi

export DEVICE_FARM_DOCKER_INTEGRATION=1
"$go_binary" test -count=1 -v ./internal/providers/docker -run '^TestDockerProviderLinuxKVMIntegration$'

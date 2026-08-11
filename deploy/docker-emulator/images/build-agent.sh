#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
operation="${1:?usage: build-agent.sh catalog|prepare [package-name] [revision]}"

case "$operation" in
  catalog)
    : "${DEVICE_FARM_SDKMANAGER_BINARY:?set the absolute path of the pinned sdkmanager binary}"
    : "${DEVICE_FARM_SDKMANAGER_VERSION:?set the pinned Android command-line tools version}"
    sdkmanager_binary="$DEVICE_FARM_SDKMANAGER_BINARY"
    command -v "$sdkmanager_binary" >/dev/null
    actual_version="$("$sdkmanager_binary" --version | awk 'NF { value=$0 } END { gsub(/^[ \t\r]+|[ \t\r]+$/, "", value); print value }')"
    if [ "$actual_version" != "$DEVICE_FARM_SDKMANAGER_VERSION" ]; then
      echo "sdkmanager version $actual_version does not match pinned $DEVICE_FARM_SDKMANAGER_VERSION" >&2
      exit 1
    fi
    temporary_output="$(mktemp)"
    trap 'rm -f -- "$temporary_output"' EXIT
    LC_ALL=C "$sdkmanager_binary" --list --channel=0 > "$temporary_output"
    python3 "$script_dir/sdk_catalog.py" "$temporary_output"
    ;;
  prepare)
    exec "$script_dir/prepare.sh" "${2:?package-name is required}" "${3:?revision is required}"
    ;;
  *)
    echo "unsupported Build Agent operation" >&2
    exit 64
    ;;
esac

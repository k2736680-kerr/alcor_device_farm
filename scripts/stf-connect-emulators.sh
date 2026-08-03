#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="${STF_COMPOSE_FILE:-${project_root}/deploy/stf/compose.yaml}"
env_file="${STF_ENV_FILE:-${project_root}/deploy/stf/.env}"

if [[ ! -f "${env_file}" ]]; then
  echo "STF env file not found: ${env_file}" >&2
  exit 1
fi
if (($# == 0)); then
  echo "usage: $0 <adb-host:port> [adb-host:port ...]" >&2
  exit 1
fi

compose=(docker compose --env-file "${env_file}" -f "${compose_file}")
for endpoint in "$@"; do
  if [[ ! "${endpoint}" =~ ^[^[:space:]:]+:[0-9]{1,5}$ ]]; then
    echo "invalid ADB endpoint: ${endpoint}" >&2
    exit 1
  fi
  "${compose[@]}" exec -T stf-adb adb -H 127.0.0.1 -P 5037 connect "${endpoint}"
done

"${compose[@]}" exec -T stf-adb adb -H 127.0.0.1 -P 5037 devices -l

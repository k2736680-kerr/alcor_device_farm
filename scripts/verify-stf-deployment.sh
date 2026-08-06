#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="${STF_COMPOSE_FILE:-${project_root}/deploy/stf/compose.yaml}"
env_file="${STF_ENV_FILE:-${project_root}/deploy/stf/.env}"
: "${STF_API_URL:?set STF_API_URL, for example http://127.0.0.1:7100}"
: "${STF_API_TOKEN:?set STF_API_TOKEN without writing it to .env or command output}"
: "${DEVICE_FARM_ADB_ENDPOINTS:?set one or more comma-separated ADB endpoints}"

for tool in docker curl python3; do
  command -v "${tool}" >/dev/null 2>&1 || { echo "missing required tool: ${tool}" >&2; exit 1; }
done
if [[ ! -f "${env_file}" ]]; then
  echo "copy deploy/stf/.env.example to deploy/stf/.env and fill real values" >&2
  exit 1
fi

compose=(docker compose --env-file "${env_file}" -f "${compose_file}")
rendered="$("${compose[@]}" config)"
if grep -Eq '(^|[/:])latest([[:space:]]|$)' <<<"${rendered}"; then
  echo "STF deployment must not use latest images" >&2
  exit 1
fi

"${compose[@]}" up -d
for _ in $(seq 1 60); do
  if curl --fail --silent --show-error --max-time 3 "${STF_API_URL%/}/" >/dev/null; then
    break
  fi
  sleep 2
done
curl --fail --silent --show-error --max-time 3 "${STF_API_URL%/}/" >/dev/null

IFS=',' read -r -a endpoints <<<"${DEVICE_FARM_ADB_ENDPOINTS}"
if ((${#endpoints[@]} < 1)) || [[ -z "${endpoints[0]}" ]]; then
  echo "at least one Emulator ADB endpoint is required" >&2
  exit 1
fi
"${project_root}/scripts/stf-connect-emulators.sh" "${endpoints[@]}"

for _ in $(seq 1 90); do
  inventory="$(curl --fail --silent --show-error --max-time 5 --config - <<EOF
header = "Authorization: Bearer ${STF_API_TOKEN}"
url = "${STF_API_URL%/}/api/v1/devices?fields=serial,present,ready,using"
EOF
)"
  if python3 -c '
import json
import sys

expected = sys.argv[1:]
payload = json.load(sys.stdin)
ready = {
    item.get("serial")
    for item in payload.get("devices", [])
    if item.get("present") is True and item.get("ready") is True
}
raise SystemExit(0 if all(serial in ready for serial in expected) else 1)
' "${endpoints[@]}" <<<"${inventory}"; then
    python3 -c '
import json
import sys

payload = json.load(sys.stdin)
devices = [
    {key: item.get(key) for key in ("serial", "present", "ready", "using")}
    for item in payload.get("devices", [])
]
print(json.dumps({"devices": devices}, ensure_ascii=False))
' <<<"${inventory}"
    echo "STF deployment verification passed"
    exit 0
  fi
  sleep 2
done

echo "STF did not report all expected Emulator serials as present and ready" >&2
exit 1

#!/usr/bin/env sh
set -eu

: "${DEVICE_FARM_CONTROL_PLANE_ORIGIN:?set DEVICE_FARM_CONTROL_PLANE_ORIGIN}"
: "${DEVICE_FARM_AGENT_SERVER_URL:?set DEVICE_FARM_AGENT_SERVER_URL}"
: "${DEVICE_FARM_LEGACY_SERVER_URL:?set DEVICE_FARM_LEGACY_SERVER_URL}"

curl_args="--fail --silent --show-error --max-time 10"
case "$DEVICE_FARM_CONTROL_PLANE_ORIGIN" in
  https://*) curl_args="$curl_args --insecure" ;;
  *) echo "control plane origin must use HTTPS" >&2; exit 2 ;;
esac

for path in /healthz /readyz /metrics /console/; do
  curl $curl_args "$DEVICE_FARM_CONTROL_PLANE_ORIGIN$path" >/dev/null
  echo "control-plane $path ok"
done

unauthenticated_status=$(curl --silent --show-error --max-time 10 --insecure -o /dev/null -w '%{http_code}' \
  "$DEVICE_FARM_CONTROL_PLANE_ORIGIN/api/v1/device-hosts")
if [ "$unauthenticated_status" != 401 ]; then
  echo "expected unauthenticated device API to return 401, got $unauthenticated_status" >&2
  exit 1
fi
echo "control-plane unauthenticated API rejected"

check_tcp_url() {
  url=$1
  label=$2
  host_port=$(printf '%s' "$url" | sed -E 's#^[a-zA-Z]+://##; s#/.*$##')
  host=${host_port%:*}
  port=${host_port##*:}
  case "$host" in
    ""|*[!A-Za-z0-9._-]*) echo "$label has invalid host" >&2; exit 2 ;;
  esac
  case "$port" in
    ''|*[!0-9]*) echo "$label has invalid port" >&2; exit 2 ;;
  esac
  if command -v nc >/dev/null 2>&1; then
    nc -z -w 5 "$host" "$port"
  else
    timeout 5 sh -c "echo >/dev/tcp/$host/$port"
  fi
  echo "$label $host:$port reachable"
}

check_tcp_url "$DEVICE_FARM_AGENT_SERVER_URL" agent-api
check_tcp_url "$DEVICE_FARM_LEGACY_SERVER_URL" legacy-server

if [ -n "${DEVICE_FARM_PRODUCTION_BACKUP:-}" ]; then
  test -f "$DEVICE_FARM_PRODUCTION_BACKUP"
  if [ -n "${DEVICE_FARM_PRODUCTION_BACKUP_SHA256:-}" ]; then
    printf '%s  %s\n' "$DEVICE_FARM_PRODUCTION_BACKUP_SHA256" "$DEVICE_FARM_PRODUCTION_BACKUP" | sha256sum -c -
  fi
  echo "production backup present"
else
  echo "production backup not supplied; import remains a cutover-step"
fi

echo "readiness checks passed; no service, Agent or NPS configuration was changed"

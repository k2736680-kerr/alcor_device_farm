#!/usr/bin/env sh
set -eu

: "${DEVICE_FARM_IOS_GATEWAY_ORIGIN:?set DEVICE_FARM_IOS_GATEWAY_ORIGIN}"
: "${DEVICE_FARM_CONTROL_PLANE_DIR:?set DEVICE_FARM_CONTROL_PLANE_DIR}"

configured_public_url=$(sed -n 's/^DEVICE_FARM_IOS_REMOTE_CONTROL_PUBLIC_URL=//p' \
  "$DEVICE_FARM_CONTROL_PLANE_DIR/server.env" | head -n 1)
if [ -z "$configured_public_url" ]; then
  echo "server.env must set DEVICE_FARM_IOS_REMOTE_CONTROL_PUBLIC_URL" >&2
  exit 2
fi

normalize_origin() {
  printf '%s' "$1" | sed 's#/*$##'
}

if [ "$(normalize_origin "$configured_public_url")" != "$(normalize_origin "$DEVICE_FARM_IOS_GATEWAY_ORIGIN")" ]; then
  echo "iOS gateway origin does not match DEVICE_FARM_IOS_REMOTE_CONTROL_PUBLIC_URL" >&2
  echo "configured=$(normalize_origin "$configured_public_url")" >&2
  echo "checked=$(normalize_origin "$DEVICE_FARM_IOS_GATEWAY_ORIGIN")" >&2
  exit 1
fi

case "$DEVICE_FARM_IOS_GATEWAY_ORIGIN" in
  https://*) ;;
  *) echo "iOS gateway origin must use HTTPS" >&2; exit 2 ;;
esac

compose="docker compose --profile ios --env-file $DEVICE_FARM_CONTROL_PLANE_DIR/postgres.env --env-file $DEVICE_FARM_CONTROL_PLANE_DIR/server.env -f $DEVICE_FARM_CONTROL_PLANE_DIR/compose.yaml"

$compose config --quiet

server_ports=$($compose config | awk '
  /^  device-farm-server:$/ { found=1; next }
  found && /^  [A-Za-z0-9_.-]+:$/ { exit }
  found && /published:/ { print }
')
if printf '%s' "$server_ports" | grep -q '18181'; then
  echo "device-farm-server must not publish 18181 directly" >&2
  exit 1
fi

$compose exec -T device-farm-server wget -qO- http://127.0.0.1:4842/simulators.json >/dev/null
echo "Baguette simulators.json reachable through the shared Server namespace"

for path in / /simulators.json; do
  http_status=$(curl --silent --show-error --insecure --max-time 10 -o /dev/null -w '%{http_code}' \
    "$DEVICE_FARM_IOS_GATEWAY_ORIGIN$path")
  if [ "$http_status" != 401 ]; then
    echo "expected unauthenticated iOS gateway $path to return 401, got $http_status" >&2
    exit 1
  fi
done
echo "iOS TLS gateway rejects unauthenticated requests"

echo "iOS predeployment checks passed; no Agent, NPS, database or 171 service was changed"

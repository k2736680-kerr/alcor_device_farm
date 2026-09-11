#!/usr/bin/env sh
set -eu

: "${DEVICE_FARM_IOS_GATEWAY_ORIGIN:?set DEVICE_FARM_IOS_GATEWAY_ORIGIN}"
: "${DEVICE_FARM_CONTROL_PLANE_DIR:?set DEVICE_FARM_CONTROL_PLANE_DIR}"

case "$DEVICE_FARM_IOS_GATEWAY_ORIGIN" in
  https://*) ;;
  *) echo "iOS gateway origin must use HTTPS" >&2; exit 2 ;;
esac

compose="docker compose --profile ios --env-file $DEVICE_FARM_CONTROL_PLANE_DIR/postgres.env --env-file $DEVICE_FARM_CONTROL_PLANE_DIR/server.env -f $DEVICE_FARM_CONTROL_PLANE_DIR/compose.yaml"

$compose config --quiet

server_ports=$($compose config | awk '/device-farm-server:/{found=1} found && /published:/{print; count++} found && count==2{exit}')
if printf '%s' "$server_ports" | grep -q '18181'; then
  echo "device-farm-server must not publish 18181 directly" >&2
  exit 1
fi

$compose exec -T device-farm-server wget -qO- http://127.0.0.1:4842/simulators.json >/dev/null
echo "Baguette simulators.json reachable through the shared Server namespace"

curl --fail --silent --show-error --insecure --max-time 10 "$DEVICE_FARM_IOS_GATEWAY_ORIGIN/" -o /dev/null || status=$?
case "${status:-0}" in
  0|22) ;;
  *) echo "iOS TLS gateway is unreachable" >&2; exit 1 ;;
esac

http_status=$(curl --silent --show-error --insecure --max-time 10 -o /dev/null -w '%{http_code}' \
  "$DEVICE_FARM_IOS_GATEWAY_ORIGIN/simulators.json")
if [ "$http_status" != 401 ]; then
  echo "expected unauthenticated iOS gateway request to return 401, got $http_status" >&2
  exit 1
fi

echo "iOS predeployment checks passed; no Agent, NPS, database or 171 service was changed"

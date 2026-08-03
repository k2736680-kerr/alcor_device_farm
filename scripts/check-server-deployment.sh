#!/usr/bin/env sh
set -eu

server_binary="${DEVICE_FARM_SERVER_BINARY:-/opt/alcor-device-farm/bin/device-farm-server}"
config_path="${DEVICE_FARM_SERVER_CONFIG-/etc/alcor-device-farm/server.yaml}"

for variable_name in DEVICE_FARM_DATABASE_URL DEVICE_FARM_SECURITY_SERVICE_TOKEN DEVICE_FARM_SECURITY_AGENT_TOKEN; do
  variable_value=$(printenv "$variable_name" 2>/dev/null || true)
  if [ -z "$variable_value" ]; then
    echo "$variable_name is required for a deployable Server" >&2
    exit 1
  fi
done
if [ "$DEVICE_FARM_SECURITY_SERVICE_TOKEN" = "$DEVICE_FARM_SECURITY_AGENT_TOKEN" ]; then
  echo "Service and Agent tokens must be different" >&2
  exit 1
fi
case "$DEVICE_FARM_DATABASE_URL" in
  postgres://*|postgresql://*) ;;
  *) echo "DEVICE_FARM_DATABASE_URL must be a PostgreSQL URL" >&2; exit 1 ;;
esac

if [ -n "$config_path" ]; then
  "$server_binary" --config "$config_path" --check-config
else
  "$server_binary" --check-config
fi

#!/bin/sh
set -eu

: "${DEVICE_FARM_IOS_TUNNEL_SSH_HOST:?set DEVICE_FARM_IOS_TUNNEL_SSH_HOST when the ios profile is enabled}"
: "${DEVICE_FARM_IOS_TUNNEL_SSH_USER:?set DEVICE_FARM_IOS_TUNNEL_SSH_USER when the ios profile is enabled}"

key=/run/secrets/ios-tunnel/id_ed25519
known_hosts=/run/secrets/ios-tunnel/known_hosts
test -r "$key"
test -r "$known_hosts"

exec ssh \
  -N -T \
  -o ExitOnForwardFailure=yes \
  -o ServerAliveInterval=30 \
  -o ServerAliveCountMax=3 \
  -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$known_hosts" \
  -o IdentitiesOnly=yes \
  -o LogLevel=ERROR \
  -i "$key" \
  -p "${DEVICE_FARM_IOS_TUNNEL_SSH_PORT:-22}" \
  -L 4811:127.0.0.1:4811 \
  -L 4842:127.0.0.1:4842 \
  "${DEVICE_FARM_IOS_TUNNEL_SSH_USER}@${DEVICE_FARM_IOS_TUNNEL_SSH_HOST}"

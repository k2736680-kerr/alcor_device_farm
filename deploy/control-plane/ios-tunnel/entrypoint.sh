#!/bin/sh
set -eu

: "${DEVICE_FARM_IOS_TUNNEL_SSH_HOST:?set DEVICE_FARM_IOS_TUNNEL_SSH_HOST when the ios profile is enabled}"
: "${DEVICE_FARM_IOS_TUNNEL_SSH_USER:?set DEVICE_FARM_IOS_TUNNEL_SSH_USER when the ios profile is enabled}"

key=/run/secrets/ios-tunnel/id_ed25519
known_hosts=/run/secrets/ios-tunnel/known_hosts
test -r "$key"
test -r "$known_hosts"

# 关键：用更短的 ServerAliveInterval + CountMax 让 SSH 端到端失活时自行退出。
# 之前 ServerAliveInterval=30 CountMax=3 让一次 Mac 端重启要等 90 秒才感知到；
# 现在 15*2=30s 内必退，配合 Docker restart: unless-stopped 快速重建。
#
# 另外 -o ExitOnForwardFailure=yes 确保 Mac 上的 4811/8421 不监听时一开始就失败退出，
# 不会出现"容器 healthy 但转发卡死"的假活状态。

exec ssh \
  -N -T \
  -o ExitOnForwardFailure=yes \
  -o ServerAliveInterval=15 \
  -o ServerAliveCountMax=2 \
  -o ConnectTimeout=10 \
  -o TCPKeepAlive=yes \
  -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$known_hosts" \
  -o IdentitiesOnly=yes \
  -o LogLevel=ERROR \
  -i "$key" \
  -p "${DEVICE_FARM_IOS_TUNNEL_SSH_PORT:-22}" \
  -L 4811:127.0.0.1:4811 \
  -L 4842:127.0.0.1:8421 \
  "${DEVICE_FARM_IOS_TUNNEL_SSH_USER}@${DEVICE_FARM_IOS_TUNNEL_SSH_HOST}"

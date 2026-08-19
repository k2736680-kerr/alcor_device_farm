#!/bin/sh
set -eu

component="${1:-}"
ios_host_root="${IOS_HOST_ROOT:-}"

case "$ios_host_root" in
  /*) ;;
  *) echo "IOS_HOST_ROOT 必须是专用 iOS 宿主机目录的绝对路径" >&2; exit 64 ;;
esac
if [ "$ios_host_root" = "/" ] || [ ! -d "$ios_host_root" ]; then
  echo "IOS_HOST_ROOT 不是可用的专用 iOS 宿主机目录" >&2
  exit 64
fi

appium="$ios_host_root/runtime/node_modules/.bin/appium"
export APPIUM_HOME="$ios_host_root/appium-home"
export PATH="$ios_host_root/node/bin:/usr/bin:/bin:/usr/sbin:/sbin"

run_appium_with_watchdog() {
  service_name="$1"
  port="$2"
  shift 2

  "$appium" "$@" &
  appium_pid=$!
  stop_appium() {
    if /bin/kill -0 "$appium_pid" 2>/dev/null; then
      /bin/kill -TERM "$appium_pid" 2>/dev/null || true
      remaining=10
      while [ "$remaining" -gt 0 ] && /bin/kill -0 "$appium_pid" 2>/dev/null; do
        /bin/sleep 1
        remaining=$((remaining - 1))
      done
      if /bin/kill -0 "$appium_pid" 2>/dev/null; then
        /bin/kill -KILL "$appium_pid" 2>/dev/null || true
      fi
    fi
    wait "$appium_pid" 2>/dev/null || true
  }
  trap 'stop_appium; exit 0' HUP INT TERM

  failures=0
  while /bin/kill -0 "$appium_pid" 2>/dev/null; do
    /bin/sleep 10
    if /usr/bin/curl --fail --silent --connect-timeout 2 --max-time 5 \
        --output /dev/null "http://127.0.0.1:${port}/status" \
      && /usr/bin/curl --fail --silent --connect-timeout 2 --max-time 5 \
        --output /dev/null "http://127.0.0.1:${port}/device-farm/api/device/ios"; then
      failures=0
    else
      failures=$((failures + 1))
      echo "Appium Device Farm ${service_name} 健康检查连续失败 ${failures}/3" >&2
    fi
    if [ "$failures" -ge 3 ]; then
      echo "Appium Device Farm ${service_name} 接口持续无响应，退出并交由 launchd 自动恢复" >&2
      stop_appium
      exit 1
    fi
  done
  set +e
  wait "$appium_pid"
  status=$?
  set -e
  exit "$status"
}

case "$component" in
  appium-hub)
    run_appium_with_watchdog "Hub" 4723 server \
      --address=127.0.0.1 \
      --port=4723 \
      --log-level=info \
      --use-plugins=device-farm \
      --plugin-device-farm-platform=ios \
      --plugin-device-farm-ios-device-type=real \
      --plugin-device-farm-remove-devices-from-database-before-running-the-plugin \
      --plugin-device-farm-bind-host-or-ip=127.0.0.1
    ;;
  appium-node)
    run_appium_with_watchdog "Node" 4724 server \
      --address=127.0.0.1 \
      --port=4724 \
      --log-level=info \
      --use-plugins=device-farm \
      --plugin-device-farm-platform=ios \
      --plugin-device-farm-ios-device-type=simulated \
      --plugin-device-farm-booted-simulators \
      --plugin-device-farm-remove-devices-from-database-before-running-the-plugin \
      --plugin-device-farm-check-stale-devices-interval-ms=5000 \
      --plugin-device-farm-hub=http://127.0.0.1:4723 \
      --plugin-device-farm-send-node-devices-to-hub-interval-ms=5000 \
      --plugin-device-farm-bind-host-or-ip=127.0.0.1
    ;;
  host-agent)
    env_file="$ios_host_root/host-agent.env"
    if [ ! -f "$env_file" ]; then
      echo "缺少权限受控的 host-agent.env" >&2
      exit 64
    fi
    permissions="$(/usr/bin/stat -f '%Lp' "$env_file")"
    if [ "$permissions" != "600" ]; then
      echo "host-agent.env 权限必须是 600" >&2
      exit 64
    fi
    set -a
    # shellcheck disable=SC1090
    . "$env_file"
    set +a
    exec "$ios_host_root/device-host-agent"
    ;;
  *)
    echo "服务类型必须是 appium-hub、appium-node 或 host-agent" >&2
    exit 64
    ;;
esac

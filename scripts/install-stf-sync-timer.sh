#!/usr/bin/env sh
# 在 STF 宿主机上安装「远程设备汇入」systemd timer（ADR-0037 决策 2）。
#
# 适用：运行着本仓库 deploy/stf 这一套 STF 的 Linux 宿主机（当前是 10.0.30.171）。
# 作用：让该宿主机的 STF ADB server 周期性地把其它宿主机的 Android 设备纳入进来，
#       从而让远程宿主机的设备也能通过 STF 被看到和远控。
#
# 用法（在仓库副本根目录，需 root）：
#   sudo scripts/install-stf-sync-timer.sh
#
# 幂等：重复执行安全。已存在的 /etc/alcor-device-farm/stf-sync.env 不会被覆盖。
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "run this installer with sudo or as root" >&2
  exit 1
fi

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
install_root="${DEVICE_FARM_INSTALL_ROOT:-/opt/alcor-device-farm}"
env_dir="/etc/alcor-device-farm"
env_file="${env_dir}/stf-sync.env"

for required in \
  "${repository_root}/scripts/stf-sync-remote-emulators.sh" \
  "${repository_root}/deploy/stf/alcor-device-farm-stf-sync.service" \
  "${repository_root}/deploy/stf/alcor-device-farm-stf-sync.timer" \
  "${repository_root}/deploy/stf/stf-sync.env.example"
do
  if [ ! -f "${required}" ]; then
    echo "missing required file: ${required}" >&2
    exit 1
  fi
done

# 1. 把脚本装到固定位置，让 unit 文件里的路径与仓库检出位置解耦。
install -d -m 0755 "${install_root}/scripts"
install -m 0755 "${repository_root}/scripts/stf-sync-remote-emulators.sh" \
  "${install_root}/scripts/stf-sync-remote-emulators.sh"

# 2. 配置目录与 env 模板（不覆盖已有配置）。
install -d -m 0700 "${env_dir}"
if [ ! -e "${env_file}" ]; then
  install -m 0600 "${repository_root}/deploy/stf/stf-sync.env.example" "${env_file}"
  # 同步脚本默认按「脚本所在目录的上级/deploy/stf」推导 compose 位置，但脚本被
  # 复制到 ${install_root}/scripts/ 后该推导失效（那里没有 deploy/stf/）。
  # 这里显式写回仓库副本的真实位置，避免 service 以 exit 1 反复失败。
  {
    echo
    echo "# 由安装器写入：STF compose 位于仓库副本，而非 ${install_root}。"
    echo "STF_COMPOSE_FILE=${repository_root}/deploy/stf/compose.yaml"
    echo "STF_ENV_FILE=${repository_root}/deploy/stf/.env"
  } >> "${env_file}"
  echo "created ${env_file} from template - review it before starting the timer"
fi

# 3. systemd 单元。ExecStart 指向固定安装位置。
service_target="/etc/systemd/system/alcor-device-farm-stf-sync.service"
timer_target="/etc/systemd/system/alcor-device-farm-stf-sync.timer"
{
  echo "[Unit]"
  echo "Description=Alcor Device Farm STF remote device sync (connect remote host Android endpoints)"
  echo "Wants=network-online.target"
  echo "After=network-online.target docker.service"
  echo "Requires=docker.service"
  echo "PartOf=docker.service"
  echo
  echo "[Service]"
  echo "Type=oneshot"
  echo "EnvironmentFile=-${env_file}"
  echo "ExecStart=${install_root}/scripts/stf-sync-remote-emulators.sh"
  echo "WorkingDirectory=${install_root}"
  echo "TimeoutStartSec=300"
  echo "Nice=10"
  echo "IOSchedulingClass=idle"
  echo
  echo "[Install]"
  echo "WantedBy=multi-user.target"
} > "${service_target}"
install -m 0644 "${repository_root}/deploy/stf/alcor-device-farm-stf-sync.timer" "${timer_target}"

systemctl daemon-reload
systemctl enable alcor-device-farm-stf-sync.timer

echo
# 只有在 Service Token 已就绪时才立即启动，否则首次触发必然以 exit 1 失败并污染 journal。
if grep -qE '^DEVICE_FARM_SERVICE_TOKEN=.+' "${env_file}" 2>/dev/null \
  || [ -r "${env_dir}/service-token" ]; then
  systemctl start alcor-device-farm-stf-sync.timer
  echo "Installed and started alcor-device-farm-stf-sync.timer."
else
  echo "Installed alcor-device-farm-stf-sync.timer (enabled, not started yet)."
  echo "  Service Token is not configured, so the timer was left stopped."
  echo "  After setting DEVICE_FARM_SERVICE_TOKEN in ${env_file}, run:"
  echo "    systemctl start alcor-device-farm-stf-sync.timer"
fi
echo
echo "  1. Edit ${env_file} (set DEVICE_FARM_API_BASE_URL and the service token)."
echo "  2. Verify:    systemctl start alcor-device-farm-stf-sync.service"
echo "  3. Inspect:   journalctl -u alcor-device-farm-stf-sync.service -n 50 --no-pager"
echo "  4. Schedule:  systemctl list-timers alcor-device-farm-stf-sync.timer"

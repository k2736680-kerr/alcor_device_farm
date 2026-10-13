#!/usr/bin/env sh
# 把一台 Linux + Docker + KVM 宿主机接入 Device Farm（ADR-0039）。
#
# 用法（在仓库副本根目录，需 root；由管理员在目标宿主机本地执行）：
#   sudo scripts/onboard-device-host.sh --server-url https://10.0.80.220:18180 \
#     --host-id <已登记的 Host ID 或留空以从 --register 登记> \
#     --agent-token-file /etc/alcor-device-farm/agent-token
#
# 职责边界（务必先读 docs/host_onboarding_standard_process.md）：
#   * 本脚本只负责「装 Agent + 写配置 + 起服务 + 自检」，**不登记 Host**
#     （登记走控制面 API/Console，见 ADR-0034；token 不进本脚本）。
#   * **不动镜像、不动池容量**。它们影响调度与资源，必须由管理员显式决定
#     （镜像搬运见标准流程第 5 节；池容量见第 5.4 节）。
#   * Server 不通过 SSH/Docker Socket 远程管理宿主（ADR-0031 决策 6）。
#
# 幂等：可重复执行。已存在的 host-agent.env 不会被覆盖（除非 --force-env）。
set -eu

usage() {
  cat <<'USAGE'
usage: onboard-device-host.sh --server-url <url> --host-id <id> --agent-token-file <path> [options]

required:
  --server-url <url>        控制面 Agent API 地址，如 https://10.0.80.220:18180
  --host-id <id>            控制面已登记的 Host ID（UUID）
  --agent-token-file <path> 含 Agent Token 的文件（只读一次，不落盘到仓库）

optional:
  --advertise-host <ip>     宿主对外地址；默认取默认路由源 IP
  --bind-address <ip>       Docker 端口绑定地址；默认与 advertise 相同
  --image <ref>            容器镜像引用；仅写入 env，不拉取也不校验
  --agent-binary <path>    已编译的 linux/amd64 device-host-agent；默认在仓库内构建
  --render-device <path>    渲染设备，如 /dev/dri/renderD128
  --concurrency <n>         并发设备数，默认 1
  --kvm-device <path>       KVM 设备，默认 /dev/kvm
  --install-root <dir>      安装根目录，默认 /opt/alcor-device-farm
  --env-dir <dir>           配置目录，默认 /etc/alcor-device-farm
  --unit-file <path>        已就绪的 systemd 单元文件（脚本不在仓库检出内时必须提供）
  --registry-prefix <ref>   本机 registry 的镜像仓库前缀，
                            默认 127.0.0.1:5001/alcor/android-emulator
  --android-sdk-root <dir>  Android SDK 根目录，默认 /opt/android-sdk
  --android-tools-version <v> cmdline-tools 版本，默认 19.0
  --force-env               覆盖已存在的 host-agent.env
  --no-start                只安装，不 enable/start
  --check-only              只做前置检查，不改动系统
  -h, --help                显示本帮助
USAGE
}

server_url=""
host_id=""
token_file=""
advertise_host=""
bind_address=""
image=""
agent_binary=""
render_device=""
concurrency="1"
kvm_device="/dev/kvm"
install_root="${DEVICE_FARM_INSTALL_ROOT:-/opt/alcor-device-farm}"
env_dir="${DEVICE_FARM_ENV_DIR:-/etc/alcor-device-farm}"
unit_file=""
registry_prefix="127.0.0.1:5001/alcor/android-emulator"
android_sdk_root="/opt/android-sdk"
android_tools_version="19.0"
force_env="no"
do_start="yes"
check_only="no"

while [ $# -gt 0 ]; do
  case "$1" in
    --server-url) server_url="${2:-}"; shift 2 ;;
    --host-id) host_id="${2:-}"; shift 2 ;;
    --agent-token-file) token_file="${2:-}"; shift 2 ;;
    --advertise-host) advertise_host="${2:-}"; shift 2 ;;
    --bind-address) bind_address="${2:-}"; shift 2 ;;
    --image) image="${2:-}"; shift 2 ;;
    --agent-binary) agent_binary="${2:-}"; shift 2 ;;
    --render-device) render_device="${2:-}"; shift 2 ;;
    --concurrency) concurrency="${2:-}"; shift 2 ;;
    --kvm-device) kvm_device="${2:-}"; shift 2 ;;
    --install-root) install_root="${2:-}"; shift 2 ;;
    --env-dir) env_dir="${2:-}"; shift 2 ;;
    --unit-file) unit_file="${2:-}"; shift 2 ;;
    --registry-prefix) registry_prefix="${2:-}"; shift 2 ;;
    --android-sdk-root) android_sdk_root="${2:-}"; shift 2 ;;
    --android-tools-version) android_tools_version="${2:-}"; shift 2 ;;
    --force-env) force_env="yes"; shift ;;
    --no-start) do_start="no"; shift ;;
    --check-only) check_only="yes"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

fail() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "==> $*"; }

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

# 脚本可能被单独复制到目标机执行（目标机不必有仓库检出）。只有确实位于仓库检出
# 内时才能从仓库取单元模板；否则必须由调用方提供 --unit-file，或预置到固定路径。
repo_unit="${repository_root}/deploy/docker-emulator/alcor-device-host-agent.service"
if [ -f "${repo_unit}" ]; then
  :
elif [ -f "/opt/alcor-device-farm/deploy/docker-emulator/alcor-device-host-agent.service" ]; then
  repo_unit="/opt/alcor-device-farm/deploy/docker-emulator/alcor-device-host-agent.service"
else
  repo_unit=""
fi

# ------------------------------------------------------------ 0. 参数校验
# 先于任何系统检查，避免 --check-only 在缺少 --server-url 时被误判为通过。
[ -n "${server_url}" ] || fail "--server-url is required"
if [ "${check_only}" != "yes" ]; then
  [ -n "${host_id}" ] || fail "--host-id is required; register the host through the control plane first"
  [ -n "${agent_binary}" ] || fail "--agent-binary is required (see docs/host_onboarding_standard_process.md 3.1)"
  [ -f "${agent_binary}" ] || fail "agent binary ${agent_binary} not found"
  # 单元模板必须在改动系统之前就绪，避免「建了用户/写了配置却没装上服务」的半成品状态。
  if [ -z "${unit_file}" ] && [ -z "${repo_unit}" ]; then
    fail "cannot locate alcor-device-host-agent.service:
  this script is not running from a repository checkout and no --unit-file was given.
  Pass --unit-file /path/to/alcor-device-host-agent.service"
  fi
  if [ -n "${unit_file}" ] && [ ! -f "${unit_file}" ]; then
    fail "unit file ${unit_file} not found"
  fi
fi

# ---------------------------------------------------------------- 1. 前置检查
info "前置检查"

[ "$(uname -s)" = "Linux" ] || fail "device host must be Linux"
case "$(uname -m)" in
  x86_64|amd64) ;;
  *) fail "unsupported architecture $(uname -m); this farm targets x86_64" ;;
esac

if [ ! -c "${kvm_device}" ]; then
  fail "${kvm_device} is not a character device; enable VT-x/VMX in BIOS/UEFI and load kvm"
fi
if [ ! -r "${kvm_device}" ] || [ ! -w "${kvm_device}" ]; then
  fail "${kvm_device} must be readable and writable by this installer"
fi
getent group kvm >/dev/null || fail "kvm group does not exist; configure KVM first"
getent group docker >/dev/null || fail "docker group does not exist; install Docker Engine first"

command -v docker >/dev/null || fail "docker is not installed"
docker version >/dev/null 2>&1 || fail "cannot talk to the Docker daemon"

if [ -n "${render_device}" ] && [ ! -e "${render_device}" ]; then
  fail "render device ${render_device} does not exist"
fi

# 磁盘：每台模拟器约 15 GB 镜像 + 数据盘，低于 40 GB 直接拒绝。
docker_root=$(docker info --format '{{.DockerRootDir}}' 2>/dev/null || echo /var/lib/docker)
avail_mb=$(df -Pk "${docker_root}" 2>/dev/null | awk 'NR==2 {print int($4/1024)}')
if [ -n "${avail_mb}" ] && [ "${avail_mb}" -lt 40960 ]; then
  fail "only ${avail_mb} MiB free on ${docker_root}; at least 40 GiB is required for an emulator image"
fi

# 控制面可达性：健康端点必须返回 200。
if [ -n "${server_url}" ]; then
  base=$(printf '%s' "${server_url}" | sed -E 's#/+$##')
  case "${base}" in
    https*) curl_flag="-k" ;;
    *) curl_flag="" ;;
  esac
  code=$(curl ${curl_flag:+"$curl_flag"} -sS -m 15 -o /dev/null -w '%{http_code}' "${base}/healthz" 2>/dev/null || echo 000)
  [ "${code}" = "200" ] || fail "control plane ${base}/healthz returned ${code}; check network and TLS"
  info "control plane ${base} reachable (healthz=200)"
fi

if [ "${check_only}" = "yes" ]; then
  if [ -n "${token_file}" ] && [ ! -f "${token_file}" ]; then
    info "warning: --agent-token-file ${token_file} not readable (not required for --check-only)"
  fi
  info "check-only: all preconditions passed"
  exit 0
fi

# ------------------------------------------------------------ 2. 凭据
[ "$(id -u)" -eq 0 ] || fail "run this installer with sudo or as root"

token=""
if [ -f "${token_file}" ]; then
  token=$(tr -d '\r\n' < "${token_file}")
fi
[ -n "${token}" ] || fail "agent token file ${token_file} missing or empty"
[ "${#token}" -ge 32 ] || fail "agent token looks too short (${#token} chars)"

if [ -z "${advertise_host}" ]; then
  advertise_host=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i=="src") {print $(i+1); exit}}')
fi
[ -n "${advertise_host}" ] || fail "cannot determine advertise host; pass --advertise-host"
[ -n "${bind_address}" ] && : || bind_address="${advertise_host}"

info "host-id=${host_id} advertise=${advertise_host} bind=${bind_address}"
info "install-root=${install_root} env-dir=${env_dir}"

# --------------------------------------------------- 3. 系统用户与目录
# 角色专属系统账号，不可登录；加入 docker 与 kvm 两个组。
if ! id device-farm >/dev/null 2>&1; then
  nologin_shell=$(command -v nologin || true)
  [ -n "${nologin_shell}" ] || fail "nologin shell is required"
  useradd --system --home-dir /var/lib/device-farm --shell "${nologin_shell}" device-farm
  install -d -m 0700 -o device-farm -g device-farm /var/lib/device-farm
  info "created system user device-farm"
else
  info "system user device-farm already exists"
fi
usermod --append --groups docker,kvm device-farm
install -d -m 0700 -o device-farm -g device-farm /var/lib/device-farm

install -d -m 0755 "${install_root}/bin"
install -m 0755 "${agent_binary}" "${install_root}/bin/device-host-agent"
chown root:root "${install_root}/bin/device-host-agent"

# ------------------------------------------------------------ 4. 配置
install -d -m 0700 -o root -g root "${env_dir}"
env_file="${env_dir}/host-agent.env"

if [ -e "${env_file}" ] && [ "${force_env}" != "yes" ]; then
  info "${env_file} already exists; keeping it (use --force-env to overwrite)"
else
  umask 077
  {
    echo "DEVICE_FARM_AGENT_SERVER_URL=${server_url}"
    echo "DEVICE_FARM_AGENT_HOST_ID=${host_id}"
    echo "DEVICE_FARM_SECURITY_AGENT_TOKEN=${token}"
    echo "DEVICE_FARM_AGENT_PROVIDER=docker"
    echo "DEVICE_FARM_AGENT_CONCURRENCY=${concurrency}"
    echo "DEVICE_FARM_DOCKER_BINARY=docker"
    [ -n "${image}" ] && echo "DEVICE_FARM_DOCKER_IMAGE=${image}"
    echo "DEVICE_FARM_DOCKER_ADVERTISE_HOST=${advertise_host}"
    echo "DEVICE_FARM_DOCKER_BIND_ADDRESS=${bind_address}"
    echo "DEVICE_FARM_DOCKER_KVM_DEVICE=${kvm_device}"
    [ -n "${render_device}" ] && echo "DEVICE_FARM_DOCKER_RENDER_DEVICE=${render_device}"
    echo "DEVICE_FARM_DOCKER_ADB_PORT=5555"
    echo "DEVICE_FARM_DOCKER_APPIUM_PORT=4723"
    echo "DEVICE_FARM_DOCKER_ADB_SERIAL=emulator-5554"
    echo "DEVICE_FARM_APPIUM_HEALTH_TIMEOUT=5s"
    echo "DEVICE_FARM_DOCKER_DATA_MOUNT_PATH=/home/androidusr"
    echo 'DEVICE_FARM_DOCKER_EMULATOR_DEVICE="Pixel 9"'
    echo "DEVICE_FARM_DOCKER_CPUS=4"
    echo "DEVICE_FARM_DOCKER_MEMORY=5g"
    echo "DEVICE_FARM_DOCKER_PIDS_LIMIT=512"
    echo "DEVICE_FARM_AGENT_LEASE_SECONDS=300"
    echo "DEVICE_FARM_AGENT_COMMAND_TIMEOUT=900s"
    echo "HOME=/var/lib/device-farm"
    echo "DEVICE_FARM_DOCKER_APPIUM_ADDITIONAL_ARGS=--allow-insecure uiautomator2:chromedriver_autodownload"
    # 0 = 不设固定台数上限，按实时 CPU/内存/磁盘计算（见 host-agent.env.example）。
    echo "DEVICE_FARM_AGENT_DEVICE_SLOT_LIMIT=0"
    echo "DEVICE_FARM_DOCKER_DATA_ROOT=/var/lib/docker"
    # 官方镜像准备链路（ADR-0015 / DF-035）。本机 registry 后就地推送取 digest。
    echo "DEVICE_FARM_IMAGE_PREPARE_TIMEOUT=2h"
    echo "DEVICE_FARM_IMAGE_PREPARE_SCRIPT=${install_root}/deploy/docker-emulator/images/build-agent.sh"
    echo "DEVICE_FARM_SDKMANAGER_BINARY=${android_sdk_root}/cmdline-tools/${android_tools_version}/bin/sdkmanager"
    echo "DEVICE_FARM_SDKMANAGER_VERSION=${android_tools_version}"
    echo "DEVICE_FARM_ANDROID_PUBLISH_REPOSITORY=${registry_prefix}"
    echo "DEVICE_FARM_ADB_BINARY=adb"
  } > "${env_file}"
  chmod 600 "${env_file}"
  chown root:root "${env_file}"
  info "wrote ${env_file} (0600 root:root)"
fi

# ------------------------------------------------------------ 5. systemd
unit_source="${unit_file:-${repo_unit}}"
[ -n "${unit_source}" ] || fail "cannot locate alcor-device-host-agent.service:
  this script is not running from a repository checkout and no --unit-file was given.
  Pass --unit-file /path/to/alcor-device-host-agent.service"
[ -f "${unit_source}" ] || fail "unit file ${unit_source} not found"
install -m 0644 "${unit_source}" /etc/systemd/system/alcor-device-host-agent.service
systemctl daemon-reload

if [ "${do_start}" = "yes" ]; then
  systemctl enable --now alcor-device-host-agent.service
else
  info "installed but not started (--no-start)"
fi

# ------------------------------------------------------------ 6. 自检
echo
info "自检"
printf '  agent binary : '; "${install_root}/bin/device-host-agent" --version 2>&1 | head -1 || true
printf '  unit state   : '; systemctl is-active alcor-device-host-agent.service 2>&1 || true
printf '  enabled      : '; systemctl is-enabled alcor-device-host-agent.service 2>&1 || true
printf '  restarts     : '; systemctl show -p NRestarts --value alcor-device-host-agent.service 2>&1 || true
printf '  device-farm  : '; id -nG device-farm 2>&1 || true

if [ "${do_start}" = "yes" ]; then
  info "等待心跳（最多 60 秒）"
  waited=0
  while [ "${waited}" -lt 60 ]; do
    if systemctl is-active --quiet alcor-device-host-agent.service; then
      break
    fi
    sleep 3
    waited=$((waited + 3))
  done
fi

cat <<EOF

安装完成。接下来必须由管理员确认（本脚本不做）：
  1. 控制面里该宿主 status=online 且心跳新鲜：
       GET /api/v1/device-hosts  (用 Console 或服务 Token)
  2. 该宿主具备池默认镜像的本地 digest：
       docker image inspect <image-ref> --format 'ID={{.Id}} RepoDigests={{.RepoDigests}}'
     与 device_images.docker_digest 比对；缺失时按标准流程第 5 节搬运。
  3. 选池自查 SQL 中该池有一台「可建机」宿主（标准流程第 6 节）。

排障：
  journalctl -u alcor-device-host-agent.service -n 50 --no-pager
EOF
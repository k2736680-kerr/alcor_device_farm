#!/usr/bin/env sh
# 把一台「全新 Ubuntu」服务器引导成 Android 模拟器宿主机（ADR-0040）。
#
# 这是接入设备农场的第 0 步（Bootstrap）。负责装 Docker、配置 KVM 组与权限、
# 装 Android command-line tools；第 1 步才是 scripts/onboard-device-host.sh
# （装 Host Agent），第 2 步是镜像分发（scripts/install-local-image-registry.sh
# + scripts/distribute-emulator-image.sh）。
#
# 为什么分开：Bootstrap 需要 root 和网络，会改动机器级配置（docker 源、KVM 模块、
# 系统组），风险和范围都与「装一个农场 Agent」不同，混在一起会让回滚和排障变难。
#
# 用法（在目标宿主机本地，需 root）：
#   sudo scripts/bootstrap-device-host.sh --yes
#
# 幂等：重复执行安全。已安装的组件会被跳过。
set -eu

usage() {
  cat <<'USAGE'
usage: bootstrap-device-host.sh [options]

optional:
  --yes                      非交互确认（自动化必须有）
  --skip-docker              不安装 Docker
  --skip-kvm                 不处理 KVM 配置
  --skip-android-tools       不安装 Android command-line tools
  --android-tools-version <v>  版本号，默认 19.0
  --android-sdk-root <dir>   SDK 根目录，默认 /opt/android-sdk
  --docker-version <ver>     指定 Docker 版本；默认装发行版仓库版本
  --check-only               只检查现状，不改动系统
  -h, --help                 显示本帮助
USAGE
}

assume_yes="no"
skip_docker="no"
skip_kvm="no"
skip_android="no"
android_tools_version="19.0"
android_sdk_root="/opt/android-sdk"
docker_version=""
check_only="no"

while [ $# -gt 0 ]; do
  case "$1" in
    --yes) assume_yes="yes"; shift ;;
    --skip-docker) skip_docker="yes"; shift ;;
    --skip-kvm) skip_kvm="yes"; shift ;;
    --skip-android-tools) skip_android="yes"; shift ;;
    --android-tools-version) android_tools_version="${2:-}"; shift 2 ;;
    --android-sdk-root) android_sdk_root="${2:-}"; shift 2 ;;
    --docker-version) docker_version="${2:-}"; shift 2 ;;
    --check-only) check_only="yes"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

fail() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "==> $*"; }
already() { echo "    [已满足] $*"; }

# --check-only 是只读勘察，不需要 root；真正的改动才要求 root。
if [ "${check_only}" != "yes" ]; then
  [ "$(id -u)" -eq 0 ] || fail "run this bootstrap with sudo or as root"
fi

# ------------------------------------------------------------ 0. 系统识别
info "系统检查"
[ "$(uname -s)" = "Linux" ] || fail "this bootstrap targets Linux only"
case "$(uname -m)" in
  x86_64|amd64) ;;
  *) fail "unsupported architecture $(uname -m); this farm targets x86_64" ;;
esac

os_id=""
os_version=""
if [ -r /etc/os-release ]; then
  # shellcheck disable=SC1091
  os_id=$(. /etc/os-release && printf '%s' "${ID:-}")
  os_version=$(. /etc/os-release && printf '%s' "${VERSION_ID:-}")
fi
info "  ${os_id} ${os_version} / $(uname -r)"

case "${os_id}" in
  ubuntu|debian) ;;
  *) fail "unsupported distribution '${os_id}'; this bootstrap supports ubuntu/debian" ;;
esac

if [ "${check_only}" = "yes" ]; then
  echo
  info "check-only 现状"
  printf '  docker         : '; (command -v docker >/dev/null && docker version --format '{{.Server.Version}}' 2>/dev/null) || echo '未安装'
  printf '  /dev/kvm       : '; ([ -c /dev/kvm ] && echo '存在') || echo '缺失'
  printf '  kvm 组         : '; (getent group kvm >/dev/null && echo '存在') || echo '缺失'
  printf '  docker 组      : '; (getent group docker >/dev/null && echo '存在') || echo '缺失'
  printf '  nested         : '; (cat /sys/module/kvm_intel/parameters/nested 2>/dev/null || cat /sys/module/kvm_amd/parameters/nested 2>/dev/null) || echo '未知'
  printf '  sdkmanager     : '; ([ -x "${android_sdk_root}/cmdline-tools/${android_tools_version}/bin/sdkmanager" ] && echo '存在') || echo '缺失'
  printf '  本机 registry  : '; (curl -sS -m 3 -o /dev/null -w '%{http_code}\n' http://127.0.0.1:5001/v2/ 2>/dev/null) || echo '未安装'
  exit 0
fi

if [ "${assume_yes}" != "yes" ]; then
  echo
  echo "即将在本机执行："
  [ "${skip_docker}" = "no" ] && echo "  - 安装 Docker Engine（若缺失）"
  [ "${skip_kvm}" = "no" ] && echo "  - 确保 kvm 组与 /dev/kvm 权限正确"
  [ "${skip_android}" = "no" ] && echo "  - 安装 Android command-line tools ${android_tools_version}"
  echo
  printf "继续？[y/N] "
  read -r answer
  case "${answer}" in y|Y|yes|YES) ;; *) echo "已取消"; exit 0 ;; esac
fi

export DEBIAN_FRONTEND=noninteractive

# ------------------------------------------------------------ 1. Docker
if [ "${skip_docker}" = "no" ]; then
  info "Docker"
  if command -v docker >/dev/null 2>&1 && docker version >/dev/null 2>&1; then
    already "docker $(docker version --format '{{.Server.Version}}' 2>/dev/null)"
  else
    info "  安装 docker.io"
    apt-get update -qq
    if [ -n "${docker_version}" ]; then
      apt-get install -y -qq "docker.io=${docker_version}" 2>/dev/null \
        || fail "requested docker version ${docker_version} is not available"
    else
      apt-get install -y -qq docker.io
    fi
    systemctl enable --now docker
    sleep 2
    docker version >/dev/null 2>&1 || fail "docker installed but daemon is not reachable"
    info "  docker 就绪"
  fi

  # 日志轮转：Docker 默认 json-file 无上限，长期运行会把磁盘写满。
  # 171 就缺这一项（见 fleet_health_sweep_20260915.md §4.1）。
  daemon_json="/etc/docker/daemon.json"
  if [ -w "${daemon_json}" ] || [ ! -e "${daemon_json}" ]; then
    if ! grep -q 'max-size' "${daemon_json}" 2>/dev/null; then
      info "  配置 docker 日志轮转"
      if [ -e "${daemon_json}" ]; then
        cp -a "${daemon_json}" "${daemon_json}.alcor-backup"
        info "  已备份到 ${daemon_json}.alcor-backup（保留原有配置，未覆盖）"
        echo "    注意：需要手工把 log-driver/log-opts 合并进既有 daemon.json。"
      else
        cat > "${daemon_json}" <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": {"max-size": "100m", "max-file": "5"}
}
EOF
        systemctl restart docker
        info "  已写入并重启 docker"
      fi
    else
      already "docker 日志轮转已配置"
    fi
  fi
fi

# ------------------------------------------------------------ 2. KVM
if [ "${skip_kvm}" = "no" ]; then
  info "KVM"
  if [ ! -e /dev/kvm ]; then
    info "  /dev/kvm 缺失，尝试加载模块"
    modprobe kvm || true
    case "$(grep -c -m1 -E 'vmx|svm' /proc/cpuinfo 2>/dev/null || echo 0)" in
      0) fail "/dev/kvm missing and CPU reports no virtualization extension; enable VT-x/AMD-V in BIOS/UEFI" ;;
    esac
    if grep -q 'vmx' /proc/cpuinfo; then modprobe kvm_intel || true; else modprobe kvm_amd || true; fi
    sleep 1
  fi
  [ -c /dev/kvm ] || fail "/dev/kvm still missing after loading modules; check BIOS/UEFI virtualization settings"

  getent group kvm >/dev/null || groupadd --system kvm
  # /dev/kvm 归属 root:kvm 且组可读写，是容器内跑模拟器的前提。
  chown root:kvm /dev/kvm
  chmod 0660 /dev/kvm
  info "  /dev/kvm 就绪（root:kvm 0660）"

  # 持久化：重启后由 udev 规则恢复权限。
  udev_rule="/etc/udev/rules.d/99-alcor-kvm.rules"
  if [ ! -e "${udev_rule}" ]; then
    echo 'KERNEL=="kvm", GROUP="kvm", MODE="0660"' > "${udev_rule}"
    udevadm control --reload-rules 2>/dev/null || true
    info "  已写入 udev 规则 ${udev_rule}"
  else
    already "udev 规则已存在"
  fi

  nested_i=$(cat /sys/module/kvm_intel/parameters/nested 2>/dev/null || echo "")
  nested_a=$(cat /sys/module/kvm_amd/parameters/nested 2>/dev/null || echo "")
  info "  嵌套虚拟化: ${nested_i:-${nested_a:-未知}}（模拟器不需要，可忽略）"
fi

# ------------------------------------------------------------ 3. Android tools
if [ "${skip_android}" = "no" ]; then
  info "Android command-line tools"
  sdkmanager_path="${android_sdk_root}/cmdline-tools/${android_tools_version}/bin/sdkmanager"
  if [ -x "${sdkmanager_path}" ]; then
    already "sdkmanager 已存在于 ${sdkmanager_path}"
  else
    info "  安装 cmdline-tools ${android_tools_version} 到 ${android_sdk_root}"
    command -v unzip >/dev/null || apt-get install -y -qq unzip
    command -v java >/dev/null || {
      info "  安装 JDK"
      apt-get install -y -qq default-jre-headless
    }
    install -d -m 0755 "${android_sdk_root}/cmdline-tools"
    tmp_zip="/tmp/commandlinetools-linux-${android_tools_version}.zip"
    url="https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip"
    if curl -fsSL -m 300 -o "${tmp_zip}" "${url}"; then
      rm -rf "${android_sdk_root}/cmdline-tools/${android_tools_version}"
      install -d -m 0755 "${android_sdk_root}/cmdline-tools/${android_tools_version}"
      unzip -q -o "${tmp_zip}" -d "${android_sdk_root}/cmdline-tools/${android_tools_version}"
      # 上游 zip 解出的是 cmdline-tools/，需要在版本目录下再套一层。
      if [ -d "${android_sdk_root}/cmdline-tools/${android_tools_version}/cmdline-tools" ]; then
        mv "${android_sdk_root}/cmdline-tools/${android_tools_version}/cmdline-tools"/* \
           "${android_sdk_root}/cmdline-tools/${android_tools_version}/" 2>/dev/null || true
        rmdir "${android_sdk_root}/cmdline-tools/${android_tools_version}/cmdline-tools" 2>/dev/null || true
      fi
      chmod +x "${android_sdk_root}/cmdline-tools/${android_tools_version}/bin/"* 2>/dev/null || true
      rm -f "${tmp_zip}"
      if [ -x "${sdkmanager_path}" ]; then
        info "  sdkmanager 就绪"
      else
        fail "sdkmanager not found at ${sdkmanager_path} after extraction"
      fi
    else
      fail "cannot download Android command-line tools; the host may have no internet access"
    fi
  fi
fi

# ------------------------------------------------------------ 4. 汇总
echo
info "Bootstrap 完成"
printf '  docker         : '; (docker version --format '{{.Server.Version}}' 2>/dev/null) || echo '未安装'
printf '  /dev/kvm       : '; ls -l /dev/kvm 2>/dev/null || echo '缺失'
printf '  kvm 组         : '; getent group kvm || echo '缺失'
printf '  docker 组      : '; getent group docker || echo '缺失'
printf '  sdkmanager     : '; ([ -x "${android_sdk_root}/cmdline-tools/${android_tools_version}/bin/sdkmanager" ] && echo "${android_sdk_root}/cmdline-tools/${android_tools_version}/bin/sdkmanager") || echo '缺失'

cat <<EOF

下一步：
  1. 把要运行农场的用户加入 docker 与 kvm 组（需要重新登录生效）：
       usermod --append --groups docker,kvm <user>
  2. 装 Host Agent：scripts/onboard-device-host.sh
  3. 装本机 registry：scripts/install-local-image-registry.sh
  4. 导入镜像：scripts/distribute-emulator-image.sh
     完整顺序见 docs/host_onboarding_standard_process.md
EOF
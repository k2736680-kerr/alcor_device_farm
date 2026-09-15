#!/usr/bin/env bash
# =============================================================================
# 10.0.30.55 (worker-05) 接入 alcor 设备农场 —— 宿主机环境准备脚本
#
# 用途：把一台空白 Ubuntu 24.04 装成可运行 Host Agent 的安卓模拟器宿主机。
# 前置：已实测该机器为 x86_64 / Ubuntu 24.04 / 有 /dev/kvm / 有 sudo。
# 本脚本只装"环境"，不装 Host Agent 本身（那是下一步，见文末）。
#
# 用法：  sudo bash prepare-docker-host.sh
# =============================================================================
set -euo pipefail

GO_VERSION="1.24.6"
STEP=0
say() { STEP=$((STEP+1)); echo; echo "===== [${STEP}] $* ====="; }
die() { echo "[FAIL] $*" >&2; exit 1; }

[[ $(id -u) -eq 0 ]] || die "必须用 root 运行：sudo bash $0"
[[ "$(uname -s)" == "Linux" ]] || die "只支持 Linux"
[[ "$(uname -m)" == "x86_64" ]] || die "只支持 x86_64（当前 $(uname -m)）"

# -----------------------------------------------------------------------------
say "校验 KVM 可用性"
[[ -c /dev/kvm ]] || die "/dev/kvm 不存在，这台机器不支持嵌套虚拟化，无法跑 Android 模拟器"
[[ -r /dev/kvm && -w /dev/kvm ]] || die "/dev/kvm 不可读写，请检查宿主机 BIOS 的 VT-x/AMD-V 与容器权限"
echo "OK: /dev/kvm 存在且可读写"
echo "CPU 虚拟化标志数: $(grep -c -E '(vmx|svm)' /proc/cpuinfo)"

# -----------------------------------------------------------------------------
say "安装 Docker Engine + CLI"
if command -v docker >/dev/null 2>&1; then
  echo "已安装：$(docker --version)"
else
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl gnupg
  # 用 Ubuntu 仓库的 docker.io（不引入 docker.com 外部源，减少网络依赖）
  apt-get install -y -qq docker.io
  systemctl enable --now docker
  echo "已安装：$(docker --version)"
fi
systemctl is-active --quiet docker || die "docker 服务未能启动"
echo "OK: docker 服务 active"

# 把 hadoop 用户加进 docker 组，省得每次 sudo
if id hadoop >/dev/null 2>&1; then
  usermod -aG docker hadoop
  echo "OK: 已把 hadoop 加入 docker 组（需重新登录生效）"
fi

# -----------------------------------------------------------------------------
say "把 hadoop 用户加进 kvm 组（免 sudo 用 KVM）"
if id hadoop >/dev/null 2>&1; then
  usermod -aG kvm hadoop
  echo "OK: 已把 hadoop 加入 kvm 组"
fi

# -----------------------------------------------------------------------------
say "安装 Go ${GO_VERSION}"
if command -v go >/dev/null 2>&1; then
  echo "已安装：$(go version)"
else
  ARCH=$(uname -m)
  TARBALL="go${GO_VERSION}.linux-${ARCH}.tar.gz"
  URL="https://go.dev/dl/${TARBALL}"
  echo "下载 ${URL}"
  if curl -fsSL --retry 3 --connect-timeout 20 -o "/tmp/${TARBALL}" "$URL"; then
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "/tmp/${TARBALL}"
    rm -f "/tmp/${TARBALL}"
    # 全局可用
    cat >/etc/profile.d/go.sh <<'EOF'
export PATH=$PATH:/usr/local/go/bin
EOF
    chmod 0644 /etc/profile.d/go.sh
    export PATH="$PATH:/usr/local/go/bin"
    echo "已安装：$(go version)"
  else
    die "Go 下载失败。请检查这台机器能否访问 go.dev，或手工下载 ${TARBALL} 解压到 /usr/local/go"
  fi
fi

# -----------------------------------------------------------------------------
say "环境自检"
echo "--- 内核 / 发行版 ---"; uname -srm; head -2 /etc/os-release
echo "--- CPU 核数 ---"; nproc
echo "--- 内存 ---"; free -m | head -2
echo "--- 磁盘 ---"; df -h / | tail -1
echo "--- KVM ---"; ls -l /dev/kvm
echo "--- Docker ---"; docker --version; docker info --format '{{.ServerVersion}} / {{.Driver}}' 2>&1 | head -1
echo "--- Go ---"; /usr/local/go/bin/go version 2>&1 || go version

cat <<'NEXT'

=============================================================================
环境准备完成。接下来在【控制台】和【本机】各做一件事：
=============================================================================

【A. 在控制台】打开 设备农场 Console →「登记宿主机」，填写：
     宿主机名称 : 安卓宿主机 - 10.0.30.55
     操作系统   : linux
     宿主机能力 : docker_emulator
     架构       : amd64
     内网地址   : 10.0.30.55
   登记后会得到一个 Host ID，形如 xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

【B. 回到这台机器】跑 Host Agent 安装脚本（源码目录下）：
     sudo bash scripts/install-device-host-agent.sh

   然后编辑 /etc/alcor-device-farm/host-agent.env，至少填这几项：
     DEVICE_FARM_AGENT_PROVIDER=docker
     DEVICE_FARM_AGENT_SERVER_URL=http://10.0.80.220:18182
     DEVICE_FARM_AGENT_HOST_ID=<上面拿到的 Host ID>
     DEVICE_FARM_SECURITY_AGENT_TOKEN=<从控制台受控 Secret 取>
     DEVICE_FARM_DOCKER_IMAGE=<Android 模拟器镜像>
     DEVICE_FARM_DOCKER_ADVERTISE_HOST=10.0.30.55

   然后：
     sudo systemctl enable --now alcor-device-host-agent
     sudo systemctl status alcor-device-host-agent

【C. 建新设备池】本机成为宿主机后，新建一个绑定它的 android 池：
     name         : android-10.0.30.55
     platform     : android
     host_id      : <上面拿到的 Host ID>
     total_target : 1        # 内存实测只能稳挂 1 台
     min_ready    : 1
     base_device_id   : 需先在 55 上创建一台模板设备
     default_image_id : 复用 android-10.0.30.171 池的镜像

NEXT
echo "DONE"

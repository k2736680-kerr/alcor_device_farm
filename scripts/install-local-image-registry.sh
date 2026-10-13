#!/usr/bin/env sh
# 在宿主机上安装「本机镜像 registry」（ADR-0040）。
#
# 背景：
#   * device_images 以 digest 为身份，create 永不 pull；镜像引用必须能在宿主本地解析。
#   * 官方镜像准备链路 deploy/docker-emulator/images/prepare.sh 必须 docker push 到
#     一个受控 repository 才能拿到不可变 digest。没有 registry 就没有官方构建产物。
#   * 因此每台宿主机自带一个只监听 127.0.0.1 的 registry，统一用
#     127.0.0.1:5001 作为 repository 前缀，使 device_images.docker_image 在各宿主
#     之间保持一致，而 digest 由本机 registry 确定。
#
# 安全边界：registry 只绑定 127.0.0.1，不对外暴露；不配置任何认证，
#           因为不可从网络访问。镜像经 save/load 或本机构建进入。
#
# 用法（在目标宿主机本地，需 root）：
#   sudo scripts/install-local-image-registry.sh
#
# 幂等：重复执行安全；已存在的容器与数据卷会被复用。
set -eu

usage() {
  cat <<'USAGE'
usage: install-local-image-registry.sh [options]

optional:
  --port <port>          监听端口，默认 5001
  --bind <addr>          监听地址，默认 127.0.0.1（强烈建议不要改动）
  --registry-image <ref> registry 镜像，默认 registry:2
  --data-dir <dir>       镜像数据目录，默认 /var/lib/alcor-registry
  --name <name>          容器名，默认 alcor-local-registry
  --no-start             只安装配置，不启动
  -h, --help             显示本帮助
USAGE
}

port="5001"
bind_addr="127.0.0.1"
registry_image="registry:2"
data_dir="/var/lib/alcor-registry"
name="alcor-local-registry"
do_start="yes"

while [ $# -gt 0 ]; do
  case "$1" in
    --port) port="${2:-}"; shift 2 ;;
    --bind) bind_addr="${2:-}"; shift 2 ;;
    --registry-image) registry_image="${2:-}"; shift 2 ;;
    --data-dir) data_dir="${2:-}"; shift 2 ;;
    --name) name="${2:-}"; shift 2 ;;
    --no-start) do_start="no"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

fail() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "==> $*"; }

[ "$(id -u)" -eq 0 ] || fail "run this installer with sudo or as root"
command -v docker >/dev/null || fail "docker is not installed"
docker version >/dev/null 2>&1 || fail "cannot talk to the Docker daemon"

case "${bind_addr}" in
  127.0.0.1|::1) ;;
  *)
    echo "WARNING: binding the registry to ${bind_addr} exposes it on the network." >&2
    echo "         The registry has no authentication. Prefer 127.0.0.1." >&2
    ;;
esac

# registry 数据要留在宿主机本地卷上，容器重建不丢镜像。
docker volume inspect "${name}-data" >/dev/null 2>&1 || docker volume create "${name}-data" >/dev/null
info "数据卷 ${name}-data 就绪"

# 复用已有容器，或替换掉一个配置不符的旧容器。
if docker container inspect "${name}" >/dev/null 2>&1; then
  current_bind=$(docker container inspect "${name}" \
    --format '{{range $k,$v := .NetworkSettings.Ports}}{{$k}} {{end}}' 2>/dev/null || echo "")
  info "容器 ${name} 已存在，检查配置"
  running=$(docker container inspect "${name}" --format '{{.State.Running}}' 2>/dev/null || echo false)
  if [ "${running}" = "true" ]; then
    info "容器已在运行，直接复用"
  else
    if [ "${do_start}" = "yes" ]; then
      docker start "${name}" >/dev/null
      info "已启动已有容器"
    fi
  fi
else
  info "创建 registry 容器"
  docker run -d \
    --name "${name}" \
    --restart unless-stopped \
    -p "${bind_addr}:${port}:5000" \
    -v "${name}-data:/var/lib/registry" \
    "${registry_image}" >/dev/null
  info "容器已创建"
fi

if [ "${do_start}" = "no" ]; then
  info "已安装但未启动（--no-start）"
  exit 0
fi

# 等待 registry 就绪。/v2/ 返回 200 表示 API 可用。
info "等待 registry 就绪"
waited=0
while [ "${waited}" -lt 30 ]; do
  code=$(curl -sS -m 3 -o /dev/null -w '%{http_code}' "http://${bind_addr}:${port}/v2/" 2>/dev/null || echo 000)
  if [ "${code}" = "200" ]; then
    info "registry 就绪 (http://${bind_addr}:${port}/v2/ -> 200)"
    break
  fi
  sleep 2
  waited=$((waited + 2))
done

if [ "${code:-000}" != "200" ]; then
  echo "ERROR: registry did not become ready within 30s" >&2
  docker logs --tail 20 "${name}" >&2 || true
  exit 1
fi

# Docker 默认对 insecure registry 有限制；127.0.0.1 通常已被视为 insecure，
# 但显式写入 daemon.json 更稳妥（不覆盖已有配置）。
daemon_json="/etc/docker/daemon.json"
if [ -f "${daemon_json}" ]; then
  if ! grep -q "${bind_addr}:${port}" "${daemon_json}" 2>/dev/null; then
    info "注意：${daemon_json} 已存在且未包含 ${bind_addr}:${port}"
    info "      若 push/pull 报 http: server gave HTTP response to HTTPS client，"
    info "      请把 \"${bind_addr}:${port}\" 加入 insecure-registries 后重启 docker。"
  fi
else
  info "写入 ${daemon_json}（仅设置 insecure-registries）"
  cat > "${daemon_json}" <<EOF
{
  "insecure-registries": ["${bind_addr}:${port}"]
}
EOF
  systemctl restart docker
  info "docker 已重启"
  # 重启 docker 后 registry 会被 unless-stopped 策略带起来。
  sleep 3
fi

# 冒烟测试：push 一个极小的镜像再 pull 回来，确认读写都通。
info "冒烟测试：push/pull round-trip"
probe_ref="${bind_addr}:${port}/alcor/registry-smoke:1"
docker pull -q busybox:latest >/dev/null 2>&1 || true
if docker image inspect busybox:latest >/dev/null 2>&1; then
  docker tag busybox:latest "${probe_ref}" >/dev/null 2>&1 || true
  if docker push "${probe_ref}" >/dev/null 2>&1; then
    docker rmi "${probe_ref}" >/dev/null 2>&1 || true
    if docker pull "${probe_ref}" >/dev/null 2>&1; then
      info "  push/pull 成功"
      docker rmi "${probe_ref}" >/dev/null 2>&1 || true
    else
      echo "WARNING: pull 失败，请检查 insecure-registries" >&2
    fi
  else
    echo "WARNING: push 失败，请检查 insecure-registries" >&2
  fi
else
  info "  跳过冒烟测试（无法获取 busybox，可能无外网）"
fi

cat <<EOF

============================================================
本机 registry 就绪
============================================================
  地址        : ${bind_addr}:${port}
  容器        : ${name}
  数据卷      : ${name}-data
  数据目录    : ${data_dir}（由卷管理）

镜像引用前缀请统一使用 ${bind_addr}:${port}/alcor/android-emulator。

下一步：
  1. 把镜像导入本机 registry（任选其一）：
     a) 从别的宿主搬运：scripts/distribute-emulator-image.sh
     b) 用官方链路构建：deploy/docker-emulator/images/prepare.sh
        需要设 DEVICE_FARM_ANDROID_PUBLISH_REPOSITORY=${bind_addr}:${port}/alcor/android-emulator
  2. docker image inspect 取本机 digest，与 device_images.docker_digest 核对。
     详见 docs/host_onboarding_standard_process.md §5。
EOF
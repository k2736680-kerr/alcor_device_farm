#!/usr/bin/env sh
# 把「接入一台宿主机」所需的全部脚本与单元模板打成可分发目录/tar.gz。
#
# 目的：install-device-host.sh 要求五个脚本同级同行；手工逐个拷容易漏，
# 也会污染目标机。这里一次产出整齐的一套，便于放到部署机或随身携带。
#
# 用法：
#   scripts/package-host-toolkit.sh
#   scripts/package-host-toolkit.sh --out /tmp/alcor-host-toolkit --with-agent /path/to/device-host-agent
set -eu

usage() {
  cat <<'USAGE'
usage: package-host-toolkit.sh [options]

optional:
  --out <dir>           输出目录，默认 <仓库>/dist/alcor-host-toolkit
  --with-agent <path>   一并放入 device-host-agent（否则目标机需自带或另传）
  --unit-file <path>    单元模板路径（脱离仓库运行时需要）
  --no-tar              只生成目录，不打包 tar.gz
  -h, --help            显示本帮助

产出结构：
  alcor-host-toolkit/
    install-device-host.sh          # 一条命令入口
    bootstrap-device-host.sh
    install-local-image-registry.sh
    onboard-device-host.sh
    distribute-emulator-image.sh
    alcor-device-host-agent.service  # 单元模板
    device-host-agent                # 仅 --with-agent 时存在
    README.txt
USAGE
}

out_dir=""
with_agent=""
make_tar="yes"
unit_arg=""

while [ $# -gt 0 ]; do
  case "$1" in
    --out) out_dir="${2:-}"; shift 2 ;;
    --with-agent) with_agent="${2:-}"; shift 2 ;;
    --unit-file) unit_arg="${2:-}"; shift 2 ;;
    --no-tar) make_tar="no"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

fail() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "==> $*"; }

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "${script_dir}/.." && pwd)

[ -n "${out_dir}" ] || out_dir="${repo_root}/dist/alcor-host-toolkit"

scripts="install-device-host.sh bootstrap-device-host.sh install-local-image-registry.sh onboard-device-host.sh distribute-emulator-image.sh"
for s in ${scripts}; do
  [ -f "${script_dir}/${s}" ] || fail "缺少 ${script_dir}/${s}"
done

unit=""
for cand in "${unit_arg}" \
            "${repo_root}/deploy/docker-emulator/alcor-device-host-agent.service" \
            "${script_dir}/alcor-device-host-agent.service" \
            "/etc/systemd/system/alcor-device-host-agent.service"; do
  if [ -n "${cand}" ] && [ -f "${cand}" ]; then
    unit="${cand}"
    break
  fi
done
[ -n "${unit}" ] || fail "找不到单元模板（alcor-device-host-agent.service）。
  从仓库运行时应在 deploy/docker-emulator/ 下；
  脱离仓库时请用 --unit-file 指定，或从已有宿主取：
    scp hadoop@10.0.30.54:/etc/systemd/system/alcor-device-host-agent.service ."

rm -rf "${out_dir}"
mkdir -p "${out_dir}"

for s in ${scripts}; do
  cp "${script_dir}/${s}" "${out_dir}/${s}"
  chmod 0755 "${out_dir}/${s}"
done
cp "${unit}" "${out_dir}/alcor-device-host-agent.service"
chmod 0644 "${out_dir}/alcor-device-host-agent.service"

if [ -n "${with_agent}" ]; then
  [ -f "${with_agent}" ] || fail "找不到 --with-agent 指定的二进制: ${with_agent}"
  cp "${with_agent}" "${out_dir}/device-host-agent"
  chmod 0755 "${out_dir}/device-host-agent"
  agent_note="已内置 device-host-agent（$(basename "${with_agent}")）"
else
  agent_note="未内置 device-host-agent；请在目标机自带或用 --agent-binary 指定"
fi

cat > "${out_dir}/README.txt" <<EOF
Alcor 设备农场 · 宿主机接入工具包
生成时间：$(date -u '+%Y-%m-%dT%H:%M:%SZ')

一条命令接入（在部署机上执行，目标机需可 SSH）：
  sh install-device-host.sh \\
    --host <目标IP> \\
    --host-id <控制面 Host UUID> \\
    --pool-image-id <device_images 记录 UUID> \\
    --token-file <服务Token文件> \\
    --ssh-user <用户> --ssh-password '<密码>' \\
    --image-source 10.0.30.54 --image-source-user hadoop --image-source-password '<密码>'

先看它打算做什么：
  sh install-device-host.sh --host <目标IP> --dry-run

Agent 二进制：${agent_note}
若未内置：scp hadoop@10.0.30.54:/opt/alcor-device-farm/bin/device-host-agent ./device-host-agent

完整流程与踩坑见 docs/host_onboarding_standard_process.md。
EOF

info "工具包已生成：${out_dir}"
ls -l "${out_dir}"

if [ "${make_tar}" = "yes" ]; then
  tar_gz="${out_dir}.tar.gz"
  ( cd "$(dirname -- "${out_dir}")" && tar czf "${tar_gz}" "$(basename -- "${out_dir}")" )
  info "已打包：${tar_gz}"
fi
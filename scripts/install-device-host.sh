#!/usr/bin/env sh
# 设备农场宿主机「一条命令」接入器：把一个全新 IP 接到农场，直到可以真机建机。
#
# 它把标准流程（docs/host_onboarding_standard_process.md §9.5）串成一个入口：
#   0. Bootstrap（docker / KVM / Android cmdline-tools）        —— 全新机器才需要
#   1. 本机镜像 registry（127.0.0.1:5001）
#   2. 把模拟器镜像搬到本机（从既有宿主经部署机中转）
#   3. 校验本机 digest
#   4. 装 Host Agent（onboard-device-host.sh）
#   5. 等待控制面看到 status=online
#   6. 建池（total_target=0 / min_ready=0，按需模式）
#   7. 选池自查
#
# 刻意不做（避免越权/误判）：
#   * 不删任何镜像与负向记录；
#   * 不改已有池的容量；
#   * 不在控制面删 Host。
#
# 用法：
#   scripts/install-device-host.sh \
#     --host 10.0.30.56 \
#     --host-id <控制面里的 Host UUID> \
#     --image-source 10.0.30.54 \
#     --registry-image <控制面里 device_images 记录的 UUID>
#
## 说明：--host-id 与 --registry-image 需要在控制面先登记。
##       用 --register 可以让脚本自己登记 Host（需要服务 Token）。
##
## 重要：本脚本必须与它的四个同级脚本放在同一目录一起分发：
##   bootstrap-device-host.sh / install-local-image-registry.sh /
##   onboard-device-host.sh / distribute-emulator-image.sh
## 缺任何一个都会在改动系统之前直接报错退出。
## 打包：sh scripts/package-host-toolkit.sh   （生成 tar.gz，整包拷到目标机旁即可）
set -eu

usage() {
  cat <<'USAGE'
usage: install-device-host.sh --host <ip> [options]

required:
  --host <ip>                目标宿主机 IP

optional（默认值已按现有农场填好）：
  --host-id <uuid>           控制面里的 Host UUID；不传则需 --register
  --register                 调用控制面 API 自动登记 Host（需 --server-url 与 --token-file）
  --server-url <url>         控制面管理 API，默认 https://10.0.80.220:18180
  --token-file <path>        服务 Token 文件（仅 --register / 建池时需要）
  --agent-token-file <path>  Agent Token 文件（默认与 token-file 相同）
  --pool-name <name>         池名，默认 android-<host>
  --pool-image-id <uuid>     池默认镜像记录 UUID（device_images.id）
  --image <ref>              镜像引用，默认 127.0.0.1:5001/alcor/android-emulator:15.0-api35-google_apis-x86_64-sdk9
  --image-source <ip>        已有该镜像的宿主，默认 10.0.30.54
  --image-source-user <user> 默认 hadoop
  --image-source-password <p> 源机密码（未装密钥时）
  --ssh-user <user>          目标机 SSH 用户，默认 hadoop
  --ssh-password <p>         目标机 SSH 密码（未装密钥时）
  --agent-binary <path>      device-host-agent 二进制
  --unit-file <path>         systemd 单元模板
  --concurrency <n>          池上限安全网，默认 8
                             （单机实际开几台由该机实时剩余内存决定；
                              设小了会成为瓶颈，见 docs/evidence/concurrency_policy_20261013.md）
  --skip-bootstrap           跳过第 0 步
  --skip-image               跳过第 2 步（镜像已在目标机）
  --dry-run                  只打印将执行的步骤
  -h, --help                 显示本帮助
USAGE
}

host=""
host_id=""
register="no"
server_url="https://10.0.80.220:18180"
token_file=""
agent_token_file=""
pool_name=""
pool_image_id=""
image="127.0.0.1:5001/alcor/android-emulator:15.0-api35-google_apis-x86_64-sdk9"
image_source="10.0.30.54"
image_source_user="hadoop"
image_source_password=""
ssh_user="hadoop"
ssh_password=""
agent_binary=""
unit_file=""
concurrency="8"
skip_bootstrap="no"
skip_image="no"
dry_run="no"

while [ $# -gt 0 ]; do
  case "$1" in
    --host) host="${2:-}"; shift 2 ;;
    --host-id) host_id="${2:-}"; shift 2 ;;
    --register) register="yes"; shift ;;
    --server-url) server_url="${2:-}"; shift 2 ;;
    --token-file) token_file="${2:-}"; shift 2 ;;
    --agent-token-file) agent_token_file="${2:-}"; shift 2 ;;
    --pool-name) pool_name="${2:-}"; shift 2 ;;
    --pool-image-id) pool_image_id="${2:-}"; shift 2 ;;
    --image) image="${2:-}"; shift 2 ;;
    --image-source) image_source="${2:-}"; shift 2 ;;
    --image-source-user) image_source_user="${2:-}"; shift 2 ;;
    --image-source-password) image_source_password="${2:-}"; shift 2 ;;
    --ssh-user) ssh_user="${2:-}"; shift 2 ;;
    --ssh-password) ssh_password="${2:-}"; shift 2 ;;
    --agent-binary) agent_binary="${2:-}"; shift 2 ;;
    --unit-file) unit_file="${2:-}"; shift 2 ;;
    --concurrency) concurrency="${2:-}"; shift 2 ;;
    --skip-bootstrap) skip_bootstrap="yes"; shift ;;
    --skip-image) skip_image="yes"; shift ;;
    --dry-run) dry_run="yes"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

fail() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "==> $*"; }
step() { echo; echo "############ $* ############"; }

[ -n "${host}" ] || fail "--host is required"
[ -n "${pool_name}" ] || pool_name="android-${host}"
[ -n "${agent_token_file}" ] && [ -z "${token_file}" ] && token_file="${agent_token_file}"

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "${script_dir}/.." && pwd)

# 脚本常被拷到别处执行（例如 /tmp），这时推导出的 repo_root 是错的，会去找
# /deploy/... 之类的路径。这里改成显式"探测"：优先用脚本同级/上层的真实文件，
# 找不到就报错，绝不静默用一个错误路径继续（否则会出现装了一半的状态）。
find_local() { # find_local <相对路径> <候选...>
  _want="$1"; shift
  for _cand in "$@"; do
    if [ -n "${_cand}" ] && [ -f "${_cand}" ]; then
      printf '%s' "${_cand}"
      return 0
    fi
  done
  return 1
}

if [ -z "${agent_binary}" ]; then
  agent_binary=$(find_local "device-host-agent" \
    "${repo_root}/tmp/onboard55/bin/device-host-agent" \
    "${repo_root}/bin/device-host-agent" \
    "${script_dir}/device-host-agent" \
    "/opt/alcor-device-farm/bin/device-host-agent") \
    || fail "找不到 device-host-agent 二进制。
  请用 --agent-binary 指定，或先把它放到 ${repo_root}/bin/。

  来源参考（现有宿主上已有可用二进制）：
    scp hadoop@10.0.30.54:/opt/alcor-device-farm/bin/device-host-agent ./bin/"
fi

if [ -z "${unit_file}" ]; then
  unit_file=$(find_local "alcor-device-host-agent.service" \
    "${repo_root}/deploy/docker-emulator/alcor-device-host-agent.service" \
    "${script_dir}/alcor-device-host-agent.service" \
    "/etc/systemd/system/alcor-device-host-agent.service") \
    || fail "找不到 alcor-device-host-agent.service 单元模板。
  请用 --unit-file 指定。"
fi

# 同样理由，子脚本也必须在本地真实存在。
[ -f "${script_dir}/bootstrap-device-host.sh" ] \
  || fail "找不到 bootstrap-device-host.sh（应与本脚本同目录）"
[ -f "${script_dir}/install-local-image-registry.sh" ] \
  || fail "找不到 install-local-image-registry.sh（应与本脚本同目录）"
[ -f "${script_dir}/onboard-device-host.sh" ] \
  || fail "找不到 onboard-device-host.sh（应与本脚本同目录）"
[ -f "${script_dir}/distribute-emulator-image.sh" ] \
  || fail "找不到 distribute-emulator-image.sh（应与本脚本同目录）"

if [ "${dry_run}" = "yes" ]; then
  cat <<EOF
将执行以下步骤（dry-run）：
  目标宿主      : ${host}
  Host ID       : ${host_id:-<需 --register 或 --host-id>}
  池名          : ${pool_name}
  池默认镜像    : ${pool_image_id:-<未指定>}
  镜像          : ${image}
  镜像源        : ${image_source}
  Agent 二进制  : ${agent_binary}
  单元模板      : ${unit_file}
  并发上限      : ${concurrency}
  跳过 bootstrap: ${skip_bootstrap}
  跳过镜像搬运  : ${skip_image}
EOF
  exit 0
fi

# ------------------------------------------------------------ SSH 封装
# 目标机源机都可能只有密码；统一用 sshpass（若可用）+ 密码，或密钥。
ssh_cmd() { # ssh_cmd <user> <host> <password> <command>
  _user="$1"; _host="$2"; _pwd="$3"; shift 3
  if [ -n "${_pwd}" ]; then
    command -v sshpass >/dev/null 2>&1 || fail "需要 sshpass 才能用密码登录 ${_host}；
  安装：apt-get install -y sshpass，或改用密钥"
    sshpass -p "${_pwd}" ssh -o BatchMode=no -o StrictHostKeyChecking=accept-new \
      -o ConnectTimeout=15 "${_user}@${_host}" "$@"
  else
    ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
      -o ConnectTimeout=15 "${_user}@${_host}" "$@"
  fi
}

sudo_on() { # sudo_on <user> <host> <password> <root-password> <command>
  _user="$1"; _host="$2"; _pwd="$3"; _root_pwd="$4"; shift 4
  _quoted=$(printf '%s' "$*" | sed "s/'/'\\\\''/g")
  if [ -n "${_root_pwd}" ] && [ "${_user}" != "root" ]; then
    ssh_cmd "${_user}" "${_host}" "${_pwd}" \
      "echo '${_root_pwd}' | sudo -S -p '' bash -c '${_quoted}'"
  else
    ssh_cmd "${_user}" "${_host}" "${_pwd}" "bash -c '${_quoted}'"
  fi
}

# 探测目标是 root 登录还是普通用户 + sudo
probe_user="${ssh_user}"
probe_pwd="${ssh_password}"

# ------------------------------------------------------------ 前置校验
# 全部在"改动任何系统之前"完成：一旦缺件，必须在动手前失败，
# 绝不允许出现"用户建好了、单元没装上"的半成品状态。
preflight() {
  command -v ssh >/dev/null 2>&1 || fail "本机需要 ssh"
  command -v docker >/dev/null 2>&1 || info "提示：本机没有 docker；镜像搬运走 save→load 时需要"

  if [ -z "${ssh_password}" ] && [ -z "${image_source_password}" ]; then
    command -v sshpass >/dev/null 2>&1 || info "提示：未提供密码，将使用密钥登录；若失败请加 --ssh-password/--image-source-password 与 sshpass"
  fi

  [ -n "${host}" ] || fail "--host is required"
  if [ "${skip_image}" != "yes" ]; then
    [ -n "${image_source}" ] || fail "需要 --image-source"
  fi
  if [ "${skip_bootstrap}" != "yes" ]; then
    [ -f "${script_dir}/bootstrap-device-host.sh" ] || fail "缺少 bootstrap 子脚本"
  fi
}

preflight
info "前置校验通过：agent=${agent_binary} unit=${unit_file}"

step "0/7 Bootstrap 系统依赖"
if [ "${skip_bootstrap}" = "yes" ]; then
  info "已跳过（--skip-bootstrap）"
else
  info "推 bootstrap-device-host.sh 并在目标机执行"
  ssh_cmd "${probe_user}" "${host}" "${probe_pwd}" "mkdir -p /tmp/alcor-install" \
    || fail "无法连到 ${host}（用户 ${probe_user}）"
  ssh_cmd "${probe_user}" "${host}" "${probe_pwd}" \
    "cat > /tmp/alcor-install/bootstrap.sh" < "${script_dir}/bootstrap-device-host.sh" \
    || fail "上传 bootstrap 脚本失败"
  sudo_on "${probe_user}" "${host}" "${probe_pwd}" "${probe_pwd}" \
    "sh /tmp/alcor-install/bootstrap.sh --yes" \
    || fail "bootstrap 失败"
fi

step "1/7 本机镜像 registry"
ssh_cmd "${probe_user}" "${host}" "${probe_pwd}" \
  "cat > /tmp/alcor-install/registry.sh" < "${script_dir}/install-local-image-registry.sh" \
  || fail "上传 registry 脚本失败"
sudo_on "${probe_user}" "${host}" "${probe_pwd}" "${probe_pwd}" \
  "sh /tmp/alcor-install/registry.sh" \
  || fail "安装本机 registry 失败"

step "2/7 搬运模拟器镜像"
if [ "${skip_image}" = "yes" ]; then
  info "已跳过（--skip-image）"
else
  info "从 ${image_source} 经本机中转到 ${host}"
  info "（约 7GB，视网络可能需要数分钟）"
  # 目标机需先有本机 registry 才能接收；这里用 save→load 而非 push/pull，
  # 因为目标 registry 只监听回环，跨机 push 不成立。
  src_opts="--source-host ${image_source} --source-user ${image_source_user}"
  [ -n "${image_source_password}" ] && src_opts="${src_opts}"
  tgt_opts="--target-host ${host} --target-user ${probe_user}"
  if [ -f "${script_dir}/distribute-emulator-image.sh" ]; then
    sh "${script_dir}/distribute-emulator-image.sh" \
      --source-host "${image_source}" --source-user "${image_source_user}" \
      --target-host "${host}" --target-user "${probe_user}" \
      --image "${image}" \
      || fail "镜像搬运失败"
  else
    fail "找不到 distribute-emulator-image.sh"
  fi
fi

step "3/7 校验目标机 digest"
real_digest=$(ssh_cmd "${probe_user}" "${host}" "${probe_pwd}" \
  "docker image inspect '${image}' --format '{{.Id}}'" 2>/dev/null || echo "")
[ -n "${real_digest}" ] || fail "目标机 ${host} 上没有镜像 ${image}"
info "目标机权威 digest: ${real_digest}"
info "（此 digest 是镜像身份，须与 device_images.docker_digest 一致）"

step "4/7 安装 Host Agent"
if [ -z "${host_id}" ] && [ "${register}" = "yes" ]; then
  [ -n "${token_file}" ] || fail "--register 需要 --token-file"
  [ -f "${token_file}" ] || fail "token 文件不存在: ${token_file}"
  info "调用控制面登记 Host ${host}"
  _tok=$(cat "${token_file}")
  _resp=$(curl -sSk -m 25 -X POST \
    -H "Authorization: Bearer ${_tok}" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: onboard-${host}" \
    -d "{\"name\":\"安卓宿主机 - ${host}\",\"host_type\":\"docker_emulator\",\"host_os\":\"linux\",\"host_arch\":\"amd64\",\"address\":\"${host}\"}" \
    "${server_url}/api/v1/device-hosts") || fail "登记 Host 失败"
  host_id=$(printf '%s' "${_resp}" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p' | head -1)
  [ -n "${host_id}" ] || fail "登记返回里没有 Host ID: ${_resp}"
  info "登记成功 Host ID: ${host_id}"
fi
[ -n "${host_id}" ] || fail "缺少 --host-id（或加 --register）"

[ -f "${agent_binary}" ] || fail "找不到 Agent 二进制: ${agent_binary}"
[ -f "${unit_file}" ] || fail "找不到单元模板: ${unit_file}"

ssh_cmd "${probe_user}" "${host}" "${probe_pwd}" "mkdir -p /tmp/alcor-install" || fail "无法连接目标机"
ssh_cmd "${probe_user}" "${host}" "${probe_pwd}" \
  "cat > /tmp/alcor-install/onboard.sh" < "${script_dir}/onboard-device-host.sh" || fail "上传 onboard 脚本失败"
ssh_cmd "${probe_user}" "${host}" "${probe_pwd}" \
  "cat > /tmp/alcor-install/device-host-agent" < "${agent_binary}" || fail "上传 Agent 二进制失败"
ssh_cmd "${probe_user}" "${host}" "${probe_pwd}" \
  "cat > /tmp/alcor-install/unit.service" < "${unit_file}" || fail "上传单元模板失败"

if [ -n "${token_file}" ] && [ -f "${token_file}" ]; then
  ssh_cmd "${probe_user}" "${host}" "${probe_pwd}" \
    "cat > /tmp/alcor-install/agent-token" < "${token_file}" || fail "上传 Agent Token 失败"
  sudo_on "${probe_user}" "${host}" "${probe_pwd}" "${probe_pwd}" "chmod 0600 /tmp/alcor-install/agent-token"
  sudo_on "${probe_user}" "${host}" "${probe_pwd}" "${probe_pwd}" \
    "sh /tmp/alcor-install/onboard.sh \
      --server-url http://10.0.80.220:18182 \
      --host-id ${host_id} \
      --agent-token-file /tmp/alcor-install/agent-token \
      --agent-binary /tmp/alcor-install/device-host-agent \
      --unit-file /tmp/alcor-install/unit.service \
      --advertise-host ${host} \
      --render-device /dev/dri/renderD128 \
      --image '${image}' \
      --android-sdk-root /opt/android-sdk" \
    || fail "安装 Agent 失败"
  sudo_on "${probe_user}" "${host}" "${probe_pwd}" "${probe_pwd}" \
    "rm -f /tmp/alcor-install/agent-token"
else
  info "未提供 token 文件；跳过 Agent 安装（请补 --token-file 后重跑）"
fi

step "5/7 等待控制面 online"
if [ -n "${token_file}" ] && [ -f "${token_file}" ]; then
  _tok=$(cat "${token_file}")
  _i=0
  while [ "${_i}" -lt 20 ]; do
    _i=$((_i + 1))
    _st=$(curl -sSk -m 15 -H "Authorization: Bearer ${_tok}" \
      "${server_url}/api/v1/device-hosts?page=1&page_size=200" 2>/dev/null \
      | tr '}' '\n' | grep "\"${host}\"" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p' | head -1)
    if [ "${_st}" = "online" ]; then info "宿主 ${host} 已 online"; break; fi
    info "  等待中（${_i}/20，当前 ${_st:-未知}）"
    sleep 5
  done
fi

step "6/7 建池（按需：total_target=0 / min_ready=0）"
if [ -n "${token_file}" ] && [ -f "${token_file}" ] && [ -n "${pool_image_id}" ]; then
  _tok=$(cat "${token_file}")

  # 先建池（拿到 pool id；此时 max_concurrency 已是安全网值）。
  _body=$(printf '{"name":"%s","platform":"android","default_lease_seconds":1800,"max_lease_seconds":7200,"max_concurrency":%s,"total_target":0,"min_ready":0,"default_image_id":"%s","reason":"一键接入 %s"}' \
    "${pool_name}" "${concurrency}" "${pool_image_id}" "${host}")
  _resp=$(curl -sSk -m 25 -X POST -H "Authorization: Bearer ${_tok}" \
    -H 'Content-Type: application/json' -H "Idempotency-Key: pool-${host}" \
    -d "${_body}" "${server_url}/api/v1/device-pools") || fail "建池失败"
  case "${_resp}" in
    *'"error":null'*|*'"id"'*) info "池 ${pool_name} 已就绪" ;;
    *) info "建池返回（请确认）: ${_resp}" ;;
  esac

  # 取池 id（用于绑定镜像）。
  #
  # 注意：不能用 `sed 's/.*"id":"...".*/\1/'` —— 该正则是贪婪的，整行只有一个
  # 匹配时会取到**最后一个** id（实测把别台服务器的池 id 取了出来），
  # 会造成"绑错池"的静默故障。这里用 grep -o 取第一个匹配，语义更明确。
  _pool_id=$(printf '%s' "${_resp}" | grep -o '"id":"[0-9a-f-]\{36\}"' | head -1 | cut -d'"' -f4)
  if [ -z "${_pool_id}" ]; then
    # 兜底：按名字在池列表里找回该池 id（同样只用 grep -o 取精确片段）。
    _pool_id=$(curl -sSk -m 25 -H "Authorization: Bearer ${_tok}" \
      "${server_url}/api/v1/device-pools?page=1&page_size=200" \
      | tr '{' '\n' | grep -F "\"name\":\"${pool_name}\"" \
      | grep -o '"id":"[0-9a-f-]\{36\}"' | head -1 | cut -d'"' -f4)
  fi

  if [ -n "${_pool_id}" ]; then
    # 再绑定镜像并放开实例上限。
    #   ⚠️ 顺序关键：SetPoolImage 强制 min_ready == max_instances，且写入会回写
    #   池级 total_target/min_ready。因此必须在池的 0/0 设置之后不再改镜像，
    #   否则池会被抬成常驻热备，破坏"按需清空"。
    #   这里先建池（0/0）已生效；随后设镜像 min_ready=max_instances=<安全网>，
    #   会回写池级——所以最后再补一次池的 0/0。
    _pi_body=$(printf '{"min_ready":%s,"max_instances":%s,"enabled":true,"reason":"一键接入放开镜像实例上限"}' \
      "${concurrency}" "${concurrency}")
    curl -sSk -m 25 -X PUT -H "Authorization: Bearer ${_tok}" \
      -H 'Content-Type: application/json' \
      -d "${_pi_body}" "${server_url}/api/v1/device-pools/${_pool_id}/images/${pool_image_id}" \
      -o /dev/null -w '绑定镜像 http=%{http_code}\n' || info "镜像绑定失败（可后续手工补）"

    # 复核并确保池回到 0/0（抵御上面 PUT 的回写）。
    curl -sSk -m 25 -X PUT -H "Authorization: Bearer ${_tok}" \
      -H 'Content-Type: application/json' \
      -d "${_body}" "${server_url}/api/v1/device-pools/${_pool_id}" \
      -o /dev/null -w '池 0/0 复核 http=%{http_code}\n' || true
  else
    info "未能取到池 id；请手工确认 device_pool_images 绑定"
  fi
else
  info "未提供 token 或 --pool-image-id；跳过建池"
fi

step "7/7 完成"
cat <<EOF

============================================================
${host} 接入流程完成
============================================================
Host ID      : ${host_id}
池           : ${pool_name}
镜像 digest  : ${real_digest}

仍需人工确认（本脚本无法代替判断）：
  1. device_images 里是否有 digest ${real_digest} 的记录？
     有 → 复用（推荐）；没有 → 需登记新记录。
  2. 该记录是否 enabled 绑定到池 ${pool_name}（device_pool_images）？
  3. 选池自查 SQL 是否显示该池有「可建机」宿主？
  4. 投一个预约做真机验收。

排障：
  ssh ${probe_user}@${host} journalctl -u alcor-device-host-agent.service -n 50 --no-pager
EOF
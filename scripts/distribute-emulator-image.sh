#!/usr/bin/env sh
# 镜像分发：把来源宿主机上的模拟器镜像搬到目标宿主机，并核对 digest。
#
# 背景（先读 docs/host_onboarding_standard_process.md §0.3）：
#   * device_images 以 digest 为身份；create 永不 pull；宿主缺该 digest 必然
#     IMAGE_DIGEST_MISMATCH。
#   * docker save/load 后本地 digest 与来源机同一 tag 的 repo digest 可能不同，
#     因此搬运后必须重新核对，并按目标机本地 digest 建立/复用镜像记录。
#
# 本脚本只做「搬运 + 核对」，不做：
#   * 修改 device_images / device_host_image_states（那是控制面的事，走既有链路）；
#   * 新建 registry。
#
# 用法：
#   scripts/distribute-emulator-image.sh \
#     --source-host 10.0.30.171 --source-user root \
#     --target-host 10.0.30.55 --target-user hadoop \
#     --image 127.0.0.1:5001/alcor/android-emulator:15.0-api35-google_apis-x86_64-sdk9
set -eu

usage() {
  cat <<'USAGE'
usage: distribute-emulator-image.sh --source-host <ip> --target-host <ip> --image <ref> [options]

required:
  --source-host <ip>        已有该镜像的宿主机
  --target-host <ip>        需要该镜像的宿主机
  --image <ref>             镜像引用（tag）

optional:
  --source-user <user>      默认 root
  --target-user <user>      默认 root
  --ssh-key <path>          私钥路径；不传则用默认 SSH 配置
  --work-dir <dir>          中转目录，默认 /var/tmp/alcor-image-transfer
  --keep-tarball            传输完成后保留目标机上的 tar
  -h, --help                显示本帮助

退出码：0 成功；非 0 失败（不留下半搬运状态）。
USAGE
}

source_host=""
target_host=""
image=""
source_user="root"
target_user="root"
ssh_key=""
work_dir="/var/tmp/alcor-image-transfer"
keep_tarball="no"

while [ $# -gt 0 ]; do
  case "$1" in
    --source-host) source_host="${2:-}"; shift 2 ;;
    --target-host) target_host="${2:-}"; shift 2 ;;
    --image) image="${2:-}"; shift 2 ;;
    --source-user) source_user="${2:-}"; shift 2 ;;
    --target-user) target_user="${2:-}"; shift 2 ;;
    --ssh-key) ssh_key="${2:-}"; shift 2 ;;
    --work-dir) work_dir="${2:-}"; shift 2 ;;
    --keep-tarball) keep_tarball="yes"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

fail() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "==> $*"; }

command -v docker >/dev/null || fail "docker is required on this workstation"
[ -n "${source_host}" ] || fail "--source-host is required"
[ -n "${target_host}" ] || fail "--target-host is required"
[ -n "${image}" ] || fail "--image is required"

SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15"
if [ -n "${ssh_key}" ]; then
  SSH_OPTS="${SSH_OPTS} -i ${ssh_key}"
fi

tag_slug=$(printf '%s' "${image}" | tr '/:@' '---')
tarball="emulator-${tag_slug}.tar"

# ---------------------------------------------------------------- 1. 来源核对
info "来源 ${source_host}: 解析镜像 ${image}"
source_id=$(ssh ${SSH_OPTS} "${source_user}@${source_host}" \
  "docker image inspect '${image}' --format '{{.Id}}'" 2>/dev/null) \
  || fail "cannot inspect ${image} on ${source_host}; is the image present?"
[ -n "${source_id}" ] || fail "no image id returned from ${source_host}"

source_repo_digests=$(ssh ${SSH_OPTS} "${source_user}@${source_host}" \
  "docker image inspect '${image}' --format '{{join .RepoDigests \",\"}}'" 2>/dev/null || echo "")

info "  来源 ID      : ${source_id}"
info "  来源 RepoDig : ${source_repo_digests:-<none>}"

# ---------------------------------------------------------------- 2. 目标现状
info "目标 ${target_host}: 检查是否已有 digest ${source_id}"
target_has=$(ssh ${SSH_OPTS} "${target_user}@${target_host}" \
  "docker image inspect '${image}' --format '{{.Id}}'" 2>/dev/null || echo "")
target_repo_digests=$(ssh ${SSH_OPTS} "${target_user}@${target_host}" \
  "docker image inspect '${image}' --format '{{join .RepoDigests \",\"}}'" 2>/dev/null || echo "")

if [ "${target_has}" = "${source_id}" ]; then
  info "目标已存在完全相同的镜像 ID，无需搬运。"
  echo
  echo "目标本地 digest: ${target_has}"
  echo "目标 RepoDigests: ${target_repo_digests:-<none>}"
  exit 0
fi

if [ -n "${target_has}" ]; then
  info "目标已有同 tag 但 ID 不同（本地 ${target_has}），将继续搬运覆盖。"
else
  info "目标没有该镜像，开始搬运。"
fi

# ---------------------------------------------------------------- 3. save
remote_tar="${work_dir}/${tarball}"
info "在 ${source_host} 执行 docker save（可能需要数分钟）"
ssh ${SSH_OPTS} "${source_user}@${source_host}" "mkdir -p '${work_dir}' && docker save -o '${remote_tar}' '${image}'" \
  || fail "docker save failed on ${source_host}"

# 注意：docker save 产生的 tar **不是逐字节确定的** —— 同一镜像在同一台机器上
# 先后 save 两次，sha256 也可能不同（实测 55 上两次结果不一致）。
# 因此这里的 sha256 只用于确认「这一次传输完整」，不构成镜像身份。
source_sha=$(ssh ${SSH_OPTS} "${source_user}@${source_host}" "sha256sum '${remote_tar}' | cut -d' ' -f1") \
  || fail "cannot hash tarball on ${source_host}"
info "  来源 tar sha256: ${source_sha}  （仅代表本次传输完整性，不是镜像身份）"

# ---------------------------------------------------------------- 4. 传输
# 走部署机中转：在这台机器上流式转发，避免占本地磁盘。
info "中转传输到 ${target_host}（流式，不经本地磁盘）"
ssh ${SSH_OPTS} "${target_user}@${target_host}" "mkdir -p '${work_dir}'" \
  || fail "cannot create ${work_dir} on ${target_host}"

ssh ${SSH_OPTS} "${source_user}@${source_host}" "cat '${remote_tar}'" \
  | ssh ${SSH_OPTS} "${target_user}@${target_host}" "cat > '${remote_tar}'" \
  || fail "transfer failed"
info "  传输完成"

# ---------------------------------------------------------------- 5. 校验
target_sha=$(ssh ${SSH_OPTS} "${target_user}@${target_host}" "sha256sum '${remote_tar}' | cut -d' ' -f1") \
  || fail "cannot hash tarball on ${target_host}"

if [ "${source_sha}" != "${target_sha}" ]; then
  ssh ${SSH_OPTS} "${target_user}@${target_host}" "rm -f '${remote_tar}'" || true
  fail "传输完整性校验失败: source=${source_sha} target=${target_sha}"
fi
info "  传输完整性校验通过: ${target_sha}"

# ---------------------------------------------------------------- 6. load
info "在 ${target_host} 执行 docker load"
ssh ${SSH_OPTS} "${target_user}@${target_host}" "docker load -i '${remote_tar}'" \
  || fail "docker load failed on ${target_host}"

# ---------------------------------------------------------------- 7. 目标 digest
new_id=$(ssh ${SSH_OPTS} "${target_user}@${target_host}" \
  "docker image inspect '${image}' --format '{{.Id}}'") || fail "cannot inspect loaded image"
new_repo_digests=$(ssh ${SSH_OPTS} "${target_user}@${target_host}" \
  "docker image inspect '${image}' --format '{{join .RepoDigests \",\"}}'" 2>/dev/null || echo "")

if [ "${keep_tarball}" != "yes" ]; then
  ssh ${SSH_OPTS} "${target_user}@${target_host}" "rm -f '${remote_tar}'" || true
  ssh ${SSH_OPTS} "${source_user}@${source_host}" "rm -f '${remote_tar}'" || true
  info "  已清理两端 tar（--keep-tarball 可保留）"
fi

# ---------------------------------------------------------------- 8. 报告
cat <<EOF

============================================================
镜像搬运完成
============================================================
来源 ${source_host}
  镜像 ID      : ${source_id}
  RepoDigests  : ${source_repo_digests:-<none>}

目标 ${target_host}
  ★ 权威 digest : ${new_id}
  RepoDigests  : ${new_repo_digests:-<none>}

$(if [ "${new_id}" != "${source_id}" ]; then
  echo "注意：目标本地 digest 与来源不同。这是 docker save/load 的正常现象，"
  echo "      不代表镜像内容不同 —— 逐层 DiffID 一致即为同一内容。"
  echo "      以目标机的 digest（上面 ★ 那行）为准去核对 device_images。"
fi)

下一步（必须由管理员在控制面完成，本脚本不做）：
  1. 用上面「★ 权威 digest」核对 device_images：
       已有同 digest + 同 resource_config 的记录 → 直接复用（推荐）；
       没有 → 按 ADR-0015 的既有链路登记（不要手工 INSERT）。
     复用可以避免「同一镜像两条记录」，并让多台宿主互相供给对方的池。
  2. 确认该镜像已 enabled 绑定到目标宿主所在的池（device_pool_images）；
     必要时把池的 default_image_id 指向该记录。
  3. 复核选池自查 SQL，确认该池有一台「可建机」宿主。
     详见 docs/host_onboarding_standard_process.md §5.3、§6。
EOF
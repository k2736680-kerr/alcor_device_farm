#!/usr/bin/env bash
# 把远程宿主机的 Android 设备汇入本机 STF 的 ADB server。
#
# 背景（ADR-0037）：
#   全局只有一套 STF，位于本机。远程宿主机的 Host Agent 无法向它注册（STF ADB registrar
#   硬性要求回环地址），因此由 STF 所在宿主机统一发起 `adb connect <远程设备 endpoint>`。
#   本机设备由本机 Host Agent 自行注册，本脚本会跳过，避免重复 connect。
#
# 与 scripts/stf-connect-emulators.sh 的关系：
#   后者是「手工连接指定 endpoint」的验收入口；本脚本是它的自动化封装，
#   数据源为 Device Farm Server 的 GET /api/v1/devices，执行的仍是同一句 adb connect。
#
# 用法：
#   scripts/stf-sync-remote-emulators.sh [--dry-run] [--verbose]
#
# 环境变量：
#   DEVICE_FARM_API_BASE_URL   控制面地址，默认 http://10.0.80.220:18182
#   DEVICE_FARM_SERVICE_TOKEN  Service Token；未设置时尝试从 API_TOKEN_FILE 读取
#   DEVICE_FARM_API_TOKEN_FILE Token 文件路径，默认 /etc/alcor-device-farm/service-token
#   STF_COMPOSE_FILE           STF compose 路径，默认 deploy/stf/compose.yaml
#   STF_ENV_FILE               STF env 路径，默认 deploy/stf/.env
#   STF_LOCAL_HOSTS            本机地址列表（逗号分隔），用于跳过本地设备
#   PAGE_SIZE                  设备列表分页大小，默认 200（服务端上限）
#   CONNECT_TIMEOUT            单次 adb connect 超时秒数，默认 15
#   DRY_RUN                    置 1 等价于 --dry-run
#   VERBOSE                    置 1 等价于 --verbose
#
# 作为 systemd 服务运行时，每次执行都会在 journal 写一行摘要
# （connected / skipped_local / failed），用于确认 timer 是否真的在工作；
# VERBOSE=1 可进一步打印逐设备明细。
set -euo pipefail

# 允许经 systemd EnvironmentFile 配置；命令行参数在下方循环中覆盖。
DRY_RUN="${DRY_RUN:-0}"
VERBOSE="${VERBOSE:-0}"
for arg in "$@"; do
  case "${arg}" in
    --dry-run) DRY_RUN=1 ;;
    --verbose) VERBOSE=1 ;;
    *) echo "unknown argument: ${arg}" >&2; exit 2 ;;
  esac
done

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
api_base="${DEVICE_FARM_API_BASE_URL:-http://10.0.80.220:18182}"
token_file="${DEVICE_FARM_API_TOKEN_FILE:-/etc/alcor-device-farm/service-token}"
compose_file="${STF_COMPOSE_FILE:-${project_root}/deploy/stf/compose.yaml}"
env_file="${STF_ENV_FILE:-${project_root}/deploy/stf/.env}"
connect_timeout="${CONNECT_TIMEOUT:-15}"
page_size="${PAGE_SIZE:-200}"

# adb connect 一律在 stf-adb 容器内执行，因此目标是容器自己的 127.0.0.1:5037。
# 容器外的 5038 只是宿主回环映射，脚本不直接使用它（也拿不到，见 ADR-0037 决策 4）。
stf_adb_host="127.0.0.1"
stf_adb_port="5037"

log() { printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*"; }
verbose() { [[ "${VERBOSE}" == "1" ]] && log "$@" || true; }

token="${DEVICE_FARM_SERVICE_TOKEN:-}"
if [[ -z "${token}" ]]; then
  if [[ -r "${token_file}" ]]; then
    token="$(tr -d '\r\n' < "${token_file}")"
  fi
fi
if [[ -z "${token}" ]]; then
  echo "service token is required (set DEVICE_FARM_SERVICE_TOKEN or create ${token_file})" >&2
  exit 1
fi

if [[ ! -f "${compose_file}" ]]; then
  echo "STF compose file not found: ${compose_file}" >&2
  exit 1
fi
if [[ ! -f "${env_file}" ]]; then
  echo "STF env file not found: ${env_file}" >&2
  exit 1
fi

# 本机地址集合：用于跳过由本机 Host Agent 自行注册的设备。
local_hosts="${STF_LOCAL_HOSTS:-}"
if [[ -z "${local_hosts}" ]]; then
  detected="$(hostname -I 2>/dev/null | tr ' ' '\n' | grep -E '^[0-9]' | paste -sd, - || true)"
  if [[ -n "${detected}" ]]; then
    local_hosts="${detected}"
  else
    # 自动探测失败时必须显式告警：否则本机设备会被当作远程设备重复 connect。
    # 虽然重复 connect 是幂等的（adb 返回 already connected），但这偏离了
    # 「守护进程只负责远程宿主机」的设计意图，需要人工设置 STF_LOCAL_HOSTS 修正。
    log "WARNING: could not detect local addresses; set STF_LOCAL_HOSTS explicitly to avoid re-connecting local devices"
    local_hosts="127.0.0.1"
  fi
fi

# 拉取设备清单。只取 android + ready，避免对已删除或不可调度设备做无谓 connect。
#
# 注意：/api/v1/devices 的 page_size 上限是 200（openapi device-farm-v1.yaml 的
# PageSize 组件：minimum 1 / maximum 200），传更大的值会被服务端拒绝为 400。
# 因此这里按页遍历，直到取完，保证宿主机数量增长后仍能取全。
fetch_endpoints() {
  python3 - "${api_base}" "${token}" "${page_size}" <<'PY'
import json
import sys
import urllib.error
import urllib.parse
import urllib.request

api_base, token, page_size = sys.argv[1], sys.argv[2], int(sys.argv[3])
page = 1
while True:
    query = urllib.parse.urlencode({
        "platform": "android",
        "lifecycle_status": "ready",
        "page": page,
        "page_size": page_size,
    })
    request = urllib.request.Request(
        f"{api_base}/api/v1/devices?{query}",
        headers={"Authorization": f"Bearer {token}"},
    )
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            payload = json.load(response)
    except urllib.error.HTTPError as exc:
        print(f"device list request failed: HTTP {exc.code}", file=sys.stderr)
        sys.exit(1)
    except Exception as exc:  # noqa: BLE001 - 需要把任何取数失败都反映为退出码
        print(f"device list request failed: {exc}", file=sys.stderr)
        sys.exit(1)

    data = payload.get("data") or {}
    items = data.get("items") or []
    for item in items:
        endpoint = (item.get("adb_endpoint") or "").strip()
        if endpoint:
            print(endpoint)

    pagination = data.get("pagination") or {}
    total = pagination.get("total")
    if not items:
        break
    if isinstance(total, int) and page * page_size >= total:
        break
    page += 1
PY
}

endpoints="$(fetch_endpoints)"

if [[ -z "${endpoints}" ]]; then
  log "sync done: no android device with adb_endpoint found"
  exit 0
fi

compose=(docker compose --env-file "${env_file}" -f "${compose_file}")

connect_one() {
  local endpoint="$1"
  local host="${endpoint%%:*}"
  # 跳过本机设备：本机 Host Agent 已通过回环 registrar 完成注册。
  if [[ ",${local_hosts}," == *",${host},"* ]]; then
    skipped_local=$((skipped_local + 1))
    verbose "skip local device ${endpoint}"
    return 0
  fi
  if [[ "${DRY_RUN}" == "1" ]]; then
    log "would connect ${endpoint}"
    return 0
  fi
  local output
  if output="$(timeout "${connect_timeout}" "${compose[@]}" exec -T stf-adb \
      adb -H "${stf_adb_host}" -P "${stf_adb_port}" connect "${endpoint}" 2>&1)"; then
    if [[ "${output}" == *"connected to ${endpoint}"* ]] || [[ "${output}" == *"already connected to ${endpoint}"* ]]; then
      connected_count=$((connected_count + 1))
      verbose "connected ${endpoint}"
      return 0
    fi
    log "unexpected response for ${endpoint}: ${output}"
    return 1
  fi
  log "connect failed for ${endpoint}: ${output}"
  return 1
}

failures=0
connected_count=0
skipped_local=0
while IFS= read -r endpoint; do
  [[ -z "${endpoint}" ]] && continue
  connect_one "${endpoint}" || failures=$((failures + 1))
done <<< "${endpoints}"

if [[ "${VERBOSE}" == "1" && "${DRY_RUN}" != "1" ]]; then
  log "current stf-adb device table:"
  "${compose[@]}" exec -T stf-adb adb -H "${stf_adb_host}" -P "${stf_adb_port}" devices -l | sed 's/^/  /'
fi

# 始终输出一行摘要：journal 是守护进程唯一的可观测面，静默会让排障无从下手。
log "sync done: connected=${connected_count} skipped_local=${skipped_local} failed=${failures}"
if (( failures > 0 )); then
  exit 1
fi

# 设备农场全链路体检 · 2026-09-15

对象：控制面 `10.0.80.220`、STF/设备宿主 `10.0.30.171`、设备宿主 `10.0.30.55`。
方式：只读取证（`tmp/health/*.py`，paramiko）。唯一变更见 §5。

---

## 一、结论：设备农场本身无故障

| 维度 | 结果 |
|---|---|
| 三台宿主心跳 | 全部 `online`，年龄 1.5~4.3 秒 |
| 设备池 | 3 个全部 `active`，目标全部达成（171 `1/1`、55 `1/1`、iOS `2/2`） |
| 设备 | 4 台 `ready/healthy`（171 一台、55 一台、iOS 两台） |
| STF 库存 | `total 2 / present 2 / ready 2 / using 0`，**零幽灵** |
| 宿主机命令 | **无** `pending/leased/running` 滞留 |
| 预约 | 无滞留；累计 `released 84 / expired 19 / failed 4 / force_released 1` |
| 容器健康 | `docker ps --filter health=unhealthy` = **0 条** |
| 控制面 | `/console/` HTTPS **200**、`/healthz` **200** |
| 业务 API | `GET /api/v1/devices` **200**（8 条：4 活跃 + 4 软删）、`GET /api/v1/device-pools` **200** |

### 修复后的决定性复验

`device_host_commands` 中 `command_type='restart'` 且 `created_at > 2026-09-15 01:28Z` 的查询
**返回空集** —— 自 STF 同步 timer 提速（2min→20s）与 55 池恢复后，**零自愈重启**。
对照：09-14 06:00~10:00 每小时 12~14 次。

---

## 二、`ios_session_cleanup_failed` 12354 条 —— 已由 `2ba13e1` 修复，无需再动

**现象**：`2026-09-14 03:21:35Z ~ 07:06:39Z`，单设备（iPhone17-1 `32155337`）产生 **12354 条 critical** 健康事件，
每小时恰好 3600 条（= 每秒 1 条）。

**机制**：iPhone17-1 上一个卡死的 Appium Session（`ac2e8543`，03:10 起）触发 Host Fence 清理失败
（`internal/iossession/cleanup.go:58`，`IOS_SESSION_CLEANUP_FAILED`）；
后台 Reaper 每秒重试，每次都走 `quarantine()`，而当时的 `quarantine()` **没有幂等短路**，于是每秒写一条事件。

**已修复**：commit `2ba13e1`（2026-09-14 16:04:32 +0800，标题「修复 iOS 会话清理失败时重复隔离刷库」）
在 `internal/iossession/reconcile.go:76-78` 加入：

```go
// 隔离是幂等操作。设备已处于 quarantined 时不再重复写入健康事件、审计或
// 累加连续失败计数；否则 fence 短暂不可达时，后台 Reaper 会每秒重试失败
// 的 iOS 会话清理，单设备即可刷出每小时数千条 device_health_events。
if lifecycle == domain.DeviceQuarantined {
    return nil
}
```

**上线确认**：风暴发生在修复提交**之前**；生产 `device-farm-server` 容器已运行 16 小时（含该修复），
自 09-14 07:06 之后 `ios_session_cleanup_failed` **零复现**。
**结论：属于已解决的历史事件，不重复处理。**

---

## 三、`host_unavailable` 每日复现 —— Mac 侧停顿，非本仓库配置问题

**现象**：iOS 两台设备每日稳定产生 `host_unavailable`（8~27 条/天）与
`ios_automation_stabilizing`（8~10 条/天），自 09-01 起每天都有。

**排除「阈值太紧」**：
- agent 心跳间隔 = **5 秒**（`cmd/device-host-agent/main.go:169`）
- 宿主离线判定 = **30 秒**（`internal/config/config.go:130` 默认 `HostTimeout`）
- ⇒ 需要**连续 6 次心跳丢失**才判离线，余量充足，**不是误报配置**。

**判定**：Mac 端确实存在 ≥30 秒的心跳停顿。诱因候选（按可能性）：
跨网段链路抖动（171→Mac ping 平均 81ms / 抖动 30ms）、共享 Appium 负载、macOS 休眠/launchd 重拉。
**影响有限**：调度器排除 offline 宿主，按 12 次/天 × 约 30~60 秒估算，iOS 可用性损失约 1%。

**建议**：需到 Mac（`10.0.33.68`）查看 agent 的 launchd 日志。**本机未保存该机凭据**，未做任何改动。

---

## 四、跨团队/宿主级问题（**不属于设备农场**，仅报告）

### 4.1 220 磁盘 82%（390G/501G，剩 90G）—— 根因不在我们

`du -xsh /*` 曾给出误导性的 15G 总计，原因是 `/var/lib/docker` 为 0700 root，
`kerr` 读不了且它是独立挂载点被 `-x` 跳过。改用 root 视角后真相是：

| 路径 | 占用 | 归属 |
|---|---|---|
| **`/var/lib/clickhouse`** | **308G** | **ClickHouse 宿主机服务**（`clickhouse-server.service`，active，已跑 5 天 19 小时；库 `ads`/`dwd`/`dws`/`default`）。**不在 `/data/stacks` 内，不属设备农场** |
| `/var/lib/docker` | ~12G | 其中 `/var/lib/docker/containers` **7.5G** 几乎全是 **`mongodb` 容器的一个 json 日志 = 7.3G** |
| `/usr` | 6.3G | 系统 |
| `/swap.img` | 4.1G | swap |
| **`/data/stacks/alcor-device-farm`** | **25M** | **设备农场本体（可忽略）** |

**两个独立风险（均非设备农场，需各自归属人处理）**：
1. **ClickHouse 占 308G** —— 需其归属人决定是否 TTL/清理历史分区。
2. **Docker 无全局日志轮转** —— 两个宿主机的 `/etc/docker/daemon.json` **都不存在**；
   `mongodb` 容器（`/data/stacks/mongodb`，另一 stack）未配 `logging` 选项，日志已涨到 7.3G。
   ⚠️ **设备农场自身的 compose 都配了轮转**（`deploy/control-plane` 20m×5、`deploy/server` 10m×5、
   `deploy/stf` 20m×5），**不受影响**。

### 4.2 55 的镜像 registry `127.0.0.1:5001` 不存在（潜伏地雷）

- 镜像 tag 为 `127.0.0.1:5001/alcor/android-emulator:15.0-api35-google_apis-x86_64-sdk9`
- 但 55 上 **无 registry 单元、无 registry 容器、5001 端口未监听**（`curl` 返回 000）
- **当前无害**：镜像 14.7G 已在 55 本地，`docker run` 不触发 pull
- **风险点**：一旦删除该镜像、切新镜像版本、或使用 `--pull`，拉取必然 `connection refused` 失败
- 缓解建议（择一）：① 在 55 常驻一个 registry ② 把镜像 tag 改为无 registry 前缀的本地名，
  并同步更新 `device_images.docker_image` ③ 明确记录「镜像只以 `docker load` 方式分发」

### 4.3 171 的内存/交换偏紧（设计使然，非故障）

- 可用内存 5.2 GiB，**swap 已用 3226 MB**，其中 `qemu-system-x86` 占 **2339 MB**
- 单台模拟器限额 7 GiB，实测 **6.97 GiB / 7 GiB（99.5%）**
- 内存压力计数器 `avg10/60/300` 全为 `0.00` ⇒ **当前无停顿**
- 171 上另有非设备农场负载：`microk8s`（kubelite 324MB + k8s-dqlite 214MB）、`mysqld` 321MB、
  `lxd`/`snapd`、`vega-face-search`（app1 764MB + nginx）
- **容量模型已自行拦截**：`memory_available - 2048 < 7168` ⇒ Android 池 `total_target` 只能是 1

### 4.4 171 journald 占 4.0G（171 磁盘仅用 16%，无风险）

---

## 五、本次唯一的变更（已执行）

**清理 220 根目录两个 0 字节垃圾文件**（删除前已逐一验证为 `regular empty file size=0`）：

```
/x27appium_endpointx27   （0 字节，root，2026-09-09 12:57）
/x27devicex27-           （0 字节，root，2026-09-09 12:57）
```

成因：某次命令的引号被转义序列吞掉（`x27` 即 `'` 的十六进制），与仓库里曾出现的 `5037` 空文件同源。
**复核：`ls / | grep -c x27` = 0，已清空。**

---

## 六、待决策事项

1. **幽灵设备清理是否自动化？** 当前幽灵数为 0（已手工清过），但 `--no-cleanup` 是契约强制的，
   设备 rebuild/delete 后会再次累积。**本仓库现状是「不自动化」**——`deploy/stf/README.md` 明确
   同步脚本只做 `adb connect`、不做清理。
   若自动化，建议规则：*删除「`present=false` 且 serial 不在控制面活跃设备清单中」的 STF 设备*。
   该规则不会误删启动中的设备（启动中的设备尚未进入 STF，或已在控制面清单内）。
   ⚠️ 这会改变同步脚本的既定契约，需先补 ADR（AGENTS.md 规则 11）。
2. **`TestManagementAPICompleteMockFlow` 脆弱断言**：断言 `device_audit_events` 总数恰好 8，
   但未排除 `reconcile` 插入的 `restart_device_self_healing` 系统审计（`internal/reconcile/service.go:515`）。
   已用 stash 对照实验证明与镜像验证改动无关（基线同样失败）。
   修法：加 `AND action <> 'restart_device_self_healing'`，或改为逐 action 计数。
3. **未提交改动**：ADR-0037 入库、`internal/imagecatalog/service.go` 与对应集成测试、
   同步脚本/单元/安装器/README 均在工作区未提交（见 `git status`）。

---

## 附录 A：数据库列名速查（本次踩坑，别再猜）

| 表 | 正确列名 | 易错点 |
|---|---|---|
| `device_reservations` / `device_provisioning_jobs` / `device_sessions` | **`status`** | ❌ 不是 `state` |
| `device_pool_devices` | `pool_id`, `device_id`, **`enabled`** | ❌ 不是 `state` |
| `device_audit_events` | **`resource_type` / `resource_id`**、`action`、`actor_type`、`summary`（**jsonb**）、`reason` | ❌ 无 `device_id`；`summary` 必须 `::text` |
| `device_health_events` | `device_id`、`source`、`event_type`、`severity`、**`reason`**、`payload`、`observed_at` | ❌ 无 `message` |
| `device_images` | **`docker_digest`**、`docker_image`、`status`、`api_level`、`abi`、`resolution` | ❌ 不是 `digest` |
| `device_hosts` | `status`、`draining`、`last_heartbeat_at`、**`capacity` / `used_capacity`**（jsonb） | ❌ 无 `memory_total_mb`（在 `capacity` json 内） |
| `device_host_commands` | **`command_type`**、`status`、`error_code`、`host_id`、`attempts`、`max_attempts`、`lease_expires_at` | ❌ 无 `kind` |
| `device_pools` | `total_target`、`min_ready`、`status`、`platform`、`default_image_id`、`base_device_id` | ❌ 无 `paused`（暂停 = `total_target` 置 0） |
| `devices` | `name`、`lifecycle_status`、`health_status`、`health_reason`、`consecutive_failures`、`last_seen_at`、`reimage_status`、`adb_endpoint`、`appium_endpoint` | — |

查询一行取全部列名：

```sh
docker exec <postgres容器> psql -U device_farm_server -d device_farm -At -F'|' -c \
  "select table_name||'.'||column_name||' : '||data_type from information_schema.columns \
   where table_schema='public' order by table_name, ordinal_position"
```

---

## 附录 B：220 磁盘测量方法（避免再次误判）

**错误做法** `du -xsh /*`：
- `/var/lib/docker` 是**独立挂载点**，被 `-x` 整段跳过
- 且它是 `0700 root`，`kerr` 读不了 → `du` 报 `4.0K`，**看起来"目录是空的"**
- ⇒ 会得出「390G 去向不明」的错误结论

**正确做法**（任选）：

```sh
# 方案 1：root 视角
sudo -S -p '' bash -lc 'du -shx /* | sort -rh | head -20'
sudo -S -p '' bash -lc "find / -xdev -type f -size +3G -printf '%s %p\n' | sort -rn | head"

# 方案 2：借 docker 拿 root 视角（只读挂载，无需 sudo）
docker run --rm -v /var/lib/docker:/d:ro alpine:latest sh -c 'du -sh /d/* | sort -rh | head'
```

**实占明细**：`/var/lib/clickhouse` 308G（宿主机服务，非设备农场）/ `/var/lib/docker` ~12G
（其中 `containers` 7.5G ≈ 一个 mongodb 日志 7.3G）/ `/usr` 6.3G / `/swap.img` 4.1G /
**`/data/stacks/alcor-device-farm` 仅 25M**。


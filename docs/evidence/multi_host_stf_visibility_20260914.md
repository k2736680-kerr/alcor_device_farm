# 多宿主机 STF 可见性：ADR-0037 落地证据

日期：2026-09-14
环境：STF 位于 `10.0.30.171`；控制面（Device Farm Server/Console）位于 `10.0.80.220:18182`
对应决策：`docs/adr/0037_multi_host_stf_visibility_and_image_validation.md`

## 一、问题

设备农场从单宿主机扩展到多宿主机后，新宿主机（`10.0.30.55`）上的设备**无法进入 STF**：

- 设备 `lifecycle_status=ready`，但反复被判定 `stf_not_visible` → 隔离 → 重启 → 再隔离；
- 镜像 `109cf3d6` 状态为 `ready`（验证曾通过），却完全无法产出可用设备。

根因（两条互相独立）：

1. **`stf_registered` 语义错误**。`internal/agent/agent.go` 中该字段表达的是「本宿主机是否配了 STF ADB registrar」，不是「设备在 STF 中可见」。它被 `internal/imagecatalog/service.go` 当作镜像可用性的硬门禁，导致远程宿主机的镜像验证结论与 STF 实际可见性完全脱钩。
2. **STF ADB registrar 只接受回环地址**（`internal/adapters/stfadb/client.go`：`STF ADB server must use a loopback address`）。远程宿主机的 Agent 即使配了 registrar 也够不到 171 的 STF，因此设备**必须**由 STF 所在宿主机主动 `adb connect` 汇入。

## 二、改动

### 2.1 镜像验证与 STF 可见性解耦

`internal/imagecatalog/service.go`：验证门禁由三项改为两项。

```go
// 改前
if !result.DigestVerified || !result.Ready || !result.STFRegistered {
// 改后
if !result.DigestVerified || !result.Ready {
```

`stf_registered` 字段保留为诊断信息。设备在 STF 中的可见性改由 reconcile 的 `stf_not_visible` 对**实际运行的设备**持续把关——那才是真正的门禁，且与宿主机位置无关。

`internal/api/imagecatalog_integration_test.go` 同步调整：

- 原「`stf_registered=false` 必须失败」的用例前提已被推翻，改写为 `withoutSTFRegistered*`，断言**创建成功**且镜像数为 2；
- **新增** `notReady*` 负向用例（`ready:false` + `stf_registered:true`），断言 `IMAGE_VALIDATION_INCOMPLETE` 恰好 1 条——保证真正的门禁仍在被测试，而不是简单地删掉负向覆盖。

### 2.2 STF 宿主机侧的统一汇入

新增 `scripts/stf-sync-remote-emulators.sh`：从控制面拉取 ready 的 Android 设备，逐个在 `stf-adb` 容器内执行 `adb connect`，并**跳过本机设备**（本机 Host Agent 已通过回环 registrar 自行注册）。

配套交付：

| 文件 | 作用 |
|---|---|
| `scripts/stf-sync-remote-emulators.sh` | 同步脚本（支持 `--dry-run` / `--verbose`） |
| `scripts/install-stf-sync-timer.sh` | 一键安装（root）：装脚本到 `/opt/alcor-device-farm`、生成 env、安装并启用 timer |
| `deploy/stf/alcor-device-farm-stf-sync.service` | oneshot 服务单元 |
| `deploy/stf/alcor-device-farm-stf-sync.timer` | 周期触发（开机后 3 分钟起，每 2 分钟一次） |
| `deploy/stf/stf-sync.env.example` | `/etc/alcor-device-farm/stf-sync.env` 模板 |

`deploy/stf/README.md` 增补「多宿主机：本机 ADB server 是唯一汇入点」一节，说明路径、排障命令与新增宿主机的接入方式。

## 三、实测证据

### 3.1 控制面数据面可用

```
GET http://10.0.80.220:18182/api/v1/devices?platform=android&lifecycle_status=ready
→ total items: 1
  - Android 15-1 | android | ready | healthy | adb_endpoint = 10.0.30.171:32821
```

### 3.2 脚本端到端运行（真实控制面、真实 Token）

```
$ DEVICE_FARM_SERVICE_TOKEN=... scripts/stf-sync-remote-emulators.sh --dry-run --verbose
2026-09-14T11:47:58Z skip local device 10.0.30.171:32821
2026-09-14T11:47:58Z all remote endpoints connected              [exit 0]

$ DEVICE_FARM_SERVICE_TOKEN=... scripts/stf-sync-remote-emulators.sh --verbose
2026-09-14T11:47:58Z skip local device 10.0.30.171:32821
2026-09-14T11:47:58Z current stf-adb device table:
  List of devices attached
  10.0.30.171:32821      device product:sdk_gphone64_x86_64 ...
2026-09-14T11:47:58Z all remote endpoints connected              [exit 0]
```

### 3.3 「只拉远程」过滤与 connect 分支双向验证

仅当远程设备存在时才会走到 connect 分支。用 `STF_LOCAL_HOSTS` 翻转判定来证明该分支确实会执行：

```
$ STF_LOCAL_HOSTS=127.0.0.1 scripts/stf-sync-remote-emulators.sh --verbose
2026-09-14T11:47:29Z connected 10.0.30.171:32821      ← connect 分支真实执行并成功
2026-09-14T11:47:30Z all remote endpoints connected             [exit 0]

$ STF_LOCAL_HOSTS= scripts/stf-sync-remote-emulators.sh --verbose
2026-09-14T11:47:30Z skip local device 10.0.30.171:32821        ← 空值触发自动探测，恢复跳过
```

且重复 connect 同一 endpoint 时 `adb` 返回 `already connected to ...`，脚本将其识别为成功——幂等性成立（实测于 `verify_chain171`）。

### 3.4 单元文件校验

```
$ systemd-analyze verify .../alcor-device-farm-stf-sync.service
alcor-device-farm-stf-sync.service: Command /opt/alcor-device-farm/scripts/... is not executable
```

此提示为**预期**：脚本尚未安装到 `/opt`（安装脚本负责此事）。单元文件本身语法正确，systemd 已成功解析并进到路径检查阶段。

`sh -n`（dash 语义）在目标机上通过，安装前置的四个依赖文件全部存在。

### 3.5 幽灵设备清理（相邻问题）

`--no-cleanup` 是契约强制的（release 不得卸载 APK/清账号），代价是 STF 设备表只涨不减。清理实测：

```
DELETE /api/v1/devices?present=false → 200 {"success":true,"description":"Deleted (devices)"}

前：total=155  present=1  幽灵=154  using=4
后：total=1    present=1  幽灵=0    using=0
```

真实设备 `10.0.30.171:32821` 完好保留，控制面设备仍 ready/healthy，零副作用。数小时后复验仍为 1 台，未回潮。

## 四、测试结果

### 4.1 改动相关测试全部通过

| 范围 | 结果 |
|---|---|
| `go build ./...` | 通过 |
| `go vet ./internal/imagecatalog/... ./internal/api/...` | 通过 |
| 单元测试 `internal/imagecatalog`、`internal/agent`、`internal/adapters/*` | 全部 ok |
| 集成测试 `TestOfficialCatalogBuildValidationAndDigestCacheFlow`（真实 PostgreSQL） | **PASS (6.52s)** |
| 全仓 `go test ./internal/... -p 1` | 41 个包中 **40 个 ok**，仅 `internal/api` 失败（见 4.2） |

### 4.2 `internal/api` 的既有失败（与本次改动无关，已证实）

```
--- FAIL: TestManagementAPICompleteMockFlow (10.67s)
    management_integration_test.go:357: device audit actions=9 missing fields=0
```

断言要求审计动作数**恰好 8**（`management_integration_test.go:356`），实测 **9**。

**排除本次改动责任的证据**：把 `internal/imagecatalog/service.go` 与 `internal/api/imagecatalog_integration_test.go`
用 `git stash` 暂存回 HEAD 基线后重跑，**失败与错误码完全一致**：

```
BASELINE (changes stashed):
--- FAIL: TestManagementAPICompleteMockFlow (6.66s)
    management_integration_test.go:357: device audit actions=9 missing fields=0
```

**根因**：`internal/reconcile/service.go:515` 在触发「自我修复重启」时会写入一条设备审计：

```sql
INSERT INTO device_audit_events (..., action, ...)
VALUES (..., 'restart_device_self_healing', ...)   -- actor_type='system', actor_id='system'
```

而测试第 350-352 行的计数查询**无条件统计所有 action**，未排除系统动作：

```sql
SELECT count(*), count(*) FILTER (WHERE actor_type<>'service' OR ...)
FROM device_audit_events WHERE resource_type='device' AND resource_id=$1
```

测试自身的 8 个动作是可数的（rebuild / restart-01 / 幂等重复不计数 / quarantine(alcor-user-01) / restart-02 /
unquarantine / rebuild-01 / restart-failure），但没有为 reconcile 异步插入的自愈审计留位。
由于 reconcile 的触发依赖设备健康状态与宽限期（`STFVisibilityGrace` 默认 30s、`FailureThreshold` 3），
**该计数会随环境抖动**，属测试脆弱性。

**本次不做修改**，理由：这超出本 ADR 的改动范围，属独立的既有测试缺陷。修法建议（供后续单独处理）：
把计数查询限定为测试自己发起的动作，例如加
`AND action <> 'restart_device_self_healing'`，或改断言 `auditedActions >= 8` 并单独断言各 action 的分布。
更稳妥的做法是断言「8 个预期 action 各自恰好存在 1 条」，而不是断言总数。

### 4.3 性能用例的抖动

首轮全包运行时还出现：

```
--- FAIL: TestManagementQueryAtTwentyRPSKeepsP95BelowThreeHundredMilliseconds
    performance_integration_test.go:60: Get ".../api/v1/devices": context deadline exceeded
```

该用例在随后的全仓串行运行（`tmp/full-test.log`）中**未再失败**，属首轮同时进行 SSH 探针时的 CPU 争用所致，非代码问题。

## 五、遗留

1. ~~timer 尚未安装到 171~~ → **2026-09-15 已完成**（171 root 凭据到位后一次装成）：
   安装器 exit 0；timer `active` + `enabled`；service `code=exited, status=0/SUCCESS`；
   journal 摘要 `sync done: connected=0 skipped_local=1 failed=0`；
   `/opt/alcor-device-farm/scripts/stf-sync-remote-emulators.sh` 与仓库副本 md5 一致
   （`7531ace35eec338c6b7e88abb549d1ee`）。
   **`skipped_local=1` 是关键证据**：证明脚本确实从控制面取回了设备清单、并把本机设备正确识别后跳过，
   即「只拉远程宿主机」的过滤逻辑在真实 systemd 环境下生效。
2. ~~**远程宿主机的实机验证待补**~~ → **2026-09-15 已补齐（决定性验证）**：`10.0.30.55` 设备池恢复后，
   journal 出现 `connected=1 skipped_local=1 failed=0`，STF `present_count=2`，
   且从 171 可执行 `adb -s 10.0.30.55:32799 get-state` → `device`，Appium `/status` 正常。
   详见 `docs/evidence/host55_emulator_restore_20260915.md`。
3. **幽灵清理需周期性执行**：`--no-cleanup` 使该问题必然重现。清理周期尚未确定，可考虑并入本 timer 或单独 timer。
4. ~~**55 宿主机凭据已拿到**并做了只读体检：Host Agent 以 `device-farm` 用户**裸进程**运行（非 systemd）……~~
   **2026-09-15 更正 + 已修复**：
   - 55 的 Host Agent 其实**由 systemd 托管**（`alcor-device-host-agent.service`，`enabled` + `active`）——
     此前「裸进程」的判断源于**查错了 unit 名**（多写了 `-farm-`）；
   - 55 上「设备起不来」**不是**与 STF 可见性无关的独立问题，而是**其设备池被暂停为 `total_target=0`**
     （`device_audit_events` 留痕：`暂停 55 池自动扩容：STF 可见性未打通，避免容器空转重建`）——
     调度器因此根本不派活，Agent 侧表现完全「正常」；
   - 池已恢复为 `1/1`，设备已自动创建并接入 STF，端到端可用。详见上述 55 证据文档。
5. **`TestManagementAPICompleteMockFlow` 的既有缺陷**（见 4.2）：断言审计总数恰好 8，未排除 reconcile 插入的自愈审计。本次未修改，建议单独处理。
6. **部署期发现并修复的三个实现缺陷**（都由「真跑一遍」暴露，只看代码发现不了）：
   - `GET /api/v1/devices` 的 `page_size` 服务端上限是 **200**（`openapi/device-farm-v1.yaml` 的 `PageSize` 组件），
     传 500 直接 **HTTP 400** → 改为按页遍历取全；
   - 安装器把脚本复制到 `/opt/alcor-device-farm/scripts/` 后，脚本默认的「脚本上级目录/deploy/stf」compose 路径推导**失效**，
     service 反复以 `STF compose file not found` 失败 → 安装器改为在生成的 env 中写入 `STF_COMPOSE_FILE` / `STF_ENV_FILE`；
   - 脚本用 `DRY_RUN=0` / `VERBOSE=0` **无条件赋值**，覆盖了 systemd `EnvironmentFile` 传入的值；
     且 service 每次运行在 journal 里**完全静默**（systemd 只记 Starting/Finished），排障无从下手
     → 改为 `"${VAR:-0}"` 允许 env 覆盖，并**始终**输出一行运行摘要。

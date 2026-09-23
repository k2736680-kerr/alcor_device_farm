# STF 释放链路诊断报告（2026-09-14）

## 起因

线上 Alcor（10.0.80.220）创建的 Android 测试连续失败。其中一次 attempt 报：

```
release Device Farm reservation: device farm STF_RELEASE_FAILED:
STF 释放失败，预约仍保持激活状态 (request_id=req_db202cc4a33e5908eae40662)
```

本文档回答：**STF 释放链路是否需要加固**。

## 结论（先说答案）

**不需要改代码。** 代码逻辑经实测逐条校验是正确的；那一次 `STF_RELEASE_FAILED` 是
**部署期容器反复重建导致的网络不可达**，不是代码缺陷，也不是 STF 链路本身不稳定。

证据：`stf_release_failed` 在设备农场**全部历史审计中只出现过 1 次**
（`8471685b-233a-43a6-84fa-ad65eed00586`，2026-09-14 06:50:26）。
同类释放（`Alcor RunAttempt cleanup`）另有数十次全部成功。

## 事实取证

### 1. STF 网络链路很快，不是瓶颈

从 220 直连 171 STF（`http://10.0.30.171:7100`）：

```
GET /                     → 302   0.004s
GET /api/v1/devices       → 401   0.004s（未带 token）
```

连续 5 次调用 `time_total` 均 **4 毫秒**。生产配置
（`/data/stacks/alcor-device-farm/server.env`）为：

```
DEVICE_FARM_STF_BASE_URL=http://10.0.30.171:7100
DEVICE_FARM_STF_TIMEOUT=5s
DEVICE_FARM_STF_ATTEMPTS=3
DEVICE_FARM_STF_RETRY_DELAY=200ms
```

5 秒超时 × 3 次尝试，对 4ms 的链路是**极其充裕**的。

### 2. STF 的三种删除响应，代码分支逐条正确

用真实 token 实测 `DELETE /api/v1/user/devices/{serial}`：

| STF 响应 | 实测样例 | 代码处理位置 | 判定 |
|---|---|---|---|
| `404 Device not found` | 不存在的 serial | `client.go:268` DELETE+404 → 直接 `return nil` | ✅ 正确 |
| `403 Not owned by you`，且设备 `present=false` | `10.0.30.171:32839` | `client.go:150` + `deviceHasNoActiveUsage` → `return nil` | ✅ 正确 |
| `403 Not owned by you`，且设备 `present=true && using=true` | 真被他人占用 | 保持 error（注释明确要求） | ✅ 正确 |

实测输出：

```
DELETE 10.0.30.171:39999 → {"success":false,"description":"Device not found"}            HTTP=404
DELETE 10.0.30.171:32839 → {"success":false,"description":"You cannot release this device..."} HTTP=403
```

`client.go:161-164` 的注释与实现一致：只有"在线且仍标记 used"的设备才有实质 claim，
其 403 必须保持错误（可能真属于别的用户）。

### 3. STF 存在历史幽灵占用（不导致本次失败，但需清理）

STF 清单共 **155 条设备记录，仅 1 台真实在线**（`10.0.30.171:32791` = `Android 15-1`）。
其中 3 条为 `using=true` 且 `present=false` 的幽灵占用：

```
10-0-30-171.nip.io:32839   present=false using=true
10-0-30-171.nip.io:32845   present=false using=true
10.0.30.171:32779          present=false using=true
```

**同一台物理设备在 STF 里存在两种 serial 写法**（`nip.io:端口` 与 `IP:端口`），例如 `32839`：

```
{'serial': '10-0-30-171.nip.io:32839', 'present': False, 'using': True}   ← 持 ACL 的那条
{'serial': '10.0.30.171:32839',        'present': False, 'using': False}
```

设备农场 `devices.stf_serial` 当前**三台安卓设备全部为空**，`repository/reservation.go:462`
用 `COALESCE(NULLIF(stf_serial,''),serial)` 回退到 `serial`。

> 这是既有设计（回退到 serial），配合 `deviceHasNoActiveUsage` 的 `present=false` 判定，
> 幽灵占用会被正确视为"可释放"。**不构成功能性缺陷**，但 154 条陈旧记录会拖慢
> `Inventory()` 的遍历，建议定期清理。

### 4. 真正的失败原因：部署期容器反复重建

排查期间观察到设备农场控制面容器**被反复重建**：

| 时间（UTC） | 现象 |
|---|---|
| 08:38:20 | server 容器启动（镜像 `predeploy-20260914-fetch-fix`）|
| 08:50:57 | server 容器**再次**启动（RestartCount=0，说明是重新 create 而非 restart）|
| 08:27:49 | `device-farm-ios-tunnel-1` **Exited (255)** |

同时 `alcor-android-worker` 日志显示连接 18182 反复失败：

```
refresh mobile Worker capacity failed; keeping current slots
  ... dial tcp 10.0.80.220:18182: connect: connection refused
```

`ios-tunnel` 以 255 退出、server 容器被重建 —— 这是**部署动作导致的控制面短暂不可用**。
在此窗口内，`releaseSTF` 的上游（设备农场 server → STF 的调用链）会整体失败，
表现为 `STF_RELEASE_FAILED`。

### 5. 预约的最终归宿：reaper 兜底正确

失败预约 `8471685b` 的完整时间线：

| 时刻 | 事件 |
|---|---|
| 06:49:39 | 创建，拿到 `f0ae3069`，租期 900s（至 07:04:39）|
| 06:50:26 | `stf_release_failed` → attempt 判 `infra_failed` |
| **07:06:40** | **reaper 按"租期 + 宽限期过期"回收**，补上 `released_at`，终态 `expired` |

即：STF 释放失败后，**reaper 兜底在 16 分钟后正确清理了预约**，设备没有永久泄漏。
`ck_device_reservations_*` 约束与 `uq_device_reservations_active_device` 唯一索引
保证了不会出现同设备双活跃预约。

## 对"让 release 失败不把整个 attempt 判 infra"的评估

**不建议改，理由如下：**

1. **语义上 release 失败必须可见。** `android_executor.go:164-176` 的 `defer` 中，
   release 失败会在 `returnErr == nil` 时提升为错误。若静默吞掉，设备泄漏将无人察觉 ——
   虽然 reaper 会兜底，但那要等租期结束（最长 `max_lease_seconds=7200s`）。
2. **已经有两层兜底**：STF 侧 3 次重试 + reaper 过期回收。再加"忽略错误"是第三层，
   且是唯一会**隐藏真实故障**的一层。
3. **本次失败的真实成因是部署抖动**，正确对策是**避免在有测试运行时重建控制面容器**，
   而不是降低 release 的错误可见性。

若确实要提升健壮性，**可接受的折中是**：release 失败时区分错误类型 ——
`Retryable=false` 且为 `INVALID_ARGUMENT`/`404`（设备根本不存在）可直接视为成功；
其余保持失败。但这属于优化，**非本次故障的必要修复**，且需先立 ADR。

## 建议动作

1. **不要在有测试运行时重建设备农场控制面容器**（本次直接诱因）。
2. 清理 STF 中 154 条陈旧设备记录与 3 条幽灵占用（运维动作，非代码）。
3. 为 `devices.stf_serial` 补齐正确值，消除 `nip.io` / `IP` 双写法歧义
   —— 这能避免 `Inventory()` 匹配退化，属长期收益。
4. 监控 `stf_release_failed` 审计事件频次；若从"偶发 1 次"变成趋势，再按第 3 点立 ADR 做类型化处理。

## 更正：不要盲目调大 Android 池（2026-09-14 补充实测）

本文档初版曾建议"调大 Android 池 `total_target`"，
**该建议已作废** —— 初版未核算宿主机内存，是错误结论。实测数据如下。

### 171 宿主机容量实测（`device_hosts` 上报值）

| 项目 | 值 |
|---|---|
| `memory_total_mb` | 15594（15.2 GiB） |
| `memory_available_mb` | 7315 |
| `used_capacity.memory_mb` | 7168（= 1 台模拟器） |
| `used_capacity.device_slots` | 1 |
| `cpu_cores` | 12（已用 5） |

单台模拟器容器实测占用（`docker stats`）：**6.89 GiB / 7 GiB 限额（98.4%）**。

### 代入容量算法（`internal/capacity/capacity.go:66-85`）

```
accountedMemory = 15594 - 2048 - 7168 - 0 = 6378 MB
realtimeMemory  =  7315 - 2048 - 0        = 5267 MB
availableMemory = min(...)                = 5267 MB

byMemory = 5267 / 7168 = 0     ← 限制项
byCPU    = (12 - 1 - 5) / 5 = 1
byDisk   = 387194 / 6144  ≈ 63
additional = min(0, 1, 63, ...) = 0
```

**结论：即使把 `total_target` 调大，调度器也会返回 0 并拒绝扩容
（`Result.Fits=false`，中文提示"宿主机内存不足：内存还缺 N MB"）。**

即：`total_target=1` 是"物理内存上限 + 容量算法自保 + 2048 MB 保留垫"三重约束下的
正确结果，**不是保守配置，也不存在"调大后 OOM 死机"的风险**——扩容请求根本不会被执行。

### 真正可行的三条路

| 方案 | 代价 | 评价 |
|---|---|---|
| A. 给 171 加内存至 32 GB | 硬件成本 | 治本，`memory_total_mb` 由心跳实时上报，加完自动生效 |
| B. 下调模拟器容器限额（7168→4096 MB） | 需实测 App 稳定性 | 可榨出第二台，但须同步调 `guest_memory_mb`，有业务风险 |
| C. 保持串行，压缩单次占用时长 | 无 | **最现实** |

方案 C 依据：`4b437f30` 一次运行 301 例耗时 **2.5 小时**（06:34→09:10），
而模拟器 CPU 占用仅 **12.66%**（12 核约 1.5 核）。

> **CPU 空闲 + 内存吃满 + 耗时 2.5 小时** ⇒ 瓶颈不在算力，而在测试脚本的等待与重试。
> 一台设备被单个 run 独占 2.5 小时，是 Android 侧排队的直接原因；
> 优化脚本耗时比扩容内存的收益更快、成本更低。

## 环境快照（2026-09-14 09:00 UTC）

- 设备农场 server：`alcor-device-farm:predeploy-20260914-fix*`，healthy
- 两台宿主机均 `online`，心跳延迟 < 1s
- Android 池 `8b97a9e7`：`status=active`，`total_target=1`，`min_ready=1`，`max_concurrency=1`
- 设备：`ef26de28`(Android 15-1) ready/healthy 唯一可用；`c9516878`/`f0ae3069` deleted
- **现场复现结果**：`POST /api/v1/device-reservations` → `pending` → `active` 用时 < 2s，
  正确分配 `ef26de28`，release 返回 200 —— **预约链路功能正常**

## 附：另一条独立问题线

`SLEEP_PERIOD_EMPTY_001` 的失败与 STF 无关：

```
error_class:  dafit_case_failed
error_detail: DataInjectionEntryError: 连续点击详情标题后，
              App 未打开数据填充编辑页或密码框
```

属 DaFit 测试脚本 / 被测 App 交互问题，归 `dafit_auto_platform` 项目，不在设备域范围。

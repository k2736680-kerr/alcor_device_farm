# 并发分配策略核对与调整（2026-10-13）

## 1. 需求（用户原话归纳）

- **Android（3 台服务器）**：
  1. 有空闲虚拟机时**优先复用**，不新建；
  2. 3 台服务器都各有 1 台在用时，再判断**该机剩余内存**是否够开第 2 台；
  3. 内存够就开，不够就**排队等待**；
  4. **每台服务器内存大小不同**，必须按各自实时可用内存判断，不能用统一计数。
- **iOS（1 台服务器）**：上限 2 台，再多排队。

## 2. 核对结论

| 需求 | 现状 | 判定 |
|---|---|---|
| 优先复用空闲设备 | `pool_selector.go` Branch A 直接复用 ready+healthy 设备 | ✅ 符合 |
| 按剩余内存决定能否再开 | `capacity.Evaluate`：`availableMemory / container_memory_mb` | ✅ **逻辑已存在且正确** |
| 3 台各 1 台后才叠第 2 台 | 宿主按 `ORDER BY h.id` 顺序填充，非"先铺开再叠" | ⚠️ 顺序不同，但结果符合（见 §4） |
| 内存不够排队 | 宿主返回 `INSUFFICIENT_HOST_RESOURCES`，预约保持 pending 等释放 | ✅ 符合 |
| 各机内存不同 | `capacity.Evaluate` 逐机用各自 `memory_available_mb` 计算 | ✅ 符合 |

**核心发现：用户要的逻辑本来就已实现**（`internal/capacity/capacity.go` +
`warmpool.lockHostCapacity`）。真正的障碍是**两个计数上限在容量判断之前就把它封死了**。

## 3. 阻塞点：`max_concurrency` / `max_instances` 提前封顶

`internal/warmpool/controller.go:1353`：

```go
demandTarget := min(activeDemand+pendingDemand, min(maxConcurrency, imageMaxInstances))
```

- `max_concurrency=1`（池级）与 `max_instances=1`（镜像级）取小值 ⇒ 每池只允许 1 台，
  **根本不会走到按内存的判断**；
- 实测确认：把 `max_concurrency` 提到 3 后，171/55 各开 2 台、54 只开 1 台，
  说明放开后**内存判断确实接管**。

## 4. 实测数据（三台宿主的真实差异）

空闲时（`free -m` 的 available）：

| 服务器 | `MemTotal` | 空闲 available | 减预留 2048MB | 实际能开 |
|---|---|---|---|---|
| 171 | 15594 MB | 12831 MB | 10783 MB | **2 台** |
| 54 | 15735 MB | 11530 MB | 9482 MB | **1 台** |
| 55 | 15657 MB | 13363 MB | 11315 MB | **2 台** |

三台总量几乎相同（约 15.6 GB），但**空闲可用内存不同**（171/55 可开 2 台，54 只够 1 台），
正是用户所说"每台内存不一样"的真实体现。

跑 1 台后 `MemAvailable` 从 ~11.5 GB 降到 ~6.8 GB（模拟器容器实测占 4.655 GiB / 5 GiB 上限），
减预留后不足 5120 MB ⇒ 第二台必然 `INSUFFICIENT_HOST_RESOURCES`。

## 5. 调整后的配置

| 层级 | 字段 | 值 | 作用 |
|---|---|---|---|
| 池 | `total_target` | 0 | 按需：空闲清空 |
| 池 | `min_ready` | 0 | 不保温 |
| 池 | `max_concurrency` | 8 | **安全网**，不提前封顶 |
| 镜像 | `max_instances` | 8 | **安全网**，不提前封顶 |

**真正的单机约束交给 `capacity.Evaluate` 按实时剩余内存判定。**

### 5.1 调整顺序有讲究

`SetPoolImage` 强制 `min_ready == max_instances`，且写入会**回写池级
`total_target`/`min_ready`**。因此必须：

1. **先** `PUT /device-pools/{id}/images/{image_id}`（设 `max_instances`）；
2. **后** `PUT /device-pools/{id}`（设 `total_target=0 / min_ready=0 / max_concurrency=8`）。

顺序颠倒会把池级 `min_ready` 抬成 8，导致池常驻 8 台热备，**破坏按需清空**（已实测复现）。

## 6. 验收证据

### 6.1 内存驱动分摊

投 5 个不指定池的 Android 预约，建机流水：

```
06:58:53 | create | succeeded | 10.0.30.54   ← 第 1 台
06:58:53 | create | FAILED    | 10.0.30.54   ← 第 2 台：INSUFFICIENT_HOST_RESOURCES（内存不够，正确拒绝）
06:58:53 | create | succeeded | 10.0.30.55
06:58:53 | create | succeeded | 10.0.30.171
06:58:53 | create | succeeded | 10.0.30.171   ← 171 第 2 台成功
```

结果：**171 × 2、55 × 1、54 × 0**（54 的第二台被宿主按内存拒绝并自动回收），
3 个预约 `active`、2 个排队。**完全符合用户的第 2、3 条要求。**

### 6.2 按需清空

释放后设备数 `3 → 0`，池回到 `0/0`。清理由 reconcile 周期驱动，约 **60~90 秒**完成
（非即时，属正常）。

### 6.3 对比改造前

| 场景 | 改造前（concurrency=1） | 改造后（内存驱动） |
|---|---|---|
| 3 台服务器并发上限 | 每池 1 台，3 台封顶且不能同机叠 | 171/55 各 2 台、54 1 台 → **最多 5 台** |
| 大内存机器 | 无法利用 | 自动多开，无需改配置 |

## 7. 遗留与风险

1. **跨池串台**：`lockHostCapacity` 的宿主条件
   （`FROM device_hosts h WHERE h.status='online' AND NOT h.draining`）
   **未与池绑定**，所以预约指定 54 的池、设备可能建到 55。已实测复现两次。
   → 用户已确认要修成"预约指定哪台就只在那台建"，尚未实施。
2. **54 上并存非农场负载**：ray 集群 `worker-04`（内存上限 14 GiB）、
   `node-exporter` 会与模拟器抢内存，导致 54 的有效容量低于硬件标称。
   171/55 上也有 worker-05 等。**排障时须把这些计入。**
3. **每台规格 5120 MB 偏大**：若要单机跑 2 台以上，需下调
   `container_memory_mb`（并确保其他负载让出内存）。
4. **`min_ready == max_instances` 的耦合**是既有约束（`management/service.go:426`），
   本方案通过"先镜像后池"的顺序绕过，未改动代码。
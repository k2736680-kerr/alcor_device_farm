# 池与宿主机的归属关系核查（2026-10-13）

> ⚠️ **本文经过 5 次修正，前四版结论都有偏差。** 保留修正过程，是为了
> 记录「为什么这个语义反复被搞错」，避免第六次。
> **最终结论见 §1（ADR-0043），验证见 §5-§6。**

## 1. 最终结论（需求方 2026-10-13 确认，ADR-0043，已修复并生产验证）

**池 = 一台宿主机。预约由农场按各宿主实时空闲内存分配，挑中谁就用谁对应的池；
全部满载就原地排队，有空闲立刻分配，上限 30 分钟。**

需求方原话（最终定稿）：

> 「alcor 给到农场一个预约的请求，农场根据这个请求自动的找空闲的机器，农场感觉 55 是空闲的，
> 就给了 55 这个池，这个池就应该接收这个预约，自动的创建虚拟机。当有很多个预约的时候，
> 已经所有的池已经全部占满了内存，无法再增加虚拟机了，这时候就等待看那个虚拟机运行结束，
> 再往这个池里面分配」

> 「当农场发现全部没有空闲的时候就是在原地等待 30 分钟，有空闲农场就分配，
> 30 分钟还没有分配就报错」

> 「在我的概念里面池和宿主机是一个东西…现在这就是一个东西啊，
> 又不是一个宿主机里面有很多个池」

正确的调度语义：

| 环节 | 做法 |
|---|---|
| 挑机器 | 农场按各宿主**实时空闲内存**自己选，调用方**不指定** |
| 定池 | **挑中谁，就用谁对应的池**接收并创建；池不是独立输入 |
| 全满 | **原地排队**（保持 pending，不建机、不溢出、不乱建），每轮重新检查 |
| 有空闲 | **立刻分配**给那台空闲宿主 |
| 排队上限 | **30 分钟**；超时报错「等不到虚拟机，需要检查」 |

**`pool_id` 不作约束**：即使调用方显式指定，也只用于追溯/展示，不限制分配宿主
（需求方明确选 B）。

### 1.1 三个环节各自的对错（容易混淆，务必分清）

实测数据显示只有**第二个**环节是错的：

| 环节 | 修复前状态 |
|---|---|
| 预约池 = 归属池 | ✅ **一直正确**（140/140）—— 预约 54 就归 54 |
| 归属池 = 宿主机 | ❌ **错的** —— 54 满后溢出到 55，归属仍记 54 |
| 调度按空闲溢出 | ✅ 正确 —— 54 满了自动用 55 |

⚠️ **诊断陷阱**：「预约池 = 归属池」一直成立，会掩盖真正的错误。
只看这一条会得出"没问题"的错误结论（第 2 版就是这么错的）。

## 2. 现状实测（问题规模）

### 2.1 池名是固定的，从来没变过

```
id 8b97a9e7…  name android-10.0.30.171   created 2026-08-06 08:28:22
id a90ef672…  name iOS - 10.0.33.68      created 2026-08-20 03:47:57
id c8e22592…  name android-10.0.30.55    created 2026-09-14 10:03:40
id 149c3632…  name android-10.0.30.54    created 2026-10-13 03:35:28
```

池是**固定的容器**；设备是**流动的**。设备名（`Android设备-<6位随机码>`）
来自创建它的池的命名体系，不含机器名。

### 2.2 设备归属与实际宿主不符

```sql
select p.name as pool_name, h.address as actual_host, count(*) as devices
from devices d
join device_pool_devices pd on pd.device_id=d.id
join device_pools p on p.id=pd.pool_id
join device_hosts h on h.id=d.host_id
where h.host_type in ('docker_emulator','hybrid')
group by p.name, h.address order by p.name, h.address;
```

| 池（归属） | 实际宿主 | 历史台数 | 一致? |
|---|---|---|---|
| `android-10.0.30.171` | 10.0.30.171 | 39 | ✅ |
| `android-10.0.30.171` | 10.0.30.55 | 4 | ❌ |
| `android-10.0.30.171` | 10.0.30.54 | 1 | ❌ |
| `android-10.0.30.54` | 10.0.30.54 | 9 | ✅ |
| `android-10.0.30.54` | 10.0.30.55 | 4 | ❌ |
| `android-10.0.30.55` | 10.0.30.171 | 14 | ❌ |
| `android-10.0.30.55` | 10.0.30.54 | 2 | ❌ |
| `android-10.0.30.55` | 10.0.30.55 | 17 | ✅ |

**不符 25 台 / 共 90 台。**

单台实例：

```
设备 dfe68b：归属池 = android-10.0.30.55，实际宿主 = 10.0.30.54   ← 不符
设备 1f99d0：归属池 = android-10.0.30.54，实际宿主 = 10.0.30.55   ← 不符
```

### 2.3 内存约束没有被违反（一个曾被误读的点）

池 `android-10.0.30.54` 名下"跑在 55 上"的 4 台，**不是同时存在**：

| 设备 | 创建 | 销毁 | 存活 |
|---|---|---|---|
| 9b852e | 06:04:23 | 06:07:23 | 180s |
| e025e5 | 06:43:23 | 06:52:53 | 570s |
| ae08b5 | 06:43:23 | 06:52:53 | 570s |
| 1f99d0 | 06:58:53 | 07:05:53 | 420s |

06:04 一台 → 06:43 同时两台 → 06:58 一台，全部 `deleted`。
**同一时刻在 55 上最多 2 台**，符合 55 的内存上限（~13GB，单台 5120MB + 2048MB 预留）。

⚠️ 教训：`count(*)` 统计的是**历史累计出现次数**，不是**同时在线台数**。
用这个数字论证"超内存"或"超并发"都是错的。

## 3. 现有调度代码的真实行为

`internal/warmpool/controller.go` 的 `lockHostCapacity`：

```sql
FROM device_hosts h WHERE h.status='online' AND NOT h.draining
  AND h.host_type IN ('docker_emulator','hybrid')
ORDER BY h.id FOR UPDATE OF h SKIP LOCKED
```

- 函数签名**不接收 poolID**（`grep poolID` 出现 0 次）——建机时完全不看池；
- 于是"哪台宿主"与"哪个池"是**两个独立决定**，这是归属错位的根因。

调用链是**先选池、后选宿主**：

```
reservation.create → selectPoolForRequest（挑池，为拿镜像/能力）
                   → warmpool.Provision/reconcile（按全局空闲挑宿主）
                   → 登记 device_pool_devices 时用的是【当初挑的池】
```

选了池之后就不再根据实际宿主修正归属，所以池名与机器脱钩。

### 3.1 宿主倾向（独立的次要问题）

`ORDER BY h.id` 意味着**多台都装得下时固定偏向 id 小的宿主**，不是"挑最空的"。
不影响"装得下就会建"（不会白等），只影响负载均衡。

## 4. 文档依据与冲突

| 问题 | 文档怎么说 | 出处 |
|---|---|---|
| 池是逻辑分组 | 「设备池是预约和调度使用的**逻辑分组**」 | `docs/04_mvp_functional_spec.md:152` |
| 池→设备 vs 设备→宿主 | 池经 `device_pool_devices` 关联设备；设备经 `devices.host_id` 关联宿主；**池与宿主无直接字段** | `docs/04_mvp_functional_spec.md:383-385` |
| `max_concurrency` | **池级总量**，不是每台上限 | `docs/04_mvp_functional_spec.md:382,268` |
| 创建时必须入池 | 「Device 创建时**同步建立 Pool membership**，不允许…成为无主资源」 | `docs/adr/0006_fixed_min_ready_warm_pool.md:21` |
| 池名命名约定 | **未规定** | `docs/04_mvp_functional_spec.md:382` |
| 池↔宿主是否一对一 | **未规定**（数据模型里不存在该关系） | — |
| 选宿主的算法 | **未规定** | — |

**关键**：`docs/adr/0006` 要求「创建时同步建立 Pool membership」，
但**没规定 membership 该指向哪个池**。本次修复要补的正是这一条语义。

### 4.1 已修正的文档冲突

- 本文前两版曾主张"池不绑定宿主、行为正确"——**已推翻**。
- `docs/evidence/concurrency_policy_20261013.md` 曾把"跨池串台"列为已确认待修缺陷
  （方向与现在的结论**一致**，但当时的需求方确认记录不完整）。
- 曾有三处对齐表引用 `ADR-0041`（"按池名推导宿主地址并收窄候选"），
  该 ADR 文件**不存在**（悬空引用），且其方向与最终结论**相反**，已全部清除。

## 5. 修复与验收

### 5.1 改动（ADR-0042）

只改一个点：`internal/warmpool/controller.go` 的 `createDeviceCommand` 登记
`device_pool_devices` 时，归属池由**实际宿主的 address** 推导，不再用调用方传入的
`poolID`。同一处返回该归属池，供 `Provision` 回写 `total_target`（否则计数会记到
设备并不在的那个池）。

新增 `resolvePoolForHost`：按 `device_hosts.address` 匹配池名
（`android-<address>`；iOS 为 `iOS - <address>`），限定平台与 `status='active'`。
推导不到时保留原 `poolID`，避免因归属推导失败而建不出设备。

**建机挑宿主的逻辑一行未动** —— 仍按各宿主实时剩余内存选，允许溢出。

### 5.2 真实 PostgreSQL 回归（先证伪、再证真）

新增 `internal/warmpool/controller_pool_attribution_test.go`：

| 用例 | 未修版本 | 已修版本 |
|---|---|---|
| `TestDevicePoolAttributionFollowsActualHostOnOverflow` | ❌ FAIL | ✅ PASS |
| `TestDevicePoolAttributionFallsBackWhenHostHasNoNamedPool` | — | ✅ PASS |

未修版本的失败信息精确复现缺陷：

```
设备跑在 10.0.30.55，归属池应为 android-10.0.30.55，
实际是 android-10.0.30.54（归属未跟随实际宿主）
```

全量回归串行（`-p 1`，避免共库假失败）：`warmpool`、`reservation`（40 用例）、
`hostcommand`、`reconcile`、`scheduler`、`capacity` **全部 PASS**。

### 5.3 生产端到端（`10.0.80.220`，镜像 `pool-attr-20261013`）

从干净状态（0 台设备）连续预约 `android-10.0.30.54`：

| 第几次 | 预约池 | 实际宿主 | 归属池 | 结果 |
|---|---|---|---|---|
| 1 | `android-10.0.30.54` | 10.0.30.54 | `android-10.0.30.54` | ✅ OK |
| 2 | `android-10.0.30.54` | 10.0.30.54 | `android-10.0.30.54` | ✅ OK |
| 3 | `android-10.0.30.54` | **10.0.30.55** | **`android-10.0.30.55`** | ✅ **OK（修复生效）** |

汇总：**3 台设备，3 台 OK，0 台 FAIL。**

**第 3 行是本次修复的直接证据**：54 装不下后溢出到 55，归属**如实跟随到 55 池**；
修复前这一行会是"归属 54、跑在 55"。

修复前后对比（同一场景）：

| | 修复前 | 修复后 |
|---|---|---|
| 设备跑在 | 10.0.30.55 | 10.0.30.55 |
| 归属池 | `android-10.0.30.54` ❌ | `android-10.0.30.55` ✅ |

清理后：活跃设备 **0 台**，各池回到 `total_target=0 / min_ready=0`。

### 5.4 ADR-0043 改动（分配模型本身）

ADR-0042 只修了"归属跟随宿主"，但**分配模型仍按 pool_id 把队列切成三段** ——
预约钉在 54 池就只驱动 54 的扩容，55 空着也看不见。ADR-0043 把队列改成
**全局一条**（按平台+能力+可用性），池由挑中的宿主决定：

| 改动点 | 修前 | 修后 |
|---|---|---|
| `LockNextAllocatablePending` | `pd.pool_id = r.pool_id` 硬过滤 | 去掉池过滤，全局 FIFO |
| `LockMatchingDevice` | 同上，4 参（含 poolID） | 去池过滤，3 参 |
| `LockPoolPolicyForHost` | —（不存在） | 按设备实际宿主取池策略（含 membership 回退） |
| warmpool `pendingDemand` | `WHERE pool_id=$1` 按池切 | 全局需求，按 capabilities |
| 选池内存判据 | 完全不看宿主内存 | `dynamic_v1` 心跳 30s 内 + 内存≥2048+镜像 ramMb |
| 条件 B 宿主限定 | 只判 host_type（跨池误判） | 限定本池宿主（`android-<address>`），含未命名回退 |
| `PendingTimeout` | 10 分钟 | **30 分钟** |
| 超时文案 | — | 「等不到虚拟机，需要检查」 |

新增 `internal/repository/allocation_global_queue_integration_test.go`（先证伪再证真）：

| 用例 | 旧代码（加回池过滤） | 新代码 |
|---|---|---|
| `TestAllocationCrossesPoolsWhenTheOtherPoolHasAMatchingDevice` | ❌ FAIL：`repository resource not found`（预约钉在 X 池、Y 池有设备也匹配不上） | ✅ PASS |
| `TestAllocationStillRespectsCapabilitiesAfterPoolFilterRemoval` | — | ✅ PASS |

旧代码的失败精确复现「55 空闲却在干等 54」：预约落在池 X、池 Y 有可用设备，
全局队列却领取不到它 → 永远 pending → 排队到 PENDING_TIMEOUT。

全量回归串行：warmpool / repository / scheduler / reservation（40 用例）/ reaper /
config / hostcommand / reconcile / capacity **全部 PASS，0 失败**。

### 5.5 生产端到端（`10.0.80.220`，镜像 `host-alloc-20261013`）

**`pending_timeout` 实测 30 分钟**（启动日志 `lease_pending_timeout: 1800000000000`）。

| 场景 | 结果 | 证据 |
|---|---|---|
| 1. 不指定池，农场自选空闲建机 | ✅ | 预约（无 pool_id）→ 农场选 54 池 → 自动建机 → active，设备跑在 54 |
| 2. 满载排队，不溢出不乱建 | ✅ | 并发约 5 台，3 台宿主内存各占满（171=3216MB/54=6429/55=5073，均 <7168 开不出下一台）→ 第 4 台保持 pending，**0 台多建** |
| 3. 释放后自动分配 | ✅ | 释放 1 台 171 的 active → 排队的 93bc4db1 **12 秒内**补上到 171 |

池由机器决定的直接证据：场景 2 的 5 台预约 `pool_id` **全部**是 54 池
（`149c3632...`），但设备实际分散在 171/54/55 三台 —— `pool_id` 只是落地记录，
不作分配约束。

清理后：活跃设备 0 台，各池回到 `total_target=0 / min_ready=0`。

## 6. 教训

这个语义被搞错了 **4 次**，根因同一个：**从池名反推规则，而不是先问清池代表什么。**

| 版本 | 错误假设 | 后果 |
|---|---|---|
| 第 1 版 | 「池名声明宿主归属，调度破坏了它」 | 按池名收窄宿主，**破坏了闲置调度**（已 revert） |
| 第 2 版 | 「池不绑定宿主，行为正确，只是名字误导」 | 差点只改名字，**漏掉真正的归属错位** |
| 第 3 版 | 「用历史累计数据论证有 25 台串台」 | 统计口径错（累计 ≠ 同时），**把历史当现状** |
| 第 4 版 | 池=机器，归属必须跟着实际宿主 | ✅ 需求方确认，已修复并生产验证 |

**做法上的教训**（三条，都是这次踩出来的）：

1. **语义问题先问需求方**"这个词代表什么"，不要靠读代码或读名字推断。
2. **区分"累计"与"同时"**：`count(*)` 是历史累计，不能用来论证并发/内存/现状。
3. **验证"现在"而不是"历史"**：清空历史数据后从零重建，才能反映当前代码行为。
   第 2 版的误判正是因为没有清空历史重测。

## 7. 相关

- `docs/adr/0043_reservation_allocates_by_host_capacity.md`：分配模型（全局队列 + 内存门槛）的 ADR
- `docs/adr/0042_device_pool_attribution_follows_host.md`：归属跟随宿主的 ADR
- `docs/adr/0006_fixed_min_ready_warm_pool.md`：创建时同步建立 pool membership
- `docs/04_mvp_functional_spec.md:152,159,382-385`：池定义与数据模型
- `docs/evidence/concurrency_policy_20261013.md`：按宿主实时剩余内存决定开几台
- `internal/capacity/capacity.go`：`Evaluate`，内存驱动的容量判据
- `internal/warmpool/controller.go`：`lockHostCapacity`（选宿主）、`resolvePoolForHost`（定归属）
- `internal/reservation/pool_selector.go`：`selectPoolForRequest`（选池）
# ADR-0042：设备归属的池由实际宿主机决定

## 状态

已确认（2026-10-13）。取代已作废的 ADR-0041（该文件从未定稿，其"按池名收窄候选宿主"方向与需求方最终确认相反）。

## 背景

多台宿主机与设备池**一对一**：

| 宿主机 address | 池名 |
|---|---|
| `10.0.30.171` | `android-10.0.30.171` |
| `10.0.30.54` | `android-10.0.30.54` |
| `10.0.30.55` | `android-10.0.30.55` |
| `10.0.33.68` | `iOS - 10.0.33.68` |

已实测确认（2026-10-13，清空历史后从零新建复现）：**每台宿主恰好 1 个池，每个池恰好 1 台宿主。**

需求方对池的语义要求（逐次澄清后确认）：

> 「android-10.0.30.54 这个一看就是在 54 这个服务器下面的虚拟机啊…
> 你扫描空闲把任务给到 54 就是这个名字啊，给到 55 服务器，名字就应该是 55 啊」

> 「分配到 55 的池（55 的宿主机），不就没有问题了…这是为了后面的追溯」

即：**池代表宿主机，设备归属的池必须等于它实际运行的那台宿主机。**

## 问题

设备农场原有调度是**先选池、后选宿主**，且两步互不知情：

```
reservation.create
  → selectPoolForRequest()          挑池（为拿镜像/能力）
  → warmpool.Provision/reconcile    按【全局空闲内存】挑宿主
  → createDeviceCommand()           登记归属 = 当初挑的池   ← 与实际宿主脱钩
```

`internal/warmpool/controller.go` 的 `createDeviceCommand` 里，
`hostID`（写入 `devices.host_id`）与 `poolID`（写入 `device_pool_devices.pool_id`）
是**两个独立入参，无任何联动**：

```go
:1526  INSERT INTO devices(..., host_id, ...)          VALUES(..., hostID, ...)
:1532  INSERT INTO device_pool_devices(pool_id, ...)   VALUES(poolID, ...)
```

于是当某台宿主容量不足、调度溢出到另一台空闲宿主时，
设备**实际跑在 A 机器上、归属却记在 B 池下**。

### 实测复现（清空历史后从零新建）

预约 `android-10.0.30.54`（54 内存约 11.5GB，单台 5120MB + 2048MB 预留 ⇒ 可开 2 台）：

| 第几次预约 | 实际宿主 | 归属池 | 结果 |
|---|---|---|---|
| 1 | 10.0.30.54 | `android-10.0.30.54` | ✅ 一致 |
| 2 | 10.0.30.54 | `android-10.0.30.54` | ✅ 一致 |
| 3 | **10.0.30.55** | `android-10.0.30.54` | ❌ **不符** |
| 4 | **10.0.30.55** | `android-10.0.30.54` | ❌ **不符** |

**溢出行为本身正确**（54 满了自动用 55 的空闲，正是需求方要的），
错的是**归属没有跟着宿主走**。

⚠️ 注意：「预约池 = 归属池」这一环**一直是对的**（140/140）。错的只有
「归属池 = 宿主机」。诊断时不要被"预约与归属一致"掩盖这个真正的不一致。

## 决策

1. **建机挑宿主的逻辑保持不变**：仍按各宿主**实时剩余内存**挑（`capacity.Evaluate`），
   允许溢出到其他空闲宿主。**不按池名收窄**。

   > 这一条是对已作废 ADR-0041 的直接否定。按池名收窄会让"54 满了却不让用 55"，
   > 与需求方要的"按空闲安排"相反。

2. **设备归属的池由实际宿主的 address 推导**，不采用调用方传入的 poolID。

   登记 `device_pool_devices` 时，用所选宿主的 `device_hosts.address` 匹配池名
   （`android-<address>`；iOS 为 `iOS - <address>`），得到该宿主对应的池。

3. **宿主机与池一对一，故不新增表/字段/API。**

   不新增 `device_pools.host_id` 或 `device_hosts.pool_id`：
   一对一关系下"按地址匹配池名"与"按外键查池"等价，而池名已经承载了这个信息。
   （需求方明确："宿主机和池的一对一的关系，没有区别啊"。）

4. **推导不到对应池时保留原 poolID 并记录**，不静默丢弃。

   若某台宿主没有同名池（新增宿主但还没建池、池被改名），
   归属退回调用方传入的 poolID，避免因归属推导失败而建不出设备。

5. **不影响预约语义**：`device_reservations.pool_id` 仍记录调用方请求的池，
   不因为设备溢出而改动。这样"我约的是哪个池"与"设备实际在哪台机器"分别可查，
   满足追溯需要。

## 边界

- 不新增 API、表、字段、状态或模块；不改 Console。
- 不改 `lockHostCapacity` 的候选宿主范围与 `capacity.Evaluate` 算法。
- 不改预约的选池逻辑（`pool_selector.go` 的可选性、并发上限、权重轮转）。
- 不改 iOS 的宿主指定方式（iOS 由上游显式给 `input.HostID`）；但 iOS 若存在
  归属推导点，应使用同一规则。
- 池名仍需保持 `含宿主 address` 的既有命名；池与宿主的一对一是本决策的前提。

## 后果

- 「设备在哪台机器上，就归属哪个池」，设备可追溯到真实宿主机。
- 溢出到其他宿主时，归属如实反映宿主，不再出现"归池 A、跑在 B"。
- 依赖"池名含宿主地址 + 一对一"这一既有事实。若将来出现一台宿主多池
  （或一池多宿主），本决策需重新评估（届时可能需要显式绑定字段）。
- 需要更新 `docs/02_architecture_alignment.md` 与
  `docs/03_alcor_generation_alignment.md` 的对应条目（rule 11）。

## 落地实现

| 文件 | 作用 |
|---|---|
| `internal/warmpool/controller.go` | `createDeviceCommand` 登记归属时，按实际宿主 address 推导目标池 |
| `internal/warmpool/controller_pool_attribution_test.go` | 回归：溢出时归属跟随宿主 |
| `docs/evidence/pool_host_relationship_20261013.md` | 实测数据与三次诊断修正记录 |

## 相关

- `docs/04_mvp_functional_spec.md:152`：设备池是"预约和调度使用的逻辑分组"
- `docs/adr/0006_fixed_min_ready_warm_pool.md:21`：创建时同步建立 Pool membership
  （本 ADR 明确 membership 应指向**实际宿主**对应的池）
- `docs/evidence/pool_host_relationship_20261013.md`：现场数据与诊断修正史
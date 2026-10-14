# ADR-0043：预约由宿主实时容量分配，满载原地排队

## 状态

已确定（2026-10-13）。**部分取代 ADR-0042。**

## 背景

需求方在 2026-10-13 澄清了预约与分配的关系，原话：

> 「alcor 给到农场一个预约的请求，农场根据这个请求自动的找空闲的机器，农场感觉 55 是空闲的，
> 就给了 55 这个池，这个池就应该接收这个预约，自动的创建虚拟机。当有很多个预约的时候，
> 已经所有的池已经全部占满了内存，无法再增加虚拟机了，这时候就等待看那个虚拟机运行结束，
> 再往这个池里面分配」

> 「当农场发现全部没有空闲的时候就是在原地等待 30 分钟，有空闲农场就分配，
> 30 分钟还没有分配就报错」

由此确定的语义是**分配**，不是**归属**：

| 环节 | 应有行为 |
|---|---|
| 挑机器 | 农场按各宿主**实时空闲内存**自己选，调用方不指定 |
| 定池 | **挑中谁，就用谁对应的池**接收并创建；池不是独立输入 |
| 全满 | **原地排队**（保持 pending，不建机、不溢出、不乱建），每轮重新检查 |
| 有空闲 | **立刻分配**给那台空闲宿主 |
| 排队上限 | **30 分钟**；超时报错「等不到虚拟机，需要检查」 |

### 现状与目标的差距

代码已经具备大部分能力，但被 `pool_id` 从中间切断了：

| 能力 | 现状 |
|---|---|
| 按内存挑宿主 | ✅ 已有（`warmpool.lockHostCapacity`，`capacity.Evaluate`） |
| 内存不足不建机、不溢出 | ✅ 已有（`controller.go:1383-1385`，`ErrNoCapacity` → `CapacityMisses++` + `break`） |
| 每轮重扫、一有空闲就分配 | ✅ 已有（`scheduler` 每 250ms 重扫，`LockNextAllocatablePending` 严格 FIFO） |
| **全局排队** | ❌ **队列被 `pool_id` 切成每池一条** |

`device_reservations.pool_id` 在**选池阶段**就被固定，而选池（`pool_selector.go`）
**完全不看宿主内存**（全文只引用 `h.status`/`h.draining`/`h.host_type`，从不读 `capacity`）。
于是：

- 预约落在池 54 → 只有池 54 被推动扩容（`controller.go:1342-1348` 的 `pendingDemand` 按 `pool_id` 统计）；
- 54 内存满 → `CapacityMisses` → 池 54 这条队列干等；
- 55 空闲 → **没有预约指向它，没人推动它扩容**；
- 结果：**55 空转，预约在等 54** —— 与「哪台空闲给哪台」完全相反；
- 而且分配闸门的钥匙是 `pd.pool_id = r.pool_id`（`reservation.go:228`），
  即使别的池建出了设备也匹配不上，最终 `PENDING_TIMEOUT`。

### 为什么"池"必须退化为机器的别名

这不是概念洁癖。**只要 `pool_id` 是队列的分区键，全局空闲就看不见。**
宿主机与池一对一（ADR-0042 已实测：4 池/4 宿主互相覆盖 4/4，且两表之间
无关联列，唯一连接是池名 `android-<宿主IP>`），因此池在分配流程中
**不承载任何独立信息**，只应是"选中哪台机器"的结果属性。

## 决策

1. **分配不再受 `pool_id` 约束。** 调用方传入的 `pool_id`（含显式指定）**不限制分配宿主**，
   仅保留用于追溯/展示。省略时由农场按宿主实时空闲内存自选 —— 这与 Alcor 侧既有契约一致
   （`android_handler.go:601-611`：「已经把选池完全交给农场…`device_pool_id` 留空交给农场」）。

2. **队列是全局一条，不是每池一条。** 去掉三处 `pool_id` 硬过滤：
   - `repository/reservation.go:228`（`LockNextAllocatablePending` 的 `EXISTS` 闸门）
   - `repository/reservation.go:921`（`LockMatchingDevice` 的设备筛选）
   - `warmpool/controller.go:1346`（`pendingDemand` 的池级统计）

   匹配条件改为**平台 + 请求能力 + 设备可用性**，与池无关。

3. **内存是唯一的真闸门，`max_concurrency` 退为运维安全上限。**
   `capacity.Evaluate`（预留 1 CPU / 2048MB / 4096MB 磁盘后按实时可用内存计算）
   决定某台宿主还能开几台。`max_concurrency=8` 之类的值不再参与"能不能开"的判断。
   一键接入脚本本来就把它当安全网（`scripts/install-device-host.sh:366` 注释：
   「此时 max_concurrency 已是安全网值」），本决策与之一致。

   > 这**取代 ADR-0011 第 15 行**中「按所有启用 Image 目标之和同步 Pool `max_concurrency`」
   > 作为容量判据的地位：Image 目标仍可驱动扩容意图，但**不得**作为"宿主还能开几台"的依据。

4. **满载时原地排队，上限 30 分钟。**
   `lease.pending_timeout` 由 10 分钟改为 **30 分钟**；超时失败码 `PENDING_TIMEOUT`，
   文案为「**等不到虚拟机，需要检查**」（提示运维介入，而不是静默失败）。
   排队期间**不创建任何虚拟机**（现有 `CapacityMisses` + `break` 行为即所需，不改）。

5. **保留 ADR-0042 第 2 条：归属跟随实际宿主。**
   建机登记 `device_pool_devices` 时仍按所选宿主的 `address` 推导池
   （`resolvePoolForHost`）。在新语义下这条更关键：池本来就是由机器决定的。

6. **部分取代 ADR-0042 第 1 条。** ADR-0042 决策 1「允许溢出到其他空闲宿主」
   表述为"溢出"，隐含"先定了池、池装不下才溢出"的旧模型。新语义下不存在"溢出"，
   只有"选中了哪台机器"。**该条的实质（按内存挑宿主、不按池名收窄）保留**，
   措辞作废。

## 后果

- **正面**：所有宿主的内存对全部排队预约可见，"哪台空闲给哪台"成立；
  不再出现"55 空转却在等 54"；排队行为与 `docs/04:162`（容量不足保持 pending、不超建）
  和 `docs/05:152`（`waiting_capacity` 不误标失败、容量恢复后继续）既有文档一致。
- **正面**：`pool_id` 从分配链路淡出后，"池=宿主机"在代码与概念上一致。
- **负面（已接受）**：显式点名某个池**不再保证**在该池宿主上创建。这是需求方
  明确选择的（问「显式指定时尊重还是无视」→ 答 **B：无视**）。
  Alcor 侧唯一依赖此行为的测试 `adapters/alcor/client_test.go:107`
  （`TestClientReserveWithPoolIDKeepsCallerChoice`）需同步改。
- **负面（已接受）**：排队可能长达 30 分钟。调用方 `WaitActive` 的 ctx deadline
  需 ≥ 30 分钟，否则客户端会先报 `DEVICE_CAPACITY_UNAVAILABLE`（可重试）。
- **风险**：`pool_selector.go:264-265` 条件 B 只检查"存在 online Android 宿主能拉起镜像"，
  **未限定该宿主是否为本池宿主**。一对一部署下结论碰巧正确，但增设第二台同镜像宿主
  后会误判池可选。本次一并修正。

## 边界

- **不新增表、字段、API、状态。** `waiting_capacity` 若需要落地，复用既有技术状态语义
  （`docs/05:152`），不新增业务状态。
- **不改 `device_reservations.pool_id` 列**（仍记录预约时落地的池，供追溯）。
- **不改建机挑宿主算法**（`lockHostCapacity` 的排序与容量判据保持原样）。
- **不改 `resolvePoolForHost`**（ADR-0042 决策 2 保留）。
- 不改 STF / Appium / DaFit 任何边界能力。

## 相关

- `docs/adr/0042_device_pool_attribution_follows_host.md`：部分被本 ADR 取代（决策 1 措辞）
- `docs/adr/0014_dynamic_host_capacity_and_device_runtime_profile.md`：内存驱动容量（本 ADR 的直接依据）
- `docs/adr/0011_console_managed_fixed_capacity.md:15`：`max_concurrency` 派生规则（第 3 条取代其容量判据地位）
- `docs/04_mvp_functional_spec.md:152,162,190,197-204`：池定义、排队、预约字段、Scheduler 步骤
- `docs/05_step_by_step_implementation.md:152,182`：`waiting_capacity` 与 FIFO 领取
- `docs/02_architecture_alignment.md:99`、`docs/03_alcor_generation_alignment.md:41`：需同步更新
- `internal/capacity/capacity.go`：`Evaluate`
- `internal/warmpool/controller.go`：`lockHostCapacity`、`reconcile`、`resolvePoolForHost`
- `internal/reservation/pool_selector.go`、`internal/scheduler/scheduler.go`、`internal/repository/reservation.go`
# ADR-0041：池与宿主机的调度归属（修跨池串台）

## 状态

已批准。

## 背景

设备农场已有多台 Android 宿主机（171、54、55）和三台独立的 Android 池
（`android-10.0.30.171`、`android-10.0.30.54`、`android-10.0.30.55`），
每个池的运维语义是「这台服务器上的模拟器」。

### 问题：预约指定 A 池，设备却建到 B 宿主

2026-10-13 实测在正式服**双向复现**：

| 预约所属池 | 设备实际所在宿主 | 次数 |
|---|---|---|
| `android-10.0.30.54` | 安卓宿主机 - 10.0.30.55 | 4 |
| `android-10.0.30.55` | 安卓宿主机 - 10.0.30.54 | 1 |

### 根因

`internal/warmpool/controller.go` 的 `lockHostCapacity` 查询候选宿主时：

```sql
FROM device_hosts h
WHERE h.status='online' AND NOT h.draining
  AND h.host_type IN ('docker_emulator','hybrid')
ORDER BY h.id FOR UPDATE OF h SKIP LOCKED
```

**完全不含池的限定**。任何在线安卓宿主都是任何池的候选，于是「先在 id 小的宿主上
塞满」成为实际行为，池名里的宿主语义被忽略。

### 为什么这是真问题

1. **容量判断失真**：用户按「每台服务器的剩余内存」规划并发，串台后实际用的是另一台的内存；
2. **运维预期落空**：以为任务跑在 54 上（可去看 54 的日志/负载），实际在 55；
3. **镜像状态被污染**：`device_host_image_states` 是按 host 记录的，串台会让「这台宿主有/没有该镜像」
   的观测与池的预期错位，进而影响 `IMAGE_DIGEST_MISMATCH` 判定与排障；
4. 与既有 `pool_selector.go` 的**选池**侧不一致：选池时已按能力/剥离镜像状态做过滤，
   但真到建机又随机落宿主，形成两层语义不一致。

### 约束（为什么不能简单加表）

- `docs/01_existing_capability_reuse_matrix.md` 要求宿主机接入**不新增表/字段/协议**；
- `docs/02_architecture_alignment.md` 的「多宿主机接入与登记」明确「**不新增表**」；
- `docs/00_scope_and_boundaries.md` 坚持设备域单一职责，不引入第二套拓扑模型。

## 决策

1. **从池名推导目标宿主地址，调度时按该地址收窄候选宿主。**

   池名（如 `android-10.0.30.54`、`iOS - 10.0.33.68`）是运维可读的既有约定，里面本就
   带着宿主地址；把它作为**调度线索**使用，不需要新增表、字段或 API。

   新增 `poolhost.AddressFromPoolName(name) string`（原为 `warmpool.PoolHostAddressFromName`，
   评审后抽到共享包，见第 5 条）：抽取池名中出现的**完整 IPv4**
   （四段、每段 ≤255、边界严格），抽不到返回空串。

2. **收窄通过 `device_hosts.address` 精确匹配，仅在推导出地址时生效。**

   `lockHostCapacity` 新增 `poolHostAddress` 参数：非空时附 `AND h.address=$2`，
   为空时保持原语义（全农场任选）。

3. **`validate_image` 路径**已合并为第 6 条。

4. **推导不出地址、或地址在农场里不存在时，一律回退到既有行为。**

   `default-android` 这类无地址池、以及全部既有测试夹具，继续走「全农场任选」。

   第二条尤其重要：池名里的地址可能**匹配不到任何宿主**（改名、写错 IP、那台宿主已被删）。
   此时若坚持收窄，该池会被**永久饿死** —— 永远 CapacityMiss、永远建不出设备，
   比串台更难排查。所以收窄前先确认该地址确实存在于 `device_hosts.address`。

   注意判定要**不看 `status`**：目标宿主只是临时离线时，它仍然是该池所属的宿主，
   池必须排队等它回来，而不是回退到别的宿主。

5. **选池（`internal/reservation`）必须用同一条归属规则，否则会出现"假通过"。**

   选池判「这个池现在能不能服务」时，原先用**任意**在线安卓宿主去验证
   「镜像可用 + 宿主在线」。收窄建机后，若选池不收窄，就会出现：
   选池说「能服务」（因为 55 上有镜像），建机却只认 54（54 上没镜像）→
   请求被选中后一直排队，表现为假通过。

   因此派生 `internal/poolhost` 作为这条规则的**唯一定义**，
   建机侧与选池侧共用，避免两处实现漂移（见规则 8：不得形成第二套兼容实现）。

6. **`validate_image` 路径刻意不按池收窄。**

   一个镜像可被多个池共用，且校验失败恰恰发生在「目标宿主上还没有该镜像」时。
   若按池限定，池与镜像同时待建时会互相等待成死锁。校验只关心
   「农场里有哪台宿主能证明这个镜像可用」。

7. **目标宿主离线/不可用时排队等待，不改落到其他宿主。**

   宁可保持 `CapacityUnavailableError`（预约 pending），也不产生"以为在 54、
   实际在 55"的静默错位。

## 边界

- 不新增 API、表、字段、状态或模块；不改 Console。
- 不改 `device_hosts.address` 的含义（仍是登记时的宿主地址）。
- 不改 iOS 路径（`reconcileIOSScaleDown` 走独立的 `base_device_id` 归属，本决策不涉及）。
- 不改 `capacity.Evaluate` 的算法与阈值。
- 不改选池的**可选性语义**（哪些池可服务、并发上限、权重轮转都不变）；
  只把其中的「宿主前提」判据收窄到该池自己的宿主。
- 池名仍是自由文本；本决策**不强制**命名规范，只是「名字里有地址时按其调度」。

## 后果

- 「预约指定哪台就只在那台建」，与池名的运维语义一致。
- 各池的容量判断回到「该服务器自己的内存」，与用户的并发规划一致。
- 选池与建机不再自相矛盾：不会出现「选池说能服务、建机却建不出来」的假通过。
- 依赖命名约定：若把池改名成不含 IP 的名字、或改成农场里不存在的 IP，
  归属会退回「全农场任选」。这是可接受的 —— 改名本身就是显式的运维动作，
  且不会把池饿死。
- 既有测试需要覆盖新语义：共 **6 个集成用例 + 4 个纯逻辑用例**。

## 评审中发现并修掉的两个问题（2026-10-13 第二轮）

初版只改了建机侧，复查时发现两个必须修的问题：

1. **池被永久饿死**（初版引入）。池名里的地址若在农场里不存在，
   坚持收窄会让该池永远 `CapacityMiss`。已加「地址存在性」前置判定。
   由 `TestWarmPoolFallsBackWhenPoolAddressMatchesNoHost` 锁定。
2. **选池与建机不一致导致假通过**（初版遗漏）。选池仍按任意在线宿主判定，
   会选中一个建不出来的池。已派生 `internal/poolhost` 让两侧共用同一条规则。

另有一处实现细节：`device_pools.id` 是 `character varying` 而**不是** `uuid`，
选池侧的 CTE 起初按 `uuid` 声明，导致 21 个既有用例报
`operator does not exist: uuid = character varying`。已改为 `text`。

## 落地实现（2026-10-13）

| 文件 | 作用 |
|---|---|
| `internal/poolhost/poolhost.go` | 归属规则的**唯一定义**：`AddressFromPoolName` / `ResolveScope`，建机与选池共用 |
| `internal/poolhost/poolhost_test.go` | 18 个解析用例 + 归属生效/回退/无探针 3 个逻辑用例 |
| `internal/warmpool/controller.go` | `lockHostCapacity` 增加 `poolHostAddress` 参数并按 `h.address` 收窄；新增「地址存在才收窄」判定；`reconcile`/`Provision` 读出池名传入 |
| `internal/warmpool/controller_pool_host_scope_test.go` | 6 个集成回归（收窄、无地址回退、地址不存在回退、离线排队、目标宿主缺镜像时不串台） |
| `internal/reservation/pool_selector.go` | 选池的 Android 支路按同一归属收窄宿主前提 |
| `docs/evidence/cross_pool_crosstalk_20261013.md` | 实测复现、根因、修复前后对比 |
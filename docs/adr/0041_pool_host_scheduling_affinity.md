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

   新增 `warmpool.PoolHostAddressFromName(name) string`：抽取池名中出现的**完整 IPv4**
   （四段、每段 ≤255、边界严格），抽不到返回空串。

2. **收窄通过 `device_hosts.address` 精确匹配，仅在推导出地址时生效。**

   `lockHostCapacity` 新增 `poolHostAddress` 参数：非空时附 `AND h.address=$2`，
   为空时保持原语义（全农场任选）。

3. **推导不出地址时回退到既有行为，不破坏既有池。**

   `default-android` 这类无地址池、以及全部既有测试夹具，继续走「全农场任选」。
   这是刻意的兼容选择：本决策只修「池名已经表达了归属、调度却没遵守」这一情形。

4. **`validate_image` 路径刻意不按池收窄。**

   一个镜像可被多个池共用，且校验失败恰恰发生在「目标宿主上还没有该镜像」时。
   若按池限定，池与镜像同时待建时会互相等待成死锁。校验只关心
   「农场里有哪台宿主能证明这个镜像可用」。

5. **目标宿主离线/不可用时排队等待，不改落到其他宿主。**

   宁可保持 `CapacityUnavailableError`（预约 pending），也不产生"以为在 54、
   实际在 55"的静默错位。

## 边界

- 不新增 API、表、字段、状态或模块；不改 Console。
- 不改 `device_hosts.address` 的含义（仍是登记时的宿主地址）。
- 不改 iOS 路径（`reconcileIOSScaleDown` 走独立的 `base_device_id` 归属，本决策不涉及）。
- 不改 `capacity.Evaluate` 的算法与阈值。
- 不改选池（`pool_selector.go`）的可选性语义。
- 池名仍是自由文本；本决策**不强制**命名规范，只是「名字里有地址时按其调度」。

## 后果

- 「预约指定哪台就只在那台建」，与池名的运维语义一致。
- 各池的容量判断回到「该服务器自己的内存」，与用户的并发规划一致。
- 依赖命名约定：若把池改名成不含 IP 的名字，归属会退回「全农场任选」。
  这是可接受的，因为改名本身就是显式的运维动作。
- 既有测试需要覆盖新语义：新增三个集成用例
  （收窄生效、无地址回退、目标宿主离线时排队）。

## 落地实现（2026-10-13）

| 文件 | 作用 |
|---|---|
| `internal/warmpool/controller.go` | 新增 `PoolHostAddressFromName`；`lockHostCapacity` 增加 `poolHostAddress` 参数并按 `h.address` 收窄；`reconcile`/`Provision` 读出池名传入 |
| `internal/warmpool/controller_pool_host_scope_test.go` | 三个集成回归（收窄、回退、离线排队）+ 一个纯解析单测 |
| `docs/evidence/cross_pool_crosstalk_20261013.md` | 实测复现、根因、修复前后对比 |
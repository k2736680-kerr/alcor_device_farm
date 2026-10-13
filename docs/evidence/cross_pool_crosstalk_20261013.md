# 跨池串台修复（2026-10-13）

## 1. 问题

预约指定某个 Android 池（如 `android-10.0.30.54`），设备却建到**另一台宿主机**（55）上。

### 1.1 实测复现（修复前，正式服）

```sql
select p.name as pool, h.name as actual_host, count(*) as n
from device_reservations r
join device_pools p on p.id=r.pool_id
left join devices d on d.id=r.device_id
left join device_hosts h on h.id=d.host_id
where r.created_at > now() - interval '6 hours'
group by p.name, h.name order by p.name, h.name
```

| 预约所属池 | 设备实际所在宿主 | 次数 |
|---|---|---|
| `android-10.0.30.171` | 安卓宿主机 - 10.0.30.171 | 5 |
| `android-10.0.30.54` | 安卓宿主机 - 10.0.30.54 | 3 |
| **`android-10.0.30.54`** | **安卓宿主机 - 10.0.30.55** | **4** |
| **`android-10.0.30.55`** | **安卓宿主机 - 10.0.30.54** | **1** |
| `android-10.0.30.55` | 安卓宿主机 - 10.0.30.55 | 2 |

**双向串台**，池名里的宿主与实际宿主不一致。

## 2. 根因

`internal/warmpool/controller.go` 的 `lockHostCapacity` 选候选宿主时：

```sql
FROM device_hosts h
WHERE h.status='online' AND NOT h.draining
  AND h.host_type IN ('docker_emulator','hybrid')
ORDER BY h.id FOR UPDATE OF h SKIP LOCKED
```

**完全不含池的限定**。任何在线安卓宿主都是任何池的候选，实际行为退化为
「按 host id 从小到大塞满」，池名表达的归属被忽略。

## 3. 修复

### 3.1 方案：从池名推导目标宿主地址

池名（`android-10.0.30.54`、`iOS - 10.0.33.68`）是**既有的**运维可读约定，里面本就带着宿主地址。
把它作为调度线索，**不需要新增表、字段或 API**（满足复用矩阵「不新增表/字段/协议」的约束）。

```go
func PoolHostAddressFromName(name string) string
```

只抽取池名中出现的**完整 IPv4**：四段、每段 ≤255、左右边界严格。抽不到返回空串。

### 3.2 收窄方式

`lockHostCapacity` 新增 `poolHostAddress` 参数：非空时附加 `AND h.address=$2`，
用 `device_hosts.address` 精确匹配；为空时保持原语义。

### 3.3 调用点

| 调用点 | 是否按池收窄 | 理由 |
|---|---|---|
| `reconcile`（按需建机） | ✅ 传入池名推导的地址 | 这就是串台发生的地方 |
| `Provision`（Console 受控创建） | ✅ 传入池名推导的地址 | 同样必须落在目标宿主 |
| `reconcileImageValidation`（镜像校验） | ❌ 刻意不传 | 见 §3.4 |

### 3.4 为什么镜像校验刻意不收窄

一个镜像可被多个池共用，且校验失败恰恰发生在「目标宿主上还没有该镜像」时。
若按池限定，池与镜像同时待建时会互相等待成**死锁**。校验只关心
「农场里有哪台宿主能证明这个镜像可用」。这是刻意的、有据的选择。

## 4. 验收证据

### 4.0 生产环境端到端验证（部署后实测）

修复以新镜像 `alcor-device-farm:crosstalk-fix-20261013` 部署到控制面 `10.0.80.220`
（`Up (healthy)`），然后在**真实生产环境**做双向验证。

**前提条件**：验证开始时 54 与 55 上**活跃模拟器均为 0**，两台都空闲 ——
这正是修复前最容易串台的场景。

| 步骤 | 池 | 预约结果 | 实际落点 | 判定 |
|---|---|---|---|---|
| 1 | `android-10.0.30.54` | HTTP 201 | 设备 `68b40e17…` → **10.0.30.54** | ✅ 一致 |
| 2 | `android-10.0.30.55` | HTTP 201 | 设备 `1a13a1d9…` → **10.0.30.55** | ✅ 一致 |

第 2 步是**更难的情形**：此时 54 上已有一台设备，若按修复前「按 host id 塞」的逻辑
很可能又落到 54；实测仍精确落在 55。

按 `device_pool_devices` 关联池名与实际宿主交叉核对：

```
device_id                            | pool_name           | actual_host | verdict
1a13a1d9-11e6-488f-9250-6558b2ce4db1 | android-10.0.30.55  | 10.0.30.55  | OK 一致
68b40e17-df1e-4807-a427-4f10f89296f5 | android-10.0.30.54  | 10.0.30.54  | OK 一致
```

近 30 分钟全量（含已删除）串台统计：**0 条串台**。

**显存对比（修复前同一库）**：`android-10.0.30.54` 池曾 4 次建到 55、
`android-10.0.30.55` 池曾 1 次建到 54。

**回收验证**：释放预约后，54/55 上的模拟器在 **~48s 内全部清空**（0 台），
证明按需清理（ADR-0038）与本次收窄调度**没有冲突**。

### 4.1 单元/集成测试（真实 PostgreSQL）

```
=== RUN   TestWarmPoolScopesCreationToPoolHostAddress
--- PASS: TestWarmPoolScopesCreationToPoolHostAddress (0.17s)
=== RUN   TestWarmPoolFallsBackWhenPoolNameHasNoAddress
--- PASS: TestWarmPoolFallsBackWhenPoolNameHasNoAddress (0.10s)
=== RUN   TestWarmPoolQueuesInsteadOfUsingAnotherHostWhenScopedHostOffline
--- PASS: TestWarmPoolQueuesInsteadOfUsingAnotherHostWhenScopedHostOffline (0.10s)
=== RUN   TestPoolHostAddressFromNameParsing
--- PASS: TestPoolHostAddressFromNameParsing (0.00s)
PASS
ok  	github.com/Ad-Quanta/alcor-device-farm/internal/warmpool	0.381s
```

### 4.2 用例设计要点

**`TestWarmPoolScopesCreationToPoolHostAddress`** —— 关键在**宿主插入顺序**：
刻意让「错的那台」（`host...002` = 10.0.30.55）id 排在「对的那台」（`host...003` = 10.0.30.54）
之前。修复前按 `ORDER BY h.id` 必然选中 002；断言必须等于 003：

```go
if hostID != "host_000000000000003" {
    t.Fatalf("池 android-10.0.30.54 的建机必须落在 10.0.30.54 上，实际 host_id=%s（串台未修复）", hostID)
}
```

**`TestWarmPoolQueuesInsteadOfUsingAnotherHostWhenScopedHostOffline`** —— 目标宿主
离线时**不得就近落到别的宿主**，必须保持 `DevicesCreated=0` 并排队。这是串台危害最大的形态：
以为在 54 上跑，其实跑在 55。

**`TestWarmPoolFallsBackWhenPoolNameHasNoAddress`** —— 兼容性保证：无地址线索的池
（如既有 `default-android`、全部测试夹具）行为与修复前完全一致。

**`TestPoolHostAddressFromNameParsing`** —— 11 个解析用例，覆盖易错点：

| 输入 | 期望 | 说明 |
|---|---|---|
| `android-10.0.30.54` | `10.0.30.54` | 正常 |
| `iOS - 10.0.33.68` | `10.0.33.68` | 空格分隔 |
| `android-16-api36` | ``（空） | **版本号不能被当地址** |
| `default-android` | ``（空） | 无地址 → 回退 |
| `pool-1.2.3.4.5` | ``（空） | 段数过多 |
| `pool-999.1.1.1` | ``（空） | 段值 >255 |
| `android-10.0.30.540` | ``（空） | 段值 >255 |
| `android-10.0.30.5.4` | ``（空） | 五段 |
| `android-10.0.30.54-extra` | `10.0.30.54` | 后缀不影响 |

## 5. 修复前后对比

| 场景 | 修复前 | 修复后 |
|---|---|---|
| 预约 `android-10.0.30.54` 池，54/55 都有空余 | 可能建到 55（按 id 顺序） | **必定建在 54** |
| 池名无地址（`default-android`） | 全农场任选 | 全农场任选（不变） |
| 54 离线、55 在线 | 静默建到 55 | **排队等 54 恢复** |

## 6. 影响面

- 用户按「每台服务器实际内存」规划并发的前提恢复成立：容量判断回到该服务器自己的内存。
- `device_host_image_states`（按 host 记录）的观测不再与池的预期错位，排障更可靠。
- **运维注意**：池名现在是**有语义的**。把池改名成不含 IP 的名字会让归属退回「全农场任选」。
  这是显式运维动作，可接受，但需知晓。

## 7. 相关文档

- ADR-0041：`docs/adr/0041_pool_host_scheduling_affinity.md`
- 对齐表：`docs/02_architecture_alignment.md`（池与宿主机的调度归属）、
  `docs/03_alcor_generation_alignment.md`（第 40 行后新增一条）
- 并发策略：`docs/evidence/concurrency_policy_20261013.md`
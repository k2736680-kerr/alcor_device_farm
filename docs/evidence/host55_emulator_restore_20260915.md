# 10.0.30.55 宿主机模拟器恢复与 STF 可见性闭环（2026-09-15）

## 一句话结论

55 宿主机「模拟器不启动 / 无法远程连接」的根因**不在 55 本身**，而是
**控制面把 55 的设备池目标台数暂停成了 0**（`total_target=0 / min_ready=0`），
调度器因此**根本不会下发创建命令**。恢复池目标后，配合把 171 侧同步定时器间隔
从 2 分钟收紧到 20 秒，整条链路在 **3 分钟内自动跑通**：

> 设备创建 → 上报 ready → 汇入 171 的 STF → 控制面标记 `healthy`

**零自愈重启、零 `stf_not_visible` 事件、对 171 自有设备零影响。**

---

## 一、症状

| 观察 | 数据 |
|---|---|
| 55 上有容器吗 | `docker ps -a` **为空**（连停止的都没有） |
| Agent 在跑吗 | 在跑：`alcor-device-host-agent.service` `active`，已 15 小时 |
| Agent 有日志吗 | 15 小时内**只有 1 条** ERROR（`STALE_COMMAND_LEASE`），此后完全静默 |
| Agent 有心跳吗 | 有且新鲜：`last_heartbeat_at = 2026-09-15 01:27:45Z` |
| Agent 健康吗 | `status=online`，`capabilities` 含 `kvm/gpu_render/provider_inventory_complete` 全 true |

**看起来一切正常，就是不干活。** 这指向「不是 Agent 的问题，而是没人给它派活」。

---

## 二、根因（两层）

### 2.1 直接根因：55 的设备池被主动暂停了

`device_pools` 表：

| 池 | total_target | min_ready | status |
|---|---|---|---|
| android-10.0.30.171 | 1 | 1 | active |
| **android-10.0.30.55** | **0** | **0** | active |
| iOS - 10.0.33.68 | 2 | 2 | active |

审计记录把因果写得很清楚：

```
action        : update_device_pool_capacity
resource_id   : c8e22592-7356-49ac-b7f2-43bf64525445   (55 池)
reason        : 暂停 55 池自动扩容：STF 可见性未打通，避免容器空转重建
summary       : {"min_ready": 0, "total_target": 0, "max_concurrency": 1, ...}
created_at    : 2026-09-14 10:58:58+00
```

调度侧判定依据在 `internal/reconcile/service.go:409`：

```sql
WHERE pd.device_id=d.id AND pd.enabled AND p.platform='android'
      AND p.status='active' AND p.total_target>0
```

→ `total_target=0` 时该池的设备**不被视为活跃成员**，自然不会被调度。

### 2.2 当初为什么会被暂停：STF 可见性未打通

2026-09-14 的完整回放（来自 `device_host_commands` + `device_audit_events`）：

| 时刻 | 事件 |
|---|---|
| 10:28:04 | `create` succeeded（设备 `ba49926c`，`10.0.30.55:32797`） |
| 10:30:29 ~ 10:57:27 | `restart` **×12 全部 succeeded**，每次 `previous_reason = "device is not visible through STF"` |
| 10:58:58 | 暂停 55 池扩容（`total_target=0`） |
| 10:59:34 | `delete` succeeded + `scale_down_device` |

**为什么反复重启也没用** —— `reconcile/service.go` 的 `withinVisibilityGrace`：

* 宽限期 = `DEVICE_FARM_RECONCILE_STF_VISIBILITY_GRACE`，默认 **30 秒**；
* 锚点 = **最近一次成功 `create`/`rebuild` 命令的完成时刻**；
* **`restart` 不重置这个锚点**。

于是只要设备在宽限期内没进 STF，`reconcile` 就开始累计 `stf_not_visible` 失败并触发自愈重启；
重启不重置锚点 ⇒ **「反复重启但永远不可见」的死循环**。当时 171 侧的同步定时器尚未上线，
设备**无论如何都不会可见**，所以重启 12 次也无济于事。

### 2.3 本轮额外发现的自身缺陷：定时器间隔超窗

本仓库新写的同步定时器最初是 `OnUnitActiveSec=2min`，注释里还误称「远低于 30s 宽限期」——
**实际是远高于**。2 分钟 > 30 秒宽限期，等于**重新引入上面那个死循环**。
这是必须与池恢复一起修掉的第二层问题。

---

## 三、修复动作

| # | 动作 | 位置 | 可回滚方式 |
|---|---|---|---|
| 1 | 同步定时器 `2min → 20s`，`AccuracySec 15s → 1s` | 171 `/etc/systemd/system/alcor-device-farm-stf-sync.timer`（仓库同步改 `deploy/stf/alcor-device-farm-stf-sync.timer`） | 还原 `.bak-20260915012946` |
| 2 | 55 池 `total_target=0→1`，`min_ready=0→1` | 控制面 `PUT /api/v1/device-pools/c8e22592-…` | PUT 回 `0/0` |

动作 2 的 API 调用（经控制面服务 Token 鉴权，走正规接口而非直改数据库）：

```sh
TOKEN=$(grep -m1 '^DEVICE_FARM_SECURITY_SERVICE_TOKEN=' /data/stacks/alcor-device-farm/server.env | cut -d= -f2-)
curl -sS -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"android-10.0.30.55","platform":"android","default_lease_seconds":1800,
       "max_lease_seconds":7200,"max_concurrency":1,"total_target":1,"min_ready":1,
       "default_image_id":"109cf3d6-ef57-485a-b924-6ecf8369200b",
       "reason":"恢复 55 池自动扩容：STF 可见性链路已打通"}' \
  https://10.0.80.220:18180/api/v1/device-pools/c8e22592-7356-49ac-b7f2-43bf64525445
```

> 注：`PUT` 走的是 `internal/management/postgres/store.go` 的 `UpdatePool`（**全量替换**），
> 并在同一事务内写入 `device_audit_events`。审计留痕已确认生成。

---

## 四、验证证据

### 4.1 端到端时间线（恢复池目标后自动完成，无人工干预）

| 时刻 (UTC) | 观察 |
|---|---|
| 01:30:34 | `create` 命令下发（`leased`） |
| 01:31:06 | 55 容器已起：`10.0.30.55:32798->4723`、`10.0.30.55:32799->5555`；设备 `provisioning` |
| 01:31:56 | `stf_stabilizing`（宽限期保护，预期行为） |
| 01:32:06 | 设备 `ready` |
| 01:32:24 | **`health_recovered` — "STF visibility recovered"** |
| 01:32:37 | 设备 `ready + healthy`；STF 设备表出现 `10.0.30.55:32799` |
| 01:33:37 | `create` 最终 `succeeded`，**全程零 restart** |

### 4.2 同步定时器（加速后）真的在周期工作

```
Sep 15 01:33:13  sync done: connected=1 skipped_local=1 failed=0
Sep 15 01:33:34  sync done: connected=1 skipped_local=1 failed=0
Sep 15 01:33:55  sync done: connected=1 skipped_local=1 failed=0
```

`connected=1`（连上了 55 的远程设备）+ `skipped_local=1`（正确跳过 171 本机设备）
⇒ **ADR-0037 决策 2 的两个分支都被真实执行**。

### 4.3 STF 侧：两台设备均在线可用

```
present_count= 2
   10.0.30.171:32821  present=True  ready=True  using=False
   10.0.30.55:32799   present=True  ready=True  using=False
```

### 4.4 决定性验证：从 171 真的能操作 55 上的设备

```
$ adb -s 10.0.30.55:32799 get-state                → device
$ adb -s 10.0.30.55:32799 shell getprop ro.product.model      → sdk_gphone64_x86_64
$ adb -s 10.0.30.55:32799 shell getprop ro.build.version.sdk  → 35
$ adb -s 10.0.30.55:32799 shell wm size                       → Physical size: 1080x2400
$ adb -s 10.0.30.55:32799 shell pm list packages              → 正常列举
$ curl http://10.0.30.55:32798/status
  → {"value":{"ready":true,"message":"The server is ready to accept new connections",
              "build":{"version":"3.5.2"}}}
```

**「无法远程连接」已解决。**

### 4.5 零回归

* 171 自有设备：`Android 15-1 | ready | healthy | consecutive_failures=0`
* 最近健康事件中**没有 `stf_not_visible`**，只有预期的 `stf_stabilizing` → `health_recovered`
* 三个池均为 `active`：171 `1/1`、55 `1/1`、iOS `2/2`

---

## 五、备忘：这一轮踩到的三个坑

1. **「Agent 在跑但没容器」先查池目标，别急着怀疑 Agent。**
   `device_pools.total_target=0` 会让调度器静默不下发命令，Agent 侧表现完全「正常」。
   排查入口：`device_audit_events` 里搜 `update_device_pool_capacity`。

2. **同步定时器间隔必须显著小于 reconcile 的 STF 宽限期（默认 30s）。**
   宽限期锚点是 `create`/`rebuild` 的完成时刻且 **`restart` 不重置锚点**，
   间隔超窗会直接导致「重启但永不可见」的死循环。同时 `AccuracySec` 默认 `1min`
   会把 20s 抖动到分钟级，必须收紧到 `1s`。

3. **Host Agent 的系统单元名里没有 `farm`。**
   是 `alcor-device-host-agent.service`，不是 `alcor-device-farm-host-agent.service`。
   查错名字会得出「它没装 systemd 单元、是裸进程」的错误结论（曾据此误判）。

---

## 六、遗留

1. **55 本地 registry `127.0.0.1:5001` 未运行**（`curl` 返回 000）。当前不影响建容器——
   镜像 `…15.0-api35-google_apis-x86_64-sdk9` 已在 55 本地（14.7 GB），`docker run` 不会 pull。
   但若将来需要 `--pull` 或换镜像，需先恢复该 registry。
2. **`TotalTarget=0` 属于「静默失效」配置**：控制面 UI/API 允许设置，但不会给出
   「该池因此永不扩容」的显式告警。是否增加校验或提醒，建议单独立项讨论。
3. 55 与 171 同属 `10.0.30.0/24`，本次依赖该网段直连。新增宿主机若跨网段，
   需先确认 171 到其设备端口范围（容器映射的高位端口）可达。

# STF 可用性实测：54 / 55 的模拟器是否都能被 STF 使用（2026-10-13）

## 1. 问题

设备农场内建 STF 功能。新增的 54、55 宿主机上的模拟器，是否**都能被 STF 正常使用**？
（不是"能创建"，而是"STF 里能看、能远控"。）

## 2. 机制（ADR-0037 已定义）

STF **全局只有一套**，位于 `10.0.30.171`。两个硬约束决定了接入方式：

| 约束 | 内容 |
|---|---|
| 约束一 | STF 不能部署到 220，也不能由服务端分发（220 服务端容器 `read_only`、无 Docker Socket） |
| 约束二 | STF ADB 注册**必须从 STF 所在机本地发起**（`internal/adapters/stfadb/client.go` 强制回环地址；171 的 `stf-adb` 为 `127.0.0.1:5038->5037`） |

因此**远程宿主机的设备无法自行注册到 171 的 STF**，只能由 **171 侧主动拉取**：

```
171 的 systemd timer (每 20s, AccuracySec=1s)
   └─▶ scripts/stf-sync-remote-emulators.sh
          ├─ GET http://10.0.80.220:18182/api/v1/devices   （拉设备清单，含 adb_endpoint）
          ├─ 按本机地址跳过 171 自己的设备（STF_LOCAL_HOSTS / hostname -I 自动探测）
          └─ 在 stf-adb 容器内逐个 `adb connect <adb_endpoint>`
```

**20 秒间隔是正确性约束，不是性能取舍**：`reconcile` 的 `STFVisibilityGrace` 默认 30s，且
`restart` 不重置锚点。若设备在宽限期内未被 `adb connect` 进 STF，会陷入
「反复重启但永远不可见」的死循环。20s 留约 10s 余量。

## 3. 实测步骤与证据

### 3.1 同步链在运行

```
systemctl is-active  alcor-device-farm-stf-sync.timer → active
systemctl is-enabled alcor-device-farm-stf-sync.timer → enabled
list-timers: NEXT 07:09:58 (149ms), LAST 07:09:38 (19s ago)   ← 20s 周期确认
```

### 3.2 设备确实被创建（54 与 55 各 1 台）

| 宿主 | serial | adb_endpoint | 最终状态 |
|---|---|---|---|
| 10.0.30.54 | `10.0.30.54:32777` | `10.0.30.54:32777` | busy / healthy |
| 10.0.30.55 | `10.0.30.55:32781` | `10.0.30.55:32781` | busy / healthy |

### 3.3 决定性证据一：STF 自己的 adb server 里两台都在

```
docker exec alcor-device-farm-stf-stf-adb-1 adb devices -l

List of devices attached
10.0.30.54:32777  device product:sdk_gphone64_x86_64 model:sdk_gphone64_x86_64 device:emu64xa transport_id:154495
10.0.30.55:32781  device product:sdk_gphone64_x86_64 model:sdk_gphone64_x86_64 device:emu64xa transport_id:154496
```

注意状态是 **`device`**（不是 `offline`/`unauthorized`），即真正可用。

### 3.4 决定性证据二：从 171 可实际操作这两台

```
adb -s 10.0.30.54:32777 shell getprop ro.build.version.sdk → 35
adb -s 10.0.30.55:32781 shell getprop ro.build.version.sdk → 35
```

即两台都是**可响应命令**的 Android 15 真机（模拟器），STF 远控所需的数据通道成立。

### 3.5 决定性证据三：控制面 reconcile 的权威判定

```
name        serial           host                    lifecycle  health   health_reason                  failures
Android设备 10.0.30.54:32777 安卓宿主机 - 10.0.30.54  busy       healthy  STF visibility recovered        0
Android设备 10.0.30.55:32781 安卓宿主机 - 10.0.30.55  busy       healthy  STF visibility recovered        0
```

`health_reason = **STF visibility recovered**` 是 `reconcile` 用 STF 的 `present && ready` 判定得出的，
且 `consecutive_failures = 0` —— 说明**没有发生 `stf_not_visible` 累计与自愈重启**。

## 4. 结论

**54 与 55 上的模拟器都可以被 STF 正常使用。** 三条独立证据互相印证：

1. STF adb server 内 `state=device`；
2. 从 171 可执行 `adb shell` 拿到真实系统属性；
3. 控制面 reconcile 标记 `STF visibility recovered` 且零失败。

**新宿主机接入后无需任何 STF 侧改动**：171 的 timer 会自动发现新设备的 `adb_endpoint` 并接入
（ADR-0037 决策 3）。宿主机侧不部署任何 STF 组件、不配置 `DEVICE_FARM_AGENT_STF_ADB_SERVER`。

## 5. 排障要点（本次实测澄清的易错点）

| 现象 | 原因 | 正确做法 |
|---|---|---|
| `curl 127.0.0.1:7100` → Connection refused（在 171 host 上） | STF 容器端口是 `10.0.30.171:7100->7100`，非回环 | 用 `10.0.30.171:7100` |
| `curl 127.0.0.1:18180` → refused（在 220 host 上） | 220 的 18180/18181/18182 绑定在 `10.0.80.220`，非回环 | 用 `10.0.80.220:18180` 或容器网络 `172.20.0.3:8080` |
| `stf-sync` 日志 `skipped_local=0` | 171 当时没有本机模拟器在跑，无设备可跳 | 正常；有本机设备时会 >0 |
| `stf-sync` 日志 `no android device with adb_endpoint found` | 控制面当时无 ready 的 Android 设备 | 正常（空闲时按需池就是空的） |
| STF `/api/v1/devices` 返回 `Requires Authentication` | STF API 需用户令牌，非 `--auth-secret` | 判定可见性改用 `adb devices` 或控制面 `health_reason` |

## 6. 遗留

- STF inventory 的历史幽灵记录（`--no-cleanup` 契约强制导致）需周期性清理，周期未定。见 ADR-0037「未纳入本 ADR 的相邻问题」。
- 本次验证的两台设备已释放，系统回到空闲（Android 活跃设备 0）。
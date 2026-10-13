# DF-071 宿主机一键接入脚本与双宿主分配验收

日期：2026-10-13
对应决策：ADR-0039、ADR-0031、ADR-0034、ADR-0035、ADR-0037、ADR-0038

## 一、结论

1. **两台 Android 宿主机都能被自动分配。** 并发投递两个「不指定池」的预约，
   农场按加权轮转分别选中 171 池与 55 池，**两台同时建机**，各自上报 `ready/healthy`
   并在 STF 中 `present=true/ready=true/using=true`；释放后两台各自被回收，
   **设备归零、容器消失、池回到 0**。
2. **一键接入脚本可用。** `scripts/onboard-device-host.sh` 在**完全清空**的 55 上一次执行
   即恢复 `online`（心跳 0.6 秒）；`--check-only`、幂等性、fail-fast 均已真实验证。

## 二、双宿主分配验收（真实环境）

### 2.1 投递方式

`POST /api/v1/device-reservations`，**不传 `pool_id`**（由农场自己选池）：

```json
{"owner_type":"manual","owner_id":"onboard55-verify-N",
 "requested_capabilities":{"platformName":"Android"},"lease_seconds":900}
```

### 2.2 分配结果

| 预约 | 自动选中的池 | 建机宿主 | 设备 | ADB Endpoint | STF |
|---|---|---|---|---|---|
| `c363e8f5…` | `8b97a9e7`（android-10.0.30.171） | **10.0.30.171** | Android设备-016dd7 | `10.0.30.171:32919` | present/ready/using |
| `4dfdc122…` | `c8e22592`（android-10.0.30.55） | **10.0.30.55** | Android设备-487c93 | `10.0.30.55:32769` | present/ready/using |

两台设备同时 `busy/healthy`，两个预约同时 `active` 且租约未过期。

### 2.3 时间线（UTC）

| 时刻 | 事件 |
|---|---|
| 01:49:20.366 | 预约 1 创建 → 选中 171 池 |
| 01:49:20.917 | 预约 2 创建 → 选中 55 池 |
| 01:49:24.163 | 55 上 `create` **failed `IMAGE_DIGEST_MISMATCH`**（见 §四 说明） |
| 01:49:24.178 | 55 上 `create` **succeeded** |
| 01:49:53.886 | 55 上 `delete` succeeded（失败占位补偿清理） |
| 01:49:30 | 55 容器起：`alcor-df-emulator-542ecf9b…`，`10.0.30.55:32768->4723`、`:32769->5555` |
| 01:51:23.874 | 171 上 `create` **succeeded**（容器 `alcor-df-emulator-60476afc…`） |
| ~01:52 | 两台设备 `busy/healthy`，两个预约 `active` |
| 01:54 | STF：`total=70 present=2 ready=2 using=2` = `10.0.30.55:32769` + `10.0.30.171:32919` |
| 01:57:23.8x | 两宿主 `delete` succeeded |
| 01:57:51 | 活跃 Android 设备 **0 条** |
| 01:58 | 两宿主模拟器容器均消失 |

### 2.4 回收后的最终状态

```
device_pools:
  android-10.0.30.171  active  total_target=0  min_ready=0  max_concurrency=1
  android-10.0.30.55   active  total_target=0  min_ready=0  max_concurrency=1
  iOS - 10.0.33.68     active  total_target=1  min_ready=0  max_concurrency=2

devices (android): 活跃 0 条（历史 62 条均为 deleted）
本轮两个预约: released / released
```

容器侧：

```
10.0.30.171: stf / stf-adb / rethinkdb / vega-*      ← 无模拟器容器
10.0.30.55 : worker-05 / node-exporter               ← 无模拟器容器
```

**证明按需模型成立：空闲时不保留设备，任务结束即释放资源。**

## 三、一键脚本验收

### 3.1 脚本范围

`scripts/onboard-device-host.sh` 做：前置检查 → 系统用户与目录 → `host-agent.env`
→ systemd 单元 → 自检。**不登记 Host、不动镜像、不动池容量**（ADR-0039 决策 2）。

### 3.2 真实测试

| 用例 | 动作 | 结果 |
|---|---|---|
| `--check-only` | 在 55 上执行 | `sh -n` 语法通过；`/dev/kvm`、docker、磁盘、控制面 `healthz=200` 全部通过；**未改动系统** |
| `--help` / 未知参数 | `--bogus` | 正确打印 usage 并退出 2 |
| **全新机器完整安装** | 清空 `/opt/alcor-device-farm`、`/etc/alcor-device-farm`、systemd 单元、`device-farm` 用户后执行 | 一次成功：建用户、装二进制、写 env(0600)、装单元、`enable --now`；**55 在 0.6 秒内恢复 `online`**，`NRestarts=0` |
| fail-fast | 不在仓库检出内且未给 `--unit-file` | **在改动系统前失败**并给出明确指引；`/opt/alcor-device-farm` 未创建 |
| 幂等 | 已就绪状态下重复执行 | `env md5` 前后一致（未被覆盖）；假 Host ID 未写入；服务仍 `active`、`NRestarts=0` |

安装后自检输出：

```
agent binary : device-host-agent version=dev commit=04573be794ea go=go1.26.5 os=linux arch=amd64
unit state   : active
enabled      : enabled
restarts     : 0
device-farm  : device-farm kvm docker
```

控制面确认：

```
iOS 宿主机 - 10.0.33.68  | online | age 1.9s
安卓宿主机 - 10.0.30.171 | online | age 2.1s
安卓宿主机 - 10.0.30.55  | online | age 2.4s
```

### 3.3 开发中发现并修复的两个真实缺陷

1. **`repository_root` 在非仓库检出时解析为 `/`**，导致单元模板路径变成
   `//deploy/...` 而失败，且当时已经建了用户、写了配置 —— 留下半成品。
   → 改为「先探测仓库内模板，否则用 `--unit-file`」，并把该校验**提前到改动系统之前**。
2. **`--check-only` 缺少 `--server-url` 时静默通过**，会给出假阳性。
   → 把参数校验前置为独立步骤。

> 这两个缺陷都是靠**真实执行**发现的，不是靠读代码。

## 四、镜像 digest 的重要发现（影响后续每台服务器）

同一 tag 在 171 上是 `ID/digest = e61f195a…`（走 registry 推拉产生的 repo digest）；
经 `docker save` → 传输 → `docker load` 到 55 后变成 `ID/digest = 4bfbcf34…`。
两者 `Created`（2026-08-26T06:41:18Z）、层数（28）、env 数（35）**完全相同**，
属**同一内容、不同 digest**。

因为 `device_images` 的唯一键是 **`(docker_digest, md5(resource_config))`**，
**digest 就是身份**，所以：

- **每台宿主本地解出的 digest 各自对应它自己的镜像记录**；
- 55 加载后的 `4bfbcf34…` 正好等于 55 池默认镜像记录 `109cf3d6` 期望值 —— **55 不需要改池配置**；
- `VerifyImageDigest`（`internal/providers/docker/provider.go:138-145`）接受
  `metadata.ID` 或任一 `RepoDigests` 命中，因此校验通过。

`0/0` 下第一次 `create` 出现的 `IMAGE_DIGEST_MISMATCH` 及随后 `delete`，
是 warmpool 的**失败占位补偿清理**（ADR-0038）：按需池尝试在 171 上建 55 的镜像失败后，
把占位清掉并改由 55 建机，最终成功。这属预期行为，不是缺陷。

### 4.1 当前镜像可用性记录（验收后）

| 宿主 | 镜像 | available | 说明 |
|---|---|---|---|
| 171 | `abc5e576`（digest `e61f195a…`） | ✅ true | 本次成功建机写入 |
| **55** | **`109cf3d6`（digest `4bfbcf34…`）** | **✅ true** | **本次成功建机写入** |
| 55 | `abc5e576` | ❌ false | 55 确实没有 171 的 digest（正确记录） |
| 171 | `109cf3d6` | ❌ false | 2026-09-28 历史记录，仍正确 |

## 五、遗留

1. **镜像 tag 仍带 `127.0.0.1:5001/` 前缀，但两台机器都没有该 registry**
   （171 的 `:5000` 是无关的 `vega-face-search` nginx）。当前 `create` 不走 pull，
   所以不影响建机；但换镜像或 `--pull` 前必须先解决。长期建议在每台宿主常驻本机
   registry，使 digest 可复现（标准流程 §5.2 方案 C）。
2. **55 现在是 Ubuntu 24.04 + Docker 29.1.3**（原为 22.04），本轮建机已真实跑通。
3. `deploy/hosts/.env` 中 `DEVICE_FARM_SERVER_IP=10.0.20.56` 与实际控制面
   `10.0.80.220` 不符（commit `9cc1621` 已确认该机不存在），待清理。
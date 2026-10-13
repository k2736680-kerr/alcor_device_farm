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

## 五、镜像架构：per-host registry（ADR-0040，2026-10-13 补充）

### 5.1 问题

`device_images.docker_image` 全部指向 `127.0.0.1:5001/...`，但**该 registry 在任何宿主机上都不存在**：
55 无监听；171 的 `:5000` 是无关的 `vega-face-search` nginx（`/v2/` 返回 404）。
官方镜像准备链路 `deploy/docker-emulator/images/prepare.sh` 又**必须 push** 到
`DEVICE_FARM_ANDROID_PUBLISH_REPOSITORY` 才能取得不可变 digest。
⇒ 全新宿主机既无处拉镜像，也无法走官方构建链路。

### 5.2 决策：每台宿主机自带一个本机 registry

统一使用 `127.0.0.1:5001` 前缀，只绑定回环、数据存 docker volume、
`restart unless-stopped`。这样 `device_images.docker_image` 在各宿主间保持一致，
而 digest 由本机 registry 确定。

安装：`scripts/install-local-image-registry.sh`（幂等；已在 54、55 上实际安装并验证）。

### 5.3 实测结论：digest 跨机可复现

| 步骤 | 10.0.30.54 | 10.0.30.55 |
|---|---|---|
| 构建/load 测试镜像 | `ID=sha256:c81401c0…` | `ID=sha256:c81401c0…` |
| push 后 repo digest | `c81401c0…` | `c81401c0…` |
| 从 registry 拉回 | `c81401c0…`（稳定） | — |

真实模拟器镜像同样验证：

| 步骤 | 结果 |
|---|---|
| 55 上 save → 搬 54 → load | `ID=sha256:4bfbcf34…`（与 55 一致） |
| 54 上 push 进本机 registry | digest `sha256:4bfbcf34…`（与 55 一致） |

**结论：registry push 产生的 repo digest 等于 image ID，且跨机器可复现。**
因此镜像分发退化为「`save`/`load` 进本机 registry」，且**同一份内容在任何宿主上都得到相同 digest**。

### 5.4 `docker save` 不是字节确定的（更正）

同一个镜像在 55 上两次 `docker save`，tar 的 sha256 **不同**：

| 时间 | sha256 |
|---|---|
| 2026-09-12 | `9681e2c8aac942aa0e5b6f3d84bde272d216ca3e97cff0b39a39c9c3dd081021` |
| 2026-10-13 | `c545b76d527e13303247f021cff1e8261d676c02417711d77a4081fe7994e7f4` |

⇒ tar 的 sha256 **只能证明单次传输未损坏**，不能用于跨次比对，更不能作为镜像身份。
**镜像身份必须用 load 后的 image ID / repo digest 判定。**

### 5.5 171 与 54/55 的镜像差异（已查明）

| | 171 | 54 / 55 |
|---|---|---|
| image ID | `e1a1d8f5c468` | `4bfbcf348b7d` |
| repo digest | `e61f195a…` | `4bfbcf34…` |
| `Created` | 2026-08-26T06:41:18.915775822Z | 同 |
| 层数 | 28 | 28 |
| **DiffIDs 逐层比对** | **28/28 完全相同** | |
| `.Size` | 7,155,557,690 | 7,237,714,980 |
| 额外 tag | `-audio-inject`（与主 tag **同一 ID**） | 无 |

**主结论：171 与 54/55 的 28 层 DiffID 完全一致，文件系统内容相同。**
`.Size` 的 82 MB 差异是**本地层记账差异**，不是镜像内容差异；
`Config` 的差异只是 `None` 与 `False`/`""` 的 JSON 序列化差异。

**决定性的交叉验证**：把 171 的镜像 `save` 出来、传到 54、再 `load`，
得到的 ID 是 **`4bfbcf34…`（与 54/55 一致）而不是 171 的 `e1a1d8f5c468`**。
⇒ `e61f195a…` 这个 repo digest **不可由内容复现**，它是 171 上一次 registry 交互留下的
历史记录，而当时那个 registry 已不存在（171 上 `127.0.0.1:5001` 连接被拒）。

**`-audio-inject` 不是独立变体**：在 171 上与主 tag 指向**同一个 image ID**
（`e1a1d8f5c468`），只是一个额外的 tag。所以无需为新宿主补齐该变体。

### 5.6 本机 registry 的边界（重要）

`127.0.0.1:5001` 只绑定回环，因此**一个宿主无法从另一个宿主的 registry 拉镜像**（已实测：
55 → `10.0.30.54:5001` 连接被拒）。这不是缺陷，而是刻意的安全边界，但意味着：

**每台宿主必须各自独立取得镜像**（`save`/`load` 或本机构建），不能靠网络共享。

因此每台宿主自己 `push` 进本机 registry 后得到的 digest 是**该宿主自己的**；
而 §5.3 已证明同一份内容在不同宿主上 push 会得到**相同** digest，所以
`device_host_image_states` 的逐宿主记录依然必要（记录的是"这台机器有没有"），
但 digest 值本身可以一致。

## 六、遗留

1. **171 即将重装**，其上的 `e1a1d8f5c468` 镜像会丢失。由于 54/55 上的
   `4bfbcf34…` 与之文件系统完全相同，重装后可直接从 54/55 任一台搬运，
   无需找回 171。
2. `deploy/hosts/.env` 的陈旧 `10.0.20.56` 已改为 `10.0.80.220`。
   该文件 ACL 原本只有 `Write, Read, Synchronize`（无 Delete），导致原子替换失败，
   已补 `Modify` 权限。
3. 55 现在是 Ubuntu 24.04 + Docker 29.1.3，本轮建机已真实跑通。
4. 54 上原有 `daemon.json` 含日志轮转（`100m × 5`）与 HTTP 代理
   （`10.0.30.155:17887`），安装脚本已正确**保留未覆盖**。
# ADR-0037：多宿主机 STF 可见性判定与镜像验证解耦

## 状态

已确定。

## 背景

设备农场从单宿主机扩展到多宿主机（171、Mac 10.0.33.68、55，后续还会继续增加）。当前存在一个使新宿主机接入必然失败的结构性问题。

### 约束一：STF 全局只有一套，位于 171

220 控制面通过 `DEVICE_FARM_STF_BASE_URL=http://10.0.30.171:7100` 远程调用唯一的 STF 实例。`deploy/stf/README.md` 明确「增加设备不改变拓扑」。220 的 `device-farm-server` 是 `read_only` + `cap_drop: ALL` + 无 Docker Socket 的容器，`internal/server/server.go` 注释明确服务端不做 Provider 操作，因此 STF 不能部署到 220，也不能由服务端向宿主机分发。

实测确认：`10.0.30.171:7100` 可达；`10.0.30.171:5038` 从外部 **ConnectionRefused**，171 本机 `ss -ltn` 显示 `LISTEN 127.0.0.1:5038`。

### 约束二：STF ADB 注册必须从 STF 所在机本地发起

`internal/adapters/stfadb/client.go` 硬性要求 `STF ADB server must use a loopback address`，注册语义是在 STF 所在机执行 `adb -H <loopback> -P <port> connect <device-endpoint>`。171 的 `stf-adb` 容器端口为 `127.0.0.1:5038->5037`，契约测试与 `docs/stf_adapter.md:155` 均禁止将其暴露。

因此宿主机无法直接向 171 的 STF 注册自身设备；跨宿主机设备的汇入只能由 171 侧发起 `adb connect <宿主机设备端口>`。

### 冲突点

`internal/imagecatalog/service.go` 把 `stf_registered` 作为镜像验证的硬性条件：

```go
if !result.DigestVerified || !result.Ready || !result.STFRegistered {
    return failPreparation(ctx, tx, job.ID, "IMAGE_VALIDATION_INCOMPLETE")
}
```

而 `internal/agent/agent.go:844` 的实现是：

```go
"stf_registered": agent.registrar != nil,
```

该值表达的是「本宿主机 Agent 是否配置了 STF ADB registrar」，由宿主机本地环境变量 `DEVICE_FARM_AGENT_STF_ADB_SERVER` 决定（`cmd/device-host-agent/main.go` 中为空即跳过）。

该检查在单宿主机时代是合理的：镜像验证的目的包含「证明该镜像在真实设备上能被 STF 看到」。但在多宿主机架构下，宿主机**不应**也无法本地注册到 171 的 STF，于是：

- 宿主机不配 registrar → `stf_registered=false` → 镜像验证永远失败，池无法建立；
- 宿主机为了通过验证而配置一个指向本地回环的 registrar → 该注册**功能上是空转的**（171 的 STF 只认自己的 adb server），仅为满足布尔自检，且每台新宿主机都要额外部署组件。

### 实测依据（2026-09-14，220 + 171 + 55 真实环境）

1. **STF 可见性判定本身是可靠的**：171 STF inventory 实测 155 条记录，因 `--no-cleanup` 长期累积；其中 `present=true` 仅 1 条，实际对应 171 上唯一在跑的模拟器（`adb devices -l` 亦仅 1 台）。幽灵记录均为 `present=false`，而 `internal/adapters/stf/client.go` 的 `Visible()` 要求 `present && ready`，能准确排除幽灵。

2. **设备汇入的数据面已具备**：`GET /api/v1/devices` 实测返回 `adb_endpoint` 字段，171 侧守护进程可据此自动发现全部宿主机的设备端点，无需人工维护列表。

3. **既有脚本可直接复用**：`scripts/stf-connect-emulators.sh` 已实现「在 171 本地、从 `stf-adb` 容器内执行 `adb connect <endpoint>`」。

4. **55 的失败实录（决定性证据）**：55 宿主机设备 `ba49926c`（serial `10.0.30.55:32797`）的命令历史为
   `10:28:04 create succeeded` → `10:30:29~10:57:27 连续 11 次 restart succeeded` → `10:59:19 起 stf_not_visible 每 2 秒一条` → `10:59:34 delete succeeded`。
   同期 `validate_image` 仅 1 次且 `succeeded`，对应镜像 `109cf3d6` 状态为 `ready`。
   **即：镜像验证通过了，设备仍然完全不可见。** 证明本地 registrar 的注册结果与 171 STF 的可见性无关。

5. **网络路径已验证**：在 171 上从 `stf-adb` 容器内实测 `nc -z 10.0.30.55 22` 返回 **OPEN**，而 `127.0.0.1:5038` 对外为 ConnectionRefused、STF 的 `Visible` 判定可靠 —— 171 侧具备直连其他宿主机设备端口的能力，且不违反回环约束。

6. **171 自身不需要守护进程**：171 的设备由本机 Host Agent 直接向本机 `127.0.0.1:5038` 注册（实测 `adb devices -l` 中 `10.0.30.171:32821` 即为 `Android 15-1`，`present=true`），该路径合法且真实工作。本决策只对**远程宿主机**生效。

### 独立发现（不在本 ADR 决策范围）

- STF inventory 中 4 条 `using=true` 的记录同时为 `present=false`，属永久僵尸占用，由 `--no-cleanup` 导致。
- `devices.stf_serial` 字段全表为空（含正常设备），该字段为预留未使用。

## 决策

1. **镜像验证不再以 `stf_registered` 为硬性门禁**：`internal/imagecatalog/service.go` 的镜像验证判定保留 `digest_verified` 与 `ready`；`stf_registered` 降级为诊断信息，不再参与 `IMAGE_VALIDATION_INCOMPLETE` 判定。

   理由：STF 可见性已由 `internal/reconcile/service.go` 的 `stf_not_visible` 对**实际运行的设备**做持续、权威的把关（实测证明其 `present && ready` 判定可靠）。镜像准备阶段再判一次属于重复把关，且在跨宿主机场景下判不准（宿主机无法也不应注册到 171 的 STF）。

2. **设备汇入 STF 采用 171 侧统一拉取（仅对远程宿主机）**：171 维护单一守护进程（systemd timer），定期调用 `GET /api/v1/devices?platform=android` 读取**其他宿主机**的 `adb_endpoint`，对每个端点调用既有 `scripts/stf-connect-emulators.sh`。

   - 171 自身设备继续由本机 Host Agent 向 `127.0.0.1:5038` 注册（现状不变，实测有效），守护进程通过宿主机地址过滤跳过 171 本地设备，避免重复 connect。
   - 其他宿主机侧不部署 STF 组件，不配置 `DEVICE_FARM_AGENT_STF_ADB_SERVER`。

3. **新宿主机接入流程固定为三步**：安装 Host Agent → 向 220 登记宿主机 → 创建或加入设备池。171 侧无需任何改动，守护进程自动发现新设备。宿主机侧无新增常驻组件。

4. **继续保持 5038 回环约束**：不放开 `stfadb` 的 loopback 校验，不把 171 的 5037/5038 暴露到内网。`docs/stf_adapter.md:155` 与 `internal/contract/stf_deployment_test.go` 的契约断言继续有效。

## 边界

- 不改变 STF 拓扑（全局单实例，位于 171）。
- 不重写 STF 的看屏、触控、日志、文件能力；STF 仍仅通过 Adapter 使用。
- 不引入宿主机侧新增常驻组件（不装 adb 容器、不装隧道、不装 autossh）。
- 不把 220 变成 Provider 执行节点；服务端继续只做数据面与状态面。
- 不改变 `stf_not_visible` 的判定逻辑（`present && ready`，按 `serial` 精确匹配）。
- 不改变 `internal/adapters/stfadb/client.go` 的回环校验。
- 本 ADR 不覆盖 iOS 路径；iOS 不参与 STF ADB 注册。
- STF inventory 的 155 条历史幽灵记录不在本 ADR 处理范围。

## 后果

- 新宿主机接入不再需要为满足镜像验证而部署空转组件，横向扩展成本降到最低。
- `stf_registered` 不再作为镜像可用性的门禁，镜像验证语义回归「镜像本身是否可用」。
- 设备在 STF 中的可见性由 171 侧守护进程负责，与镜像准备流程解耦，故障域更清晰。
- 171 侧新增一个守护进程，需监控其运行状态；其失效只影响 STF 远控可见性，不影响设备创建与预约（`stf_not_visible` 仍会标记，但按既有宽限期处理）。
- **既有测试需要同步修改**：`internal/api/imagecatalog_integration_test.go` 中「`stf_registered=false` 必须导致 `IMAGE_VALIDATION_INCOMPLETE`」的用例前提不再成立，需按新语义调整；`internal/agent/execute_test.go` 对 `stf_registered` 的断言可保留为诊断字段校验。

## 待确认事项

- 171 守护进程的实现语言与部署位置（建议宿主机 host 层 systemd，非容器）。
- 拉取间隔（建议 30s，与 `STFVisibilityGrace` 默认值一致）。
- 守护进程访问 220 API 的 Token 来源与存放方式。
- 是否需要在守护进程内对 `adb connect` 失败做退避与告警。

## 落地实现（2026-09-14）

决策 2 已实现并实测，交付物：

| 文件 | 作用 |
|---|---|
| `scripts/stf-sync-remote-emulators.sh` | 同步脚本。从控制面 `GET /api/v1/devices` 拉取 ready 的 Android 设备，在 `stf-adb` 容器内逐个 `adb connect`，并按宿主机地址跳过本机设备。支持 `--dry-run` / `--verbose`。 |
| `scripts/install-stf-sync-timer.sh` | 一键安装（root）。把脚本装到 `/opt/alcor-device-farm/scripts/`，生成 `/etc/alcor-device-farm/stf-sync.env`（不覆盖已有配置），安装并启用 timer。 |
| `deploy/stf/alcor-device-farm-stf-sync.service` | oneshot 服务单元。 |
| `deploy/stf/alcor-device-farm-stf-sync.timer` | 开机后 3 分钟首次触发，之后 **每 20 秒**一次（`AccuracySec=1s`）。间隔受「STF 可见性宽限期」硬约束，见下。 |
| `deploy/stf/stf-sync.env.example` | `/etc/alcor-device-farm/stf-sync.env` 的模板。 |

上述「待确认事项」的落定结果：

- **实现语言与位置**：Bash 脚本 + host 层 systemd（非容器），符合决策 2「宿主机侧无新增常驻组件」。
- **拉取间隔**：**20 秒**（2026-09-15 由 2 分钟收紧）。这不是性能取舍，而是**正确性约束**：
  `reconcile` 的 `STFVisibilityGrace`（默认 30s）以「最近一次成功 `create`/`rebuild` 的完成时刻」为锚点，
  且 **`restart` 不重置该锚点**。若设备在宽限期内仍未被 `adb connect` 进 STF，`reconcile` 就开始累计
  `stf_not_visible` 失败并触发自愈重启，而重启又不重置锚点 ⇒ **「反复重启但永远不可见」的死循环**。
  因此间隔必须**显著小于宽限期**；20s 留出约 10s 余量。`AccuracySec` 同步由 15s 收紧为 `1s` ——
  systemd 默认 `1min` 会把 20s 的间隔抖动到分钟级，等于重新超窗。
  实测代价可忽略：单次执行约 43ms CPU；journal 每天约 4300 行摘要。
- **Token 来源**：控制面（220）的 `DEVICE_FARM_SECURITY_SERVICE_TOKEN`。存放于 `/etc/alcor-device-farm/stf-sync.env`，权限 0600，属主 root；也可用 `DEVICE_FARM_SERVICE_TOKEN` 直接注入，或由 `DEVICE_FARM_API_TOKEN_FILE` 指向文件。**不写入仓库。**
- **失败退避与告警**：脚本对每个 endpoint 有 15s 超时，失败计入 `failures` 并以非零退出码结束（由 systemd 记录到 journal）。不做指数退避——单次失败由 20 秒后的下一轮自然重试，避免引入常驻状态。
- **分页**：服务端 `page_size` 上限为 200（`openapi/device-farm-v1.yaml` 的 `PageSize` 组件），脚本自动翻页取全，宿主机规模增长后仍能取全。

**2026-09-15 已在 STF 宿主机（171）实装并验证**：

- timer `active` + `enabled`；service 单次执行 `code=exited, status=0/SUCCESS`。
- 首次上线时 171 上只有本机设备，journal 摘要 `sync done: connected=0 skipped_local=1 failed=0`
  —— **`skipped_local=1` 是关键证据**：脚本确实取回了控制面设备清单，并正确识别本机地址后跳过。
- `/opt/alcor-device-farm/scripts/stf-sync-remote-emulators.sh` 与仓库副本 md5 一致（`7531ace35eec338c6b7e88abb549d1ee`），installer 安装结果可复现。
- **2026-09-15 远程设备首次真实接入（决定性验证）**：`10.0.30.55` 的设备池恢复后，journal 变为
  `sync done: connected=1 skipped_local=1 failed=0` —— **「跳过本机」与「接入远程」两个分支同时被真实执行**。
  STF 侧 `present_count=2`，其中 `10.0.30.55:32799 present=True ready=True using=False`；
  从 171 执行 `adb -s 10.0.30.55:32799 get-state` 返回 `device`，`shell getprop` 与 Appium `/status`
  均正常 ⇒ 远程宿主机的设备**确实可被 STF 看到并可远控**，决策 2 至此闭环。
  完整时间线与可复现命令见 `docs/evidence/host55_emulator_restore_20260915.md`。

实装过程暴露并修复了三个实现缺陷（详见证据文档「遗留」第 6 条）：

1. `/api/v1/devices` 的 `page_size` 服务端上限为 200，传更大值返回 **400** → 改为按页遍历取全。
2. 安装器把脚本复制到 `/opt/alcor-device-farm/scripts/` 后，脚本默认的「脚本上级目录/deploy/stf」compose 路径推导**失效**，
   导致 service 反复以 `STF compose file not found` 失败 → 安装器改为在生成的 env 中写入 `STF_COMPOSE_FILE` / `STF_ENV_FILE`。
3. 脚本对 `DRY_RUN` / `VERBOSE` **无条件赋值**，覆盖了 systemd `EnvironmentFile` 传入的值；且 service 每次运行在 journal 里**完全静默**，
   排障无从下手 → 改为 `"${VAR:-0}"` 允许 env 覆盖，并**始终**输出一行运行摘要。

实测证据见 `docs/evidence/multi_host_stf_visibility_20260914.md`。

## 未纳入本 ADR 的相邻问题

- STF inventory 的历史幽灵记录（实测 154 条 `present=false`）。因 `--no-cleanup` 是契约强制的，该问题会随宿主机接入持续重现，**需要周期性清理**。清理周期未定，见证据文档「遗留」。
- 4 条 `using=true` 且 `present=false` 的永久僵尸占用。

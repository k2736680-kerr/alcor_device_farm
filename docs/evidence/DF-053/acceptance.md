# DF-053 受管虚拟设备自动淘汰替换与可用性收敛验收证据

验收日期：2026-08-29

## 当前结论

本地实现、真实 PostgreSQL 集成测试、Console 回归、构建和静态检查通过。DF-053 Server 与 Mac Agent 已部署到测试环境，并完成真实 CoreSimulator 删除、自动补建、双设备并发预约与释放验收。最终 iOS Pool 保持两台 `ready/healthy`，开放 Reservation、Session 和 Host Command 均为 0，DF-053 状态为 `completed`。

基线版本：`c63f6a516ef4`，工作分支：`codex/device-farm-v2`。

## 实现与边界

- iOS Pool 只把 provisioning/booting 和 healthy 的 ready/reserved/busy/recycling 计入可服务目标；故障资源在 Provider 删除成功前继续占用 Host 槽位，防止盲目超建；
- 完整 inventory 成功且已登记 Simulator 持续缺失超过 30 秒时才标记故障；inventory 请求失败和未升级的旧 Agent 均失败开放，不把空清单解释为全部设备消失；
- 无活动 Reservation、Session 或在途 Host Command 的故障 Simulator 只生成一个幂等 delete Command；删除成功后保留历史记录、禁用成员关系并按原目标补建，删除失败则保留明确故障并停止补建；
- `quarantined` 仍保留给真机、清理失败、审计与人工处置；受管 iOS Simulator 日常路径只把它作为短暂安全过渡态；
- 基础模板被替换后仍可读取历史 Device 的静态 Host、Runtime 和 Device Type 配置，每次补建仍重新验证当前 Host 心跳、目录和容量；
- Console 日常只突出可用、使用中、恢复中、故障和历史记录；Host 的 online 明确显示为“Agent 心跳”，不再暗示设备可用；
- 没有新增 Alcor Case、Run、Result、Artifact 业务模型，没有重写 Appium、STF、Baguette 或 DaFit 能力。

## AT-IOS-SIM-016～019 自动化证据

使用临时 PostgreSQL 17.10，完整执行 migrations up/down/up 和 Repository 集成测试：

```powershell
.\scripts\verify-migrations.ps1 -Port 55433 -RunRepositoryTests
```

结果：通过。默认端口 `55432` 已被本机既有 PostgreSQL 使用，因此改用空闲端口 `55433`；验收结束后临时 PostgreSQL 已停止，数据目录、日志和监听端口均已清理。

关键用例：

| 验收项 | 自动化用例 | 结果 |
| --- | --- | --- |
| AT-IOS-SIM-016 | `TestIOSUnavailableDeviceIsDeletedThenReplacedWithoutChangingTarget` | 只排队一个 delete；成功后目标仍为 2，并创建全新替代设备 |
| AT-IOS-SIM-017 | `TestHeartbeatOnlyQuarantinesMissingIOSDeviceAfterCompleteInventory` | 不完整 inventory 不改变设备；完整清单持续缺失才收敛为故障，事件只写一次 |
| AT-IOS-SIM-018 | `TestIOSAutoReplacementDeleteFailureBlocksOverbuildAndRetryStorm` | 删除失败不创建第三台、不重复排队、故障事件不形成风暴 |
| AT-IOS-SIM-019 | `TestIOSUnavailableDeviceIsDeletedThenReplacedWithoutChangingTarget` | 基础模板成为 deleted 历史后，仍使用其静态 Runtime/机型补建 |
| 稳定化降噪 | `TestIOSSharedAutomationStabilizationDoesNotQuarantineSimulator` | 连续 5 次 reconcile 只记录 1 条稳定化事件 |
| 指标与契约 | `TestDatabaseMetricsExposeDeviceSchedulerAgentAndReservationState`、部署契约测试 | 平台、Pool 目标和四类可用性指标与告警规则通过 |

## 全量回归

- `go test ./...`：通过；
- `go vet ./...`：通过；
- PostgreSQL migrations up/down/up、iOS Session、Repository、Scheduler、Reaper、Reconcile、Host Command、Metrics、API、Warm Pool 集成测试：通过；
- Console `pnpm test`：9 个测试文件、45 项测试通过；
- Console 定向回归：PoolsPage、DevicesPage 共 23 项测试通过；其中包含“不健康 busy 设备不得计入可服务容量”的专门回归；
- Console `pnpm build`：通过；仅保留既有的大 chunk 体积警告；
- `git diff --check`：通过；仅有 Windows LF/CRLF 提示。

## 测试环境真实验收

测试 Server：`10.0.30.171`；Mac Host：`10.0.33.68`。部署产物的内嵌版本标记为：

```text
version=df053
commit=6c946dabdc28
build_date=2026-08-29T03:46:45Z
Server SHA-256=35bb2161c7162f3e029b5085c8e06d91904d92efb62c39b82868a1efae53cf30
Agent SHA-256=8d53c2ae289d8072034cdb2b41c2566797efe9f980b6917fadf88ac4f5775403
```

真实环境结果：

- 先通过正式管理 API 把现场误设的 `total_target/min_ready/max_concurrency=1/1/1` 恢复为 `2/2/2`，第二台 Simulator 自动创建并达到 `ready/healthy`；
- 对空闲基础模板设备触发故障后，只生成 1 条 `operation_source=ios_auto_replacement` 的 delete Command；事件顺序为 `ios_auto_replacement_queued`、Provider delete 成功、`ios_auto_replacement_delete_completed`、新 create 成功；
- 原 Provider UDID 前缀 `9FECC106` 已从 `simctl` 清单消失，原 Device 保留为 `deleted` 历史且 membership 已禁用；Controller 使用其历史 Runtime/Device Type 配置补建新设备；
- 最终受管 Simulator 为 `2AB04FF6…` 和 `09DF8E4E…`，均由 CoreSimulator 报告 `Booted`，数据库均为 `ready/healthy`；
- 两个 Reservation 同时进入 active，分别占用两个不同 Device，释放后 `active_reservations=0`、开放 Session 为 0、pending/leased Command 为 0；
- 从部署工作站访问 HTTPS Console 返回 200，外部 `/readyz` 返回 200；指标报告 iOS Pool `target(total/min_ready/max_concurrency)=2/2/2`、`available=2`、`fault=0`、`in_use=0`、`recovering=0`；
- 删除失败、完整 inventory 失败开放、重复 reconcile 降噪和阻止第三台超建由真实 PostgreSQL 集成测试覆盖；真实成功路径使用 Mac CoreSimulator 与正式 Host Agent 覆盖，未使用数据库 Mock 代替 Provider 删除；
- Server、iOS Session Fence 隧道、Mac Agent、Baguette、STF、PostgreSQL 和 Console Proxy 在验收后保持运行或健康，Android 镜像与现有 STF/DaFit 执行能力未改动。

## 部署清理与回滚

- Linux 只保留当前容器 `alcor-device-farm-server-df017`、当前镜像 `alcor-device-farm:df053-self-healing-20260829`，以及一个即时回滚容器/镜像 `alcor-device-farm-server-pre-df053-rollback` / `alcor-device-farm:df051-member-remote-20260820`；
- Mac 只保留当前 `device-host-agent` 和一个即时回滚文件 `device-host-agent.pre-df053.rollback`；
- DF-028～DF-051 的旧 Server 容器、旧 Server 镜像、多代 Agent 备份、旧回滚日志、本地/远端 DF-053 临时构建与 API 响应文件均已删除；
- PostgreSQL、STF、Android Emulator、Console Proxy、Baguette 和其他非 DF-053 资源未纳入清理范围。

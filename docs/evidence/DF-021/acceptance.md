# DF-021 实施与验收证据

## 当前结论

DF-021 已在真实 Linux KVM、Docker Android 16 Emulator、STF 3.7.9 和 Appium 环境完成故障恢复、清理、数据隔离和稳定性验收，状态为 `completed`。

真实验收覆盖 Agent 离线、Server 重启、STF 超时、Appium 不健康、Emulator boot timeout、Host Command 租约超时/Agent 中断和 Docker volume 删除失败。所有场景均通过正式状态机、Host Command 或管理 API 收敛，没有直接修改生产数据库状态；最终开放 Reservation 为 0，受管容器/网络/卷稳定为 `1/1/1`。

## 真实环境与版本

- 验收日期：2026-08-06；
- Linux/KVM Host：`10.0.30.171`，Ubuntu 22.04.3 LTS，`/dev/kvm` 可读写；
- Device Farm Server：`alcor-device-farm:df021-stf-recovery10-20260806`，镜像 ID `sha256:4f8605935c3d0be2f8d6e2fa149efb9d797fef74d86097748bc8c3bc5e23b91a`；
- Host Agent：SHA-256 `bf457d76eddc39358d0664dd0702d4ec7a38826a8679e24417a63ac6a3b27cff`；
- Agent 生产参数：Host Command lease `300s`、Provider command timeout `270s`；
- Emulator：Android 16 / API 36 / x86_64，5 GiB、4 CPU；
- STF：DeviceFarmer/STF 3.7.9；
- 真实证据目录：`/home/kerr/df021-acceptance-20260806/`。

Agent 运行时同时固定以下配置，避免重启后退回错误默认值：

```text
DEVICE_FARM_DOCKER_EMULATOR_DEVICE="Pixel 9"
DEVICE_FARM_AGENT_STF_ADB_SERVER=127.0.0.1:5038
DEVICE_FARM_ADB_BINARY=/home/kerr/alcor-device-farm-tools/platform-tools/adb
DEVICE_FARM_AGENT_LEASE_SECONDS=300
DEVICE_FARM_AGENT_COMMAND_TIMEOUT=270s
```

## 本任务修复

- STF 可见性必须同时满足 `present=true` 和 `ready=true`；
- Host Agent 在 create/rebuild/restart 成功前，把动态 ADB Endpoint 注册到同机 loopback STF ADB server，并在 heartbeat 中幂等补偿；
- create/rebuild 完成后进入 STF 可见性稳定窗口，稳定完成前设备保持不可调度；
- Agent heartbeat 不得用容器、ADB、Appium healthy 覆盖 STF unhealthy 原因；
- STF 恢复后清零 Reconciler 失败计数，Host/Agent 恢复后清除残留失败计数；
- Warm Pool 和管理命令 completion 在同一事务写入不可调度的 `ready/unhealthy/0`，封闭 Scheduler 竞态；
- Agent lease 和 command timeout 改为可配置参数，且启动时强制 lease 严格长于 command timeout；
- 清理失败保留确定性的失败码并隔离，修复根因后只能通过正式 rebuild API 恢复。

## 自动化门禁

本地固定 Go 工具链执行：

```text
go test ./cmd/device-host-agent ./internal/agent ./internal/hostcommand -p=1 -count=1
go test ./internal/... -p=1 -count=1
```

结果：所有 package 通过，包括 STF inventory、STF ADB registrar、Agent、Host Command、Reconciler、Scheduler、Warm Pool、Appium 和 Docker Provider 回归。新增 Agent 环境参数测试也在 Linux Go 1.24.6 容器中通过：

```text
ok github.com/Ad-Quanta/alcor-device-farm/cmd/device-host-agent
```

## 真实故障矩阵

| 故障 | 注入与预期状态 | 真实结果 | 证据日志 |
|---|---|---|---|
| Agent 离线 | 停止 Agent，Host 变为 offline，设备不可调度 | `host_offline`，可调度数 0；Agent 恢复后回到 `online/ready/healthy/0` | `fault-agent-offline-final.log` |
| Server 重启 | active Reservation 期间重启 Server | Reservation、Session ID 和 serial 保持；release 后 `ready/healthy/0` | `fault-server-restart.log` |
| STF timeout | 暂停 STF API/ADB 可见性 | `ready/unhealthy`，可调度数 0；恢复后 `ready/healthy/0` | `fault-stf-timeout.log` |
| Appium unhealthy | 终止真实 Emulator 内 Appium | 3 次失败后 `quarantined/unhealthy`；正式 rebuild 后 `ready/healthy/0` | `fault-appium-unhealthy.log` |
| Emulator boot timeout | 临时使用 `120s/30s` lease/command timeout，正式 rebuild | Host Command 3 次后 `failed/DEVICE_BOOT_TIMEOUT`，设备隔离；恢复默认参数并正式 rebuild 后 `ready/healthy/0` | `fault-emulator-boot-timeout.log` |
| Host Command lease timeout | 临时使用 `20s/15s`，命令 leased 后强制中断 Agent | 第 1 次租约过期回到 `pending`；默认参数 Agent 接管同一命令，第 2 次 `succeeded` | `fault-command-lease-timeout.log` |
| Docker volume 删除失败 | 用独立容器占用当前真实数据卷后正式 rebuild | 3 次后 `failed/EMULATOR_DELETE_FAILED` 并隔离；解除占用并正式 rebuild 后卷实体重建、设备健康 | `fault-docker-volume-delete.log` |

关键输出：

```text
BOOT_TIMEOUT_COMMAND=failed|3|DEVICE_BOOT_TIMEOUT
BOOT_TIMEOUT_RECOVERED=ready|healthy|0
LEASE_EXPIRED_AND_REQUEUED=pending|1|
LEASE_COMMAND_RECOVERED=succeeded|2|
VOLUME_DELETE_COMMAND=failed|3|EMULATOR_DELETE_FAILED
VOLUME_DELETE_RECOVERED=ready|healthy|0
OPEN_RESERVATIONS=0
RESOURCES=1|1|1
```

## 数据隔离与 50 次稳定循环

`cycles-recovery9.log` 完成连续 50 次真实申请、写标记、release、重建和下一次申请：

- 每轮在 `/data/local/tmp/df021_cycle_marker` 和 `/sdcard/Download/df021_cycle_marker` 写入本次 owner；
- release 后必须观察 `ready/unhealthy/0` STF 稳定窗口，下一条 Reservation 在稳定窗口完成前不得 active；
- 每轮 serial 改变，Docker volume `CreatedAt` 改变；
- 下一台重建设备上两个标记文件均不存在，数据检出率 0；
- 每轮受管容器、网络、卷均为 `1/1/1`；
- 50 次结束后开放 Reservation 为 0，无双占、无永久 reserved/busy/recycling、无持续资源增长。

最终输出：

```text
CYCLE_PASS n=50 ... stabilization=true ... resources=1/1/1
RUN_PASS=2026-08-06T17:08:18Z cycles=50 open_reservations=0 containers=1 networks=1 volumes=1
RUN_FINISHED=2026-08-06T17:08:18Z exit=0
```

部署最终 Agent 二进制后，另外执行 3 次同等真实循环作为配置化 lease/timeout 的回归冒烟，三轮均为 `CYCLE_PASS`，资源始终为 `1/1/1`。由于正式 50 次门槛已由 recovery9 满足，追加长跑在第 3 轮后停止；脚本已经释放 active Reservation，新增 pending Reservation 通过正式 release API 取消，最终清理为：

```text
CYCLE_PASS n=3 ... stabilization=true ... resources=1/1/1
FINAL50_RELEASE=...|pending|..."status":"failed","failure_code":"RESERVATION_CANCELED"...
FINAL50_STOP_CLEAN=ready|healthy|0 open=0 resources=1/1/1
```

Docker volume 删除失败场景还单独证明了同名确定性卷确实被重建，而不是继续复用旧实体：

```text
VOLUME_RECREATED=alcor-df-emulator-...-data|2026-08-06T18:09:58Z|alcor-df-emulator-...-data|2026-08-06T18:13:27Z
```

## 最终结论

DF-021 验收条件已满足：全部指定故障进入明确终态并可恢复；不存在永久 Reservation 或 Host Command 悬挂；重建后上一任务的内部和外部存储标记检出率为 0；真实 50 次循环无双占和资源泄漏。后续 DF-022～DF-024 可以继续按实施顺序进行真实安全、运维和全量验收。

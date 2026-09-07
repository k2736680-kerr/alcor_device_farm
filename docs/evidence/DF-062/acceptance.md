# DF-062 Emulator OOM 幽灵占用与原机恢复验收

## 结论

通过。正式 Android 15-1 的 Emulator 在 7 GiB 容器内存耗尽后出现“容器仍 running、Docker `OOMKilled=true`、QEMU 已退出、ADB offline”的僵尸状态；同时 STF 保留 `present=false / using=true` 的历史占用，导致 Reaper 无法关闭已过期 Reservation，现有非破坏自愈链路也被活动预约阻挡。

修复于 2026-09-07 部署到 `10.0.30.171`。卡住预约已自动过期回收，原设备只执行一次 self-healing restart 后恢复，真实 UiAutomator2 Session 和 `/source` 通过。未删除、重建、重装或补建替代设备。

## 修复范围

- Docker inspect 解析 `State.OOMKilled`，Provider 以 `EMULATOR_OOM_KILLED` 明确报告内存溢出。
- Agent 对运行中但未完全 Ready 的 Android Emulator 上报 `booting/unhealthy`，不再写成 `booting/unknown`。
- Reconciler 检查无 create/rebuild 在途命令的 `booting/unhealthy` 设备，达到既有阈值后隔离并只排队一次原机 restart。
- STF inventory 中设备缺失或 `present=false` 时，403 repeated release 作为幂等终态；`present=true && using=true` 继续返回错误，避免释放他人真实占用。
- Docker Agent 心跳把运行时配置 `docker` 映射为设备域契约值 `docker_emulator`。
- Docker 模式不创建 iOS Session Fence 时，以具体指针判空，避免 typed nil 进入并发组件执行造成 panic。
- 没有新增 API、数据库表、migration、配置或第二套恢复实现。

## 自动化验证

| 检查 | 结果 |
|---|---|
| `go test ./...` | 通过 |
| `go vet ./...` | 通过 |
| STF absent/missing/free/真实占用单元测试 | 通过 |
| Docker OOM inspect 与健康分类测试 | 通过 |
| Agent booting/unhealthy、Provider 类型与 typed nil 回归 | 通过 |
| 真实 PostgreSQL Reconciler 集成测试 | `booting/unhealthy` 隔离、单次 restart、零破坏命令通过；create/rebuild 在途保护通过 |
| migration、Repository、Scheduler、Reaper、Reconciler、Host Command、Metrics 集成包 | 通过 |

完整 PostgreSQL 验收脚本继续运行到管理 API 包；其中既有 `TestManagementAPICompleteMockFlow` 的设备审计绝对计数断言期望 8、实际 9。该测试不经过本次修改的 STF、Docker Provider、Agent 心跳或 Reconciler `RunOnce` 路径，本次没有为满足无关绝对计数而改动管理 API。

## 正式部署

- Git 分支：`codex/device-farm-v2`，直接推送，不创建 PR。
- 最终提交：`559df64`。
- Server 镜像：`alcor-device-farm:559df64-oom-recovery-20260907`。
- Server 与 Host Agent 版本：`0.1.0-559df64`，Go `1.26.5 linux/amd64`。
- Server 二进制 SHA-256：`e79af3dccf542eb39a1b57d88ffee94a048a4ce38e2340ff0186b47f831fe02f`。
- Host Agent 二进制 SHA-256：`168f1d2544098d3253ee4893d9c82d384274ba00adb5d068c65106e9dc131c82`。
- `/healthz` 与 `/readyz` 均返回 200；Agent 心跳持续返回 200。
- 上一稳定 Server `6a08b89` 以停止容器 `alcor-device-farm-server-df017-before-1edc081-20260907` 保留用于回滚；本次失败预检与中间镜像、容器、脚本已清理。

## 正式故障收敛

- 原卡住 Reservation `dcda0612-8b35-4167-b0af-511cc2f64490`：`active -> expired`。
- Android Device `ef26de28-b0d8-4afa-894d-7a8b1d3716ad`：最终 `ready/healthy`，连续失败数 0。
- Provider ref：继续为 `emulator-ef26de28-b0d8-4afa-894d-7a8b1d3716ad`。
- 容器：继续为 `alcor-df-emulator-ef26de28-b0d8-4afa-894d-f437ebb5`。
- 数据卷：继续为 `alcor-df-emulator-ef26de28-b0d8-4afa-894d-f437ebb5-data`，挂载 `/home/androidusr`。
- Pool membership：原 Device 的启用关系仍为 1 条。
- `operation_source=self_healing` 的 restart 为 1 条；self-healing delete/rebuild/create 为 0 条。
- Docker 最终 `running / OOMKilled=false`；容器内 ADB=`device`、`sys.boot_completed=1`。
- STF 当前 serial `10-0-30-171.nip.io:32847` 为 `present=true / ready=true / using=false`。

## 真实 Appium 冒烟与最终状态

通过正式 Android Pool 创建 `test_run` Reservation，建立指定 `emulator-5554` 的 UiAutomator2 Session；`GET /session/{id}/source` 成功返回 24,367 字节。随后删除 Appium Session 并通过正式 Release API 释放 Reservation。

最终开放 Reservation=0、开放 Device Session=0、pending/leased Host Command=0。Server 和 Host Agent 继续运行 `559df64`，没有当前 panic、fatal 或持续心跳失败。全过程未输出或提交 Service Token、Agent Token、STF Token、数据库口令、Console 密码或 SSH 私钥。

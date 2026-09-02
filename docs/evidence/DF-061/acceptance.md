# DF-061 跨入口挂断与过期预约回收验收

## 结论

通过。生产故障不是 Android Worker、STF 配置或设备数量导致，而是预约关闭链路对“设备已提前恢复为 ready”和“同一用户从不同可信入口操作”处理不完整。修复已于 2026-09-02 部署到 `10.0.30.171`，原卡住预约已经自动过期回收，新的 Android 远控申请、连接和明确挂断也完整通过。

## 修复范围

- Release/Reaper 在设备已为 `ready` 时不再执行非法的 `ready -> ready` 领域转换。
- Repository 关闭预约时允许绑定设备已经处于 `ready`，同时仍以受影响行检查确认设备真实存在。
- Alcor 可信服务代当前用户创建的 `owner_type=manual` 预约，可由相同 Console 用户正常释放；`client_id` 继续只作为幂等命名空间，不再错误替代 Owner 身份。
- 非 Owner 的普通释放仍返回 forbidden；Console 管理员释放其他归属的预约时显式提交 `force=true`，沿用既有管理员校验、`force_released` 状态和强制释放审计。
- 未新增 API、状态、表、migration 或配置项；没有修改 Alcor Worker、DaFit、STF、Appium 或 iOS Host 代码。

## 自动化验证

| 检查 | 结果 |
|---|---|
| `go test ./...` | 通过 |
| `go vet ./...` | 通过 |
| `pnpm test -- --run` | 10 个测试文件、56 项测试全部通过 |
| `pnpm build` | OpenAPI Client 生成、TypeScript 编译和 Vite 生产构建通过 |
| `git diff --check` | 通过 |
| 独立 PostgreSQL 16 全 migration 后运行 Release/Reaper 专项集成测试 | 2 项全部通过 |

隔离 PostgreSQL 专项覆盖：

- `TestConsoleOwnerCanReleaseManualReservationCreatedThroughService`：拒绝非 Owner 普通释放，并允许相同 Console Owner 释放由 Service 命名空间创建的预约；
- `TestReaperClosesReservationWhenDeviceIsAlreadyReady`：真实 SQL 下关闭 active Reservation 和 Session，设备保持 `ready/healthy`。

隔离测试容器、测试数据库和展开的源码目录在测试后已删除，未连接或清空正式 PostgreSQL。

## 30.171 正式发布

- 发布代码：`6a08b89b0ac7`（`修复预约挂断和过期回收`）；
- 正式镜像：`alcor-device-farm:6a08b89-reservation-release-20260902`；
- 运行版本：`0.1.0-6a08b89`，Go `1.24.6 linux/amd64`；
- 发布前备份：`/home/kerr/device-farm-backups/device-farm-before-8557dac-20260902-1020.dump`；
- 备份 SHA-256：`d9fc9b02d92994d1d2e7d87dab0f05307a83d7dab173dbcd4bb0df52a4212254`；
- `pg_restore --list` 校验通过；本次无 migration 和配置变更；
- 上一稳定镜像 `8cd498d` 与中间验证镜像 `8557dac` 的停止容器均保留，可立即回滚。

## 生产真实回归

原故障对象：

- Reservation `12ad0948-40e4-4a82-aff1-51289e7aeafe`：`active -> expired`；
- 对应 Device Session：`active -> closed`，`ended_at` 已写入；
- Android Device `ef26de28-b0d8-4afa-894d-7a8b1d3716ad`：保持 `ready/healthy`，没有 rebuild、reimage 或数据卷删除；
- 新 Server 日志不再出现生命周期 `ready -> ready`、`device reservation reaper cycle failed`、panic 或 fatal。

新增 Android 远控冒烟：

- Reservation `5190e704-5b1c-44dd-9750-5d88aec1e321`；
- 真实流程为申请、STF claim、状态 `connected`、明确 DELETE 挂断；
- 最终 Reservation=`released`、Session=`closed`、Device=`ready/healthy`，审计动作为 `release_device_reservation`。

最终运行状态：

- `/healthz`、`/readyz` 和正式 HTTPS `/console/` 均返回 200；
- open Reservation=0、open Session=0、in-flight Host Command=0；
- 当前一台 Android 和两台 iOS 均为 `ready/healthy`；
- STF inventory 中当前 Android `present=true`，远控释放后无预约泄漏；
- iOS SSH tunnel `running`、restart count=0，Server 网络空间内 Baguette `simulators.json` 可达；
- 正式 Server `running`、restart count=0。

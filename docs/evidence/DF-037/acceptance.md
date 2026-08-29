# DF-037 验收记录：Phone 硬件模板和受控模拟器创建向导

## 范围和复用

本任务仅提供 Phone 硬件模板；Tablet、Wear、TV、Automotive、Desktop 与 XR 未进入本期目录或创建入口。开工前已搜索 `D:/AutoTestTools/Projects/Alcor` 与 `D:/AutoTestTools/Projects/dafit_auto_platform`：两者没有可复用的受控 AVD 模板目录和设备创建编排。本实现复用既有 Host Command、Host Agent、Docker Provider、STF Adapter、Appium 健康探针和 Warm Pool 收敛，不复制 DaFit Runner 或 STF 功能。

## 交付与本地验证

- 新增 ADR-0017；OpenAPI 1.8 提供 `GET /api/v1/android-hardware-profiles` 与 `POST /api/v1/device-provisionings`。
- Phone 目录提供 21 条可搜索模板；创建向导选择已验证镜像、目标 Pool 和完整 runtime profile（容器/Guest CPU、内存、数据盘、分辨率、DPI、VM Heap、GPU）。
- Android SDK 目录放宽至稳定 API 26～99 的 `default`、`google_apis`、`google_play` x86_64 条目；未准备项保持不可创建。
- 受控创建在同一事务中锁定 Pool/Host 容量、创建 Device/Pool membership/Host Command 并增加 `total_target`；Idempotency-Key 重试返回同一设备和命令。
- `go test ./...` 通过。
- `scripts/verify-migrations.ps1 -RunRepositoryTests` 通过，包括约束、down、up-down-up、Repository、Scheduler、Reaper、Reconcile、Host Command、Metrics、API 与 Warm Pool 集成测试。
- `console` 的 `pnpm test -- --run` 通过：7 个测试文件、30 个测试；`pnpm build` 通过。

## 真实 Linux KVM 验收

生产数据库变更前已创建可恢复备份；备份 SHA-256：`0f78920d51b5b0ed28ecbc9dcaa0a70c160633fec2cbb3b9fcc6be278dba8cff`。部署后 Server 健康检查和就绪检查均为成功，运行镜像快照：`sha256:ca4f4a53c4b927de269f4b63c64a15bdb88b50c95fab571722928ca819346fe7`。

真实 SDK 同步结果为 21 项、API 范围 26～36；不再只有原先 4 项。同步过程中发现旧数据库遗留的 image type 限制会拒绝 `default` 条目，已补入 DF-037 migration 的 type 约束替换并重新跑过本地 up/down/up 验证。

使用已验证 Android 16 / API 36 镜像创建 Phone 实例：

| 项目 | 实测值 |
| --- | --- |
| 设备 | `105555ce-dfcb-467b-8143-6a5a46a653dd` |
| 创建命令 | `4f31b88a-fae4-46ea-ac6b-79fad9a00a20` |
| Phone 模板 | Pixel 9 |
| 参数 | 4 container CPU、5120 MiB container memory、4 guest CPU、4096 MiB guest memory、1080×2424、420 dpi、512 MiB heap、auto graphics |
| 重试 | 相同 Idempotency-Key 返回同一 Device/Command；Pool `total_target` 从 1 变为 2，仅新增一次 |
| 最终设备状态 | `ready / healthy` |
| ADB | `10-0-30-171.nip.io:32797 device` |
| STF | `present=true, ready=true, using=false` |
| Appium | `ready=true, version=3.5.2` |

Provider 实际容器使用选定的 `EMULATOR_DEVICE=Pixel 9`；创建命令的 capabilities 同时记录 `hardware_profile_id=pixel_9`、`resolution=1080x2424` 和完整 runtime profile。STF 的短暂注册稳定窗口结束后由既有 Reconciler 收敛至 healthy，未跳过 ADB、STF 或 Appium 门禁。

## 回滚

应用可切换回部署前保留的 Server 二进制/镜像；数据如需回退，先停止当前 Server，再从本记录列出的 PostgreSQL 备份恢复。新创建设备通过现有 Device 管理入口受控删除，不直接删库或删除运行容器。

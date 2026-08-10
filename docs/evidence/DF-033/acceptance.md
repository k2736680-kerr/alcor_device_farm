# DF-033 Pool 总目标和默认镜像验收证据

## 结论

DF-033 通过。设备池容量改为 Pool 级的 `total_target / min_ready / max_concurrency`，Android 13～16 只作为可选镜像目录，不再把每个镜像的旧 `max_instances` 相加。自动补建设备只使用 Pool 默认镜像；切换默认镜像不会重装已有设备。缩容只选择没有活动预约、没有在途命令且不被其他 Pool 共享的空闲设备，正在使用的设备不会被删除。

`min_ready=0` 时空闲期间不维持暖机；出现可由默认镜像满足的 pending Reservation 后，Controller 会按需创建，但始终受 `total_target` 和 Host 实际 CPU、内存、Docker 数据盘容量约束。

## 主要实现

- migration `000007_pool_total_target_and_default_image` 为 Pool 增加总目标、最小预热和默认镜像；迁移旧单镜像 Pool 时保留原目标，并只从 `ready` 镜像中选择最高 Android API 作为默认镜像；
- OpenAPI 1.4.0 和 Management Service 校验 `min_ready <= total_target`、`max_concurrency <= total_target`；缩容必须填写原因并写入设备域审计；
- Warm Pool 按整个 Pool 统计已有和在途设备，默认镜像只负责未来新增；资源不足时保留目标，不登记无法启动的 Device/Command；
- Console 的“设备池 → 配置”可直接调整总目标、最小预热、最大并发和默认镜像，并显示当前数量、待补建提示和 Host 实际资源排查入口；
- 默认镜像不能直接停用，必须先切换到另一个已经关联且 `ready` 的镜像；
- 旧 Pool Image 目标接口保留滚动兼容，但非默认镜像不再改变 Pool 总目标。

## 自动化验证

在 Linux Go 1.24.6 容器和隔离 PostgreSQL 16 数据库上，从空库顺序执行 `000001`～`000007` migration 和 `migrations/test/constraints.sql`，结果通过。

随后执行：

```text
go vet ./...                         passed
go test -p 1 -count=1 ./...         passed
pnpm test -- --run                   7 files / 26 tests passed
pnpm build                           passed
```

新增回归覆盖：

- 两个 Controller 并发扩容不超过 Pool 总目标；
- 多个 Image 只使用默认 Image 自动补建，切换默认 Image 后旧 Device/Command 保留，新增使用新 Image；
- `min_ready=0` 且无预约时不创建，出现匹配的 pending Reservation 后只创建一台，并且重复 Controller 周期不重复创建；
- 目标 3 缩到 1 时保留最新空闲设备；最旧设备正在使用时保护该设备并选择其他可删空闲设备；
- 资源只够一台时产生容量 miss，目标配置不被降低；
- Pool API 缩容、默认镜像校验、审计写入和默认镜像停用冲突；
- Console 扩容无需原因，缩容必须原因和二次确认。

## 真实测试环境

目标主机为 `10.0.30.171`。生产数据库在迁移前已保存可恢复备份：

```text
/home/kerr/alcor-device-farm-runtime-20260806/backups/df033-before-migration-20260810.dump
```

生产 `000007` migration 已通过，迁移后的原单镜像 Pool 为：

```text
total_target=1
min_ready=1
max_concurrency=1
default_image=android-16-api36-df017 (API 36)
```

候选 Server 镜像 `alcor-device-farm:df033-candidate-20260810` 已通过 `/readyz`，Console 生产资源包含“总目标数量”配置。现有设备保持 `ready/healthy`，Android 16 Emulator 的 `StartedAt` 仍为 `2026-08-10T05:59:22.468686907Z`，说明 migration 和 Server 切换没有重启或重装模拟器。Host Agent 未重启。

旧 Server 容器保留为 `alcor-device-farm-server-df017-rollback-before-df033`；新增列对旧 Server 向后兼容，需要时可直接回切，数据库备份提供额外恢复路径。

## 边界

- 本步骤完成 Pool 级容量和默认镜像。空闲 Device 的 CPU/内存/镜像受控重装在 DF-034 实施；
- Android 13～16 的正式不可变镜像、统一下载地址和逐版本真实冒烟在 DF-035 实施；当前生产默认镜像仍是已验证的 Android 16；
- 按需创建需要等待 Emulator 启动和 ADB/Appium/STF 就绪，不等同于已有暖机的即时分配；对低延迟任务应把 `min_ready` 设置为至少 1。

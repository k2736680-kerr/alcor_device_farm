# ADR-0005：MVP 使用单一逻辑设备池和固定两台设备

## 状态

已被 ADR-0006 取代。

## 背景

首期实际容量最多为两台 Docker Android Emulator，后续主要增加 USB 真机。原方案同时设计了逻辑设备池和 `min_ready/max_instances` 自动 warm pool Controller。对于两台固定设备，自动创建、自动补齐、缩容和退避控制会增加运维与故障面，但不会提升当前可用性。

现有逻辑设备池已经进入 PostgreSQL、OpenAPI、Reservation、Scheduler 和并发测试：Reservation 必须指向 `pool_id`，Scheduler 使用 Pool 的启停状态、最大并发和成员关系筛选设备。直接删除 Pool 会改动稳定契约并削弱并发和资源隔离能力。

## 决策

- 保留逻辑 `device_pools`、`device_pool_devices`、Reservation `pool_id` 和现有 Pool API；
- MVP 只配置一个默认 Android 设备池，`max_concurrency=2`；
- 两台是部署默认值，不写死在 Provider、数据库约束或 Scheduler 代码中；以后增加固定设备只调整 Host/Pool 容量和成员关系；
- 两台 Emulator 由管理员显式创建、验证并加入默认池，不实现按 `min_ready` 自动创建或按空闲时间自动删除；
- `device_pool_images`、`min_ready`、`max_instances` 表结构保留，MVP 使用 `min_ready=0`、`max_instances=2`，作为未来扩容兼容字段，不启动 warm pool Controller；
- DF-016 保留镜像 digest 和启动验证，但不建设自动补池、预测扩容和缩容；
- 第三个并发预约保持 pending/capacity unavailable，不临时创建第三台 Emulator；
- 后续真机可加入现有默认池；需要区分模拟器和真机时只新增一个逻辑真机池并设置成员关系，不修改 Device、Reservation、Scheduler 或 Provider 主架构；
- 未来设备数量明显增加并有稳定 Linux Host 后，再通过独立 ADR 启用 warm pool Controller。

## 后果

- 已完成的 migration、OpenAPI、Scheduler、Reservation 和测试无需回退；
- MVP 部署和运维只管理最多两台明确设备，避免命令风暴和自动扩容误操作；
- 逻辑 Pool 仍提供最大并发、默认/最大租期、停用和设备归属；
- 真机接入继续复用统一 Device/Provider 模型，不需要推倒现有架构；
- `device_pool_images` 暂时没有运行时 Controller 消费，不能把表存在误认为自动补池已启用。

# ADR-0006：使用参数化固定目标自动补齐模拟器

## 状态

已确定。

## 背景

MVP 实际最多运行两台 Docker Android Emulator，但希望部署人员只修改参数，不手工逐台创建、验证和加入设备池。数据库已经具有 `device_pool_images.min_ready/max_instances`，Host Agent 已具有命令领取框架，适合实现固定目标自动补齐。

ADR-0005 原决定由管理员显式创建两台设备。该方式虽然简单，但设备删除、隔离或服务器重启后仍需要人工恢复，因此由本 ADR 取代。

## 决策

- 保留逻辑 Pool、Reservation `pool_id` 和现有 Scheduler；
- DF-016 实现固定目标补齐 Controller：当可用及正在创建的 Emulator 少于 `min_ready` 时自动创建，且任何时候不得超过 `max_instances`；
- MVP 默认 `min_ready=2`、`max_instances=2`，数值来自数据库或配置，不写死在 Provider、Scheduler 或数据库约束中；
- 自动创建必须走 PostgreSQL Host Command → Host Agent → Docker Provider，Server 不能调用 Mock Provider 冒充真实设备，也不能访问 Docker Socket；
- Controller 在一个数据库事务内锁定对应 `device_pool_images` 行、重新统计容量并登记 provisioning Device/Command，避免多个 Server 同时超额创建；
- Agent 的创建结果和后续心跳更新同一 Device；只有容器 running、ADB online、boot completed、Appium healthy 后才进入 ready；
- Device 创建时同步建立 Pool membership，不允许“容器创建成功但未加入池”成为无主资源；任何中途失败必须补偿或进入明确的 failed/quarantined 状态；
- 失败使用有上限的指数退避，避免 Docker/KVM 故障时形成 Host Command 风暴；
- Controller 只自动向上补齐，不主动删除设备。降低 `min_ready` 后停止补充，已有设备通过 drain/人工删除安全缩容；
- `min_ready/max_instances` 只适用于 Docker Emulator Image。USB 真机由 Agent 发现并显式加入池，不能自动“创建”真机；
- 不做负载预测、跨地域弹性和 Kubernetes 调度。

## 后果

- 日常部署只需设置目标数量，默认自动维持两台 Emulator；
- 未来增加到 N 台固定模拟器只调整 Host `device_slots`、Pool `max_concurrency` 和 Image `min_ready/max_instances`，不修改架构或 migration；
- 自动补齐必须等待 DF-014 真实 Docker Provider 和 DF-015 Appium 健康链路验收后才能标记 DF-016 完成；
- 本地可以先做数据库锁、命令编排和 Mock Adapter 测试，但这些测试不能替代 Linux KVM 双设备验收；
- 真机继续复用 Device、Reservation、Scheduler 和 Pool，创建方式与 Emulator 明确分离。

# ADR-0008：当前测试环境使用单台模拟器验收

## 状态

已确定。

## 背景

当前 Linux KVM 服务器用于开发验收，不是最终真机设备农场。Android 16 模拟器实际运行接近 4 GiB 内存；同时启动两台会增加服务器压力，也没有必要用真实资源重复证明已经由 PostgreSQL 并发测试覆盖的调度正确性。后续主要接入 USB 真机，也可能按需增加更多模拟器。

## 决策

- 当前默认池使用 `min_ready=1`、`max_instances=1`、`max_concurrency=1`；
- 单台 Android 16 模拟器容器使用 `5g` 内存和 `4` 核 CPU 上限；这些值是上限，不是启动时预占。真实验收表明 4 GiB/2 核会在 APK 安装期间出现 Android package/settings 服务不稳定；
- DF-014、DF-015 和 DF-016 的当前 Linux KVM P0 验收以一台模拟器完成真实创建、ADB、boot、Appium、自动入池、预约、释放、重建和清理；
- 第二个并发预约必须保持 pending 或返回可重试的容量不足，且不能突破 `max_instances=1`；
- 双占、两个 Controller/Scheduler/Reaper 并发、目标从 1 调到 N 和多设备端口隔离继续由 PostgreSQL 集成测试、Provider 测试和可选多设备环境覆盖；
- 数量只存在于配置和数据库，不写死进 Provider、Scheduler、Reservation、Device 或 migration；
- 以后增加模拟器只调整 Host `device_slots`、Pool `max_concurrency` 和 Image `min_ready/max_instances`；接入真机只增加 USB Provider 和池成员，不修改上层架构。

## 后果

- 当前服务器只需要稳定承载一台 Android 16 模拟器，降低内存和 CPU 压力；
- 自动创建、自动补齐和失败退避仍保留，不退回手工建机；
- 当前真实验收不再因缺少第二台模拟器而阻塞；
- 扩容能力和真机扩展边界保持不变，后续不需要推倒重写。

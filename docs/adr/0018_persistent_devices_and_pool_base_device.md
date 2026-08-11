# ADR-0018：长期设备与 Pool 基础设备

## 决策

Device Farm 的 Emulator 是用户可长期维护的设备。Reservation release 仅释放 Reservation、Session 和 STF claim，Device 从 `busy` 直接回到 `ready/healthy`，不自动恢复出厂、不清理数据卷。管理员显式 rebuild/reimage 仍是恢复出厂操作；delete 则永久清理 Provider 资源并保留 Device 审计历史。

每个 Pool 可选一个 ready/healthy Phone Emulator 作为 `base_device_id`。Warm Pool 后续扩容复制该 Device 当前已生效的 Android Image、Phone Profile 和 runtime profile，创建全新的干净 Emulator 数据卷；不复制 APK、账号、缓存、文件或其他 Device 状态。基础设备被成功重装后，未来扩容自动跟随其最新配置。

## 后果

Pool 默认 Image 保留为历史兼容和无基础设备迁移兜底，但新 Console 不再让用户直接配置它。直接删除空闲 Device 时同步降低其 Pool 的目标数，避免用户删除后被自动补建。删除基础设备前必须先更换基础设备。该决策只涉及设备域，不新增 Alcor 业务对象，也不重写 STF/Appium/DaFit 能力。

Android Studio 式创建由持久化 `device_provisioning_jobs` 编排：Console 只提交 `catalog_id`、Phone Profile、Pool 和 runtime profile；Server 复用或排队既有镜像准备，准备验证完成后再写入既有 Device/Host Command 链路。刷新或幂等重试只恢复同一 job，绝不由浏览器重复下载、重复创建设备或重复增加 Pool 目标。

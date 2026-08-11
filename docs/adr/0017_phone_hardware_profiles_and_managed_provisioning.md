# ADR-0017：首期仅提供 Phone 硬件模板和受控模拟器创建

## 状态

已确定。

## 决策

Device Farm Console 在 Device 页面提供 Android Studio 风格的创建向导：先从多条 Phone 硬件模板中搜索并选择，再选择 Server 同步的官方 Android System Image、目标 Pool 和完整 runtime profile。首期仅暴露 Phone；Tablet、Wear、TV、Automotive、Desktop、XR 全部不在接口和页面中出现。

提交创建不会由浏览器或 Server 启动 Docker。Server 在一个 PostgreSQL 事务中锁定容量、创建 provisioning Device、Pool membership 和 `create` Host Command，并增加 Pool 的 `total_target`；Host Agent 继续经 Docker Emulator Provider 创建。只有已有 ADB、STF、Appium 健康链路成功后，Controller 才将设备收敛为 ready/healthy。

硬件模板映射为 Android SDK/`avdmanager` 的受控 Profile 名称；它是设备运行参数，不是 Android System Image 的官方属性。System Image 未完成准备时只能显示状态，不能绕过 digest/验证门禁创建。

## 后果

- 用户能从 Device 页直接创建一台加入所选 Pool 的 Phone Emulator，而不必先在 Image 页填写 CPU/内存；
- CPU、内存、磁盘、分辨率、DPI、VM Heap、图形模式归属 Device runtime profile；
- 继续复用 Host Command、Host Agent、Docker Provider、容量预检、STF 和 Appium，不产生第二套 Emulator 或执行器；
- 后续增加其他 form factor 时必须单独扩展目录、兼容矩阵和验收，不能在 Phone 实现中暗中放开。

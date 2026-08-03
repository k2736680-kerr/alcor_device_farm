# ADR-0001：设备农场使用独立开发工作区，DaFit 通过适配联调

## 状态

已确定。

## 决策

`alcor_device_farm` 使用新的 Git 仓库和 `master` 分支，从设备域边界开始建设，不从 `dafit_auto_platform` 创建分支或复制仓库。该仓库是 Alcor 接口尚未完成期间的开发与验证工作区，不是对最终系统拓扑的重新设计。

`dafit_auto_platform` 保持原仓库和 `main` 分支。进入真实联调阶段时，从其 `main` 创建 `feature/device-farm-adapter`，仅增加外部指定设备、远程 Appium、独立报告目录和标准退出信息，不改变现有页面、场景、断言和本地运行链路。

## 原因

- 两个项目职责不同：一个管理设备资源，一个实现 DaFit 业务测试；
- 从 DaFit 分支建立设备农场会继承大量无关业务代码；
- 直接复制会形成 Appium、ADB、结果和报告的双重实现；
- 薄适配可以立即用于联调，也能被未来 Alcor Worker 复用；
- 兼容确定方案的北向 API 使设备域可以在 Alcor 接口尚未完成时先行验证。

## 后果

- 设备农场缺失能力从零实现，但严格限制为设备域；
- DaFit 已验证的自动化能力继续复用，不在新仓库重写；
- 当前可通过 API 和外部 Owner ID 独立联调；
- 新版 Alcor 接口具备后，由独立 Worker 的 Device Farm Adapter 调用本项目北向 API，并使用 RunAttempt UUID/ULID 作为预约 Owner ID；
- 是否在更后阶段调整代码仓库或部署边界，以新版 Alcor 的 Device Farm 专项设计为准；当前不以旧版目录结构提前锁死。

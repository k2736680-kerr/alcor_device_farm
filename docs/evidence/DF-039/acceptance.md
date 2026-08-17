# DF-039 第二版多平台宿主机与 iOS 接入设计验收

## 当前状态

`pending`

DF-039 尚未开始实现。本文件先固定开工边界：本任务只允许开展 Appium Device Farm、Appium 3、XCUITest、WebDriverAgent、go-ios、macOS/Xcode、iOS 真机与 Simulator 的复用调查和架构设计。在复用矩阵、架构对齐、专项 ADR、实施拆分及真实验收环境全部明确以前，不修改设备 API、数据库 migration、领域状态机、Host Agent 或 Provider 代码。

## 必须保存的证据

- Android 第一版归档 commit、Tag 和恢复验证；
- Alcor、DaFit 与当前设备农场的复用搜索结果；
- Appium Device Farm 固定版本、官方能力、已知限制和许可证核对；
- macOS/Xcode、证书、Provisioning Profile、WDA 和测试设备环境盘点；
- 我方 Reservation 与宿主机 Session 路由之间的唯一占用时序；
- Android 回归范围、iOS 真机/Simulator 真实验收矩阵和回滚方案。

验收完成前，本任务保持 `pending`。

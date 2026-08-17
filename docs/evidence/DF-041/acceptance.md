# DF-041 macOS Host Agent 与 Appium Device Farm Adapter 验收

## 当前状态

`pending`

验收入口：E4 macOS Host、固定版本工具链、插件 inventory/busy/health Adapter、Host readiness、未知设备隔离、Secret 扫描和 Android Agent 回归。没有真实 macOS Host 时不得 completed。

必须保存 Host 脱敏规格、macOS/Xcode build、Node/Appium/插件/XCUITest/WDA/go-ios 锁定版本、Appium doctor、inventory 加入/移除、Node 故障和恢复时间线。未知设备不得自动 ready，插件数据库不得导入为业务表，任何 Apple Secret 不得进入证据。

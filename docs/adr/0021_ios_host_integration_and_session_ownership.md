# ADR-0021：iOS 使用宿主机侧 Appium Device Farm，设备占用继续由现有内核负责

## 状态

已接受。

## 背景

Android 第一版已经归档，第二版需要接入 iOS 真机和 Simulator。Appium Device Farm 12.0.1 能发现 Android、iOS/tvOS 真机和模拟设备，并提供 Appium Session 分配、Dashboard 及 Hub/Node 能力；它也维护自己的设备 busy 状态和数据库。如果直接把它作为新的设备池，会与本项目现有 PostgreSQL Scheduler、Pool、Reservation、Lease、Reaper 和审计形成两套占用真相。

Appium Device Farm 12.x 已明确删除人工设备控制和实时串流。当前 Appium XCUITest Driver 12.4.0 的完整支持路径仍是 macOS + Xcode；Windows/Linux 只对 iOS/tvOS 18+ 真机提供受限支持，要求明确 UDID、RemoteXPC tunnel，并使用预装 WDA 或外部 WDA。

## 决策

1. 现有 Device Farm Server、PostgreSQL Scheduler、Pool、Reservation、Lease、Reaper、Device Session 和设备域审计继续是唯一资源分配真相；不采用 Appium Device Farm 的 Team Allocation、人工 block/unblock 或 Dashboard 作为业务占用入口。
2. 首期 iOS Host 必须是专用 macOS Host。Host Agent 在该主机上管理固定版本的 Appium、XCUITest Driver、Appium Device Farm、go-ios 和本地 Xcode 工具链；iOS Simulator 只在 macOS 上运行。
3. Appium Device Farm 只作为每台 macOS Host 内部的设备发现、技术 busy 互斥和 Session 路由组件。首期不启用它的跨主机 Hub 分配；多台 macOS Host 仍由我方 Scheduler 选择明确 Device 和 Host Endpoint。
4. iOS Appium Endpoint 不直接暴露给浏览器或任意客户端。可信执行器只能在持有 active Reservation 时创建 Session，并同时提交由服务端生成的单元素 `df:udids=<reserved_udid>` 与 `appium:udid=<reserved_udid>`。两个值缺失、不一致或不属于该 Reservation 时必须拒绝。
5. Appium Device Farm 的 `busy` 只是宿主机技术锁，不回写覆盖 PostgreSQL 状态。出现“PostgreSQL 已预约但插件 busy”“插件有 Session 但没有 active Reservation”或 Session/UDID 不一致时，停止新分配并将设备标记 degraded/quarantined，由 Reconciler 和审计流程收敛。
6. 共享 Appium Node Endpoint 可以对应多台 iOS Device，不能继续要求 `devices.appium_endpoint` 全局唯一；UDID/serial、Provider identity 和 active Reservation 仍必须唯一。实际字段和索引变化由 DF-040 migration 明确实现。
7. 首期支持两类固定库存：已经创建并允许 Agent 管理的 booted iOS Simulator，以及已配对、信任、启用 Developer Mode/UI Automation 并完成 WDA 签名准备的 iOS 真机。首期不自动创建任意 Simulator Runtime，不管理 IPA/App Build，也不自动注册未知 USB 设备为可调度设备。
8. WDA、XCUITest 和 WebDriver 协议完全复用 Appium 上游。设备农场只管理版本、签名就绪状态、Endpoint、端口、健康和预约绑定，不实现页面动作、断言、用例 Runner 或业务报告。
9. WDA 证书私钥、Apple Account、Provisioning Profile 和签名口令只存在于 macOS Host 的受控 Keychain/部署 Secret。Server、Console、普通日志、设备连接快照和 Appium Device Farm Dashboard 均不得保存或返回这些秘密。
10. iOS App、Build、Case、Run、RunAttempt、Result 和 Artifact 继续属于 Alcor/对应 iOS Executor。设备农场只保存 Device、Host、Pool、Reservation、Device Session 和经过脱敏的技术连接状态。
11. Android 路径不迁移到 Appium Device Farm：Android 继续使用现有 Docker Emulator、独立 Appium Endpoint 和 STF 远控，避免为统一外观破坏已经验收的第一版链路。
12. iOS 首期不提供浏览器人工远控。Appium Device Farm 12.x 的 Dashboard 只能作为受限运维诊断候选，不能冒充 STF；如未来需要 iOS 人工调试，必须另立 ADR，使用独占 Reservation 且不得与自动化 WDA Session 竞争。
13. Windows/Linux iOS 真机模式、tvOS、无线设备、Appium Device Farm Hub/Node 跨主机分配和自动 Simulator Runtime 生命周期均延期，只有独立真实验收后才能进入生产范围。

## 后果

- 可以复用 Appium Device Farm 的 iOS 发现与 Session 路由，同时不引入第二套业务设备池；
- 首期需要增加平台中立字段、macOS Agent 运行方式、iOS inventory/health Adapter 和 Reservation 绑定的 Session 防护；
- 共享 Endpoint、签名过期、WDA/RemoteXPC 状态和插件内部 busy 漂移会成为新的必须监控的故障类型；
- iOS 交付必须通过真实 macOS Simulator 和真机验收，Windows Mock 或 Android 回归不能替代；
- 任何 iOS 执行器开发都在 Alcor 或独立执行器仓库完成，本仓库不扩展为第二套 App 自动化平台。

## 参考版本与依据

- Appium 3.6.0：<https://github.com/appium/appium/releases/tag/appium%403.6.0>
- Appium Device Farm 12.0.1：<https://github.com/AppiumTestDistribution/appium-device-farm/releases/tag/v12.0.1>
- Appium Device Farm 12.x 人工串流移除说明：<https://github.com/AppiumTestDistribution/appium-device-farm/blob/v12.0.1/README.md>
- XCUITest Driver 12.4.0：<https://github.com/appium/appium-xcuitest-driver/releases/tag/v12.4.0>
- XCUITest System Requirements 与非 macOS 限制：<https://appium.github.io/appium-xcuitest-driver/latest/getting-started/system-requirements/>、<https://appium.github.io/appium-xcuitest-driver/latest/guides/non-macos-hosts/>
- go-ios 1.3.2：<https://github.com/danielpaulus/go-ios/releases/tag/v1.3.2>

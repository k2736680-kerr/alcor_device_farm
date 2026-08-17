# DF-039 官方来源与复用盘点

## 1. 核对时间与代码基线

- 核对日期：2026-08-17；
- Device Farm 基线：`2497c47`，分支 `codex/device-farm-v2`；
- Android V1：`106e9dd`，Tag `archive/android-baseline-2026-08-17`；
- 核对仓库：`alcor_device_farm`、`E:/AutoTestTools/Projects/Alcor`、`E:/AutoTestTools/Projects/dafit_auto_platform`。

## 2. 上游固定版本

通过 npm Registry、GitHub Release、版本化 README/CHANGELOG 和 XCUITest Driver 文档核对：

| 组件 | 版本 | 发布日期 | 许可证/约束 |
|---|---:|---|---|
| Node.js | 22.23.2 LTS | 2026-07-28 | Appium 3.6.0 支持 `^22.12.0` |
| Appium | 3.6.0 | 2026-07-25 | Apache-2.0 |
| Appium Device Farm | 12.0.1 | 2026-07-25 | Apache-2.0，peer Appium `^3.0.0` |
| XCUITest Driver | 12.4.0 | 2026-08-17 | Apache-2.0，peer Appium 3 |
| WebDriverAgent | 16.2.0 候选锁定 | 2026-08-13 | XCUITest 12.4.0 依赖 `^16.1.4` |
| appium-ios-remotexpc | 5.14.4 候选锁定 | 2026-08-12 | XCUITest 12.4.0 可选依赖 `^5.13.2` |
| go-ios | 1.3.2 | 2026-08-11 | MIT |

实现环境必须生成 lockfile/安装清单并保存实际解析版本与校验值，不允许运行时安装 `latest`。

## 3. 官方能力结论

1. Appium Device Farm 12.0.1 支持 Android、iOS/tvOS 真机、Emulator/Simulator 的发现和自动化 Session；`df:udids` 可以把候选限制为指定 UDID 集合。
2. 插件会自行把 Session 设备标为 busy，并维护 Dashboard/数据库/Team Allocation，因此只能把 busy 当技术锁，不能把插件变成第二套预约真相。
3. 12.0.0 起 Dashboard 已删除人工设备控制和 live streaming；README 明确 WDA screenshot streaming 会与自动化 WDA 竞争并导致不稳定。
4. XCUITest Driver 的完整路径是 macOS + Xcode。Simulator 只能运行在 macOS。
5. Windows/Linux 只有限支持 iOS/tvOS 18+ 真机，必须明确 `appium:udid` 和 `appium:platformVersion`，并使用 RemoteXPC + 预装 WDA 或外部 WDA；不进入首期。
6. 真机必须处理配对/信任、Developer Mode、UI Automation、WDA Provisioning Profile 和目标 iOS/Xcode 兼容性。

## 4. 三仓库复用结论

### alcor_device_farm

可直接复用 PostgreSQL Scheduler、Pool、Reservation、Lease、Reaper、Reconciler、Device Session、Host Command、Agent 协议、Appium Endpoint 健康、审计和 Console。当前 Provider `Health`、`ConnectionInfo`、Host/Device check constraint 和 Endpoint 唯一索引仍偏 Android，需要 DF-040 原位扩展，不能复制一套 iOS 控制面。

### Alcor

本地 `feature/ADQ-431-app-automation-device-farm` 已有 Android DaFit Executor、Device Farm Adapter、RunAttempt/Artifact 和统一设备入口。没有 XCUITest/iOS Executor。后续 iOS Executor 应复用其 Reserve/WaitActive/Extend/Release、RunAttempt、ArtifactStore 和错误映射，但在 Alcor 仓库单独设计，不迁入设备农场。

### dafit_auto_platform

已有 Android Appium Session、页面对象、动作、断言、证据、Runner 和报告；Farm 模式依赖 `ANDROID_UDID`/`APPIUM_SERVER`。没有 iOS/XCUITest/WDA 实现。DaFit 继续只作 Android 执行复用来源，不复制也不强行扩展成 iOS Runner。

## 5. 官方来源

- <https://registry.npmjs.org/appium>
- <https://registry.npmjs.org/appium-device-farm>
- <https://registry.npmjs.org/appium-xcuitest-driver>
- <https://github.com/AppiumTestDistribution/appium-device-farm/blob/v12.0.1/README.md>
- <https://github.com/AppiumTestDistribution/appium-device-farm/blob/v12.0.1/CHANGELOG.md>
- <https://devicefarm.org/capabilities/>
- <https://devicefarm.org/architecture/>
- <https://appium.github.io/appium-xcuitest-driver/latest/getting-started/system-requirements/>
- <https://appium.github.io/appium-xcuitest-driver/latest/getting-started/device-setup/>
- <https://appium.github.io/appium-xcuitest-driver/latest/guides/non-macos-hosts/>
- <https://appium.github.io/appium-xcuitest-driver/latest/guides/remotexpc-tunnels-real-devices/>

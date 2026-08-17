# 第二版 iOS 设备农场功能设计

## 1. 文档状态与目标

本文是 DF-039 的 iOS 实施基线。它在 Android 第一版之上增加多平台设备域设计，不修改 Alcor/DaFit 业务边界，也不代表当前生产代码已经支持 iOS。

首期目标是让现有设备农场内核管理专用 macOS Host 上的 iOS Simulator 和真机，并向可信 iOS Executor 返回与 active Reservation 唯一绑定的 Appium XCUITest 连接。首期不建设 iOS 业务执行器、不管理 IPA、不提供人工远控、不把 Android 迁入 Appium Device Farm。

## 2. 总体结论

```text
Alcor / iOS Executor
        |
        | Reserve / Extend / Release（唯一业务占用）
        v
Device Farm Server + PostgreSQL
Scheduler / Pool / Reservation / Lease / Reaper / Audit
        |
        | 明确 Host + Device + UDID + Session Grant
        v
macOS Host Agent
Inventory Adapter / Health Probe / Session Fence
        |
        | 单元素 df:udids + appium:udid
        v
Appium 3 + Device Farm 12 + XCUITest + WDA
        |
        +-- iOS Simulator（simctl/Xcode）
        +-- iOS 真机（配对、签名、Developer Mode）
```

多台 macOS Host 不使用 Appium Device Farm Hub 再次选机，而是分别上报自己的固定库存。我方 Scheduler 先选中设备，Session 请求再路由到该设备所属 Host 的 Appium Node。

## 3. 组件职责

| 组件 | 负责 | 不负责 |
|---|---|---|
| Device Farm Server | Host/Device/Pool、预约、租约、唯一占用、审计和北向契约 | Xcode、WDA 构建、IPA、页面动作和结果 |
| PostgreSQL | active Reservation、Device Session、连接快照和状态收敛真相 | 保存 Apple 私钥或 Appium Device Farm 业务分配 |
| macOS Host Agent | 心跳、受控命令、inventory、健康、Endpoint 和本机 Secret 引用 | Alcor Run、Case、Result、评分和报告 |
| Appium Device Farm | 宿主机内 iOS 发现、技术 busy、明确 UDID 的 Session 路由 | 业务设备池、人工预约、跨 Host 调度、浏览器远控 |
| XCUITest Driver/WDA | WebDriver Session、XCTest 通信和设备自动化 | 设备农场预约、业务用例和报告 |
| go-ios | 真机发现/诊断和插件所需的受控技术连接 | 独立设备池、业务执行或签名 Secret 存储 |
| Xcode/simctl/devicectl | Simulator 和 Apple 官方设备工具链 | 由 Server 或浏览器直接调用 |
| Alcor iOS Executor | IPA/Build、Session 客户端、Case 执行、Result/Artifact | 修改设备农场数据库或自行挑选其他 UDID |
| STF | 继续服务 Android 原生远控 | iOS 设备、iOS Session 或跨平台占用真相 |

## 4. 版本基线

设计核对日期为 2026-08-17。实现时使用 lockfile、校验和或不可变安装包固定以下版本；升级必须单独回归，不使用浮动 `latest`。

| 组件 | DF-039 基线 | 说明 |
|---|---:|---|
| Node.js | 22.23.2 LTS | 满足 Appium 3 的 `^22.12.0` 要求；npm 10.9.8 |
| Appium | 3.6.0 | Apache-2.0 |
| Appium Device Farm | 12.0.1 | Apache-2.0；要求 Appium 3；人工控制/实时串流已移除 |
| XCUITest Driver | 12.4.0 | Apache-2.0；要求 Appium 3 |
| WebDriverAgent | 16.2.0 锁定候选 | XCUITest 12.4.0 依赖范围为 `^16.1.4`，实施 lockfile 固定实际解析版本 |
| appium-ios-remotexpc | 5.14.4 锁定候选 | XCUITest 12.4.0 可选依赖范围为 `^5.13.2`；首期 macOS 仍需针对目标 iOS 验证 |
| go-ios | 1.3.2 | MIT；只作为宿主机工具，不由 Server 下载执行任意版本 |
| Xcode/macOS | 按目标 iOS 矩阵固定 | iOS 26 需要 Xcode >=26、macOS >=15.6；iOS 27 需要 Xcode >=27、macOS >=26.4；以真实设备版本选择 |

Appium Device Farm 网站仍存在 Appium 2.4 和 streaming 的旧页面，不能作为 12.x 行为依据；版本化 README、CHANGELOG、npm peer dependency 和 XCUITest 版本文档优先。

## 5. 首期范围

### 5.1 纳入

- 一个或多个专用 macOS Host 的注册、心跳、排空和维护；
- 固定库存的 booted iOS Simulator 发现、健康和预约；
- 已配对真机的发现、信任/Developer Mode/UI Automation/WDA 签名就绪检查；
- iOS Pool、能力筛选和单设备 active Reservation；
- 共享 Appium Node Endpoint 与明确 UDID 的连接快照；
- Session 创建前的 Reservation/UDID 防护、插件 busy 漂移检测和失败隔离；
- Console 中的 iOS Host、Device、Pool、预约、签名就绪状态和审计；
- Android 第一版全链路回归。

### 5.2 延期

- 自动下载 Xcode、Simulator Runtime 或任意 IPA；
- 克隆 Simulator、Erase All Content、真机恢复出厂和大规模动态扩缩容；
- Windows/Linux Host 运行 iOS 真机 Session；
- Appium Device Farm Hub/Node 跨主机自动分配；
- tvOS、无线 iOS、浏览器人工远控、WDA 视频串流和 Appium Inspector 托管；
- Alcor iOS Case/Runner/Result 页面与执行实现。

## 6. 平台中立模型

DF-040 应通过 migration 和 OpenAPI 明确以下概念，不能继续只靠自由 JSON 猜测平台：

| 资源 | 计划字段/约束 | 兼容策略 |
|---|---|---|
| Device Host | `host_os=linux|macos|windows`、`host_arch`、工具链版本和平台能力 | 现有 Android Host 回填 `linux`，旧 `host_type` 在迁移期保留 |
| Device Pool | `platform=android|ios`，首期禁止混合平台 Pool | 现有 Pool 回填 `android` |
| Device | `platform=android|ios`；`device_kind=emulator|simulator|physical`；新增 `appium_device_farm_ios` Provider | 现有 Device 回填 `android`，Android 值和 API 行为不变 |
| Connection | `appium_endpoint` 允许同 Host 多设备共享；UDID/serial 和 Provider identity 仍唯一 | 删除 Endpoint 全局唯一约束，改为普通查询索引 |
| Capabilities | 规范化 `platformName`、`platformVersion`、`automationName`、`deviceClass`、`realDevice`、`model` | 未知字段只作为非权威扩展，不参与未登记匹配 |
| Device Session | 保存 Reservation、Device、Host、平台、Endpoint、UDID、插件 Node ID、Appium Session ID 和时间 | 不保存 Cookie、Token、证书、私钥、Apple Account 或 IPA 路径 |

Pool 必须是单平台；调度先匹配 Pool.platform，再匹配 Device.platform 和白名单能力。大小写差异只能在 API 边界规范化，数据库保存标准小写平台值。

## 7. Host 与设备发现

### 7.1 macOS Host

Host Agent 心跳至少上报：macOS 版本、架构、Xcode build、可用 iOS Runtime、Node/Appium/Plugin/XCUITest/go-ios 版本、Appium Endpoint、端口范围、Simulator 容量、连接真机数量和签名就绪摘要。版本不匹配、Xcode license 未接受、Appium doctor 失败或 Endpoint 不健康时 Host 不可接收新预约。

### 7.2 Simulator

Appium Device Farm/simctl 只发现管理员预先允许的 Simulator。首期只纳入 `Booted` 且 UDID 在 Host allowlist 中的实例；发现新 Simulator 时先登记为 unknown/quarantined，管理员加入 Pool 并通过健康检查后才能 ready。Simulator 的 Runtime、机型和 UDID 都进入能力快照。

### 7.3 真机

真机必须满足：稳定 UDID、已信任 Host、Developer Mode、UI Automation、目标 iOS/Xcode 兼容、有效 WDA 签名、Developer Disk Image/RemoteXPC 所需服务可用。未知 USB 设备只产生脱敏 discovery 事件，不自动加入 Pool。

## 8. 平台中立健康模型

Host Agent 后续用组件探针代替 Android 专用布尔值作为内部真相：

| 探针 | Android 映射 | iOS Simulator | iOS 真机 |
|---|---|---|---|
| `transport` | ADB online | simctl 可见且 Booted | paired/trusted、usbmux/设备服务可达 |
| `os_ready` | boot completed | SpringBoard/Simulator ready | Developer Mode/UI Automation 就绪 |
| `automation` | UiAutomator2/Appium 冒烟 | XCUITest/WDA 冒烟 | 已签名 WDA 可启动并返回 status |
| `router` | 独立 Endpoint healthy | Appium DF Node healthy、UDID inventory 一致 | 同左，必要 tunnel 就绪 |
| `remote_control` | STF 可见性 | `unsupported` | `unsupported` |

只有必需探针全部通过才能 `ready/healthy`。`unsupported` 不是失败；签名临近过期为 degraded，过期或 UDID 漂移为 unhealthy/quarantined。

## 9. 预约与 Session 防双分配

### 9.1 正常时序

1. Executor 使用 `platformName=iOS` 和 Pool ID 创建 Reservation；
2. Scheduler 在 PostgreSQL 行锁和 active-device 唯一索引下选中一台 ready iOS Device；
3. Device Session 固化 Host、UDID、平台、Appium Endpoint 和插件 Node identity；
4. 服务端签发短时、单次、只绑定该 Reservation/Device/UDID/Endpoint 的 Session Grant；
5. 可信 Session Fence 校验 Reservation 仍 active，拒绝调用方自选设备；
6. 发送给 Appium Device Farm 的 capabilities 必须同时包含完全相同的 `appium:udid` 和单元素 `df:udids`；
7. 插件把目标设备标为 busy，XCUITest 创建 WDA Session；成功后记录 Appium Session ID，Device 从 reserved 进入 busy；
8. WebDriver 后续请求只能沿已绑定 Session 路由；Session 删除后清除技术 busy；
9. Executor 释放 Reservation；Reaper 负责异常超时，健康设备回到 ready。

### 9.2 强制不变式

- 没有 active Reservation 就不能创建 iOS Session；
- 一个 Session Grant 只能消费一次，不能延长 Reservation；
- `appium:udid`、`df:udids`、连接快照 UDID 和 Device.serial 必须四者相同；
- 客户端提交的 `df:tags`、`filterByHost`、多 UDID 或自动选机能力必须被拒绝或覆盖；
- Appium Node 只允许来自 Session Fence/受信 Worker 网络的流量，Dashboard 不提供人工 block/unblock；
- 插件 busy 不能让 PostgreSQL Reservation 自动变 active，也不能延长租约；
- 同一 Device 同时只能有一个 active Reservation 和一个 active Appium Session 绑定。

### 9.3 漂移处理

| 漂移 | 处理 |
|---|---|
| Reservation active，插件报告目标 busy 且 Session 不匹配 | 创建失败，Device degraded；查询绑定后隔离或等待旧 Session 清理 |
| 插件存在 Session，PostgreSQL 无 active Reservation | 终止技术 Session、记录高优先级审计、Device quarantined |
| Reservation 过期但 Session 仍存在 | Reaper 先关闭 Session，再关闭 Reservation；失败保持 quarantined |
| UDID 或 Host Node identity 变化 | 不更新活动连接快照；停止调度并要求重新发现/人工确认 |
| Appium/Agent/Host 离线 | 停止新预约；现有租约到期后按未知状态隔离，不假设 Session 已关闭 |

## 10. WDA、签名与 Secret

- 生产建议使用公司 Apple Developer Team 和受控签名身份，不使用个人免费账号作为稳定环境；
- WDA bundle ID、Team ID 的非敏感标识可以进入 Host readiness 摘要，证书私钥、Apple Account、Profile 内容和口令不得进入数据库；
- 签名、安装和更新只能由受控 macOS 运维/Agent 命令完成，命令 payload 使用 Secret 引用而非明文；
- 每次 Agent 启动和定时检查证书/Profile 有效期，设置预警窗口；过期前进入 degraded，过期立即停止新预约；
- iOS 17+ 可以使用预装 WDA 加快启动；实际策略必须针对目标 iOS、Xcode 和 WDA 版本真实验收；
- 非 macOS 模式要求 iOS 18+、RemoteXPC、明确 UDID/平台版本和预装/外部 WDA，不进入首期。

## 11. Console 与人工调试

Console 只展示 iOS Host、Device、Pool、预约、健康组件、签名到期摘要和审计。浏览器不获得 Appium Endpoint、Session Grant、Apple Secret 或 WDA 内部地址。Appium Device Farm 12.x 没有人工串流，因此 iOS Device 页面首期明确显示“自动化可用，人工远控暂不支持”，不能复用 Android STF 按钮或伪造远控入口。

## 12. 安全、可观测性和回滚

- Appium Node、go-ios、simctl、devicectl、WDA 和 tunnel 端口只绑定 Host 内部地址或受控网段；
- Host Agent Token、Appium 管理凭证与 Apple Secret 分离；日志统一脱敏 UDID 的展示值，但数据库内部保留稳定完整 UDID 用于唯一性；
- 指标至少包含 discovery 数、ready/busy/drift、Session 创建耗时与失败分类、WDA 签名剩余天数、Appium/XCUITest 版本漂移；
- 禁用 iOS Pool 或 drain macOS Host 即可停止新流量；Android Pool、STF 和独立 Appium Endpoint 不受影响；
- migration 必须可回滚到 Android-only 模式，回滚前先确保没有 active iOS Reservation/Session；
- Appium Device Farm 12.0.1 升级失败时回退锁定安装目录和配置，不修改 PostgreSQL Reservation 记录伪造成功。

## 13. 后续任务边界

DF-039 只交付设计。实现顺序固定为：平台中立契约与 migration → macOS Host/插件 Adapter → Reservation Session Fence → Simulator → 真机/WDA → Console → 真实验收与 Android 回归。任何任务不得提前建设 Alcor iOS Executor 或 IPA 业务模型。

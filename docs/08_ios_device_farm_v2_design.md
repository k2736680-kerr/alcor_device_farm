# 第二版 iOS 设备农场功能设计

## 1. 文档状态与目标

本文是 DF-039 的 iOS 实施基线。它在 Android 第一版之上增加多平台设备域设计，不修改 Alcor/DaFit 业务边界，也不代表当前生产代码已经支持 iOS。

当前目标是让现有设备农场内核把专用 macOS Host 作为动态 iOS 虚拟设备宿主机，按需创建、启动、停止、重建和删除 CoreSimulator 虚拟 iPhone，并向可信 iOS Executor 返回与 active Reservation 唯一绑定的 Appium XCUITest 连接。DF-046 按 ADR-0025 增加只操作目标 Simulator 的受控人工远控；当前仍不建设 iOS 业务执行器、不管理 IPA、不把 Android 迁入 Appium Device Farm，真实 iPhone延后独立接入。

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
        | 单值字符串 df:udids + appium:udid
        v
本机 Appium Hub（127.0.0.1:4723）
        ^
        | 动态发现 Node 每 5 秒注册 inventory（127.0.0.1:4724）
        |
Appium Device Farm 12 + XCUITest + WDA
        |
        +-- iOS Simulator（simctl/Xcode）
        +-- iOS 真机（配对、签名、Developer Mode）
```

每台 macOS Host 都使用一个只在本机 loopback 内工作的 Hub/动态发现 Node 组合，以便新启动的 Simulator 无需重启 Hub 即可在 5 秒内加入 inventory。这不是跨 Host Hub：各 Host 分别上报已安装 Runtime、Device Type 和受控动态库存，我方 PostgreSQL Scheduler 先选中唯一 Host、Device 和 UDID，Session Fence 再只把请求路由到该 Host 的本机 Hub，插件不得跨 Host 重新选机。

## 3. 组件职责

| 组件 | 负责 | 不负责 |
|---|---|---|
| Device Farm Server | Host/Device/Pool、预约、租约、唯一占用、审计和北向契约 | Xcode、WDA 构建、IPA、页面动作和结果 |
| PostgreSQL | active Reservation、Device Session、连接快照和状态收敛真相 | 保存 Apple 私钥或 Appium Device Farm 业务分配 |
| macOS Host Agent | 心跳、受控命令、inventory、健康、Endpoint 和本机 Secret 引用 | Alcor Run、Case、Result、评分和报告 |
| Appium Device Farm | 宿主机内 Hub/动态 Node 注册、iOS 发现、技术 busy、明确 UDID 的 Session 路由 | 业务设备池、人工预约、跨 Host 调度或现成浏览器远控页面 |
| XCUITest Driver/WDA | WebDriver Session、XCTest 通信和设备自动化 | 设备农场预约、业务用例和报告 |
| go-ios | 保留为未来真机发现/诊断和插件依赖 | 当前 Simulator 创建、独立设备池或业务执行 |
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
- 已安装 iOS Runtime 与 iPhone Device Type 的受控目录；
- iOS Simulator 动态创建、启动、停止、擦除重建、删除、发现、健康和预约；
- iOS Pool、能力筛选和单设备 active Reservation；
- 每台 Host 的本机 Appium Hub Endpoint、动态发现 Node 与明确 UDID 的连接快照；
- Session 创建前的 Reservation/UDID 防护、插件 busy 漂移检测和失败隔离；
- Console 中的 iOS Host、Device、Pool、预约、签名就绪状态和审计；
- Android 第一版全链路回归。

### 5.2 延期

- 自动下载 Xcode、Simulator Runtime 或任意 IPA；
- 克隆 Simulator、真机恢复出厂和预测式大规模弹性；
- 真实 iPhone、配对、Developer Mode 与 WDA 签名；
- Windows/Linux Host 运行 iOS 真机 Session；
- Appium Device Farm Hub/Node 跨主机自动分配；本机 Hub/动态 Node 只用于刷新单台 Host inventory；
- tvOS、无线 iOS、Appium Inspector 托管、原始 WebDriver/WDA 暴露和宿主机桌面远控；
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

Host Agent 心跳至少上报：macOS 版本、架构、Xcode build、可用 iOS Runtime 标识、iPhone Device Type 标识、Node/Appium/Plugin/XCUITest/go-ios 版本、Appium Endpoint、Simulator 数量和容量。版本不匹配、Xcode license 未接受、Appium doctor 失败或 Endpoint 不健康时 Host 不可接收新创建与新预约。

### 7.2 Simulator

Appium Device Farm/simctl 发现历史 allowlist 与使用保留名称前缀创建的动态 Simulator。Runtime 和 Device Type 必须来自 Host 实际可用目录与部署 allowlist 的交集；Server 生成受控名称，Agent 用 `simctl create` 返回的 UDID 建立 Provider identity。Shutdown 设备保留为 stopped；只有 boot、bootstatus、插件 inventory 和 XCUITest 健康全部通过后才能 ready。Simulator 的 Runtime、机型和 UDID 都进入能力快照。

### 7.3 动态生命周期

- 创建：Server 校验 Host/Pool/目录/容量并原子登记 provisioning Device、Pool membership 和 create Host Command；Agent 幂等执行 create、boot、bootstatus，动态 Node 在 5 秒轮询内把新设备注册到本机 Hub。创建后 inventory 故障时，Agent 按刚创建的受管 UDID 直接从 CoreSimulator 清理，不能遗留半台虚拟设备。
- 重建：无活动 Reservation/Session 且插件不 busy 时执行 shutdown、erase、boot、bootstatus；Device ID 与 UDID 不变。
- 删除：无活动 Reservation/Session 且插件不 busy 时执行 shutdown、delete；数据库 Device 标记 deleted 并保留审计。
- Agent 重启：按保留名称前缀和 UDID 从 CoreSimulator 重新发现，不因内存状态丢失而重复创建。

### 7.4 Pool 固定目标自动伸缩

- iOS Pool 与 Android Pool 共用 `total_target/min_ready/max_concurrency`，控制台不得把 iOS 目标输入禁用或显示成写死数量；
- 管理员从本 Pool 选择一台 `ready/healthy` Simulator 作为扩容模板，模板只提供 Mac Host、Runtime ID、iPhone Device Type ID 和显示规格，不复制 App、账号、缓存或 CoreSimulator 数据；
- 目标增加时，Controller 在 Pool 行锁内重新统计已登记和正在创建的 Simulator，复用 7.3 的受控创建、Host 实时容量预检、幂等和审计链路补足差额；
- 目标降低时，只选择没有活动 Reservation/Session、没有在途命令且不属于其他 Pool 的最旧空闲 Simulator，复用既有 delete Host Command；占用中的设备等待释放，不强制中断；
- 模板缺失、Host 离线/排空、目录变化或容量不足时保留管理员设置的真实目标，并向 Console 返回或展示中文阻塞原因；不得伪造已达到目标，也不得留下半条 Device、membership、command 或 CoreSimulator。

## 8. 平台中立健康模型

Host Agent 后续用组件探针代替 Android 专用布尔值作为内部真相：

| 探针 | Android 映射 | iOS Simulator | iOS 真机 |
|---|---|---|---|
| `transport` | ADB online | simctl 可见且 Booted |
| `os_ready` | boot completed | SpringBoard/Simulator ready |
| `automation` | UiAutomator2/Appium 冒烟 | XCUITest/WDA 冒烟 |
| `router` | 独立 Endpoint healthy | 本机 Appium Hub 与动态 Node healthy、UDID inventory 一致 |
| `remote_control` | STF 可见性 | DF-046 的 Appium/XCUITest/WDA MJPEG 与动作白名单 |

只有必需探针全部通过才能 `ready/healthy`。`unsupported` 不是失败；Runtime 不可用、UDID 漂移或插件 inventory 冲突为 unhealthy/quarantined。插件 `providerBusy` 只表示技术占用，不降低 Router 健康：Reservation 和 Appium Session 一致时 Device 为 `busy/healthy`。固定版 XCUITest doctor 的必需结果必须通过。

## 9. 预约与 Session 防双分配

### 9.1 正常时序

1. Executor 使用 `platformName=iOS` 和 Pool ID 创建 Reservation；
2. Scheduler 在 PostgreSQL 行锁和 active-device 唯一索引下选中一台 ready iOS Device；
3. Device Session 固化 Host、UDID、平台、Appium Endpoint 和插件 Node identity；
4. 服务端签发短时、单次、只绑定该 Reservation/Device/UDID/Endpoint 的 Session Grant；
5. 可信 Session Fence 校验 Reservation 仍 active，拒绝调用方自选设备，并只连接设备所属 Host 的本机 Hub；
6. 发送给 Appium Device Farm 的 capabilities 必须同时包含完全相同的 `appium:udid` 和单值字符串 `df:udids=<reserved_udid>`；
7. 插件把目标设备标为 busy，XCUITest 创建 WDA Session；成功后记录 Appium Session ID，Device 从 reserved 进入 busy；
8. WebDriver 后续请求只能沿已绑定 Session 路由；Session 删除后清除技术 busy；
9. Executor 释放 Reservation；Reaper 负责异常超时，健康设备回到 ready。

### 9.2 强制不变式

- 没有 active Reservation 就不能创建 iOS Session；
- 一个 Session Grant 只能消费一次，不能延长 Reservation；
- `appium:udid`、`df:udids`、连接快照 UDID 和 Device.serial 必须四者相同；
- 客户端提交的 `df:tags`、`filterByHost`、多 UDID 或自动选机能力必须被拒绝或覆盖；
- Appium Hub 和动态发现 Node 均只监听 loopback；只有 Session Fence 可连接 Hub，Dashboard 不提供人工 block/unblock；
- 插件 busy 不能让 PostgreSQL Reservation 自动变 active，也不能延长租约；
- 同一 Device 同时只能有一个 active Reservation 和一个 active Appium Session 绑定。

### 9.3 漂移处理

| 漂移 | 处理 |
|---|---|
| Reservation active，插件报告目标 busy 且 Session 不匹配 | 创建失败，Device degraded；查询绑定后隔离或等待旧 Session 清理 |
| 插件存在 Session，PostgreSQL 无 active Reservation | 正常 Session 结束后留 30 秒等待 Agent 刷新 busy；超时仍 busy 时终止技术 Session、记录高优先级审计、Device quarantined |
| Reservation 过期但 Session 仍存在 | Reaper 先关闭 Session，再关闭 Reservation；失败保持 quarantined |
| Reservation 已到期或距离到期不足 30 秒，插件已先释放 busy | Reconciler 不隔离；由 Reaper 和 Session cleanup 正常关闭并恢复 ready/healthy |
| UDID 或 Host Node identity 变化 | 不更新活动连接快照；停止调度并要求重新发现/人工确认 |
| Appium/Agent/Host 离线 | 停止新预约；现有租约到期后按未知状态隔离，不假设 Session 已关闭 |

## 10. CoreSimulator 目录与命令安全

- Server 目录只展示 `simctl list runtimes/devicetypes -j` 中可用项与部署 allowlist 的交集；
- 只允许 iOS Runtime 和 iPhone Device Type，拒绝 tvOS、watchOS、visionOS、任意文本 ID 与失效 Runtime；
- 设备名称由 Server 规范化并加保留前缀，Provider 只删除该前缀或历史固定 allowlist 中的明确 UDID；
- 命令使用参数数组直接调用固定 `xcrun`，不经过 shell；错误输出只归类并脱敏，不回传宿主机路径；
- Runtime 下载和 Xcode 升级是运维动作，当前 API 不提供入口。

## 11. Console 与人工调试

Console 展示 iOS Host、Runtime/机型目录、动态创建向导、Device、Pool、预约、健康组件和审计。DF-045 只交付设备域页面；DF-046 按 ADR-0025 复用目标 Appium Session 的 WDA MJPEG/动作增加独立同源入口。Appium Device Farm 12.x 没有可直接复用的人工远控页面，因此本项目只补预约鉴权代理和动作白名单；浏览器不获得 Host/Fence/Appium/WDA/MJPEG 地址、Agent Token、Session Grant 或原始 WebDriver 能力。

短时同源签名是首次打开控制页的入口票据，不是远控 Session 的固定寿命。已加载页面的资源、画面和动作继续由 Console 会话、操作者与滑动续约的 active Reservation 鉴权；Fence 重启时从绑定 Appium Session capabilities 恢复 MJPEG 端口，不要求重启 Simulator 或暴露端口给浏览器。

## 12. 安全、可观测性和回滚

- Appium Node、go-ios、simctl、devicectl、WDA 和 tunnel 端口只绑定 Host 内部地址或受控网段；
- Host Agent Token、Appium 管理凭证与 Apple Secret 分离；日志统一脱敏 UDID 的展示值，但数据库内部保留稳定完整 UDID 用于唯一性；
- 指标至少包含 discovery 数、ready/busy/drift、Session 创建耗时与失败分类、WDA 签名剩余天数、Appium/XCUITest 版本漂移；
- 禁用 iOS Pool 或 drain macOS Host 即可停止新流量；Android Pool、STF 和独立 Appium Endpoint 不受影响；
- migration 必须可回滚到 Android-only 模式，回滚前先确保没有 active iOS Reservation/Session；
- Appium Device Farm 12.0.1 升级失败时回退锁定安装目录和配置，不修改 PostgreSQL Reservation 记录伪造成功。

## 13. 后续任务边界

DF-039～DF-043 已交付平台中立契约、macOS Host、Session Fence 和固定 Simulator 基础。ADR-0024 后的顺序为：动态 CoreSimulator 生命周期 → Console 创建与管理 → 真实 Simulator 稳定性/回滚与 Android 回归。真实 iPhone/WDA 签名另立后续任务；任何任务不得提前建设 Alcor iOS Executor 或 IPA 业务模型。

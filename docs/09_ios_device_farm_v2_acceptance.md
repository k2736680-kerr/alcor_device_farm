# 第二版 iOS 设备农场验收方案

## 1. 原则

- Android 第一版验收继续有效，但不能替代 iOS 真实验收；
- Mock 只验证契约、状态机、并发和故障注入，不能证明 Xcode/WDA/iOS 可用；
- iOS P0/P1 必须在固定版本的真实 macOS Host 上通过；真机能力必须至少使用一台实际 iPhone，Simulator 结果不能替代；
- PostgreSQL active Reservation 始终是唯一业务占用真相；Appium Device Farm busy 仅作为技术互斥；
- 不允许通过手工改数据库、人工 block 插件设备或暴露 Appium Dashboard 来制造成功；
- 所有日志、截图和配置证据必须移除 Token、Cookie、Apple Account、证书私钥、Profile 内容和 Session Grant。

## 2. 验收环境

### E4：macOS Simulator 环境

- 专用 Apple Silicon macOS Host，版本与目标 Xcode 兼容；
- 固定 Xcode 和至少一个稳定 iOS Simulator Runtime；
- Node 22.23.2、Appium 3.6.0、Device Farm 12.0.1、XCUITest 12.4.0；
- Device Farm Host Agent、PostgreSQL 和 Device Farm Server；
- 至少两个不同 UDID 的 booted Simulator，用于单设备和并发防串机；
- Appium Node 只在受控网络可达，Dashboard 不提供人工分配入口。

### E5：macOS iOS 真机环境

- E4 的专用 macOS Host；
- 至少一台已配对和信任的 iPhone，启用 Developer Mode 和 UI Automation；
- 与目标 iOS 匹配的 Xcode/SDK；
- 受控 Apple Developer Team、有效 WDA 签名和 Provisioning Profile；
- go-ios 1.3.2 及目标版本所需的 RemoteXPC/WDA 依赖；
- 一份最小、已正确签名的测试 IPA，由外部 iOS Executor/验收 Harness 使用，不存入设备农场数据库。

### E6：多 Host 与 Alcor 联调环境

- E4/E5 至少两个 macOS Host 或一个 macOS Host 加故障替身；
- Alcor 可信 iOS Executor/契约 Harness；
- Device Farm Service Token 与 Host Agent Token 分离；
- Android E2/E3 环境保持可用，用于回归。

## 3. Gate

| Gate | 对应任务 | 通过条件 |
|---|---|---|
| G10 iOS 设计 | DF-039 | ADR、复用矩阵、功能设计、任务拆分、版本和验收环境明确，无生产代码 |
| G11 平台中立控制面 | DF-040 | migration/OpenAPI/领域状态支持 Android+iOS，Android 数据无破坏，Mock 无双占 |
| G12 macOS 与路由 | DF-041～DF-042 | Host/插件 inventory、健康和 Reservation Session Fence 在 E4 可用 |
| G13 Simulator | DF-043 | 两台 Simulator 发现、预约、并发 Session、释放和故障恢复通过 |
| G14 真机 | DF-044 | E5 完成配对、签名、WDA、明确 UDID Session、释放和签名故障收敛 |
| G15 Console | DF-045 | iOS 设备域页面、安全和审计通过，未伪造人工远控 |
| G16 发布 | DF-046 | E6 全链路、稳定性、回滚和 Android 全量回归通过 |

## 4. P0/P1 验收用例

### 4.1 契约与数据

| 编号 | 级别 | 场景 | 预期 |
|---|---|---|---|
| AT-IOS-DATA-001 | P0 | Android V1 数据执行 migration | 全部回填 `platform=android`，现有 API/Pool/Device/Reservation 不变 |
| AT-IOS-DATA-002 | P0 | 创建 iOS Pool 并加入 Android Device | 事务拒绝，无半条 membership |
| AT-IOS-DATA-003 | P0 | 多台 iOS Device 共用一个 Appium Node Endpoint | 合法；UDID、serial、Provider identity 仍唯一 |
| AT-IOS-DATA-004 | P0 | 请求平台大小写和未知能力 | 标准化已登记字段；未知/冲突能力返回稳定错误，不错配设备 |
| AT-IOS-DATA-005 | P0 | migration up/down/up | 无数据丢失；回滚前存在 iOS 活动资源时安全拒绝或按手册排空 |

### 4.2 macOS Host 与发现

| 编号 | 级别 | 场景 | 预期 |
|---|---|---|---|
| AT-IOS-HOST-001 | P0 | Agent 上报 macOS/Xcode/Appium/插件/XCUITest/go-ios | 版本和能力完整；Secret 不进入心跳 |
| AT-IOS-HOST-002 | P0 | Xcode license、Appium doctor 或插件版本失败 | Host 不接收新预约并给出稳定原因 |
| AT-IOS-DISC-001 | P0 | allowlist 中 booted Simulator 出现/消失 | Device 唯一映射，状态在时限内收敛 |
| AT-IOS-DISC-002 | P0 | 新未知 Simulator/USB 真机接入 | 只登记 unknown/quarantined，不自动进入 Pool |
| AT-IOS-DISC-003 | P0 | 同一 UDID 从另一 Host 上报 | 不静默迁移；两台 Host 停止相关新分配并产生冲突审计 |
| AT-IOS-DISC-004 | P1 | Agent 或 Appium Node 重启 | 120 秒内恢复 inventory，旧 Generation 不覆盖新状态 |

### 4.3 Reservation 与 Session Fence

| 编号 | 级别 | 场景 | 预期 |
|---|---|---|---|
| AT-IOS-RES-001 | P0 | 100 个并发请求竞争一台 iPhone | PostgreSQL 只有一个 active Reservation，零双占 |
| AT-IOS-RES-002 | P0 | 无 Reservation 直接创建 Session | 网络/Session Fence 拒绝，插件不产生 Session |
| AT-IOS-RES-003 | P0 | `appium:udid` 与 `df:udids` 不同 | 请求拒绝并审计，不转发 Appium |
| AT-IOS-RES-004 | P0 | `df:udids` 包含多个设备或使用 tags/filterByHost | 请求拒绝，不允许插件再次自由选机 |
| AT-IOS-RES-005 | P0 | 重放已消费或已过期 Session Grant | 请求拒绝，不能创建第二 Session |
| AT-IOS-RES-006 | P0 | 插件 busy 与 Reservation 不一致 | 不返回可用；设备 degraded/quarantined，产生漂移事件 |
| AT-IOS-RES-007 | P0 | Reservation 过期时 Appium Session 未关闭 | Reaper 关闭 Session；失败时保持隔离且不重新分配 |
| AT-IOS-RES-008 | P1 | 两台 Simulator 并发 | Session/UDID/Host/结果互不串联，两个插件 busy 与两个 Reservation 一一对应 |

### 4.4 Simulator 与真机自动化

| 编号 | 级别 | 场景 | 预期 |
|---|---|---|---|
| AT-IOS-SIM-001 | P0 | 指定 Simulator UDID 创建 XCUITest Session | WDA status、最小页面查询和 Session 删除成功 |
| AT-IOS-SIM-002 | P0 | Simulator shutdown/boot failure | 不进入 ready；已有预约失败分类为基础设施故障 |
| AT-IOS-REAL-001 | P0 | 已配对真机明确 UDID 创建 Session | 不自动选择其他设备；WDA 与最小操作成功 |
| AT-IOS-REAL-002 | P0 | 未信任、Developer Mode 关闭或 UI Automation 关闭 | 健康检查失败，设备不可预约，错误指出缺失步骤 |
| AT-IOS-REAL-003 | P0 | WDA 签名/Profile 过期 | 设备停止新预约，无 Secret 泄露；更新后可人工解除隔离 |
| AT-IOS-REAL-004 | P0 | iOS/Xcode/XCUITest 不兼容 | Host/Device readiness 拒绝，不在 Session 时才随机失败 |
| AT-IOS-REAL-005 | P1 | Session 成功、测试失败、Worker 取消和超时 | 四条路径都最终删除 Session 并释放 Reservation |

### 4.5 安全、Console 和回归

| 编号 | 级别 | 场景 | 预期 |
|---|---|---|---|
| AT-IOS-SEC-001 | P0 | 扫描 API、日志、审计、数据库普通字段和浏览器 | 无 Apple Account、私钥、Profile、Token、Grant 或内部 WDA 地址 |
| AT-IOS-SEC-002 | P0 | 浏览器访问 Appium Node/Dashboard/Session Grant | 网络和 API 均拒绝 |
| AT-IOS-UI-001 | P0 | iOS Device 页面 | 显示平台、机型、OS、真机/Simulator、健康和签名摘要；明确人工远控不支持 |
| AT-IOS-UI-002 | P0 | 点击 Android STF 远控逻辑作用于 iOS | UI 不提供该操作，API 也拒绝 |
| AT-IOS-REG-001 | P0 | Android 全量 Go/Console/契约和真实冒烟 | 第一版预约、STF、Appium、DaFit 和长期设备语义无回归 |
| AT-IOS-RBK-001 | P0 | drain iOS Host、禁用 iOS Pool并回滚版本 | 无新 iOS 流量，活动 Session 正常结束或受控终止；Android 不受影响 |

## 5. 非功能标准

| 指标 | 标准 |
|---|---|
| 双占/串设备 | 0 |
| 已 ready iOS 设备预约 | 10 秒内 active 或稳定可重试错误 |
| Session 路由 | 100% 与 Reservation UDID 相同 |
| Host/Agent/Appium 恢复 | 120 秒内收敛或明确隔离 |
| 过期回收 | grace period 后 60 秒内开始关闭 Session |
| 稳定性 | Simulator 50 次循环；真机至少 20 次循环，无永久 busy/Reservation |
| Secret 泄露 | 0 |
| Android 回归 | P0/P1 仍全部通过 |

## 6. 必须保存的证据

- Host 硬件、macOS、Xcode、iOS Runtime 和测试设备脱敏清单；
- Node/Appium/插件/XCUITest/WDA/go-ios 的固定版本和安装校验；
- Reservation、Session Grant、Appium Session、UDID 与插件 busy 的脱敏时间线；
- Simulator/真机的成功、故障、取消、过期和恢复日志；
- WDA 签名到期测试只保存状态和有效期摘要，不保存证书/Profile 内容；
- PostgreSQL 唯一约束、漂移事件、最终清理和 Android 回归结果；
- 回滚步骤与回滚后资源状态。

# 第二版 iOS 设备农场验收方案

## 1. 原则

- Android 第一版验收继续有效，但不能替代 iOS 真实验收；
- Mock 只验证契约、状态机、并发和故障注入，不能证明 Xcode/WDA/iOS 可用；
- iOS P0/P1 必须在固定版本的真实 macOS Host 上通过；当前签收范围是动态 Simulator，不宣称真实 iPhone 已接入；
- PostgreSQL active Reservation 始终是唯一业务占用真相；Appium Device Farm busy 仅作为技术互斥；
- 不允许通过手工改数据库、人工 block 插件设备或暴露 Appium Dashboard 来制造成功；
- 所有日志、截图和配置证据必须移除 Token、Cookie、Apple Account、证书私钥、Profile 内容和 Session Grant。

## 2. 验收环境

### E4：macOS Simulator 环境

- 专用 Apple Silicon macOS Host，版本与目标 Xcode 兼容；
- 固定 Xcode 和至少一个稳定 iOS Simulator Runtime；
- Node 22.23.2、Appium 3.6.0、Device Farm 12.0.1、XCUITest 12.4.0；
- Device Farm Host Agent、PostgreSQL 和 Device Farm Server；
- 至少两个由后台动态创建的不同 UDID Simulator，用于单设备和并发防串机；
- 每台 Mac 的 Appium Hub 固定为 `127.0.0.1:4723`，动态发现 Node 固定为 `127.0.0.1:4724` 并每 5 秒向本机 Hub 注册 inventory；两者均不对外开放，Dashboard 不提供人工分配入口；
- Session Fence 只连接本机 Hub；PostgreSQL Scheduler 先确定唯一 Host/UDID，本机 Hub/Node 不承担跨 Host 自动分配。

### E6：多 Host 与 Alcor 联调环境

- E4 至少两个 macOS Host 或一个 macOS Host 加故障替身；
- Alcor 可信 iOS Executor/契约 Harness；
- Device Farm Service Token 与 Host Agent Token 分离；
- Android E2/E3 环境保持可用，用于回归。

## 3. Gate

| Gate | 对应任务 | 通过条件 |
|---|---|---|
| G10 iOS 设计 | DF-039 | ADR、复用矩阵、功能设计、任务拆分、版本和验收环境明确，无生产代码 |
| G11 平台中立控制面 | DF-040 | migration/OpenAPI/领域状态支持 Android+iOS，Android 数据无破坏，Mock 无双占 |
| G12 macOS 与路由 | DF-041～DF-042 | Host/插件 inventory、健康和 Reservation Session Fence 在 E4 可用 |
| G13 Simulator 基础 | DF-043 | 两台固定 Simulator 发现、预约、并发 Session、释放和故障恢复通过 |
| G14 动态 Simulator | DF-044 | E4 从受控目录创建、启动、Session、重建和删除通过 |
| G15 Console | DF-045 | iOS 设备域页面、安全和审计通过，未伪造人工远控 |
| G16 iOS 受控远控替换 | DF-050 | Baguette 原生目标 Simulator 画面、点击、滑动、文本、Home、互斥和释放通过；旧自写实现零残留 |
| G17 发布 | DF-047 | E6 全链路、稳定性、回滚和 Android 全量回归通过 |
| G18 多设备安装目标绑定 | DF-052 | 两台 Simulator 并存时每个浏览器会话和 App 上传都固定到预约 UDID；Android STF 同步回归明确 `stf_serial` |

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
| AT-IOS-HOST-003 | P0 | 已结束 Session 的 WDA 端口仍存活，doctor 可选 Remote XPC 探测不返回 | 15 秒后继续心跳；仅三项 doctor 必需检查已明确通过时生效，Host 不误离线 |
| AT-IOS-DISC-001 | P0 | allowlist 中 booted Simulator 出现/消失 | Device 唯一映射，状态在时限内收敛 |
| AT-IOS-DISC-002 | P0 | 新未知 Simulator/USB 真机接入 | 只登记 unknown/quarantined，不自动进入 Pool |
| AT-IOS-DISC-003 | P0 | 同一 UDID 从另一 Host 上报 | 不静默迁移；两台 Host 停止相关新分配并产生冲突审计 |
| AT-IOS-DISC-004 | P1 | Agent、本机 Hub 或动态发现 Node 重启 | 120 秒内恢复 inventory，旧 Generation 不覆盖新状态；Session Fence 始终只连接 Hub |
| AT-IOS-DISC-005 | P0 | 读取 Runtime 与 Device Type 目录 | 只返回 Host 已安装、可用且 allowlist 内的 iOS/iPhone 项，不返回其他平台或任意命令参数 |

### 4.3 Reservation 与 Session Fence

| 编号 | 级别 | 场景 | 预期 |
|---|---|---|---|
| AT-IOS-RES-001 | P0 | 100 个并发请求竞争一台 iPhone | PostgreSQL 只有一个 active Reservation，零双占 |
| AT-IOS-RES-002 | P0 | 无 Reservation 直接创建 Session | 网络/Session Fence 拒绝，插件不产生 Session |
| AT-IOS-RES-003 | P0 | `appium:udid` 与 `df:udids` 不同 | 请求拒绝并审计，不转发 Appium |
| AT-IOS-RES-004 | P0 | `df:udids` 包含逗号分隔的多个设备、使用数组或使用 tags/filterByHost | 请求拒绝，不允许插件再次自由选机 |
| AT-IOS-RES-005 | P0 | 重放已消费或已过期 Session Grant | 请求拒绝，不能创建第二 Session |
| AT-IOS-RES-006 | P0 | 插件 busy 与 Reservation 不一致 | 不返回可用；设备 degraded/quarantined，产生漂移事件 |
| AT-IOS-RES-007 | P0 | Reservation 过期时 Appium Session 未关闭 | Reaper 关闭 Session；失败时保持隔离且不重新分配 |
| AT-IOS-RES-008 | P1 | 两台 Simulator 并发 | Session/UDID/Host/结果互不串联，两个插件 busy 与两个 Reservation 一一对应 |
| AT-IOS-RES-009 | P0 | 正常删除 Session 后 Agent 尚未刷新插件 busy | 30 秒清理宽限内不误隔离；随后收敛为 ready/healthy，超时仍 busy 才隔离 |

### 4.4 动态 Simulator 与自动化

| 编号 | 级别 | 场景 | 预期 |
|---|---|---|---|
| AT-IOS-SIM-001 | P0 | 指定 Simulator UDID 创建 XCUITest Session | WDA status、最小页面查询和 Session 删除成功 |
| AT-IOS-SIM-002 | P0 | Simulator shutdown/boot failure | 不进入 ready；已有预约失败分类为基础设施故障 |
| AT-IOS-SIM-003 | P0 | 后台选择可用 Runtime 与 iPhone 类型创建 | 原子登记 Device/Pool/Host Command；`simctl create/boot/bootstatus` 后 ready/healthy |
| AT-IOS-SIM-004 | P0 | Runtime/Device Type 不在目录或请求携带任意参数 | 中文稳定错误，Agent 不执行任何变更命令 |
| AT-IOS-SIM-005 | P0 | 容量不足 | 返回内存/磁盘/槽位中文缺口，无半条 Device、membership 或 command |
| AT-IOS-SIM-006 | P0 | 空闲 Simulator 重建 | shutdown/erase/boot 后 UDID 不变、旧数据清空并恢复 ready/healthy |
| AT-IOS-SIM-007 | P0 | 空闲 Simulator 删除 | CoreSimulator UDID 消失，Device 标记 deleted，Pool membership/Endpoint 按既有语义清理 |
| AT-IOS-SIM-008 | P0 | busy 或有活动 Reservation 时重建/删除 | Console 与 Server/Provider 均拒绝，不中断 Session |
| AT-IOS-SIM-009 | P0 | 相同幂等键重放创建或 Agent 在 create 后重启 | 只存在一台对应名称/Device/Command，不产生重复 UDID |
| AT-IOS-SIM-010 | P0 | `simctl create` 成功后 Hub inventory 读取或 Node 注册失败 | Agent 按刚创建的受管 UDID 直接执行 shutdown/delete 补偿，无 CoreSimulator、Device、membership 或 command 半成品 |
| AT-IOS-SIM-011 | P0 | 删除 Pool 最后一台动态 Simulator | 删除成功且 `total_target=0`；`max_concurrency` 保留合法最小值，不因数据库约束遗留 Simulator |
| AT-IOS-SIM-012 | P0 | iOS Pool 目标从 3 调到 6 | 目标可在 Console 输入；模板和容量满足时只新增 3 台，Runtime/机型一致但设备数据全新 |
| AT-IOS-SIM-013 | P0 | 两个 Controller 并发扩容同一 iOS Pool | Pool 行锁后重新计数，最终不超过目标、无重复 Device/Command/UDID |
| AT-IOS-SIM-014 | P0 | iOS Pool 缺少扩容模板或 Host 容量不足 | 保留真实目标并显示中文阻塞原因；不创建半条 Device、membership、command 或 CoreSimulator |
| AT-IOS-SIM-015 | P0 | iOS Pool 缩容且部分 Simulator 使用中 | 不强制中断 Reservation/Session；只删除可安全处理的最旧空闲设备，其余等待释放后收敛 |

### 4.5 安全、Console 和回归

| 编号 | 级别 | 场景 | 预期 |
|---|---|---|---|
| AT-IOS-SEC-001 | P0 | 扫描 API、日志、审计、数据库普通字段和浏览器 | 无 Apple Account、私钥、Profile、Token、Grant 或内部 WDA 地址 |
| AT-IOS-SEC-002 | P0 | 浏览器访问 Appium Hub、动态发现 Node、Dashboard 或 Session Grant | 网络和 API 均拒绝 |
| AT-IOS-UI-001 | P0 | iOS Device 页面 | 显示平台、Runtime、机型、Simulator、健康、创建/管理和受控远控入口 |
| AT-IOS-UI-002 | P0 | 打开 iOS 远控 | 使用 Baguette 原生 Web UI，只显示目标 Simulator；连续画面、点击、滑动、文本、Home 和应用切换真实有效，不显示 macOS 桌面或其他 Simulator |
| AT-IOS-UI-003 | P0 | 两台 iOS 远控并行拖入 App | 两个标签页使用不同的按 UDID 会话 Cookie；每次只请求本页 `/simulators/:udid/files`，交叉 Cookie、其他 UDID 和模糊目标均拒绝，上游失败不得显示安装成功 |
| AT-IOS-UI-003 | P0 | 尝试访问设备墙、其他 UDID、生命周期或插件命令 | Gateway 拒绝，当前预约只能控制目标 Simulator；iOS 不进入 STF，也不创建人工 Appium/WDA Session |
| AT-IOS-UI-004 | P0 | 扫描仓库和运行路由 | 不存在自写 iOS HTML/CSS/JavaScript、MJPEG/截图代理、坐标转换、Appium 人工动作或旧 Alcor iOS 代理路由 |
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
| 稳定性 | Simulator 创建、Session、重建、删除至少 50 次循环，无永久 busy/Reservation 或残留 UDID |
| Secret 泄露 | 0 |
| Android 回归 | P0/P1 仍全部通过 |

## 6. 必须保存的证据

- Host 硬件、macOS、Xcode、iOS Runtime 和测试设备脱敏清单；
- Node/Appium/插件/XCUITest/WDA/go-ios 的固定版本和安装校验；
- Reservation、Session Grant、Appium Session、UDID 与插件 busy 的脱敏时间线；
- Simulator 创建、Session、重建、删除、故障、取消、过期和恢复日志；
- PostgreSQL 唯一约束、漂移事件、最终清理和 Android 回归结果；
- 回滚步骤与回滚后资源状态。

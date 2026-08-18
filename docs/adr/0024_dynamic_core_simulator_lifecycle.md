# ADR-0024：macOS 宿主机动态管理 CoreSimulator 虚拟 iPhone

## 状态

已接受。本文取代 ADR-0021 第 7、13 条和 DF-043 中“Simulator 只能使用固定库存、禁止创建/删除”的首期限制；DF-043 已完成的发现、启动、停止、预约和 Session Fence 仍作为实现基础保留。

## 背景

需求方已明确：授权 Mac 是 iOS 虚拟设备宿主机，设备农场必须像 Android 宿主机一样，从后台按需创建、启动、停止、重建和删除虚拟手机。此前把固定 Simulator 库存和真实 iPhone/WDA 签名作为后续主线，不能满足这一目标。

iOS Simulator 不是可部署到普通 Docker 容器的 Linux 虚拟机。Apple 官方、可维护的虚拟设备运行层是 macOS 上随 Xcode 提供的 CoreSimulator，受控入口是 `xcrun simctl`。Appium Device Farm 继续负责本机 inventory、技术 busy 和明确 UDID 的 Session 路由，不负责创建业务 Reservation，也不取代 CoreSimulator 生命周期。

## 决策

1. 专用 macOS Host 复用 Xcode CoreSimulator。首期不在 Docker 中运行 iOS，不引入第三方 macOS 虚拟机或第二套虚拟化层。
2. Host Agent 上报本机已安装且 `isAvailable=true` 的 iOS Runtime 和 iPhone Device Type。Server 只返回部署 allowlist 与实际目录的交集；不自动下载 Xcode或 Runtime。
3. 创建入口复用现有 Device、Pool、Host Command、Agent、Provider、容量预检、审计和幂等链路。新增的只是 CoreSimulator 编排，不新增第二套 Scheduler、Reservation 或 Session 数据库。
4. Agent 只允许固定参数调用：`simctl list runtimes/devicetypes/devices -j`、`create`、`boot`、`bootstatus -b`、`shutdown`、`erase` 和 `delete`。Runtime ID 与 Device Type ID 必须来自受控目录，名称必须由 Server 生成并使用保留前缀；任何 API 都不得接收 shell、任意二进制或任意参数数组。
5. 动态 Simulator 使用保留名称前缀和 Device ID 建立幂等身份。Agent 重启后从 CoreSimulator 与 Appium Device Farm 重新发现；重复 create 不得生成第二台设备。
6. 每台 macOS Host 在 loopback 内运行一个 Appium Hub（默认 `127.0.0.1:4723`）和一个动态发现 Node（默认 `127.0.0.1:4724`）。Node 每 5 秒重新发现本机已启动 Simulator 并注册到本机 Hub；Session Fence 只连接 Hub。该 Hub/Node 只解决单台 Host 内动态 inventory 刷新，不参与跨 Host 自动选机；PostgreSQL Scheduler 仍先确定唯一 Host、Device 和 UDID。
7. `create` 创建后立即 boot 并等待 `bootstatus`，只有 CoreSimulator、动态发现 Node、Hub inventory 和 XCUITest 健康全部通过才进入 `ready/healthy`。若 CoreSimulator 创建成功后 Hub inventory 读取或注册失败，Agent 必须绕过 inventory，按刚创建的受管 UDID 直接执行 shutdown/delete 补偿，不能遗留虚拟设备。
8. `rebuild` 对无活动 Reservation 的 Simulator 执行受控 shutdown、erase、boot、bootstatus；Device ID 和 UDID 保持不变，设备内容清空。`delete` 执行 shutdown 与 delete，并沿现有管理删除链把 Device 标记为 `deleted`，保留审计。删除 Pool 最后一台设备时 `total_target` 可以降为 0，但数据库要求的 `max_concurrency` 仍最少保留 1，不能阻断最终清理。
9. busy、reserved、recycling 或存在活动 Reservation/Session 的设备禁止重建和删除。Provider 也必须再次检查 Appium Device Farm busy，不能只依赖 Console 按钮状态。
10. 创建前同时检查 Host online/draining、平台、实际设备槽位、可用内存和磁盘安全阈值。资源不足返回稳定错误码、结构化缺口和中文说明，不创建 Device、命令、Pool membership 或 CoreSimulator 残留。
11. 新增只读 `GET /api/v1/ios-simulator-catalog?host_id=` 和受控 `POST /api/v1/ios-simulators`。创建请求只包含 Host、Pool、Runtime ID、Device Type ID、显示名称和审计原因；响应返回 Device 与持久化 Host Command，不暴露 Appium 内部 Endpoint。
12. DF-044 实现后端动态生命周期，DF-045 增加 Console 目录与创建/管理页面，DF-046 完成真实 Mac 稳定性、故障恢复、回滚和 Android 回归。
13. 真实 iPhone、配对、Developer Mode 和 WDA 签名仍可作为后续 Provider 扩展，但不再阻塞本轮动态 iOS Simulator 设备农场交付。

## 后果

- 授权 Mac 成为真正的动态 iOS 虚拟设备宿主机；
- Xcode/CoreSimulator 仍是唯一实际虚拟化实现，本仓库只做受控编排；
- DF-043 的固定库存证据不被改写，但其“禁止创建删除”结论不再约束后续版本；
- 当前没有真实 iPhone 不会阻塞 DF-044～DF-046；
- 后续若增加 Runtime 下载、Simulator 克隆、macOS 虚拟机或真机，必须新增 ADR 和独立真实验收。

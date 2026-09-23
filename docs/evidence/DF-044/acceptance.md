# DF-044 iOS Simulator 动态创建、重建和删除验收

## 当前状态

`completed`

验收日期：2026-08-18。

本任务在专用 Apple Silicon macOS Host 上验证 CoreSimulator 动态生命周期，不代表真实 iPhone、IPA 安装或 iOS 业务执行器已经接入。下列证据已隐藏 Host 地址、完整 UDID、Token、密码和 Session Grant。

## 环境与拓扑

- macOS 26.5.1（arm64），Xcode 26.3（Build 17C529）；
- 可用 Runtime 包含 iOS 18.6 和 iOS 26.3；
- Node.js 22.23.2、Appium 3.6.0、Device Farm 12.0.1、XCUITest 12.4.0；
- Device Farm Server、Host Agent 和 PostgreSQL 使用本任务构建；
- 本机 Appium Hub 监听 `127.0.0.1:4723`；动态发现 Node 监听 `127.0.0.1:4724`，每 5 秒把本机 Simulator inventory 注册到 Hub；
- Session Fence 只连接 Hub。PostgreSQL Scheduler 先确定唯一 UDID，Appium Device Farm 不承担跨 Host 自动选机。

## 自动化验证

以下完整验证通过：

```text
go test ./...
cd console && pnpm test
cd console && pnpm build
```

Go 全部包通过；Console 7 个测试文件共 33 个测试通过并完成生产构建。覆盖内容包括：受控 Runtime/Device Type 目录、参数拒绝、容量结构化缺口、创建幂等、生命周期冲突、Provider 失败补偿、最后一台 Pool 设备删除、统一中文 API 提示以及 OpenAPI/生成客户端契约。

## 真实 Mac 验收

使用一台后台动态创建的受管 Simulator（仅记录脱敏 ID 前缀 `01f34c92…`）完成：

1. 从受控目录选择 Host、iOS Runtime 和 iPhone Device Type，Server 原子创建 provisioning Device、Pool membership 与 Host Command；Agent 执行 `simctl create/boot/bootstatus`，UDID 回写后设备收敛为 `ready/healthy`。
2. PostgreSQL Scheduler 为该 UDID 创建唯一 active Reservation；Session Grant 经 Fence 在本机 Hub 建立真实 XCUITest/WDA Session，最小 `/source` 查询成功并返回 40515 个字符。
3. 删除 Appium Session 并释放 Reservation 后，插件 busy 收敛为 false；无活动 Session 时 stop/start 均成功。
4. Agent 重启后按受管名称和 UDID 重新发现设备，没有创建重复 Simulator。
5. erase rebuild 成功，CoreSimulator UDID 保持不变并恢复 `ready/healthy`。
6. 正式 DELETE API/Host Command 成功，CoreSimulator 中该 UDID 消失，Device 保留为 deleted 审计记录。
7. active Reservation/Session 存在时，rebuild 和 delete 均返回中文冲突说明，运行中的自动化没有被中断。
8. 注入容量不足后返回 `宿主机资源不足：内存还缺 1096 MB，磁盘还缺 6384 MB`，同时包含结构化 `details.shortfall`；数据库没有半条 Device、Command 或 membership。
9. 注入 create 后 inventory 故障时，Agent 按刚创建的受管 UDID 直接执行 CoreSimulator 清理；首次联调遗留的一台受管 Simulator 也已通过正式 DELETE 链路清理，没有绕过审计修改数据库。

## 最终清理状态

```text
active_reservations=0
active_device_sessions=0
open_appium_sessions=0
active_host_commands=0
managed_nondeleted_devices=0
provider_busy_devices=0
pool_ready_busy_total=0|0|1
host=online|schedulable
managed_core_simulators=0
server_agent_hub_node=running
```

Pool 删除最后一台设备后 `total_target=0`，`max_concurrency` 仍按数据库约束保留最小值 1，不再阻断设备清理。

## 结论与边界

DF-044 的动态创建、真实 Session、停止/启动、重建、删除、容量拒绝、活动占用保护、Agent 重启发现和失败补偿均通过。DF-046 要求的至少 50 次循环、Host/Agent/Appium 故障恢复、升级回滚和 Android 全量回归尚未在本任务冒充完成，继续由 DF-046 独立验收。

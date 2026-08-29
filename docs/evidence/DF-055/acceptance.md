# DF-055 简化设备运行视图并清理历史噪声入口

## 结论

2026-08-29 通过。控制台日常设备页只保留可用、使用中和故障三个视图，不再展示 37 条已删除 Device 和 4,365 条底层健康事件。宿主机主表不再显示高频变化的精确心跳时间，详情仍可用于故障诊断。

数据库历史未硬删除。已删除 Device 仍被 190 条 Reservation、190 条 Session、3 条 Provisioning Job、34 条 Pool Membership 和 1 个 Pool Base 引用；物理删除会破坏外键、审计和故障追溯。心跳仍是离线和容量判定的实时输入，健康事件仍作为后台诊断证据；本任务只清理误导日常操作的入口。

## 边界与复用

- 开发前按项目强制规则复核设备域设计、Alcor 新一代对齐、MVP 规格、实施顺序、验收计划和相关 ADR。
- 已在迁移后的 `D:/AutoTestTools/Projects/Alcor` 和 `D:/AutoTestTools/Projects/dafit_auto_platform` 核对可复用能力；本任务只调整 Device Farm Console 展示和现有 Pool API 参数，没有新建 Appium、STF、Runner、报告或 Alcor 业务能力。
- 心跳、健康收敛、Reservation、Session Fence 和审计的服务端语义未改变。

## 现场恢复

环境：Device Farm Server `10.0.30.171`，macOS Host `10.0.33.68`。

1. 原 Android 设备真实 ADB、Appium 和 STF 均在线，但数据库为永久隔离。通过正式 Pool 目标 `1 -> 0 -> 1` 删除并自动重建，新 Device 为 `ef26de28-b0d8-4afa-894d-7a8b1d3716ad`。
2. 原 iOS Simulator 在人工验证时绕过 Reservation/Fence 直连 Appium，正确触发 `IOS_PROVIDER_BUSY_WITHOUT_RESERVATION` 防双占收敛。清理 Appium 技术绑定后，通过正式 Pool API 把 iOS 目标 `2 -> 0 -> 2`，故障 Simulator 的 delete Host Command 成功，随后自动重建两台新设备。
3. 恢复后 Pool 目标为 Android `1/1/1`、iOS `2/2/2`；两台 Host 均为 `online`，心跳延迟不超过 1 秒。

## 真实设备验收

| 设备 | 预约与自动化 | 最小页面查询 | 释放 |
| --- | --- | --- | --- |
| Android `ef26de28-...` | 正式 Reservation 激活，UiAutomator2 Session 成功 | `/source` HTTP 200，24,401 字节 | Session 删除、Reservation 释放成功 |
| iOS `32155337-...` | 正式 Reservation + 一次性 Grant + Fence，XCUITest Session 成功 | `/source` HTTP 200，45,980 字节 | Fence 关闭、Reservation 释放成功 |
| iOS `eb3aad90-...` | 与上一台同时预约到不同 UDID，XCUITest Session 成功 | `/source` HTTP 200，45,980 字节 | Fence 关闭、Reservation 释放成功 |

macOS CoreSimulator 最终两台均为 `Booted / isAvailable=true`；Server 心跳快照为 `ready / healthy / providerState=Booted / providerBusy=false`。两台 iOS 同时预约时设备 ID 不同，未发生双分配。

最终清理状态：

```text
pending/active Reservation = 0
starting/active/closing Session = 0
pending/leased Host Command = 0
Android ready/healthy = 1
iOS ready/healthy = 2
```

## 控制台验收

- 移除“健康事件”菜单和页面，旧 `/health-events` 路由安全跳转到故障设备视图。
- 移除“已删除历史”、“全部记录”和逐设备“健康记录”入口，删除后的资源不再出现在设备列表。
- 宿主机主表移除精确“最后心跳”列，详情仍保留。
- Pool 目标降为 0 时提交 `total_target=0 / min_ready=0 / max_concurrency=1`，真实生产 API 返回 200，可用于先清空故障资源再自动补建。

## 自动化回归

```text
Console Vitest: 9 个测试文件，50 项全部通过
Console production build: 通过
go test ./...: 通过
go vet ./...: 通过
git diff --check: 通过
```

构建仅有已知的 Vite 单 chunk 超过 500 kB 提示；测试仅有已知的 Ant Design `useForm` 警告，均不影响验收结果。

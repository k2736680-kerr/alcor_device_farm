# iOS Worker 接入：设备农场侧边界

本文说明 iOS Worker 接入时设备农场负责什么、不能负责什么。Worker 的 Run/Attempt 领取、iOS Executor 和业务结果处理必须在新版 Alcor/Worker 仓库实现；该仓库存在对应方案时，两边应以稳定 Device Farm API 和 OpenAPI 契约对齐，不能依赖本机绝对路径或未发布 commit。

---

## 一、两层调度不是一回事

| | 任务分发（改动所在） | 设备调度（本仓库职责） |
|---|---|---|
| 回答的问题 | 哪个 **Worker 进程** 领哪个 **Run** | 哪个 **Reservation** 拿哪台 **Device** |
| 归属 | **Alcor / alcor_server** | 本仓库 |
| 依据字段 | `platform_runs.executor_type` | `device_pools.platform` + 容量 |
| 核心代码 | 新版 Alcor/Worker 的领取循环 | `internal/scheduler/scheduler.go` 的设备分配循环 |
| 产出类型 | `claimedAttempt{RunID, AttemptID, ExecutorType}` | `Assignment{Reservation, Session}` |

Worker 分发是**任务层**的事，属于 Alcor 业务域。本仓库只在 Alcor 主动 `Reserve` 时，把一台匹配平台的设备分配出去。

## 二、本仓库不感知 `executor_type`

本仓库不保存或解释 `executor_type`、`platform_runs`。`run_attempt` 只作为“谁占用了设备”的 Owner 标签，不是任务类型：

```
adapters/alcor/types.go            const OwnerTypeRunAttempt = "run_attempt"
internal/reservation/service.go    case "run_attempt", "manual", "test_run":
```

设备调度器产出的是 `Assignment{Reservation, Session}`，围绕 `DeviceClaimer` 接口，与 Alcor 的 Run/Attempt 状态机无关。

## 三、符合 AGENTS.md 的边界约束

`AGENTS.md` 规则 3、10：

> 3. 本项目只实现设备域。禁止新增新版 Alcor 业务域的 Case、Dataset、Target、Config、Run、RunAttempt、评分、业务报告……
> 10. 本仓库不得发展成第二套评估平台、第二套 Eval Console 或第二套 Run/Result 数据库

把 Worker 分发放进本仓库，就是越界去实现 Run/Attempt 调度，**直接违反规则 3 与规则 10**。

## 四、本仓库需要做的（运维，非代码）

Alcor 侧 iOS Worker 会复用现成的 `adapters/alcor` 客户端，以下能力均已就绪，**无需改代码**：

| 能力 | 证据 | 状态 |
|---|---|---|
| `Lease` 能承载 iOS | `adapters/alcor` 从预约快照返回 `Serial`、`AppiumEndpoint` 和 `AppiumUDID`；iOS 使用 UDID，不要求 ADB | 已具备 |
| 预约链路平台中立 | Reservation 校验并归一化 `RequestedCapabilities.platformName` 为 Android/iOS | 已具备 |
| iOS 释放不误走 STF | Reservation 释放按平台调用 iOS Session Fence 清理，不走 Android STF | 已具备 |
| 设备表平台中立 | migration 和 Device/Host/Pool 模型均保存明确平台 | 已具备 |

运维前提：

1. macOS Host、Appium Device Farm、XCUITest、Session Fence 和 Baguette 按现有 iOS Host 部署包运行；iOS Simulator 只由 macOS/Xcode CoreSimulator 提供。
2. 设备农场已创建平台为 iOS 的 Host、Pool 和 Device；Provider 使用 `appium_device_farm_ios`。
3. Alcor 的 `Reserve` 请求必须带 `RequestedCapabilities{"platformName":"iOS"}`，并由设备农场校验其与 Pool 平台一致。
4. Worker 取得 active Reservation 后，使用同一设备的 `AppiumEndpoint` 和 `AppiumUDID`；创建 Session 时不得让 Appium Device Farm 再次自由选机。

## 五、接口注意事项

`Lease.ADBEndpoint` 对 iOS **恒为空字符串**。Alcor 侧 iOS executor 不得复用 `AndroidExecutor.prepareADB()`，应改用 `Lease.AppiumEndpoint` + `Lease.AppiumUDID`。

Worker 应以本次请求的 `platformName` 和返回的明确 Appium UDID 选择执行器，不得仅依赖 `ADBEndpoint == ""` 猜测平台。若未来需要在 `Lease` 顶层增加 `Platform`，必须先更新 OpenAPI、三方对齐表和 Adapter 契约，不能只改单方结构体。

## 六、版本与验收

正式集成前必须在新版 Alcor/Worker 的实际目标分支核对 Device Farm Adapter 依赖，不得继续引用缺少 iOS 能力的历史 commit，也不得把本机 `replace` 当作发布方式。至少完成以下契约验收：

1. iOS Run 只由 iOS Executor 领取，Android Run 行为不变；
2. `Reserve` 明确携带 `platformName=iOS`，返回的 Device、UDID、Endpoint 与 active Reservation 一致；
3. Worker 通过一次性 Session Grant 和对应 Host Fence 创建 XCUITest Session；
4. 成功、失败、取消和超时都释放同一 Reservation，不留下 Appium busy 或技术 Session；
5. 结果、报告和 Artifact 只写入 Alcor，不写入设备农场。

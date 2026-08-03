# Mock Provider

## 用途与边界

Mock Provider 用于 E0 开发环境验证 Image、Host、Pool、Device、Reservation、Scheduler 和故障恢复，不需要 Docker、KVM、STF、ADB、Appium 或 PostgreSQL。它实现与后续 Docker Emulator 和 USB 真机相同的 `providers.Provider` 接口，但不执行 DaFit 页面操作，也不模拟 WebDriver 协议。

统一接口包括：`Discover / Create / Start / Stop / Restart / Rebuild / Delete / InspectHealth / GetConnectionInfo`。

## 正常链路

```text
Create → created
Start  → running + ADB online + boot completed + Appium healthy
InspectHealth → ready
Stop → stopped
Restart → running + ready
Rebuild → generation + 1 + running + ready
Delete → inventory 中移除
```

每台 Mock Device 获得稳定 serial、独立 ADB Endpoint 和独立 Appium Endpoint。上层只读取连接信息，不会因 Mock 而改变 Device、Reservation 或 Session 模型。

## 故障场景

`mock.Scenario` 可以在测试过程中替换，配置立即对后续操作生效：

| 配置 | 稳定错误码 |
|---|---|
| `CreateFailure` | `EMULATOR_CREATE_FAILED` |
| `StartFailure` | `EMULATOR_START_FAILED` |
| `Offline` | `HOST_OFFLINE` / `DEVICE_OFFLINE` |
| `BootTimeout` | `DEVICE_BOOT_TIMEOUT` |
| `AppiumUnhealthy` | `APPIUM_UNHEALTHY` |
| `DeleteFailure` | `EMULATOR_DELETE_FAILED` |
| 操作 delay + 上下文超时 | `PROVIDER_OPERATION_TIMEOUT` |

延迟通过可注入 `Sleeper` 执行，单元测试不等待真实时间。Mock 不启动后台 goroutine，不持有数据库连接；所有内存设备都归单个 Provider 实例所有，测试释放实例后没有外部残留。

# DF-007 验收证据

## 交付内容

- `internal/providers/provider.go`：统一 Provider 接口、Snapshot、Health、ConnectionInfo 和稳定错误；
- `internal/providers/mock/mock.go`：完整内存 Mock Provider；
- `docs/mock_provider.md`：正常生命周期、故障配置和边界；
- Mock 单元测试：正常链路、端口隔离、故障确定性、延迟/超时和 goroutine 残留。

统一接口覆盖：

```text
Discover
Create
Start
Stop
Restart
Rebuild
Delete
InspectHealth
GetConnectionInfo
```

后续 Docker Emulator Provider 和 USBPhysicalDeviceProvider 都实现同一接口，上层 Device、Pool、Reservation、Session、Scheduler 和 API 不需要按模拟器/真机拆两套。

## 验收测试

执行：

```powershell
go test -count=1 -v ./internal/providers/...
```

结果：

```text
PASS TestMockProviderHappyLifecycleCreatesReadyDevice
PASS TestMockProviderFailureScenariosAreDeterministic
  PASS create_failure
  PASS start_failure
  PASS offline
  PASS boot_timeout
  PASS Appium_unhealthy
  PASS delete_failure
PASS TestMockProviderDelayAndTimeoutInjection
PASS TestMockProviderCreatesNoBackgroundGoroutines
```

正常链路在没有 Docker、KVM、STF、ADB、Appium 和 PostgreSQL 的情况下完成：创建两台 Device、分配不同 ADB/Appium 端口、启动为 ready、发现 inventory、重建、停止、重启和删除。

故障场景连续执行两次均返回相同稳定错误码，证明可以确定性复现。创建延迟由可注入 Sleeper 验证，不等待真实 17 秒；超时稳定映射为 `PROVIDER_OPERATION_TIMEOUT`。

Mock 实现没有 `go` 后台任务，不导入数据库包。测试连续创建、启动、删除 100 个独立 Provider 后 goroutine 数量没有增长，也没有数据库连接或外部资源残留。

## 工程回归

执行 `scripts/dev.ps1 -Task check`，`gofmt`、`go vet ./...`、`go test ./...`、Server 构建和 Agent 构建全部通过。

## 验收结论

DF-007 已提供可控且无外部依赖的设备基础设施替身，可以支撑 DF-008 管理 API 和后续 Scheduler/Reconciler 的成功与故障测试。

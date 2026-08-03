# DF-025 实施与验收证据

## 结论

新版 Alcor Device Farm Adapter 契约包已完成，可在没有 PostgreSQL、Docker 和真实设备的环境中独立验证 RunAttempt 申请、pending 到 active、设备连接信息、续租、幂等释放、容量不足和基础设施失败。DF-025 状态为 `completed`；真实 Alcor Worker 联调仍属于 ALCOR-001，不在本步骤冒充完成。

## 交付内容

- 冻结 OpenAPI `1.0.0`，明确 Run/RunAttempt 关联 Header、Reservation/Device 响应和稳定错误码；
- `adapters/alcor` 类型化示例客户端，提供 Reserve、WaitActive、Extend、Release 和错误决策；
- `cmd/device-farm-adapter-mock` 独立 Mock Server，支持 happy、capacity unavailable 和 infra failure；
- `examples/alcor-adapter-client` 可运行示例；
- RunAttempt/Reservation 时序、错误映射、版本和正式接入说明；
- OpenAPI、Mock、客户端生命周期和故障映射自动测试。

## 自动验收

```text
PASS RunAttempt 创建 pending 预约并保持 owner_type=run_attempt
PASS 轮询进入 active 并取得 Device、ADB、Appium Endpoint、Appium UDID
PASS extension 相同幂等键不重复增加租期
PASS release 可重放并保持 released
PASS DEVICE_CAPACITY_UNAVAILABLE -> retry_capacity
PASS KVM_UNAVAILABLE(non-retryable) -> infra_failed
PASS 等待 active 超时 -> retry_capacity
PASS OpenAPI 只使用 Case/Run/RunAttempt 新语义和冻结错误码
```

验收命令：

```powershell
./scripts/dev.ps1 -Task check
./scripts/verify-migrations.ps1 -RunRepositoryTests
```

## 边界复核

- 没有新增 Alcor Case、Run、RunAttempt、结果或 Artifact 表；
- 没有复制 Scheduler、Reservation、Device、STF、Appium 或 DaFit 执行逻辑；
- active 后复用现有 Reservation 和 Device API，不新增第二套 Session 真相；
- Mock 仅用于契约测试，不参与生产降级；
- Service Token 不进入日志、示例输出或命令行参数；
- 真实 Alcor 接口未发布前，不猜测其最终状态枚举和代码目录。

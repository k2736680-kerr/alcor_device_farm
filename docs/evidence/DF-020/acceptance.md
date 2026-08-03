# DF-020 实施与验收证据

## 当前结论

DaFit 端到端 Harness 已完成本地可验证实现：申请设备、等待分配、注入连接信息、调用 DaFit 原入口、检查报告并在所有退出路径释放预约。当前机器没有 Linux KVM、Docker Emulator、STF 和远程 Appium，无法完成真实无人值守冒烟与 Reaper 实机兜底验证，因此 DF-020 状态为 `blocked`，不能标记 `completed`。

## 已完成交付

- 新增 `cmd/dafit-farm-harness` 单一命令入口；
- 使用服务 Token 创建 `owner_type=test_run` 的 Reservation，并轮询到 `active`；
- 从同一分配 Device 读取宿主机 ADB Endpoint、容器内 Appium UDID 和 Appium Endpoint；
- 网络 ADB Endpoint 先执行明确的 `adb connect`，不自动选择第一台设备；
- 调用 DaFit 原有 `python tools/run_full.py --case STEPS_SMOKE_001`，未复制 Appium Session、页面、动作、断言、Runner 或报告；
- 为每次运行注入独立绝对报告目录，并检查原有 `report.html`、`report.json`；
- 成功、用例失败、等待容量超时、DaFit 命令超时、Ctrl+C 和进程终止均通过独立清理上下文释放 Reservation；
- pending 且未分配设备的预约可取消为 `failed/RESERVATION_CANCELED`，并写入审计；
- Scheduler 正在 claim 时，Harness 对 release 做短时重试；已经进入终态的 release 保持幂等；
- DaFit 子进程不会继承 Device Farm 服务 Token、数据库地址或 STF Token；
- Harness 标准输出只返回 Reservation、Device 和报告路径，不把服务 Token 放入参数或结果。

## 本地验收结果

执行：

```powershell
$env:DEVICE_FARM_GO='E:\AutoTestTools\Tools\go1.26.5\go\bin\go.exe'
$env:DEVICE_FARM_POSTGRES_BIN='E:\AutoTestTools\Tools\PostgreSQL-17.10\pgsql\bin'
.\scripts\dev.ps1 -Task check
.\scripts\verify-migrations.ps1 -RunRepositoryTests
```

通过项：

```text
PASS Harness 成功运行、报告收集和 finally release
PASS DaFit 故意失败仍保留报告并 release
PASS 等待 active 超时仍取消 pending Reservation
PASS DaFit 命令超时仍 release
PASS DaFit 子进程不继承 Device Farm 敏感变量
PASS pending Reservation 取消状态和审计集成测试
PASS gofmt、go vet、go test、三程序 build
PASS migration up/down/up 和 Repository 集成测试
```

## 真实环境验收步骤

1. 在 Linux KVM 服务器部署 Device Farm、两台 Docker Emulator、STF 和独立 Appium Endpoint；
2. 启动 Server、Agent、Scheduler、Reaper 和 Reconciler；
3. 使用 Harness 执行 DaFit `STEPS_SMOKE_001`，确认无人值守完成并生成独立 HTML/JSON 报告；
4. 故意制造 DaFit 断言失败，确认报告保留且设备释放；
5. 执行期间终止 Harness，确认 finally release 成功；再模拟进程被强制杀死，确认租约过期后 Reaper 回收；
6. 检查 Reservation、Session、Device、STF claim 和审计最终一致，无永久 reserved/busy 悬挂；
7. 保存脱敏命令、状态查询、日志、报告路径和清理结果。

## 阻塞解除条件

DF-014～DF-019 的真实环境依赖可用，并完成上述成功、故意失败、中断和 Reaper 兜底场景后，将 DF-020 改为 `completed`。本地单元测试和 Mock API 只证明编排逻辑，不替代真实 Emulator/Appium/DaFit 验收。
